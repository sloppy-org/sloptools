package surface

func init() {
	MCPTools = append(MCPTools,
		Tool{Name: "brain.meeting.kickoff", Description: "Pre-meeting helper for the §5 group-sync format: read the date-keyed Zulip <date> sync topic, cluster posts into 2-4 breakouts plus pair-off-cycle candidates, and emit a draft frame with questions and decisions from the prior meeting note.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"config_path":     {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sources_config":  {Type: "string", Description: "Optional sources/meetings config path. Defaults to ~/.config/sloptools/sources.toml."},
			"sphere":          {Type: "string", Description: "Vault sphere whose meetings + zulip credentials apply.", Enum: []string{"work", "private"}},
			"meeting_id":      {Type: "string", Description: "Optional meeting series id (e.g. 'plasma-orga') configured under [meetings.<sphere>.meeting_series]. Resolves to a Zulip stream + topic format when stream/topic are not supplied."},
			"stream":          {Type: "string", Description: "Zulip stream name. Required when meeting_id does not resolve via config."},
			"topic":           {Type: "string", Description: "Zulip topic name. Defaults to the configured topic_format with {date} replaced by the cutoff date."},
			"cutoff":          {Type: "string", Description: "Meeting start time. Accepts RFC3339, YYYY-MM-DDTHH:MM, or YYYY-MM-DD. Defaults to now (UTC)."},
			"window":          {Type: "string", Description: "Look-back duration as a Go duration string (e.g. 24h). Defaults to 24h per §5."},
			"questions":       {Type: "array", Description: "Optional 1-2 frame questions to surface in the opening block."},
			"prior_note_path": {Type: "string", Description: "Optional vault-relative Markdown path to the previous meeting note. Decisions under '## Decisions' are echoed in the frame."},
		}},
	)
}
