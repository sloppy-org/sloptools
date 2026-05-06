package brain

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SleepPacket is the full prompt context handed to Codex during sleep.
type SleepPacket struct {
	Report      *DreamReport
	PrunePlan   *MovePlan
	Cold        []ColdLink
	NREM        []ConsolidateRow
	RecentPaths []string
	Sphere      Sphere
	Autonomy    string
	Now         time.Time
	GitPacket   string
}

func limitConsolidateRows(rows []ConsolidateRow, limit int) []ConsolidateRow {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return append([]ConsolidateRow(nil), rows[:limit]...)
}

func prioritizeSleepNREM(rows []ConsolidateRow, recent []string, limit int) []ConsolidateRow {
	if len(rows) == 0 {
		return nil
	}
	recentSet := recentPathSet(recent)
	picked := append([]ConsolidateRow(nil), rows...)
	sort.SliceStable(picked, func(i, j int) bool {
		leftRecent := recentSet[canonicalSleepPath(picked[i].Path)]
		rightRecent := recentSet[canonicalSleepPath(picked[j].Path)]
		if leftRecent != rightRecent {
			return leftRecent
		}
		if picked[i].Score != picked[j].Score {
			return picked[i].Score > picked[j].Score
		}
		return picked[i].Path < picked[j].Path
	})
	return limitConsolidateRows(picked, limit)
}

func recentPathSet(paths []string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, path := range paths {
		set[canonicalSleepPath(path)] = true
	}
	return set
}

func canonicalSleepPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	return strings.TrimPrefix(path, "brain/")
}

func recentBrainMemory(vault Vault, now time.Time) []string {
	root := vault.BrainRoot()
	if _, ok := gitWorkTreeRoot(root); !ok {
		return nil
	}
	base, ok := latestSleepCommit(root)
	var args []string
	if ok {
		args = []string{"diff", "--name-only", inclusiveCommitRange(root, base), "--"}
	} else {
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		args = []string{"log", "--since=" + start.Format(time.RFC3339), "--name-only", "--format=", "HEAD", "--"}
	}
	out, err := gitOutput(root, args...)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		rel := filepath.ToSlash(strings.TrimSpace(line))
		if rel == "" || seen[rel] || strings.HasPrefix(rel, "personal/") {
			continue
		}
		seen[rel] = true
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	if len(paths) > 200 {
		return paths[:200]
	}
	return paths
}

// renderSleepPacket builds the autonomous sleep packet. In full autonomy Codex
// is expected to edit the vault; in plan-only it only writes a report.
func renderSleepPacket(packet SleepPacket) string {
	report := packet.Report
	var b strings.Builder
	fmt.Fprintf(&b, "# Brain sleep run — %s — %s\n\n", packet.Sphere, packet.Now.Format("2006-01-02"))
	fmt.Fprintf(&b, "Generated: %s\n", packet.Now.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Autonomy: %s\n\n", packet.Autonomy)
	writeSleepMission(&b, packet.Autonomy, packet.Sphere)
	writeRecentSection(&b, packet.RecentPaths, packet.GitPacket)
	writeNREMSection(&b, packet.NREM)
	writeREMSection(&b, report)
	writePruneSection(&b, packet.PrunePlan, packet.Cold, report)
	writeSleepReturnContract(&b, packet.Autonomy)
	return b.String()
}

func writeSleepMission(b *strings.Builder, autonomy string, sphere Sphere) {
	fmt.Fprintln(b, "## Mission")
	fmt.Fprintln(b)
	if autonomy == SleepAutonomyPlanOnly {
		fmt.Fprintln(b, "Run a read-only sleep analysis. Do not edit files.")
	} else {
		fmt.Fprintln(b, "Run an autonomous brain sleep cycle. Edit the brain directly when the evidence supports it.")
	}
	fmt.Fprintf(b, "Sphere: `%s`. Keep this sphere isolated. Do not read or edit `personal/` paths. Do not mix work and private memory.\n\n", sphere)
	fmt.Fprintln(b, "NREM means replay recent memories into stable canonical notes: people, projects, topics, institutions, glossary, and folder/topic boundaries.")
	fmt.Fprintln(b, "REM means associative graph rewiring: missing semantic links, aliases, contradictions, unsupported claims, stale links, and abstractions across nearby notes.")
	if autonomy == SleepAutonomyFull {
		fmt.Fprintln(b, "You may create, edit, merge, retire, delete, and relink brain notes. Git history is the rollback layer; keep each change evidence-backed and local to the packet.")
	}
	fmt.Fprintln(b)
}

func writeRecentSection(b *strings.Builder, paths []string, gitPacket string) {
	fmt.Fprintln(b, "## Recent Memory")
	fmt.Fprintln(b)
	if len(paths) == 0 {
		fmt.Fprintln(b, "_No changed brain paths detected._")
	} else {
		for _, path := range paths {
			fmt.Fprintf(b, "- %s\n", path)
		}
	}
	fmt.Fprintln(b)
	if strings.TrimSpace(gitPacket) != "" {
		fmt.Fprintln(b, "### Git Context")
		fmt.Fprintln(b)
		fmt.Fprintln(b, gitPacket)
		fmt.Fprintln(b)
	}
}

func writeNREMSection(b *strings.Builder, rows []ConsolidateRow) {
	fmt.Fprintf(b, "## NREM Recent-Prioritized Consolidation Candidates (%d)\n\n", len(rows))
	if len(rows) == 0 {
		fmt.Fprintln(b, "_(none)_")
		fmt.Fprintln(b)
		return
	}
	fmt.Fprintln(b, "| Outcome | Score | Path | Proposed | Rationale |")
	fmt.Fprintln(b, "|---------|-------|------|----------|-----------|")
	for _, row := range rows {
		fmt.Fprintf(b, "| %s | %d | %s | %s | %s |\n",
			row.Outcome, row.Score, mdCell(row.Path), mdCell(row.Proposed), mdCell(row.Rationale))
	}
	fmt.Fprintln(b)
}

func writeREMSection(b *strings.Builder, report *DreamReport) {
	fmt.Fprintf(b, "## REM Picked Topics (%d)\n\n", len(report.Topics))
	for _, topic := range report.Topics {
		fmt.Fprintf(b, "- %s\n", topic)
	}
	if len(report.Topics) == 0 {
		fmt.Fprintln(b, "_(none)_")
	}
	fmt.Fprintln(b)
	fmt.Fprintf(b, "## REM Cross-Link Suggestions (%d)\n\n", len(report.CrossLinks))
	for _, item := range report.CrossLinks {
		fmt.Fprintf(b, "- %s -> %s: %s\n", item.From, item.To, item.Reason)
	}
	if len(report.CrossLinks) == 0 {
		fmt.Fprintln(b, "_(none)_")
	}
	fmt.Fprintln(b)
}

func writePruneSection(b *strings.Builder, plan *MovePlan, cold []ColdLink, report *DreamReport) {
	fmt.Fprintln(b, "## Synaptic Maintenance")
	fmt.Fprintln(b)
	fmt.Fprintf(b, "- cold-link prune count: %d\n", len(cold))
	if plan != nil {
		fmt.Fprintf(b, "- cold-link prune digest: %s\n", plan.Digest)
	}
	fmt.Fprintf(b, "- cold targets reached from picked notes: %d\n\n", len(report.Cold))
	for _, item := range cold {
		fmt.Fprintf(b, "- %s -> %s (%d days)\n", item.Source, item.Target, item.LastTouchDays)
	}
	if len(cold) == 0 {
		fmt.Fprintln(b, "_(none)_")
	}
	fmt.Fprintln(b)
}

func writeSleepReturnContract(b *strings.Builder, autonomy string) {
	fmt.Fprintln(b, "## Return Contract")
	fmt.Fprintln(b)
	fmt.Fprintln(b, "Return a concise Markdown report with:")
	fmt.Fprintln(b, "- NREM changes made")
	fmt.Fprintln(b, "- REM changes made")
	fmt.Fprintln(b, "- deleted/merged/retired notes")
	fmt.Fprintln(b, "- unresolved contradictions or evidence gaps")
	fmt.Fprintln(b, "- paths changed")
	if autonomy == SleepAutonomyPlanOnly {
		fmt.Fprintln(b, "- proposed edits only, because this run is plan-only")
	}
}

func mdCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.TrimSpace(value)
	if value == "" {
		return " "
	}
	return value
}
