package sleepconv

import (
	"fmt"
	"sort"
	"strings"
)

// RenderActivitySection emits the structured Markdown describing what
// the user actually did since the previous sleep, at a level the
// model can act on. Sections are gated so empty data does not bloat
// the packet.
//
// The shape is dictated by the working principle the user named: in
// the physical brain, sleep both rewires connections AND modifies the
// content stored at synapses; in the Markdown brain, the analogue is
// editing the prose, frontmatter, and dated-bullet sections of
// canonical entity notes — not just adding wikilinks. The packet
// therefore surfaces the day's actual experiences (files touched,
// URLs fetched, decisions, GitHub refs, sub-agent dispatches), the
// canonical note that already governs each subject, and a checklist
// of mandatory actions for the model.
func RenderActivitySection(a Activity, idx *BrainIndex, vaultRoots []string) string {
	if !activityHasContent(a) {
		return ""
	}
	if idx != nil && len(vaultRoots) > 0 {
		a.FilesTouched = idx.EnrichFiles(a.FilesTouched, vaultRoots)
	}
	var b strings.Builder
	b.WriteString("## Activity since previous sleep\n\n")
	b.WriteString("This is the day's actual experience extracted from interactive Claude Code and Codex CLI sessions: files touched, web pages fetched, git/gh/sloptools commands run, sub-agents dispatched. **This — not the user-prompt prose alone — is what you must consolidate into the brain.**\n\n")
	b.WriteString("Sleep in this brain has two halves, and tonight's run must do both:\n\n")
	b.WriteString("1. **Rewire** — wikilinks between notes, the synaptic graph. (Already covered by the REM cross-link suggestions later in the packet.)\n")
	b.WriteString("2. **Modify content** — the prose, frontmatter, dated bullets, and lists *inside* canonical notes, the analogue of microscale memory updates in the physical brain. The previous sleep run only rewired; that is the failure mode this section is here to break.\n\n")
	b.WriteString("For every row in the tables below, locate the canonical brain note (column \"Note\") and **EDIT IT IN PLACE**. New facts from the day's activity become dated bullets under the appropriate section; status changes update frontmatter; new relations extend `relations:` lists; new aliases extend `aliases:`. When no note exists for a subject that the day's activity clearly establishes (recurring file edits, repeated mentions, declared commitments), **CREATE** the canonical note using `brain/conventions/attention.md` (people) or `brain/conventions/entity-graph.md` (other kinds).\n\n")
	writeSessionsTable(&b, a.Sessions)
	writeFilesTable(&b, a.FilesTouched)
	writeWebFetchesTable(&b, a.WebFetches)
	writeGitOpsTable(&b, a.GitOps)
	writeGitHubRefsTable(&b, a.GitHubRefs)
	writeSearchesTable(&b, a.Searches)
	writeSubagentsTable(&b, a.SubAgents)
	b.WriteString("**Anti-feedback rule.** Some of the bash hits, file reads, and web fetches above were the agents' own retrieval over the brain. Treat any path under `brain/`, any URL serving brain content, and any prose that merely echoes existing canonical text as evidence of *the brain's current state*, not as a new candidate fact. Do not extract facts you only saw because the agent already read them out of the brain.\n\n")
	return b.String()
}

func activityHasContent(a Activity) bool {
	return len(a.Sessions) > 0 || len(a.FilesTouched) > 0 || len(a.WebFetches) > 0 || len(a.BashHits) > 0 || len(a.SubAgents) > 0 || len(a.GitOps) > 0 || len(a.GitHubRefs) > 0 || len(a.Searches) > 0
}

func writeSessionsTable(b *strings.Builder, sessions []SessionDigest) {
	if len(sessions) == 0 {
		return
	}
	fmt.Fprintf(b, "### Sessions (%d)\n\n", len(sessions))
	b.WriteString("| Source | CWD | User turns | Tool events |\n|---|---|---|---|\n")
	cap := len(sessions)
	if cap > 30 {
		cap = 30
	}
	for _, s := range sessions[:cap] {
		fmt.Fprintf(b, "| %s | %s | %d | %d |\n", s.Source, mdCell(s.CWD), s.UserTurns, s.ToolEvents)
	}
	if len(sessions) > cap {
		fmt.Fprintf(b, "_(+%d more sessions truncated)_\n", len(sessions)-cap)
	}
	b.WriteByte('\n')
}

func writeFilesTable(b *strings.Builder, files []FileTouch) {
	if len(files) == 0 {
		return
	}
	fmt.Fprintf(b, "### Files touched (%d)\n\n", len(files))
	b.WriteString("Op rank: write > edit > read. The Note column is the canonical brain-relative path of the folder note or project note that already governs the file's directory; act on that note when present, or create one when the path's repository or folder is clearly a recurring subject of the user's work.\n\n")
	b.WriteString("| Op | Path | Sessions | Note |\n|---|---|---|---|\n")
	cap := len(files)
	if cap > 60 {
		cap = 60
	}
	for _, f := range files[:cap] {
		note := f.BrainHit
		if note == "" {
			note = "_(none)_"
		}
		fmt.Fprintf(b, "| %s | %s | %d | %s |\n", f.Op, mdCell(f.Path), f.Sessions, mdCell(note))
	}
	if len(files) > cap {
		fmt.Fprintf(b, "_(+%d more files truncated)_\n", len(files)-cap)
	}
	b.WriteByte('\n')
}

func writeWebFetchesTable(b *strings.Builder, fetches []WebFetchOp) {
	if len(fetches) == 0 {
		return
	}
	fmt.Fprintf(b, "### Web research (%d)\n\n", len(fetches))
	b.WriteString("| URL | Hits | Intent |\n|---|---|---|\n")
	cap := len(fetches)
	if cap > 40 {
		cap = 40
	}
	for _, f := range fetches[:cap] {
		fmt.Fprintf(b, "| %s | %d | %s |\n", mdCell(f.URL), f.Hits, mdCell(f.Intent))
	}
	if len(fetches) > cap {
		fmt.Fprintf(b, "_(+%d more URLs truncated)_\n", len(fetches)-cap)
	}
	b.WriteByte('\n')
}

func writeGitOpsTable(b *strings.Builder, ops []GitOp) {
	if len(ops) == 0 {
		return
	}
	commits := []GitOp{}
	others := map[string]int{}
	for _, op := range ops {
		if op.Op == "commit" && op.Subject != "" {
			commits = append(commits, op)
		} else {
			others[op.Op]++
		}
	}
	fmt.Fprintf(b, "### Git operations (%d total)\n\n", len(ops))
	if len(commits) > 0 {
		b.WriteString("Commits this period:\n\n")
		for _, c := range commits {
			fmt.Fprintf(b, "- %s\n", mdCell(c.Subject))
		}
		b.WriteByte('\n')
	}
	if len(others) > 0 {
		keys := make([]string, 0, len(others))
		for k := range others {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("Other ops: ")
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s×%d", k, others[k]))
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("\n\n")
	}
}

func writeGitHubRefsTable(b *strings.Builder, refs []GitHubRef) {
	if len(refs) == 0 {
		return
	}
	fmt.Fprintf(b, "### GitHub references (%d)\n\n", len(refs))
	b.WriteString("| Owner/Repo | Kind | Number |\n|---|---|---|\n")
	cap := len(refs)
	if cap > 30 {
		cap = 30
	}
	for _, r := range refs[:cap] {
		num := ""
		if r.Number > 0 {
			num = fmt.Sprintf("%d", r.Number)
		}
		fmt.Fprintf(b, "| %s/%s | %s | %s |\n", mdCell(r.Owner), mdCell(r.Repo), r.Kind, num)
	}
	if len(refs) > cap {
		fmt.Fprintf(b, "_(+%d more refs truncated)_\n", len(refs)-cap)
	}
	b.WriteByte('\n')
}

func writeSearchesTable(b *strings.Builder, searches []SearchOp) {
	if len(searches) == 0 {
		return
	}
	web := []SearchOp{}
	other := []SearchOp{}
	for _, s := range searches {
		if s.Tool == "WebSearch" {
			web = append(web, s)
		} else {
			other = append(other, s)
		}
	}
	if len(web) > 0 {
		fmt.Fprintf(b, "### Web searches (%d)\n\n", len(web))
		cap := len(web)
		if cap > 20 {
			cap = 20
		}
		for _, s := range web[:cap] {
			fmt.Fprintf(b, "- %s\n", mdCell(s.Query))
		}
		if len(web) > cap {
			fmt.Fprintf(b, "_(+%d more queries truncated)_\n", len(web)-cap)
		}
		b.WriteByte('\n')
	}
}

func writeSubagentsTable(b *strings.Builder, agents []SubAgentDispatch) {
	if len(agents) == 0 {
		return
	}
	fmt.Fprintf(b, "### Sub-agent dispatches (%d)\n\n", len(agents))
	b.WriteString("| Type | Description |\n|---|---|\n")
	cap := len(agents)
	if cap > 20 {
		cap = 20
	}
	for _, a := range agents[:cap] {
		fmt.Fprintf(b, "| %s | %s |\n", mdCell(a.Type), mdCell(a.Description))
	}
	if len(agents) > cap {
		fmt.Fprintf(b, "_(+%d more dispatches truncated)_\n", len(agents)-cap)
	}
	b.WriteByte('\n')
}
