package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// normalizeArchiveSource validates the vault-relative source path is under
// <vault>/archive/ and returns its slash form plus absolute path.
func normalizeArchiveSource(vault Vault, raw string) (string, string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", "", fmt.Errorf("brain archive: source is required")
	}
	if filepath.IsAbs(clean) {
		abs := filepath.Clean(clean)
		if !isWithin(vault.Root, abs) {
			return "", "", fmt.Errorf("brain archive: %q is outside vault %s", raw, vault.Sphere)
		}
		rel, err := filepath.Rel(vault.Root, abs)
		if err != nil {
			return "", "", fmt.Errorf("brain archive: rel %q: %w", abs, err)
		}
		clean = rel
	}
	clean = filepath.Clean(clean)
	relSlash := filepath.ToSlash(clean)
	if relSlash == "" || strings.HasPrefix(relSlash, "..") {
		return "", "", fmt.Errorf("brain archive: %q resolves outside vault", raw)
	}
	if relSlash == "personal" || strings.HasPrefix(relSlash, "personal/") {
		return "", "", fmt.Errorf("brain archive: %q is inside personal/ (off limits)", raw)
	}
	if !strings.HasPrefix(relSlash, "archive/") && relSlash != "archive" {
		return "", "", fmt.Errorf("brain archive: source must be under <vault>/archive/, got %q", raw)
	}
	if relSlash == "archive" {
		return "", "", fmt.Errorf("brain archive: refusing to archive the entire archive/ root")
	}
	if IsProtectedPath(relSlash) {
		return "", "", fmt.Errorf("brain archive: %q is in a protected brain area", raw)
	}
	return relSlash, filepath.Join(vault.Root, filepath.FromSlash(relSlash)), nil
}

// computeGigaDest derives the destination path under the giga archive base.
// Stripped: leading "archive/" segment from source. Suffix: ".tar.xz" for
// directories, original name for files. Includes a leading "<vault>/" path
// component so work and private trees stay separate.
func computeGigaDest(vault Vault, srcRel, kind string) string {
	stripped := strings.TrimPrefix(srcRel, "archive/")
	vaultName := vaultArchiveName(vault.Sphere)
	leaf := stripped
	if kind == "dir" {
		leaf = stripped + ".tar.xz"
	}
	return filepath.ToSlash(filepath.Join(vaultName, leaf))
}

func vaultArchiveName(sphere Sphere) string {
	switch sphere {
	case SphereWork:
		return "Nextcloud"
	case SpherePrivate:
		return "Dropbox"
	}
	return string(sphere)
}

// findFolderNote searches for an existing folder note that matches the
// source path. Returns vault-relative folder-note path and its body, or
// empty strings if no folder note exists.
func findFolderNote(vault Vault, srcRel string) (string, string) {
	candidate := filepath.ToSlash(filepath.Join("brain", "folders", srcRel)) + ".md"
	abs := filepath.Join(vault.Root, filepath.FromSlash(candidate))
	body, err := os.ReadFile(abs)
	if err != nil {
		return "", ""
	}
	return candidate, string(body)
}

// dissectFolderNote pulls the entity wikilinks plus a Distilled-facts seed
// from the folder note. Returns wikilinks (not paths), distilled prose, and
// the frozen body to embed in the archive-event note's last section.
func dissectFolderNote(body string) (people, projects, topics, institutions []string, distilled, frozen string) {
	if strings.TrimSpace(body) == "" {
		return nil, nil, nil, nil,
			"To be filled in: what is worth remembering about this folder.\n",
			"(no folder note existed at archive time)\n"
	}
	note, _ := ParseMarkdownNote(body, MarkdownParseOptions{})
	people = wikilinkValues(listField(note, "people"))
	projects = wikilinkValues(listField(note, "projects"))
	topics = wikilinkValues(listField(note, "topics"))
	institutions = wikilinkValues(listField(note, "institutions"))
	distilled = composeDistilledSeed(note)
	frozen = strings.TrimSpace(body) + "\n"
	return
}

// wikilinkValues normalises "[[people/Foo]]" to "people/Foo" entries.
func wikilinkValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		clean := strings.TrimSpace(raw)
		clean = strings.TrimPrefix(clean, "[[")
		clean = strings.TrimSuffix(clean, "]]")
		if pipe := strings.Index(clean, "|"); pipe >= 0 {
			clean = clean[:pipe]
		}
		clean = strings.TrimSpace(clean)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return out
}

// composeDistilledSeed builds the pre-filled Distilled-facts paragraph from
// the folder note's Summary and Key Facts sections.
func composeDistilledSeed(note *MarkdownNote) string {
	var b strings.Builder
	if section, ok := note.Section("Summary"); ok {
		body := strings.TrimSpace(section.Body)
		if body != "" {
			b.WriteString(body)
			b.WriteString("\n")
		}
	}
	if section, ok := note.Section("Key Facts"); ok {
		body := strings.TrimSpace(section.Body)
		if body != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(body)
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return "Folder note had no Summary or Key Facts content. Hand-write the durable facts before apply.\n"
	}
	return b.String()
}

// archiveEventInputs is the bag of fields that buildArchiveEventBody
// stitches into the archive-event note Markdown.
type archiveEventInputs struct {
	Sphere        Sphere
	ArchivedAt    string
	SourceRel     string
	GigaDest      string
	Sha256        string
	SizeBytes     string
	People        []string
	Projects      []string
	Topics        []string
	Institutions  []string
	DistilledFact string
	FrozenFolder  string
	Basename      string
	Kind          string
}

func buildArchiveEventBody(in archiveEventInputs) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("kind: archive-event\n")
	fmt.Fprintf(&b, "sphere: %s\n", in.Sphere)
	fmt.Fprintf(&b, "archived_at: %s\n", in.ArchivedAt)
	fmt.Fprintf(&b, "source_folder: %s\n", in.SourceRel)
	fmt.Fprintf(&b, "cold_archive: %s\n", in.GigaDest)
	fmt.Fprintf(&b, "sha256: %s\n", in.Sha256)
	fmt.Fprintf(&b, "size_bytes: %s\n", in.SizeBytes)
	emitYAMLList(&b, "people", in.People)
	emitYAMLList(&b, "projects", in.Projects)
	emitYAMLList(&b, "topics", in.Topics)
	emitYAMLList(&b, "institutions", in.Institutions)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# Archive: %s\n\n", in.Basename)
	b.WriteString("## Distilled facts\n\n")
	b.WriteString(strings.TrimRight(in.DistilledFact, "\n"))
	b.WriteString("\n\n")
	b.WriteString("## Recovery\n\n")
	b.WriteString(buildRecoverySnippet(in))
	b.WriteString("\n## Source folder note\n\n")
	b.WriteString(strings.TrimRight(in.FrozenFolder, "\n"))
	b.WriteString("\n")
	return b.String()
}

func buildRecoverySnippet(in archiveEventInputs) string {
	if in.Kind == "dir" {
		return "```bash\n" +
			"ssh giga 'pixz -d < /mnt/files/archive/chris/" + in.GigaDest + "' | tar -xC /tmp/restore-" + in.ArchivedAt + "\n" +
			"```\n"
	}
	return "```bash\n" +
		"scp giga:/mnt/files/archive/chris/" + in.GigaDest + " /tmp/\n" +
		"```\n"
}

func emitYAMLList(b *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		fmt.Fprintf(b, "%s: []\n", key)
		return
	}
	fmt.Fprintf(b, "%s:\n", key)
	for _, v := range values {
		fmt.Fprintf(b, "  - \"[[%s]]\"\n", v)
	}
}

// buildBacklinkEdits computes the `## Archive` H2 bullet to append to each
// canonical entity note referenced by the folder note.
func buildBacklinkEdits(eventRel, srcRel, basename string, when time.Time,
	people, projects, topics, institutions []string) []ArchiveBacklink {
	var edits []ArchiveBacklink
	bullet := fmt.Sprintf("- %s: archived %s -> [[archive-events/%s]]",
		when.Format("2006-01-02"),
		strings.TrimPrefix(srcRel, "archive/"),
		strings.TrimSuffix(strings.TrimPrefix(eventRel, "brain/archive-events/"), ".md"))
	for _, group := range [][]string{people, projects, topics, institutions} {
		for _, target := range group {
			canonical := filepath.ToSlash(filepath.Join("brain", target+".md"))
			edits = append(edits, ArchiveBacklink{CanonicalNote: canonical, Bullet: bullet})
		}
	}
	sort.Slice(edits, func(i, j int) bool {
		return edits[i].CanonicalNote < edits[j].CanonicalNote
	})
	return edits
}

// appendArchiveBacklink finds (or creates) the `## Archive` H2 in path and
// appends the bullet line. If the section already contains an identical
// bullet, the file is left untouched.
func appendArchiveBacklink(path, bullet string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			content := "\n## Archive\n\n" + bullet + "\n"
			return os.WriteFile(path, []byte(content), 0o644)
		}
		return err
	}
	content := string(data)
	if strings.Contains(content, bullet) {
		return nil
	}
	if idx := strings.Index(content, "\n## Archive\n"); idx >= 0 {
		insertAt := idx + len("\n## Archive\n")
		updated := content[:insertAt] + "\n" + bullet + content[insertAt:]
		updated = collapseBlankLineRun(updated, insertAt)
		return os.WriteFile(path, []byte(updated), 0o644)
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "\n## Archive\n\n" + bullet + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// collapseBlankLineRun keeps the post-heading blank line but avoids two
// consecutive empty lines after the inserted bullet.
func collapseBlankLineRun(s string, anchor int) string {
	if anchor >= len(s) {
		return s
	}
	for strings.HasPrefix(s[anchor:], "\n\n\n") {
		s = s[:anchor] + s[anchor+1:]
	}
	return s
}

// stageArchiveLocal materialises the source as either a .tar.xz tarball or
// (for single-file sources) a verbatim copy in stagingDir, computes its
// sha256, and reports the staging file path, hex digest, and size.
func stageArchiveLocal(srcAbs string, plan *ArchivePlan, stagingDir string) (string, string, int64, error) {
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return "", "", 0, err
	}
	stagingName := filepath.Base(plan.GigaDest)
	stagingPath := filepath.Join(stagingDir, stagingName)
	if plan.SourceKind == "dir" {
		if err := tarPixzDirectory(srcAbs, stagingPath); err != nil {
			return "", "", 0, err
		}
	} else {
		if err := copyFile(srcAbs, stagingPath); err != nil {
			return "", "", 0, err
		}
	}
	sha, size, err := sha256AndSize(stagingPath)
	if err != nil {
		return "", "", 0, err
	}
	return stagingPath, sha, size, nil
}

// tarPixzDirectory streams `tar -C parent -cf - basename | pixz -9 > out`.
func tarPixzDirectory(srcAbs, outPath string) error {
	parent := filepath.Dir(srcAbs)
	base := filepath.Base(srcAbs)
	tarCmd := exec.Command("tar", "-C", parent, "-cf", "-", base)
	pixzCmd := exec.Command("pixz", "-9")
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	pipe, err := tarCmd.StdoutPipe()
	if err != nil {
		return err
	}
	pixzCmd.Stdin = pipe
	pixzCmd.Stdout = out
	pixzCmd.Stderr = os.Stderr
	tarCmd.Stderr = os.Stderr
	if err := pixzCmd.Start(); err != nil {
		return fmt.Errorf("start pixz: %w", err)
	}
	if err := tarCmd.Run(); err != nil {
		_ = pixzCmd.Wait()
		return fmt.Errorf("tar: %w", err)
	}
	if err := pixzCmd.Wait(); err != nil {
		return fmt.Errorf("pixz: %w", err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sha256AndSize(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// scpToGiga uploads stagingPath to the remote and ensures the destination
// directory exists. Uses ssh + cat rather than scp's create-with-rename
// behaviour so partial failures leave no dangling tmp files on giga.
func scpToGiga(stagingPath, host, remotePath string) error {
	mkdirCmd := exec.Command("ssh", host, "mkdir -p "+shellQuote(filepath.Dir(remotePath)))
	mkdirCmd.Stderr = os.Stderr
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("ssh mkdir: %w", err)
	}
	in, err := os.Open(stagingPath)
	if err != nil {
		return err
	}
	defer in.Close()
	cmd := exec.Command("ssh", host, "cat > "+shellQuote(remotePath))
	cmd.Stdin = in
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// verifyGigaSha256 runs `sha256sum` on the remote and compares with want.
func verifyGigaSha256(host, remotePath, want string) error {
	cmd := exec.Command("ssh", host, "sha256sum "+shellQuote(remotePath))
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("ssh sha256sum: %w", err)
	}
	parts := strings.Fields(string(out))
	if len(parts) < 1 {
		return fmt.Errorf("unexpected sha256sum output: %q", out)
	}
	if parts[0] != want {
		return fmt.Errorf("sha256 mismatch: local %s, remote %s", want, parts[0])
	}
	return nil
}

// canonicalArchiveDigest hashes the plan fields that bind apply: source,
// destination, event note path, event note body, folder note path, and the
// sorted backlink edits. SourceSizeBytes is intentionally excluded so a
// minor file-tree mtime change between plan and apply does not invalidate
// the digest.
func canonicalArchiveDigest(plan *ArchivePlan) string {
	edits := append([]ArchiveBacklink(nil), plan.BacklinkEdits...)
	sort.Slice(edits, func(i, j int) bool {
		return edits[i].CanonicalNote < edits[j].CanonicalNote
	})
	payload := struct {
		Sphere      Sphere            `json:"sphere"`
		Source      string            `json:"source"`
		Kind        string            `json:"kind"`
		GigaDest    string            `json:"giga_dest"`
		EventPath   string            `json:"event_path"`
		EventBody   string            `json:"event_body"`
		FolderNote  string            `json:"folder_note"`
		Backlinks   []ArchiveBacklink `json:"backlinks"`
	}{plan.Sphere, plan.Source, plan.SourceKind, plan.GigaDest,
		plan.EventNotePath, plan.EventNoteBody, plan.FolderNotePath, edits}
	buf, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// treeSize returns the total size of all regular files under root.
func treeSize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// writeArchiveLog appends a row to brain/generated/archival-log.md so the
// human-readable audit log has parity with `brain move`.
func writeArchiveLog(vault Vault, plan *ArchivePlan, sha string) error {
	logPath := filepath.Join(vault.Root, "brain", "generated", "archival-log.md")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	short := plan.Digest
	if len(short) > 12 {
		short = short[:12]
	}
	line := fmt.Sprintf("%s  archive  %s  -> %s  (sha256 %s, plan %s)\n",
		time.Now().UTC().Format("2006-01-02"),
		plan.Source,
		"giga:/mnt/files/archive/chris/"+plan.GigaDest,
		sha[:12], short)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}
