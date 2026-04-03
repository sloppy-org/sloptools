package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/krystophny/sloppy/internal/email"
	"github.com/krystophny/sloppy/internal/providerdata"
	"github.com/krystophny/sloppy/internal/store"
)

func TestMailHandoffLifecycle(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	bodyText := "Plain text summary"
	bodyHTML := "<p>Plain text summary</p>"
	firstDate := time.Date(2026, time.March, 20, 9, 30, 0, 0, time.UTC)
	secondDate := firstDate.Add(2 * time.Hour)
	provider := &fakeMailProvider{
		messages: map[string]*providerdata.EmailMessage{
			"m1": {
				ID:                "m1",
				ThreadID:          "thread-1",
				InternetMessageID: "<m1@example.test>",
				Subject:           "Quarterly review",
				Sender:            "Ada <ada@example.com>",
				Recipients:        []string{"team@example.com", "ops@example.com"},
				Date:              firstDate,
				Snippet:           "Summary",
				Labels:            []string{"Inbox", "Important"},
				IsRead:            true,
				IsFlagged:         true,
				BodyText:          &bodyText,
				BodyHTML:          &bodyHTML,
				Attachments: []providerdata.Attachment{{
					ID:       "att-1",
					Filename: "report.pdf",
					MimeType: "application/pdf",
					Size:     128,
				}},
			},
			"m2": {
				ID:                "m2",
				ThreadID:          "thread-2",
				InternetMessageID: "<m2@example.test>",
				Subject:           "Follow-up",
				Sender:            "Grace <grace@example.com>",
				Recipients:        []string{"team@example.com"},
				Date:              secondDate,
				Snippet:           "Next steps",
			},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	created, err := s.callTool("handoff.create", map[string]interface{}{
		"kind": "mail",
		"selector": map[string]interface{}{
			"account_id":  account.ID,
			"message_ids": []interface{}{"m1", "m2"},
		},
		"policy": map[string]interface{}{
			"max_consumes": 2,
		},
	})
	if err != nil {
		t.Fatalf("handoff.create failed: %v", err)
	}
	createdMap := normalizeMap(t, created)
	handoffID := stringValue(t, createdMap["handoff_id"])
	if handoffID == "" {
		t.Fatal("handoff_id = empty")
	}
	if got := stringValue(t, createdMap["kind"]); got != handoffKindMail {
		t.Fatalf("kind = %q", got)
	}
	meta := mapValue(t, createdMap["meta"])
	if got := intValue(t, meta["message_count"]); got != 2 {
		t.Fatalf("message_count = %d", got)
	}
	if got := stringSliceValue(t, meta["message_ids"]); len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("message_ids = %#v", got)
	}
	if got := stringSliceValue(t, meta["subjects"]); len(got) != 2 || got[0] != "Quarterly review" || got[1] != "Follow-up" {
		t.Fatalf("subjects = %#v", got)
	}
	if got := stringSliceValue(t, meta["senders"]); len(got) != 2 || got[0] != "Ada <ada@example.com>" || got[1] != "Grace <grace@example.com>" {
		t.Fatalf("senders = %#v", got)
	}
	policySummary := mapValue(t, createdMap["policy_summary"])
	if got := intValue(t, policySummary["remaining_consumes"]); got != 2 {
		t.Fatalf("remaining_consumes = %d", got)
	}

	peeked, err := s.callTool("handoff.peek", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.peek failed: %v", err)
	}
	peekMap := normalizeMap(t, peeked)
	if _, ok := peekMap["payload"]; ok {
		t.Fatalf("handoff.peek payload = %#v, want none", peekMap["payload"])
	}

	consumed, err := s.callTool("handoff.consume", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.consume failed: %v", err)
	}
	consumeMap := normalizeMap(t, consumed)
	payload := mapValue(t, consumeMap["payload"])
	messages := sliceValue(t, payload["messages"])
	if len(messages) != 2 {
		t.Fatalf("payload.messages len = %d", len(messages))
	}
	firstMessage := mapValue(t, messages[0])
	if got := stringValue(t, firstMessage["message_id"]); got != "m1" {
		t.Fatalf("message_id = %q", got)
	}
	if got := stringValue(t, firstMessage["subject"]); got != "Quarterly review" {
		t.Fatalf("subject = %q", got)
	}
	if got := stringValue(t, firstMessage["sender"]); got != "Ada <ada@example.com>" {
		t.Fatalf("sender = %q", got)
	}
	if got := stringSliceValue(t, firstMessage["recipients"]); len(got) != 2 || got[0] != "team@example.com" || got[1] != "ops@example.com" {
		t.Fatalf("recipients = %#v", got)
	}
	if got := stringValue(t, firstMessage["date"]); got != firstDate.Format(time.RFC3339) {
		t.Fatalf("date = %q", got)
	}
	if got := stringValue(t, firstMessage["body_text"]); got != bodyText {
		t.Fatalf("body_text = %q", got)
	}
	attachments := sliceValue(t, firstMessage["attachments"])
	if len(attachments) != 1 {
		t.Fatalf("attachments len = %d", len(attachments))
	}
	policyState := mapValue(t, consumeMap["policy"])
	if got := intValue(t, policyState["consumed_count"]); got != 1 {
		t.Fatalf("consumed_count = %d", got)
	}
	if got := intValue(t, policyState["remaining_consumes"]); got != 1 {
		t.Fatalf("remaining_consumes = %d", got)
	}

	status, err := s.callTool("handoff.status", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.status failed: %v", err)
	}
	statusMap := normalizeMap(t, status)
	statusPolicy := mapValue(t, statusMap["policy_summary"])
	if got := intValue(t, statusPolicy["consumed_count"]); got != 1 {
		t.Fatalf("status consumed_count = %d", got)
	}

	revoked, err := s.callTool("handoff.revoke", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		t.Fatalf("handoff.revoke failed: %v", err)
	}
	revokedMap := normalizeMap(t, revoked)
	if !boolValue(t, revokedMap["revoked"]) {
		t.Fatalf("revoked = %#v", revokedMap["revoked"])
	}
	if _, err := s.callTool("handoff.consume", map[string]interface{}{"handoff_id": handoffID}); err == nil {
		t.Fatal("handoff.consume after revoke error = nil")
	}
}

func TestMailHandoffConsumeLimit(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeMailProvider{
		messages: map[string]*providerdata.EmailMessage{
			"m1": {
				ID:      "m1",
				Subject: "Only once",
				Sender:  "ada@example.com",
				Date:    time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC),
			},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	created, err := s.callTool("handoff.create", map[string]interface{}{
		"kind": "mail",
		"selector": map[string]interface{}{
			"account_id": account.ID,
			"message_id": "m1",
		},
	})
	if err != nil {
		t.Fatalf("handoff.create failed: %v", err)
	}
	handoffID := stringValue(t, normalizeMap(t, created)["handoff_id"])
	if _, err := s.callTool("handoff.consume", map[string]interface{}{"handoff_id": handoffID}); err != nil {
		t.Fatalf("first handoff.consume failed: %v", err)
	}
	_, err = s.callTool("handoff.consume", map[string]interface{}{"handoff_id": handoffID})
	if err == nil {
		t.Fatal("second handoff.consume error = nil")
	}
	if got := err.Error(); got != "handoff has no remaining consumes" {
		t.Fatalf("error = %q", got)
	}
}

func TestHandoffToolDefinitions(t *testing.T) {
	defs := toolDefinitions()
	names := map[string]map[string]interface{}{}
	for _, def := range defs {
		name, _ := def["name"].(string)
		names[name] = def
	}
	for _, name := range []string{"handoff.create", "handoff.peek", "handoff.consume", "handoff.revoke", "handoff.status"} {
		if names[name] == nil {
			t.Fatalf("%s missing from tool definitions", name)
		}
	}
	schema, _ := names["handoff.create"]["inputSchema"].(map[string]interface{})
	props, _ := schema["properties"].(map[string]interface{})
	if props["selector"] == nil || props["policy"] == nil {
		t.Fatalf("handoff.create properties = %#v", props)
	}
}

func normalizeMap(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return out
}

func mapValue(t *testing.T, value interface{}) map[string]interface{} {
	t.Helper()
	typed, ok := value.(map[string]interface{})
	if !ok {
		t.Fatalf("value = %#v, want map", value)
	}
	return typed
}

func sliceValue(t *testing.T, value interface{}) []interface{} {
	t.Helper()
	typed, ok := value.([]interface{})
	if !ok {
		t.Fatalf("value = %#v, want slice", value)
	}
	return typed
}

func stringValue(t *testing.T, value interface{}) string {
	t.Helper()
	typed, ok := value.(string)
	if !ok {
		t.Fatalf("value = %#v, want string", value)
	}
	return typed
}

func stringSliceValue(t *testing.T, value interface{}) []string {
	t.Helper()
	items := sliceValue(t, value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("item = %#v, want string", item)
		}
		out = append(out, text)
	}
	return out
}

func intValue(t *testing.T, value interface{}) int {
	t.Helper()
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		t.Fatalf("value = %#v, want number", value)
		return 0
	}
}

func boolValue(t *testing.T, value interface{}) bool {
	t.Helper()
	typed, ok := value.(bool)
	if !ok {
		t.Fatalf("value = %#v, want bool", value)
	}
	return typed
}
