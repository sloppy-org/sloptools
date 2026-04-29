package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "brain_search", Description: "Search a configured brain vault with rg-backed exact, regex, link, or alias matching.", Required: []string{"sphere", "query"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to search.", Enum: []string{"work", "private"}},
			"query":       {Type: "string", Description: "Search query."},
			"mode":        {Type: "string", Description: "Search mode.", Enum: []string{"text", "regex", "wikilink", "markdown_link", "alias"}},
			"limit":       {Type: "integer", Description: "Maximum results to return. Defaults to 50."},
		}},
		Tool{Name: "brain_backlinks", Description: "Find Markdown and wikilink backlinks to a note in a configured brain vault.", Required: []string{"sphere", "target"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to search.", Enum: []string{"work", "private"}},
			"target":      {Type: "string", Description: "Target note path, relative to the brain root or absolute inside the vault."},
			"limit":       {Type: "integer", Description: "Maximum results to return. Defaults to 50."},
		}},
	)
}
