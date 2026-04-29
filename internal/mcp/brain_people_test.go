package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrainPeopleDashboardAggregatesOpenLoops(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writePersonNote(t, tmp, "Ada Example", "# Ada Example\n")
	recent := time.Now().UTC().AddDate(0, 0, -3).Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339)
	writePeopleCommitment(t, tmp, "wait.md", "waiting", "Waiting for draft", "Ada Example", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "defer.md", "deferred", "Waiting for review", "Ada Example", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "owe.md", "next", "Send recommendation", "", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "inbox.md", "inbox", "Clarify scope", "", []string{"Ada Example"}, "", recent)
	writePeopleCommitment(t, tmp, "closed.md", "closed", "Submitted form", "", []string{"Ada Example"}, recent, recent)
	writePeopleCommitment(t, tmp, "old.md", "closed", "Old context", "", []string{"Ada Example"}, old, old)
	writePeopleCommitment(t, tmp, "other.md", "next", "Other person task", "", []string{"Other Person"}, "", recent)

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.people.dashboard", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada",
	})
	if err != nil {
		t.Fatalf("brain.people.dashboard: %v", err)
	}
	if got["person"].(string) != "Ada Example" {
		t.Fatalf("person = %v, want Ada Example", got["person"])
	}
	assertPeopleLoopCount(t, got, "waiting_on_them", 2)
	assertPeopleLoopCount(t, got, "i_owe_them", 2)
	assertPeopleLoopCount(t, got, "recently_closed", 1)
}

func TestBrainPeopleDashboardResolvesFoldedParentheticalPerson(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writePersonNote(t, tmp, "Zoë Example (Lab)", "# Zoe Example\n")
	writePeopleCommitment(t, tmp, "owe.md", "next", "Send outline", "", []string{"Zoe Example"}, "", "")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.people.dashboard", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Zoe",
	})
	if err != nil {
		t.Fatalf("brain.people.dashboard folded name: %v", err)
	}
	if got["person"].(string) != "Zoë Example (Lab)" {
		t.Fatalf("person = %v, want folded parenthetical match", got["person"])
	}
	assertPeopleLoopCount(t, got, "i_owe_them", 1)
}

func TestBrainPeopleRenderReplacesOnlyCurrentOpenLoops(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	personPath := writePersonNote(t, tmp, "Ada Example", "# Ada Example\n\nIntro.\n\n## Current open loops\nold\n\n## Notes\nKeep me.\n")
	writePeopleCommitment(t, tmp, "wait.md", "waiting", "Waiting for draft", "Ada Example", []string{"Ada Example"}, "", "")
	writePeopleCommitment(t, tmp, "owe.md", "next", "Send recommendation", "", []string{"Ada Example"}, "", "")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.people.render", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada Example",
	})
	if err != nil {
		t.Fatalf("brain.people.render: %v", err)
	}
	if got["changed"] != true {
		t.Fatalf("changed = %v, want true", got["changed"])
	}
	rendered := readPeopleFile(t, personPath)
	if !strings.Contains(rendered, "## Current open loops\n\n### Waiting on them\n- [Waiting for draft](../gtd/wait.md)") {
		t.Fatalf("rendered missing waiting section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "### I owe them\n- [Send recommendation](../gtd/owe.md)") {
		t.Fatalf("rendered missing owed section:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Intro.\n\n## Current open loops") || !strings.Contains(rendered, "## Notes\nKeep me.\n") {
		t.Fatalf("render changed unrelated note content:\n%s", rendered)
	}
	firstInfo := statPeopleFile(t, personPath)
	second, err := s.callTool("brain.people.render", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada Example",
	})
	if err != nil {
		t.Fatalf("second brain.people.render: %v", err)
	}
	if second["changed"] != false {
		t.Fatalf("second changed = %v, want false", second["changed"])
	}
	if statPeopleFile(t, personPath).ModTime() != firstInfo.ModTime() {
		t.Fatal("idempotent render changed mtime")
	}
}

func TestBrainPeopleRenderEmptyAndMissingPerson(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	personPath := writePersonNote(t, tmp, "Ada Example", "# Ada Example\n")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.people.render", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Ada Example",
	})
	if err != nil {
		t.Fatalf("brain.people.render empty: %v", err)
	}
	if got["changed"] != true {
		t.Fatalf("changed = %v, want true", got["changed"])
	}
	if !strings.Contains(readPeopleFile(t, personPath), "## Current open loops\n\n_None at present._\n") {
		t.Fatalf("missing empty state:\n%s", readPeopleFile(t, personPath))
	}
	missing, err := s.callTool("brain.people.render", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"name":        "Missing Person",
	})
	if err != nil {
		t.Fatalf("missing person render returned error: %v", err)
	}
	diags, _ := missing["diagnostics"].([]string)
	if len(diags) != 1 || diags[0] != "needs_person_note: Missing Person" {
		t.Fatalf("missing person diagnostics = %#v", missing["diagnostics"])
	}
	if missing["changed"] != false {
		t.Fatalf("missing person changed = %v, want false", missing["changed"])
	}
}

func assertPeopleLoopCount(t *testing.T, got map[string]interface{}, key string, want int) {
	t.Helper()
	items, ok := got[key].([]personOpenLoop)
	if !ok {
		t.Fatalf("%s type = %T", key, got[key])
	}
	if len(items) != want {
		t.Fatalf("%s count = %d, want %d: %#v", key, len(items), want, items)
	}
}

func writePersonNote(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, "work", "brain", "people", name+".md")
	writeMCPBrainFile(t, path, content)
	return path
}

func writePeopleCommitment(t *testing.T, root, name, status, title, waitingFor string, people []string, closedAt, evidenceAt string) {
	t.Helper()
	body := "---\nkind: commitment\nsphere: work\nstatus: " + status + "\ntitle: " + title + "\noutcome: " + title + "\ncontext: test\n"
	if status == "next" {
		body += "next_action: " + title + "\n"
	}
	if status == "deferred" {
		body += "follow_up: 2026-05-01\n"
	}
	if waitingFor != "" {
		body += "waiting_for: " + waitingFor + "\n"
	}
	if len(people) > 0 {
		body += "people:\n"
		for _, person := range people {
			body += "  - " + person + "\n"
		}
	}
	if evidenceAt != "" {
		body += "last_evidence_at: " + evidenceAt + "\n"
	}
	if closedAt != "" {
		body += "local_overlay:\n  closed_at: " + closedAt + "\n"
	}
	body += "source_bindings:\n  - provider: manual\n    ref: " + name + "\n---\nBody.\n"
	writeMCPBrainFile(t, filepath.Join(root, "work", "brain", "gtd", name), body)
}

func readPeopleFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func statPeopleFile(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}
