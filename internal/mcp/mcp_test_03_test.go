package mcp

import (
	"bytes"
	"context"
	"errors"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMailDraftSendUnsupportedProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "ada@example.com", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return &minimalDraftProvider{}, nil
	}
	_, err = s.callTool("mail_draft_send", map[string]interface{}{"account_id": float64(account.ID), "draft_id": "draft-abc"})
	if err == nil {
		t.Fatalf("expected error for provider without ExistingDraftSender")
	}
	if !strings.Contains(err.Error(), "does not support sending an existing draft") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func extractContentType(raw []byte) string {
	header, _, _ := splitHeaderAndBody(raw)
	for _, line := range strings.Split(header, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			return strings.TrimSpace(line[len("content-type:"):])
		}
	}
	return ""
}

func extractMIMEBody(raw []byte) []byte {
	_, body, _ := splitHeaderAndBody(raw)
	return []byte(body)
}

func splitHeaderAndBody(raw []byte) (string, string, bool) {
	idx := bytes.Index(raw, []byte("\r\n\r\n"))
	if idx < 0 {
		return string(raw), "", false
	}
	return string(raw[:idx]), string(raw[idx+4:]), true
}

type flagStubProvider struct {
	name        string
	flagErr     error
	flagStatus  string
	flagDueAt   *time.Time
	flagCount   int
	lastFlagIDs []string // flagStubProvider is the minimum EmailProvider implementation plus the
	// FlagMutator and CategoryMutator capabilities so server_mail_flags.go
	// routing can be exercised without touching the real backends.

	clearCount     int
	lastClearIDs   []string
	catErr         error
	catCount       int
	lastCatIDs     []string
	lastCategories []string
	supportFlag    bool
	supportCat     bool
}

func (p *flagStubProvider) ListLabels(_ context.Context) ([]providerdata.Label, error) {
	return nil, nil
}

func (p *flagStubProvider) ListMessages(_ context.Context, _ email.SearchOptions) ([]string, error) {
	return nil, nil
}

func (p *flagStubProvider) GetMessage(_ context.Context, _, _ string) (*providerdata.EmailMessage, error) {
	return nil, nil
}

func (p *flagStubProvider) GetMessages(_ context.Context, _ []string, _ string) ([]*providerdata.EmailMessage, error) {
	return nil, nil
}

func (p *flagStubProvider) MarkRead(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (p *flagStubProvider) MarkUnread(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (p *flagStubProvider) Archive(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (p *flagStubProvider) MoveToInbox(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (p *flagStubProvider) Trash(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (p *flagStubProvider) Delete(_ context.Context, _ []string) (int, error) {
	return 0, nil
}

func (p *flagStubProvider) ProviderName() string {
	if p.name != "" {
		return p.name
	}
	return "flagstub"
}

func (p *flagStubProvider) Close() error {
	return nil
}

type flagMutatorStub struct{ *flagStubProvider }

func (p *flagMutatorStub) SetFlag(_ context.Context, ids []string, flag email.Flag) (int, error) {
	p.lastFlagIDs = append([]string(nil), ids...)
	p.flagStatus = flag.Status
	p.flagDueAt = flag.DueAt
	if p.flagErr != nil {
		return 0, p.flagErr
	}
	return p.flagCount, nil
}

func (p *flagMutatorStub) ClearFlag(_ context.Context, ids []string) (int, error) {
	p.lastClearIDs = append([]string(nil), ids...)
	return p.clearCount, nil
}

type categoryMutatorStub struct{ *flagStubProvider }

func (p *categoryMutatorStub) SetCategories(_ context.Context, ids, categories []string) (int, error) {
	p.lastCatIDs = append([]string(nil), ids...)
	p.lastCategories = append([]string(nil), categories...)
	if p.catErr != nil {
		return 0, p.catErr
	}
	return p.catCount, nil
}

type fullFlagStub struct {
	*flagMutatorStub
	*categoryMutatorStub
}

func (p *fullFlagStub) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	return p.flagMutatorStub.ListLabels(ctx)
}

func (p *fullFlagStub) ListMessages(ctx context.Context, opts email.SearchOptions) ([]string, error) {
	return p.flagMutatorStub.ListMessages(ctx, opts)
}

func (p *fullFlagStub) GetMessage(ctx context.Context, id, format string) (*providerdata.EmailMessage, error) {
	return p.flagMutatorStub.GetMessage(ctx, id, format)
}

func (p *fullFlagStub) GetMessages(ctx context.Context, ids []string, format string) ([]*providerdata.EmailMessage, error) {
	return p.flagMutatorStub.GetMessages(ctx, ids, format)
}

func (p *fullFlagStub) MarkRead(ctx context.Context, ids []string) (int, error) {
	return p.flagMutatorStub.MarkRead(ctx, ids)
}

func (p *fullFlagStub) MarkUnread(ctx context.Context, ids []string) (int, error) {
	return p.flagMutatorStub.MarkUnread(ctx, ids)
}

func (p *fullFlagStub) Archive(ctx context.Context, ids []string) (int, error) {
	return p.flagMutatorStub.Archive(ctx, ids)
}

func (p *fullFlagStub) MoveToInbox(ctx context.Context, ids []string) (int, error) {
	return p.flagMutatorStub.MoveToInbox(ctx, ids)
}

func (p *fullFlagStub) Trash(ctx context.Context, ids []string) (int, error) {
	return p.flagMutatorStub.Trash(ctx, ids)
}

func (p *fullFlagStub) Delete(ctx context.Context, ids []string) (int, error) {
	return p.flagMutatorStub.Delete(ctx, ids)
}

func (p *fullFlagStub) ProviderName() string {
	return p.flagMutatorStub.ProviderName()
}

func (p *fullFlagStub) Close() error {
	return p.flagMutatorStub.Close()
}

func newFlagMutatorOnly(stub *flagStubProvider) email.EmailProvider {
	return &struct {
		*flagStubProvider
		*flagMutatorStub
	}{stub, &flagMutatorStub{stub}}
}

func newCategoryMutatorOnly(stub *flagStubProvider) email.EmailProvider {
	return &struct {
		*flagStubProvider
		*categoryMutatorStub
	}{stub, &categoryMutatorStub{stub}}
}

func newFullFlagProvider(stub *flagStubProvider) email.EmailProvider {
	return &struct {
		*flagStubProvider
		*flagMutatorStub
		*categoryMutatorStub
	}{stub, &flagMutatorStub{stub}, &categoryMutatorStub{stub}}
}

func seedFlagMailAccount(t *testing.T) (*store.Store, store.ExternalAccount) {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Mail Flag", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	return st, account
}

func TestMailFlagSetRoutesThroughMutator(t *testing.T) {
	st, account := seedFlagMailAccount(t)
	stub := &flagStubProvider{flagCount: 2}
	s := NewServerWithStore(t.TempDir(), st)
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return newFullFlagProvider(stub), nil
	}
	got, err := s.callTool("mail_flag_set", map[string]interface{}{"account_id": account.ID, "message_ids": []interface{}{"m1", "m2"}, "status": "flagged"})
	if err != nil {
		t.Fatalf("mail_flag_set failed: %v", err)
	}
	if stub.flagStatus != email.FlagStatusFlagged {
		t.Fatalf("status = %q, want %q", stub.flagStatus, email.FlagStatusFlagged)
	}
	if len(stub.lastFlagIDs) != 2 || stub.lastFlagIDs[0] != "m1" {
		t.Fatalf("lastFlagIDs = %v", stub.lastFlagIDs)
	}
	if got["succeeded"].(int) != 2 {
		t.Fatalf("succeeded = %v, want 2", got["succeeded"])
	}
}

func TestMailFlagSetCapabilityUnsupportedSurfaced(t *testing.T) {
	st, account := seedFlagMailAccount(t)
	stub := &flagStubProvider{flagErr: email.ErrCapabilityUnsupported}
	s := NewServerWithStore(t.TempDir(), st)
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return newFullFlagProvider(stub), nil
	}
	got, err := s.callTool("mail_flag_set", map[string]interface{}{"account_id": account.ID, "message_ids": []interface{}{"m1"}, "status": "complete"})
	if err != nil {
		t.Fatalf("mail_flag_set returned error: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
}

func TestMailFlagSetRejectsProviderWithoutMutator(t *testing.T) {
	st, account := seedFlagMailAccount(t)
	stub := &flagStubProvider{}
	s := NewServerWithStore(t.TempDir(), st)
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return stub, nil
	}
	if _, err := s.callTool("mail_flag_set", map[string]interface{}{"account_id": account.ID, "message_ids": []interface{}{"m1"}, "status": "flagged"}); err == nil || !contains(err.Error(), "flag mutation is not supported") {
		t.Fatalf("expected capability error, got %v", err)
	}
}

func TestMailFlagClearDelegatesToProvider(t *testing.T) {
	st, account := seedFlagMailAccount(t)
	stub := &flagStubProvider{clearCount: 3}
	s := NewServerWithStore(t.TempDir(), st)
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return newFullFlagProvider(stub), nil
	}
	got, err := s.callTool("mail_flag_clear", map[string]interface{}{"account_id": account.ID, "message_ids": []interface{}{"m1", "m2", "m3"}})
	if err != nil {
		t.Fatalf("mail_flag_clear failed: %v", err)
	}
	if got["succeeded"].(int) != 3 {
		t.Fatalf("succeeded = %v, want 3", got["succeeded"])
	}
	if len(stub.lastClearIDs) != 3 {
		t.Fatalf("lastClearIDs = %v", stub.lastClearIDs)
	}
}

func TestMailCategoriesSetDelegatesToProvider(t *testing.T) {
	st, account := seedFlagMailAccount(t)
	stub := &flagStubProvider{catCount: 2}
	s := NewServerWithStore(t.TempDir(), st)
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return newFullFlagProvider(stub), nil
	}
	got, err := s.callTool("mail_categories_set", map[string]interface{}{"account_id": account.ID, "message_ids": []interface{}{"m1", "m2"}, "categories": []interface{}{"Clients", "Urgent"}})
	if err != nil {
		t.Fatalf("mail_categories_set failed: %v", err)
	}
	if got["succeeded"].(int) != 2 {
		t.Fatalf("succeeded = %v, want 2", got["succeeded"])
	}
	if len(stub.lastCategories) != 2 || stub.lastCategories[0] != "Clients" {
		t.Fatalf("lastCategories = %v", stub.lastCategories)
	}
}

func TestMailCategoriesSetDedupesAndTrims(t *testing.T) {
	cases := map[string]struct {
		in   interface{}
		want []string
	}{"mixed-case dedupe": {in: []interface{}{"Clients", " clients ", "  ", "Urgent"}, want: []string{"Clients", "Urgent"}}, "string slice": {in: []string{"A", "B"}, want: []string{"A", "B"}}, "empty": {in: []interface{}{}, want: []string{}}}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			got, err := mailCategoriesArg(map[string]interface{}{"categories": tc.in})
			if err != nil {
				t.Fatalf("mailCategoriesArg: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tc.want), got)
			}
			for i, v := range got {
				if v != tc.want[i] {
					t.Fatalf("got[%d] = %q, want %q", i, v, tc.want[i])
				}
			}
		})
	}
}

func TestNormalizeFlagStatus(t *testing.T) {
	cases := map[string]struct {
		in      string
		want    string
		wantErr bool
	}{"empty defaults to flagged": {in: "", want: email.FlagStatusFlagged}, "flagged": {in: "flagged", want: email.FlagStatusFlagged}, "mixed case": {in: "Complete", want: email.FlagStatusComplete}, "not flagged": {in: "notFlagged", want: email.FlagStatusNotFlagged}, "unknown": {in: "foo", wantErr: true}}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			got, err := normalizeFlagStatus(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseFlagDueAtFormats(t *testing.T) {
	for _, raw := range []string{"2026-04-20", "2026-04-20 12:34", "2026-04-20T12:34", "2026-04-20T12:34:56Z"} {
		if _, err := parseFlagDueAt(raw); err != nil {
			t.Errorf("parseFlagDueAt(%q) err = %v", raw, err)
		}
	}
	if _, err := parseFlagDueAt("not-a-time"); err == nil {
		t.Fatal("parseFlagDueAt(not-a-time) expected error")
	}
}

func TestErrCapabilityUnsupportedWraps(t *testing.T) {
	wrapped := errors.New("gmail flag status \"complete\": " + email.ErrCapabilityUnsupported.Error())
	if errors.Is(wrapped, email.ErrCapabilityUnsupported) {
		t.Fatal("plain-string wrap should NOT satisfy errors.Is")
	}
	wrappedW := errors.Join(email.ErrCapabilityUnsupported, errors.New("context"))
	if !errors.Is(wrappedW, email.ErrCapabilityUnsupported) {
		t.Fatal("errors.Join wrap should satisfy errors.Is")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

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
	lastFormat    string
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

func (p *fakeMailProvider) GetMessages(_ context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	p.lastFormat = format
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
