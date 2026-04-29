package surface

func init() {
	MCPTools = append(MCPTools, Tool{
		Name:        "mail_commitment_list",
		Description: "Derive GTD commitments from mail messages and linked email artifacts. Use body_limit to bound confirmation fetches.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter used when account_id is omitted.",
				Enum:        []string{"work", "private"},
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of messages to inspect.",
			},
			"body_limit": {
				Type:        "integer",
				Description: "Optional maximum number of matching messages whose full bodies may be fetched. Defaults to 5.",
			},
			"project_config": {
				Type:        "string",
				Description: "Optional path to per-user project matching rules. Defaults to ~/.config/sloptools/projects.toml when present.",
			},
			"vault_config": {
				Type:        "string",
				Description: "Optional vault config path used for person-note diagnostics. Defaults to ~/.config/sloptools/vaults.toml when present.",
			},
			"writeable": {
				Type:        "boolean",
				Description: "When true, returned source bindings opt into upstream sync-back.",
			},
		},
	})
	MCPTools = append(MCPTools, Tool{
		Name:        "mail_commitment_close",
		Description: "Close a writeable mail-bound commitment by applying an upstream mail action.",
		Required:    []string{"account_id", "message_id", "writeable"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"message_id": {
				Type:        "string",
				Description: "Provider message id from the commitment source binding.",
			},
			"writeable": {
				Type:        "boolean",
				Description: "Must be true, copied from the source binding.",
			},
			"action": {
				Type:        "string",
				Description: "Mail action to apply. Defaults to archive.",
				Enum:        []string{"archive", "trash", "mark_read", "move_to_folder", "archive_label"},
			},
			"folder": {
				Type:        "string",
				Description: "Target folder for move_to_folder.",
			},
			"label": {
				Type:        "string",
				Description: "Label or archive bucket for label actions.",
			},
		},
	})
}
