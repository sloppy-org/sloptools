package sleepconv

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// parseCodexActivity walks ~/.codex/sessions/YYYY/MM/DD/rollout-*.jsonl
// since the cutoff. Codex emits function_call events for shell, web,
// and patch operations; we map them onto the same Activity shape used
// for claude.
func parseCodexActivity(home string, since time.Time) Activity {
	root := filepath.Join(home, ".codex", "sessions")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Activity{}
	}
	var a Activity
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		stat, statErr := os.Stat(path)
		if statErr != nil || stat.ModTime().Before(since) {
			return nil
		}
		a = mergeActivity(a, walkCodexSession(path, since, home))
		return nil
	})
	return a
}

func walkCodexSession(path string, since time.Time, home string) Activity {
	f, err := os.Open(path)
	if err != nil {
		return Activity{}
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	digest := SessionDigest{Source: "codex"}
	var a Activity
	for dec.More() {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		etype := jsonString(raw["type"])
		if etype == "session_meta" {
			payload := raw["payload"]
			var meta map[string]json.RawMessage
			if err := json.Unmarshal(payload, &meta); err == nil {
				digest.ID = jsonString(meta["id"])
				digest.CWD = strings.TrimSpace(jsonString(meta["cwd"]))
			}
			continue
		}
		ts := codexEntryTimestamp(raw)
		if !ts.IsZero() {
			if ts.Before(since) {
				continue
			}
			if digest.Start.IsZero() || ts.Before(digest.Start) {
				digest.Start = ts
			}
			if ts.After(digest.End) {
				digest.End = ts
			}
		}
		if etype != "response_item" {
			continue
		}
		payload := raw["payload"]
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(payload, &inner); err != nil {
			continue
		}
		ptype := jsonString(inner["type"])
		switch ptype {
		case "message":
			role := jsonString(inner["role"])
			if role == "user" {
				if !codexFirstMessageIsAgentsPreamble(inner) {
					digest.UserTurns++
				}
			}
		case "function_call":
			a = mergeActivity(a, codexFunctionCall(inner, home, &digest))
		}
	}
	if digest.UserTurns > 0 || digest.ToolEvents > 0 {
		digest.Sphere = classifyPathSphere(digest.CWD, home)
		if digest.Sphere == "" || digest.Sphere == "skip" {
			digest.Sphere = SphereWork
		}
		a.Sessions = append(a.Sessions, digest)
	}
	return a
}

func codexFirstMessageIsAgentsPreamble(inner map[string]json.RawMessage) bool {
	content, ok := inner["content"]
	if !ok {
		return false
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(content, &arr); err != nil {
		return false
	}
	for _, b := range arr {
		if jsonString(b["type"]) != "input_text" {
			continue
		}
		text := jsonString(b["text"])
		if codexLooksLikeAgentsPreamble(text) {
			return true
		}
		break
	}
	return false
}

// codexFunctionCall handles the codex-side tool invocations:
//
//   - exec_command: shell command (bash equivalent); arguments has
//     {"cmd": "<command>", "workdir": "<cwd>"}.
//   - apply_patch: file edit; arguments has {"input": "*** Begin Patch
//     ..."}. Path extraction parses the patch header.
//   - web_fetch / web_search: web operations.
//   - view_image: image read; arguments has {"path": "..."}.
//   - write_stdin: writes to a previously-spawned process; we treat
//     the cwd of the parent as the active dir.
func codexFunctionCall(inner map[string]json.RawMessage, home string, digest *SessionDigest) Activity {
	name := jsonString(inner["name"])
	rawArgs := jsonString(inner["arguments"])
	if rawArgs == "" {
		return Activity{}
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return Activity{}
	}
	var a Activity
	switch name {
	case "exec_command":
		cmd := jsonString(args["cmd"])
		cat := classifyBashCategory(cmd)
		if cat != "" {
			cwd := jsonString(args["workdir"])
			sphere := classifyPathSphere(cwd, home)
			if sphere == "" || sphere == "skip" {
				sphere = digest.Sphere
			}
			a.BashHits = append(a.BashHits, BashHit{Command: cmd, Category: cat, Sphere: sphere})
			digest.ToolEvents++
		}
		// File touches inferred from common command patterns.
		for _, p := range extractPathsFromShellCommand(cmd, home) {
			if sphere := classifyPathSphere(p.Path, home); sphere != "" && sphere != "skip" {
				p.Sphere = sphere
				a.FilesTouched = append(a.FilesTouched, p)
				digest.ToolEvents++
			}
		}
	case "apply_patch":
		patch := jsonString(args["input"])
		for _, path := range parseApplyPatchPaths(patch) {
			if sphere := classifyPathSphere(path, home); sphere != "" && sphere != "skip" {
				a.FilesTouched = append(a.FilesTouched, FileTouch{Path: path, Op: "edit", Sphere: sphere})
				digest.ToolEvents++
			}
		}
	case "web_fetch":
		if u := jsonString(args["url"]); u != "" {
			a.WebFetches = append(a.WebFetches, WebFetchOp{URL: u, Intent: clipProseShort(jsonString(args["query"])), Sphere: classifyURLSphere(u)})
			digest.ToolEvents++
		}
	case "web_search":
		if q := jsonString(args["query"]); q != "" {
			a.Searches = append(a.Searches, SearchOp{Tool: "WebSearch", Query: q, Sphere: digest.Sphere})
			digest.ToolEvents++
		}
	case "view_image":
		if p := jsonString(args["path"]); p != "" {
			if sphere := classifyPathSphere(p, home); sphere != "" && sphere != "skip" {
				a.FilesTouched = append(a.FilesTouched, FileTouch{Path: p, Op: "read", Sphere: sphere})
				digest.ToolEvents++
			}
		}
	}
	return a
}

// extractPathsFromShellCommand scans `cat /path`, `head /path`,
// `tail /path`, `less /path`, `vim /path`, `wc /path`, `sed -i ... /path`
// patterns and yields the file path. Conservative: only first-token
// arguments that look like absolute paths qualify. This catches the
// codex-side reads that aren't through view_image.
func extractPathsFromShellCommand(cmd, home string) []FileTouch {
	out := []FileTouch{}
	tokens := strings.Fields(cmd)
	if len(tokens) == 0 {
		return nil
	}
	first := tokens[0]
	op := ""
	switch first {
	case "cat", "head", "tail", "less", "more", "wc", "file":
		op = "read"
	case "vim", "vi", "nano", "emacs":
		op = "edit"
	case "sed":
		// sed -i requires write; sed without -i is read-only.
		op = "read"
		for _, t := range tokens[1:] {
			if t == "-i" || strings.HasPrefix(t, "-i") {
				op = "edit"
				break
			}
		}
	default:
		return nil
	}
	for _, t := range tokens[1:] {
		if strings.HasPrefix(t, "-") {
			continue
		}
		if !strings.HasPrefix(t, "/") {
			continue
		}
		t = strings.Trim(t, "\"'`;|&")
		out = append(out, FileTouch{Path: t, Op: op})
	}
	_ = home
	return out
}

// parseApplyPatchPaths walks the codex apply_patch input and yields the
// file paths it touches. Format:
//
//	*** Begin Patch
//	*** Add File: <path>
//	*** Update File: <path>
//	*** Delete File: <path>
//	...
//	*** End Patch
func parseApplyPatchPaths(patch string) []string {
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{
			"*** Add File:",
			"*** Update File:",
			"*** Delete File:",
		} {
			if strings.HasPrefix(line, prefix) {
				p := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				if p != "" {
					paths = append(paths, p)
				}
			}
		}
	}
	return paths
}
