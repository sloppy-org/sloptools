package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "brain.gtd.tracks", Description: "Return canonical GTD attention labels of the form track/<name> for one sphere, including configured wip_limit and WIP status when gtd.toml provides limits.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"sphere":     {Type: "string", Description: "Vault sphere: work or private.", Enum: []string{"work", "private"}},
			"gtd_config": {Type: "string", Description: "Optional GTD config path with per-track wip_limit. Defaults to ~/.config/sloptools/gtd.toml."},
		}},
		Tool{Name: "brain.gtd.focus", Description: "Get or update the active GTD attention-label focus stored by sloptools.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"sphere":         {Type: "string", Description: "Vault sphere: work or private.", Enum: []string{"work", "private"}},
			"track":          {Type: "string", Description: "Active attention label value without the track/ prefix."},
			"clear_track":    {Type: "boolean", Description: "Clear active track and remembered project/action."},
			"project_source": {Type: "string", Description: "Canonical source provider for the active project item."},
			"project_ref":    {Type: "string", Description: "Canonical source reference for the active project item."},
			"project_path":   {Type: "string", Description: "Markdown path for the active project item; implies source=markdown when source/ref are omitted."},
			"clear_project":  {Type: "boolean", Description: "Clear remembered project and action for this track."},
			"action_source":  {Type: "string", Description: "Canonical source provider for the active action item."},
			"action_ref":     {Type: "string", Description: "Canonical source reference for the active action item."},
			"action_path":    {Type: "string", Description: "Markdown path for the active action item; implies source=markdown when source/ref are omitted."},
			"clear_action":   {Type: "boolean", Description: "Clear remembered action for this track."},
		}},
	)
}
