package brain

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/glossary"
)

type FolderFinding struct {
	Code   string `json:"code"`
	Weight int    `json:"weight"`
	Detail string `json:"detail"`
}

type FolderQualityCandidate struct {
	Source   ResolvedPath    `json:"source"`
	Folder   string          `json:"folder"`
	Status   string          `json:"status"`
	Score    int             `json:"score"`
	Findings []FolderFinding `json:"findings"`
}

type FolderReviewItem struct {
	Source        ResolvedPath    `json:"source"`
	Folder        string          `json:"folder"`
	Status        string          `json:"status"`
	Score         int             `json:"score"`
	Route         string          `json:"route"`
	EvidenceBasis string          `json:"evidence_basis"`
	Findings      []FolderFinding `json:"findings"`
}

type FolderStabilityRow struct {
	Sphere  string `json:"sphere"`
	Total   int    `json:"total"`
	Valid   int    `json:"valid"`
	Invalid int    `json:"invalid"`
	Status  string `json:"status"`
}

type WorkUnitIssue struct {
	Unit   string `json:"unit"`
	Issue  string `json:"issue"`
	Detail string `json:"detail,omitempty"`
}

type ArchiveCandidate struct {
	Vault      string `json:"vault"`
	Path       string `json:"path"`
	Action     string `json:"action"`
	Confidence string `json:"confidence"`
	Score      int    `json:"score"`
	Rationale  string `json:"rationale"`
}

var yearPattern = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

func FolderQuality(cfg *Config, sphere Sphere) ([]FolderQualityCandidate, error) {
	var rows []FolderQualityCandidate
	err := WalkVaultNotes(cfg, sphere, func(snap NoteSnapshot) error {
		if snap.Kind != "folder" || snap.Note == nil {
			return nil
		}
		folder, diags := ValidateFolderNote(snap.Body, LinkValidationContext{Config: cfg, Sphere: snap.Source.Sphere, Path: snap.Source.Path})
		findings := folderFindings(snap.Note, folder, diags)
		if len(findings) == 0 {
			return nil
		}
		score := 0
		for _, finding := range findings {
			score += finding.Weight
		}
		rows = append(rows, FolderQualityCandidate{Source: snap.Source, Folder: folder.SourceFolder, Status: folder.Status, Score: score, Findings: findings})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Score != rows[j].Score {
			return rows[i].Score > rows[j].Score
		}
		return rows[i].Folder < rows[j].Folder
	})
	return rows, nil
}

func FolderReviewQueue(cfg *Config, sphere Sphere, limit int) ([]FolderReviewItem, error) {
	quality, err := FolderQuality(cfg, sphere)
	if err != nil {
		return nil, err
	}
	items := make([]FolderReviewItem, 0, len(quality))
	for _, row := range quality {
		basis := ""
		if _, data, err := ReadNoteFile(cfg, sphere, row.Source.Rel); err == nil {
			note, _ := ParseMarkdownNote(string(data), MarkdownParseOptions{})
			basis = keyFactValue(note, "Evidence basis")
		}
		items = append(items, FolderReviewItem{
			Source: row.Source, Folder: row.Folder, Status: row.Status, Score: row.Score,
			Route: reviewRoute(row.Folder, basis, row.Findings, row.Score), EvidenceBasis: basis,
			Findings: row.Findings,
		})
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func FolderStability(cfg *Config, sphere Sphere) (FolderStabilityRow, error) {
	notes, err := AuditFolderVault(cfg, sphere)
	if err != nil {
		return FolderStabilityRow{}, err
	}
	row := FolderStabilityRow{Sphere: string(sphere), Total: len(notes)}
	for _, note := range notes {
		if note.Valid {
			row.Valid++
		} else {
			row.Invalid++
		}
	}
	switch {
	case row.Total == 0:
		row.Status = "empty"
	case row.Invalid == 0:
		row.Status = "complete"
	case row.Valid == 0:
		row.Status = "pending"
	default:
		row.Status = "running"
	}
	return row, nil
}

func FolderReviewPacket(cfg *Config, sphere Sphere, path string) (string, error) {
	resolved, data, err := ReadNoteFile(cfg, sphere, path)
	if err != nil {
		return "", err
	}
	note, diags := ParseMarkdownNote(string(data), MarkdownParseOptions{})
	if len(diags) != 0 {
		return "", fmt.Errorf("note parse diagnostics: %s", diagnosticsString(diags))
	}
	folder, _ := ParseFolderNote(string(data))
	var b strings.Builder
	b.WriteString("# Folder Note Review Packet\n\n")
	b.WriteString("Output only revised Markdown body sections starting with `## Summary`.\n\n")
	b.WriteString("## Current Note\n\n")
	b.Write(data)
	b.WriteString("\n## Parsed Context\n\n")
	fmt.Fprintf(&b, "- Note: `%s`\n- Source folder: `%s`\n- Status: `%s`\n", resolved.Rel, folder.SourceFolder, folder.Status)
	if vault, ok := cfg.Vault(sphere); ok {
		if section := glossary.FormatPacketSection(glossary.RelevantTerms(vault.BrainRoot(), string(data))); section != "" {
			b.WriteString("\n")
			b.WriteString(section)
		}
	}
	b.WriteString("\n## Section Excerpts\n\n")
	for _, section := range note.Sections() {
		if section.Level == 2 {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", section.Name, truncate(section.Body, 1600))
		}
	}
	return b.String(), nil
}

func ApplyFolderReview(cfg *Config, sphere Sphere, path, review string) (ResolvedPath, bool, error) {
	resolved, original, err := ReadNoteFile(cfg, sphere, path)
	if err != nil {
		return ResolvedPath{}, false, err
	}
	body, err := extractFolderReviewBody(review)
	if err != nil {
		return ResolvedPath{}, false, err
	}
	updated, err := replaceFolderBody(string(original), body)
	if err != nil {
		return ResolvedPath{}, false, err
	}
	_, diags := ValidateFolderNote(updated, LinkValidationContext{Config: cfg, Sphere: sphere, Path: resolved.Path})
	if len(diags) != 0 {
		return ResolvedPath{}, false, fmt.Errorf("reviewed note failed validation: %s", diagnosticsString(diags))
	}
	if updated == string(original) {
		return resolved, false, nil
	}
	if err := os.WriteFile(resolved.Path, []byte(updated), 0o644); err != nil {
		return ResolvedPath{}, false, err
	}
	return resolved, true, nil
}

func ValidateWorkUnits(root string) ([]WorkUnitIssue, error) {
	rows, err := readTSV(filepath.Join(root, "data", "folder", "work_units.tsv"))
	if err != nil {
		return nil, err
	}
	var issues []WorkUnitIssue
	seen := map[string]string{}
	for _, row := range rows {
		unit := strings.Trim(row["vault"]+":"+row["unit_root"], ":")
		if strings.TrimSpace(row["unit_root"]) == "" {
			issues = append(issues, WorkUnitIssue{Unit: unit, Issue: "missing-unit-root"})
		}
		for existing := range seen {
			if unit != existing && (strings.HasPrefix(unit, existing+"/") || strings.HasPrefix(existing, unit+"/")) {
				issues = append(issues, WorkUnitIssue{Unit: unit, Issue: "overlap", Detail: existing})
			}
		}
		seen[unit] = unit
	}
	return issues, nil
}

func ArchiveCandidates(root, vaultFilter string, limit int) ([]ArchiveCandidate, error) {
	rows, err := readTSV(archiveProfilePath(root))
	if err != nil {
		return nil, err
	}
	var out []ArchiveCandidate
	for _, row := range rows {
		if vaultFilter != "" && vaultFilter != "both" && row["vault"] != vaultFilter {
			continue
		}
		score, rationale := archiveScore(row["path"], row["extensions"], atoiDefault(row["processable_files"], 0), atoiDefault(row["processable_dirs"], 0))
		if score < 45 {
			continue
		}
		action := "needs_input"
		confidence := "medium"
		if strings.Contains(rationale, "third-party") && atoiDefault(row["processable_files"], 0) >= 50 {
			action = "archive_sure"
			confidence = "high"
		}
		out = append(out, ArchiveCandidate{Vault: row["vault"], Path: row["path"], Action: action, Confidence: confidence, Score: score, Rationale: rationale})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Action != out[j].Action {
			return out[i].Action < out[j].Action
		}
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func archiveProfilePath(root string) string {
	candidates := []string{
		filepath.Join(root, "data", "folder", "profiles.tsv"),
		filepath.Join(root, "data", "folder", "tree_profile_fast.tsv"),
		filepath.Join(root, "data", "folder", "tree_profile.tsv"),
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return candidates[0]
}

func folderFindings(note *MarkdownNote, folder FolderNote, diags []MarkdownDiagnostic) []FolderFinding {
	var out []FolderFinding
	for _, diag := range diags {
		out = append(out, FolderFinding{"validation", 8, diag.Message})
	}
	if folder.Status == "active" {
		years := yearPattern.FindAllString(folder.SourceFolder, -1)
		if len(years) > 0 {
			year := atoiDefault(years[0], time.Now().Year())
			if year <= time.Now().Year()-3 {
				out = append(out, FolderFinding{"old-active-path", 4, "active status on dated path"})
			}
		}
	}
	if section, ok := note.Section("Open Questions"); ok && hasUsefulBullets(section.Body) {
		out = append(out, FolderFinding{"open-questions", 3, "non-empty Open Questions"})
	}
	if section, ok := note.Section("Summary"); ok && len(strings.TrimSpace(section.Body)) < 160 {
		out = append(out, FolderFinding{"short-summary", 2, "summary shorter than 160 characters"})
	}
	return out
}

func reviewRoute(source, basis string, findings []FolderFinding, score int) string {
	text := source + " " + basis
	for _, finding := range findings {
		text += " " + finding.Code
	}
	high := regexp.MustCompile(`(?i)\b(NTV|TSVV|DIPLOMARBEIT|PROPOSALS|TALKS|EURATOM|ISHW|EPS|papers?|thes[ie]s)\b`).MatchString(text)
	switch {
	case strings.Contains(text, "validation"):
		return "deterministic"
	case high && score >= 8:
		return "qwen122b"
	default:
		return "qwen"
	}
}

func extractFolderReviewBody(review string) (string, error) {
	text := strings.TrimSpace(review)
	text = strings.TrimPrefix(text, "```markdown")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	idx := strings.Index(text, "## Summary")
	if idx < 0 {
		return "", fmt.Errorf("review output does not contain ## Summary")
	}
	body := strings.TrimSpace(text[idx:]) + "\n"
	if _, diags := ParseMarkdownNote(body, MarkdownParseOptions{RequiredSections: []string{"Summary", "Key Facts", "Important Files", "Related Folders", "Related Notes", "Notes", "Open Questions"}}); len(diags) != 0 {
		return "", fmt.Errorf("review body failed validation: %s", diagnosticsString(diags))
	}
	return body, nil
}

func replaceFolderBody(original, body string) (string, error) {
	idx := strings.Index(original, "\n## Summary")
	if idx < 0 {
		return "", fmt.Errorf("original note does not contain ## Summary")
	}
	return strings.TrimRight(original[:idx], "\n") + "\n\n" + body, nil
}

func keyFactValue(note *MarkdownNote, label string) string {
	section, ok := note.Section("Key Facts")
	if !ok {
		return ""
	}
	for _, line := range strings.Split(section.Body, "\n") {
		if strings.HasPrefix(line, "- "+label+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- "+label+":"))
		}
	}
	return ""
}

func diagnosticsString(diags []MarkdownDiagnostic) string {
	parts := make([]string, 0, len(diags))
	for _, diag := range diags {
		parts = append(parts, diag.Message)
	}
	return strings.Join(parts, "; ")
}

func hasUsefulBullets(section string) bool {
	for _, line := range strings.Split(section, "\n") {
		value := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(value, "- ") && value != "- none." && value != "- none" && value != "- none identified." {
			return true
		}
	}
	return false
}

func archiveScore(path, extensions string, files, dirs int) (int, string) {
	lowered := strings.ToLower(path)
	score := 0
	var hits []string
	for _, hint := range []string{"sharpziplib", "zlib", "node_modules", "vendor", "thirdparty", "3rdparty", "sdk", "library", "lib"} {
		if strings.Contains(lowered, hint) {
			score += 50
			hits = append(hits, "third-party:"+hint)
		}
	}
	for _, hint := range []string{"backup", "old", "cache", "generated", "templates", "thumbnails"} {
		if strings.Contains(lowered, hint) {
			score += 25
			hits = append(hits, "generated-or-old:"+hint)
		}
	}
	if files >= 80 {
		score += files / 10
		hits = append(hits, fmt.Sprintf("many-files:%d", files))
	}
	if dirs >= 12 {
		score += dirs / 2
		hits = append(hits, fmt.Sprintf("many-dirs:%d", dirs))
	}
	if strings.Contains(extensions, ".dll:") || strings.Contains(extensions, ".exe:") || strings.Contains(extensions, ".jar:") {
		score += 35
		hits = append(hits, "binary-heavy")
	}
	return score, strings.Join(hits, "; ")
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	sum := sha1.Sum([]byte(text))
	return text[:limit] + "\n...[truncated " + hex.EncodeToString(sum[:4]) + "]"
}
