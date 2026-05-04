// Package people derives canonical monthly journal indexes from `## Log`
// entries kept in `brain/people`, `brain/projects`, and `brain/topics`
// notes. Ports the legacy `derive_monthly_index.py` writer.
package people

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// LogSection is the canonical H2 heading whose dated bullets are
// promoted into a monthly journal index.
const LogSection = "## Log"

// JournalDir is the path under brainRoot where monthly indexes live.
const JournalDir = "journal"

var logEntryPattern = regexp.MustCompile(`^- (\d{4}-\d{2})(?:-\d{2})? [\x{2014}-] (.+)$`)

// sourceDirs lists the canonical brain subdirectories whose notes
// contribute log entries to the monthly index.
var sourceDirs = []string{"people", "projects", "topics"}

// MonthlyIndex groups dated log entries from a single calendar month.
// Lines are pre-rendered as Obsidian wikilink bullets.
type MonthlyIndex struct {
	Month string
	Lines []string
}

// Result reports counts of months collected and journal files written by
// WriteMonthlyIndexes. DryRun mirrors the request flag so callers can log
// the mode without re-threading it.
type Result struct {
	Months int  `json:"months"`
	Writes int  `json:"writes"`
	DryRun bool `json:"dry_run"`
}

// CollectMonthlyIndexes scans brainRoot/people, brainRoot/projects, and
// brainRoot/topics for notes carrying a `## Log` section and returns the
// dated bullets bucketed by YYYY-MM. Buckets and lines inside each bucket
// are sorted; missing source directories are skipped silently.
func CollectMonthlyIndexes(brainRoot string) ([]MonthlyIndex, error) {
	buckets := map[string][]string{}
	for _, dir := range sourceDirs {
		base := filepath.Join(brainRoot, dir)
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			path := filepath.Join(base, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			text := string(data)
			if !hasLogSection(text) {
				continue
			}
			stem := strings.TrimSuffix(entry.Name(), ".md")
			collectLines(text, stem, buckets)
		}
	}
	months := make([]string, 0, len(buckets))
	for month := range buckets {
		months = append(months, month)
	}
	sort.Strings(months)
	out := make([]MonthlyIndex, 0, len(months))
	for _, month := range months {
		lines := buckets[month]
		sort.Strings(lines)
		out = append(out, MonthlyIndex{Month: month, Lines: lines})
	}
	return out, nil
}

// RenderMonthlyIndex returns the canonical Markdown body for one month's
// journal page: an H1 month heading, blank line, sorted bullets, trailing
// newline.
func RenderMonthlyIndex(month string, lines []string) string {
	sorted := append([]string(nil), lines...)
	sort.Strings(sorted)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(month)
	b.WriteString("\n\n")
	for i, line := range sorted {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	b.WriteByte('\n')
	return b.String()
}

// WriteMonthlyIndexes derives, renders, and writes the monthly journal
// pages under brainRoot/journal. Existing pages with identical contents
// are not rewritten so the result counts only real changes; in dry-run
// mode every month is counted as a would-be write but no files are
// touched.
func WriteMonthlyIndexes(brainRoot string, dryRun bool) (Result, error) {
	indexes, err := CollectMonthlyIndexes(brainRoot)
	if err != nil {
		return Result{}, err
	}
	res := Result{Months: len(indexes), DryRun: dryRun}
	for _, index := range indexes {
		path := filepath.Join(brainRoot, JournalDir, index.Month+".md")
		body := RenderMonthlyIndex(index.Month, index.Lines)
		if dryRun {
			res.Writes++
			continue
		}
		changed, err := writeIfChanged(path, body)
		if err != nil {
			return Result{}, err
		}
		if changed {
			res.Writes++
		}
	}
	return res, nil
}

func hasLogSection(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, " \t") == LogSection {
			return true
		}
	}
	return false
}

func collectLines(text, stem string, buckets map[string][]string) {
	for _, line := range strings.Split(text, "\n") {
		match := logEntryPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		month := match[1]
		body := strings.TrimSpace(match[2])
		if body == "" {
			continue
		}
		buckets[month] = append(buckets[month], "- [["+stem+"]] — "+body)
	}
}

func writeIfChanged(path, body string) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == body {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
