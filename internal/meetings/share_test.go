package meetings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveShareTargetPrefersFolderOverLooseFile(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "2026-04-29-standup")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, "MEETING_NOTES.md"), []byte("# Standup"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "2026-04-29-standup.md"), []byte("# Loose"), 0o644); err != nil {
		t.Fatalf("write loose: %v", err)
	}
	target, err := ResolveShareTarget(root, "2026-04-29-standup")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target.Kind != ShareTargetFolder || target.AbsolutePath != folder {
		t.Fatalf("target = %#v", target)
	}
	if !strings.HasSuffix(target.StatePath, ".share.json") {
		t.Fatalf("state path = %q", target.StatePath)
	}
}

func TestResolveShareTargetFallsBackToLooseFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "2026-05-01-1on1.md"), []byte("# Sync"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	target, err := ResolveShareTarget(root, "2026-05-01-1on1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if target.Kind != ShareTargetFile {
		t.Fatalf("kind = %q", target.Kind)
	}
}

func TestResolveShareTargetMissingMeeting(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveShareTarget(root, "absent"); err == nil {
		t.Fatal("expected error for missing meeting")
	}
}

func TestWriteAndLoadShareStateRoundTrip(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "m")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := ShareTarget{Slug: "m", Kind: ShareTargetFolder, AbsolutePath: folder, StatePath: filepath.Join(folder, shareStateFilename)}
	if err := WriteShareState(target, ShareState{URL: "https://cloud.example/s/AAA", Permissions: "edit"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	state, ok, err := LoadShareState(target)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if state.Slug != "m" || state.Kind != ShareTargetFolder || state.URL != "https://cloud.example/s/AAA" {
		t.Fatalf("state = %#v", state)
	}
	if err := RemoveShareState(target); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok, _ := LoadShareState(target); ok {
		t.Fatal("state must be gone after revoke")
	}
}

func TestShareLinkPrefersStateURLThenTemplateThenFallback(t *testing.T) {
	target := ShareTarget{VaultRelativePath: "MEETINGS/sync/MEETING_NOTES.md"}
	share := ShareConfig{
		URLTemplate:      "https://cloud/s/{vault_relative_path}",
		NoteLinkFallback: "vault://{vault_relative_path}",
	}
	url, live := ShareLink(target, ShareState{URL: "https://cloud/s/AAA"}, true, share)
	if !live || url != "https://cloud/s/AAA" {
		t.Fatalf("recorded URL must win: live=%v url=%q", live, url)
	}
	url, live = ShareLink(target, ShareState{}, false, share)
	if !live || url != "https://cloud/s/MEETINGS/sync/MEETING_NOTES.md" {
		t.Fatalf("template url=%q live=%v", url, live)
	}
	share.URLTemplate = ""
	url, live = ShareLink(target, ShareState{}, false, share)
	if live || url != "vault://MEETINGS/sync/MEETING_NOTES.md" {
		t.Fatalf("fallback url=%q live=%v", url, live)
	}
	url, live = ShareLink(target, ShareState{}, false, ShareConfig{})
	if live || url != "MEETINGS/sync/MEETING_NOTES.md" {
		t.Fatalf("relative fallback url=%q live=%v", url, live)
	}
}
