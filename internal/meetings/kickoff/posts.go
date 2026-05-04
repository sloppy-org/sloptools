// Package kickoff assembles the read-only `brain.meeting.kickoff`
// payload: a draft frame plus a clustered breakout grouping built from
// pre-meeting Zulip posts. The data layer stays out of internal/mcp so
// the algorithm can be unit-tested without spinning up the MCP server.
package kickoff

import (
	"regexp"
	"sort"
	"strings"

	"github.com/sloppy-org/sloptools/internal/zulip"
)

// Post is one parsed pre-meeting submission. Sender is a normalized
// person name (typically the Zulip full-name); Mentions is the
// dedup'd list of other people referenced in the body, also normalized
// to a canonical form.
type Post struct {
	Sender   string
	Mentions []string
	Body     string
}

// canonicalName is the case-folded, whitespace-collapsed form used as
// the union-find key. Inputs are full names like "Ada Example" or
// Zulip mention syntax like `@**Ada Example**`.
func canonicalName(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	clean = strings.ReplaceAll(clean, "_", " ")
	fields := strings.Fields(clean)
	for i, f := range fields {
		fields[i] = strings.ToLower(f)
	}
	return strings.Join(fields, " ")
}

var (
	mentionPattern = regexp.MustCompile(`@\*\*([^*]+)\*\*`)
	bareAtPattern  = regexp.MustCompile(`@([A-Za-z][\w-]*)`)
)

// ParsePost turns a Zulip Message into a Post by extracting the sender
// and any people referenced via Zulip mention syntax. Bodies that lack
// any mentions still produce a Post — single-poster topics flow into
// the pair-off-cycle bucket.
func ParsePost(msg zulip.Message) Post {
	sender := strings.TrimSpace(msg.SenderName)
	mentions := extractMentions(msg.Content)
	mentions = filterOutSelf(mentions, sender)
	return Post{Sender: sender, Mentions: mentions, Body: strings.TrimSpace(msg.Content)}
}

func extractMentions(body string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(name string) {
		key := canonicalName(name)
		clean := strings.TrimSpace(name)
		if key == "" || clean == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	for _, match := range mentionPattern.FindAllStringSubmatch(body, -1) {
		add(match[1])
	}
	for _, match := range bareAtPattern.FindAllStringSubmatch(body, -1) {
		if strings.Contains(match[0], "**") {
			continue
		}
		add(match[1])
	}
	sort.SliceStable(out, func(i, j int) bool {
		return canonicalName(out[i]) < canonicalName(out[j])
	})
	return out
}

func filterOutSelf(mentions []string, sender string) []string {
	if sender == "" {
		return mentions
	}
	self := canonicalName(sender)
	out := mentions[:0]
	for _, name := range mentions {
		if canonicalName(name) == self {
			continue
		}
		out = append(out, name)
	}
	return out
}

// ParsePosts turns a slice of Zulip messages into Posts and drops
// entries that have no resolvable sender. The order matches the input
// so callers can stable-sort by timestamp upstream.
func ParsePosts(messages []zulip.Message) []Post {
	out := make([]Post, 0, len(messages))
	for _, msg := range messages {
		post := ParsePost(msg)
		if post.Sender == "" {
			continue
		}
		out = append(out, post)
	}
	return out
}
