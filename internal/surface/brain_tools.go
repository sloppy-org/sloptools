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
		Tool{Name: "brain.gtd.bind", Description: "Attach source bindings to a GTD commitment or collapse same-outcome commitments into one local aggregate overlay.", Required: []string{"sphere", "winner_path"}, Properties: map[string]ToolProperty{
			"config_path":     {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":          {Type: "string", Description: "Vault sphere to update.", Enum: []string{"work", "private"}},
			"winner_path":     {Type: "string", Description: "Commitment path whose outcome and local overlay survive."},
			"paths":           {Type: "array", Description: "Commitment paths to bind. Same-outcome non-winners become equivalent to winner."},
			"outcome":         {Type: "string", Description: "Optional winning outcome text."},
			"source_bindings": {Type: "array", Description: "Optional source binding objects with provider/ref to attach to the winner."},
		}},
		Tool{Name: "brain.gtd.dedup_scan", Description: "Reconcile GTD commitments by stable source binding and return non-destructive duplicate review candidates.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"config_path":             {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":                  {Type: "string", Description: "Vault sphere to scan.", Enum: []string{"work", "private"}},
			"dedup_config":            {Type: "string", Description: "Optional per-user TOML with dedup thresholds and local LLM command."},
			"deterministic_threshold": {Type: "number", Description: "Optional deterministic candidate threshold."},
			"llm_threshold":           {Type: "number", Description: "Optional deterministic score threshold before local LLM review."},
			"candidate_threshold":     {Type: "number", Description: "Optional LLM confidence threshold for review candidates."},
			"llm_command":             {Type: "string", Description: "Optional local command that reads a JSON prompt on stdin and emits JSON."},
		}},
		Tool{Name: "brain.gtd.dedup_review_apply", Description: "Apply a user dedup decision without mutating upstream sources.", Required: []string{"sphere", "id", "decision"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to update.", Enum: []string{"work", "private"}},
			"id":          {Type: "string", Description: "Candidate id returned by brain.gtd.dedup_scan."},
			"decision":    {Type: "string", Description: "Review decision.", Enum: []string{"merge", "keep_separate", "defer"}},
			"winner_path": {Type: "string", Description: "For merge, commitment path whose outcome text survives."},
			"outcome":     {Type: "string", Description: "For merge, optional winning outcome text."},
		}},
		Tool{Name: "brain.gtd.dedup_history", Description: "Return durable GTD dedup decisions stored in commitment front matter.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"config_path": {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":      {Type: "string", Description: "Vault sphere to inspect.", Enum: []string{"work", "private"}},
		}},
		Tool{Name: "brain.gtd.sync", Description: "Reconcile writeable GTD source bindings by pushing closed local overlays upstream or pulling periodic upstream closure state down.", Required: []string{"sphere"}, Properties: map[string]ToolProperty{
			"config_path":     {Type: "string", Description: "Optional vault config path. Defaults to ~/.config/sloptools/vaults.toml."},
			"sphere":          {Type: "string", Description: "Vault sphere to sync.", Enum: []string{"work", "private"}},
			"path":            {Type: "string", Description: "Optional commitment note path. When omitted, every commitment in the sphere is scanned."},
			"commitment_id":   {Type: "string", Description: "Alias for path."},
			"periodic":        {Type: "boolean", Description: "When true, read upstream state and pull closed state into the local overlay."},
			"dry_run":         {Type: "boolean", Description: "Report actions without applying upstream or Markdown writes."},
			"mail_action":     {Type: "string", Description: "Mail action for closed mail bindings. Defaults to archive."},
			"mail_label":      {Type: "string", Description: "Optional label for label-based mail actions."},
			"mail_folder":     {Type: "string", Description: "Optional folder for folder-based mail actions."},
			"todoist_list_id": {Type: "string", Description: "Optional Todoist project/list id when the binding ref does not include one."},
		}},
	)
}
