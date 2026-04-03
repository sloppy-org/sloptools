package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/krystophny/sloppy/internal/surface"
)

func isPathWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) resolveTempArtifactsDir(cwdArg string) (string, string, error) {
	cwd := strings.TrimSpace(cwdArg)
	if cwd == "" {
		cwd = s.projectDir
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	rootAbs, err := filepath.Abs(cwd)
	if err != nil {
		return "", "", err
	}
	tmpAbs := filepath.Clean(filepath.Join(rootAbs, tempArtifactsDirRel))
	if !isPathWithinDir(tmpAbs, rootAbs) {
		return "", "", errors.New("temp artifacts directory escapes project root")
	}
	return rootAbs, tmpAbs, nil
}

func (s *Server) tempFileCreate(args map[string]interface{}) (map[string]interface{}, error) {
	rootAbs, tmpAbs, err := s.resolveTempArtifactsDir(strArg(args, "cwd"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpAbs, 0755); err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(strArg(args, "prefix"))
	if prefix == "" {
		prefix = "tmp"
	}
	prefix = strings.ReplaceAll(prefix, string(os.PathSeparator), "-")
	prefix = strings.ReplaceAll(prefix, "/", "-")
	suffix := strings.TrimSpace(strArg(args, "suffix"))
	if suffix == "" {
		suffix = ".md"
	}
	suffix = strings.ReplaceAll(suffix, string(os.PathSeparator), "")
	suffix = strings.ReplaceAll(suffix, "/", "")
	pattern := prefix + "-*" + suffix
	f, err := os.CreateTemp(tmpAbs, pattern)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content := strArg(args, "content")
	if content != "" {
		if _, err := f.WriteString(content); err != nil {
			return nil, err
		}
	}
	absPath := filepath.Clean(f.Name())
	relPath, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"ok":       true,
		"path":     filepath.ToSlash(relPath),
		"abs_path": absPath,
	}, nil
}

func (s *Server) tempFileRemove(args map[string]interface{}) (map[string]interface{}, error) {
	target := strings.TrimSpace(strArg(args, "path"))
	if target == "" {
		return nil, errors.New("path is required")
	}
	rootAbs, tmpAbs, err := s.resolveTempArtifactsDir(strArg(args, "cwd"))
	if err != nil {
		return nil, err
	}
	var absPath string
	if filepath.IsAbs(target) {
		absPath = filepath.Clean(target)
	} else {
		absPath = filepath.Clean(filepath.Join(rootAbs, target))
	}
	if !isPathWithinDir(absPath, tmpAbs) {
		return nil, errors.New("path must be under .sloppy/artifacts/tmp")
	}
	err = os.Remove(absPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	removed := err == nil
	relPath, relErr := filepath.Rel(rootAbs, absPath)
	if relErr != nil {
		relPath = absPath
	}
	return map[string]interface{}{
		"ok":      true,
		"path":    filepath.ToSlash(relPath),
		"removed": removed,
	}, nil
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return def
	}
}

func toolDefinitions() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(surface.MCPTools))
	for _, tool := range surface.MCPTools {
		schema := map[string]interface{}{"type": "object"}
		if len(tool.Required) > 0 {
			schema["required"] = append([]string(nil), tool.Required...)
		}
		if len(tool.Properties) > 0 {
			props := make(map[string]interface{}, len(tool.Properties))
			for k, v := range tool.Properties {
				prop := map[string]interface{}{
					"type":        v.Type,
					"description": v.Description,
				}
				if len(v.Enum) > 0 {
					prop["enum"] = v.Enum
				}
				props[k] = prop
			}
			schema["properties"] = props
		}
		out = append(out, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": schema,
		})
	}
	return out
}
