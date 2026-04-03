package llmcache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var utcTimeLineRe = regexp.MustCompile(`(?m)^Current UTC time:\s*(\d{4}-\d{2}-\d{2})T[^\n]*$`)
var runningTasksLineRe = regexp.MustCompile(`(?m)^Running tasks:\s*[^\n]*$`)
var recentConversationBlockRe = regexp.MustCompile(`(?ms)^Recent conversation:\n(?:- [^\n]*\n?)*`)
var recentMessagesBlockRe = regexp.MustCompile(`(?ms)^Recent messages:\n(?:[A-Z]+: [^\n]*\n?)*`)

// BuildKey produces a SHA256 cache key from the LLM request components.
func BuildKey(messages []map[string]any, tools []map[string]any, model string, enableThinking bool) string {
	normalized := normalizeMessages(messages)
	messagesJSON, _ := json.Marshal(normalized)
	toolsJSON, _ := json.Marshal(tools)
	thinking := "0"
	if enableThinking {
		thinking = "1"
	}
	h := sha256.New()
	fmt.Fprintf(h, "m:%d:%s\n", len(messagesJSON), messagesJSON)
	fmt.Fprintf(h, "t:%d:%s\n", len(toolsJSON), toolsJSON)
	fmt.Fprintf(h, "model:%s\n", model)
	fmt.Fprintf(h, "think:%s\n", thinking)
	return hex.EncodeToString(h.Sum(nil))
}

// BuildIntentKey produces a cache key for intent classification that
// strips conversation history from the user prompt. This allows cache
// hits when the same self-contained question is asked in different
// conversational contexts. Falls back to the full BuildKey when the
// query appears to be a follow-up (starts with "and", "und", "also",
// pronouns referencing prior context).
func BuildIntentKey(messages []map[string]any, model string) string {
	normalized := make([]map[string]any, len(messages))
	for i, msg := range messages {
		cp := make(map[string]any, len(msg))
		for k, v := range msg {
			cp[k] = v
		}
		if content, ok := cp["content"].(string); ok {
			content = normalizeContent(content)
			content = stripConversationHistory(content)
			cp["content"] = content
		}
		normalized[i] = cp
	}
	messagesJSON, _ := json.Marshal(normalized)
	h := sha256.New()
	fmt.Fprintf(h, "intent:%d:%s\n", len(messagesJSON), messagesJSON)
	fmt.Fprintf(h, "model:%s\n", model)
	return hex.EncodeToString(h.Sum(nil))
}

// IsSelfContainedQuery returns true if the user text appears to be a
// standalone request rather than a follow-up that depends on conversation
// context. Follow-ups like "and tomorrow?", "und das?", "what about X?"
// return false because their meaning depends on prior messages.
func IsSelfContainedQuery(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	followUpPrefixes := []string{
		"and ", "und ", "also ", "what about ", "was ist mit ",
		"how about ", "wie steht es mit ", "wie sieht es mit ",
		"the same ", "das gleiche ", "noch ", "ditto",
	}
	for _, prefix := range followUpPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	pronounOnlyPrefixes := []string{
		"it ", "them ", "this ", "that ", "those ", "these ",
		"es ", "das ", "die ", "den ", "dem ", "deren ",
	}
	for _, prefix := range pronounOnlyPrefixes {
		if strings.HasPrefix(lower, prefix) && len(lower) < 40 {
			return false
		}
	}
	return true
}

func stripConversationHistory(content string) string {
	content = recentConversationBlockRe.ReplaceAllString(content, "")
	content = recentMessagesBlockRe.ReplaceAllString(content, "")
	return strings.TrimRight(content, " \t\n")
}

// ContainsToolResults returns true if any message has role "tool",
// indicating this is a follow-up round with live tool results that
// should not be cached.
func ContainsToolResults(messages []map[string]any) bool {
	for _, msg := range messages {
		if role, _ := msg["role"].(string); role == "tool" {
			return true
		}
	}
	return false
}

func normalizeMessages(messages []map[string]any) []map[string]any {
	out := make([]map[string]any, len(messages))
	for i, msg := range messages {
		cp := make(map[string]any, len(msg))
		for k, v := range msg {
			cp[k] = v
		}
		if content, ok := cp["content"].(string); ok {
			cp["content"] = normalizeContent(content)
		}
		out[i] = cp
	}
	return out
}

func normalizeContent(content string) string {
	// Replace full ISO timestamp with date-only so same-day queries
	// produce the same cache key but next-day queries miss.
	content = utcTimeLineRe.ReplaceAllString(content, "Current UTC time: $1")
	// Strip ephemeral scheduler state.
	content = runningTasksLineRe.ReplaceAllString(content, "")
	return strings.TrimRight(content, " \t\n")
}
