package brain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type MarkdownDiagnostic struct {
	Line    int    `json:"line,omitempty"`
	Message string `json:"message,omitempty"`
}

type MarkdownParseOptions struct {
	RequiredSections []string
}

type MarkdownNote struct {
	frontMatter *markdownFrontMatter
	elements    []markdownElement
}

type MarkdownFrontMatter struct {
	StartLine int        `json:"start_line,omitempty"`
	EndLine   int        `json:"end_line,omitempty"`
	Raw       string     `json:"raw,omitempty"`
	Node      *yaml.Node `json:"-"`
}

type MarkdownSection struct {
	Level     int    `json:"level,omitempty"`
	Name      string `json:"name,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Body      string `json:"body,omitempty"`
}

type markdownFrontMatter struct {
	public   MarkdownFrontMatter
	modified bool
}

type markdownElement struct {
	kind    markdownElementKind
	raw     string
	section MarkdownSection
	heading string
	body    string
	changed bool
}

type markdownElementKind int

const (
	markdownProse markdownElementKind = iota
	markdownSection
)

type lineSpan struct {
	line       int
	start, end int
	text       string
}

var yamlLinePattern = regexp.MustCompile(`line ([0-9]+)`)

func ParseMarkdownNote(src string, opts MarkdownParseOptions) (*MarkdownNote, []MarkdownDiagnostic) {
	spans := splitLineSpans(src)
	fm, bodyIndex, diags := parseMarkdownFrontMatter(src, spans)
	note := &MarkdownNote{frontMatter: fm}
	note.elements = parseMarkdownElements(src, spans[bodyIndex:])
	diags = append(diags, duplicateRequiredSectionDiagnostics(note.sections(), opts.RequiredSections)...)
	return note, diags
}

func (n *MarkdownNote) FrontMatter() (MarkdownFrontMatter, bool) {
	if n == nil || n.frontMatter == nil {
		return MarkdownFrontMatter{}, false
	}
	return n.frontMatter.public, true
}

func (n *MarkdownNote) FrontMatterField(name string) (*yaml.Node, bool) {
	fm, ok := n.FrontMatter()
	if !ok || fm.Node == nil || len(fm.Node.Content) == 0 {
		return nil, false
	}
	mapping := documentMapping(fm.Node)
	if mapping == nil {
		return nil, false
	}
	return mappingValue(mapping, name)
}

func (n *MarkdownNote) SetFrontMatterField(name string, value any) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("frontmatter field name is required")
	}
	node, err := yamlValueNode(value)
	if err != nil {
		return err
	}
	fm := n.ensureFrontMatter()
	mapping := ensureDocumentMapping(fm.public.Node)
	setMappingValue(mapping, name, node)
	fm.modified = true
	return nil
}

func (n *MarkdownNote) Sections() []MarkdownSection {
	if n == nil {
		return nil
	}
	return append([]MarkdownSection(nil), n.sections()...)
}

func (n *MarkdownNote) Section(name string) (MarkdownSection, bool) {
	normalized := normalizeSectionName(name)
	for _, section := range n.sections() {
		if normalizeSectionName(section.Name) == normalized {
			return section, true
		}
	}
	return MarkdownSection{}, false
}

func (n *MarkdownNote) SetSectionBody(name, body string) error {
	normalized := normalizeSectionName(name)
	for i := range n.elements {
		elem := &n.elements[i]
		if elem.kind != markdownSection || normalizeSectionName(elem.section.Name) != normalized {
			continue
		}
		elem.body = body
		elem.section.Body = body
		elem.changed = true
		return nil
	}
	return fmt.Errorf("section %q not found", name)
}

func (n *MarkdownNote) Render() (string, error) {
	if n == nil {
		return "", nil
	}
	var out strings.Builder
	if n.frontMatter != nil {
		rendered, err := n.frontMatter.render()
		if err != nil {
			return "", err
		}
		out.WriteString(rendered)
	}
	for _, elem := range n.elements {
		out.WriteString(elem.render())
	}
	return out.String(), nil
}

func (d MarkdownDiagnostic) Error() string {
	if d.Line > 0 {
		return fmt.Sprintf("line %d: %s", d.Line, d.Message)
	}
	return d.Message
}

func (f *markdownFrontMatter) render() (string, error) {
	if !f.modified {
		return f.public.Raw, nil
	}
	body, err := yaml.Marshal(f.public.Node)
	if err != nil {
		return "", err
	}
	return "---\n" + strings.TrimSuffix(string(body), "\n") + "\n---\n", nil
}

func (e markdownElement) render() string {
	if e.kind != markdownSection || !e.changed {
		return e.raw
	}
	return e.heading + e.body
}

func (n *MarkdownNote) ensureFrontMatter() *markdownFrontMatter {
	if n.frontMatter != nil {
		if n.frontMatter.public.Node == nil {
			n.frontMatter.public.Node = emptyDocumentMapping()
		}
		return n.frontMatter
	}
	n.frontMatter = &markdownFrontMatter{
		public: MarkdownFrontMatter{StartLine: 1, EndLine: 2, Node: emptyDocumentMapping()},
	}
	return n.frontMatter
}

func splitLineSpans(src string) []lineSpan {
	var spans []lineSpan
	for line, start := 1, 0; start < len(src); line++ {
		end := strings.IndexByte(src[start:], '\n')
		if end < 0 {
			spans = append(spans, lineSpan{line: line, start: start, end: len(src), text: src[start:]})
			break
		}
		end += start + 1
		spans = append(spans, lineSpan{line: line, start: start, end: end, text: src[start:end]})
		start = end
	}
	return spans
}

func parseMarkdownFrontMatter(src string, spans []lineSpan) (*markdownFrontMatter, int, []MarkdownDiagnostic) {
	if len(spans) == 0 || !isFrontMatterDelimiter(spans[0].text) {
		return nil, 0, nil
	}
	for i := 1; i < len(spans); i++ {
		if !isFrontMatterDelimiter(spans[i].text) {
			continue
		}
		fm, diags := decodeFrontMatter(src, spans[0], spans[i])
		return fm, i + 1, diags
	}
	return nil, len(spans), []MarkdownDiagnostic{{Line: 1, Message: "frontmatter closing delimiter is missing"}}
}

func decodeFrontMatter(src string, open, close lineSpan) (*markdownFrontMatter, []MarkdownDiagnostic) {
	content := src[open.end:close.start]
	fm := &markdownFrontMatter{
		public: MarkdownFrontMatter{
			StartLine: open.line,
			EndLine:   close.line,
			Raw:       src[open.start:close.end],
		},
	}
	node, diags := parseYAMLFrontMatter(content, open.line+1)
	fm.public.Node = node
	return fm, diags
}

func parseYAMLFrontMatter(content string, firstLine int) (*yaml.Node, []MarkdownDiagnostic) {
	node := emptyDocumentMapping()
	if strings.TrimSpace(content) == "" {
		return node, nil
	}
	if err := yaml.Unmarshal([]byte(content), node); err != nil {
		return node, []MarkdownDiagnostic{{Line: yamlErrorLine(err, firstLine), Message: "invalid frontmatter: " + err.Error()}}
	}
	return node, duplicateYAMLKeyDiagnostics(node, firstLine)
}

func isFrontMatterDelimiter(line string) bool {
	return strings.TrimRight(line, "\r\n") == "---"
}

func yamlErrorLine(err error, firstLine int) int {
	match := yamlLinePattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return firstLine
	}
	line, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		return firstLine
	}
	return firstLine + line - 1
}

func parseMarkdownElements(src string, spans []lineSpan) []markdownElement {
	if len(spans) == 0 {
		return nil
	}
	var elements []markdownElement
	proseStart := spans[0].start
	sectionStart := -1
	var heading lineSpan
	var section MarkdownSection
	for _, span := range spans {
		level, name, ok := parseATXHeading(span.text)
		if !ok {
			continue
		}
		elements = appendPreviousElement(elements, src, proseStart, sectionStart, span.start, heading, section)
		sectionStart = span.start
		heading = span
		section = MarkdownSection{Level: level, Name: name, StartLine: span.line}
	}
	return appendPreviousElement(elements, src, proseStart, sectionStart, len(src), heading, section)
}

func appendPreviousElement(elements []markdownElement, src string, proseStart, sectionStart, end int, heading lineSpan, section MarkdownSection) []markdownElement {
	if sectionStart < 0 {
		if proseStart < end {
			return append(elements, markdownElement{kind: markdownProse, raw: src[proseStart:end]})
		}
		return elements
	}
	body := src[heading.end:end]
	section.Body = body
	section.EndLine = lineEnd(src[sectionStart:end], section.StartLine)
	return append(elements, markdownElement{
		kind: markdownSection, raw: src[sectionStart:end], section: section,
		heading: heading.text, body: body,
	})
}

func parseATXHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if len(line)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level > 6 || (level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t') {
		return 0, "", false
	}
	name := strings.TrimSpace(strings.TrimRight(trimmed[level:], "\r\n"))
	name = strings.TrimSpace(strings.TrimRight(name, "#"))
	return level, name, name != ""
}

func lineEnd(prefix string, fallback int) int {
	if prefix == "" {
		return fallback
	}
	count := strings.Count(prefix, "\n")
	if strings.HasSuffix(prefix, "\n") && count > 0 {
		count--
	}
	return fallback + count
}

func (n *MarkdownNote) sections() []MarkdownSection {
	var sections []MarkdownSection
	for _, elem := range n.elements {
		if elem.kind == markdownSection {
			sections = append(sections, elem.section)
		}
	}
	return sections
}

func duplicateRequiredSectionDiagnostics(sections []MarkdownSection, required []string) []MarkdownDiagnostic {
	requiredSet := map[string]bool{}
	for _, name := range required {
		requiredSet[normalizeSectionName(name)] = true
	}
	first := map[string]MarkdownSection{}
	var diags []MarkdownDiagnostic
	for _, section := range sections {
		key := normalizeSectionName(section.Name)
		if !requiredSet[key] {
			continue
		}
		if prev, ok := first[key]; ok {
			msg := fmt.Sprintf("duplicate required section %q first defined on line %d", section.Name, prev.StartLine)
			diags = append(diags, MarkdownDiagnostic{Line: section.StartLine, Message: msg})
			continue
		}
		first[key] = section
	}
	return diags
}

func normalizeSectionName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func duplicateYAMLKeyDiagnostics(node *yaml.Node, firstLine int) []MarkdownDiagnostic {
	var diags []MarkdownDiagnostic
	visitYAMLMapping(node, "", firstLine, &diags)
	return diags
}

func visitYAMLMapping(node *yaml.Node, path string, firstLine int, diags *[]MarkdownDiagnostic) {
	if node == nil {
		return
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		visitYAMLMapping(node.Content[0], path, firstLine, diags)
		return
	}
	if node.Kind != yaml.MappingNode {
		visitYAMLChildren(node, path, firstLine, diags)
		return
	}
	seen := map[string]int{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		full := yamlKeyPath(path, key.Value)
		if first, ok := seen[key.Value]; ok {
			msg := fmt.Sprintf("duplicate frontmatter key %q first defined on line %d", full, first)
			*diags = append(*diags, MarkdownDiagnostic{Line: firstLine + key.Line - 1, Message: msg})
		} else {
			seen[key.Value] = firstLine + key.Line - 1
		}
		visitYAMLMapping(node.Content[i+1], full, firstLine, diags)
	}
}

func visitYAMLChildren(node *yaml.Node, path string, firstLine int, diags *[]MarkdownDiagnostic) {
	for _, child := range node.Content {
		visitYAMLMapping(child, path, firstLine, diags)
	}
}

func yamlKeyPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func documentMapping(node *yaml.Node) *yaml.Node {
	if node == nil || node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return nil
	}
	if node.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return node.Content[0]
}

func ensureDocumentMapping(node *yaml.Node) *yaml.Node {
	mapping := documentMapping(node)
	if mapping != nil {
		return mapping
	}
	node.Kind = yaml.DocumentNode
	node.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	return node.Content[0]
}

func mappingValue(mapping *yaml.Node, name string) (*yaml.Node, bool) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			return mapping.Content[i+1], true
		}
	}
	return nil, false
}

func setMappingValue(mapping *yaml.Node, name string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			mapping.Content[i+1] = value
			return
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}
	mapping.Content = append(mapping.Content, key, value)
}

func yamlValueNode(value any) (*yaml.Node, error) {
	var doc yaml.Node
	raw, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""}, nil
	}
	return doc.Content[0], nil
}

func emptyDocumentMapping() *yaml.Node {
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
}
