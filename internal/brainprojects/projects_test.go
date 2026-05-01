package brainprojects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
)

func TestRenderHubReplacesOnlyOpenLoopsAndIsIdempotent(t *testing.T) {
	root, cfg := projectTestConfig(t)
	hub := filepath.Join(root, "work", "brain", "projects", "Alpha.md")
	writeProjectTestFile(t, hub, "# Alpha\n\nIntro.\n\n## Open Loops\nold\n\n## Notes\nKeep me.\n")
	writeProjectCommitment(t, root, "ask.md", "next", "Ask for alpha budget", "", "2026-05-03", "", "")
	writeProjectCommitment(t, root, "wait.md", "waiting", "Review alpha plan", "Ada Example", "", "2026-05-05", "")
	writeProjectCommitment(t, root, "closed.md", "closed", "Filed alpha report", "", "", "", "2026-04-25T10:00:00Z")
	writeProjectCommitment(t, root, "other.md", "next", "Other project", "", "", "", "")

	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	got, err := RenderHub(cfg, brain.SphereWork, "brain/projects/Alpha.md", now)
	if err != nil {
		t.Fatalf("RenderHub: %v", err)
	}
	if !got.Changed {
		t.Fatal("RenderHub changed = false, want true")
	}
	rendered := readProjectTestFile(t, hub)
	for _, want := range []string{
		"Intro.\n\n## Open Loops\n",
		"### Chris owes\n- [ ] [[commitments/ask|Ask for alpha budget]] — due 2026-05-03",
		"### Waiting on others\n- [ ] [[commitments/wait|Review alpha plan]] — [[people/Ada Example|Ada Example]] — follow up 2026-05-05",
		"### Closed (last 14 days)\n- [x] [[commitments/closed|Filed alpha report]] — closed 2026-04-25",
		"## Notes\nKeep me.\n",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered missing %q:\n%s", want, rendered)
		}
	}
	info := statProjectTestFile(t, hub)
	second, err := RenderHub(cfg, brain.SphereWork, "brain/projects/Alpha.md", now)
	if err != nil {
		t.Fatalf("second RenderHub: %v", err)
	}
	if second.Changed {
		t.Fatal("second RenderHub changed = true, want false")
	}
	if !statProjectTestFile(t, hub).ModTime().Equal(info.ModTime()) {
		t.Fatal("idempotent RenderHub changed mtime")
	}
}

func TestListHubsCountsCommitmentBuckets(t *testing.T) {
	root, cfg := projectTestConfig(t)
	writeProjectTestFile(t, filepath.Join(root, "work", "brain", "projects", "Alpha.md"), "# Alpha\n")
	writeProjectTestFile(t, filepath.Join(root, "work", "brain", "projects", "Beta.md"), "# Beta\n")
	writeProjectCommitment(t, root, "next.md", "next", "Alpha next", "", "", "", "")
	writeProjectCommitment(t, root, "wait.md", "waiting", "Alpha waiting", "Ada Example", "", "", "")
	writeProjectCommitment(t, root, "closed.md", "closed", "Alpha closed", "", "", "", "2026-03-01")
	writeProjectCommitmentWithProject(t, root, "beta.md", "next", "Beta next", "[[projects/Beta]]")

	got, err := ListHubs(cfg, brain.SphereWork, time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ListHubs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("hub count = %d, want 2: %#v", len(got), got)
	}
	alpha := got[0]
	if alpha.Path != "brain/projects/Alpha.md" {
		t.Fatalf("first hub = %s, want Alpha", alpha.Path)
	}
	if alpha.Counts.Next != 1 || alpha.Counts.Waiting != 1 || alpha.Counts.Closed != 1 {
		t.Fatalf("alpha counts = %#v", alpha.Counts)
	}
	if got[1].Counts.Next != 1 {
		t.Fatalf("beta counts = %#v", got[1].Counts)
	}
}

func TestBulkLinkAppliesRulesAndReportsAmbiguity(t *testing.T) {
	root, cfg := projectTestConfig(t)
	writeProjectTestFile(t, filepath.Join(root, "work", "brain", "projects", "Alpha.md"), "# Alpha\n")
	writeProjectTestFile(t, filepath.Join(root, "work", "brain", "projects", "Beta.md"), "# Beta\n")
	writeUnlinkedCommitment(t, root, "person.md", "Discuss budget", []string{"Ada Example"})
	writeUnlinkedCommitment(t, root, "keyword.md", "alpha budget planning", nil)
	writeUnlinkedCommitment(t, root, "ambiguous.md", "alpha beta overlap", nil)
	writeProjectCommitmentWithProject(t, root, "linked.md", "next", "Already linked", "[[projects/Alpha]]")
	rules := filepath.Join(root, "projects.toml")
	writeProjectTestFile(t, rules, `
[project.alpha]
hub = "brain/projects/Alpha.md"
match.people = ["Ada Example"]
match.keywords = ["alpha budget", "alpha beta"]

[project.beta]
hub = "brain/projects/Beta.md"
match.keywords = ["alpha beta"]
`)

	got, err := BulkLink(cfg, brain.SphereWork, rules)
	if err != nil {
		t.Fatalf("BulkLink: %v", err)
	}
	if got.Linked != 2 || got.Skipped != 2 || len(got.Ambiguous) != 1 {
		t.Fatalf("BulkLink result = %#v", got)
	}
	assertProjectField(t, filepath.Join(root, "work", "brain", "commitments", "person.md"), "[[projects/Alpha]]")
	assertProjectField(t, filepath.Join(root, "work", "brain", "commitments", "keyword.md"), "[[projects/Alpha]]")
	assertProjectField(t, filepath.Join(root, "work", "brain", "commitments", "ambiguous.md"), "")
}

func projectTestConfig(t *testing.T) (string, *brain.Config) {
	t.Helper()
	root := t.TempDir()
	cfg, err := brain.NewConfig([]brain.Vault{{Sphere: brain.SphereWork, Root: filepath.Join(root, "work"), Brain: "brain"}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	return root, cfg
}

func writeProjectCommitment(t *testing.T, root, name, status, title, waitingFor, due, followUp, closedAt string) {
	t.Helper()
	writeProjectCommitmentWithProject(t, root, name, status, title, "[[projects/Alpha]]")
	path := filepath.Join(root, "work", "brain", "commitments", name)
	data := readProjectTestFile(t, path)
	if waitingFor != "" {
		data = strings.Replace(data, "context: test\n", "context: test\nwaiting_for: "+waitingFor+"\n", 1)
	}
	if due != "" {
		data = strings.Replace(data, "context: test\n", "context: test\ndue: "+due+"\n", 1)
	}
	if followUp != "" {
		data = strings.Replace(data, "context: test\n", "context: test\nfollow_up: "+followUp+"\n", 1)
	}
	if closedAt != "" {
		data = strings.Replace(data, "---\n# ", "local_overlay:\n  closed_at: "+closedAt+"\n---\n# ", 1)
	}
	writeProjectTestFile(t, path, data)
}

func writeProjectCommitmentWithProject(t *testing.T, root, name, status, title, project string) {
	t.Helper()
	body := "---\nkind: commitment\nsphere: work\nstatus: " + status + "\ntitle: " + title + "\noutcome: " + title + "\ncontext: test\n"
	if project != "" {
		body += "project: " + project + "\n"
	}
	body += "---\n# " + title + "\n"
	writeProjectTestFile(t, filepath.Join(root, "work", "brain", "commitments", name), body)
}

func writeUnlinkedCommitment(t *testing.T, root, name, title string, people []string) {
	t.Helper()
	body := "---\nkind: commitment\nsphere: work\nstatus: next\ntitle: " + title + "\noutcome: " + title + "\ncontext: test\n"
	if len(people) > 0 {
		body += "people:\n"
		for _, person := range people {
			body += "  - " + person + "\n"
		}
	}
	body += "---\n# " + title + "\n"
	writeProjectTestFile(t, filepath.Join(root, "work", "brain", "commitments", name), body)
}

func assertProjectField(t *testing.T, path, want string) {
	t.Helper()
	commitment, _, diags := braingtd.ParseCommitmentMarkdown(readProjectTestFile(t, path))
	if len(diags) != 0 {
		t.Fatalf("parse %s: %#v", path, diags)
	}
	if commitment.Project != want {
		t.Fatalf("%s project = %q, want %q", path, commitment.Project, want)
	}
}

func writeProjectTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readProjectTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func statProjectTestFile(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info
}
