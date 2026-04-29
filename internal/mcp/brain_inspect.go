package mcp

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/store"
)

func NewServerWithStoreAndBrainConfig(projectDir string, st *store.Store, brainConfigPath string) *Server {
	srv := NewServerWithStore(projectDir, st)
	srv.brainConfigPath = strings.TrimSpace(brainConfigPath)
	return srv
}

func (s *Server) brainConfigArg(args map[string]interface{}) string {
	if path := strings.TrimSpace(strArg(args, "config_path")); path != "" {
		return path
	}
	return s.brainConfigPath
}

func (s *Server) brainNoteParse(args map[string]interface{}) (map[string]interface{}, error) {
	return s.brainInspectNote(args, false)
}

func (s *Server) brainNoteValidate(args map[string]interface{}) (map[string]interface{}, error) {
	return s.brainInspectNote(args, true)
}

func (s *Server) brainVaultValidate(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, fmt.Errorf("unknown vault %q", sphere)
	}
	var notes []map[string]interface{}
	err = filepath.WalkDir(vault.BrainRoot(), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		resolved := brain.ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: vault.BrainRoot(), Path: filepath.Clean(path), Rel: filepath.ToSlash(rel)}
		entry, err := s.brainInspectNoteContent(cfg, resolved, string(data), true)
		if err != nil {
			return err
		}
		notes = append(notes, map[string]interface{}{
			"source":      entry["source"],
			"kind":        entry["kind"],
			"diagnostics": entry["diagnostics"],
			"count":       entry["count"],
			"valid":       entry["valid"],
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	issues := 0
	for _, note := range notes {
		if count, ok := note["count"].(int); ok {
			issues += count
		}
	}
	return map[string]interface{}{
		"source": brain.ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: vault.BrainRoot()},
		"notes":  notes,
		"count":  len(notes),
		"issues": issues,
		"valid":  issues == 0,
		"sphere": vault.Sphere,
		"vault":  vault,
	}, nil
}

func (s *Server) brainLinksResolve(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	path := strings.TrimSpace(strArg(args, "path"))
	link := strings.TrimSpace(strArg(args, "link"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if path == "" {
		return nil, errors.New("path is required")
	}
	if link == "" {
		return nil, errors.New("link is required")
	}
	source, _, err := brain.ReadNoteFile(cfg, brain.Sphere(sphere), path)
	if err != nil {
		return nil, err
	}
	resolved, err := cfg.Resolver().ResolveLink(brain.Sphere(sphere), source.Path, link)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"source":   source,
		"link":     link,
		"resolved": resolved,
	}, nil
}

func (s *Server) brainInspectNote(args map[string]interface{}, validate bool) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	path := strings.TrimSpace(strArg(args, "path"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if path == "" {
		return nil, errors.New("path is required")
	}
	source, body, err := brain.ReadNoteFile(cfg, brain.Sphere(sphere), path)
	if err != nil {
		return nil, err
	}
	return s.brainInspectNoteContent(cfg, source, string(body), validate)
}

func (s *Server) brainInspectNoteContent(cfg *brain.Config, source brain.ResolvedPath, body string, validate bool) (map[string]interface{}, error) {
	kind, note, parseDiags := brainNoteKind(body)
	result := map[string]interface{}{
		"source": source,
		"kind":   kind,
	}
	switch kind {
	case "commitment":
		if validate {
			parsed := braingtd.ParseAndValidate(body)
			result["parsed"] = parsed.Commitment
			result["commitment"] = parsed.Commitment
			result["diagnostics"] = parsed.Diagnostics
			result["count"] = len(parsed.Diagnostics)
			result["valid"] = len(parsed.Diagnostics) == 0
			return result, nil
		}
		commitment, _, diags := braingtd.ParseCommitmentMarkdown(body)
		result["parsed"] = commitment
		result["commitment"] = commitment
		result["diagnostics"] = diags
		result["count"] = len(diags)
		return result, nil
	case "folder":
		var parsed brain.FolderNote
		var diags []brain.MarkdownDiagnostic
		if validate {
			parsed, diags = brain.ValidateFolderNote(body, brain.LinkValidationContext{Config: cfg, Sphere: source.Sphere, Path: source.Path})
		} else {
			parsed, diags = brain.ParseFolderNote(body)
		}
		result["parsed"] = parsed
		result["folder"] = parsed
		result["diagnostics"] = diags
		result["count"] = len(diags)
		if validate {
			result["valid"] = len(diags) == 0
		}
		return result, nil
	case "glossary":
		var parsed brain.GlossaryNote
		var diags []brain.MarkdownDiagnostic
		if validate {
			parsed, diags = brain.ValidateGlossaryNote(body, brain.LinkValidationContext{Config: cfg, Sphere: source.Sphere, Path: source.Path})
		} else {
			parsed, diags = brain.ParseGlossaryNote(body)
		}
		result["parsed"] = parsed
		result["glossary"] = parsed
		result["diagnostics"] = diags
		result["count"] = len(diags)
		if validate {
			result["valid"] = len(diags) == 0
		}
		return result, nil
	case "attention", "human", "project", "topic", "institution":
		var parsed brain.AttentionFields
		var diags []brain.MarkdownDiagnostic
		if validate {
			parsed, diags = brain.ValidateAttentionFields(body)
		} else {
			parsed, diags = brain.ParseAttentionFields(body)
		}
		result["parsed"] = parsed
		result["attention"] = parsed
		result["diagnostics"] = diags
		result["count"] = len(diags)
		if validate {
			result["valid"] = len(diags) == 0
		}
		return result, nil
	default:
		result["parsed"] = map[string]interface{}{"sections": note.Sections()}
		result["markdown"] = map[string]interface{}{"sections": note.Sections()}
		result["diagnostics"] = parseDiags
		result["count"] = len(parseDiags)
		result["valid"] = len(parseDiags) == 0
		return result, nil
	}
}

func brainNoteKind(body string) (string, *brain.MarkdownNote, []brain.MarkdownDiagnostic) {
	note, diags := brain.ParseMarkdownNote(body, brain.MarkdownParseOptions{})
	kind := ""
	if note != nil {
		if node, ok := note.FrontMatterField("kind"); ok {
			kind = strings.ToLower(strings.TrimSpace(node.Value))
			switch kind {
			case "gtd":
				kind = "commitment"
			}
		}
	}
	return kind, note, diags
}
