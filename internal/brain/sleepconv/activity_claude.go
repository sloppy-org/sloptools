package sleepconv

import (
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// parseClaudeActivity walks ~/.claude/projects/<encoded>/<session>.jsonl
// since the cutoff and extracts an Activity record. The traversal is
// resilient to mid-line decode errors: a malformed event ends parsing
// of that session but does not bubble up.
func parseClaudeActivity(home string, since time.Time) Activity {
	root := filepath.Join(home, ".claude", "projects")
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
		a = mergeActivity(a, walkClaudeSession(path, since, home))
		return nil
	})
	return a
}

func walkClaudeSession(path string, since time.Time, home string) Activity {
	f, err := os.Open(path)
	if err != nil {
		return Activity{}
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.UseNumber()
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	digest := SessionDigest{
		ID:     sessionID,
		Source: "claude",
	}
	var a Activity
	for dec.More() {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		if cwd := jsonString(raw["cwd"]); cwd != "" && digest.CWD == "" {
			digest.CWD = cwd
		}
		if ts := claudeEntryTimestamp(raw); !ts.IsZero() {
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
		etype := jsonString(raw["type"])
		switch etype {
		case "user":
			if claudeEntryIsUserText(raw) {
				digest.UserTurns++
			}
		case "assistant":
			a = mergeActivity(a, walkClaudeAssistant(raw, home, &digest))
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

// walkClaudeAssistant pulls tool_use blocks out of an assistant turn's
// content array. Each Read/Edit/Write/Bash/WebFetch/WebSearch/Grep/
// Agent block is converted to the matching Activity record.
func walkClaudeAssistant(raw map[string]json.RawMessage, home string, digest *SessionDigest) Activity {
	msg, ok := raw["message"]
	if !ok {
		return Activity{}
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(msg, &inner); err != nil {
		return Activity{}
	}
	content, ok := inner["content"]
	if !ok {
		return Activity{}
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil {
		return Activity{}
	}
	var a Activity
	for _, b := range blocks {
		if jsonString(b["type"]) != "tool_use" {
			continue
		}
		name := jsonString(b["name"])
		var input map[string]json.RawMessage
		if raw, ok := b["input"]; ok {
			_ = json.Unmarshal(raw, &input)
		}
		switch name {
		case "Read":
			if path := jsonString(input["file_path"]); path != "" {
				if sphere := classifyPathSphere(path, home); sphere != "" && sphere != "skip" {
					a.FilesTouched = append(a.FilesTouched, FileTouch{Path: path, Op: "read", Sphere: sphere})
					digest.ToolEvents++
				}
			}
		case "Edit", "NotebookEdit":
			if path := jsonString(input["file_path"]); path != "" {
				if sphere := classifyPathSphere(path, home); sphere != "" && sphere != "skip" {
					a.FilesTouched = append(a.FilesTouched, FileTouch{Path: path, Op: "edit", Sphere: sphere})
					digest.ToolEvents++
				}
			}
		case "Write":
			if path := jsonString(input["file_path"]); path != "" {
				if sphere := classifyPathSphere(path, home); sphere != "" && sphere != "skip" {
					a.FilesTouched = append(a.FilesTouched, FileTouch{Path: path, Op: "write", Sphere: sphere})
					digest.ToolEvents++
				}
			}
		case "Bash":
			cmd := jsonString(input["command"])
			cat := classifyBashCategory(cmd)
			if cat == "" {
				continue
			}
			a.BashHits = append(a.BashHits, BashHit{Command: cmd, Category: cat, Sphere: digest.Sphere})
			digest.ToolEvents++
		case "WebFetch":
			if u := jsonString(input["url"]); u != "" {
				a.WebFetches = append(a.WebFetches, WebFetchOp{URL: u, Intent: clipProseShort(jsonString(input["prompt"])), Sphere: classifyURLSphere(u)})
				digest.ToolEvents++
			}
		case "WebSearch":
			if q := jsonString(input["query"]); q != "" {
				a.Searches = append(a.Searches, SearchOp{Tool: "WebSearch", Query: q, Sphere: digest.Sphere})
				digest.ToolEvents++
			}
		case "Grep":
			q := jsonString(input["pattern"])
			if q != "" {
				a.Searches = append(a.Searches, SearchOp{Tool: "Grep", Query: q, Sphere: digest.Sphere})
				digest.ToolEvents++
			}
		case "Glob":
			q := jsonString(input["pattern"])
			if q != "" {
				a.Searches = append(a.Searches, SearchOp{Tool: "Glob", Query: q, Sphere: digest.Sphere})
				digest.ToolEvents++
			}
		case "Agent", "Task":
			a.SubAgents = append(a.SubAgents, SubAgentDispatch{
				Type:        jsonString(input["subagent_type"]),
				Description: clipProseShort(jsonString(input["description"])),
				Sphere:      digest.Sphere,
			})
			digest.ToolEvents++
		}
	}
	return a
}

// classifyURLSphere routes a URL to a sphere based on the host. Most
// research URLs land in work; private-finance domains route private.
// Empty when neither — the consumer keeps such URLs but doesn't
// sphere-bias them.
func classifyURLSphere(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "tugraz.at"),
		strings.Contains(host, "itp.tugraz"),
		strings.Contains(host, "euro-fusion"),
		strings.Contains(host, "iter.org"),
		strings.Contains(host, "ipp.mpg.de"),
		strings.Contains(host, "mpg.de"),
		strings.Contains(host, "arxiv.org"),
		strings.Contains(host, "doi.org"),
		strings.Contains(host, "github.com"),
		strings.Contains(host, "gitlab.tugraz.at"):
		return SphereWork
	case strings.Contains(host, "hetzner"),
		strings.Contains(host, "flatex"),
		strings.Contains(host, "broker"),
		strings.Contains(host, "bank"):
		return SpherePrivate
	}
	return ""
}

func clipProseShort(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
