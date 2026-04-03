package evernote

import (
	"encoding/xml"
	"strings"
	"unicode"
	"unicode/utf8"
)

type enmlRenderer struct {
	text     strings.Builder
	markdown strings.Builder
	tasks    []Task
	task     *Task
}

func ConvertENMLToText(enml string) (text string, markdown string, tasks []Task) {
	clean := strings.TrimSpace(enml)
	if clean == "" {
		return "", "", nil
	}
	decoder := xml.NewDecoder(strings.NewReader(clean))
	decoder.Strict = false
	renderer := &enmlRenderer{}
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch tok := token.(type) {
		case xml.StartElement:
			renderer.handleStart(tok)
		case xml.EndElement:
			renderer.handleEnd(tok)
		case xml.CharData:
			renderer.handleText(string(tok))
		}
	}
	renderer.finishTask()
	text = strings.TrimSpace(normalizeRenderedText(renderer.text.String()))
	markdown = strings.TrimSpace(normalizeRenderedText(renderer.markdown.String()))
	return text, markdown, renderer.tasks
}

func (r *enmlRenderer) handleStart(tok xml.StartElement) {
	switch strings.ToLower(tok.Name.Local) {
	case "en-note", "div", "p":
		r.startBlock("")
	case "li":
		r.startBlock("- ")
	case "br":
		r.endBlock()
	case "en-todo":
		r.finishTask()
		r.startBlock("")
		checked := false
		for _, attr := range tok.Attr {
			if strings.EqualFold(attr.Name.Local, "checked") && isTruthy(attr.Value) {
				checked = true
				break
			}
		}
		r.task = &Task{Checked: checked}
		marker := "[ ] "
		if checked {
			marker = "[x] "
		}
		r.writeLiteral(marker)
	}
}

func (r *enmlRenderer) handleEnd(tok xml.EndElement) {
	switch strings.ToLower(tok.Name.Local) {
	case "div", "p", "li", "en-note":
		r.endBlock()
	}
}

func (r *enmlRenderer) handleText(raw string) {
	text := collapseWhitespace(raw)
	if text == "" {
		return
	}
	r.writeText(text)
	if r.task != nil {
		if r.task.Text != "" {
			r.task.Text += " "
		}
		r.task.Text += text
	}
}

func (r *enmlRenderer) finishTask() {
	if r.task == nil {
		return
	}
	r.task.Text = strings.TrimSpace(r.task.Text)
	if r.task.Text != "" {
		r.tasks = append(r.tasks, *r.task)
	}
	r.task = nil
}

func (r *enmlRenderer) startBlock(prefix string) {
	r.finishTask()
	r.ensureParagraphBreak()
	if prefix != "" {
		r.writeLiteral(prefix)
	}
}

func (r *enmlRenderer) endBlock() {
	r.finishTask()
	r.writeNewline()
}

func (r *enmlRenderer) ensureParagraphBreak() {
	ensureParagraphBreak(&r.text)
	ensureParagraphBreak(&r.markdown)
}

func (r *enmlRenderer) writeLiteral(value string) {
	if value == "" {
		return
	}
	r.text.WriteString(value)
	r.markdown.WriteString(value)
}

func (r *enmlRenderer) writeText(value string) {
	if value == "" {
		return
	}
	writeSeparated(&r.text, value)
	writeSeparated(&r.markdown, value)
}

func (r *enmlRenderer) writeNewline() {
	writeNewline(&r.text)
	writeNewline(&r.markdown)
}

func writeSeparated(b *strings.Builder, value string) {
	if b.Len() > 0 {
		last, _ := lastRune(b.String())
		if !unicode.IsSpace(last) && last != '[' && last != '(' && last != '-' {
			b.WriteByte(' ')
		}
	}
	b.WriteString(value)
}

func writeNewline(b *strings.Builder) {
	s := b.String()
	if s == "" || strings.HasSuffix(s, "\n") {
		return
	}
	b.WriteByte('\n')
}

func ensureParagraphBreak(b *strings.Builder) {
	s := b.String()
	if s == "" {
		return
	}
	switch {
	case strings.HasSuffix(s, "\n\n"):
		return
	case strings.HasSuffix(s, "\n"):
		b.WriteByte('\n')
	default:
		b.WriteString("\n\n")
	}
}

func normalizeRenderedText(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(collapseWhitespace(line))
	}
	var kept []string
	blank := false
	for _, line := range lines {
		if line == "" {
			if !blank && len(kept) > 0 {
				kept = append(kept, "")
			}
			blank = true
			continue
		}
		kept = append(kept, line)
		blank = false
	}
	return strings.Join(kept, "\n")
}

func collapseWhitespace(raw string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
		space = false
	}
	return b.String()
}

func isTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "checked", "1", "yes":
		return true
	default:
		return false
	}
}

func lastRune(raw string) (rune, bool) {
	if raw == "" {
		return 0, false
	}
	r, _ := utf8.DecodeLastRuneInString(raw)
	if r == utf8.RuneError && len(raw) == 1 {
		return 0, false
	}
	return r, true
}
