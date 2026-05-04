# Brain-ingest Python → Go MCP migration

Tracks the deprecation lifecycle of the legacy Python brain-ingest pipeline
in `~/<vault>/tools/brain-ingest/synthesize/` against the Go MCP equivalents
served by `sloptools mcp-server`. See umbrella issue #60.

The Python scripts are vault-side (Nextcloud / Dropbox), not part of this
repository. This doc is the in-repo source of truth for which Python entry
points have a Go counterpart, what is still missing, and where each script
sits in the deprecate → soak → archive lifecycle defined by #60.

## Lifecycle states

Each script moves through:

1. **planned** — Go counterpart not yet merged.
2. **ported** — Go MCP verb merged; Python still authoritative for users.
3. **banner** — Python script prints a deprecation banner to stderr naming
   the Go MCP verb. 60-day soak starts.
4. **archived** — Python script removed from the vault (E10.3).

## Per-script tracking issues

One open `Port <script>.py to internal/brain/<pkg>` issue per Python script
(E10.1 of #60). Each tracking issue carries a `Closes` cross-reference to the
relevant Go counterpart child issue.

| Script | Issue | Target package | Status |
|---|---|---|---|
| gtd.py | #102 | internal/brain/gtd | ported |
| folder_markdown.py | #103 | internal/brain/folder | ported |
| folder_review_queue.py | #104 | internal/brain/folder | planned |
| folder_review_apply.py | #105 | internal/brain/folder | planned |
| folder_quality.py | #106 | internal/brain/folder | planned |
| folder_stability.py | #107 | internal/brain/folder | planned |
| folder_units.py | #108 | internal/brain/folder | planned |
| glossary.py | #109 | internal/brain/glossary | ported |
| attention.py | #110 | internal/brain/attention | ported |
| entity_candidates.py | #111 | internal/brain/entities | ported |
| relation_candidates.py | #112 | internal/brain/entities | planned |
| archive_candidates.py | #113 | internal/brain/folder | planned |
| derive_monthly_index.py | #114 | internal/brain/people | ported |
| runtime_plan.py | #115 | internal/brain/runtime | planned |
| final_report.py | #116 | internal/brain/report | planned |
| stream_opencode_report.py | #117 | internal/brain/report | planned |
| validate_outputs.py | #118 | internal/brain/validate | ported |
| folder_review_packet.py | #119 | internal/brain/folder | planned |

## Sister epics (Go side)

All sister epics that produced the Go MCP surface are merged:

| Issue | Title | State |
|-------|-------|-------|
| #42 | Promote brain Markdown validators and parsers into reusable tools | closed |
| #46 | Add GTD commitment parser and validator for Markdown brain files | closed |
| #47 | Add folder, glossary, and attention note validators | closed |
| #50 | Add Todoist source adapter for GTD aggregate items | closed |
| #51 | Add GitHub and GitLab source adapters for assigned issues and reviews | closed |
| #52 | Add email follow-up and waiting-for source adapter | closed |
| #56 | Epic: Meetings as a first-class GTD source with sync-back | closed |
| #57 | Epic: Project hubs auto-render commitment lists; bulk-link by rules | closed |
| #58 | Epic: Per-person open-loops aggregation and dashboard | closed |
| #59 | Epic: Per-recipient meeting-summary email drafter | closed |
| #61 | Add brain.gtd.sync MCP verb | closed |

## Per-script status

Coverage is verified against the brain MCP dispatch table at
`internal/mcp/mcp_tool_dispatch.go:180` and the per-method handlers in
`internal/mcp/mcp_15.go`.

### gtd.py — multi-command commitment tooling (tracked in #102)

CLI surface (`gtd.py <command>`): `init`, `validate`, `index`, `import`,
`parse`, `organize`, `resurface`, `triage`, `apply-triage`, `review`. Plus
source adapters: `task_candidates`, `todoist_candidates`, `issue_candidates`,
`calendar_candidates`, `mail_candidates`, `discord_candidates`,
`markdown_candidates`, `evernote_candidates`, `meeting_candidates`,
`generated_candidates`.

| Python entry point | Go MCP verb | Notes |
|---|---|---|
| `gtd.py validate` | `brain.note.validate`, `brain.vault.validate` | per-note and vault-wide |
| `gtd.py parse` | `brain.gtd.parse`, `brain.note.parse` | |
| `gtd.py index` | `brain.gtd.dashboard`, `brain.gtd.today` | renders generated views |
| `gtd.py organize` | `brain.gtd.organize` | |
| `gtd.py resurface` | `brain.gtd.resurface` | |
| `gtd.py triage` / `apply-triage` | `brain.gtd.review_batch`, `brain.gtd.review_list`, `brain.gtd.set_status` | batch review pipeline |
| `gtd.py review` | `brain.gtd.review_batch`, `brain.gtd.set_status` | interactive flow superseded by batch + status writes |
| `gtd.py import --sources mail` | `brain.gtd.ingest source=mail` | |
| `gtd.py import --sources issues` | `brain.gtd.ingest source=github`, `source=gitlab` | |
| `todoist_candidates` | `brain.gtd.ingest source=todoist` | |
| `evernote_candidates` | `brain.gtd.ingest source=evernote` | |
| `meeting_candidates` | `brain.gtd.ingest source=meetings` | |
| `task_candidates` | `task_list`, `task_get` (groupware tasks) | tasks read via groupware MCP |
| `generated_candidates` | (vault-internal) | not a Go target; collapses once GTD lives in canonical notes |
| `markdown_candidates` | `brain.search`, `brain.backlinks` | ad-hoc note discovery via search |
| `calendar_candidates` | `calendar_events` | calendar read via groupware MCP |
| `discord_candidates` | — | no Discord adapter on the Go side; tracked separately if needed |
| dedup helpers | `brain.gtd.dedup_scan`, `brain.gtd.dedup_review_apply`, `brain.gtd.dedup_history` | |
| sync writeback | `brain.gtd.sync`, `brain.gtd.bind`, `brain.gtd.bulk_link` | per #61 |
| people dashboards | `brain.people.dashboard`, `brain.people.render`, `brain.people.brief` | per #58 |
| meeting kickoff | `brain.meeting.kickoff` | per #56 |
| project hubs | `brain.projects.render`, `brain.projects.list` | per #57 |

**Status: ported.** The Discord adapter has no Go counterpart; if the user
still relies on it, file a separate adapter issue rather than blocking
deprecation of the rest of `gtd.py`.

### folder_markdown.py — folder-note parser/validator (tracked in #103)

| Subcommand | Go MCP verb |
|---|---|
| `parse` | `brain.folder.parse` |
| `links` | `brain.folder.links` |
| `validate` | `brain.folder.validate` |
| `audit` | `brain.folder.audit` |
| `index` | (renderer, vault-internal) |

**Status: ported.** Index rendering remains vault-side because it writes
human-curated `_index.md` files; the data underneath comes from the parser.

### glossary.py — glossary validator and TSV/index emitter (tracked in #109)

| Subcommand | Go MCP verb |
|---|---|
| parse | `brain.glossary.parse` |
| validate | `brain.glossary.validate` |
| TSV / index emitters | (vault-internal renderers) |

**Status: ported.** Renderers (TSV, index Markdown) are deterministic
post-processing on top of the parsed structure; not Go-side targets.

### attention.py — attention-field tooling (tracked in #110)

| Subcommand | Go MCP verb |
|---|---|
| parse | `brain.attention.parse` |
| validate | `brain.attention.validate` |
| dashboard | `brain.people.dashboard`, `brain.people.render` |
| migrate (one-shot) | — |

**Status: ported.** The `migrate` subcommand was a one-shot bootstrapper
to add `focus`/`cadence` fields to existing notes; intentionally not ported.

### entity_candidates.py — entity extraction from reviewed notes (tracked in #111)

| Subcommand | Go MCP verb |
|---|---|
| candidates | `brain.entities.candidates` |

**Status: ported.**

### validate_outputs.py — synthesis-output validator (tracked in #118)

`brain.vault.validate` covers vault-level validation. The script also wraps
parser-specific validators that map to `brain.folder.validate`,
`brain.glossary.validate`, `brain.attention.validate`, `brain.note.validate`.

**Status: ported.**

### folder_review_packet.py — review packets for folder notes (tracked in #119)

Builds compact review packets including PDF page renders and direct child
note excerpts for one-shot LLM review of a single folder note. No Go
counterpart yet; the packet shape is review-pipeline-specific.

**Status: planned.**

### folder_review_queue.py — second-stage review queue (tracked in #104)

Builds a TSV/Markdown queue selecting folder notes for review based on
quality codes, stability cache, and sweep scope.

**Status: planned.**

### folder_review_apply.py — apply reviewed body (tracked in #105)

Applies a reviewed Markdown body back to the canonical folder note after
deterministic validation, refusing to run while a sweep is active.

**Status: planned.** Distinct from `brain.note.write` because it owns the
sweep-lock contract documented in `~/CLAUDE.md`.

### folder_quality.py — deterministic quality queue and repair (tracked in #106)

Two subcommands: `candidates` (TSV/Markdown report) and `repair`
(deterministic note rewrites with frontmatter normalization). The repair
half overlaps with `brain.note.write` semantics but encodes additional
folder-note repair rules.

**Status: planned.**

### folder_stability.py — pass-1 stability report (tracked in #107)

Aggregates folder-note ingestion stability by top-level vault subtree.
Reads the same parser data; emits Markdown + TSV.

**Status: planned.**

### folder_units.py — semantic folder work-unit planner (tracked in #108)

Plans non-overlapping folder-sweep work units. Heaviest of the planner
scripts (≈600 LOC); calls Codex/Qwen for suggestions and validates the
resulting `work_units.tsv`.

**Status: planned.** Significant LLM-orchestration logic; port carefully.

### archive_candidates.py — bulky-tree archive recommender (tracked in #113)

Identifies vault subtrees that should be archived as a single zip rather
than ingested. Mixes deterministic prefilter with optional Codex review.

**Status: planned.**

### derive_monthly_index.py — monthly journal index generator (tracked in #114)

Emits `brain/journal/<YYYY-MM>.md` indexes from `## Log` bullets in
`brain/people`, `brain/projects`, and `brain/topics` notes.

| Subcommand | Go MCP verb |
|---|---|
| (single command) | `brain.people.monthly_index` |

**Status: ported.** Idempotent: re-runs do not rewrite unchanged pages.
The Python writes both vaults in one invocation; the Go verb takes a
required `sphere` so callers iterate explicitly.

### relation_candidates.py — typed-relation extraction (tracked in #112)

Extracts simple typed relations between entities for the semantic graph
phase.

**Status: planned.**

### runtime_plan.py — runtime estimator (tracked in #115)

Estimates remaining runtime for the brain-ingest workflow. Useful as a
human dashboard, not a writer; low priority for porting.

**Status: planned.**

### final_report.py / stream_opencode_report.py — report emitters (tracked in #116, #117)

Emit final review reports / stream Opencode output. Pipeline-local
diagnostics; consider whether they need a Go port at all.

**Status: planned.** May be archived without porting if the Go pipeline
emits equivalent diagnostics natively.

## Summary

| Bucket | Scripts |
|---|---|
| ported (banner + soak ready) | gtd.py, folder_markdown.py, glossary.py, attention.py, entity_candidates.py, validate_outputs.py, derive_monthly_index.py |
| planned | folder_review_packet.py, folder_review_queue.py, folder_review_apply.py, folder_quality.py, folder_stability.py, folder_units.py, archive_candidates.py, relation_candidates.py, runtime_plan.py, final_report.py, stream_opencode_report.py |

Seven of eighteen scripts have Go counterparts merged and are ready for the
deprecation banner step (E10.2 of #60). The remaining eleven are the
brain-ingest review/queue/report pipeline whose Go port has not been
scoped yet; they remain Python-authoritative for now.

## How to update this doc

When a Python script gains a Go counterpart, move its row from `planned` to
`ported` in the summary and fill in the per-script Go MCP verb table.
When a banner lands, note the soak start date in the per-script section.
When a script is archived (E10.3), strike the row through and note the
archive commit. This file is the only place where lifecycle status lives;
do not duplicate the table elsewhere in the repo.
