package meetings

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

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
