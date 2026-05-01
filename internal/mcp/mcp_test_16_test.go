package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/meetings"
)

func writeMeetingsSourcesWithNextcloud(t *testing.T, root, meetingsRoot, baseURL, localSyncDir string) string {
	t.Helper()
	path := filepath.Join(root, "sources.toml")
	var b strings.Builder
	b.WriteString("[meetings.work]\n")
	b.WriteString("meetings_root = \"" + filepath.ToSlash(meetingsRoot) + "\"\n")
	b.WriteString("owner = \"Christopher Albert\"\n")
	b.WriteString("[meetings.work.share]\n")
	b.WriteString("permissions = \"edit\"\n")
	b.WriteString("[meetings.work.nextcloud]\n")
	b.WriteString("base_url = \"" + baseURL + "\"\n")
	b.WriteString("user = \"alice\"\n")
	b.WriteString("app_password = \"app-pass\"\n")
	b.WriteString("local_sync_dir = \"" + filepath.ToSlash(localSyncDir) + "\"\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write sources: %v", err)
	}
	return path
}

type fakeNextcloudShareClient struct {
	cfg         meetings.NextcloudConfig
	createCalls int
	deleteCalls int
	createOpts  meetings.NextcloudShareCreateOptions
	deleteIDs   []string
	createErr   error
	deleteErr   error
	record      meetings.NextcloudShareRecord
}

func (f *fakeNextcloudShareClient) CreatePublicShare(_ context.Context, opts meetings.NextcloudShareCreateOptions) (meetings.NextcloudShareRecord, error) {
	f.createCalls++
	f.createOpts = opts
	if f.createErr != nil {
		return meetings.NextcloudShareRecord{}, f.createErr
	}
	return f.record, nil
}

func (f *fakeNextcloudShareClient) DeleteShare(_ context.Context, id string) error {
	f.deleteCalls++
	f.deleteIDs = append(f.deleteIDs, id)
	return f.deleteErr
}

func (f *fakeNextcloudShareClient) ResolveServerPath(absolutePath string) (string, error) {
	rel := strings.TrimPrefix(absolutePath, strings.TrimSuffix(f.cfg.LocalSyncDir, "/"))
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	return rel, nil
}

func TestMeetingShareCreateCallsOCSWhenNoURLProvided(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	sourcesPath := writeMeetingsSourcesWithNextcloud(t, tmp, meetingsRoot, "https://cloud.example", filepath.Join(tmp, "work"))

	server := NewServer(t.TempDir())
	fake := &fakeNextcloudShareClient{record: meetings.NextcloudShareRecord{ID: "42", URL: "https://cloud.example/s/live", Token: "live"}}
	server.newNextcloudShareClient = func(cfg meetings.NextcloudConfig) (meetings.NextcloudShareClient, error) {
		fake.cfg = cfg
		return fake, nil
	}

	created, err := server.callTool("meeting.share.create", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
		"permissions":    "edit",
		"expiry_days":    30,
	})
	if err != nil {
		t.Fatalf("share.create: %v", err)
	}
	if fake.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", fake.createCalls)
	}
	if fake.createOpts.Permissions != meetings.PermissionsBitmask("edit") {
		t.Fatalf("permissions bitmask = %d", fake.createOpts.Permissions)
	}
	if !strings.HasSuffix(fake.createOpts.ServerPath, "/MEETINGS/2026-04-29-standup") {
		t.Fatalf("server path = %q", fake.createOpts.ServerPath)
	}
	if fake.createOpts.Label != "meeting:2026-04-29-standup" {
		t.Fatalf("label = %q", fake.createOpts.Label)
	}
	if fake.createOpts.ExpireDate == "" {
		t.Fatalf("expected expire date when expiry_days > 0")
	}
	if created["url"].(string) != "https://cloud.example/s/live" {
		t.Fatalf("returned url = %#v", created["url"])
	}
	if created["share_id"].(string) != "42" {
		t.Fatalf("share_id = %#v", created["share_id"])
	}

	statePath := filepath.Join(meetingsRoot, "2026-04-29-standup", ".share.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state meetings.ShareState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if state.ID != "42" || state.URL != "https://cloud.example/s/live" || state.Token != "live" {
		t.Fatalf("state = %#v", state)
	}
}

func TestMeetingShareCreateRequiresNextcloudOrURL(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	sourcesPath := writeMeetingsSummarySources(t, tmp, meetingsRoot, nil, "")

	server := NewServer(t.TempDir())
	if _, err := server.callTool("meeting.share.create", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	}); err == nil {
		t.Fatal("expected error when neither nextcloud config nor url is supplied")
	}
}

func TestMeetingShareRevokeCallsOCSDeleteWhenIDRecorded(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	sourcesPath := writeMeetingsSourcesWithNextcloud(t, tmp, meetingsRoot, "https://cloud.example", filepath.Join(tmp, "work"))

	server := NewServer(t.TempDir())
	fake := &fakeNextcloudShareClient{record: meetings.NextcloudShareRecord{ID: "77", URL: "https://cloud.example/s/77", Token: "tok"}}
	server.newNextcloudShareClient = func(cfg meetings.NextcloudConfig) (meetings.NextcloudShareClient, error) {
		fake.cfg = cfg
		return fake, nil
	}

	if _, err := server.callTool("meeting.share.create", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	}); err != nil {
		t.Fatalf("share.create: %v", err)
	}

	out, err := server.callTool("meeting.share.revoke", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	})
	if err != nil {
		t.Fatalf("share.revoke: %v", err)
	}
	if fake.deleteCalls != 1 || len(fake.deleteIDs) != 1 || fake.deleteIDs[0] != "77" {
		t.Fatalf("delete calls = %d ids = %v", fake.deleteCalls, fake.deleteIDs)
	}
	if got, _ := out["share_id_purged"].(bool); !got {
		t.Fatalf("share_id_purged = %#v", out["share_id_purged"])
	}
	statePath := filepath.Join(meetingsRoot, "2026-04-29-standup", ".share.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state must be removed: err=%v", err)
	}
}

func TestMeetingShareRevokeReportsLiveDeleteFailure(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	sourcesPath := writeMeetingsSourcesWithNextcloud(t, tmp, meetingsRoot, "https://cloud.example", filepath.Join(tmp, "work"))

	server := NewServer(t.TempDir())
	fake := &fakeNextcloudShareClient{record: meetings.NextcloudShareRecord{ID: "77", URL: "https://cloud.example/s/77"}, deleteErr: errors.New("boom")}
	server.newNextcloudShareClient = func(cfg meetings.NextcloudConfig) (meetings.NextcloudShareClient, error) {
		fake.cfg = cfg
		return fake, nil
	}

	if _, err := server.callTool("meeting.share.create", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	}); err != nil {
		t.Fatalf("share.create: %v", err)
	}

	if _, err := server.callTool("meeting.share.revoke", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	}); err == nil {
		t.Fatal("expected error when OCS DELETE fails")
	}
	statePath := filepath.Join(meetingsRoot, "2026-04-29-standup", ".share.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state should be preserved when revoke fails: %v", err)
	}
}
