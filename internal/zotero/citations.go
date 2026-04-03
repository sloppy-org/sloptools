package zotero

import (
	"context"
	"regexp"
	"strings"
)

var (
	latexCitationPattern  = regexp.MustCompile(`\\cite[a-zA-Z*]*\{([^}]+)\}`)
	pandocCitationPattern = regexp.MustCompile(`\[[^\]]*@([^\]]+)\]`)
	pandocKeyPattern      = regexp.MustCompile(`@([A-Za-z0-9:_./-]+)`)
)

func ExtractCitationKeys(text string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	appendKey := func(raw string) {
		key := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	for _, match := range latexCitationPattern.FindAllStringSubmatch(text, -1) {
		for _, raw := range strings.Split(match[1], ",") {
			appendKey(raw)
		}
	}
	for _, match := range pandocCitationPattern.FindAllStringSubmatch(text, -1) {
		for _, keyMatch := range pandocKeyPattern.FindAllStringSubmatch(match[0], -1) {
			appendKey(keyMatch[1])
		}
	}
	return out
}

func (r *Reader) ResolveItemsByCitationKey(ctx context.Context, keys []string) ([]Item, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	items, err := r.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]Item, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.CitationKey)
		if key == "" {
			continue
		}
		byKey[key] = item
	}
	out := make([]Item, 0, len(keys))
	for _, key := range keys {
		item, ok := byKey[strings.TrimSpace(key)]
		if !ok {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Reader) ResolveCitationText(ctx context.Context, text string) ([]Item, error) {
	return r.ResolveItemsByCitationKey(ctx, ExtractCitationKeys(text))
}
