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
	for _, name := range []string{"mail_label_list", "mail_message_list", "mail_message_get", "mail_attachment_get", "mail_commitment_list"} {
		schema, _ := names[name]["inputSchema"].(map[string]interface{})
		props, _ := schema["properties"].(map[string]interface{})
		if props["sphere"] == nil {
			t.Fatalf("%s schema lacks sphere property: %#v", name, props)
		}
		requiredFields, _ := schema["required"].([]string)
		for _, required := range requiredFields {
			if required == "account_id" {
				t.Fatalf("%s still requires account_id: %#v", name, schema["required"])
			}
		}
		if name == "mail_commitment_list" && props["body_limit"] == nil {
			t.Fatalf("%s schema lacks body_limit property: %#v", name, props)
		}
	}
	closeSchema, _ := names["mail_commitment_close"]["inputSchema"].(map[string]interface{})
	closeProps, _ := closeSchema["properties"].(map[string]interface{})
	if closeProps["writeable"] == nil {
		t.Fatalf("mail_commitment_close schema lacks writeable property: %#v", closeProps)
	}
	requiredFields, _ := closeSchema["required"].([]string)
	hasAccountID := false
	for _, required := range requiredFields {
		if required == "account_id" {
			hasAccountID = true
		}
	}
	if !hasAccountID {
		t.Fatalf("mail_commitment_close should require account_id for sync-back: %#v", closeSchema["required"])
	}
}

func TestToolDefinitionsAdvertiseCompactDefaults(t *testing.T) {
	defs := toolDefinitions()
	names := map[string]map[string]interface{}{}
	for _, def := range defs {
		name, _ := def["name"].(string)
		names[name] = def
	}
	calendarDesc, _ := names["calendar_events"]["description"].(string)
	if !strings.Contains(calendarDesc, "personal/work groupware calendar") ||
		!strings.Contains(calendarDesc, "limit 5-10") {
		t.Fatalf("calendar_events description should steer compact groupware routing: %q", calendarDesc)
	}
	mailDesc, _ := names["mail_message_list"]["description"].(string)
	if !strings.Contains(mailDesc, "folder=INBOX") ||
		!strings.Contains(mailDesc, "without full bodies") {
		t.Fatalf("mail_message_list description should steer compact mail routing: %q", mailDesc)
	}
	commitmentDesc, _ := names["mail_commitment_list"]["description"].(string)
	if !strings.Contains(commitmentDesc, "commitments") ||
		!strings.Contains(commitmentDesc, "body_limit") {
		t.Fatalf("mail_commitment_list description should mention bounded commitment derivation: %q", commitmentDesc)
	}
	closeDesc, _ := names["mail_commitment_close"]["description"].(string)
	if !strings.Contains(closeDesc, "writeable") {
		t.Fatalf("mail_commitment_close description should mention writeable sync-back: %q", closeDesc)
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

// TestGroupwareDocListsEveryMCPTool ensures the groupware doc inventory
// matches the code inventory. Tools in the Canvas/Handoff/Temp/Workspace/
// Items/Artifacts/Actors sections are out of scope for the groupware doc.
func TestGroupwareDocListsEveryMCPTool(t *testing.T) {
	docPath := "../../docs/groupware.md"
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read groupware doc: %v", err)
	}
	doc := string(data)

	// Extract tool names from the doc. The doc uses backtick-quoted names
	// like `mail_send`, `calendar_events`, etc. Only match names that
	// look like actual MCP tools (mail/contact/calendar/task/evernote/inbox prefix), not
	// parameter references like `task_id` or `contact_id`.
	docRe := regexp.MustCompile("`((?:(?:mail|contact|calendar|task|evernote)_[a-z][a-z0-9_]*|inbox\\.[a-z][a-z0-9_]*))`")
	docNames := make(map[string]bool)
	for _, m := range docRe.FindAllStringSubmatch(doc, -1) {
		docNames[m[1]] = true
	}

	// Build the set of groupware tool names from surface.MCPTools.
	// Out-of-scope prefixes (Canvas, Handoff, Temp, Workspace, Items,
	// Artifacts, Actors) are excluded.
	groupwarePrefixes := []string{
		"canvas_",
		"handoff.",
		"temp_file_",
		"workspace_",
		"item_",
		"artifact_",
		"actor_",
		"brain.",
		"brain_",
		"meeting.",
	}

	codeNames := make(map[string]bool)
	for _, tool := range surface.MCPTools {
		skip := false
		for _, prefix := range groupwarePrefixes {
			if strings.HasPrefix(tool.Name, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			codeNames[tool.Name] = true
		}
	}

	// Check that every code name appears in the doc.
	for name := range codeNames {
		if !docNames[name] {
			t.Errorf("tool %q in code but missing from doc", name)
		}
	}

	// Check that every doc name (that looks like a groupware tool) is in code.
	knownParams := map[string]bool{"account_id": true, "task_id": true, "contact_id": true, "calendar_id": true, "message_id": true, "event_id": true, "list_id": true, "filter_id": true, "draft_id": true, "attachment_id": true, "session_id": true, "handoff_id": true, "workspace_id": true, "artifact_id": true, "actor_id": true, "item_id": true, "source_account_id": true, "target_account_id": true, "target_folder": true}
	for name := range docNames {
		if knownParams[name] {
			continue
		}
		if !codeNames[name] {
			t.Errorf("name %q in doc but not a known tool", name)
		}
	}
}

func TestTaskMutationSurfaceExposesSourceMetadata(t *testing.T) {
	for _, name := range []string{"task_create", "task_update"} {
		tool := surfaceToolByName(t, name)
		for _, prop := range []string{"start_at", "follow_up_at", "deadline", "description", "section_id", "parent_id", "labels", "assignee_id"} {
			if _, ok := tool.Properties[prop]; !ok {
				t.Fatalf("%s missing property %s", name, prop)
			}
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

	got, err := s.callTool("brain.people.brief", map[string]interface{}{
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
