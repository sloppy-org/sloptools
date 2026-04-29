package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "brain.note.parse", Description: "Parse a brain note from the configured work/private vault and return structured content plus source-path metadata.", Required: []string{"sphere", "path"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
			"path":        {Type: "string", Description: "Note path, relative to the brain root or absolute inside the vault."},
		}},
		Tool{Name: "brain.note.validate", Description: "Validate a brain note from the configured work/private vault and return structured diagnostics plus source-path metadata.", Required: []string{"sphere", "path"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
			"path":        {Type: "string", Description: "Note path, relative to the brain root or absolute inside the vault."},
		}},
		Tool{Name: "brain.vault.validate", Description: "Validate every Markdown brain note in a configured vault and return diagnostics with source paths.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
		}},
		Tool{Name: "brain.links.resolve", Description: "Resolve a link safely inside a configured brain vault and return the resolved source path.", Required: []string{"sphere", "path", "link"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
			"path":        {Type: "string", Description: "Source note path, relative to the brain root or absolute inside the vault."},
			"link":        {Type: "string", Description: "Link text to resolve."},
		}},
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
