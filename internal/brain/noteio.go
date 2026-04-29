package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ReadNoteFile(cfg *Config, sphere Sphere, rawPath string) (ResolvedPath, []byte, error) {
	resolved, err := ResolveNotePath(cfg, sphere, rawPath)
	if err != nil {
		return ResolvedPath{}, nil, err
	}
	data, err := os.ReadFile(resolved.Path)
	if err != nil {
		return ResolvedPath{}, nil, err
	}
	return resolved, data, nil
}

func ResolveNotePath(cfg *Config, sphere Sphere, rawPath string) (ResolvedPath, error) {
	if cfg == nil {
		return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Sphere: sphere, Err: fmt.Errorf("config is nil")}
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return ResolvedPath{}, &PathError{Kind: ErrorUnknownVault, Sphere: normalizeSphere(sphere)}
	}
	candidate := strings.TrimSpace(rawPath)
	if candidate == "" {
		return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Op: OpRead, Sphere: vault.Sphere, Err: fmt.Errorf("path is required")}
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
