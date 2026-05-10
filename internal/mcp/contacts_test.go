package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/sloppy-org/sloptools/internal/canvas"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (p *fakeMailProvider) Trash(_ context.Context, ids []string) (int, error) {
	return p.record("trash", ids), nil
}

func (p *fakeMailProvider) TrashResolved(_ context.Context, ids []string) ([]email.ActionResolution, error) {
	p.record("trash", ids)
	return p.resolutions(ids), nil
}

func (p *fakeMailProvider) Delete(_ context.Context, ids []string) (int, error) {
	return p.record("delete", ids), nil
}

func (p *fakeMailProvider) Defer(_ context.Context, messageID string, untilAt time.Time) (email.MessageActionResult, error) {
	p.record("defer", []string{messageID})
	p.lastUntil = untilAt
	return email.MessageActionResult{Provider: p.ProviderName(), Action: "defer", MessageID: messageID, Status: "ok", EffectiveProviderMode: "native", DeferredUntilAt: untilAt.UTC().Format(time.RFC3339)}, nil
}

func (p *fakeMailProvider) SupportsNativeDefer() bool {
	return p.supportsDefer
}

func (p *fakeMailProvider) ProviderName() string {
	return "fake"
}

func (p *fakeMailProvider) Close() error {
	return nil
}

func (p *fakeMailProvider) MoveToFolder(_ context.Context, ids []string, folder string) (int, error) {
	p.record("move_to_folder", ids)
	p.lastFolder = folder
	return len(ids), nil
}

func (p *fakeMailProvider) MoveToFolderResolved(_ context.Context, ids []string, folder string) ([]email.ActionResolution, error) {
	p.record("move_to_folder", ids)
	p.lastFolder = folder
	return p.resolutions(ids), nil
}

func (p *fakeMailProvider) ApplyNamedLabel(_ context.Context, ids []string, label string, _ bool) (int, error) {
	p.record("apply_label", ids)
	p.lastLabel = label
	return len(ids), nil
}

func (p *fakeMailProvider) ServerFilterCapabilities() email.ServerFilterCapabilities {
	return email.ServerFilterCapabilities{SupportsList: true, SupportsUpsert: true, SupportsDelete: true}
}

func (p *fakeMailProvider) ListServerFilters(context.Context) ([]email.ServerFilter, error) {
	return append([]email.ServerFilter(nil), p.filters...), nil
}

func (p *fakeMailProvider) UpsertServerFilter(_ context.Context, filter email.ServerFilter) (email.ServerFilter, error) {
	if filter.ID == "" {
		filter.ID = "generated"
	}
	p.filters = []email.ServerFilter{filter}
	return filter, nil
}

func (p *fakeMailProvider) DeleteServerFilter(context.Context, string) error {
	p.filters = nil
	return nil
}

func (p *fakeMailProvider) record(action string, ids []string) int {
	p.lastAction = action
	p.lastIDs = append([]string(nil), ids...)
	return len(ids)
}

func (p *fakeMailProvider) resolutions(ids []string) []email.ActionResolution {
	out := make([]email.ActionResolution, 0, len(ids))
	for _, id := range ids {
		resolved := id
		if p.resolvedIDs != nil {
			if mapped := strings.TrimSpace(p.resolvedIDs[id]); mapped != "" {
				resolved = mapped
			}
		}
		out = append(out, email.ActionResolution{OriginalMessageID: id, ResolvedMessageID: resolved})
	}
	return out
}

func newMailToolsFixture(t *testing.T) (*Server, store.ExternalAccount, *fakeMailProvider) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	now := time.Date(2026, time.March, 16, 15, 0, 0, 0, time.UTC)
	provider := &fakeMailProvider{labels: []providerdata.Label{{ID: "inbox", Name: "Inbox"}}, pageIDs: []string{"m1"}, nextPage: "next-2", messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Subject", Date: now}}, attachment: &providerdata.AttachmentData{ID: "att-1", Filename: "plan.pdf", MimeType: "application/pdf", Size: 8, Content: []byte("pdfbytes")}, filters: []email.ServerFilter{{ID: "f1", Name: "Known sender", Enabled: true, Action: email.ServerFilterAction{Archive: true}}}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	return s, account, provider
}

func TestMailToolsActAndFilter(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	acted, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "mail_action", "account_id": account.ID, "mail_action": "archive_label", "message_ids": []interface{}{"m1"}, "label": "project-x"})
	if err != nil {
		t.Fatalf("mail_action failed: %v", err)
	}
	if succeeded, _ := acted["succeeded"].(int); succeeded != 1 {
		t.Fatalf("succeeded = %#v", acted["succeeded"])
	}
	if provider.lastAction != "move_to_folder" {
		t.Fatalf("lastAction = %q", provider.lastAction)
	}
	affected := requireSingleAffectedRef(t, acted)
	if affected.Domain != "mail" || affected.Kind != "message" || affected.ID != "m1" || affected.AccountID != account.ID {
		t.Fatalf("affected = %#v", affected)
	}
	filters, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "server_filter_list", "account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_server_filter_list failed: %v", err)
	}
	gotFilters, _ := filters["filters"].([]email.ServerFilter)
	if len(gotFilters) != 1 || gotFilters[0].ID != "f1" {
		t.Fatalf("filters = %+v", gotFilters)
	}
	upserted, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "server_filter_upsert", "account_id": account.ID, "filter": map[string]interface{}{"name": "Archive updates", "enabled": true, "action": map[string]interface{}{"archive": true}}})
	if err != nil {
		t.Fatalf("mail_server_filter_upsert failed: %v", err)
	}
	gotFilter, _ := upserted["filter"].(email.ServerFilter)
	if gotFilter.ID == "" || gotFilter.Name != "Archive updates" {
		t.Fatalf("filter = %+v", gotFilter)
	}
	deleted, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "server_filter_delete", "account_id": account.ID, "filter_id": "generated"})
	if err != nil {
		t.Fatalf("mail_server_filter_delete failed: %v", err)
	}
	if ok, _ := deleted["deleted"].(bool); !ok {
		t.Fatalf("deleted = %#v", deleted["deleted"])
	}
}
func TestMailActionResolvesTargetsFromQuery(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	now := time.Date(2026, time.March, 19, 9, 0, 0, 0, time.UTC)
	provider := &fakeMailProvider{listIDs: []string{"m1", "m2"}, messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Weekly digest", Sender: "newsletter@example.com", Date: now}, "m2": {ID: "m2", Subject: "Second digest", Sender: "newsletter@example.com", Date: now.Add(-time.Hour)}}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	acted, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "mail_action", "account_id": account.ID, "mail_action": "archive", "query": "from:newsletter@example.com newer_than:7d", "limit": 2})
	if err != nil {
		t.Fatalf("mail_action failed: %v", err)
	}
	if provider.lastAction != "archive" {
		t.Fatalf("lastAction = %q", provider.lastAction)
	}
	if got := provider.lastOpts.Text; got != "from:newsletter@example.com newer_than:7d" {
		t.Fatalf("lastOpts.Text = %q", got)
	}
	if got := provider.lastOpts.MaxResults; got != 2 {
		t.Fatalf("lastOpts.MaxResults = %d", got)
	}
	if succeeded, _ := acted["succeeded"].(int); succeeded != 2 {
		t.Fatalf("succeeded = %#v", acted["succeeded"])
	}
	if gotIDs, _ := acted["message_ids"].([]string); len(gotIDs) != 2 || gotIDs[0] != "m1" || gotIDs[1] != "m2" {
		t.Fatalf("message_ids = %#v", acted["message_ids"])
	}
	logs, err := st.ListMailActionLogs(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailActionLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs len = %d", len(logs))
	}
	request := map[string]any{}
	if err := json.Unmarshal([]byte(logs[0].RequestJSON), &request); err != nil {
		t.Fatalf("Unmarshal(request_json): %v", err)
	}
	if got := strings.TrimSpace(strFromAny(request["query"])); got != "from:newsletter@example.com newer_than:7d" {
		t.Fatalf("request query = %q", got)
	}
}
func TestMailActionDeferResolvesTargetsFromQuery(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	now := time.Date(2026, time.March, 19, 9, 0, 0, 0, time.UTC)
	untilAt := time.Date(2030, time.March, 20, 15, 4, 5, 0, time.UTC)
	provider := &fakeMailProvider{listIDs: []string{"m1", "m2"}, supportsDefer: true, messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Weekly digest", Sender: "newsletter@example.com", Date: now}, "m2": {ID: "m2", Subject: "Second digest", Sender: "newsletter@example.com", Date: now.Add(-time.Hour)}}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	acted, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "mail_action", "account_id": account.ID, "mail_action": "defer", "query": "from:newsletter@example.com newer_than:7d", "limit": 2, "until": untilAt.Format(time.RFC3339)})
	if err != nil {
		t.Fatalf("mail_action failed: %v", err)
	}
	if provider.lastAction != "defer" {
		t.Fatalf("lastAction = %q", provider.lastAction)
	}
	if len(provider.lastIDs) != 1 || provider.lastIDs[0] != "m2" {
		t.Fatalf("lastIDs = %#v", provider.lastIDs)
	}
	if !provider.lastUntil.Equal(untilAt) {
		t.Fatalf("lastUntil = %s, want %s", provider.lastUntil.Format(time.RFC3339), untilAt.Format(time.RFC3339))
	}
	if got := provider.lastOpts.Text; got != "from:newsletter@example.com newer_than:7d" {
		t.Fatalf("lastOpts.Text = %q", got)
	}
	if got := provider.lastOpts.MaxResults; got != 2 {
		t.Fatalf("lastOpts.MaxResults = %d", got)
	}
	if succeeded, _ := acted["succeeded"].(int); succeeded != 2 {
		t.Fatalf("succeeded = %#v", acted["succeeded"])
	}
	if got := strings.TrimSpace(strFromAny(acted["until"])); got != untilAt.Format(time.RFC3339) {
		t.Fatalf("until = %q", got)
	}
	logs, err := st.ListMailActionLogs(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailActionLogs: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs len = %d", len(logs))
	}
	request := map[string]any{}
	if err := json.Unmarshal([]byte(logs[0].RequestJSON), &request); err != nil {
		t.Fatalf("Unmarshal(request_json): %v", err)
	}
	if got := strings.TrimSpace(strFromAny(request["until"])); got != untilAt.Format(time.RFC3339) {
		t.Fatalf("request until = %q", got)
	}
}
func TestMailActionRejectsMissingIDsAndQuery(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return &fakeMailProvider{}, nil
	}
	_, err = s.callTool("sloppy_mail", map[string]interface{}{"action": "mail_action", "account_id": account.ID, "mail_action": "archive"})
	if err == nil {
		t.Fatal("mail_action error = nil, want missing target error")
	}
	if got := err.Error(); got != "message_ids or query are required" {
		t.Fatalf("error = %q", got)
	}
}
func TestMailActionDeferRequiresUntil(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return &fakeMailProvider{supportsDefer: true}, nil
	}
	_, err = s.callTool("sloppy_mail", map[string]interface{}{"action": "mail_action", "account_id": account.ID, "mail_action": "defer", "message_ids": []interface{}{"m1"}})
	if err == nil {
		t.Fatal("mail_action error = nil, want missing until error")
	}
	if got := err.Error(); got != "until is required" {
		t.Fatalf("error = %q", got)
	}
}
func TestMailActionDeferRejectsUnsupportedProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderIMAP, "Work IMAP", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return &fakeMailProvider{}, nil
	}
	_, err = s.callTool("sloppy_mail", map[string]interface{}{"action": "mail_action", "account_id": account.ID, "mail_action": "defer", "message_ids": []interface{}{"m1"}, "until": "2030-03-20T15:04:05Z"})
	if err == nil {
		t.Fatal("mail_action error = nil, want unsupported defer error")
	}
	if got := err.Error(); got != "defer is not supported for provider imap" {
		t.Fatalf("error = %q", got)
	}
	logs, err := st.ListMailActionLogs(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailActionLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs len = %d", len(logs))
	}
	if logs[0].Status != store.MailActionLogFailed {
		t.Fatalf("log status = %q", logs[0].Status)
	}
	if logs[0].ErrorText != "defer is not supported for provider imap" {
		t.Fatalf("log error = %q", logs[0].ErrorText)
	}
}
func TestMailActionLogsAndReconcilesExchangeBindings(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	item, err := st.CreateItem("Follow up", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	title := "Mail"
	artifact, err := st.CreateArtifact(store.ArtifactKindEmail, nil, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}
	containerRef := "Posteingang"
	if _, err := st.UpsertExternalBinding(store.ExternalBinding{AccountID: account.ID, Provider: store.ExternalProviderExchangeEWS, ObjectType: "email", RemoteID: "m1", ItemID: &item.ID, ArtifactID: &artifact.ID, ContainerRef: &containerRef}); err != nil {
		t.Fatalf("UpsertExternalBinding: %v", err)
	}
	now := time.Date(2026, time.March, 17, 10, 0, 0, 0, time.UTC)
	provider := &fakeMailProvider{resolvedIDs: map[string]string{"m1": "m1-trash"}, messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Subject", Sender: "alice@example.com", Labels: []string{"Posteingang", "INBOX"}, Date: now}}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	acted, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "mail_action", "account_id": account.ID, "mail_action": "trash", "message_ids": []interface{}{"m1"}})
	if err != nil {
		t.Fatalf("mail_action failed: %v", err)
	}
	if succeeded, _ := acted["succeeded"].(int); succeeded != 1 {
		t.Fatalf("succeeded = %#v", acted["succeeded"])
	}
	if _, err := st.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, "email", "m1"); err == nil {
		t.Fatal("old binding still exists")
	}
	binding, err := st.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, "email", "m1-trash")
	if err != nil {
		t.Fatalf("GetBindingByRemote(new): %v", err)
	}
	if binding.ContainerRef == nil || *binding.ContainerRef != "Gelöschte Elemente" {
		t.Fatalf("binding container_ref = %v", binding.ContainerRef)
	}
	updatedItem, err := st.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if updatedItem.State != store.ItemStateDone {
		t.Fatalf("item state = %q", updatedItem.State)
	}
	logs, err := st.ListMailActionLogs(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailActionLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs len = %d", len(logs))
	}
	if logs[0].ResolvedMessageID != "m1-trash" {
		t.Fatalf("resolved id = %q", logs[0].ResolvedMessageID)
	}
}
func TestCanvasImportHandoffFileText(t *testing.T) {
	content := []byte("hello from handoff")
	sum := sha256.Sum256(content)
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var structured map[string]interface{}
		switch name {
		case "handoff.peek":
			structured = map[string]interface{}{"handoff_id": "h1", "kind": "file"}
		case "handoff.consume":
			structured = map[string]interface{}{"spec_version": "handoff.v1", "handoff_id": "h1", "kind": "file", "meta": map[string]interface{}{"filename": "note.txt", "mime_type": "text/plain", "size_bytes": len(content), "sha256": stringHex(sum[:])}, "payload": map[string]interface{}{"content_base64": base64.StdEncoding.EncodeToString(content)}}
		default:
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "result": map[string]interface{}{"isError": false, "structuredContent": structured}})
	}))
	defer producer.Close()
	projectDir := t.TempDir()
	s := NewServer(projectDir)
	s.SetAdapter(canvas.NewAdapter(projectDir, nil))
	got, err := s.callTool("sloppy_canvas", map[string]interface{}{"action": "import_handoff", "session_id": "s1", "handoff_id": "h1", "producer_mcp_url": producer.URL, "title": "Imported File"})
	if err != nil {
		t.Fatalf("canvas_import_handoff failed: %v", err)
	}
	if got["kind"] != "file" {
		t.Fatalf("expected kind=file, got %#v", got["kind"])
	}
	if got["artifact_id"] == nil {
		t.Fatalf("missing artifact_id: %#v", got)
	}
	matches, err := filepath.Glob(filepath.Join(projectDir, ".sloptools", "artifacts", "imports", "h1-*.txt"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one imported file, found %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("imported content mismatch")
	}
}
func stringHex(b []byte) string {
	const hextable = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hextable[v>>4]
		out[i*2+1] = hextable[v&0x0f]
	}
	return string(out)
}
