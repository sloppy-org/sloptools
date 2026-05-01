package meetings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverPicksUpMeetingNotesAndLooseSlugs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "2026-04-29-board", "MEETING_NOTES.md"), "# board\n")
	mustWrite(t, filepath.Join(root, "2026-04-30-1on1.md"), "# 1on1\n")
	mustWrite(t, filepath.Join(root, ".obsidian", "should-skip.md"), "# obsidian\n")
	mustWrite(t, filepath.Join(root, "2026-04-29-stale.failed.md"), "# stale\n")
	mustWrite(t, filepath.Join(root, "notes.txt"), "ignored\n")

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	wantSet := map[string]bool{
		filepath.Join(root, "2026-04-29-board", "MEETING_NOTES.md"): true,
		filepath.Join(root, "2026-04-30-1on1.md"):                   true,
	}
	if len(got.Paths) != len(wantSet) {
		t.Fatalf("got %d paths, want %d: %#v", len(got.Paths), len(wantSet), got.Paths)
	}
	for _, path := range got.Paths {
		if !wantSet[path] {
			t.Fatalf("unexpected path discovered: %s", path)
		}
	}
}

func TestDiscoverMissingRootReturnsEmptyWithoutError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	got, err := Discover(missing)
	if err != nil {
		t.Fatalf("Discover missing root: %v", err)
	}
	if len(got.Paths) != 0 {
		t.Fatalf("expected zero paths, got %v", got.Paths)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
