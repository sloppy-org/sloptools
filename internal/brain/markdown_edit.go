package brain

import "strings"

func (n *MarkdownNote) DeleteFrontMatterField(name string) bool {
	if strings.TrimSpace(name) == "" || n == nil || n.frontMatter == nil {
		return false
	}
	mapping := documentMapping(n.frontMatter.public.Node)
	if mapping == nil {
		return false
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != name {
			continue
		}
		mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
		n.frontMatter.modified = true
		return true
	}
	return false
}
