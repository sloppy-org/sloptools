package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/meetings"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
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

func writeMCPBrainConfig(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "vaults.toml")
	body := `[[vault]]
sphere = "work"
root = "` + filepath.ToSlash(filepath.Join(root, "work")) + `"
brain = "brain"

[[vault]]
sphere = "private"
root = "` + filepath.ToSlash(filepath.Join(root, "private")) + `"
brain = "brain"
`
	writeMCPBrainFile(t, path, body)
	initMCPBrainGit(t, filepath.Join(root, "work", "brain"), filepath.Join(root, "work-brain.git"))
	initMCPBrainGit(t, filepath.Join(root, "private", "brain"), filepath.Join(root, "private-brain.git"))
	return path
}

func writeMCPBrainFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	commitMCPBrainFileIfTracked(t, path)
}

func initMCPBrainGit(t *testing.T, workTree, remote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	if err := os.MkdirAll(workTree, 0o755); err != nil {
		t.Fatalf("mkdir git worktree: %v", err)
	}
	runMCPGit(t, "", "init", "--bare", remote)
	runMCPGit(t, "", "init", "-q", "-b", "main", workTree)
	runMCPGit(t, workTree, "config", "user.email", "test@example.invalid")
	runMCPGit(t, workTree, "config", "user.name", "sloptools test")
	runMCPGit(t, workTree, "commit", "-q", "--allow-empty", "-m", "init")
	runMCPGit(t, workTree, "remote", "add", "origin", remote)
	runMCPGit(t, workTree, "push", "-q", "-u", "origin", "main")
}

func commitMCPBrainFileIfTracked(t *testing.T, path string) {
	t.Helper()
	dir := filepath.Dir(path)
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return
	}
	runMCPGit(t, root, "add", path)
	status := outputMCPGit(t, root, "diff", "--cached", "--name-only")
	if strings.TrimSpace(status) == "" {
		return
	}
	runMCPGit(t, root, "commit", "-q", "-m", "test fixture")
	runMCPGit(t, root, "push", "-q")
}

func runMCPGit(t *testing.T, workTree string, args ...string) {
	t.Helper()
	cmdArgs := args
	if workTree != "" {
		cmdArgs = append([]string{"-C", workTree}, args...)
	}
	cmd := exec.Command("git", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", cmdArgs, err, strings.TrimSpace(string(out)))
	}
}

func outputMCPGit(t *testing.T, workTree string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", workTree}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func TestInboxSourceListIncludesGoogleTasksInboxAndBareFileSource(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "private", "INBOX", "receipt.pdf"), "pdf")
	if err := os.MkdirAll(filepath.Join(tmp, "private", "INBOX", "special"), 0o755); err != nil {
		t.Fatalf("mkdir special: %v", err)
	}
	writeMCPBrainFile(t, filepath.Join(tmp, "private", "INBOX", "special", "ignored.txt"), "ignored")
	s, st, _ := newDomainServerForTest(t)
	s.brainConfigPath = configPath
	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Google", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "google_tasks", taskLists: []providerdata.TaskList{{ID: "inbox", Name: "INBOX", Primary: true}}, tasksByList: map[string][]providerdata.TaskItem{"inbox": {{ID: "milk", ListID: "inbox", Title: "Milch"}, {ID: "done", ListID: "inbox", Title: "Alt", Completed: true}}}}
	s.newTasksProvider = func(_ context.Context, got store.ExternalAccount) (tasks.Provider, error) {
		if got.ID != account.ID {
			t.Fatalf("account = %d, want %d", got.ID, account.ID)
		}
		return provider, nil
	}
	got, err := s.callTool("sloppy_inbox", map[string]interface{}{"action": "source_list", "sphere": "private"})
	if err != nil {
		t.Fatalf("inbox.source_list: %v", err)
	}
	sources := got["sources"].([]map[string]interface{})
	if got["count"] != 2 || sources[0]["pending_count"] != 1 || sources[1]["pending_count"] != 1 {
		t.Fatalf("sources = %#v", got)
	}
	if sources[1]["id"] != "file:private:INBOX" || sources[1]["mode"] != "active" {
		t.Fatalf("file source = %#v", sources[1])
	}
}

func TestInboxPlansAndAcknowledgesTaskAndFile(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "private", "INBOX", "receipt.pdf"), "pdf")
	s, st, _ := newDomainServerForTest(t)
	s.brainConfigPath = configPath
	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Google", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{name: "google_tasks", hasCompleter: true, taskLists: []providerdata.TaskList{{ID: "inbox", Name: "INBOX", Primary: true}}, tasksByList: map[string][]providerdata.TaskItem{"inbox": {{ID: "milk", ListID: "inbox", Title: "Milch"}}}, getTaskByID: map[string]providerdata.TaskItem{"milk": {ID: "milk", ListID: "inbox", Title: "Milch"}}}
	s.newTasksProvider = func(_ context.Context, got store.ExternalAccount) (tasks.Provider, error) {
		if got.ID != account.ID {
			t.Fatalf("account = %d, want %d", got.ID, account.ID)
		}
		return provider, nil
	}
	planned, err := s.callTool("sloppy_inbox", map[string]interface{}{"action": "item_plan", "source_id": "google_tasks:private:1:inbox", "id": "milk"})
	if err != nil {
		t.Fatalf("task inbox.item_plan: %v", err)
	}
	plan := planned["plan"].(map[string]interface{})
	if plan["kind"] != "shopping" || plan["language"] != "de" {
		t.Fatalf("task plan = %#v", plan)
	}
	if _, err := s.callTool("sloppy_inbox", map[string]interface{}{"action": "item_ack", "source_id": "google_tasks:private:1:inbox", "id": "milk"}); err == nil {
		t.Fatal("task ack without target_ref should fail")
	}
	if _, err := s.callTool("sloppy_inbox", map[string]interface{}{"action": "item_ack", "source_id": "google_tasks:private:1:inbox", "id": "milk", "target_ref": "brain:private:shopping"}); err != nil {
		t.Fatalf("task inbox.item_ack: %v", err)
	}
	if provider.completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1", provider.completeCalls)
	}
	filePlan, err := s.callTool("sloppy_inbox", map[string]interface{}{"action": "item_plan", "source_id": "file:private:INBOX", "id": "receipt.pdf"})
	if err != nil {
		t.Fatalf("file inbox.item_plan: %v", err)
	}
	if filePlan["plan"].(map[string]interface{})["kind"] != "scan_or_document" {
		t.Fatalf("file plan = %#v", filePlan)
	}
	acked, err := s.callTool("sloppy_inbox", map[string]interface{}{"action": "item_ack", "source_id": "file:private:INBOX", "id": "receipt.pdf", "target_ref": "file:private:Documents/receipt.pdf", "target_path": "Documents/receipt.pdf"})
	if err != nil {
		t.Fatalf("file inbox.item_ack: %v", err)
	}
	if acked["target_path"] != "Documents/receipt.pdf" {
		t.Fatalf("file ack = %#v", acked)
	}
	if _, err := os.Stat(filepath.Join(tmp, "private", "Documents", "receipt.pdf")); err != nil {
		t.Fatalf("moved file missing: %v", err)
	}
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

	created, err := server.callTool("sloppy_meeting", map[string]interface{}{"action": "share_create", 
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
	if _, err := server.callTool("sloppy_meeting", map[string]interface{}{"action": "share_create", 
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

	if _, err := server.callTool("sloppy_meeting", map[string]interface{}{"action": "share_create", 
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	}); err != nil {
		t.Fatalf("share.create: %v", err)
	}

	out, err := server.callTool("sloppy_meeting", map[string]interface{}{"action": "share_revoke", 
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

	if _, err := server.callTool("sloppy_meeting", map[string]interface{}{"action": "share_create", 
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	}); err != nil {
		t.Fatalf("share.create: %v", err)
	}

	if _, err := server.callTool("sloppy_meeting", map[string]interface{}{"action": "share_revoke", 
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
