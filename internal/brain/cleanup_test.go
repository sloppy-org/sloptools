package brain

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// cleanupVault produces a Config with both spheres rooted under t.TempDir().
// It mirrors the brain test config helper but uses unique roots so vault
// scanning sees a real on-disk tree.
func cleanupVault(t *testing.T) (*Config, Vault) {
	t.Helper()
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{
		{Sphere: SphereWork, Root: filepath.Join(root, "work"), Brain: "brain"},
		{Sphere: SpherePrivate, Root: filepath.Join(root, "private"), Brain: "brain"},
	})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	work, ok := cfg.Vault(SphereWork)
	if !ok {
		t.Fatal("missing work vault")
	}
	if err := os.MkdirAll(work.BrainRoot(), 0o755); err != nil {
		t.Fatalf("mkdir brain: %v", err)
	}
	private, _ := cfg.Vault(SpherePrivate)
	if err := os.MkdirAll(private.BrainRoot(), 0o755); err != nil {
		t.Fatalf("mkdir brain private: %v", err)
	}
	return cfg, work
}

func mkdirIn(t *testing.T, vault Vault, rel string) string {
	t.Helper()
	abs := filepath.Join(vault.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", abs, err)
	}
	return abs
}

func writeIn(t *testing.T, vault Vault, rel, content string) string {
	t.Helper()
	abs := filepath.Join(vault.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir parent %s: %v", abs, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", abs, err)
	}
	return abs
}

func candidatePaths(candidates []DeadDirCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.Path)
	}
	sort.Strings(out)
	return out
}

func findCandidate(candidates []DeadDirCandidate, path string) (DeadDirCandidate, bool) {
	for _, c := range candidates {
		if c.Path == path {
			return c, true
		}
	}
	return DeadDirCandidate{}, false
}

func TestCleanupDeadDirsScanDetectsSvnAtMultipleDepths(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "brain/projects/.svn")
	mkdirIn(t, work, "brain/projects/foo/.svn")
	mkdirIn(t, work, "brain/projects/foo/.svn/wc.db.tmp")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	want := []string{"brain/projects/.svn", "brain/projects/foo/.svn"}
	if diff := candidatePaths(got); !reflect.DeepEqual(diff, want) {
		t.Fatalf("svn paths = %v, want %v", diff, want)
	}
	for _, c := range got {
		if c.Reason != "svn" {
			t.Fatalf("reason = %q, want svn", c.Reason)
		}
		if c.Confidence != "high" {
			t.Fatalf("confidence = %q, want high", c.Confidence)
		}
	}
}

func TestCleanupDeadDirsScanDetectsPycache(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "brain/scripts/__pycache__")
	writeIn(t, work, "brain/scripts/__pycache__/foo.pyc", "binary")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	c, ok := findCandidate(got, "brain/scripts/__pycache__")
	if !ok {
		t.Fatalf("missing pycache candidate, got %v", got)
	}
	if c.Reason != "pycache" || c.Confidence != "high" {
		t.Fatalf("pycache candidate = %+v", c)
	}
}

func TestCleanupDeadDirsScanDetectsNodeModulesAtDepthAndSkipsTopLevel(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "brain/tools/node_modules")
	writeIn(t, work, "brain/tools/node_modules/package.json", "{}")
	mkdirIn(t, work, "node_modules")
	writeIn(t, work, "node_modules/package.json", "{}")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	c, ok := findCandidate(got, "brain/tools/node_modules")
	if !ok {
		t.Fatalf("missing node_modules candidate, got %v", got)
	}
	if c.Reason != "node-modules" {
		t.Fatalf("reason = %q, want node-modules", c.Reason)
	}
	if _, exists := findCandidate(got, "node_modules"); exists {
		t.Fatalf("top-level node_modules should not be flagged: %v", got)
	}
}

func TestCleanupDeadDirsScanFlagsOldWithLiveSibling(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "brain/code/3DGeoInt")
	writeIn(t, work, "brain/code/3DGeoInt/main.f90", "program p\nend\n")
	mkdirIn(t, work, "brain/code/3DGeoInt.old")
	writeIn(t, work, "brain/code/3DGeoInt.old/main.f90", "program old\nend\n")
	mkdirIn(t, work, "brain/code/orphan.old")
	writeIn(t, work, "brain/code/orphan.old/main.f90", "program orphan\nend\n")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	c, ok := findCandidate(got, "brain/code/3DGeoInt.old")
	if !ok {
		t.Fatalf("missing old-with-live-sibling candidate, got %v", got)
	}
	if c.Reason != "old-with-live-sibling" {
		t.Fatalf("reason = %q", c.Reason)
	}
	if _, exists := findCandidate(got, "brain/code/orphan.old"); exists {
		t.Fatalf("orphan.old without sibling must not be flagged: %v", got)
	}
}

func TestCleanupDeadDirsScanFlagsBakWithLiveSibling(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "brain/notes/draft")
	writeIn(t, work, "brain/notes/draft/index.md", "draft")
	mkdirIn(t, work, "brain/notes/draft.bak")
	writeIn(t, work, "brain/notes/draft.bak/index.md", "old")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	c, ok := findCandidate(got, "brain/notes/draft.bak")
	if !ok {
		t.Fatalf("missing bak-with-live-sibling candidate, got %v", got)
	}
	if c.Reason != "bak-with-live-sibling" {
		t.Fatalf("reason = %q", c.Reason)
	}
}

func TestCleanupDeadDirsScanFlagsEmptyAtDepthButNotTopLevel(t *testing.T) {
	cfg, work := cleanupVault(t)
	// Live siblings keep brain and brain/projects "non-empty" so the empty
	// walker reaches the abandoned subtree and emits its top-most empty dir.
	writeIn(t, work, "brain/index.md", "---\nkind: note\n---\n")
	writeIn(t, work, "brain/projects/active.md", "active project")
	mkdirIn(t, work, "brain/projects/abandoned/inner")
	mkdirIn(t, work, "lonely")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	c, ok := findCandidate(got, "brain/projects/abandoned")
	if !ok {
		t.Fatalf("missing empty candidate, got %v", got)
	}
	if c.Reason != "empty" {
		t.Fatalf("reason = %q", c.Reason)
	}
	if _, exists := findCandidate(got, "brain/projects/abandoned/inner"); exists {
		t.Fatalf("inner empty dir already covered by ancestor: %v", got)
	}
	if _, exists := findCandidate(got, "lonely"); exists {
		t.Fatalf("top-level empty dir must not be flagged: %v", got)
	}
}

func TestCleanupDeadDirsScanTreatsGitCheckoutAsNonEmpty(t *testing.T) {
	cfg, work := cleanupVault(t)
	// Live siblings so the walker reaches code/.
	writeIn(t, work, "brain/index.md", "---\nkind: note\n---\n")
	writeIn(t, work, "code/active/main.go", "package main")
	// Sandbox checkout: only metadata, no tracked working files yet. The
	// pre-fix empty-detector flagged this as `empty` and apply rmdir then
	// failed because .git/HEAD was actually present.
	mkdirIn(t, work, "code/checkout/.git/refs/heads")
	writeIn(t, work, "code/checkout/.git/HEAD", "ref: refs/heads/main\n")
	mkdirIn(t, work, "code/hg-checkout/.hg/store")
	writeIn(t, work, "code/hg-checkout/.hg/hgrc", "[paths]\n")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, c := range got {
		if c.Path == "code/checkout" || c.Path == "code/hg-checkout" {
			t.Fatalf("VCS checkout flagged as %s: %+v", c.Reason, c)
		}
	}
}

func TestCleanupDeadDirsScanSkipsPersonalSubtree(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "personal/secrets/.svn")
	mkdirIn(t, work, "brain/keep/.svn")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if _, exists := findCandidate(got, "personal/secrets/.svn"); exists {
		t.Fatalf("personal subtree must not be scanned: %v", got)
	}
	if _, ok := findCandidate(got, "brain/keep/.svn"); !ok {
		t.Fatalf("expected brain/keep/.svn candidate, got %v", got)
	}
}

func TestCleanupDeadDirsScanDowngradesWhenInboundLinkExists(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "brain/archive/foo")
	writeIn(t, work, "brain/archive/foo/keep.md", "x")
	mkdirIn(t, work, "brain/archive/foo.old")
	writeIn(t, work, "brain/archive/foo.old/old.md", "x")
	writeIn(t, work, "brain/projects/refers.md", "---\nkind: note\n---\nSee [[archive/foo.old]] later.\n")

	got, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	c, ok := findCandidate(got, "brain/archive/foo.old")
	if !ok {
		t.Fatalf("missing candidate, got %v", got)
	}
	if c.Inbound != 1 {
		t.Fatalf("inbound = %d, want 1", c.Inbound)
	}
	if c.Confidence != "medium" {
		t.Fatalf("confidence = %q, want medium", c.Confidence)
	}
}

func TestCleanupDeadDirsScanIsDeterministic(t *testing.T) {
	cfg, work := cleanupVault(t)
	mkdirIn(t, work, "brain/a/.svn")
	mkdirIn(t, work, "brain/b/__pycache__")
	mkdirIn(t, work, "brain/c/empty/inner")
	mkdirIn(t, work, "brain/d/live")
	writeIn(t, work, "brain/d/live/main.go", "package main")
	mkdirIn(t, work, "brain/d/live.old")
	writeIn(t, work, "brain/d/live.old/main.go", "package old")

	first, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan 1: %v", err)
	}
	second, err := CleanupDeadDirsScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan 2: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("scans differ:\n%v\nvs\n%v", first, second)
	}
}
