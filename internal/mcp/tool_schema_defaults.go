package mcp

func applyToolSchemaDefaults(name string, schema map[string]interface{}) {
	switch name {
	case "calendar_events":
		props, _ := schema["properties"].(map[string]interface{})
		if props == nil {
			props = map[string]interface{}{}
			schema["properties"] = props
		}
		props["limit"] = map[string]interface{}{"type": "integer", "description": "Maximum events to return. Use 5-10 for triage/counts; only request more when the user asks for breadth."}
		props["days"] = map[string]interface{}{"type": "integer", "description": "Days forward from now. Use 7 for upcoming-week summaries."}
	case "mail_label_list", "mail_message_list", "mail_message_get", "mail_attachment_get":
		removeRequired(schema, "account_id")
		props, _ := schema["properties"].(map[string]interface{})
		if props == nil {
			props = map[string]interface{}{}
			schema["properties"] = props
		}
		props["account_id"] = map[string]interface{}{"type": "integer", "description": "Optional external account id. Defaults to the first enabled email account for the sphere."}
		props["sphere"] = map[string]interface{}{"type": "string", "description": "Optional work/private account filter used when account_id is omitted.", "enum": []string{"work", "private"}}
		if name == "mail_message_list" {
			props["folder"] = map[string]interface{}{"type": "string", "description": "Folder or label scope. Use INBOX for recent inbox triage."}
			props["limit"] = map[string]interface{}{"type": "integer", "description": "Maximum messages to return. Use 5-10 for triage/counts; only request more when the user asks for breadth."}
			props["include_body"] = map[string]interface{}{"type": "boolean", "description": "Include full message bodies. Defaults to false; prefer mail_message_get for one chosen message."}
		}
	}
}

func applyToolDefinitionDefaults(name string, def map[string]interface{}) {
	switch name {
	case "calendar_events":
		def["description"] = "List upcoming personal/work groupware calendar events. Compact by default: descriptions and attendee lists are omitted; use sphere plus limit 5-10 for triage/counts."
	case "mail_message_list":
		def["description"] = "List newest mail metadata without full bodies by default. Prefer sphere plus folder=INBOX and limit 5-10 for triage/counts; use mail_message_get for one chosen message body."
	}
}

func removeRequired(schema map[string]interface{}, field string) {
	required, _ := schema["required"].([]string)
	if len(required) == 0 {
		return
	}
	filtered := required[:0]
	for _, item := range required {
		if item != field {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		delete(schema, "required")
		return
	}
	schema["required"] = filtered
}
