package mcp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/braincatalog"
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
	err = brain.WalkVaultNotes(cfg, brain.Sphere(sphere), func(snapshot brain.NoteSnapshot) error {
		entry, err := s.brainInspectNoteContent(cfg, snapshot, true)
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
	return s.brainInspectNoteContent(cfg, brain.NoteSnapshot{Source: source, Body: string(body)}, validate)
}

func (s *Server) brainInspectNoteContent(cfg *brain.Config, snapshot brain.NoteSnapshot, validate bool) (map[string]interface{}, error) {
	kind, note, parseDiags := brainNoteKind(snapshot.Body)
	result := map[string]interface{}{
		"source": snapshot.Source,
		"kind":   kind,
	}
	switch kind {
	case "commitment":
		if validate {
			parsed := braingtd.ParseAndValidate(snapshot.Body)
			result["parsed"] = parsed.Commitment
			result["commitment"] = parsed.Commitment
			result["diagnostics"] = parsed.Diagnostics
			result["count"] = len(parsed.Diagnostics)
			result["valid"] = len(parsed.Diagnostics) == 0
			return result, nil
		}
		commitment, _, diags := braingtd.ParseCommitmentMarkdown(snapshot.Body)
		result["parsed"] = commitment
		result["commitment"] = commitment
		result["diagnostics"] = diags
		result["count"] = len(diags)
		return result, nil
	case "folder":
		var parsed brain.FolderNote
		var diags []brain.MarkdownDiagnostic
		if validate {
			parsed, diags = brain.ValidateFolderNote(snapshot.Body, brain.LinkValidationContext{Config: cfg, Sphere: snapshot.Source.Sphere, Path: snapshot.Source.Path})
		} else {
			parsed, diags = brain.ParseFolderNote(snapshot.Body)
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
			parsed, diags = brain.ValidateGlossaryNote(snapshot.Body, brain.LinkValidationContext{Config: cfg, Sphere: snapshot.Source.Sphere, Path: snapshot.Source.Path})
		} else {
			parsed, diags = brain.ParseGlossaryNote(snapshot.Body)
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
			parsed, diags = brain.ValidateAttentionFields(snapshot.Body)
		} else {
			parsed, diags = brain.ParseAttentionFields(snapshot.Body)
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

func (s *Server) brainConfigGet(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"config_path": s.brainConfigArg(args),
		"vaults":      brain.ListVaults(cfg),
		"count":       len(cfg.Vaults),
	}, nil
}

func (s *Server) brainVaultList(args map[string]interface{}) (map[string]interface{}, error) {
	return s.brainConfigGet(args)
}

func (s *Server) brainGTDParseVault(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	records, err := braincatalog.ParseGTDVault(cfg, brain.Sphere(sphere))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sphere":      sphere,
		"commitments": records,
		"count":       len(records),
	}, nil
}

func (s *Server) brainGTDListVault(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	items, err := braincatalog.ListGTDVault(cfg, brain.Sphere(sphere), braincatalog.GTDListFilter{
		Status:  strArg(args, "status"),
		Person:  strArg(args, "person"),
		Project: strArg(args, "project"),
		Source:  strArg(args, "source"),
		Limit:   intArg(args, "limit", 0),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sphere": sphere,
		"filter": map[string]interface{}{
			"status":  strArg(args, "status"),
			"person":  strArg(args, "person"),
			"project": strArg(args, "project"),
			"source":  strArg(args, "source"),
			"limit":   intArg(args, "limit", 0),
		},
		"items": items,
		"count": len(items),
	}, nil
}

func (s *Server) brainFolderLinks(args map[string]interface{}) (map[string]interface{}, error) {
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
	parsed, diags := brain.ParseFolderNote(string(body))
	return map[string]interface{}{
		"source": source,
		"folder": parsed,
		"links": map[string]interface{}{
			"markdown_links": parsed.MarkdownLinks,
			"wikilinks":      parsed.Wikilinks,
		},
		"diagnostics": diags,
		"count":       len(diags),
	}, nil
}

func (s *Server) brainFolderAudit(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	notes, err := brain.AuditFolderVault(cfg, brain.Sphere(sphere))
	if err != nil {
		return nil, err
	}
	issues := 0
	for _, note := range notes {
		issues += note.Count
	}
	return map[string]interface{}{
		"sphere": sphere,
		"notes":  notes,
		"count":  len(notes),
		"issues": issues,
		"valid":  issues == 0,
	}, nil
}

func (s *Server) brainEntitiesCandidates(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	candidates, err := brain.EntityCandidates(cfg, brain.Sphere(sphere))
	if err != nil {
		return nil, err
	}
	limit := intArg(args, "limit", 0)
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return map[string]interface{}{
		"sphere":     sphere,
		"candidates": candidates,
		"count":      len(candidates),
	}, nil
}
