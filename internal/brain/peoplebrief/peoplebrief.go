// Package peoplebrief assembles the read-only `brain.people.brief` payload
// from a person's note frontmatter, status bullets, open commitments,
// most recent meeting note, and most recent inbound mail message. The
// package keeps pure data assembly out of `internal/mcp` so it can be
// unit-tested without spinning up the MCP server.
package peoplebrief

import (
	"regexp"
	"sort"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"gopkg.in/yaml.v3"
)

// DefaultStatusSection is the canonical H2 heading whose dated bullet list
// captures the user's recent context with a person.
const DefaultStatusSection = "Recent context"

// DefaultStatusLimit is the bullet count returned when callers do not
// override `status_limit`.
const DefaultStatusLimit = 3

// FrontmatterFields lists the canonical person-note fields that the brief
// echoes back, in dashboard order.
var FrontmatterFields = []string{
	"role",
	"supervision_role",
	"focus",
	"cadence",
	"strategic",
	"enjoyment",
	"last_seen",
	"affiliation",
}

// FallbackStatusSections lists the H2 headings the brief tries when no
// explicit `status_section` is supplied. The first match wins.
var FallbackStatusSections = []string{
	DefaultStatusSection,
	"Recent",
	"Status",
	"Activity",
	"Updates",
}

var (
	statusBulletPattern    = regexp.MustCompile(`^\s*[-*]\s+(\d{4}-\d{2}-\d{2}(?:/\d{1,2}(?:-\d{1,2})?)?)[\s:.-]+(.*)$`)
	emailBulletPattern     = regexp.MustCompile(`(?i)^\s*[-*]\s+(?:e[- ]?mail|email)\s*[:=]\s*(\S+@\S+)\s*$`)
	emailFrontmatterFields = []string{"email", "emails", "primary_email"}
)

// StatusBullet is a single dated entry parsed from the person's status
// section, preserving the date prefix and the trailing prose.
type StatusBullet struct {
	Date string `json:"date"`
	Text string `json:"text"`
}

// OpenLoop is one open commitment surfaced in the brief, scoped to the path
// and metadata a reader needs to find the canonical Markdown note.
type OpenLoop struct {
	Path     string `json:"path"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Due      string `json:"due,omitempty"`
	FollowUp string `json:"follow_up,omitempty"`
}

// Meeting is the most recent meeting note that wikilinks the person.
type Meeting struct {
	Path  string `json:"path"`
	Title string `json:"title,omitempty"`
	Date  string `json:"date,omitempty"`
}

// Mail is the most recent inbound message from the person, returned as
// metadata only — the brief never fetches or echoes message bodies.
type Mail struct {
	AccountID int64  `json:"account_id"`
	MessageID string `json:"message_id"`
	ThreadID  string `json:"thread_id,omitempty"`
	Subject   string `json:"subject"`
	Date      string `json:"date,omitempty"`
	Folder    string `json:"folder,omitempty"`
}

// Commitment is the projection of a parsed GTD commitment node that the
// brief needs to bucket by relationship to the person. Callers fill these
// from their own commitment store rather than depending on dedupNote
// internals.
type Commitment struct {
	Path        string
	Title       string
	Status      string
	Due         string
	FollowUp    string
	People      []string
	WaitingFor  string
	DelegatedTo string
	Actor       string
	Closed      bool
}

// Frontmatter returns the canonical person-note frontmatter fields the brief
// surfaces. Missing or empty values are omitted.
func Frontmatter(note *brain.MarkdownNote) map[string]interface{} {
	out := make(map[string]interface{}, len(FrontmatterFields))
	if note == nil {
		return out
	}
	for _, key := range FrontmatterFields {
		node, ok := note.FrontMatterField(key)
		if !ok {
			continue
		}
		if value := frontmatterScalar(node); value != "" {
			out[key] = value
		}
	}
	return out
}

// StatusBullets parses the configured status section and returns the most
// recent dated bullets, newest first. Returns nil when no recognised section
// holds dated bullets.
func StatusBullets(note *brain.MarkdownNote, sectionOverride string, limit int) []StatusBullet {
	if note == nil {
		return nil
	}
	if limit <= 0 {
		limit = DefaultStatusLimit
	}
	candidates := FallbackStatusSections
	if section := strings.TrimSpace(sectionOverride); section != "" {
		candidates = append([]string{section}, candidates...)
	}
	for _, name := range candidates {
		section, ok := note.Section(name)
		if !ok {
			continue
		}
		bullets := parseStatusBullets(section.Body)
		if len(bullets) == 0 {
			continue
		}
		if len(bullets) > limit {
			bullets = bullets[:limit]
		}
		return bullets
	}
	return nil
}

// ClassifyOpenLoops splits the open commitments into the four buckets
// described in the issue: owner (actor matches the person), delegated_to,
// waiting, and mentioned (people field only). Closed commitments are
// dropped. Buckets are stable-sorted by path so callers get deterministic
// payloads across runs.
func ClassifyOpenLoops(commitments []Commitment, person string) map[string][]OpenLoop {
	out := map[string][]OpenLoop{
		"owner":        {},
		"delegated_to": {},
		"waiting":      {},
		"mentioned":    {},
	}
	for _, commitment := range commitments {
		if commitment.Closed {
			continue
		}
		bucket := bucketFor(commitment, person)
		if bucket == "" {
			continue
		}
		out[bucket] = append(out[bucket], OpenLoop{
			Path:     commitment.Path,
			Title:    strings.TrimSpace(commitment.Title),
			Status:   strings.TrimSpace(commitment.Status),
			Due:      strings.TrimSpace(commitment.Due),
			FollowUp: strings.TrimSpace(commitment.FollowUp),
		})
	}
	for key := range out {
		sortOpenLoops(out[key])
	}
	return out
}

// PersonEmail extracts a contact email for the person from the note's
// frontmatter (canonical) or, as a fallback, from a top-of-note bullet
// pattern such as `- Email: ada@example.com`.
func PersonEmail(note *brain.MarkdownNote, src string) string {
	if note != nil {
		for _, key := range emailFrontmatterFields {
			if value := frontmatterStringField(note, key); value != "" {
				return strings.SplitN(strings.TrimSpace(value), ",", 2)[0]
			}
		}
	}
	for _, line := range strings.Split(src, "\n") {
		if match := emailBulletPattern.FindStringSubmatch(line); match != nil {
			return strings.TrimSpace(match[1])
		}
	}
	return ""
}

// CommitmentMatchesPerson reports whether a parsed commitment is
// classifiable into one of the four open-loop buckets for the person. It
// is exported so callers can pre-filter before the closed-status flag is
// finalised.
func CommitmentMatchesPerson(c Commitment, person string) bool {
	return bucketFor(c, person) != ""
}

// CommitmentFromCommitment projects a parsed braingtd.Commitment into the
// brief's bucketing shape. The closed-status decision stays with the caller
// because closed-status conventions vary by sphere.
func CommitmentFromCommitment(path string, c braingtd.Commitment, closed bool) Commitment {
	status := strings.ToLower(strings.TrimSpace(c.LocalOverlay.Status))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(c.Status))
	}
	title := strings.TrimSpace(c.Title)
	if title == "" {
		title = strings.TrimSpace(c.Outcome)
	}
	return Commitment{
		Path:        path,
		Title:       title,
		Status:      status,
		Due:         c.Due,
		FollowUp:    c.FollowUp,
		People:      append([]string(nil), c.People...),
		WaitingFor:  c.WaitingFor,
		DelegatedTo: c.DelegatedTo,
		Actor:       c.Actor,
		Closed:      closed,
	}
}

func bucketFor(c Commitment, person string) string {
	if personFieldMatches(c.DelegatedTo, person) {
		return "delegated_to"
	}
	if personFieldMatches(c.WaitingFor, person) {
		return "waiting"
	}
	if personFieldMatches(c.Actor, person) {
		return "owner"
	}
	if peopleFieldMatches(c.People, person) {
		return "mentioned"
	}
	return ""
}

func parseStatusBullets(body string) []StatusBullet {
	if body == "" {
		return nil
	}
	var bullets []StatusBullet
	for _, line := range strings.Split(body, "\n") {
		match := statusBulletPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		date := strings.TrimSpace(match[1])
		text := strings.TrimSpace(match[2])
		if text == "" {
			continue
		}
		bullets = append(bullets, StatusBullet{Date: date, Text: text})
	}
	sort.SliceStable(bullets, func(i, j int) bool {
		return bullets[i].Date > bullets[j].Date
	})
	return bullets
}

func sortOpenLoops(items []OpenLoop) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Path == items[j].Path {
			return items[i].Title < items[j].Title
		}
		return items[i].Path < items[j].Path
	})
}

func frontmatterStringField(note *brain.MarkdownNote, name string) string {
	if note == nil {
		return ""
	}
	node, ok := note.FrontMatterField(name)
	if !ok {
		return ""
	}
	return frontmatterScalar(node)
}

func frontmatterScalar(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	switch node.Kind {
	case yaml.ScalarNode:
		return strings.TrimSpace(node.Value)
	case yaml.SequenceNode:
		parts := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			if child.Kind == yaml.ScalarNode && strings.TrimSpace(child.Value) != "" {
				parts = append(parts, strings.TrimSpace(child.Value))
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}
