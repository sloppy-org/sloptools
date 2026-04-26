package mcp

import "testing"

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
