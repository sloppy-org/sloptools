package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "brain.people.dashboard", Description: "Return per-person GTD open loops split into waiting on them, owed to them, and recently closed buckets.", Required: []string{"sphere", "name"}, Properties: map[string]ToolProperty{
			"config_path":  {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":       {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
			"name":         {Type: "string", Description: "Person name, resolved against brain/people notes."},
			"recent_limit": {Type: "integer", Description: "Maximum recently closed commitments to return. Defaults to 10."},
		}},
		Tool{Name: "brain.people.render", Description: "Render the per-person open-loop dashboard into the person's Current open loops section.", Required: []string{"sphere", "name"}, Properties: map[string]ToolProperty{
			"config_path":  {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":       {Type: "string", Description: "Vault sphere to update.", Enum: []string{"work", "private"}},
			"name":         {Type: "string", Description: "Person name, resolved against brain/people notes."},
			"recent_limit": {Type: "integer", Description: "Maximum recently closed commitments to render. Defaults to 10."},
		}},
		Tool{Name: "brain.people.brief", Description: "Assemble a one-screen pre-meeting person brief: frontmatter, last dated status bullets, open commitments by relationship, latest meeting note, and latest mail thread. Pure read; never writes.", Required: []string{"sphere", "name"}, Properties: map[string]ToolProperty{
			"config_path":    {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":         {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
			"name":           {Type: "string", Description: "Person name, resolved against brain/people notes."},
			"status_section": {Type: "string", Description: "Optional H2 section name to scan for dated bullets. Defaults to 'Recent context' with sensible fallbacks."},
			"status_limit":   {Type: "integer", Description: "Maximum dated status bullets to return, newest first. Defaults to 3."},
			"email":          {Type: "string", Description: "Optional override email address used to look up the latest mail thread. Defaults to the person note's email."},
			"account_id":     {Type: "integer", Description: "Optional mail account id. Defaults to the first enabled email account in the sphere."},
		}},
		Tool{Name: "brain.people.monthly_index", Description: "Derive monthly journal index pages from `## Log` bullets in brain/people, brain/projects, and brain/topics notes. Writes brain/journal/<YYYY-MM>.md files; idempotent.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to derive indexes for.", Enum: []string{"work", "private"}},
			"dry_run":     {Type: "boolean", Description: "If true, count writes but do not modify any files."},
		}},
	)
}
