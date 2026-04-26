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
	for _, name := range []string{"mail_label_list", "mail_message_list", "mail_message_get", "mail_attachment_get"} {
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
}
