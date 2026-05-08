package sleepconv

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func readClaudeUserPrompts(home string, since time.Time) []Prompt {
	root := filepath.Join(home, ".claude", "projects")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	var out []Prompt
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
		out = append(out, parseClaudeJSONL(path, since)...)
		return nil
	})
	return out
}

func parseClaudeJSONL(path string, since time.Time) []Prompt {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.UseNumber()
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	dirName := filepath.Base(filepath.Dir(path))
	fallbackCWD := decodeClaudeProjectDir(dirName)
	var out []Prompt
	for dec.More() {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		if !claudeEntryIsUserText(raw) {
			continue
		}
		ts := claudeEntryTimestamp(raw)
		if !ts.IsZero() && ts.Before(since) {
			continue
		}
		text := claudeEntryProse(raw)
		text = stripHarnessMarkers(text)
		text = strings.TrimSpace(text)
		if proseTooThin(text) {
			continue
		}
		cwd := claudeEntryCWD(raw)
		if cwd == "" {
			cwd = fallbackCWD
		}
		out = append(out, Prompt{
			Timestamp: ts,
			Source:    "claude",
			SessionID: sessionID,
			CWD:       cwd,
			Prose:     text,
		})
	}
	return out
}

func claudeEntryIsUserText(raw map[string]json.RawMessage) bool {
	if t := jsonString(raw["type"]); t != "user" {
		return false
	}
	if jsonBool(raw["isSidechain"]) {
		return false
	}
	msg, ok := raw["message"]
	if !ok {
		return false
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(msg, &inner); err != nil {
		return false
	}
	return jsonString(inner["role"]) == "user"
}

// claudeEntryProse returns the user-typed text, joined when the
// content is an array of blocks. tool_result blocks are skipped.
func claudeEntryProse(raw map[string]json.RawMessage) string {
	msg, ok := raw["message"]
	if !ok {
		return ""
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(msg, &inner); err != nil {
		return ""
	}
	content, ok := inner["content"]
	if !ok {
		return ""
	}
	if s, err := strconvJSONString(content); err == nil {
		return s
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(content, &arr); err != nil {
		return ""
	}
	var parts []string
	for _, block := range arr {
		if jsonString(block["type"]) != "text" {
			continue
		}
		parts = append(parts, jsonString(block["text"]))
	}
	return strings.Join(parts, "\n")
}

func claudeEntryCWD(raw map[string]json.RawMessage) string {
	return strings.TrimSpace(jsonString(raw["cwd"]))
}

func claudeEntryTimestamp(raw map[string]json.RawMessage) time.Time {
	s := jsonString(raw["timestamp"])
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

// decodeClaudeProjectDir reverses the dash-encoded cwd used by Claude
// Code project directories. Lossy: real dashes in path segments
// collide with the separator. Fallback only when an event lacks `cwd`.
func decodeClaudeProjectDir(name string) string {
	if name == "" || !strings.HasPrefix(name, "-") {
		return ""
	}
	return strings.ReplaceAll(name, "-", "/")
}
