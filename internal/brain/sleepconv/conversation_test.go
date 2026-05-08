package sleepconv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStripHarnessMarkers_RemovesAllKnownBlocks(t *testing.T) {
	in := "real prose\n<system-reminder>\nharness reminder\n</system-reminder>\nmore prose\n<task-notification>\n<task-id>x</task-id>\n</task-notification>\ntail\n<command-name>/foo</command-name><command-args>bar</command-args><local-command-stdout>output</local-command-stdout><local-command-caveat>caveat</local-command-caveat><command-message>msg</command-message>"
	out := stripHarnessMarkers(in)
	for _, banned := range []string{
		"system-reminder", "task-notification", "command-name",
		"command-args", "local-command-stdout", "local-command-caveat",
		"command-message", "harness reminder", "<task-id>",
	} {
		if strings.Contains(out, banned) {
			t.Errorf("stripped output still contains %q: %q", banned, out)
		}
	}
	for _, kept := range []string{"real prose", "more prose", "tail"} {
		if !strings.Contains(out, kept) {
			t.Errorf("stripped output dropped real prose %q: %q", kept, out)
		}
	}
}

func TestProseTooThin(t *testing.T) {
	cases := []struct {
		in   string
		thin bool
	}{
		{"", true},
		{"   \n  \t", true},
		{"abc", true},
		{"abcd", true},
		{"abcde", false},
		{"yes the build", false},
		{"  ok  ", true},
	}
	for _, c := range cases {
		got := proseTooThin(c.in)
		if got != c.thin {
			t.Errorf("proseTooThin(%q) = %v, want %v", c.in, got, c.thin)
		}
	}
}

func TestCodexLooksLikeAgentsPreamble(t *testing.T) {
	cases := map[string]bool{
		"# AGENTS.md instructions for /home/ert/Nextcloud\n\n<INSTRUCTIONS>\n": true,
		"<INSTRUCTIONS>\n# Agent rules\n":                                      true,
		"\n\n# AGENTS.md instructions for /home/ert/code\n":                    true,
		"normal user prompt asking a question":                                 false,
		"<turn_aborted>\nThe user interrupted</turn_aborted>":                  false,
	}
	for in, want := range cases {
		got := codexLooksLikeAgentsPreamble(in)
		if got != want {
			t.Errorf("codexLooksLikeAgentsPreamble(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCodexIsHarnessOnly(t *testing.T) {
	if !codexIsHarnessOnly("<turn_aborted>\nThe user interrupted the previous turn on purpose. \n</turn_aborted>") {
		t.Error("turn_aborted block should be classed harness-only")
	}
	if codexIsHarnessOnly("nononono there is a NEW jpeg. from today.") {
		t.Error("real prose should not be harness-only")
	}
}

func TestExplicitSphereTag(t *testing.T) {
	cases := map[string]string{
		"[sphere=work] do the thing":        SphereWork,
		"[ sphere = private ] do the thing": SpherePrivate,
		"[Sphere=Private] mixed case":       SpherePrivate,
		"no tag":                            "",
	}
	for in, want := range cases {
		got := explicitSphereTag(in)
		if got != want {
			t.Errorf("explicitSphereTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyPromptSphere_CWDPrimary(t *testing.T) {
	home := "/home/u"
	cases := []struct {
		cwd   string
		prose string
		want  string
	}{
		{"/home/u/Nextcloud", "anything", SphereWork},
		{"/home/u/Nextcloud/brain", "anything", SphereWork},
		{"/home/u/Dropbox", "anything", SpherePrivate},
		{"/home/u/Dropbox/brain", "anything", SpherePrivate},
		{"/home/u/code/sloppy", "anything", SphereWork},
		{"/home/u/code/itpplasma", "anything", SphereWork},
		{"/home/u/code/lazy-fortran", "anything", SpherePrivate},
		{"/home/u/code/krystophny", "anything", SpherePrivate},
		{"/home/u/data", "anything", SphereWork},
		{"/home/u", "asking about TU Graz course", SphereWork},
		{"/home/u", "Hetzner Kontoauszug for last month", SpherePrivate},
		{"/home/u", "no markers either way", SphereWork},
		{"/tmp/somewhere", "[sphere=private] override wins", SpherePrivate},
	}
	for _, c := range cases {
		got := classifyPromptSphere(Prompt{CWD: c.cwd, Prose: c.prose}, home)
		if got != c.want {
			t.Errorf("classify(cwd=%q, prose=%q) = %q, want %q", c.cwd, c.prose, got, c.want)
		}
	}
}

func TestFilterPersonalGuardrail_DropsByCWDAndProse(t *testing.T) {
	home := "/home/u"
	in := []Prompt{
		{CWD: "/home/u/Nextcloud/personal/banking", Prose: "ok"},
		{CWD: "/home/u/Nextcloud", Prose: "look at /home/u/Nextcloud/personal/foo for the receipt"},
		{CWD: "/home/u/code/sloppy", Prose: "real prose with no personal path"},
		{CWD: "/home/u/Nextcloud/personal", Prose: "exact root"},
	}
	out := filterPersonalGuardrail(in, home)
	if len(out) != 1 {
		t.Fatalf("want 1 surviving prompt, got %d: %+v", len(out), out)
	}
	if out[0].CWD != "/home/u/code/sloppy" {
		t.Fatalf("wrong survivor: %+v", out[0])
	}
}

func TestCapPrompts_DropsOldestUntilUnderLimits(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	prompts := []Prompt{
		{Timestamp: t0.Add(0 * time.Minute), Prose: strings.Repeat("a", 800)},
		{Timestamp: t0.Add(1 * time.Minute), Prose: strings.Repeat("b", 800)},
		{Timestamp: t0.Add(2 * time.Minute), Prose: strings.Repeat("c", 800)},
	}
	out := capPrompts(append([]Prompt(nil), prompts...), 10, 1700)
	if len(out) != 2 {
		t.Fatalf("byte cap 1700 with three 800-byte prompts should keep last two (1600 bytes), got %d", len(out))
	}
	if out[0].Prose[0] != 'b' || out[1].Prose[0] != 'c' {
		t.Fatalf("oldest should be dropped first, got prefixes %c%c", out[0].Prose[0], out[1].Prose[0])
	}
	out = capPrompts(append([]Prompt(nil), prompts...), 2, 1<<20)
	if len(out) != 2 || out[0].Prose[0] != 'b' {
		t.Fatalf("count cap should drop oldest, got %d, prefix %c", len(out), out[0].Prose[0])
	}
}

func TestParseClaudeJSONL_KeepsUserProse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.jsonl")
	body := strings.Join([]string{
		// Real user prose.
		`{"type":"user","isSidechain":false,"timestamp":"2026-05-08T01:00:00.000Z","cwd":"/home/u/Nextcloud","message":{"role":"user","content":"how do we handle X?"}}`,
		// Tool result, content is array — must be skipped.
		`{"type":"user","isSidechain":false,"timestamp":"2026-05-08T01:01:00.000Z","cwd":"/home/u/Nextcloud","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"x"}]}}`,
		// Sidechain — subagent dispatch, must be skipped.
		`{"type":"user","isSidechain":true,"timestamp":"2026-05-08T01:02:00.000Z","cwd":"/home/u/Nextcloud","message":{"role":"user","content":"sidechain instruction"}}`,
		// Wrapped in system-reminder only — empty after stripping, must be skipped.
		`{"type":"user","isSidechain":false,"timestamp":"2026-05-08T01:03:00.000Z","cwd":"/home/u/Nextcloud","message":{"role":"user","content":"<system-reminder>only this</system-reminder>"}}`,
		// Assistant message — must be skipped.
		`{"type":"assistant","timestamp":"2026-05-08T01:04:00.000Z","message":{"role":"assistant","content":"reply"}}`,
		// Mixed text + reminder — keep prose, drop reminder.
		`{"type":"user","isSidechain":false,"timestamp":"2026-05-08T01:05:00.000Z","cwd":"/home/u/Nextcloud","message":{"role":"user","content":"real prompt here\n<system-reminder>noise</system-reminder>"}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	got := parseClaudeJSONL(path, since)
	if len(got) != 2 {
		t.Fatalf("want 2 user prompts kept, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Prose, "how do we handle X") {
		t.Errorf("first prompt mismatched: %q", got[0].Prose)
	}
	if !strings.Contains(got[1].Prose, "real prompt here") {
		t.Errorf("second prompt mismatched: %q", got[1].Prose)
	}
	if strings.Contains(got[1].Prose, "noise") {
		t.Errorf("second prompt should have reminder stripped: %q", got[1].Prose)
	}
}

func TestParseCodexRollout_DropsAgentsPreambleAndTurnAborted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	body := strings.Join([]string{
		`{"timestamp":"2026-05-08T01:00:00.000Z","type":"session_meta","payload":{"id":"sess1","cwd":"/home/u/Nextcloud"}}`,
		// First user message: AGENTS.md auto-prepend, must be dropped.
		`{"timestamp":"2026-05-08T01:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /home/u/Nextcloud\n\n<INSTRUCTIONS>\nrules go here\n</INSTRUCTIONS>"}]}}`,
		// Real prompt.
		`{"timestamp":"2026-05-08T01:00:02.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"please rewrite the abstract"}]}}`,
		// Turn-aborted harness marker.
		`{"timestamp":"2026-05-08T01:00:03.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<turn_aborted>\nThe user interrupted the previous turn\n</turn_aborted>"}]}}`,
		// Assistant — skip.
		`{"timestamp":"2026-05-08T01:00:04.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"sure"}]}}`,
		// Another real prompt.
		`{"timestamp":"2026-05-08T01:00:05.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"now make it shorter"}]}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	got := parseCodexRollout(path, since)
	if len(got) != 2 {
		t.Fatalf("want 2 user prompts kept, got %d: %+v", len(got), got)
	}
	if got[0].Prose != "please rewrite the abstract" {
		t.Errorf("first prompt: %q", got[0].Prose)
	}
	if got[1].Prose != "now make it shorter" {
		t.Errorf("second prompt: %q", got[1].Prose)
	}
	for _, p := range got {
		if p.CWD != "/home/u/Nextcloud" {
			t.Errorf("cwd not propagated from session_meta: %q", p.CWD)
		}
		if p.SessionID != "sess1" {
			t.Errorf("session_id not propagated: %q", p.SessionID)
		}
	}
}

func TestBuildSleepConversations_EndToEnd_ClaudeAndCodex_RoutedBySphere(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects", "-home-u-Nextcloud"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects", "-home-u-Dropbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex", "sessions", "2026", "05", "08"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Claude session in Nextcloud (work).
	claudeWork := strings.Join([]string{
		`{"type":"user","isSidechain":false,"timestamp":"2026-05-08T01:00:00.000Z","cwd":"` + home + `/Nextcloud","message":{"role":"user","content":"draft the EUROfusion summary"}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, ".claude", "projects", "-home-u-Nextcloud", "s1.jsonl"), []byte(claudeWork), 0o644); err != nil {
		t.Fatal(err)
	}
	// Claude session in Dropbox (private).
	claudePriv := strings.Join([]string{
		`{"type":"user","isSidechain":false,"timestamp":"2026-05-08T02:00:00.000Z","cwd":"` + home + `/Dropbox","message":{"role":"user","content":"sort the family photos folder"}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, ".claude", "projects", "-home-u-Dropbox", "s2.jsonl"), []byte(claudePriv), 0o644); err != nil {
		t.Fatal(err)
	}
	// Codex session in Nextcloud/personal — must be dropped entirely.
	codexPersonal := strings.Join([]string{
		`{"timestamp":"2026-05-08T03:00:00.000Z","type":"session_meta","payload":{"id":"x","cwd":"` + home + `/Nextcloud/personal/eyes-only"}}`,
		`{"timestamp":"2026-05-08T03:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"never leave this filesystem"}]}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, ".codex", "sessions", "2026", "05", "08", "rollout-personal.jsonl"), []byte(codexPersonal), 0o644); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 8, 4, 0, 0, 0, time.UTC)

	work := Build(home, SphereWork, since, now)
	if work.Count != 1 {
		t.Fatalf("work sphere want 1 prompt, got %d (markdown: %q)", work.Count, work.Markdown)
	}
	if !strings.Contains(work.Markdown, "EUROfusion summary") {
		t.Errorf("work markdown missing prompt prose: %q", work.Markdown)
	}
	if strings.Contains(work.Markdown, "family photos") {
		t.Errorf("work markdown leaked private prompt: %q", work.Markdown)
	}
	if strings.Contains(work.Markdown, "never leave") {
		t.Errorf("work markdown leaked personal/ prompt: %q", work.Markdown)
	}

	priv := Build(home, SpherePrivate, since, now)
	if priv.Count != 1 {
		t.Fatalf("private sphere want 1 prompt, got %d (markdown: %q)", priv.Count, priv.Markdown)
	}
	if !strings.Contains(priv.Markdown, "family photos") {
		t.Errorf("private markdown missing prompt prose: %q", priv.Markdown)
	}
	if strings.Contains(priv.Markdown, "EUROfusion") {
		t.Errorf("private markdown leaked work prompt: %q", priv.Markdown)
	}
	if strings.Contains(priv.Markdown, "never leave") {
		t.Errorf("private markdown leaked personal/ prompt: %q", priv.Markdown)
	}

	// The reflect-not-execute preamble must be present whenever any
	// prompt survives the filters.
	if !strings.Contains(work.Markdown, "Do not execute") {
		t.Errorf("work markdown missing reflect-not-execute preamble: %q", work.Markdown)
	}
}
