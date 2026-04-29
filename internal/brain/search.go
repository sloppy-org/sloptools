package brain

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type SearchMode string

const (
	SearchText         SearchMode = "text"
	SearchRegex        SearchMode = "regex"
	SearchWikilink     SearchMode = "wikilink"
	SearchMarkdownLink SearchMode = "markdown_link"
	SearchAlias        SearchMode = "alias"
)

type SearchOptions struct {
	Sphere Sphere
	Query  string
	Mode   SearchMode
	Limit  int
}

type BacklinkOptions struct {
	Sphere Sphere
	Target string
	Limit  int
}

type SearchResult struct {
	Sphere   Sphere `json:"sphere"`
	Path     string `json:"path"`
	Rel      string `json:"rel"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
	NoteType string `json:"note_type,omitempty"`
	Why      string `json:"why"`
}

const defaultSearchLimit = 50

var (
	wikilinkPattern     = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
	markdownLinkPattern = regexp.MustCompile(`\[[^\]\n]*\]\(([^)\s]+)(?:\s+[^)]*)?\)`)
)

func Search(ctx context.Context, cfg *Config, opts SearchOptions) ([]SearchResult, error) {
	vault, err := searchVault(cfg, opts.Sphere)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return nil, errors.New("query is required")
	}
	mode, err := ParseSearchMode(string(opts.Mode))
	if err != nil {
		return nil, err
	}
	if mode == SearchAlias {
		return searchAliases(ctx, vault, query, normalizedLimit(opts.Limit))
	}
	pattern := searchPattern(mode, query)
	fixed := mode == SearchText
	results, err := rgMatches(ctx, vault, pattern, fixed, normalizedLimit(opts.Limit))
	if err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Why = string(mode) + ":" + query
	}
	return results, nil
}

func Backlinks(ctx context.Context, cfg *Config, opts BacklinkOptions) ([]SearchResult, error) {
	vault, err := searchVault(cfg, opts.Sphere)
	if err != nil {
		return nil, err
	}
	target, err := resolveSearchTarget(vault, opts.Target)
	if err != nil {
		return nil, err
	}
	files, err := rgFiles(ctx, vault)
	if err != nil {
		return nil, err
	}
	limit := normalizedLimit(opts.Limit)
	var results []SearchResult
	for _, rel := range files {
		path := filepath.Join(vault.Root, filepath.FromSlash(rel))
		if filepath.Clean(path) == target.Path {
			continue
		}
		found, err := backlinksInFile(vault, path, rel, target)
		if err != nil {
			return nil, err
		}
		results = append(results, found...)
		if len(results) >= limit {
			break
		}
	}
	sortResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func searchVault(cfg *Config, sphere Sphere) (Vault, error) {
	if cfg == nil {
		return Vault{}, &PathError{Kind: ErrorInvalidConfig, Sphere: sphere, Err: errors.New("config is nil")}
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return Vault{}, &PathError{Kind: ErrorUnknownVault, Sphere: normalizeSphere(sphere)}
	}
	return vault, nil
}

func ParseSearchMode(raw string) (SearchMode, error) {
	mode := SearchMode(strings.TrimSpace(raw))
	switch mode {
	case "":
		return SearchText, nil
	case SearchRegex, SearchWikilink, SearchMarkdownLink, SearchAlias:
		return mode, nil
	case SearchText:
		return SearchText, nil
	default:
		return "", fmt.Errorf("unsupported search mode %q", raw)
	}
}

func normalizedLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func searchPattern(mode SearchMode, query string) string {
	switch mode {
	case SearchWikilink:
		return `\[\[[^\]\n]*` + regexp.QuoteMeta(query) + `[^\]\n]*\]\]`
	case SearchMarkdownLink:
		return `\[[^\]\n]*\]\([^)\n]*` + regexp.QuoteMeta(query) + `[^)\n]*\)`
	default:
		return query
	}
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		LineNumber int `json:"line_number"`
		Lines      struct {
			Text string `json:"text"`
		} `json:"lines"`
	} `json:"data"`
}

func rgMatches(ctx context.Context, vault Vault, pattern string, fixed bool, limit int) ([]SearchResult, error) {
	args := []string{"--json", "--line-number", "--with-filename", "--color", "never", "--glob", "*.md"}
	args = appendExcludes(args, vault)
	if fixed {
		args = append(args, "-F")
	}
	args = append(args, "--", pattern, ".")
	out, err := runRG(ctx, vault.Root, args...)
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		var ev rgEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil || ev.Type != "match" {
			continue
		}
		result, ok := rgResult(vault, ev)
		if !ok {
			continue
		}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sortResults(results)
	return results, nil
}

func rgResult(vault Vault, ev rgEvent) (SearchResult, bool) {
	rel := filepath.Clean(filepath.FromSlash(ev.Data.Path.Text))
	if rel == "." || strings.HasPrefix(rel, "..") {
		return SearchResult{}, false
	}
	path := filepath.Join(vault.Root, rel)
	resolved, err := resolveCandidate(vault, path, OpIndex)
	if err != nil {
		return SearchResult{}, false
	}
	return SearchResult{
		Sphere:   vault.Sphere,
		Path:     resolved.Path,
		Rel:      filepath.ToSlash(resolved.Rel),
		Line:     ev.Data.LineNumber,
		Text:     strings.TrimSpace(ev.Data.Lines.Text),
		NoteType: noteType(vault, resolved.Path),
	}, true
}

func rgFiles(ctx context.Context, vault Vault) ([]string, error) {
	args := []string{"--files", "--glob", "*.md"}
	args = appendExcludes(args, vault)
	out, err := runRG(ctx, vault.Root, args...)
	if err != nil {
		return nil, err
	}
	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		rel := strings.TrimSpace(scanner.Text())
		if rel != "" {
			files = append(files, filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel))))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func appendExcludes(args []string, vault Vault) []string {
	for _, exclude := range vault.Exclude {
		slash := filepath.ToSlash(filepath.Clean(exclude))
		args = append(args, "--glob", "!"+slash, "--glob", "!"+slash+"/**")
	}
	return args
}

func runRG(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil, nil
	}
	if errors.As(err, &exitErr) {
		return nil, fmt.Errorf("rg: %s", strings.TrimSpace(string(exitErr.Stderr)))
	}
	return nil, fmt.Errorf("rg: %w", err)
}

func searchAliases(ctx context.Context, vault Vault, query string, limit int) ([]SearchResult, error) {
	files, err := rgFiles(ctx, vault)
	if err != nil {
		return nil, err
	}
	var results []SearchResult
	for _, rel := range files {
		path := filepath.Join(vault.Root, filepath.FromSlash(rel))
		found, err := aliasesInFile(vault, path, rel, query)
		if err != nil {
			return nil, err
		}
		results = append(results, found...)
		if len(results) >= limit {
			break
		}
	}
	sortResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func aliasesInFile(vault Vault, path, rel, query string) ([]SearchResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	note, diags := ParseMarkdownNote(string(data), MarkdownParseOptions{})
	if len(diags) > 0 {
		return nil, nil
	}
	fm, ok := note.FrontMatter()
	if !ok {
		return nil, nil
	}
	var out []SearchResult
	for _, field := range []string{"aliases", "alias"} {
		node, ok := note.FrontMatterField(field)
		if !ok {
			continue
		}
		for _, alias := range aliasValues(node) {
			if !strings.Contains(strings.ToLower(alias.value), strings.ToLower(query)) {
				continue
			}
			out = append(out, SearchResult{
				Sphere:   vault.Sphere,
				Path:     path,
				Rel:      filepath.ToSlash(rel),
				Line:     fm.StartLine + alias.line,
				Text:     alias.value,
				NoteType: noteType(vault, path),
				Why:      "alias:" + alias.value,
			})
		}
	}
	return out, nil
}

type aliasValue struct {
	value string
	line  int
}

func aliasValues(node *yaml.Node) []aliasValue {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.SequenceNode:
		out := make([]aliasValue, 0, len(node.Content))
		for _, child := range node.Content {
			if strings.TrimSpace(child.Value) != "" {
				out = append(out, aliasValue{value: strings.TrimSpace(child.Value), line: child.Line})
			}
		}
		return out
	case yaml.ScalarNode:
		if strings.TrimSpace(node.Value) == "" {
			return nil
		}
		return []aliasValue{{value: strings.TrimSpace(node.Value), line: node.Line}}
	default:
		return nil
	}
}

func backlinksInFile(vault Vault, path, rel string, target ResolvedPath) ([]SearchResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []SearchResult
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := scanner.Text()
		for _, match := range wikilinkPattern.FindAllStringSubmatch(line, -1) {
			if wikilinkMatches(vault, match[1], target) {
				out = append(out, linkResult(vault, path, rel, lineNo, line, "wikilink:"+match[1]))
			}
		}
		for _, match := range markdownLinkPattern.FindAllStringSubmatch(line, -1) {
			resolved, err := resolveMarkdownLink(vault, path, match[1])
			if err == nil && resolved.Path == target.Path {
				out = append(out, linkResult(vault, path, rel, lineNo, line, "markdown_link:"+match[1]))
			}
		}
	}
	return out, scanner.Err()
}

func resolveMarkdownLink(vault Vault, notePath, rawLink string) (ResolvedPath, error) {
	note, err := resolveCandidate(vault, notePath, OpLink)
	if err != nil {
		return ResolvedPath{}, err
	}
	target, err := cleanLinkTarget(rawLink)
	if err != nil {
		return ResolvedPath{}, err
	}
	if target == "" {
		target = note.Path
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(note.Path), target)
	}
	return resolveCandidate(vault, target, OpLink)
}

func linkResult(vault Vault, path, rel string, line int, text, why string) SearchResult {
	return SearchResult{
		Sphere:   vault.Sphere,
		Path:     path,
		Rel:      filepath.ToSlash(rel),
		Line:     line,
		Text:     strings.TrimSpace(text),
		NoteType: noteType(vault, path),
		Why:      why,
	}
}

func wikilinkMatches(vault Vault, raw string, target ResolvedPath) bool {
	clean := strings.TrimSpace(strings.SplitN(strings.SplitN(raw, "|", 2)[0], "#", 2)[0])
	if clean == "" || hasURLScheme(clean) {
		return false
	}
	clean = strings.TrimSuffix(filepath.ToSlash(clean), ".md")
	for _, candidate := range wikiTargetNames(vault, target) {
		if strings.EqualFold(clean, candidate) {
			return true
		}
	}
	return false
}

func wikiTargetNames(vault Vault, target ResolvedPath) []string {
	relVault := strings.TrimSuffix(filepath.ToSlash(target.Rel), ".md")
	relBrain, err := filepath.Rel(vault.BrainRoot(), target.Path)
	if err != nil {
		relBrain = filepath.Base(target.Path)
	}
	relBrain = strings.TrimSuffix(filepath.ToSlash(relBrain), ".md")
	base := strings.TrimSuffix(filepath.Base(target.Path), ".md")
	return []string{relVault, relBrain, base}
}

func noteType(vault Vault, path string) string {
	rel, err := filepath.Rel(vault.BrainRoot(), path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

func sortResults(results []SearchResult) {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Rel != results[j].Rel {
			return results[i].Rel < results[j].Rel
		}
		if results[i].Line != results[j].Line {
			return results[i].Line < results[j].Line
		}
		return results[i].Why < results[j].Why
	})
}
