package brainprojects

import "strings"

func formatOpenLoops(items ProjectBuckets) string {
	if len(items.Next) == 0 && len(items.Waiting) == 0 && len(items.Closed) == 0 {
		return "\n_None at present._\n"
	}
	var b strings.Builder
	writeCommitmentSection(&b, "Chris owes", items.Next, false)
	writeWaitingSection(&b, items.Waiting)
	writeCommitmentSection(&b, "Closed (last 14 days)", items.Closed, true)
	return b.String()
}

func writeCommitmentSection(b *strings.Builder, title string, items []ProjectCommitment, closed bool) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n### " + title + "\n")
	for _, item := range items {
		box := " "
		if closed {
			box = "x"
		}
		b.WriteString("- [" + box + "] " + item.Link)
		writeCommitmentSuffix(b, item, closed)
		b.WriteByte('\n')
	}
}

func writeCommitmentSuffix(b *strings.Builder, item ProjectCommitment, closed bool) {
	switch {
	case closed && item.ClosedAt != "":
		b.WriteString(" — closed " + shortDate(item.ClosedAt))
	case item.Due != "":
		b.WriteString(" — due " + shortDate(item.Due))
	case item.FollowUp != "":
		b.WriteString(" — follow up " + shortDate(item.FollowUp))
	}
}

func writeWaitingSection(b *strings.Builder, items []ProjectCommitment) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n### Waiting on others\n")
	for _, item := range items {
		b.WriteString("- [ ] ")
		if item.Person != "" {
			b.WriteString(personLink(item.Person) + " — ")
		}
		b.WriteString(item.Outcome)
		if item.FollowUp != "" {
			b.WriteString(" — follow up " + shortDate(item.FollowUp))
		}
		b.WriteByte('\n')
	}
}

func renderOpenLoops(src, body string) string {
	section := "## " + openLoopsHeading + "\n" + body
	start, end, ok := h2Bounds(src, openLoopsHeading)
	if !ok {
		return strings.TrimRight(src, "\n") + "\n\n" + section
	}
	return src[:start] + section + src[end:]
}

func h2Bounds(src, heading string) (int, int, bool) {
	lines := strings.SplitAfter(src, "\n")
	offset := 0
	start := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "## "+heading {
			start = offset
			break
		}
		offset += len(line)
	}
	if start < 0 {
		return 0, 0, false
	}
	end := len(src)
	offset = start + len(linesAt(src[start:])[0])
	for _, line := range linesAt(src[offset:]) {
		if isH2(line) {
			end = offset
			break
		}
		offset += len(line)
	}
	return start, end, true
}
