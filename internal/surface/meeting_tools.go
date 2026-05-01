package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "meeting.summary.draft", Description: "Render per-recipient meeting summary email drafts from a parsed MEETING_NOTES.md. One draft per non-owner attendee, containing the Decisions list and the recipient's Action Checklist tasks plus a share link.", Required: []string{"sphere", "slug"}, Properties: map[string]ToolProperty{
			"config_path":    {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sources_config": {Type: "string", Description: "Optional sources/meetings config path. Defaults to ~/.config/sloptools/sources.toml."},
			"sphere":         {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
			"slug":           {Type: "string", Description: "Meeting slug (folder under meetings_root or loose file name without .md)."},
			"recipient":      {Type: "string", Description: "Optional single recipient. When omitted, drafts are emitted for every non-owner attendee."},
		}},
		Tool{Name: "meeting.summary.send", Description: "Render and create a real mail provider draft for one recipient via mail_send draft_only=true. Never auto-sends.", Required: []string{"sphere", "slug", "recipient"}, Properties: map[string]ToolProperty{
			"config_path":    {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sources_config": {Type: "string", Description: "Optional sources/meetings config path. Defaults to ~/.config/sloptools/sources.toml."},
			"sphere":         {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
			"slug":           {Type: "string", Description: "Meeting slug (folder under meetings_root or loose file name without .md)."},
			"recipient":      {Type: "string", Description: "Recipient name as it appears in the Action Checklist or Attendees list."},
			"to":             {Type: "string", Description: "Optional explicit recipient email. Overrides resolver output."},
			"account_id":     {Type: "integer", Description: "Optional mail account id; defaults to [meetings.<sphere>].mail_account_id."},
			"send_now":       {Type: "boolean", Description: "When true, send immediately instead of saving as a draft. Default false."},
		}},
		Tool{Name: "meeting.share.create", Description: "Persist the public share metadata for a meeting note. The actual Nextcloud share is created via helpy nextcloud_share_create; this verb records the URL so meeting.summary.draft can embed it.", Required: []string{"sphere", "slug"}, Properties: map[string]ToolProperty{
			"config_path":    {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sources_config": {Type: "string", Description: "Optional sources/meetings config path. Defaults to ~/.config/sloptools/sources.toml."},
			"sphere":         {Type: "string", Description: "Vault sphere to update.", Enum: []string{"work", "private"}},
			"slug":           {Type: "string", Description: "Meeting slug."},
			"url":            {Type: "string", Description: "Public share URL produced by helpy nextcloud_share_create."},
			"token":          {Type: "string", Description: "Optional share token returned by Nextcloud."},
			"permissions":    {Type: "string", Description: "Share permissions. Defaults to the per-sphere config or edit.", Enum: []string{"edit", "read", "comment"}},
			"expiry_days":    {Type: "integer", Description: "Optional expiry window in days."},
			"password":       {Type: "boolean", Description: "Whether the share is password-protected."},
		}},
		Tool{Name: "meeting.share.revoke", Description: "Remove the persisted public share metadata for a meeting. Live revocation via helpy must be performed by the caller.", Required: []string{"sphere", "slug"}, Properties: map[string]ToolProperty{
			"config_path":    {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sources_config": {Type: "string", Description: "Optional sources/meetings config path. Defaults to ~/.config/sloptools/sources.toml."},
			"sphere":         {Type: "string", Description: "Vault sphere to update.", Enum: []string{"work", "private"}},
			"slug":           {Type: "string", Description: "Meeting slug."},
		}},
	)
}
