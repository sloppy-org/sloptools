package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/meetings"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

type fakeOOFProvider struct {
	name        string
	state       providerdata.OOFSettings
	getCalls    int
	setCalls    int
	closeCalls  int
	failGetWith error
	failSetWith error
}

func (p *fakeOOFProvider) GetOOF(_ context.Context) (providerdata.OOFSettings, error) {
	p.getCalls++
	if p.failGetWith != nil {
		return providerdata.OOFSettings{}, p.failGetWith
	}
	return p.state, nil
}

func (p *fakeOOFProvider) SetOOF(_ context.Context, settings providerdata.OOFSettings) error {
	p.setCalls++
	if p.failSetWith != nil {
		return p.failSetWith
	}
	p.state = settings
	return nil
}

func (p *fakeOOFProvider) ProviderName() string {
	if p.name == "" {
		return "fake_oof"
	}
	return p.name
}

func (p *fakeOOFProvider) Close() error {
	p.closeCalls++
	return nil
}

type fakeDelegationProvider struct {
	fakeOOFProvider
	delegates       []providerdata.Delegate
	shared          []providerdata.SharedMailbox
	delegateErr     error
	sharedErr       error
	delegateCalls   int
	sharedCalls     int
	closeCallsExtra int
}

func (p *fakeDelegationProvider) ListDelegates(_ context.Context) ([]providerdata.Delegate, error) {
	p.delegateCalls++
	if p.delegateErr != nil {
		return nil, p.delegateErr
	}
	return p.delegates, nil
}

func (p *fakeDelegationProvider) ListSharedMailboxes(_ context.Context) ([]providerdata.SharedMailbox, error) {
	p.sharedCalls++
	if p.sharedErr != nil {
		return nil, p.sharedErr
	}
	return p.shared, nil
}

func TestMailDelegateListReturnsDelegatesAndSharedMailboxes(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeDelegationProvider{
		fakeOOFProvider: fakeOOFProvider{name: "gmail_mailbox_settings"},
		delegates: []providerdata.Delegate{
			{Email: "alice@example.com", Name: "Alice", Permissions: []string{"verification:accepted"}},
			{Email: "bob@example.com", Permissions: []string{"verification:pending"}},
		},
		shared: []providerdata.SharedMailbox{
			{Email: "archive@example.com", AccessLevel: "forwarding:accepted"},
		},
	}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_delegate_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_delegate_list: %v", err)
	}
	if got["provider"] != "gmail_mailbox_settings" {
		t.Fatalf("provider = %v", got["provider"])
	}
	delegates, ok := got["delegates"].([]map[string]interface{})
	if !ok {
		t.Fatalf("delegates = %T, want []map[string]interface{}", got["delegates"])
	}
	if len(delegates) != 2 {
		t.Fatalf("len(delegates) = %d, want 2", len(delegates))
	}
	if stringValue(t, delegates[0]["email"]) != "alice@example.com" {
		t.Fatalf("delegates[0].email = %v", delegates[0]["email"])
	}
	if stringValue(t, delegates[0]["name"]) != "Alice" {
		t.Fatalf("delegates[0].name = %v", delegates[0]["name"])
	}
	perms, ok := delegates[0]["permissions"].([]string)
	if !ok || len(perms) != 1 || perms[0] != "verification:accepted" {
		t.Fatalf("delegates[0].permissions = %v", delegates[0]["permissions"])
	}
	shared, ok := got["shared_mailboxes"].([]map[string]interface{})
	if !ok {
		t.Fatalf("shared_mailboxes = %T", got["shared_mailboxes"])
	}
	if len(shared) != 1 {
		t.Fatalf("len(shared_mailboxes) = %d, want 1", len(shared))
	}
	if stringValue(t, shared[0]["access_level"]) != "forwarding:accepted" {
		t.Fatalf("shared_mailboxes[0].access_level = %v", shared[0]["access_level"])
	}
	if provider.delegateCalls != 1 || provider.sharedCalls != 1 {
		t.Fatalf("delegateCalls=%d sharedCalls=%d, both want 1", provider.delegateCalls, provider.sharedCalls)
	}
	if provider.closeCalls != 1 {
		t.Fatalf("provider.closeCalls = %d, want 1", provider.closeCalls)
	}
}

func TestMailDelegateListReturnsCapabilityUnsupportedForNonDelegationProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return &fakeOOFProvider{name: "fake_no_delegation"}, nil
	}

	got, err := s.callTool("mail_delegate_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_delegate_list: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "mailboxsettings.DelegationProvider" {
		t.Fatalf("capability = %v", got["capability"])
	}
}

func TestMailDelegateListSurfacesUnsupportedErrorFromDelegatesListAsCapabilityCode(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeDelegationProvider{
		fakeOOFProvider: fakeOOFProvider{name: "exchange_ews_mailbox_settings"},
		delegateErr:     errors.New("wrapped: " + mailboxsettings.ErrUnsupported.Error()),
	}
	provider.delegateErr = &wrappedErr{inner: mailboxsettings.ErrUnsupported, msg: "ews: wrapped"}
	s.newMailboxSettingsProvider = func(_ context.Context, _ store.ExternalAccount) (mailboxsettings.OOFProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_delegate_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("mail_delegate_list: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
}

type wrappedErr struct {
	inner error
	msg   string
}

func (e *wrappedErr) Error() string { return e.msg }
func (e *wrappedErr) Unwrap() error { return e.inner }

const summaryMeetingNote = `---
title: "Standup 2026-04-29"
date: 2026-04-29
owner: "Christopher Albert"
---
# Standup 2026-04-29

## Attendees
- Christopher Albert
- Ada Lovelace
- Charles Babbage

## Decisions
- Ship the analytical engine paper before the conference.
- Hold a calibration retro on Friday.

## Action Checklist

### Ada Lovelace
- [ ] Draft benchmark write-up @due:2026-05-02

### Charles Babbage
- [ ] Send the analytical engine paper @due:2026-05-09

### Christopher Albert
- [ ] File the conference travel claim
`

func TestMeetingSummaryDraftEmitsOneDraftPerNonOwnerAttendee(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Ada Lovelace.md"), `---
email: ada@example.com
---
# Ada Lovelace
`)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Charles Babbage.md"), `---
---
# Charles Babbage
`)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Christopher Albert.md"), "# Christopher Albert\n")

	sourcesPath := writeMeetingsSummarySources(t, tmp, meetingsRoot, map[string]string{"Charles Babbage": "babbage@example.com"}, "")

	server := NewServer(t.TempDir())
	got, err := server.callTool("meeting.summary.draft", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	})
	if err != nil {
		t.Fatalf("meeting.summary.draft: %v", err)
	}
	if got["count"].(int) != 2 {
		t.Fatalf("count = %#v", got["count"])
	}
	drafts, ok := got["drafts"].([]meetings.Draft)
	if !ok {
		t.Fatalf("drafts payload type: %T", got["drafts"])
	}
	if len(drafts) != 2 {
		t.Fatalf("drafts = %#v", drafts)
	}
	emails := map[string]meetings.Draft{}
	for _, d := range drafts {
		emails[strings.ToLower(d.Recipient)] = d
	}
	ada := emails["ada lovelace"]
	if ada.Email != "ada@example.com" || ada.Diagnostic != "" {
		t.Fatalf("ada draft = %#v", ada)
	}
	if !strings.Contains(ada.Body, "Draft benchmark write-up") {
		t.Fatalf("ada body missing tasks: %s", ada.Body)
	}
	if strings.Contains(ada.Body, "File the conference travel claim") {
		t.Fatalf("ada body leaked owner task")
	}
	if !strings.Contains(ada.Body, "Decisions:") || !strings.Contains(ada.Body, "analytical engine paper") {
		t.Fatalf("ada body missing decisions: %s", ada.Body)
	}
	babbage := emails["charles babbage"]
	if babbage.Email != "babbage@example.com" {
		t.Fatalf("babbage email override missed: %#v", babbage)
	}

	share, _ := got["share"].(meetingShareView)
	if share.Kind != "folder" {
		t.Fatalf("share kind = %#v", share)
	}
	if !strings.HasSuffix(share.AbsolutePath, filepath.Join("MEETINGS", "2026-04-29-standup")) {
		t.Fatalf("share absolute path = %q", share.AbsolutePath)
	}
}

func TestMeetingSummaryDraftPrefersBrainEmailOverPerUserOverride(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Ada Lovelace.md"), `---
email: brain@example.com
---
# Ada Lovelace
`)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Charles Babbage.md"), "# Charles Babbage\n")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Christopher Albert.md"), "# Christopher Albert\n")

	sourcesPath := writeMeetingsSummarySources(t, tmp, meetingsRoot, map[string]string{"Ada Lovelace": "override@example.com"}, "")

	server := NewServer(t.TempDir())
	got, err := server.callTool("meeting.summary.draft", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
		"recipient":      "Ada Lovelace",
	})
	if err != nil {
		t.Fatalf("meeting.summary.draft: %v", err)
	}
	drafts := got["drafts"].([]meetings.Draft)
	if len(drafts) != 1 {
		t.Fatalf("drafts = %#v", drafts)
	}
	if drafts[0].Email != "brain@example.com" {
		t.Fatalf("brain frontmatter must win over per-user override; got %q", drafts[0].Email)
	}
	if drafts[0].Diagnostic != "" {
		t.Fatalf("diagnostic should be empty: %q", drafts[0].Diagnostic)
	}
}

func TestMeetingSummaryDraftEmitsNeedsRecipientWhenEmailMissing(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Ada Lovelace.md"), "# Ada\n")

	sourcesPath := writeMeetingsSummarySources(t, tmp, meetingsRoot, nil, "")

	server := NewServer(t.TempDir())
	got, err := server.callTool("meeting.summary.draft", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
		"recipient":      "Ada Lovelace",
	})
	if err != nil {
		t.Fatalf("meeting.summary.draft: %v", err)
	}
	drafts := got["drafts"].([]meetings.Draft)
	if len(drafts) != 1 || drafts[0].Diagnostic != meetingSummaryDiagnosticNeedsRecipient {
		t.Fatalf("expected needs_recipient diagnostic, got %#v", drafts)
	}
}

func TestMeetingSummaryDraftSelectsLooseFileWhenFolderAbsent(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-05-01-1on1.md")
	writeMCPBrainFile(t, notesPath, `---
title: "1on1 with Ada"
owner: "Christopher Albert"
---
# 1on1 with Ada

## Attendees
- Christopher Albert
- Ada Lovelace

## Decisions
- Defer the proposal review until Monday.

## Action Checklist

### Ada Lovelace
- [ ] Send the proposal draft
`)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "people", "Ada Lovelace.md"), `---
email: ada@example.com
---
# Ada
`)
	sourcesPath := writeMeetingsSummarySources(t, tmp, meetingsRoot, nil, "https://cloud.example/s/{vault_relative_path}")

	server := NewServer(t.TempDir())
	got, err := server.callTool("meeting.summary.draft", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-05-01-1on1",
	})
	if err != nil {
		t.Fatalf("meeting.summary.draft: %v", err)
	}
	share := got["share"].(meetingShareView)
	if share.Kind != "file" {
		t.Fatalf("expected file share, got %#v", share)
	}
	if !strings.Contains(share.URL, "MEETINGS/2026-05-01-1on1.md") {
		t.Fatalf("share URL = %q", share.URL)
	}
	drafts := got["drafts"].([]meetings.Draft)
	if len(drafts) != 1 || !strings.Contains(drafts[0].Body, share.URL) {
		t.Fatalf("draft body must embed share url: %#v", drafts)
	}
}

func TestMeetingShareCreateAndRevokeRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	meetingsRoot := filepath.Join(tmp, "work", "MEETINGS")
	notesPath := filepath.Join(meetingsRoot, "2026-04-29-standup", "MEETING_NOTES.md")
	writeMCPBrainFile(t, notesPath, summaryMeetingNote)
	sourcesPath := writeMeetingsSummarySources(t, tmp, meetingsRoot, nil, "")

	server := NewServer(t.TempDir())
	created, err := server.callTool("meeting.share.create", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
		"url":            "https://cloud.example/s/AAA",
		"token":          "AAA",
		"permissions":    "edit",
		"expiry_days":    90,
	})
	if err != nil {
		t.Fatalf("share.create: %v", err)
	}
	if created["url"].(string) != "https://cloud.example/s/AAA" {
		t.Fatalf("share url not persisted: %#v", created)
	}
	if created["permissions"].(string) != "edit" {
		t.Fatalf("permissions = %#v", created["permissions"])
	}
	statePath := filepath.Join(meetingsRoot, "2026-04-29-standup", ".share.json")
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state meetings.ShareState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.URL != "https://cloud.example/s/AAA" || state.Permissions != "edit" {
		t.Fatalf("state = %#v", state)
	}
	if _, err := server.callTool("meeting.share.revoke", map[string]interface{}{
		"config_path":    configPath,
		"sources_config": sourcesPath,
		"sphere":         "work",
		"slug":           "2026-04-29-standup",
	}); err != nil {
		t.Fatalf("share.revoke: %v", err)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("state must be removed after revoke, err=%v", err)
	}
}

func writeMeetingsSummarySources(t *testing.T, root, meetingsRoot string, peopleEmails map[string]string, urlTemplate string) string {
	t.Helper()
	path := filepath.Join(root, "sources.toml")
	var b strings.Builder
	b.WriteString("[meetings.work]\n")
	b.WriteString("meetings_root = \"" + filepath.ToSlash(meetingsRoot) + "\"\n")
	b.WriteString("owner = \"Christopher Albert\"\n")
	if urlTemplate != "" {
		b.WriteString("[meetings.work.share]\n")
		b.WriteString("url_template = \"" + urlTemplate + "\"\n")
		b.WriteString("permissions = \"edit\"\n")
	}
	if len(peopleEmails) > 0 {
		b.WriteString("[meetings.work.people_emails]\n")
		for name, email := range peopleEmails {
			b.WriteString("\"" + name + "\" = \"" + email + "\"\n")
		}
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write sources: %v", err)
	}
	return path
}
