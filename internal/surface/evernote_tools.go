package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "evernote_notebook_list", Description: "List Evernote notebooks from a configured Evernote account.", Properties: map[string]ToolProperty{
			"account_id": {Type: "integer", Description: "Optional Evernote account id for sphere."},
			"sphere":     {Type: "string", Description: "Optional legacy work/private top-level context filter.", Enum: []string{"work", "private"}},
		}},
		Tool{Name: "evernote_note_search", Description: "Search Evernote notes without mutating them.", Properties: map[string]ToolProperty{
			"account_id":    {Type: "integer", Description: "Optional Evernote account id for sphere."},
			"sphere":        {Type: "string", Description: "Optional legacy work/private top-level context filter.", Enum: []string{"work", "private"}},
			"notebook_id":   {Type: "string", Description: "Optional notebook id."},
			"query":         {Type: "string", Description: "Optional free-text query."},
			"tag":           {Type: "string", Description: "Optional tag filter."},
			"updated_after": {Type: "string", Description: "Optional provider timestamp lower bound."},
			"limit":         {Type: "integer", Description: "Maximum notes to return, capped at 50."},
		}},
		Tool{Name: "evernote_note_get", Description: "Fetch one Evernote note as text, Markdown, and parsed checkbox tasks.", Required: []string{"id"}, Properties: map[string]ToolProperty{
			"account_id": {Type: "integer", Description: "Optional Evernote account id for sphere."},
			"sphere":     {Type: "string", Description: "Optional legacy work/private top-level context filter.", Enum: []string{"work", "private"}},
			"id":         {Type: "string", Description: "Evernote note id."},
		}},
	)
}
