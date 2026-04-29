package brain

import (
	"errors"
	"path/filepath"
	"strings"
)

func resolveSearchTarget(vault Vault, rawPath string) (ResolvedPath, error) {
	candidate := strings.TrimSpace(rawPath)
	if candidate == "" {
		return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Op: OpRead, Sphere: vault.Sphere, Err: errors.New("path is required")}
	}
	if filepath.IsAbs(candidate) {
		return resolveCandidate(vault, candidate, OpRead)
	}
	slash := filepath.ToSlash(filepath.Clean(candidate))
	if pathStartsWith(slash, filepath.ToSlash(vault.Brain)) || pathMatchesExclude(slash, vault.Exclude) {
		return resolveCandidate(vault, filepath.Join(vault.Root, filepath.FromSlash(slash)), OpRead)
	}
	return resolveCandidate(vault, filepath.Join(vault.BrainRoot(), filepath.FromSlash(slash)), OpRead)
}

func pathMatchesExclude(path string, excludes []string) bool {
	for _, exclude := range excludes {
		if pathStartsWith(path, filepath.ToSlash(filepath.Clean(exclude))) {
			return true
		}
	}
	return false
}

func pathStartsWith(path, prefix string) bool {
	prefix = strings.Trim(prefix, "/")
	path = strings.Trim(path, "/")
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
