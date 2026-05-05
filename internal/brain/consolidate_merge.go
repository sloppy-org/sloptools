package brain

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// mergeFrontMatter unions YAML frontmatter, marking conflicting scalars.
func mergeFrontMatter(loser, survivor *MarkdownNote, loserPath, survivorPath string) (string, error) {
	loserMap := frontMatterMap(loser)
	survivorMap := frontMatterMap(survivor)
	merged := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range orderedFrontMatterKeys(loser, survivor) {
		left, leftOK := loserMap[key]
		right, rightOK := survivorMap[key]
		switch {
		case leftOK && rightOK:
			node, err := unionYAMLValues(left, right, loserPath, survivorPath)
			if err != nil {
				return "", err
			}
			appendMappingEntry(merged, key, node)
		case leftOK:
			appendMappingEntry(merged, key, left)
		case rightOK:
			appendMappingEntry(merged, key, right)
		}
	}
	out, err := yaml.Marshal(merged)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n") + "\n", nil
}

func frontMatterMap(note *MarkdownNote) map[string]*yaml.Node {
	out := map[string]*yaml.Node{}
	mapping := frontMatterMapping(note)
	if mapping == nil {
		return out
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		out[mapping.Content[i].Value] = mapping.Content[i+1]
	}
	return out
}

func orderedFrontMatterKeys(loser, survivor *MarkdownNote) []string {
	seen := map[string]bool{}
	var keys []string
	for _, source := range []*MarkdownNote{survivor, loser} {
		mapping := frontMatterMapping(source)
		if mapping == nil {
			continue
		}
		for i := 0; i+1 < len(mapping.Content); i += 2 {
			key := mapping.Content[i].Value
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func frontMatterMapping(note *MarkdownNote) *yaml.Node {
	if note == nil {
		return nil
	}
	fm, ok := note.FrontMatter()
	if !ok || fm.Node == nil {
		return nil
	}
	return documentMapping(fm.Node)
}

func appendMappingEntry(mapping *yaml.Node, key string, value *yaml.Node) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

func unionYAMLValues(left, right *yaml.Node, leftPath, rightPath string) (*yaml.Node, error) {
	if left.Kind == yaml.SequenceNode && right.Kind == yaml.SequenceNode {
		return uniqueSequenceUnion(left, right), nil
	}
	if scalarEqual(left, right) {
		return right, nil
	}
	marker := fmt.Sprintf(">>>>>>> %s | <<<<<<< %s | %s vs %s",
		leftPath, rightPath,
		yamlScalarValue(left), yamlScalarValue(right),
	)
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: marker}, nil
}

func uniqueSequenceUnion(left, right *yaml.Node) *yaml.Node {
	out := &yaml.Node{Kind: yaml.SequenceNode}
	seen := map[string]bool{}
	for _, source := range []*yaml.Node{right, left} {
		for _, child := range source.Content {
			key := child.Value
			if seen[key] {
				continue
			}
			seen[key] = true
			out.Content = append(out.Content, child)
		}
	}
	return out
}

func scalarEqual(left, right *yaml.Node) bool {
	if left.Kind != yaml.ScalarNode || right.Kind != yaml.ScalarNode {
		return false
	}
	return strings.TrimSpace(left.Value) == strings.TrimSpace(right.Value)
}

func yamlScalarValue(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	out, err := yaml.Marshal(node)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// mergeBodies aligns survivor and loser bodies by H2 heading. Sections present
// in both with diverging prose get conflict markers; sections only in the
// loser are appended after a `## (from ...)` heading.
func mergeBodies(loser, survivor *MarkdownNote, loserPath string) string {
	loserBy, loserKeys := indexH2Sections(loser)
	var out strings.Builder
	survivorKeys := map[string]bool{}
	for _, section := range h2Sections(survivor) {
		key := normalizeSectionName(section.Name)
		survivorKeys[key] = true
		if loserSection, ok := loserBy[key]; ok {
			out.WriteString(renderMergedSection(section, loserSection))
			continue
		}
		out.WriteString(renderSurvivorOnlySection(section))
	}
	for _, key := range loserKeys {
		if survivorKeys[key] {
			continue
		}
		section := loserBy[key]
		fmt.Fprintf(&out, "\n## (from %s) %s\n\n%s", loserPath, section.Name, strings.TrimLeft(section.Body, "\n"))
		if !strings.HasSuffix(section.Body, "\n") {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func indexH2Sections(note *MarkdownNote) (map[string]MarkdownSection, []string) {
	by := map[string]MarkdownSection{}
	var keys []string
	for _, section := range h2Sections(note) {
		key := normalizeSectionName(section.Name)
		by[key] = section
		keys = append(keys, key)
	}
	return by, keys
}

func h2Sections(note *MarkdownNote) []MarkdownSection {
	if note == nil {
		return nil
	}
	var out []MarkdownSection
	for _, section := range note.Sections() {
		if section.Level == 2 {
			out = append(out, section)
		}
	}
	return out
}

func renderMergedSection(survivor, loser MarkdownSection) string {
	survivorBody := strings.TrimSpace(survivor.Body)
	loserBody := strings.TrimSpace(loser.Body)
	if survivorBody == loserBody {
		return fmt.Sprintf("## %s\n%s", survivor.Name, survivor.Body)
	}
	return fmt.Sprintf("## %s\n<<< loser\n%s\n=== survivor\n%s\n>>>\n", survivor.Name, loserBody, survivorBody)
}

func renderSurvivorOnlySection(section MarkdownSection) string {
	return fmt.Sprintf("## %s\n%s", section.Name, section.Body)
}

// MergeBodyHasUnresolvedConflicts returns true when conflict markers remain.
func MergeBodyHasUnresolvedConflicts(body string) bool {
	return strings.Contains(body, "<<<<<<<") || strings.Contains(body, ">>>>>>>")
}
