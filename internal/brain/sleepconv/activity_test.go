package sleepconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGitOp_Commit(t *testing.T) {
	cases := []struct {
		cmd     string
		op      string
		subject string
	}{
		{`git commit -m "brain night: scout fixes"`, "commit", "brain night: scout fixes"},
		{`git push`, "push", ""},
		{`git log --oneline -5`, "log", ""},
		{`gh issue list --repo sloppy-org/sloptools`, "", ""},
		{`git status`, "status", ""},
	}
	for _, c := range cases {
		got := parseGitOp(c.cmd)
		if got.Op != c.op {
			t.Errorf("parseGitOp(%q).Op = %q, want %q", c.cmd, got.Op, c.op)
		}
		if got.Subject != c.subject {
			t.Errorf("parseGitOp(%q).Subject = %q, want %q", c.cmd, got.Subject, c.subject)
		}
	}
}

func TestParseGHRef(t *testing.T) {
	cases := []struct {
		cmd    string
		owner  string
		repo   string
		number int
		kind   string
	}{
		{`gh issue list --repo sloppy-org/sloptools --state open`, "sloppy-org", "sloptools", 0, "issue"},
		{`gh pr view sloppy-org/helpy#80`, "sloppy-org", "helpy", 80, "pull"},
		{`gh repo view itpplasma/NEO-RT`, "itpplasma", "NEO-RT", 0, "repo"},
		{`gh issue view 13`, "", "", 0, "issue"}, // no owner/repo in args; kind set
	}
	for _, c := range cases {
		got := parseGHRef(c.cmd)
		if got.Owner != c.owner || got.Repo != c.repo || got.Number != c.number {
			t.Errorf("parseGHRef(%q) = %+v, want owner=%q repo=%q number=%d", c.cmd, got, c.owner, c.repo, c.number)
		}
		if got.Kind != c.kind {
			t.Errorf("parseGHRef(%q).Kind = %q, want %q", c.cmd, got.Kind, c.kind)
		}
	}
}

func TestClassifyBashCategory(t *testing.T) {
	cases := map[string]string{
		"git commit -m 'x'":         "git",
		"gh issue list":             "gh",
		"sloptools brain night":     "sloptools",
		"go test ./...":             "build",
		"pytest":                    "test",
		"ls -la":                    "",
		"cd /tmp && pwd":            "",
		"cat file.txt":              "",
		"some-other-command --flag": "other",
	}
	for cmd, want := range cases {
		if got := classifyBashCategory(cmd); got != want {
			t.Errorf("classifyBashCategory(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestClassifyPathSphere_Boundaries(t *testing.T) {
	home := "/home/u"
	cases := []struct {
		path string
		want string
	}{
		{"/home/u/Nextcloud/brain/people/X.md", SphereWork},
		{"/home/u/Nextcloud/personal/anything", "skip"},
		{"/home/u/Dropbox/finanzen/X", SpherePrivate},
		{"/home/u/code/sloppy/sloptools/file.go", SphereWork},
		{"/home/u/code/itpplasma/NEO-RT/x.f90", SphereWork},
		{"/home/u/code/lazy-fortran/parser/x.f90", SpherePrivate},
		{"/home/u/code/krystophny/dotfiles/x", SpherePrivate},
		{"/home/u/data/AUG/x.h5", SphereWork},
		{"/home/u/.claude/projects/x.jsonl", "skip"},
		{"/home/u/.codex/sessions/x.jsonl", "skip"},
		{"/tmp/x", "skip"},
		{"/home/u/elsewhere", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := classifyPathSphere(c.path, home); got != c.want {
			t.Errorf("classifyPathSphere(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestExtractPathsFromShellCommand(t *testing.T) {
	got := extractPathsFromShellCommand("cat /home/u/code/foo.go", "/home/u")
	if len(got) != 1 || got[0].Path != "/home/u/code/foo.go" || got[0].Op != "read" {
		t.Errorf("cat path: %+v", got)
	}
	got = extractPathsFromShellCommand("sed -i 's/x/y/' /home/u/file.go", "/home/u")
	if len(got) != 1 || got[0].Path != "/home/u/file.go" || got[0].Op != "edit" {
		t.Errorf("sed -i path: %+v", got)
	}
	got = extractPathsFromShellCommand("ls -la", "/home/u")
	if len(got) != 0 {
		t.Errorf("ls should yield no path: %+v", got)
	}
}

func TestParseApplyPatchPaths(t *testing.T) {
	patch := `*** Begin Patch
*** Update File: /home/u/code/foo.go
@@ ...
*** Add File: /home/u/code/bar.go
*** Delete File: /home/u/code/baz.go
*** End Patch`
	got := parseApplyPatchPaths(patch)
	want := []string{"/home/u/code/foo.go", "/home/u/code/bar.go", "/home/u/code/baz.go"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("path %d: got %q want %q", i, got[i], p)
		}
	}
}

func TestBuildActivity_EndToEnd_ClaudeAssistantToolUse(t *testing.T) {
	home := t.TempDir()
	// Plant a claude session with a Read, Edit, Bash(git), WebFetch.
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects", "-home-u-code-sloppy"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","cwd":"` + home + `/code/sloppy/sloptools","timestamp":"2026-05-08T10:00:00.000Z","message":{"role":"user","content":"fix the picker"}}`,
		`{"type":"assistant","cwd":"` + home + `/code/sloppy/sloptools","timestamp":"2026-05-08T10:00:30.000Z","message":{"role":"assistant","content":[` +
			`{"type":"tool_use","name":"Read","input":{"file_path":"` + home + `/code/sloppy/sloptools/internal/brain/scout/picker.go"}},` +
			`{"type":"tool_use","name":"Edit","input":{"file_path":"` + home + `/code/sloppy/sloptools/internal/brain/scout/picker.go"}},` +
			`{"type":"tool_use","name":"Bash","input":{"command":"git commit -m \"scout: drop cadence\""}},` +
			`{"type":"tool_use","name":"WebFetch","input":{"url":"https://arxiv.org/abs/2504.19413","prompt":"Mem0 paper"}}` +
			`]}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, ".claude", "projects", "-home-u-code-sloppy", "s1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "code", "sloppy"), 0o755); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	a := BuildActivity(home, SphereWork, since, now)
	if len(a.Sessions) != 1 {
		t.Fatalf("want 1 session, got %d (%+v)", len(a.Sessions), a.Sessions)
	}
	if len(a.FilesTouched) != 1 {
		t.Fatalf("want 1 unique file touch (read+edit consolidated), got %d (%+v)", len(a.FilesTouched), a.FilesTouched)
	}
	if a.FilesTouched[0].Op != "edit" {
		t.Errorf("want op=edit (write>edit>read rank), got %q", a.FilesTouched[0].Op)
	}
	if a.FilesTouched[0].Sphere != SphereWork {
		t.Errorf("want sphere=work, got %q", a.FilesTouched[0].Sphere)
	}
	if len(a.GitOps) != 1 {
		t.Fatalf("want 1 git op, got %d (%+v)", len(a.GitOps), a.GitOps)
	}
	if a.GitOps[0].Op != "commit" || !strings.Contains(a.GitOps[0].Subject, "scout") {
		t.Errorf("git commit op malformed: %+v", a.GitOps[0])
	}
	if len(a.WebFetches) != 1 || !strings.Contains(a.WebFetches[0].URL, "arxiv") {
		t.Errorf("web fetch missing or malformed: %+v", a.WebFetches)
	}
}

func TestRenderActivitySection_HasContentEditMandate(t *testing.T) {
	a := Activity{
		Sessions:     []SessionDigest{{Source: "claude", CWD: "/home/u/code/sloppy", UserTurns: 3, ToolEvents: 5, Sphere: SphereWork}},
		FilesTouched: []FileTouch{{Path: "/home/u/code/sloppy/sloptools/x.go", Op: "edit", Sphere: SphereWork, Sessions: 2}},
		GitOps:       []GitOp{{Op: "commit", Subject: "test commit", Sphere: SphereWork}},
	}
	out := RenderActivitySection(a, nil, nil)
	for _, want := range []string{
		"## Activity since previous sleep",
		"Modify content",
		"EDIT IT IN PLACE",
		"Anti-feedback",
		"Sessions (1)",
		"Files touched (1)",
		"test commit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered packet missing %q:\n---\n%s\n---", want, out)
		}
	}
}

func TestBrainIndex_LookupFolder(t *testing.T) {
	brainRoot := t.TempDir()
	mustWrite(t, filepath.Join(brainRoot, "folders", "code", "sloppy", "sloptools.md"), "# sloptools\n")
	mustWrite(t, filepath.Join(brainRoot, "folders", "code", "sloppy.md"), "# sloppy umbrella\n")
	mustWrite(t, filepath.Join(brainRoot, "people", "Sebastian Riepl.md"), "# Sebastian Riepl\n")
	idx := NewBrainIndex(brainRoot)

	// Exact folder hit.
	got := idx.LookupFolder("/home/u/code/sloppy/sloptools/internal/brain/scout/picker.go", []string{"/home/u"})
	if got != "folders/code/sloppy/sloptools.md" {
		t.Errorf("nested folder lookup: %q", got)
	}
	// Falls back to ancestor.
	got = idx.LookupFolder("/home/u/code/sloppy/some-other-thing/x.go", []string{"/home/u"})
	if got != "folders/code/sloppy.md" {
		t.Errorf("ancestor folder lookup: %q", got)
	}
	// No match.
	got = idx.LookupFolder("/home/u/code/lazy-fortran/x.f90", []string{"/home/u"})
	if got != "" {
		t.Errorf("no-match should return empty: %q", got)
	}
	// Name lookup.
	path, kind := idx.LookupName("Sebastian Riepl")
	if kind != "people" || path != "people/Sebastian Riepl.md" {
		t.Errorf("name lookup: path=%q kind=%q", path, kind)
	}
}
