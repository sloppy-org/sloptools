package kickoff

import (
	"strings"
)

// DecisionsHeading is the H2 the meeting-note template (`brain/conventions/meetings.md` §6)
// reserves for decisions. The kickoff helper reads this section from the
// previous meeting note so the new frame echoes prior commitments.
const DecisionsHeading = "Decisions"

// Frame is the 0-5 minute opening block: 1-2 questions to answer plus a
// list of decisions inherited from the prior meeting.
type Frame struct {
	Questions []string
	Decisions []string
}

// MaxFrameQuestions reflects the §5 cap of 1-2 questions for the
// opening frame. Callers that supply more have the slice trimmed.
const MaxFrameQuestions = 2

// BuildFrame composes a Frame from caller-provided questions and the
// prior meeting note's `## Decisions` section. Whitespace-only inputs
// are dropped; nil slices are returned as nil rather than empty.
func BuildFrame(questions []string, priorNote string) Frame {
	frame := Frame{}
	for _, q := range questions {
		clean := strings.TrimSpace(q)
		if clean == "" {
			continue
		}
		frame.Questions = append(frame.Questions, clean)
		if len(frame.Questions) >= MaxFrameQuestions {
			break
		}
	}
	frame.Decisions = ExtractDecisions(priorNote)
	return frame
}

// ExtractDecisions walks a Markdown document and returns the bullet
// lines under the first `## Decisions` heading. Bullets with the
// canonical `- [ ]` / `- [x]` task prefix have the prefix stripped so
// the kickoff frame echoes the decision text rather than a checkbox.
func ExtractDecisions(src string) []string {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	lines := strings.Split(src, "\n")
	out := []string{}
	inSection := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if isDecisionsHeading(trimmed) {
			inSection = true
			continue
		}
		if inSection && isAnyH2Heading(trimmed) {
			break
		}
		if !inSection {
			continue
		}
		if bullet, ok := bulletText(line); ok {
			out = append(out, bullet)
		}
	}
	return out
}

func isDecisionsHeading(line string) bool {
	clean := strings.TrimSpace(strings.TrimPrefix(line, "##"))
	return strings.EqualFold(clean, DecisionsHeading)
}

func isAnyH2Heading(line string) bool {
	if !strings.HasPrefix(line, "## ") {
		return false
	}
	if strings.HasPrefix(line, "### ") {
		return false
	}
	return true
}

func bulletText(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if !(strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ")) {
		return "", false
	}
	body := strings.TrimSpace(trimmed[2:])
	body = stripTaskPrefix(body)
	if body == "" {
		return "", false
	}
	return body, true
}

func stripTaskPrefix(body string) string {
	if len(body) < 4 || body[0] != '[' || body[2] != ']' {
		return body
	}
	switch body[1] {
	case ' ', 'x', 'X':
		return strings.TrimSpace(body[3:])
	}
	return body
}
