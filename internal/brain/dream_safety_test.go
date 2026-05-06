package brain

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// gitInit lays down a brain-shaped git repo at root with a single root
// commit at commitTime. It uses --allow-empty so we don't have to seed a
// file before the commit (callers add files afterwards).
func gitInit(t *testing.T, root string, commitTime time.Time) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_AUTHOR_DATE="+commitTime.Format(time.RFC3339),
			"GIT_COMMITTER_DATE="+commitTime.Format(time.RFC3339),
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q", "-b", "main", root)
	run("git", "-C", root, "commit", "-q", "--allow-empty", "-m", "init")
}

// gitAddCommit stages everything at root and creates a follow-up commit.
func gitAddCommit(t *testing.T, root, msg string, commitTime time.Time) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_AUTHOR_DATE="+commitTime.Format(time.RFC3339),
			"GIT_COMMITTER_DATE="+commitTime.Format(time.RFC3339),
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "-C", root, "add", "-A")
	run("git", "-C", root, "commit", "-q", "-m", msg)
}

func TestGitRepoAgeNotARepo(t *testing.T) {
	root := t.TempDir()
	if _, ok := gitRepoAge(root); ok {
		t.Fatalf("gitRepoAge ok=true for non-repo, want false")
	}
}

func TestGitRepoAgeReturnsAge(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	gitInit(t, root, old)
	age, ok := gitRepoAge(root)
	if !ok {
		t.Fatalf("gitRepoAge ok=false, want true")
	}
	if age < 399*24*time.Hour || age > 401*24*time.Hour {
		t.Fatalf("age=%v, want ~400d", age)
	}
}

func TestGitFileTouchMap(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-400 * 24 * time.Hour)
	recent := time.Now().Add(-10 * 24 * time.Hour)
	gitInit(t, root, old)
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitAddCommit(t, root, "add a", old)
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitAddCommit(t, root, "add b", recent)
	touch, ok := gitFileTouchMap(root)
	if !ok {
		t.Fatalf("gitFileTouchMap ok=false")
	}
	if got := touch["a.md"]; got.Unix() != old.Unix() {
		t.Fatalf("a.md touch=%v, want %v", got, old)
	}
	if got := touch["b.md"]; got.Unix() != recent.Unix() {
		t.Fatalf("b.md touch=%v, want %v", got, recent)
	}
}

func TestLastTouchTimePicksMax(t *testing.T) {
	old := time.Now().Add(-500 * 24 * time.Hour)
	mid := time.Now().Add(-100 * 24 * time.Hour)
	new := time.Now().Add(-1 * 24 * time.Hour)
	cases := []struct {
		name string
		note *dreamNote
		want time.Time
	}{
		{"only mtime", &dreamNote{mtime: old}, old},
		{"git wins over mtime", &dreamNote{mtime: old, gitTouch: new}, new},
		{"last_seen wins", &dreamNote{lastSeen: new.Format(time.RFC3339), mtime: old}, new},
		{"max of three", &dreamNote{lastSeen: old.Format(time.RFC3339), mtime: mid, gitTouch: new}, new},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lastTouchTime(tc.note)
			if got.Unix() != tc.want.Unix() {
				t.Fatalf("lastTouchTime=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestDreamPruneLinksScanRepoAgeFloor(t *testing.T) {
	root := t.TempDir()
	brainRoot := filepath.Join(root, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Backdate file mtime so the source note is well past the cold
	// threshold by mtime alone. Without the repo-age floor, the link
	// would be pruned.
	old := time.Now().Add(-500 * 24 * time.Hour)
	writeDreamRaw(t, root, "brain/topics/source.md",
		"---\nkind: topic\ndisplay_name: Source\n---\n\n# Source\n\n[[topics/cold]]\n")
	writeDreamRaw(t, root, "brain/topics/cold.md",
		"---\nkind: topic\ndisplay_name: Cold\n---\n\n# Cold\n")
	for _, p := range []string{"brain/topics/source.md", "brain/topics/cold.md"} {
		if err := os.Chtimes(filepath.Join(root, p), old, old); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	// Initialise the brain repo with the oldest commit dated today —
	// the safety floor must trigger.
	gitInit(t, brainRoot, time.Now())

	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	cold, err := DreamPruneLinksScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("DreamPruneLinksScan: %v", err)
	}
	if len(cold) != 0 {
		t.Fatalf("cold=%d in fresh repo, want 0 (safety floor must trigger). cold=%v", len(cold), cold)
	}
}

func TestDreamPruneLinksScanWorksInOldRepo(t *testing.T) {
	root := t.TempDir()
	brainRoot := filepath.Join(root, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Source recent, target genuinely cold by mtime AND git history.
	old := time.Now().Add(-500 * 24 * time.Hour)
	recent := time.Now().Add(-10 * 24 * time.Hour)
	writeDreamRaw(t, root, "brain/topics/source.md",
		"---\nkind: topic\ndisplay_name: Source\n---\n\n# Source\n\n[[topics/cold]]\n")
	writeDreamRaw(t, root, "brain/topics/cold.md",
		"---\nkind: topic\ndisplay_name: Cold\n---\n\n# Cold\n")
	if err := os.Chtimes(filepath.Join(brainRoot, "topics/cold.md"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Chtimes(filepath.Join(brainRoot, "topics/source.md"), recent, recent); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	gitInit(t, brainRoot, time.Now().Add(-500*24*time.Hour))
	gitAddCommit(t, brainRoot, "import", old)
	if err := os.WriteFile(filepath.Join(brainRoot, "topics/source.md"),
		[]byte("---\nkind: topic\ndisplay_name: Source\n---\n\n# Source v2\n\n[[topics/cold]]\n"),
		0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitAddCommit(t, brainRoot, "touch source", recent)

	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	cold, err := DreamPruneLinksScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("DreamPruneLinksScan: %v", err)
	}
	if len(cold) != 1 {
		t.Fatalf("cold=%d, want 1; cold=%v", len(cold), cold)
	}
	if cold[0].Source != "topics/source.md" || cold[0].Target != "topics/cold.md" {
		t.Fatalf("cold[0]=%+v, want source/source target/cold", cold[0])
	}
}

func TestDreamPruneLinksApplyReturnsEditedPaths(t *testing.T) {
	root := t.TempDir()
	brainRoot := filepath.Join(root, "brain")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := time.Now().Add(-500 * 24 * time.Hour)
	writeDreamRaw(t, root, "brain/topics/source.md",
		"---\nkind: topic\ndisplay_name: Source\n---\n\n# Source\n\n[[topics/cold]]\n")
	writeDreamRaw(t, root, "brain/topics/cold.md",
		"---\nkind: topic\ndisplay_name: Cold\n---\n\n# Cold\n")
	if err := os.Chtimes(filepath.Join(brainRoot, "topics/cold.md"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	gitInit(t, brainRoot, time.Now().Add(-500*24*time.Hour))
	gitAddCommit(t, brainRoot, "import", old)

	cfg, err := NewConfig([]Vault{{Sphere: SphereWork, Root: root}})
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	cold, err := DreamPruneLinksScan(cfg, SphereWork)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	plan, err := BuildDreamPrunePlan(cfg, SphereWork, cold)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	summary, err := DreamPruneLinksApply(cfg, SphereWork, plan.Digest)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(summary.EditedPaths) != 1 {
		t.Fatalf("EditedPaths=%v, want 1 entry", summary.EditedPaths)
	}
	if summary.EditedPaths[0] != "brain/topics/source.md" {
		t.Fatalf("EditedPaths[0]=%q, want brain/topics/source.md", summary.EditedPaths[0])
	}
	if summary.FilesEdited != 1 {
		t.Fatalf("FilesEdited=%d, want 1", summary.FilesEdited)
	}
}
