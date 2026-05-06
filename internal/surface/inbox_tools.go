package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{
			Name:        "inbox.source_list",
			Description: "List configured capture inbox sources. Google Tasks INBOX and bare files in vault INBOX folders are active.",
			Properties: map[string]ToolProperty{
				"sphere": {Type: "string", Description: "Optional sphere filter.", Enum: []string{"work", "private"}},
			},
		},
		Tool{
			Name:        "inbox.item_list",
			Description: "List items in one capture inbox source without acknowledging them.",
			Properties: map[string]ToolProperty{
				"source_id":   {Type: "string", Description: "Optional source id from inbox.source_list."},
				"account_id":  {Type: "integer", Description: "Optional task account id. Defaults to the first tasks-capable account for sphere."},
				"sphere":      {Type: "string", Description: "Optional sphere filter.", Enum: []string{"work", "private"}},
				"list_id":     {Type: "string", Description: "Optional task list id. Defaults to a list named INBOX, then primary."},
				"config_path": {Type: "string", Description: "Optional brain vault config path for file inbox sources."},
				"limit":       {Type: "integer", Description: "Maximum items to return. Defaults to 20."},
			},
		},
		Tool{
			Name:        "inbox.item_plan",
			Description: "Classify one capture item and return the proposed canonical target plus acknowledge action. Does not mutate source.",
			Required:    []string{"id"},
			Properties: map[string]ToolProperty{
				"source_id":   {Type: "string", Description: "Optional source id from inbox.source_list."},
				"account_id":  {Type: "integer", Description: "Optional task account id. Defaults to the first tasks-capable account for sphere."},
				"sphere":      {Type: "string", Description: "Optional sphere filter.", Enum: []string{"work", "private"}},
				"list_id":     {Type: "string", Description: "Optional task list id. Defaults to a list named INBOX, then primary."},
				"id":          {Type: "string", Description: "Provider item id."},
				"context":     {Type: "string", Description: "Optional user context for classification."},
				"config_path": {Type: "string", Description: "Optional brain vault config path for file inbox sources."},
			},
		},
		Tool{
			Name:        "inbox.item_ack",
			Description: "Acknowledge one capture item after a canonical target has been written and validated.",
			Required:    []string{"id", "target_ref"},
			Properties: map[string]ToolProperty{
				"source_id":   {Type: "string", Description: "Optional source id from inbox.source_list."},
				"account_id":  {Type: "integer", Description: "Optional task account id. Defaults to the first tasks-capable account for sphere."},
				"sphere":      {Type: "string", Description: "Optional sphere filter.", Enum: []string{"work", "private"}},
				"list_id":     {Type: "string", Description: "Optional task list id. Defaults to a list named INBOX, then primary."},
				"id":          {Type: "string", Description: "Provider item id."},
				"target_ref":  {Type: "string", Description: "Canonical target reference that was written and validated."},
				"target_path": {Type: "string", Description: "For file inbox sources, vault-relative destination path outside INBOX."},
				"config_path": {Type: "string", Description: "Optional brain vault config path for file inbox sources."},
			},
		},
	)
}
