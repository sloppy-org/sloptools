package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeMailProvider struct {
	labels        []providerdata.Label
	listIDs       []string
	pageIDs       []string
	nextPage      string
	messages      map[string]*providerdata.EmailMessage
	attachment    *providerdata.AttachmentData
	filters       []email.ServerFilter
	resolvedIDs   map[string]string
	lastOpts      email.SearchOptions
	lastAction    string
	lastIDs       []string
	lastFolder    string
	lastLabel     string
	lastUntil     time.Time
	supportsDefer bool
}

func (p *fakeMailProvider) ListLabels(_ context.Context) ([]providerdata.Label, error) {
	return append([]providerdata.Label(nil), p.labels...), nil
}

func (p *fakeMailProvider) ListMessages(_ context.Context, opts email.SearchOptions) ([]string, error) {
	p.lastOpts = opts
	return append([]string(nil), p.listIDs...), nil
}

func (p *fakeMailProvider) ListMessagesPage(_ context.Context, opts email.SearchOptions, _ string) (email.MessagePage, error) {
	p.lastOpts = opts
	ids := p.pageIDs
	if len(ids) == 0 {
		ids = p.listIDs
	}
	return email.MessagePage{IDs: append([]string(nil), ids...), NextPageToken: p.nextPage}, nil
}

func (p *fakeMailProvider) GetMessage(_ context.Context, messageID, _ string) (*providerdata.EmailMessage, error) {
	return p.messages[messageID], nil
}

func (p *fakeMailProvider) GetMessages(_ context.Context, messageIDs []string, _ string) ([]*providerdata.EmailMessage, error) {
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		out = append(out, p.messages[id])
	}
	return out, nil
}

func (p *fakeMailProvider) GetAttachment(_ context.Context, _, _ string) (*providerdata.AttachmentData, error) {
	if p.attachment == nil {
		return nil, nil
	}
	copyValue := *p.attachment
	copyValue.Content = append([]byte(nil), p.attachment.Content...)
	return &copyValue, nil
}

func (p *fakeMailProvider) MarkRead(_ context.Context, ids []string) (int, error) {
	return p.record("mark_read", ids), nil
}
func (p *fakeMailProvider) MarkUnread(_ context.Context, ids []string) (int, error) {
	return p.record("mark_unread", ids), nil
}
func (p *fakeMailProvider) Archive(_ context.Context, ids []string) (int, error) {
	return p.record("archive", ids), nil
}
func (p *fakeMailProvider) ArchiveResolved(_ context.Context, ids []string) ([]email.ActionResolution, error) {
	p.record("archive", ids)
	return p.resolutions(ids), nil
}
func (p *fakeMailProvider) MoveToInbox(_ context.Context, ids []string) (int, error) {
	return p.record("move_to_inbox", ids), nil
}
func (p *fakeMailProvider) MoveToInboxResolved(_ context.Context, ids []string) ([]email.ActionResolution, error) {
	p.record("move_to_inbox", ids)
	return p.resolutions(ids), nil
}
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
	return email.MessageActionResult{
		Provider:              p.ProviderName(),
		Action:                "defer",
		MessageID:             messageID,
		Status:                "ok",
		EffectiveProviderMode: "native",
		DeferredUntilAt:       untilAt.UTC().Format(time.RFC3339),
	}, nil
}
func (p *fakeMailProvider) SupportsNativeDefer() bool { return p.supportsDefer }
func (p *fakeMailProvider) ProviderName() string      { return "fake" }
func (p *fakeMailProvider) Close() error              { return nil }
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
		out = append(out, email.ActionResolution{
			OriginalMessageID: id,
			ResolvedMessageID: resolved,
		})
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
	provider := &fakeMailProvider{
		labels:   []providerdata.Label{{ID: "inbox", Name: "Inbox"}},
		pageIDs:  []string{"m1"},
		nextPage: "next-2",
		messages: map[string]*providerdata.EmailMessage{
			"m1": {ID: "m1", Subject: "Subject", Date: now},
		},
		attachment: &providerdata.AttachmentData{
			ID:       "att-1",
			Filename: "plan.pdf",
			MimeType: "application/pdf",
			Size:     8,
			Content:  []byte("pdfbytes"),
		},
		filters: []email.ServerFilter{{
			ID:      "f1",
			Name:    "Known sender",
			Enabled: true,
			Action:  email.ServerFilterAction{Archive: true},
		}},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	return s, account, provider
}

func TestMailToolsListReadAndAttachment(t *testing.T) {
	s, account, _ := newMailToolsFixture(t)
	listed, err := s.callTool("mail_account_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("mail_account_list failed: %v", err)
	}
	accounts, _ := listed["accounts"].([]store.ExternalAccount)
	if len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("accounts = %+v", accounts)
	}

	messages, err := s.callTool("mail_message_list", map[string]interface{}{
		"account_id": account.ID,
		"page_token": "next-1",
	})
	if err != nil {
		t.Fatalf("mail_message_list failed: %v", err)
	}
	if got := messages["next_page_token"]; got != "next-2" {
		t.Fatalf("next_page_token = %#v", got)
	}

	message, err := s.callTool("mail_message_get", map[string]interface{}{
		"account_id": account.ID,
		"message_id": "m1",
	})
	if err != nil {
		t.Fatalf("mail_message_get failed: %v", err)
	}
	gotMessage, _ := message["message"].(*providerdata.EmailMessage)
	if gotMessage == nil || gotMessage.ID != "m1" {
		t.Fatalf("message = %#v", message["message"])
	}

	destDir := t.TempDir()
	attachment, err := s.callTool("mail_attachment_get", map[string]interface{}{
		"account_id":    account.ID,
		"message_id":    "m1",
		"attachment_id": "att-1",
		"dest_dir":      destDir,
	})
	if err != nil {
		t.Fatalf("mail_attachment_get failed: %v", err)
	}
	gotAttachment, _ := attachment["attachment"].(map[string]interface{})
	if gotAttachment["id"] != "att-1" {
		t.Fatalf("attachment id = %#v", gotAttachment["id"])
	}
	if _, hasB64 := gotAttachment["content_base64"]; hasB64 {
		t.Fatalf("attachment must not contain content_base64: %#v", gotAttachment)
	}
	pathAny, ok := gotAttachment["path"].(string)
	if !ok || pathAny == "" {
		t.Fatalf("attachment path missing: %#v", gotAttachment)
	}
	if !strings.HasPrefix(pathAny, destDir) {
		t.Fatalf("attachment path %q not under destDir %q", pathAny, destDir)
	}
	data, err := os.ReadFile(pathAny)
	if err != nil {
		t.Fatalf("read saved attachment: %v", err)
	}
	if string(data) != "pdfbytes" {
		t.Fatalf("saved attachment bytes = %q", data)
	}
	if gotAttachment["size_bytes"] != len([]byte("pdfbytes")) {
		t.Fatalf("size_bytes = %#v", gotAttachment["size_bytes"])
	}
	if filepath.Base(pathAny) == "" {
		t.Fatalf("empty basename for %q", pathAny)
	}
}

func TestMailToolsActAndFilter(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	acted, err := s.callTool("mail_action", map[string]interface{}{
		"account_id":  account.ID,
		"action":      "archive_label",
		"message_ids": []interface{}{"m1"},
		"label":       "project-x",
	})
	if err != nil {
		t.Fatalf("mail_action failed: %v", err)
	}
	if succeeded, _ := acted["succeeded"].(int); succeeded != 1 {
		t.Fatalf("succeeded = %#v", acted["succeeded"])
	}
	if provider.lastAction != "move_to_folder" {
		t.Fatalf("lastAction = %q", provider.lastAction)
	}

	filters, err := s.callTool("mail_server_filter_list", map[string]interface{}{
		"account_id": account.ID,
	})
	if err != nil {
		t.Fatalf("mail_server_filter_list failed: %v", err)
	}
	gotFilters, _ := filters["filters"].([]email.ServerFilter)
	if len(gotFilters) != 1 || gotFilters[0].ID != "f1" {
		t.Fatalf("filters = %+v", gotFilters)
	}

	upserted, err := s.callTool("mail_server_filter_upsert", map[string]interface{}{
		"account_id": account.ID,
		"filter": map[string]interface{}{
			"name":    "Archive updates",
			"enabled": true,
			"action": map[string]interface{}{
				"archive": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("mail_server_filter_upsert failed: %v", err)
	}
	gotFilter, _ := upserted["filter"].(email.ServerFilter)
	if gotFilter.ID == "" || gotFilter.Name != "Archive updates" {
		t.Fatalf("filter = %+v", gotFilter)
	}

	deleted, err := s.callTool("mail_server_filter_delete", map[string]interface{}{
		"account_id": account.ID,
		"filter_id":  "generated",
	})
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
	provider := &fakeMailProvider{
		listIDs: []string{"m1", "m2"},
		messages: map[string]*providerdata.EmailMessage{
			"m1": {ID: "m1", Subject: "Weekly digest", Sender: "newsletter@example.com", Date: now},
			"m2": {ID: "m2", Subject: "Second digest", Sender: "newsletter@example.com", Date: now.Add(-time.Hour)},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	acted, err := s.callTool("mail_action", map[string]interface{}{
		"account_id": account.ID,
		"action":     "archive",
		"query":      "from:newsletter@example.com newer_than:7d",
		"limit":      2,
	})
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
	provider := &fakeMailProvider{
		listIDs:       []string{"m1", "m2"},
		supportsDefer: true,
		messages: map[string]*providerdata.EmailMessage{
			"m1": {ID: "m1", Subject: "Weekly digest", Sender: "newsletter@example.com", Date: now},
			"m2": {ID: "m2", Subject: "Second digest", Sender: "newsletter@example.com", Date: now.Add(-time.Hour)},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	acted, err := s.callTool("mail_action", map[string]interface{}{
		"account_id": account.ID,
		"action":     "defer",
		"query":      "from:newsletter@example.com newer_than:7d",
		"limit":      2,
		"until":      untilAt.Format(time.RFC3339),
	})
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

	_, err = s.callTool("mail_action", map[string]interface{}{
		"account_id": account.ID,
		"action":     "archive",
	})
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

	_, err = s.callTool("mail_action", map[string]interface{}{
		"account_id":  account.ID,
		"action":      "defer",
		"message_ids": []interface{}{"m1"},
	})
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

	_, err = s.callTool("mail_action", map[string]interface{}{
		"account_id":  account.ID,
		"action":      "defer",
		"message_ids": []interface{}{"m1"},
		"until":       "2030-03-20T15:04:05Z",
	})
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
	if _, err := st.UpsertExternalBinding(store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     store.ExternalProviderExchangeEWS,
		ObjectType:   "email",
		RemoteID:     "m1",
		ItemID:       &item.ID,
		ArtifactID:   &artifact.ID,
		ContainerRef: &containerRef,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding: %v", err)
	}
	now := time.Date(2026, time.March, 17, 10, 0, 0, 0, time.UTC)
	provider := &fakeMailProvider{
		resolvedIDs: map[string]string{"m1": "m1-trash"},
		messages: map[string]*providerdata.EmailMessage{
			"m1": {ID: "m1", Subject: "Subject", Sender: "alice@example.com", Labels: []string{"Posteingang", "INBOX"}, Date: now},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	acted, err := s.callTool("mail_action", map[string]interface{}{
		"account_id":  account.ID,
		"action":      "trash",
		"message_ids": []interface{}{"m1"},
	})
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
