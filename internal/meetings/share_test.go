package meetings

import (
	"context"
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

func TestChooseSharePermissionsHonoursPriorityChain(t *testing.T) {
	if got := ChooseSharePermissions("", "edit", ""); got != "edit" {
		t.Fatalf("requested wins: %q", got)
	}
	if got := ChooseSharePermissions("comment", "", "read"); got != "comment" {
		t.Fatalf("existing should win over fallback: %q", got)
	}
	if got := ChooseSharePermissions("", "", "READ"); got != "read" {
		t.Fatalf("fallback lower-cased: %q", got)
	}
	if got := ChooseSharePermissions("", "", ""); got != "edit" {
		t.Fatalf("default = %q", got)
	}
}

type fakeShareClient struct {
	createCalls int
	deleteCalls int
	createOpts  NextcloudShareCreateOptions
	deleteIDs   []string
	createErr   error
	deleteErr   error
	record      NextcloudShareRecord
	syncDir     string
}

func (f *fakeShareClient) CreatePublicShare(_ context.Context, opts NextcloudShareCreateOptions) (NextcloudShareRecord, error) {
	f.createCalls++
	f.createOpts = opts
	if f.createErr != nil {
		return NextcloudShareRecord{}, f.createErr
	}
	return f.record, nil
}

func (f *fakeShareClient) DeleteShare(_ context.Context, id string) error {
	f.deleteCalls++
	f.deleteIDs = append(f.deleteIDs, id)
	return f.deleteErr
}

func (f *fakeShareClient) ResolveServerPath(absolutePath string) (string, error) {
	rel := strings.TrimPrefix(absolutePath, strings.TrimSuffix(f.syncDir, "/"))
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	return rel, nil
}

func TestCreateLiveShareCallsClientWithExpectedOptions(t *testing.T) {
	fake := &fakeShareClient{record: NextcloudShareRecord{ID: "1", URL: "https://cloud.example/s/1"}, syncDir: "/home/u/Nextcloud"}
	sphere := SphereConfig{Sphere: "work", Nextcloud: NextcloudConfig{BaseURL: "https://cloud.example", User: "u", AppPassword: "p", LocalSyncDir: "/home/u/Nextcloud"}}
	target := ShareTarget{Slug: "m", Kind: ShareTargetFolder, AbsolutePath: "/home/u/Nextcloud/MEETINGS/m"}
	state := ShareState{Permissions: "edit", ExpiryDays: 14, Password: false}
	rec, err := CreateLiveShare(target, sphere, state, "m", func(NextcloudConfig) (NextcloudShareClient, error) { return fake, nil })
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.ID != "1" {
		t.Fatalf("record = %#v", rec)
	}
	if fake.createOpts.ServerPath != "/MEETINGS/m" {
		t.Fatalf("server path = %q", fake.createOpts.ServerPath)
	}
	if fake.createOpts.Permissions != PermissionsBitmask("edit") {
		t.Fatalf("perm bitmask = %d", fake.createOpts.Permissions)
	}
	if fake.createOpts.ExpireDate == "" {
		t.Fatalf("expire date should be set when ExpiryDays > 0")
	}
	if fake.createOpts.Password != "" {
		t.Fatalf("password must be empty when state.Password is false")
	}
	if fake.createOpts.Label != "meeting:m" {
		t.Fatalf("label = %q", fake.createOpts.Label)
	}
}

func TestCreateLiveShareGeneratesPasswordWhenRequested(t *testing.T) {
	fake := &fakeShareClient{record: NextcloudShareRecord{ID: "1"}, syncDir: "/root"}
	sphere := SphereConfig{Sphere: "work", Nextcloud: NextcloudConfig{BaseURL: "https://cloud", User: "u", AppPassword: "p", LocalSyncDir: "/root"}}
	target := ShareTarget{Slug: "m", AbsolutePath: "/root/m.md"}
	if _, err := CreateLiveShare(target, sphere, ShareState{Permissions: "edit", Password: true}, "m", func(NextcloudConfig) (NextcloudShareClient, error) { return fake, nil }); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(fake.createOpts.Password) < 16 {
		t.Fatalf("password should be a non-trivial string: %q", fake.createOpts.Password)
	}
}

func TestCreateLiveShareErrorsWithoutNextcloudConfig(t *testing.T) {
	if _, err := CreateLiveShare(ShareTarget{Slug: "m"}, SphereConfig{Sphere: "work"}, ShareState{}, "m", nil); err == nil {
		t.Fatal("expected error when nextcloud is unconfigured")
	}
}

func TestRevokeLiveShareSkipsEmptyID(t *testing.T) {
	if err := RevokeLiveShare(SphereConfig{Sphere: "work"}, "", nil); err != nil {
		t.Fatalf("empty id should be no-op: %v", err)
	}
}

func TestRevokeLiveShareCallsClient(t *testing.T) {
	fake := &fakeShareClient{}
	sphere := SphereConfig{Sphere: "work", Nextcloud: NextcloudConfig{BaseURL: "https://cloud", User: "u", AppPassword: "p", LocalSyncDir: "/root"}}
	if err := RevokeLiveShare(sphere, "42", func(NextcloudConfig) (NextcloudShareClient, error) { return fake, nil }); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if fake.deleteCalls != 1 || fake.deleteIDs[0] != "42" {
		t.Fatalf("delete calls = %d ids = %v", fake.deleteCalls, fake.deleteIDs)
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
