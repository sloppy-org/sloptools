package mcp

import (
	"strings"
	"testing"
)

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
