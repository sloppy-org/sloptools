package sleepconv

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func readCodexUserPrompts(home string, since time.Time) []Prompt {
	root := filepath.Join(home, ".codex", "sessions")
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
		out = append(out, parseCodexRollout(path, since)...)
		return nil
	})
	return out
}

func parseCodexRollout(path string, since time.Time) []Prompt {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	sessionID := ""
	cwd := ""
	var out []Prompt
	first := true
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
				sessionID = jsonString(meta["id"])
				cwd = strings.TrimSpace(jsonString(meta["cwd"]))
			}
			continue
		}
		if etype != "response_item" {
			continue
		}
		ts := codexEntryTimestamp(raw)
		if !ts.IsZero() && ts.Before(since) {
			continue
		}
		role, text := codexExtractUserText(raw)
		if role != "user" || text == "" {
			continue
		}
		if first {
			first = false
			if codexLooksLikeAgentsPreamble(text) {
				continue
			}
		}
		if codexIsHarnessOnly(text) {
			continue
		}
		text = strings.TrimSpace(text)
		if proseTooThin(text) {
			continue
		}
		out = append(out, Prompt{
			Timestamp: ts,
			Source:    "codex",
			SessionID: sessionID,
			CWD:       cwd,
			Prose:     text,
		})
	}
	return out
}

func codexExtractUserText(raw map[string]json.RawMessage) (string, string) {
	payload, ok := raw["payload"]
	if !ok {
		return "", ""
	}
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(payload, &inner); err != nil {
		return "", ""
	}
	if jsonString(inner["type"]) != "message" {
		return "", ""
	}
	role := jsonString(inner["role"])
	if role != "user" {
		return role, ""
	}
	content, ok := inner["content"]
	if !ok {
		return role, ""
	}
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(content, &arr); err != nil {
		return role, ""
	}
	var parts []string
	for _, block := range arr {
		if jsonString(block["type"]) != "input_text" {
			continue
		}
		parts = append(parts, jsonString(block["text"]))
	}
	return role, strings.Join(parts, "\n")
}

func codexEntryTimestamp(raw map[string]json.RawMessage) time.Time {
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

// codexAgentsPreambleHead matches the auto-prepended AGENTS.md rules
// block that codex injects as the first user-role message of every
// session. The user did not type this.
var codexAgentsPreambleHead = regexp.MustCompile(`^(?s)\s*(#\s+AGENTS\.md instructions for /|<INSTRUCTIONS>)`)

func codexLooksLikeAgentsPreamble(text string) bool {
	return codexAgentsPreambleHead.MatchString(text)
}

// codexIsHarnessOnly catches codex-internal user-role markers the
// CLI emits without user typing (turn aborts, environment notices).
func codexIsHarnessOnly(text string) bool {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "<turn_aborted>") && strings.HasSuffix(t, "</turn_aborted>") {
		return true
	}
	if strings.HasPrefix(t, "<environment_context>") && strings.HasSuffix(t, "</environment_context>") {
		return true
	}
	return false
}
