package mcp

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/peoplebrief"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/surface"
)

type fakeMailProvider struct {
	labels             []providerdata.Label
	listIDs            []string
	pageIDs            []string
	nextPage           string
	messages           map[string]*providerdata.EmailMessage
	attachment         *providerdata.AttachmentData
	filters            []email.ServerFilter
	resolvedIDs        map[string]string
	lastOpts           email.SearchOptions
	lastAction         string
	lastIDs            []string
	lastFolder         string
	lastLabel          string
	lastUntil          time.Time
	lastFormat         string
	lastFlag           email.Flag
	lastCategories     []string
	getMessagesFormats []string
	supportsDefer      bool
}

func (p *fakeMailProvider) SetFlag(_ context.Context, ids []string, flag email.Flag) (int, error) {
	p.record("set_flag", ids)
	p.lastFlag = flag
	return len(ids), nil
}

func (p *fakeMailProvider) ClearFlag(_ context.Context, ids []string) (int, error) {
	p.record("clear_flag", ids)
	p.lastFlag = email.Flag{}
	return len(ids), nil
}

func (p *fakeMailProvider) SetCategories(_ context.Context, ids []string, categories []string) (int, error) {
	p.record("set_categories", ids)
	p.lastCategories = append([]string(nil), categories...)
	return len(ids), nil
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
	msg := p.messages[messageID]
	if msg == nil {
		return nil, nil
	}
	return cloneMailMessage(msg), nil
}

func (p *fakeMailProvider) GetMessages(_ context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	p.getMessagesFormats = append(p.getMessagesFormats, format)
	p.lastFormat = format
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		msg := p.messages[id]
		if msg == nil {
			out = append(out, nil)
			continue
		}
		clone := cloneMailMessage(msg)
		if format == "metadata" {
			clone.BodyText = nil
			clone.BodyHTML = nil
			clone.Attachments = nil
		}
		out = append(out, clone)
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

func TestMailToolsListReadAndAttachment(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	listed, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "account_list"})
	if err != nil {
		t.Fatalf("mail_account_list failed: %v", err)
	}
	accounts, _ := listed["accounts"].([]store.ExternalAccount)
	if len(accounts) != 1 || accounts[0].ID != account.ID {
		t.Fatalf("accounts = %+v", accounts)
	}
	messages, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "message_list", "account_id": account.ID, "page_token": "next-1"})
	if err != nil {
		t.Fatalf("mail_message_list failed: %v", err)
	}
	if got := messages["next_page_token"]; got != "next-2" {
		t.Fatalf("next_page_token = %#v", got)
	}
	if provider.lastFormat != "metadata" {
		t.Fatalf("list format = %q, want metadata", provider.lastFormat)
	}
	if provider.lastOpts.MaxResults != compactListLimit {
		t.Fatalf("default list limit = %d, want %d", provider.lastOpts.MaxResults, compactListLimit)
	}
	message, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "message_get", "account_id": account.ID, "message_id": "m1"})
	if err != nil {
		t.Fatalf("mail_message_get failed: %v", err)
	}
	gotMessage, _ := message["message"].(*providerdata.EmailMessage)
	if gotMessage == nil || gotMessage.ID != "m1" {
		t.Fatalf("message = %#v", message["message"])
	}
	destDir := t.TempDir()
	attachment, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "attachment_get", "account_id": account.ID, "message_id": "m1", "attachment_id": "att-1", "dest_dir": destDir})
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

func TestMailMessageListDefaultsToSphereAccount(t *testing.T) {
	s, account, _ := newMailToolsFixture(t)
	messages, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "message_list", "sphere": store.SphereWork, "limit": 3})
	if err != nil {
		t.Fatalf("mail_message_list by sphere failed: %v", err)
	}
	gotAccount, ok := messages["account"].(store.ExternalAccount)
	if !ok {
		t.Fatalf("account payload = %#v", messages["account"])
	}
	if gotAccount.ID != account.ID {
		t.Fatalf("account id = %d, want %d", gotAccount.ID, account.ID)
	}
	if got := messages["count"]; got != 1 {
		t.Fatalf("count = %#v, want 1", got)
	}
}

func TestMailMessageListCanRequestBody(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	if _, err := s.callTool("sloppy_mail", map[string]interface{}{"action": "message_list", "account_id": account.ID, "include_body": true}); err != nil {
		t.Fatalf("mail_message_list failed: %v", err)
	}
	if provider.lastFormat != "full" {
		t.Fatalf("list format = %q, want full", provider.lastFormat)
	}
}

func requireAffectedRefs(t *testing.T, got map[string]interface{}) []affectedRef {
	t.Helper()
	affected, ok := got["affected"].([]affectedRef)
	if !ok {
		t.Fatalf("affected = %T, want []affectedRef", got["affected"])
	}
	if len(affected) == 0 {
		t.Fatalf("affected is empty: %#v", got)
	}
	return affected
}

func requireSingleAffectedRef(t *testing.T, got map[string]interface{}) affectedRef {
	t.Helper()
	affected := requireAffectedRefs(t, got)
	if len(affected) != 1 {
		t.Fatalf("len(affected) = %d, want 1: %#v", len(affected), affected)
	}
	return affected[0]
}

func TestMailReadToolDefinitionsAllowSphereDefault(t *testing.T) {
	defs := toolDefinitions()
	names := map[string]map[string]interface{}{}
	for _, def := range defs {
		name, _ := def["name"].(string)
		names[name] = def
	}
	schema, _ := names["sloppy_mail"]["inputSchema"].(map[string]interface{})
	props, _ := schema["properties"].(map[string]interface{})
	if props["sphere"] == nil {
		t.Fatalf("sloppy_mail schema lacks sphere property: %#v", props)
	}
	requiredFields, _ := schema["required"].([]string)
	for _, required := range requiredFields {
		if required == "account_id" {
			t.Fatalf("sloppy_mail still requires account_id: %#v", schema["required"])
		}
	}
}

func TestToolDefinitionsAdvertiseCompactDefaults(t *testing.T) {
	defs := toolDefinitions()
	names := map[string]map[string]interface{}{}
	for _, def := range defs {
		name, _ := def["name"].(string)
		names[name] = def
	}
	mailDesc, _ := names["sloppy_mail"]["description"].(string)
	if !strings.Contains(mailDesc, "mail") {
		t.Fatalf("sloppy_mail description should mention mail: %q", mailDesc)
	}
	calendarDesc, _ := names["sloppy_calendar"]["description"].(string)
	if !strings.Contains(strings.ToLower(calendarDesc), "calendar") {
		t.Fatalf("sloppy_calendar description should mention calendar: %q", calendarDesc)
	}
}

func TestToolDefinitionsStayConcise(t *testing.T) {
	for _, def := range toolDefinitions() {
		name, _ := def["name"].(string)
		desc, _ := def["description"].(string)
		if len(desc) > 300 {
			t.Fatalf("%s description has %d chars, want <= 300", name, len(desc))
		}
		schema, _ := def["inputSchema"].(map[string]interface{})
		props, _ := schema["properties"].(map[string]interface{})
		for propName, raw := range props {
			prop, _ := raw.(map[string]interface{})
			propDesc, _ := prop["description"].(string)
			if len(propDesc) > 160 {
				t.Fatalf("%s.%s description has %d chars, want <= 160", name, propName, len(propDesc))
			}
		}
	}
}

func TestGroupwareDocListsEveryMCPTool(t *testing.T) {
	docPath := "../../docs/groupware.md"
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read groupware doc: %v", err)
	}
	doc := string(data)

	// groupware scope for this doc (brain, canvas, handoff, workspace are out of scope)
	inScope := []string{"sloppy_mail", "sloppy_calendar", "sloppy_contacts", "sloppy_tasks", "sloppy_evernote", "sloppy_inbox"}

	// code → doc: every in-scope tool name must appear backtick-quoted in the doc
	for _, name := range inScope {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("tool %q missing from groupware doc", name)
		}
	}

	// doc → code: every backtick-quoted sloppy_* name in the doc must be a known code tool
	codeToolNames := map[string]bool{}
	for _, tool := range surface.MCPTools {
		codeToolNames[tool.Name] = true
	}
	docRe := regexp.MustCompile("`(sloppy_[a-z][a-z0-9_]*)`")
	for _, m := range docRe.FindAllStringSubmatch(doc, -1) {
		if !codeToolNames[m[1]] {
			t.Errorf("tool %q in groupware doc but not in code", m[1])
		}
	}
}

func TestTaskMutationSurfaceExposesSourceMetadata(t *testing.T) {
	tool := surfaceToolByName(t, "sloppy_tasks")
	if tool.Name == "" {
		t.Fatal("sloppy_tasks not found in surface")
	}
	for _, action := range []string{"create", "update"} {
		if !strings.Contains(tool.Description, action) {
			t.Errorf("sloppy_tasks description missing action %q", action)
		}
	}
}

func TestBrainPeopleBriefDispatchSurfacesAllFourDataSources(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writePersonNote(t, tmp, "Ada Example", `---
kind: human
sphere: work
role: collaborator
supervision_role: postdoc co-advisor
focus: active
cadence: monthly
last_seen: 2026-04-15
affiliation: Example Lab
email: ada@example.com
---

# Ada Example

## Recent context

- 2026-04-22: Reviewed plasma outline.
- 2026-03-10: Aligned on funding wording.
- 2026-02-01: Initial scoping call.
`)
	writePeopleCommitment(t, tmp, "wait.md", "waiting", "Waiting on Ada", "Ada Example", []string{"Ada Example"}, "", "")
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "meetings", "2026-04-29-standup.md"), `---
kind: meeting
title: Standup
date: 2026-04-29
---

# Standup

- [[people/Ada Example]]
`)
	s, st, _ := newDomainServerForTest(t)
	s.brainConfigPath = configPath
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "Work Mail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	when := time.Date(2026, time.April, 28, 14, 30, 0, 0, time.UTC)
	provider := &fakeMailProvider{
		listIDs:  []string{"m1"},
		messages: map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Latest", Sender: "Ada <ada@example.com>", Date: when, Folder: "INBOX"}},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "people_brief",
		"config_path": configPath, "sphere": "work", "name": "Ada Example", "account_id": account.ID,
	})
	if err != nil {
		t.Fatalf("brain.people.brief: %v", err)
	}
	if got["person"] != "Ada Example" || got["person_path"] != "brain/people/Ada Example.md" {
		t.Fatalf("person = %#v / %#v", got["person"], got["person_path"])
	}
	if fm, _ := got["frontmatter"].(map[string]interface{}); fm["supervision_role"] != "postdoc co-advisor" {
		t.Fatalf("frontmatter = %#v", fm)
	}
	if bullets, _ := got["status_bullets"].([]peoplebrief.StatusBullet); len(bullets) != 3 || bullets[0].Date != "2026-04-22" {
		t.Fatalf("status_bullets = %#v", bullets)
	}
	loops, _ := got["open_loops"].(map[string][]peoplebrief.OpenLoop)
	if len(loops["waiting"]) != 1 || loops["waiting"][0].Path != "brain/gtd/wait.md" {
		t.Fatalf("open_loops[waiting] = %#v", loops["waiting"])
	}
	if meeting, _ := got["latest_meeting"].(*peoplebrief.Meeting); meeting == nil || meeting.Path != "brain/meetings/2026-04-29-standup.md" {
		t.Fatalf("latest_meeting = %#v", got["latest_meeting"])
	}
	if mail, _ := got["latest_mail"].(*peoplebrief.Mail); mail == nil || mail.MessageID != "m1" || mail.AccountID != account.ID {
		t.Fatalf("latest_mail = %#v", got["latest_mail"])
	}
}

func surfaceToolByName(t *testing.T, name string) surface.Tool {
	t.Helper()
	for _, tool := range surface.MCPTools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("surface tool %q not found", name)
	return surface.Tool{}
}
