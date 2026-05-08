package sleepconv

import (
	"sort"
	"strings"
	"time"
)

// Activity is the structured day's work pulled from interactive Claude
// Code and Codex CLI session logs since the previous sleep commit.
// Unlike the user-prompt extraction in conversation.go, this captures
// the substantive technical context: which files were read or edited,
// which web pages were fetched, which git/gh/sloptools commands ran,
// which subagents were dispatched. This is the analogue of "today's
// hippocampal experience" the sleep stage is meant to consolidate
// into the canonical brain notes.
type Activity struct {
	Sessions     []SessionDigest
	FilesTouched []FileTouch
	WebFetches   []WebFetchOp
	Searches     []SearchOp
	BashHits     []BashHit
	SubAgents    []SubAgentDispatch
	GitOps       []GitOp
	GitHubRefs   []GitHubRef
}

// SessionDigest collapses one claude or codex session into the
// metadata the sleep stage cares about.
type SessionDigest struct {
	ID         string
	Source     string // "claude" | "codex"
	CWD        string
	Sphere     string
	Start      time.Time
	End        time.Time
	UserTurns  int
	ToolEvents int
}

// FileTouch records a Read/Edit/Write of a file. Op is the strongest
// observed verb across the day (write > edit > read).
type FileTouch struct {
	Path       string
	Op         string // "read" | "edit" | "write"
	Sphere     string
	Sessions   int    // count of sessions that touched this file
	BrainHit   string // brain-relative path of an existing folder/project note that governs this path, empty when none
	Importance int    // 1-10 deterministic score; higher means more worth promoting to canonical
}

// WebFetchOp captures a WebFetch (claude) or web_fetch (codex) call.
type WebFetchOp struct {
	URL    string
	Intent string // the prompt/instruction passed alongside, trimmed
	Sphere string
	Hits   int
}

// SearchOp captures Grep / WebSearch queries. Tool tells which shape.
type SearchOp struct {
	Tool   string // "Grep" | "WebSearch" | "Glob"
	Query  string
	Sphere string
}

// BashHit captures notable shell commands. Cosmetic mundane stuff
// like `ls`, `cat`, `cd` is dropped; categorised commands surface.
type BashHit struct {
	Command  string
	Category string // "git" | "gh" | "sloptools" | "build" | "test" | "other"
	Sphere   string
}

// SubAgentDispatch records when an Agent / Task tool was invoked.
type SubAgentDispatch struct {
	Type        string
	Description string
	Sphere      string
}

// GitOp is a `git` invocation parsed out of the bash stream. Subject
// captures the commit subject for `git commit -m "..."` lines.
type GitOp struct {
	Op      string // "commit" | "push" | "log" | "diff" | "stash" | "checkout" | other
	Subject string
	Sphere  string
}

// GitHubRef is any github.com URL or `gh` command that names an
// org/repo and optionally an issue or PR number.
type GitHubRef struct {
	Owner  string
	Repo   string
	Number int    // 0 when not a specific issue/PR
	Kind   string // "issue" | "pull" | "repo"
	Sphere string
}

// BuildActivity walks the same session files as Build but extracts the
// structured tool-call stream rather than just user-typed prose.
// The two are complementary: prompts say what the user asked for,
// activity says what actually happened.
func BuildActivity(home string, sphere string, since, now time.Time) Activity {
	if home == "" || since.IsZero() {
		return Activity{}
	}
	a := Activity{}
	a = mergeActivity(a, parseClaudeActivity(home, since))
	a = mergeActivity(a, parseCodexActivity(home, since))
	a = filterActivityByPersonal(a, home)
	a = filterActivityBySphere(a, sphere, home)
	a = consolidateActivity(a)
	return a
}

func mergeActivity(a, b Activity) Activity {
	a.Sessions = append(a.Sessions, b.Sessions...)
	a.FilesTouched = append(a.FilesTouched, b.FilesTouched...)
	a.WebFetches = append(a.WebFetches, b.WebFetches...)
	a.Searches = append(a.Searches, b.Searches...)
	a.BashHits = append(a.BashHits, b.BashHits...)
	a.SubAgents = append(a.SubAgents, b.SubAgents...)
	return a
}

// consolidateActivity dedupes per-file/per-URL records, escalates op
// strength (write > edit > read), counts sessions, derives GitOps and
// GitHubRefs from the bash stream, and scores each FileTouch by a
// deterministic importance rule so the renderer can sort by signal.
func consolidateActivity(a Activity) Activity {
	a.FilesTouched = consolidateFiles(a.FilesTouched)
	a.WebFetches = consolidateFetches(a.WebFetches)
	a.GitOps, a.GitHubRefs = deriveGitAndGitHub(a.BashHits)
	a.FilesTouched = scoreFileImportance(a.FilesTouched)
	return a
}

// scoreFileImportance assigns a 1-10 importance score to each
// FileTouch using deterministic rules:
//
//   - op=write           +6 (creating a new file is a strong signal)
//   - op=edit            +4 (modifying an existing file is strong)
//   - op=read            +1 (just reading is weak)
//   - sessions ≥ 3       +2 (recurring across sessions is stronger)
//   - sessions == 2      +1
//   - existing brain note governs path  +1 (canonical-note candidate)
//   - path under brain/  -3 (brain self-reads, anti-feedback rule)
//
// The score is clamped to [1, 10]. The renderer uses it to sort
// rows so the model encounters the highest-importance edits first.
func scoreFileImportance(in []FileTouch) []FileTouch {
	for i := range in {
		s := 0
		switch in[i].Op {
		case "write":
			s += 6
		case "edit":
			s += 4
		case "read":
			s += 1
		}
		switch {
		case in[i].Sessions >= 3:
			s += 2
		case in[i].Sessions == 2:
			s += 1
		}
		if in[i].BrainHit != "" {
			s += 1
		}
		if isBrainSelfRead(in[i].Path) {
			s -= 3
		}
		if s < 1 {
			s = 1
		}
		if s > 10 {
			s = 10
		}
		in[i].Importance = s
	}
	return in
}

// isBrainSelfRead is a cheap path-prefix check used by the importance
// scorer; the activity already routed `personal/` paths to drop, so
// here we only need to detect canonical brain reads.
func isBrainSelfRead(path string) bool {
	return strings.Contains(path, "/Nextcloud/brain/") || strings.Contains(path, "/Dropbox/brain/")
}

func consolidateFiles(in []FileTouch) []FileTouch {
	rank := map[string]int{"read": 1, "edit": 2, "write": 3}
	merged := map[string]*FileTouch{}
	for _, f := range in {
		key := f.Path
		if cur, ok := merged[key]; ok {
			if rank[f.Op] > rank[cur.Op] {
				cur.Op = f.Op
			}
			cur.Sessions++
			continue
		}
		copy := f
		copy.Sessions = 1
		merged[key] = &copy
	}
	out := make([]FileTouch, 0, len(merged))
	for _, v := range merged {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		if rank[out[i].Op] != rank[out[j].Op] {
			return rank[out[i].Op] > rank[out[j].Op]
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func consolidateFetches(in []WebFetchOp) []WebFetchOp {
	merged := map[string]*WebFetchOp{}
	for _, f := range in {
		if cur, ok := merged[f.URL]; ok {
			cur.Hits++
			if cur.Intent == "" {
				cur.Intent = f.Intent
			}
			continue
		}
		copy := f
		copy.Hits = 1
		merged[f.URL] = &copy
	}
	out := make([]WebFetchOp, 0, len(merged))
	for _, v := range merged {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Hits != out[j].Hits {
			return out[i].Hits > out[j].Hits
		}
		return out[i].URL < out[j].URL
	})
	return out
}

// deriveGitAndGitHub scans BashHits for `git` and `gh` commands and
// produces structured records. Subjects for `git commit -m "<x>"`
// are extracted; org/repo for `gh issue list <owner>/<repo>` etc.
func deriveGitAndGitHub(hits []BashHit) ([]GitOp, []GitHubRef) {
	var gits []GitOp
	var ghs []GitHubRef
	for _, h := range hits {
		switch h.Category {
		case "git":
			if op := parseGitOp(h.Command); op.Op != "" {
				op.Sphere = h.Sphere
				gits = append(gits, op)
			}
		case "gh":
			if ref := parseGHRef(h.Command); ref.Owner != "" {
				ref.Sphere = h.Sphere
				ghs = append(ghs, ref)
			}
		}
	}
	return dedupeGitOps(gits), dedupeGHRefs(ghs)
}

func dedupeGitOps(in []GitOp) []GitOp {
	seen := map[string]bool{}
	out := in[:0]
	for _, op := range in {
		key := op.Op + "|" + op.Subject + "|" + op.Sphere
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, op)
	}
	return out
}

func dedupeGHRefs(in []GitHubRef) []GitHubRef {
	seen := map[string]bool{}
	out := in[:0]
	for _, r := range in {
		key := r.Owner + "/" + r.Repo + "/" + r.Kind
		if r.Number > 0 {
			key += "#" + strings.TrimSpace(strings.Repeat(" ", 0))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// classifyBashCategory tags a shell command into a coarse bucket so
// the day's bash stream isn't a wall of `ls` and `cat` lines. Drops
// purely-cosmetic verbs (returning "" tells the caller to skip).
func classifyBashCategory(cmd string) string {
	c := strings.TrimSpace(cmd)
	first := firstShellWord(c)
	switch first {
	case "git":
		return "git"
	case "gh":
		return "gh"
	case "glab":
		return "gh"
	case "sloptools", "helpy", "slopshell":
		return "sloptools"
	case "go", "cargo", "make", "cmake", "ninja", "fpm", "npm", "pnpm", "yarn":
		return "build"
	case "pytest", "jest":
		return "test"
	case "ls", "cd", "pwd", "cat", "head", "tail", "wc", "stat", "file", "find", "echo", "date", "which", "type":
		return ""
	}
	return "other"
}

func firstShellWord(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '|' || r == ';' || r == '&' {
			return s[:i]
		}
	}
	return s
}

// classifyPathSphere maps an absolute path to a sphere using the same
// rules as classifyPromptSphere, plus a "skip" return value for paths
// under .claude/, .codex/, .cache/, .local/, and (only when not inside
// $HOME) /tmp, /var, /proc, /sys.
func classifyPathSphere(path, home string) string {
	if path == "" || !strings.HasPrefix(path, "/") {
		return ""
	}
	personal := home + "/Nextcloud/personal"
	if path == personal || strings.HasPrefix(path, personal+"/") {
		return "skip"
	}
	switch {
	case underHome(path, home, "Nextcloud"):
		return SphereWork
	case underHome(path, home, "Dropbox"):
		return SpherePrivate
	case underHome(path, home, "code", "sloppy"),
		underHome(path, home, "code", "itpplasma"),
		underHome(path, home, "data"):
		return SphereWork
	case underHome(path, home, "code", "lazy-fortran"),
		underHome(path, home, "code", "krystophny"):
		return SpherePrivate
	}
	for _, skip := range []string{
		home + "/.claude", home + "/.codex",
		home + "/.cache", home + "/.local",
	} {
		if path == skip || strings.HasPrefix(path, skip+"/") {
			return "skip"
		}
	}
	if !strings.HasPrefix(path, home+"/") {
		for _, skip := range []string{"/tmp", "/var", "/proc", "/sys"} {
			if path == skip || strings.HasPrefix(path, skip+"/") {
				return "skip"
			}
		}
	}
	return ""
}

// filterActivityByPersonal drops any record whose path or URL touches
// Nextcloud/personal/ — same guardrail as the prompt filter.
func filterActivityByPersonal(a Activity, home string) Activity {
	personal := home + "/Nextcloud/personal"
	keepFile := func(f FileTouch) bool {
		return !strings.Contains(f.Path, personal)
	}
	a.FilesTouched = filterSlice(a.FilesTouched, keepFile)
	keepBash := func(b BashHit) bool {
		return !strings.Contains(b.Command, "Nextcloud/personal/")
	}
	a.BashHits = filterSlice(a.BashHits, keepBash)
	return a
}

// filterActivityBySphere keeps only records routed to the requested
// sphere. Records with sphere=="" or sphere=="skip" are dropped.
func filterActivityBySphere(a Activity, want string, home string) Activity {
	a.Sessions = filterSlice(a.Sessions, func(s SessionDigest) bool { return s.Sphere == want })
	a.FilesTouched = filterSlice(a.FilesTouched, func(f FileTouch) bool { return f.Sphere == want })
	a.WebFetches = filterSlice(a.WebFetches, func(f WebFetchOp) bool { return f.Sphere == "" || f.Sphere == want })
	a.Searches = filterSlice(a.Searches, func(s SearchOp) bool { return s.Sphere == "" || s.Sphere == want })
	a.BashHits = filterSlice(a.BashHits, func(b BashHit) bool { return b.Sphere == "" || b.Sphere == want })
	a.SubAgents = filterSlice(a.SubAgents, func(s SubAgentDispatch) bool { return s.Sphere == "" || s.Sphere == want })
	return a
}

func filterSlice[T any](in []T, keep func(T) bool) []T {
	out := in[:0]
	for _, x := range in {
		if keep(x) {
			out = append(out, x)
		}
	}
	return out
}
