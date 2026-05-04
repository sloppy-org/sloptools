package brain

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type RelationCandidate struct {
	Type         string       `json:"type"`
	SourceKind   string       `json:"source_kind"`
	Source       string       `json:"source"`
	TargetKind   string       `json:"target_kind"`
	Target       string       `json:"target"`
	EvidenceNote ResolvedPath `json:"evidence_note"`
	Folder       string       `json:"folder,omitempty"`
	Status       string       `json:"status"`
	Context      string       `json:"context"`
}

type RuntimeUnit struct {
	Vault               string `json:"vault"`
	UnitRoot            string `json:"unit_root"`
	Status              string `json:"status"`
	Exists              bool   `json:"exists"`
	ExistingNotes       int    `json:"existing_notes"`
	EstimatedModelCalls int    `json:"estimated_model_calls"`
	Files               int    `json:"files"`
}

type RuntimePlan struct {
	Slots               int           `json:"slots"`
	PendingUnits        int           `json:"pending_units"`
	EstimatedModelCalls int           `json:"estimated_model_calls"`
	Units               []RuntimeUnit `json:"units"`
}

type FinalReport struct {
	FolderNotes            int `json:"folder_notes"`
	CommitmentNotes        int `json:"commitment_notes"`
	WorkUnits              int `json:"work_units"`
	WorkUnitSuggestions    int `json:"work_unit_suggestions"`
	DiscordDMRows          int `json:"discord_dm_rows"`
	DiscordChannelRows     int `json:"discord_channel_rows"`
	LinkedInConnectionRows int `json:"linkedin_connection_rows"`
	LinkedInImportedRows   int `json:"linkedin_imported_contact_rows"`
	LinkedInMessageRows    int `json:"linkedin_message_rows"`
}

var relationTriggerPattern = regexp.MustCompile(`(?i)\b(supervis(?:e[sd]?|ion)|co-?author(?:ed|ship)?|collaborat(?:e[sd]?|ion)|affiliat(?:ed|ion)|member(?:ship)?|taught|teaching|review(?:ed)?|organ(?:ized|ised|ization|isation)|uses?|depends? on|funded by)\b`)

func RelationCandidates(cfg *Config, sphere Sphere) ([]RelationCandidate, error) {
	seen := map[string]struct{}{}
	var rows []RelationCandidate
	err := WalkVaultNotes(cfg, sphere, func(snap NoteSnapshot) error {
		if snap.Kind != "folder" || snap.Note == nil {
			return nil
		}
		folder, _ := ParseFolderNote(snap.Body)
		entities := folderEntities(folder)
		if len(entities) < 2 {
			return nil
		}
		text := folderRelationText(snap.Note)
		for _, match := range relationTriggerPattern.FindAllString(text, -1) {
			rtype := relationType(match)
			for _, dst := range entities[1:minInt(len(entities), 4)] {
				src := entities[0]
				key := strings.Join([]string{rtype, src.kind, src.name, dst.kind, dst.name, snap.Source.Rel}, "\x00")
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				rows = append(rows, RelationCandidate{
					Type: rtype, SourceKind: src.kind, Source: src.name,
					TargetKind: dst.kind, Target: dst.name, EvidenceNote: snap.Source,
					Folder: folder.SourceFolder, Status: "candidate", Context: match,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		left := []string{a.Type, strings.ToLower(a.Source), strings.ToLower(a.Target), a.Folder}
		right := []string{b.Type, strings.ToLower(b.Source), strings.ToLower(b.Target), b.Folder}
		for idx := range left {
			if left[idx] != right[idx] {
				return left[idx] < right[idx]
			}
		}
		return false
	})
	return rows, nil
}

func RuntimeEstimate(root string, cfg *Config, slots int) (RuntimePlan, error) {
	if slots < 1 {
		slots = 1
	}
	path := filepath.Join(root, "data", "folder", "work_units.tsv")
	rows, err := readTSV(path)
	if err != nil {
		return RuntimePlan{Slots: slots}, err
	}
	units := make([]RuntimeUnit, 0, len(rows))
	for _, row := range rows {
		unit := RuntimeUnit{
			Vault: row["vault"], UnitRoot: row["unit_root"], Status: defaultString(row["status"], "pending"),
			Files: atoiDefault(row["processable_files"], 0),
		}
		if vault, ok := vaultByName(cfg, row["vault"]); ok {
			rootPath := filepath.Join(vault.Root, filepath.FromSlash(unit.UnitRoot))
			unit.Exists = pathExists(rootPath)
			unit.ExistingNotes = countFolderNotesForRoot(vault, unit.UnitRoot)
		}
		unit.EstimatedModelCalls = maxInt(0, atoiDefault(row["folders"], unit.ExistingNotes)-unit.ExistingNotes)
		if unit.EstimatedModelCalls == 0 && unit.Status != "complete" {
			unit.EstimatedModelCalls = 1
		}
		units = append(units, unit)
	}
	plan := RuntimePlan{Slots: slots, Units: units}
	for _, unit := range units {
		if unit.Status == "complete" {
			continue
		}
		plan.PendingUnits++
		plan.EstimatedModelCalls += unit.EstimatedModelCalls
	}
	return plan, nil
}

func BuildFinalReport(root string, cfg *Config) FinalReport {
	report := FinalReport{
		WorkUnits:              countTSVRows(filepath.Join(root, "data", "folder", "work_units.tsv")),
		WorkUnitSuggestions:    countTSVRows(filepath.Join(root, "data", "folder", "work_unit_suggestions.tsv")),
		DiscordDMRows:          countJSONL(filepath.Join(root, "data", "discord", "dm_results.jsonl")),
		DiscordChannelRows:     countJSONL(filepath.Join(root, "data", "discord", "server_channel_results.jsonl")),
		LinkedInConnectionRows: countJSONL(filepath.Join(root, "data", "linkedin", "connections_triage.jsonl")),
		LinkedInImportedRows:   countJSONL(filepath.Join(root, "data", "linkedin_imported_contacts_triage.jsonl")),
		LinkedInMessageRows:    countJSONL(filepath.Join(root, "data", "linkedin", "messages_results.jsonl")),
	}
	for _, vault := range cfg.Vaults {
		report.FolderNotes += countMarkdown(filepath.Join(vault.BrainRoot(), "folders"))
		report.CommitmentNotes += countMarkdown(filepath.Join(vault.BrainRoot(), "commitments"))
	}
	return report
}

func OpencodeTextFromEvent(line string) string {
	var event map[string]interface{}
	if json.Unmarshal([]byte(line), &event) != nil || event["type"] != "text" {
		return ""
	}
	part, ok := event["part"].(map[string]interface{})
	if !ok {
		return ""
	}
	text, _ := part["text"].(string)
	return text
}

func OpencodeProgressFromEvent(line string) string {
	var event map[string]interface{}
	if json.Unmarshal([]byte(line), &event) != nil || event["type"] != "tool_use" {
		return ""
	}
	part, ok := event["part"].(map[string]interface{})
	if !ok {
		return ""
	}
	state, ok := part["state"].(map[string]interface{})
	if !ok || state["status"] != "completed" {
		return ""
	}
	title, _ := state["title"].(string)
	title = strings.Join(strings.Fields(title), " ")
	if len(title) > 180 {
		title = title[:177] + "..."
	}
	tool, _ := part["tool"].(string)
	if tool == "" {
		tool = "tool"
	}
	return fmt.Sprintf("\n- progress: `%s` %s\n", tool, title)
}

func StreamOpencodeReport(input io.Reader, report io.Writer, events io.Writer) error {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		if _, err := events.Write([]byte(line)); err != nil {
			return err
		}
		if text := OpencodeTextFromEvent(line); text != "" {
			if _, err := report.Write([]byte(text)); err != nil {
				return err
			}
			continue
		}
		if progress := OpencodeProgressFromEvent(line); progress != "" {
			if _, err := report.Write([]byte(progress)); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

type relationEntity struct{ kind, name string }

func folderEntities(folder FolderNote) []relationEntity {
	var out []relationEntity
	for _, item := range folder.People {
		out = append(out, relationEntity{"person", item})
	}
	for _, item := range folder.Institutions {
		out = append(out, relationEntity{"institution", item})
	}
	for _, item := range folder.Projects {
		out = append(out, relationEntity{"project", item})
	}
	for _, item := range folder.Topics {
		out = append(out, relationEntity{"topic", item})
	}
	return out
}

func folderRelationText(note *MarkdownNote) string {
	var parts []string
	for _, name := range []string{"Summary", "Key Facts", "Notes", "Open Questions"} {
		if section, ok := note.Section(name); ok {
			parts = append(parts, section.Body)
		}
	}
	return strings.Join(parts, "\n")
}

func relationType(text string) string {
	lowered := strings.ToLower(text)
	switch {
	case strings.Contains(lowered, "supervis"):
		return "supervises"
	case strings.Contains(lowered, "author"):
		return "coauthored"
	case strings.Contains(lowered, "collaborat"):
		return "collaborated_with"
	case strings.Contains(lowered, "affiliat"):
		return "affiliated_with"
	case strings.Contains(lowered, "member"):
		return "member_of"
	case strings.Contains(lowered, "taught") || strings.Contains(lowered, "teaching"):
		return "taught"
	case strings.Contains(lowered, "review"):
		return "reviewed"
	case strings.Contains(lowered, "organ"):
		return "organized"
	case strings.Contains(lowered, "depend"):
		return "depends_on"
	case strings.Contains(lowered, "funded by"):
		return "funded_by"
	case regexp.MustCompile(`\buses?\b`).MatchString(lowered):
		return "uses"
	default:
		return "related_to"
	}
}

func readTSV(path string) ([]map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	rows, err := reader.ReadAll()
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	out := make([]map[string]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		item := map[string]string{}
		for i, name := range rows[0] {
			if i < len(row) {
				item[name] = row[i]
			}
		}
		out = append(out, item)
	}
	return out, nil
}

func vaultByName(cfg *Config, name string) (Vault, bool) {
	for _, vault := range cfg.Vaults {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case string(vault.Sphere), vault.Label:
			return vault, true
		case "nextcloud":
			if vault.Sphere == SphereWork {
				return vault, true
			}
		case "dropbox":
			if vault.Sphere == SpherePrivate {
				return vault, true
			}
		}
	}
	return Vault{}, false
}

func countFolderNotesForRoot(vault Vault, root string) int {
	prefix := strings.Trim(filepath.ToSlash(root), "/")
	count := 0
	_ = filepath.WalkDir(filepath.Join(vault.BrainRoot(), "folders"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" || d.Name() == "index.md" {
			return nil
		}
		rel, err := filepath.Rel(filepath.Join(vault.BrainRoot(), "folders"), path)
		if err != nil {
			return nil
		}
		source := strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		if source == prefix || strings.HasPrefix(source, prefix+"/") {
			count++
		}
		return nil
	})
	return count
}

func countMarkdown(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ".md" && d.Name() != "README.md" {
			count++
		}
		return nil
	})
	return count
}

func countTSVRows(path string) int {
	rows, err := readTSV(path)
	if err != nil {
		return 0
	}
	return len(rows)
}

func countJSONL(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func atoiDefault(value string, fallback int) int {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return fallback
	}
	return number
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
