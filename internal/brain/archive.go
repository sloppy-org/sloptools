package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ArchiveBacklink describes one bullet to append to a canonical note's
// `## Archive` section.
type ArchiveBacklink struct {
	CanonicalNote string `json:"canonical_note"` // vault-relative .md path
	Bullet        string `json:"bullet"`         // line to append (no trailing newline)
}

// ArchivePlan is the output of `sloptools brain archive plan`. ApplyArchive
// re-derives it from disk and refuses if the digest changed.
type ArchivePlan struct {
	Sphere          Sphere            `json:"sphere"`
	Source          string            `json:"source"`            // vault-relative source
	SourceKind      string            `json:"source_kind"`       // "dir" or "file"
	SourceSizeBytes int64             `json:"source_size_bytes"` // rough size estimate
	GigaDest        string            `json:"giga_dest"`         // remote path under giga archive base
	EventNotePath   string            `json:"event_note_path"`   // vault-relative archive-event note
	EventNoteBody   string            `json:"event_note_body"`   // pre-apply draft (sha256 placeholder)
	FolderNotePath  string            `json:"folder_note_path,omitempty"`
	BacklinkEdits   []ArchiveBacklink `json:"backlink_edits"`
	Digest          string            `json:"digest"`
	Notes           []string          `json:"notes,omitempty"`
}

// ArchiveApplyConfig carries the runtime knobs the apply step needs that
// are not part of the digest (host, base path, staging dir).
type ArchiveApplyConfig struct {
	GigaHost        string
	GigaArchiveBase string
	StagingDir      string
}

// archiveSha256Placeholder is the literal frontmatter token we emit during
// plan; apply substitutes the real digest after the tarball lands and
// remote sha256sum agrees with the local one.
const archiveSha256Placeholder = "PENDING-FILLED-AT-APPLY"

// archiveSizePlaceholder is the sentinel for the size_bytes frontmatter
// value that gets rewritten by apply.
const archiveSizePlaceholder = "0"

// PlanArchive resolves source under <vault>/archive/, locates the matching
// folder note (if any), and emits a plan that includes the archive-event
// note draft, the canonical-note backlink edits, and the giga destination
// path. No filesystem changes happen here.
func PlanArchive(cfg *Config, sphere Sphere, source string) (*ArchivePlan, error) {
	if cfg == nil {
		return nil, fmt.Errorf("brain archive: config is nil")
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return nil, &PathError{Kind: ErrorUnknownVault, Sphere: normalizeSphere(sphere)}
	}
	srcRel, srcAbs, err := normalizeArchiveSource(vault, source)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(srcAbs)
	if err != nil {
		return nil, fmt.Errorf("brain archive: stat %q: %w", srcRel, err)
	}
	kind := "file"
	var size int64
	if info.IsDir() {
		kind = "dir"
		size, err = treeSize(srcAbs)
		if err != nil {
			return nil, fmt.Errorf("brain archive: size %q: %w", srcRel, err)
		}
	} else {
		size = info.Size()
	}

	gigaDest := computeGigaDest(vault, srcRel, kind)

	folderRel, folderBody := findFolderNote(vault, srcRel)
	people, projects, topics, institutions, distilled, frozen := dissectFolderNote(folderBody)

	now := time.Now().UTC()
	basename := filepath.Base(srcRel)
	eventName := fmt.Sprintf("%s-%s.md", now.Format("2006-01-02"), basename)
	eventRel := filepath.ToSlash(filepath.Join("brain", "archive-events", eventName))

	body := buildArchiveEventBody(archiveEventInputs{
		Sphere:        sphere,
		ArchivedAt:    now.Format("2006-01-02"),
		SourceRel:     srcRel,
		GigaDest:      gigaDest,
		Sha256:        archiveSha256Placeholder,
		SizeBytes:     archiveSizePlaceholder,
		People:        people,
		Projects:      projects,
		Topics:        topics,
		Institutions:  institutions,
		DistilledFact: distilled,
		FrozenFolder:  frozen,
		Basename:      basename,
		Kind:          kind,
	})

	backlinks := buildBacklinkEdits(eventRel, srcRel, basename, now,
		people, projects, topics, institutions)

	plan := &ArchivePlan{
		Sphere:          sphere,
		Source:          srcRel,
		SourceKind:      kind,
		SourceSizeBytes: size,
		GigaDest:        gigaDest,
		EventNotePath:   eventRel,
		EventNoteBody:   body,
		FolderNotePath:  folderRel,
		BacklinkEdits:   backlinks,
	}
	plan.Digest = canonicalArchiveDigest(plan)
	return plan, nil
}

// ApplyArchive re-derives the plan from disk, performs the on-disk + on-
// giga writes in order, and rewrites the event note with the real sha256
// once the tarball lands and remote verification passes.
func ApplyArchive(cfg *Config, plan *ArchivePlan, confirm string, ac ArchiveApplyConfig) error {
	if plan == nil {
		return fmt.Errorf("brain archive: plan is nil")
	}
	if confirm != plan.Digest {
		return fmt.Errorf("brain archive: confirm digest %q does not match plan digest %q", confirm, plan.Digest)
	}
	fresh, err := PlanArchive(cfg, plan.Sphere, plan.Source)
	if err != nil {
		return fmt.Errorf("brain archive: re-derive plan: %w", err)
	}
	if fresh.Digest != plan.Digest {
		return fmt.Errorf("brain archive: plan digest changed since dry-run (have %q, fresh %q)", plan.Digest, fresh.Digest)
	}
	vault, ok := cfg.Vault(plan.Sphere)
	if !ok {
		return &PathError{Kind: ErrorUnknownVault, Sphere: plan.Sphere}
	}
	if ac.GigaHost == "" {
		ac.GigaHost = "giga"
	}
	if ac.GigaArchiveBase == "" {
		ac.GigaArchiveBase = "/mnt/files/archive/chris"
	}
	if ac.StagingDir == "" {
		ac.StagingDir = os.TempDir()
	}

	srcAbs := filepath.Join(vault.Root, filepath.FromSlash(plan.Source))

	stagingPath, sha, sizeBytes, err := stageArchiveLocal(srcAbs, plan, ac.StagingDir)
	if err != nil {
		return fmt.Errorf("brain archive: stage local: %w", err)
	}
	defer os.Remove(stagingPath)

	remotePath := filepath.ToSlash(filepath.Join(ac.GigaArchiveBase, plan.GigaDest))
	if err := scpToGiga(stagingPath, ac.GigaHost, remotePath); err != nil {
		return fmt.Errorf("brain archive: scp to giga: %w", err)
	}
	if err := verifyGigaSha256(ac.GigaHost, remotePath, sha); err != nil {
		return fmt.Errorf("brain archive: remote sha256 verify: %w", err)
	}

	body := strings.ReplaceAll(plan.EventNoteBody, archiveSha256Placeholder, sha)
	body = strings.Replace(body,
		"size_bytes: "+archiveSizePlaceholder,
		fmt.Sprintf("size_bytes: %d", sizeBytes), 1)

	eventAbs := filepath.Join(vault.Root, filepath.FromSlash(plan.EventNotePath))
	if err := os.MkdirAll(filepath.Dir(eventAbs), 0o755); err != nil {
		return fmt.Errorf("brain archive: mkdir event note dir: %w", err)
	}
	if err := os.WriteFile(eventAbs, []byte(body), 0o644); err != nil {
		return fmt.Errorf("brain archive: write event note: %w", err)
	}

	for _, edit := range plan.BacklinkEdits {
		abs := filepath.Join(vault.Root, filepath.FromSlash(edit.CanonicalNote))
		if err := appendArchiveBacklink(abs, edit.Bullet); err != nil {
			return fmt.Errorf("brain archive: backlink %q: %w", edit.CanonicalNote, err)
		}
	}

	if plan.FolderNotePath != "" {
		folderAbs := filepath.Join(vault.Root, filepath.FromSlash(plan.FolderNotePath))
		if err := os.Remove(folderAbs); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("brain archive: remove folder note: %w", err)
		}
	}

	if err := os.RemoveAll(srcAbs); err != nil {
		return fmt.Errorf("brain archive: remove local source: %w", err)
	}

	if err := writeArchiveLog(vault, plan, sha); err != nil {
		return fmt.Errorf("brain archive: append archival log: %w", err)
	}
	return nil
}
