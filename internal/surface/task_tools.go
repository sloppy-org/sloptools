package surface

func init() {
	for i := range MCPTools {
		switch MCPTools[i].Name {
		case "task_create", "task_update":
			ensureTaskMutationProperties(MCPTools[i].Properties)
		}
	}
}

func ensureTaskMutationProperties(props map[string]ToolProperty) {
	props["start_at"] = ToolProperty{Type: "string", Description: "Optional defer/start timestamp as RFC3339 or YYYY-MM-DD."}
	props["follow_up_at"] = ToolProperty{Type: "string", Description: "Alias for start_at for GTD follow-up timing."}
	props["deadline"] = ToolProperty{Type: "string", Description: "Alias for due; hard deadline as RFC3339 or YYYY-MM-DD."}
	props["description"] = ToolProperty{Type: "string", Description: "Optional provider-native description/body."}
	props["section_id"] = ToolProperty{Type: "string", Description: "Optional provider section id, supported by Todoist."}
	props["parent_id"] = ToolProperty{Type: "string", Description: "Optional provider parent task id, supported by Todoist."}
	props["labels"] = ToolProperty{Type: "array", Description: "Optional provider labels/tags, supported by Todoist."}
	props["assignee_id"] = ToolProperty{Type: "string", Description: "Optional provider assignee id, supported by Todoist."}
}
