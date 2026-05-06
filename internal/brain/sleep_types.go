package brain

// Types and function signatures for the brain "sleep cycle":
// link-aware moves, cleanup, consolidation (Phase 6), dreaming (Phase 7).
// Bodies live in movenote.go, cleanup.go, consolidate.go, dream.go.

// MovePlan describes a planned move and the link rewrites it implies across
// every configured vault. Plan is canonicalized and digested so that an
// apply step can refuse to run if the world changed since the dry-run.
//
// MergeTarget is set only by PlanMerge. When set, the plan deletes the
// loser file (To == "/dev/null") and rewrites every inbound `[[loser]]`
// reference to `[[survivor]]` instead of stripping the wikilink. ApplyMove
// dispatches to PlanMerge for re-derivation when MergeTarget is non-empty.
type MovePlan struct {
	Sphere      Sphere     `json:"sphere"`
	From        string     `json:"from"`                   // vault-relative source
	To          string     `json:"to"`                     // vault-relative dest; "/dev/null" means delete
	MergeTarget string     `json:"merge_target,omitempty"` // consolidate redirect target (loser->survivor)
	Files       []FileMove `json:"files"`                  // files/dirs that move
	Edits       []LinkEdit `json:"edits"`                  // wikilink/markdown rewrites in non-moved files
	Inner       []LinkEdit `json:"inner_edits"`            // relative-link rewrites inside moved files
	Digest      string     `json:"digest"`                 // sha256 over canonicalized (Files,Edits,Inner)
	Notes       []string   `json:"notes,omitempty"`        // warnings: inbound links to a delete, etc
}

// LinkEdit is a single line edit to apply.
type LinkEdit struct {
	Path    string `json:"path"`     // vault-relative path of file to edit
	Sphere  Sphere `json:"sphere"`   // which vault the edited file lives in
	Line    int    `json:"line"`     // 1-based line number
	OldText string `json:"old_text"` // exact existing line
	NewText string `json:"new_text"` // replacement line
	Kind    string `json:"kind"`     // "wikilink" | "markdown" | "frontmatter"
}

// FileMove is a single rename. IsDir is true when moving a directory.
type FileMove struct {
	From  string `json:"from"`
	To    string `json:"to"`
	IsDir bool   `json:"is_dir"`
}

// DeadDirCandidate is a directory that the cleanup scanner proposes to delete.
type DeadDirCandidate struct {
	Sphere     Sphere `json:"sphere"`
	Path       string `json:"path"`
	Reason     string `json:"reason"`     // "svn" | "empty" | "old-with-live-sibling" | "bak-with-live-sibling" | "pycache" | "node-modules"
	Confidence string `json:"confidence"` // "high" | "medium"
	Inbound    int    `json:"inbound"`    // count of inbound brain wikilinks; >0 demotes to medium
}

// ConsolidateOutcome enumerates the proposed actions for the Phase 6 queue.
type ConsolidateOutcome string

const (
	OutcomeKeep        ConsolidateOutcome = "keep"
	OutcomeConsolidate ConsolidateOutcome = "consolidate"
	OutcomeDemote      ConsolidateOutcome = "demote"
	OutcomeRetire      ConsolidateOutcome = "retire"
	OutcomeArchive     ConsolidateOutcome = "archive"
	OutcomeDelete      ConsolidateOutcome = "delete"
)

// ConsolidateRow is one queue row from `brain consolidate plan`.
type ConsolidateRow struct {
	Sphere    Sphere             `json:"sphere"`
	Outcome   ConsolidateOutcome `json:"outcome"`
	Path      string             `json:"path"`
	Score     int                `json:"score"`
	Rationale string             `json:"rationale"`
	Proposed  string             `json:"proposed,omitempty"` // proposed survivor or destination
}

// MergePlan describes a consolidate --merge dry-run output.
type MergePlan struct {
	Loser    string    `json:"loser"`
	Survivor string    `json:"survivor"`
	Body     string    `json:"body"`      // merged body with conflict markers
	YAML     string    `json:"yaml"`      // merged frontmatter with conflict markers
	LinkPlan *MovePlan `json:"link_plan"` // PlanMove(from=loser, to=survivor)
}

// DreamReport is the Phase 7 free-association evidence packet.
type DreamReport struct {
	Sphere     Sphere           `json:"sphere"`
	Topics     []string         `json:"topics"`
	CrossLinks []LinkSuggestion `json:"cross_links"`
	Cold       []ColdLink       `json:"cold"`
	Generated  string           `json:"generated_at"`
}

// LinkSuggestion proposes a missing wikilink between two notes that mention
// each other in prose but lack the wikilink.
type LinkSuggestion struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// ColdLink is a wikilink whose target hasn't been touched within the cold
// threshold. Prune-links degrades these to plain text after two cycles.
type ColdLink struct {
	Source        string `json:"source"`
	Target        string `json:"target"`
	LastTouchDays int    `json:"last_touch_days"`
}

// Function stubs. Each is replaced by a real implementation in its own
// file (movenote.go, cleanup.go, consolidate.go, dream.go).
// Until then they panic so callers fail loudly during incremental builds.

// PlanMove and ApplyMove now live in movenote.go.

// CleanupDeadDirsScan now lives in cleanup.go.

// ConsolidatePlan and PrepareMerge now live in consolidate.go.

// DreamReportRun and DreamPruneLinksScan now live in dream.go.
