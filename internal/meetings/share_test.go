package meetings

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestPermissionsBitmaskMapsIssue59Strings(t *testing.T) {
	got := PermissionsBitmask("edit")
	want := NextcloudPermRead | NextcloudPermUpdate | NextcloudPermCreate | NextcloudPermDelete | NextcloudPermShare
	if got != want {
		t.Fatalf("edit = %d, want %d", got, want)
	}
	if v := PermissionsBitmask("read"); v != NextcloudPermRead {
		t.Fatalf("read = %d", v)
	}
	if v := PermissionsBitmask("comment"); v != NextcloudPermRead|NextcloudPermShare {
		t.Fatalf("comment = %d", v)
	}
	if v := PermissionsBitmask("nonsense"); v != NextcloudPermRead {
		t.Fatalf("unknown should fall back to read; got %d", v)
	}
}

func TestNextcloudConfigConfiguredRequiresAllCredentials(t *testing.T) {
	cases := []struct {
		name string
		cfg  NextcloudConfig
		want bool
	}{
		{"all set", NextcloudConfig{BaseURL: "https://cloud.example", User: "u", AppPassword: "p"}, true},
		{"missing url", NextcloudConfig{User: "u", AppPassword: "p"}, false},
		{"missing user", NextcloudConfig{BaseURL: "https://cloud.example", AppPassword: "p"}, false},
		{"missing password", NextcloudConfig{BaseURL: "https://cloud.example", User: "u"}, false},
	}
	for _, tc := range cases {
		if got := tc.cfg.Configured(); got != tc.want {
			t.Fatalf("%s: Configured = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNewNextcloudShareClientRejectsMissingFields(t *testing.T) {
	if _, err := NewNextcloudShareClient(NextcloudConfig{}); err == nil {
		t.Fatal("expected error for empty config")
	}
	if _, err := NewNextcloudShareClient(NextcloudConfig{BaseURL: "https://cloud.example", User: "u"}); err == nil {
		t.Fatal("expected error for missing app_password")
	}
}

func TestHTTPNextcloudShareClientResolveServerPathTranslatesAbsolutePath(t *testing.T) {
	client, err := NewNextcloudShareClient(NextcloudConfig{
		BaseURL:      "https://cloud.example",
		User:         "alice",
		AppPassword:  "secret",
		LocalSyncDir: "/home/alice/Nextcloud",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	got, err := client.ResolveServerPath(filepath.Join("/home/alice/Nextcloud", "MEETINGS", "2026-04-29-standup"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "/MEETINGS/2026-04-29-standup" {
		t.Fatalf("server path = %q", got)
	}
	if _, err := client.ResolveServerPath("/var/other"); err == nil {
		t.Fatal("expected error for path outside local_sync_dir")
	}
}

func TestHTTPNextcloudShareClientResolveServerPathRequiresLocalSyncDir(t *testing.T) {
	client, err := NewNextcloudShareClient(NextcloudConfig{BaseURL: "https://cloud.example", User: "u", AppPassword: "p"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.ResolveServerPath("/anywhere"); err == nil {
		t.Fatal("expected error when local_sync_dir is empty")
	}
}

func TestHTTPNextcloudShareClientCreateAndDeleteHitsOCSEndpoints(t *testing.T) {
	var (
		gotCreatePath string
		gotCreateForm string
		gotDeletePath string
		gotAuthHeader string
		gotOCSReqHdr  string
		callsCreate   int
		callsDelete   int
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/ocs/v2.php/apps/files_sharing/api/v1/shares", func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotOCSReqHdr = r.Header.Get("OCS-APIRequest")
		switch r.Method {
		case http.MethodPost:
			callsCreate++
			gotCreatePath = r.URL.Path
			body, _ := io.ReadAll(r.Body)
			gotCreateForm = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ocs":{"meta":{"status":"ok","statuscode":200,"message":"OK"},"data":{"id":1234,"url":"https://cloud.example/s/abcXYZ","token":"abcXYZ"}}}`))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/ocs/v2.php/apps/files_sharing/api/v1/shares/", func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotOCSReqHdr = r.Header.Get("OCS-APIRequest")
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		callsDelete++
		gotDeletePath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ocs":{"meta":{"status":"ok","statuscode":200,"message":"OK"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewNextcloudShareClient(NextcloudConfig{
		BaseURL:      srv.URL,
		User:         "alice",
		AppPassword:  "secret",
		LocalSyncDir: "/tmp",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	rec, err := client.CreatePublicShare(context.Background(), NextcloudShareCreateOptions{
		ServerPath:  "/MEETINGS/2026-04-29-standup",
		Permissions: PermissionsBitmask("edit"),
		ExpireDate:  "2026-08-01",
		Label:       "meeting:2026-04-29-standup",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.ID != "1234" || rec.URL != "https://cloud.example/s/abcXYZ" || rec.Token != "abcXYZ" {
		t.Fatalf("record = %#v", rec)
	}
	if callsCreate != 1 {
		t.Fatalf("create calls = %d", callsCreate)
	}
	if gotCreatePath != "/ocs/v2.php/apps/files_sharing/api/v1/shares" {
		t.Fatalf("create path = %q", gotCreatePath)
	}
	if gotOCSReqHdr != "true" {
		t.Fatalf("OCS-APIRequest header = %q", gotOCSReqHdr)
	}
	if !strings.HasPrefix(gotAuthHeader, "Basic ") {
		t.Fatalf("auth header = %q", gotAuthHeader)
	}
	for _, want := range []string{"shareType=3", "permissions=31", "path=%2FMEETINGS%2F2026-04-29-standup", "expireDate=2026-08-01", "label=meeting%3A2026-04-29-standup"} {
		if !strings.Contains(gotCreateForm, want) {
			t.Fatalf("create form missing %q: %s", want, gotCreateForm)
		}
	}

	if err := client.DeleteShare(context.Background(), "1234"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if callsDelete != 1 {
		t.Fatalf("delete calls = %d", callsDelete)
	}
	if gotDeletePath != "/ocs/v2.php/apps/files_sharing/api/v1/shares/1234" {
		t.Fatalf("delete path = %q", gotDeletePath)
	}
}

func TestHTTPNextcloudShareClientCreateSurfacesOCSError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ocs":{"meta":{"status":"failure","statuscode":404,"message":"not found"},"data":{}}}`))
	}))
	defer srv.Close()

	client, err := NewNextcloudShareClient(NextcloudConfig{BaseURL: srv.URL, User: "u", AppPassword: "p", LocalSyncDir: "/tmp"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if _, err := client.CreatePublicShare(context.Background(), NextcloudShareCreateOptions{ServerPath: "/x", Permissions: NextcloudPermRead}); err == nil {
		t.Fatal("expected error for non-OK OCS meta")
	}
}

func TestHTTPNextcloudShareClientDeleteSurfacesHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client, err := NewNextcloudShareClient(NextcloudConfig{BaseURL: srv.URL, User: "u", AppPassword: "p", LocalSyncDir: "/tmp"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := client.DeleteShare(context.Background(), "1"); err == nil {
		t.Fatal("expected error for 500 response")
	}
}
