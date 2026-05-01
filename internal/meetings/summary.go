package meetings

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// AttendeesSection is the H2 heading whose bullet list names every
// participant.
const AttendeesSection = "Attendees"

// DecisionsSection is the H2 heading whose bullet list captures the
// agreements made in the meeting.
const DecisionsSection = "Decisions"

// SummaryNote is a meeting note parsed for the per-recipient summary
// drafter. It carries everything the renderer needs without re-reading
// the source.
type SummaryNote struct {
	Slug      string
	Title     string
	Date      string
	Owner     string
	Attendees []string
	Decisions []string
	Tasks     []Task
}

// ParseSummary extracts the title, date, owner, attendees, decisions, and
// per-person Action Checklist tasks from src. Tasks are returned with the
// canonical owner casing as parsed; callers run ResolvePerson to map
// aliases against brain/people candidates before rendering. The optional
// frontmatter fields recognised are `title`, `date`, and `owner` —
// anything else is ignored.
func ParseSummary(slug, src string) SummaryNote {
	note := SummaryNote{Slug: slug, Tasks: Parse(slug, src).Tasks}
	frontMatter, body := splitFrontMatter(src)
	note.Title = stringField(frontMatter, "title")
	note.Date = stringField(frontMatter, "date")
	note.Owner = stringField(frontMatter, "owner")
	for _, section := range parseTopLevelSections(body) {
		switch strings.TrimSpace(section.heading) {
		case AttendeesSection:
			note.Attendees = parseBulletList(section.body)
		case DecisionsSection:
			note.Decisions = parseBulletList(section.body)
		}
	}
	if note.Title == "" {
		note.Title = parseFirstHeading(body)
	}
	return note
}

// SummaryRecipients returns the names that should receive a summary
// draft. The default rule is `Attendees minus owner`; when the note has
// no Attendees section it falls back to the unique persons that own
// Action Checklist subsections. When owner is empty no filtering is
// performed.
func (s SummaryNote) SummaryRecipients() []string {
	pool := append([]string(nil), s.Attendees...)
	if len(pool) == 0 {
		seen := map[string]struct{}{}
		for _, task := range s.Tasks {
			name := strings.TrimSpace(task.Person)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			pool = append(pool, name)
		}
	}
	owner := strings.ToLower(strings.TrimSpace(s.Owner))
	out := make([]string, 0, len(pool))
	for _, name := range pool {
		if owner != "" && strings.EqualFold(strings.TrimSpace(name), owner) {
			continue
		}
		out = append(out, strings.TrimSpace(name))
	}
	return out
}

// TasksFor returns the Action Checklist tasks for a recipient. Matching
// is case-insensitive on the trimmed person name so callers can pass
// whichever casing the source used.
func (s SummaryNote) TasksFor(person string) []Task {
	want := NormalizePersonName(person)
	if want == "" {
		return nil
	}
	out := make([]Task, 0)
	for _, task := range s.Tasks {
		if NormalizePersonName(task.Person) == want {
			out = append(out, task)
		}
	}
	return out
}

// DraftRequest carries the share URL and account context that the
// summary renderer needs but cannot derive from the source note alone.
type DraftRequest struct {
	ShareURL string
	From     string
}

// Draft is the rendered email payload for one recipient. Email is empty
// when the resolver returned `needs_recipient`; the diagnostic is
// populated in that case.
type Draft struct {
	Recipient   string `json:"recipient"`
	Email       string `json:"email,omitempty"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	ShareURL    string `json:"share_url,omitempty"`
	Diagnostic  string `json:"diagnostic,omitempty"`
	Tasks       []Task `json:"tasks"`
	HasTasks    bool   `json:"has_tasks"`
	HasDecision bool   `json:"has_decisions"`
}

// RenderDraft turns the per-recipient slice of a SummaryNote into a
// finished Draft. Email is supplied by the caller (resolver runs
// outside the renderer because it depends on per-user config and brain
// frontmatter that the meetings package does not own).
func (s SummaryNote) RenderDraft(recipient, email string, request DraftRequest) Draft {
	tasks := s.TasksFor(recipient)
	subject := s.draftSubject(recipient)
	body := s.draftBody(recipient, tasks, request)
	return Draft{
		Recipient:   strings.TrimSpace(recipient),
		Email:       strings.TrimSpace(email),
		Subject:     subject,
		Body:        body,
		ShareURL:    strings.TrimSpace(request.ShareURL),
		Tasks:       tasks,
		HasTasks:    len(tasks) > 0,
		HasDecision: len(s.Decisions) > 0,
	}
}

func (s SummaryNote) draftSubject(recipient string) string {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = strings.TrimSpace(s.Slug)
	}
	clean := strings.TrimSpace(recipient)
	if clean == "" {
		return fmt.Sprintf("Meeting summary: %s", title)
	}
	first := strings.Fields(clean)
	if len(first) > 0 {
		clean = first[0]
	}
	return fmt.Sprintf("Meeting summary: %s — %s", title, clean)
}

func (s SummaryNote) draftBody(recipient string, tasks []Task, request DraftRequest) string {
	var b strings.Builder
	first := strings.TrimSpace(recipient)
	if first != "" {
		if parts := strings.Fields(first); len(parts) > 0 {
			first = parts[0]
		}
	}
	if first == "" {
		first = "there"
	}
	fmt.Fprintf(&b, "Hi %s,\n\n", first)
	header := strings.TrimSpace(s.Title)
	if header == "" {
		header = strings.TrimSpace(s.Slug)
	}
	if date := strings.TrimSpace(s.Date); date != "" {
		fmt.Fprintf(&b, "Notes from our meeting %q on %s.\n\n", header, date)
	} else {
		fmt.Fprintf(&b, "Notes from our meeting %q.\n\n", header)
	}
	if len(s.Attendees) > 0 {
		fmt.Fprintln(&b, "Attendees:")
		for _, name := range s.Attendees {
			fmt.Fprintf(&b, "- %s\n", name)
		}
		fmt.Fprintln(&b)
	}
	if url := strings.TrimSpace(request.ShareURL); url != "" {
		fmt.Fprintf(&b, "Shared note: %s\n\n", url)
	}
	if len(s.Decisions) > 0 {
		fmt.Fprintln(&b, "Decisions:")
		for _, line := range s.Decisions {
			fmt.Fprintf(&b, "- %s\n", line)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "Your tasks:")
	if len(tasks) == 0 {
		fmt.Fprintln(&b, "- (no action items captured for you in this meeting)")
	}
	for _, task := range tasks {
		fmt.Fprintln(&b, formatTaskLine(task))
	}
	fmt.Fprintln(&b)
	if url := strings.TrimSpace(request.ShareURL); url != "" {
		fmt.Fprintf(&b, "You can edit the shared note directly to tick off items or add new ones: %s\n", url)
	}
	return b.String()
}

func formatTaskLine(task Task) string {
	parts := []string{strings.TrimSpace(task.Text)}
	if due := strings.TrimSpace(task.Due); due != "" {
		parts = append(parts, "(due "+due+")")
	} else if follow := strings.TrimSpace(task.FollowUp); follow != "" {
		parts = append(parts, "(follow up "+follow+")")
	}
	if project := strings.TrimSpace(task.Project); project != "" {
		parts = append(parts, "[project: "+project+"]")
	}
	return "- " + strings.Join(parts, " ")
}

// SortDraftsByRecipient orders drafts alphabetically by recipient name
// so callers and tests see a stable enumeration.
func SortDraftsByRecipient(drafts []Draft) {
	sort.SliceStable(drafts, func(i, j int) bool {
		return strings.ToLower(drafts[i].Recipient) < strings.ToLower(drafts[j].Recipient)
	})
}

func splitFrontMatter(src string) (map[string]string, string) {
	lines := strings.Split(src, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return nil, src
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			fm := parseSimpleFrontMatter(lines[1:i])
			body := ""
			if i+1 < len(lines) {
				body = strings.Join(lines[i+1:], "\n")
			}
			return fm, body
		}
	}
	return nil, src
}

var simpleScalarFrontMatter = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*:\s*(.*)$`)

func parseSimpleFrontMatter(lines []string) map[string]string {
	out := map[string]string{}
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		match := simpleScalarFrontMatter.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(match[1]))
		value := strings.TrimSpace(match[2])
		value = strings.Trim(value, `"'`)
		out[key] = value
	}
	return out
}

func stringField(frontMatter map[string]string, name string) string {
	if frontMatter == nil {
		return ""
	}
	return strings.TrimSpace(frontMatter[strings.ToLower(name)])
}

type topLevelSection struct {
	heading string
	body    string
}

func parseTopLevelSections(body string) []topLevelSection {
	lines := strings.Split(body, "\n")
	var sections []topLevelSection
	var current *topLevelSection
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		heading, level := parseHeading(line)
		if level == 2 {
			if current != nil {
				sections = append(sections, *current)
			}
			next := topLevelSection{heading: strings.TrimSpace(heading)}
			current = &next
			continue
		}
		if current == nil {
			continue
		}
		current.body += line + "\n"
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return sections
}

var bulletPattern = regexp.MustCompile(`^[ \t]*[-*+][ \t]+(.*)$`)

func parseBulletList(body string) []string {
	var out []string
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		if line == "" {
			continue
		}
		match := bulletPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		text := strings.TrimSpace(match[1])
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func parseFirstHeading(body string) string {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, "\r")
		heading, level := parseHeading(line)
		if level == 1 && strings.TrimSpace(heading) != "" {
			return strings.TrimSpace(heading)
		}
	}
	return ""
}
