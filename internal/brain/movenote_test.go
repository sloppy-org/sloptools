package brain

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// movePlanFixture builds a two-vault config with synthetic notes for the move
// tests. Layout (work vault):
//
//	work/brain/projects/old/index.md   <- single moved file (directory move)
//	work/brain/projects/old/child.md   <- nested file inside the moved tree
//	work/brain/projects/old/other.md   <- another nested file with self-links
//	work/brain/people/alice.md         <- wikilink [[projects/old/index]]
//	work/brain/people/bob.md           <- relative MD link to projects/old/index.md
//	work/brain/topics/deep.md          <- alias+anchor wikilink under projects/old
//	work/personal/secret.md            <- excluded from scan
//	private/brain/refs/note.md         <- cross-vault wikilink that must rewrite
func movePlanFixture(t *testing.T) (*Config, string) {
	t.Helper()
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{
		{Sphere: SphereWork, Root: filepath.Join(root, "work"), Brain: "brain"},
		{Sphere: SpherePrivate, Root: filepath.Join(root, "private"), Brain: "brain"},
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	work := cfg.mustVault(t, SphereWork)
	priv := cfg.mustVault(t, SpherePrivate)

	writeFile(t, filepath.Join(work.Root, "brain", "projects", "old", "index.md"),
		"# Old Project\n\nSee [[people/alice]] for context.\n")
	writeFile(t, filepath.Join(work.Root, "brain", "projects", "old", "child.md"),
		"# Child\n\nSibling note: [external](../../people/alice.md)\nSelf: [self](./other.md)\n")
	writeFile(t, filepath.Join(work.Root, "brain", "projects", "old", "other.md"),
		"# Other\n\nback to [child](./child.md)\n")
	writeFile(t, filepath.Join(work.Root, "brain", "people", "alice.md"),
		"# Alice\n\nWorks on [[projects/old/index]] this year.\n")
	writeFile(t, filepath.Join(work.Root, "brain", "people", "bob.md"),
		"# Bob\n\nDocumented in [old project](../projects/old/index.md).\n")
	writeFile(t, filepath.Join(work.Root, "brain", "topics", "deep.md"),
		"# Deep\n\nAnchor link [[projects/old/index#section|the project]] still works.\n")
	writeFile(t, filepath.Join(work.Root, "personal", "secret.md"),
		"# Secret\n\nMust not be scanned. [[projects/old/index]]\n")
	writeFile(t, filepath.Join(priv.Root, "brain", "refs", "note.md"),
		"# Private Note\n\nRefers to [[projects/old/index]] for completeness.\n")
	return cfg, root
}

func TestPlanMoveRewritesWikilinkAndRelativeMarkdownLink(t *testing.T) {
	cfg, _ := movePlanFixture(t)

	plan, err := PlanMove(cfg, SphereWork, "brain/projects/old", "brain/projects/new")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	if len(plan.Files) == 0 {
		t.Fatal("plan.Files is empty")
	}
	if plan.Digest == "" {
		t.Fatal("plan.Digest is empty")
	}

	// Wikilink in alice.md should be rewritten.
	foundAlice := false
	foundBob := false
	foundDeep := false
	foundPrivate := false
	for _, edit := range plan.Edits {
		if edit.Path == "brain/people/alice.md" && edit.Sphere == SphereWork {
			if !strings.Contains(edit.NewText, "[[projects/new/index]]") {
				t.Errorf("alice rewrite missing new target: %q", edit.NewText)
			}
			foundAlice = true
		}
		if edit.Path == "brain/people/bob.md" && edit.Sphere == SphereWork {
			if !strings.Contains(edit.NewText, "../projects/new/index.md") {
				t.Errorf("bob md-link rewrite wrong: %q", edit.NewText)
			}
			foundBob = true
		}
		if edit.Path == "brain/topics/deep.md" && edit.Sphere == SphereWork {
			if !strings.Contains(edit.NewText, "[[projects/new/index#section|the project]]") {
				t.Errorf("deep alias+anchor rewrite wrong: %q", edit.NewText)
			}
			foundDeep = true
		}
		if edit.Path == "brain/refs/note.md" && edit.Sphere == SpherePrivate {
			if !strings.Contains(edit.NewText, "[[projects/new/index]]") {
				t.Errorf("private cross-vault rewrite wrong: %q", edit.NewText)
			}
			foundPrivate = true
		}
		if strings.Contains(edit.Path, "personal/") {
			t.Errorf("personal/ path leaked into edits: %q", edit.Path)
		}
	}
	if !foundAlice {
		t.Error("alice wikilink rewrite missing")
	}
	if !foundBob {
		t.Error("bob markdown-link rewrite missing")
	}
	if !foundDeep {
		t.Error("deep alias+anchor wikilink rewrite missing")
	}
	if !foundPrivate {
		t.Error("private cross-vault wikilink rewrite missing")
	}
}

func TestPlanMoveInnerEditWhenSourceMoves(t *testing.T) {
	cfg, _ := movePlanFixture(t)
	plan, err := PlanMove(cfg, SphereWork, "brain/projects/old", "brain/projects/new")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	// child.md links to ../../people/alice.md, which stays put. After child.md
	// moves under brain/projects/new/child.md, the relative link must adjust.
	found := false
	for _, edit := range plan.Inner {
		if !strings.HasSuffix(edit.Path, "child.md") {
			continue
		}
		if !strings.Contains(edit.NewText, "../../people/alice.md") {
			// Old depth was projects/old/child.md -> ../../people/alice.md.
			// New depth is projects/new/child.md -> ../../people/alice.md.
			// Same depth, same relative link -> no edit emitted is correct.
			t.Errorf("unexpected inner rewrite: %q", edit.NewText)
		}
		found = true
	}
	// In this fixture old and new live at the same depth, so we should have
	// zero inner edits. Verify inner is empty by virtue of `found` staying
	// false.
	if found {
		t.Error("inner edits should be empty when depth is preserved")
	}
}

func TestPlanMoveInnerEditWhenDepthChanges(t *testing.T) {
	cfg, _ := movePlanFixture(t)
	// Deepen the destination so relative links to outside-the-tree must shift.
	plan, err := PlanMove(cfg, SphereWork, "brain/projects/old", "brain/archive/projects/old")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	found := false
	for _, edit := range plan.Inner {
		if !strings.HasSuffix(edit.Path, "child.md") {
			continue
		}
		if !strings.Contains(edit.NewText, "../../../people/alice.md") {
			t.Errorf("expected three-up rewrite, got %q", edit.NewText)
		}
		found = true
	}
	if !found {
		t.Error("expected inner edit for child.md after depth change")
	}
}

func TestPlanMoveRefusesCrossVault(t *testing.T) {
	cfg, _ := movePlanFixture(t)
	priv := cfg.mustVault(t, SpherePrivate)
	_, err := PlanMove(cfg, SphereWork, "brain/projects/old", filepath.Join(priv.Root, "brain", "elsewhere"))
	if err == nil {
		t.Fatal("cross-vault destination must fail")
	}
}

func TestPlanMoveRefusesPersonal(t *testing.T) {
	cfg, _ := movePlanFixture(t)
	if _, err := PlanMove(cfg, SphereWork, "personal/secret.md", "brain/projects/notes.md"); err == nil {
		t.Fatal("personal/ source must fail")
	}
	if _, err := PlanMove(cfg, SphereWork, "brain/projects/old", "personal/dest"); err == nil {
		t.Fatal("personal/ destination must fail")
	}
}

func TestPlanMoveDigestIsIdempotent(t *testing.T) {
	cfg, _ := movePlanFixture(t)
	plan1, err := PlanMove(cfg, SphereWork, "brain/projects/old", "brain/projects/new")
	if err != nil {
		t.Fatalf("PlanMove first: %v", err)
	}
	plan2, err := PlanMove(cfg, SphereWork, "brain/projects/old", "brain/projects/new")
	if err != nil {
		t.Fatalf("PlanMove second: %v", err)
	}
	if plan1.Digest != plan2.Digest {
		t.Fatalf("digest not stable: %q vs %q", plan1.Digest, plan2.Digest)
	}
}

func TestApplyMoveRejectsBadConfirm(t *testing.T) {
	cfg, root := movePlanFixture(t)
	plan, err := PlanMove(cfg, SphereWork, "brain/projects/old", "brain/projects/new")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	if err := ApplyMove(cfg, plan, "deadbeef"); err == nil {
		t.Fatal("ApplyMove with bad confirm must fail")
	}
	srcPath := filepath.Join(root, "work", "brain", "projects", "old", "index.md")
	if _, err := os.Stat(srcPath); err != nil {
		t.Errorf("source unexpectedly missing after rejected apply: %v", err)
	}
	dstPath := filepath.Join(root, "work", "brain", "projects", "new", "index.md")
	if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
		t.Errorf("destination created on rejected apply: err=%v", err)
	}
}

func TestApplyMoveSameFSPreservesMtime(t *testing.T) {
	cfg, root := movePlanFixture(t)
	srcPath := filepath.Join(root, "work", "brain", "projects", "old", "index.md")
	originalInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("stat source: %v", err)
	}
	originalMtime := originalInfo.ModTime()

	originalIno := inodeOf(t, srcPath)

	plan, err := PlanMove(cfg, SphereWork, "brain/projects/old", "brain/projects/new")
	if err != nil {
		t.Fatalf("PlanMove: %v", err)
	}
	if err := ApplyMove(cfg, plan, plan.Digest); err != nil {
		t.Fatalf("ApplyMove: %v", err)
	}
	dstPath := filepath.Join(root, "work", "brain", "projects", "new", "index.md")
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("stat destination: %v", err)
	}
	if !dstInfo.ModTime().Equal(originalMtime) {
		t.Errorf("mtime changed after rename: %v -> %v", originalMtime, dstInfo.ModTime())
	}
	if originalIno != 0 {
		newIno := inodeOf(t, dstPath)
		if newIno != originalIno {
			t.Errorf("inode changed: %d -> %d (rename should preserve)", originalIno, newIno)
		}
	}
	// Source must be gone.
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("source still present after successful apply: err=%v", err)
	}
	// Inbound wikilink in alice.md should now contain the new target.
	aliceData, err := os.ReadFile(filepath.Join(root, "work", "brain", "people", "alice.md"))
	if err != nil {
		t.Fatalf("read alice: %v", err)
	}
	if !strings.Contains(string(aliceData), "[[projects/new/index]]") {
		t.Errorf("alice not rewritten: %q", string(aliceData))
	}
	// Cross-vault private note should be rewritten too.
	privData, err := os.ReadFile(filepath.Join(root, "private", "brain", "refs", "note.md"))
	if err != nil {
		t.Fatalf("read private: %v", err)
	}
	if !strings.Contains(string(privData), "[[projects/new/index]]") {
		t.Errorf("private cross-vault not rewritten: %q", string(privData))
	}
	// Archival log row must exist.
	logData, err := os.ReadFile(filepath.Join(root, "work", "brain", "generated", "archival-log.md"))
	if err != nil {
		t.Fatalf("read archival log: %v", err)
	}
	if !strings.Contains(string(logData), "brain/projects/old") {
		t.Errorf("archival log missing entry: %q", string(logData))
	}
}

func TestPlanMoveDeleteRecordsInboundWarning(t *testing.T) {
	cfg, _ := movePlanFixture(t)
	plan, err := PlanMove(cfg, SphereWork, "brain/projects/old", "/dev/null")
	if err != nil {
		t.Fatalf("PlanMove delete: %v", err)
	}
	if len(plan.Notes) == 0 {
		t.Fatal("expected inbound link warnings on delete")
	}
	hasAliceWarning := false
	for _, note := range plan.Notes {
		if strings.Contains(note, "alice.md") {
			hasAliceWarning = true
		}
	}
	if !hasAliceWarning {
		t.Errorf("expected warning mentioning alice.md, got %v", plan.Notes)
	}
}

func TestPlanMoveDeleteWithoutInbound(t *testing.T) {
	cfg, root := movePlanFixture(t)
	// Drop a fresh dead directory with no inbound links.
	deadDir := filepath.Join(root, "work", "brain", "projects", "stale.bak")
	writeFile(t, filepath.Join(deadDir, "remnant.md"), "# stale\n")
	plan, err := PlanMove(cfg, SphereWork, "brain/projects/stale.bak", "/dev/null")
	if err != nil {
		t.Fatalf("PlanMove delete: %v", err)
	}
	if len(plan.Notes) != 0 {
		t.Errorf("unexpected warnings on clean delete: %v", plan.Notes)
	}
	if err := ApplyMove(cfg, plan, plan.Digest); err != nil {
		t.Fatalf("ApplyMove delete: %v", err)
	}
	if _, err := os.Stat(deadDir); !os.IsNotExist(err) {
		t.Errorf("stale dir still present: err=%v", err)
	}
}

// inodeOf returns the syscall inode for the given file or 0 on platforms
// where the cast does not apply.
func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return stat.Ino
}

