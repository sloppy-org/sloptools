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
	case "mail_label_list", "mail_message_list", "mail_message_get", "mail_attachment_get", "mail_commitment_list":
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
		if name == "mail_commitment_list" {
			props["limit"] = map[string]interface{}{"type": "integer", "description": "Maximum messages to inspect. Use 5-10 for triage/counts; only request more when the user asks for breadth."}
			props["body_limit"] = map[string]interface{}{"type": "integer", "description": "Maximum number of matching messages whose full bodies may be fetched to confirm a commitment. Defaults to 5."}
		}
	}
}

func applyToolDefinitionDefaults(name string, def map[string]interface{}) {
	switch name {
	case "calendar_events":
		def["description"] = "List upcoming personal/work groupware calendar events. Compact by default: descriptions and attendee lists are omitted; use sphere plus limit 5-10 for triage/counts."
	case "mail_message_list":
		def["description"] = "List newest mail metadata without full bodies by default. Prefer sphere plus folder=INBOX and limit 5-10 for triage/counts; use mail_message_get for one chosen message body."
	case "mail_commitment_list":
		def["description"] = "Derive GTD commitments from mail messages without fetching every body. Prefer sphere plus limit 5-10 for triage/counts; use body_limit to bound confirmation fetches."
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
