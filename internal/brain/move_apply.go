package brain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// applyFileMoves performs the rename(2)/copy fallback per file, deleting the
// source after a copy. For deletions, just removes files and empty dirs.
func applyFileMoves(vault Vault, plan *MovePlan) error {
	if plan.To == nullDestination {
		return applyDeletion(vault, plan)
	}
	srcRoot := filepath.Join(vault.Root, filepath.FromSlash(plan.From))
	dstRoot := filepath.Join(vault.Root, filepath.FromSlash(plan.To))
	if err := os.MkdirAll(filepath.Dir(dstRoot), 0o755); err != nil {
		return fmt.Errorf("brain move: mkdir %q: %w", filepath.Dir(plan.To), err)
	}
	if err := os.Rename(srcRoot, dstRoot); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) && !isCrossDevice(err) {
		return fmt.Errorf("brain move: rename %q -> %q: %w", plan.From, plan.To, err)
	}
	if err := copyRecursive(srcRoot, dstRoot); err != nil {
		return err
	}
	return os.RemoveAll(srcRoot)
}

func applyDeletion(vault Vault, plan *MovePlan) error {
	files := append([]FileMove(nil), plan.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].From < files[j].From })
	var fileEntries, dirEntries []FileMove
	for _, f := range files {
		if f.IsDir {
			dirEntries = append(dirEntries, f)
		} else {
			fileEntries = append(fileEntries, f)
		}
	}
	for _, f := range fileEntries {
		abs := filepath.Join(vault.Root, filepath.FromSlash(f.From))
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("brain move: delete %q: %w", f.From, err)
		}
	}
	sort.Slice(dirEntries, func(i, j int) bool { return dirEntries[i].From > dirEntries[j].From })
	for _, d := range dirEntries {
		abs := filepath.Join(vault.Root, filepath.FromSlash(d.From))
		if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("brain move: rmdir %q: %w", d.From, err)
		}
	}
	return nil
}

func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "cross-device") || strings.Contains(msg, "EXDEV") || strings.Contains(msg, "invalid cross-device link")
}

func copyRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFilePreservingMeta(path, target)
	})
}

func copyFilePreservingMeta(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(dst, time.Now(), info.ModTime())
}

// applyEdits writes per-file line edits. Each file is read, the matched line
// is replaced exactly, then the file is written back.
func applyEdits(cfg *Config, edits []LinkEdit) error {
	grouped := map[string][]LinkEdit{}
	keys := []string{}
	for _, edit := range edits {
		key := string(edit.Sphere) + "\x00" + edit.Path
		if _, ok := grouped[key]; !ok {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], edit)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fileEdits := grouped[key]
		first := fileEdits[0]
		if first.Sphere == "" {
			return fmt.Errorf("brain move: edit missing sphere for %q", first.Path)
		}
		vault, ok := cfg.Vault(first.Sphere)
		if !ok {
			return &PathError{Kind: ErrorUnknownVault, Sphere: first.Sphere}
		}
		abs := filepath.Join(vault.Root, filepath.FromSlash(first.Path))
		if err := applyFileEdits(abs, fileEdits); err != nil {
			return err
		}
	}
	return nil
}

// applyInnerEdits applies edits that carry empty sphere; the source vault is
// supplied by the caller because translateInnerToDestination remaps paths
// after the file move.
func applyInnerEdits(vault Vault, edits []LinkEdit) error {
	grouped := map[string][]LinkEdit{}
	keys := []string{}
	for _, edit := range edits {
		if _, ok := grouped[edit.Path]; !ok {
			keys = append(keys, edit.Path)
		}
		grouped[edit.Path] = append(grouped[edit.Path], edit)
	}
	sort.Strings(keys)
	for _, key := range keys {
		abs := filepath.Join(vault.Root, filepath.FromSlash(key))
		if err := applyFileEdits(abs, grouped[key]); err != nil {
			return err
		}
	}
	return nil
}

func applyFileEdits(path string, edits []LinkEdit) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("brain move: read %q: %w", path, err)
	}
	lines := splitLinesPreserve(data)
	byLine := map[int]LinkEdit{}
	for _, edit := range edits {
		byLine[edit.Line] = edit
	}
	for i, line := range lines {
		edit, ok := byLine[i+1]
		if !ok {
			continue
		}
		if line.Text != edit.OldText {
			return fmt.Errorf("brain move: line %d in %q does not match expected old text", edit.Line, path)
		}
		if LineHasTODOMarker(edit.OldText) && LineRewriteTouchesNonLinkText(edit.OldText, edit.NewText) {
			return fmt.Errorf("brain move: refusing to rewrite TODO/checkbox line %d in %q (--protect-todos)", edit.Line, path)
		}
		lines[i].Text = edit.NewText
	}
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0o644); err != nil {
		return fmt.Errorf("brain move: write %q: %w", path, err)
	}
	return nil
}

type lineWithEnding struct {
	Text   string
	Ending string
}

// splitLinesPreserve splits data on '\n' while remembering the original line
// terminator so we can write the file back without changing line endings.
func splitLinesPreserve(data []byte) []lineWithEnding {
	var lines []lineWithEnding
	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			lines = append(lines, lineWithEnding{Text: string(data), Ending: ""})
			return lines
		}
		text := data[:idx]
		ending := "\n"
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text = text[:len(text)-1]
			ending = "\r\n"
		}
		lines = append(lines, lineWithEnding{Text: string(text), Ending: ending})
		data = data[idx+1:]
	}
	return lines
}

func joinLines(lines []lineWithEnding) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Text)
		b.WriteString(l.Ending)
	}
	return b.String()
}

// writeArchivalLog appends one row per move to the source vault's
// brain/generated/archival-log.md.
func writeArchivalLog(vault Vault, plan *MovePlan) error {
	logPath := filepath.Join(vault.Root, archivalLogRel)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("brain move: mkdir log dir: %w", err)
	}
	short := plan.Digest
	if len(short) > 12 {
		short = short[:12]
	}
	action := "move"
	switch {
	case plan.MergeTarget != "":
		action = "merge"
	case plan.To == nullDestination:
		action = "delete"
	case strings.HasPrefix(plan.To, "archive/") || strings.Contains(plan.To, "/archive/"):
		action = "archive"
	}
	dest := plan.To
	switch {
	case plan.MergeTarget != "":
		dest = "(merged into " + plan.MergeTarget + ")"
	case dest == nullDestination:
		dest = "(deleted)"
	}
	row := fmt.Sprintf("%s  %s  %s  -> %s  (digest %s)\n",
		time.Now().UTC().Format("2006-01-02"), action, plan.From, dest, short)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("brain move: open log: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(row); err != nil {
		return fmt.Errorf("brain move: write log: %w", err)
	}
	return nil
}
