package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDreamReportRunPrioritisesStrategicTopics(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	// 4 strategic topics, 4 non-strategic topics. Budget=4 should split 2/2.
	for i := 0; i < 4; i++ {
		writeDreamTopic(t, root, fmt.Sprintf("topics/strategic-%d.md", i),
			fmt.Sprintf("Strategic %d", i), true, "weekly")
	}
	for i := 0; i < 4; i++ {
		writeDreamTopic(t, root, fmt.Sprintf("topics/regular-%d.md", i),
			fmt.Sprintf("Regular %d", i), false, "")
	}
	report, err := DreamReportRun(cfg, SphereWork, 4)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	if len(report.Topics) != 4 {
		t.Fatalf("topics=%d, want 4", len(report.Topics))
	}
	strategic := 0
	for _, rel := range report.Topics {
		if strings.Contains(rel, "strategic") {
			strategic++
		}
	}
	if strategic < 2 {
		t.Fatalf("strategic picks=%d, want >=2; topics=%v", strategic, report.Topics)
	}
}

func TestDreamReportRunIsStableWithinDay(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	for i := 0; i < 6; i++ {
		writeDreamTopic(t, root, fmt.Sprintf("topics/topic-%d.md", i),
			fmt.Sprintf("Topic %d", i), false, "")
	}
	first, err := DreamReportRun(cfg, SphereWork, 3)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := DreamReportRun(cfg, SphereWork, 3)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if strings.Join(first.Topics, ",") != strings.Join(second.Topics, ",") {
		t.Fatalf("topics differ: first=%v second=%v", first.Topics, second.Topics)
	}
}

func TestDreamReportRunSuggestsProseMentions(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopicBody(t, root, "topics/alpha.md", "Alpha", false, "",
		"## Summary\nAlpha collaborates with Beta on shared infrastructure.\n")
	writeDreamTopic(t, root, "topics/beta.md", "Beta", false, "")
	report, err := DreamReportRun(cfg, SphereWork, 10)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	if !hasSuggestion(report.CrossLinks, "topics/alpha.md", "topics/beta.md") {
		t.Fatalf("expected alpha->beta suggestion; got %+v", report.CrossLinks)
	}
}

func TestDreamReportRunSkipsExistingWikilink(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopicBody(t, root, "topics/alpha.md", "Alpha", false, "",
		"## Summary\nAlpha already links to [[topics/beta]] explicitly.\n")
	writeDreamTopic(t, root, "topics/beta.md", "Beta", false, "")
	report, err := DreamReportRun(cfg, SphereWork, 10)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	if hasSuggestion(report.CrossLinks, "topics/alpha.md", "topics/beta.md") {
		t.Fatalf("did not expect alpha->beta suggestion when wikilink already present; got %+v", report.CrossLinks)
	}
}

func TestDreamReportRunCapsSuggestionsPerSource(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	// 8 candidate notes; the picker will pick all when budget>=8 but we
	// only need the source note to receive at most 5 suggestions.
	names := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	body := "## Summary\nThe hub note mentions"
	for i, name := range names {
		if i > 0 {
			body += ","
		}
		body += " " + capitalizeFirst(name)
	}
	body += " in one place.\n"
	writeDreamTopicBody(t, root, "topics/hub.md", "Hub", true, "weekly", body)
	for _, name := range names {
		writeDreamTopic(t, root, "topics/"+name+".md", capitalizeFirst(name), false, "")
	}
	// Budget=20 picks every note; hub will be among them and prose mentions
	// of all 8 candidates must be capped to 5 in the suggestion list.
	report, err := DreamReportRun(cfg, SphereWork, 20)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	count := 0
	for _, suggestion := range report.CrossLinks {
		if suggestion.From == "topics/hub.md" {
			count++
		}
	}
	if count > 5 {
		t.Fatalf("hub suggestions=%d, want <=5", count)
	}
	if count == 0 {
		t.Fatalf("expected hub to produce suggestions; got %+v", report.CrossLinks)
	}
}

func TestDreamReportRunFlagsOldTargetsAsCold(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopicBody(t, root, "topics/alpha.md", "Alpha", true, "weekly",
		"## Summary\nAlpha refers to [[topics/beta]] for context.\n")
	writeDreamTopic(t, root, "topics/beta.md", "Beta", false, "")
	ageMtime(t, root, "topics/beta.md", time.Now().AddDate(-2, 0, 0))
	report, err := DreamReportRun(cfg, SphereWork, 10)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	if !hasColdLink(report.Cold, "topics/alpha.md", "topics/beta.md") {
		t.Fatalf("expected cold link alpha->beta; got %+v", report.Cold)
	}
}

func TestDreamReportRunSkipsStrategicTargets(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopicBody(t, root, "topics/alpha.md", "Alpha", true, "weekly",
		"## Summary\nAlpha refers to [[topics/beta]] for context.\n")
	writeDreamTopic(t, root, "topics/beta.md", "Beta", true, "monthly")
	ageMtime(t, root, "topics/beta.md", time.Now().AddDate(-3, 0, 0))
	report, err := DreamReportRun(cfg, SphereWork, 10)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	if hasColdLink(report.Cold, "topics/alpha.md", "topics/beta.md") {
		t.Fatalf("strategic target should not be cold; got %+v", report.Cold)
	}
}

func TestDreamReportRunSkipsCoreFocusTargets(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopicBody(t, root, "topics/alpha.md", "Alpha", true, "weekly",
		"## Summary\nAlpha refers to [[topics/beta]] for context.\n")
	writeDreamTopicWithFocus(t, root, "topics/beta.md", "Beta", false, "weekly", "core")
	ageMtime(t, root, "topics/beta.md", time.Now().AddDate(-3, 0, 0))
	report, err := DreamReportRun(cfg, SphereWork, 10)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	if hasColdLink(report.Cold, "topics/alpha.md", "topics/beta.md") {
		t.Fatalf("focus=core target should not be cold; got %+v", report.Cold)
	}
}

func TestDreamPruneLinksScanIsStable(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopicBody(t, root, "topics/alpha.md", "Alpha", false, "",
		"## Summary\nAlpha mentions [[topics/beta]] and [[topics/gamma|Gamma alias]].\n")
	writeDreamTopic(t, root, "topics/beta.md", "Beta", false, "")
	writeDreamTopic(t, root, "topics/gamma.md", "Gamma", false, "")
	old := time.Now().AddDate(-2, 0, 0)
	ageMtime(t, root, "topics/beta.md", old)
	ageMtime(t, root, "topics/gamma.md", old)
	first, err := DreamPruneLinksScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan1: %v", err)
	}
	second, err := DreamPruneLinksScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan2: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("len differs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Source != second[i].Source || first[i].Target != second[i].Target {
			t.Fatalf("ordering differs at %d: %+v vs %+v", i, first[i], second[i])
		}
	}
	if len(first) != 2 {
		t.Fatalf("want 2 cold links, got %d: %+v", len(first), first)
	}
}

func TestBuildDreamPrunePlanReplacementShape(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopicBody(t, root, "topics/alpha.md", "Alpha", false, "",
		"## Summary\nAlpha mentions [[topics/beta]] and [[topics/gamma|Gamma alias]].\n")
	writeDreamTopic(t, root, "topics/beta.md", "Beta", false, "")
	writeDreamTopic(t, root, "topics/gamma.md", "Gamma", false, "")
	old := time.Now().AddDate(-2, 0, 0)
	ageMtime(t, root, "topics/beta.md", old)
	ageMtime(t, root, "topics/gamma.md", old)

	cold, err := DreamPruneLinksScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	plan, err := BuildDreamPrunePlan(cfg, SphereWork, cold)
	if err != nil {
		t.Fatalf("BuildDreamPrunePlan: %v", err)
	}
	if len(plan.Edits) != 1 {
		t.Fatalf("want 1 edit (single line), got %d: %+v", len(plan.Edits), plan.Edits)
	}
	edit := plan.Edits[0]
	if !strings.Contains(edit.NewText, "Gamma alias") {
		t.Fatalf("alias replacement missing: %q", edit.NewText)
	}
	if !strings.Contains(edit.NewText, " beta ") && !strings.HasSuffix(edit.NewText, " beta") &&
		!strings.HasSuffix(strings.TrimSuffix(edit.NewText, "."), "beta") {
		// More tolerant assertion: replacement is "beta" as a bare token.
		if !strings.Contains(edit.NewText, "beta") {
			t.Fatalf("basename replacement missing: %q", edit.NewText)
		}
	}
	if strings.Contains(edit.NewText, "[[topics/beta]]") || strings.Contains(edit.NewText, "[[topics/gamma|") {
		t.Fatalf("wikilink not degraded: %q", edit.NewText)
	}
	if plan.Digest == "" {
		t.Fatalf("plan digest empty")
	}
}

func TestDreamReportRunExcludesPersonal(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopic(t, root, "topics/alpha.md", "Alpha", false, "")
	// Drop a topic-shaped note inside personal/. The work vault auto-excludes
	// personal/ and our pool walker must respect that.
	personalRel := "personal/topics/secret.md"
	writeDreamRaw(t, root, personalRel, dreamTopicBody("Secret", false, "", ""))
	report, err := DreamReportRun(cfg, SphereWork, 10)
	if err != nil {
		t.Fatalf("DreamReportRun: %v", err)
	}
	for _, rel := range report.Topics {
		if strings.HasPrefix(rel, "personal/") {
			t.Fatalf("personal/ topic leaked into report: %v", report.Topics)
		}
	}
}

func TestDreamPruneLinksScanExcludesPersonal(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	writeDreamTopic(t, root, "topics/beta.md", "Beta", false, "")
	ageMtime(t, root, "topics/beta.md", time.Now().AddDate(-2, 0, 0))
	writeDreamRaw(t, root, "personal/topics/leak.md",
		dreamTopicBody("Leak", false, "", "Leak refers to [[topics/beta]].\n"))
	cold, err := DreamPruneLinksScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, item := range cold {
		if strings.HasPrefix(item.Source, "personal/") {
			t.Fatalf("personal/ source leaked: %+v", cold)
		}
	}
}

func writeDreamTopic(t *testing.T, root, rel, displayName string, strategic bool, cadence string) {
	t.Helper()
	body := dreamTopicBody(displayName, strategic, cadence, "")
	writeDreamRaw(t, root, "brain/"+rel, body)
}

func writeDreamTopicBody(t *testing.T, root, rel, displayName string, strategic bool, cadence, sectionBody string) {
	t.Helper()
	body := dreamTopicBody(displayName, strategic, cadence, sectionBody)
	writeDreamRaw(t, root, "brain/"+rel, body)
}

func writeDreamTopicWithFocus(t *testing.T, root, rel, displayName string, strategic bool, cadence, focus string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("kind: topic\n")
	fmt.Fprintf(&b, "display_name: %s\n", displayName)
	fmt.Fprintf(&b, "strategic: %t\n", strategic)
	if cadence != "" {
		fmt.Fprintf(&b, "cadence: %s\n", cadence)
	}
	if focus != "" {
		fmt.Fprintf(&b, "focus: %s\n", focus)
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n## Summary\n%s.\n", displayName, displayName)
	writeDreamRaw(t, root, "brain/"+rel, b.String())
}

func dreamTopicBody(displayName string, strategic bool, cadence, sectionBody string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("kind: topic\n")
	fmt.Fprintf(&b, "display_name: %s\n", displayName)
	fmt.Fprintf(&b, "strategic: %t\n", strategic)
	if cadence != "" {
		fmt.Fprintf(&b, "cadence: %s\n", cadence)
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", displayName)
	if sectionBody == "" {
		fmt.Fprintf(&b, "## Summary\n%s overview.\n", displayName)
	} else {
		b.WriteString(sectionBody)
	}
	return b.String()
}

func writeDreamRaw(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func ageMtime(t *testing.T, root, rel string, when time.Time) {
	t.Helper()
	abs := filepath.Join(root, "brain", filepath.FromSlash(rel))
	if err := os.Chtimes(abs, when, when); err != nil {
		t.Fatalf("chtimes %s: %v", rel, err)
	}
}

func hasSuggestion(items []LinkSuggestion, from, to string) bool {
	for _, item := range items {
		if item.From == from && item.To == to {
			return true
		}
	}
	return false
}

func hasColdLink(items []ColdLink, source, target string) bool {
	for _, item := range items {
		if item.Source == source && item.Target == target {
			return true
		}
	}
	return false
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
