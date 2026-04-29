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
	)
}
