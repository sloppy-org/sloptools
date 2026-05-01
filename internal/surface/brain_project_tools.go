package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "brain.projects.render", Description: "Render a project hub's Open Loops section from live commitments linked to that hub.", Required: []string{"sphere", "hub"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to update.", Enum: []string{"work", "private"}},
			"hub":         {Type: "string", Description: "Project hub note path, for example brain/projects/Name.md."},
			"path":        {Type: "string", Description: "Alias for hub."},
		}},
		Tool{Name: "brain.projects.list", Description: "Return per-project hub commitment counts for next, waiting, and closed items.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
		}},
		Tool{Name: "brain.gtd.bulk_link", Description: "Apply per-user project rules to unlinked commitments and report linked, skipped, and ambiguous matches.", Required: []string{"sphere", "rules"}, Properties: map[string]ToolProperty{
			"config_path":    {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":         {Type: "string", Description: "Vault sphere to update.", Enum: []string{"work", "private"}},
			"rules":          {Type: "string", Description: "Path to TOML project rules using [project.<key>] tables."},
			"rules_path":     {Type: "string", Description: "Alias for rules."},
			"project_config": {Type: "string", Description: "Alias for rules."},
		}},
	)
}
