package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func (s *Server) dispatchBrain(method string, args map[string]interface{}) (map[string]interface{}, error) {
	switch method {
	case "brain.note.parse":
		return s.brainNoteParse(args)
	case "brain.note.validate":
		return s.brainNoteValidate(args)
	case "brain.vault.validate":
		return s.brainVaultValidate(args)
	case "brain.links.resolve":
		return s.brainLinksResolve(args)
	case "brain_search":
		return s.brainSearch(args)
	case "brain_backlinks":
		return s.brainBacklinks(args)
	default:
		return nil, errors.New("unknown brain method: " + method)
	}
}

func (s *Server) brainSearch(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(strArg(args, "config_path"))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	query := strings.TrimSpace(strArg(args, "query"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if query == "" {
		return nil, errors.New("query is required")
	}
	mode, err := brain.ParseSearchMode(strArg(args, "mode"))
	if err != nil {
		return nil, err
	}
	results, err := brain.Search(context.Background(), cfg, brain.SearchOptions{
		Sphere: brain.Sphere(sphere),
		Query:  query,
		Mode:   mode,
		Limit:  intArg(args, "limit", 50),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"sphere": sphere, "mode": string(mode), "query": query, "results": results, "count": len(results)}, nil
}

func (s *Server) brainBacklinks(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(strArg(args, "config_path"))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	target := strings.TrimSpace(strArg(args, "target"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if target == "" {
		return nil, errors.New("target is required")
	}
	results, err := brain.Backlinks(context.Background(), cfg, brain.BacklinkOptions{
		Sphere: brain.Sphere(sphere),
		Target: target,
		Limit:  intArg(args, "limit", 50),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"sphere": sphere, "target": target, "results": results, "count": len(results)}, nil
}
