package brain

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Resolver struct {
	config *Config
}

type ResolvedPath struct {
	Sphere    Sphere
	VaultRoot string
	BrainRoot string
	Path      string
	Rel       string
}

func (r Resolver) ResolvePath(sphere Sphere, rawPath string, op PathOp) (ResolvedPath, error) {
	vault, err := r.vault(sphere)
	if err != nil {
		return ResolvedPath{}, err
	}
	candidate := strings.TrimSpace(rawPath)
	if candidate == "" {
		return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Op: op, Sphere: vault.Sphere, Err: fmt.Errorf("path is required")}
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(vault.BrainRoot(), candidate)
	}
	return resolveCandidate(vault, candidate, op)
}

func (r Resolver) ResolveLink(sphere Sphere, notePath, rawLink string) (ResolvedPath, error) {
	vault, err := r.vault(sphere)
	if err != nil {
		return ResolvedPath{}, err
	}
	noteCandidate := strings.TrimSpace(notePath)
	if !filepath.IsAbs(noteCandidate) {
		noteCandidate = filepath.Join(vault.BrainRoot(), noteCandidate)
	}
	note, err := resolveCandidate(vault, noteCandidate, OpLink)
	if err != nil {
		return ResolvedPath{}, err
	}
	target, err := cleanLinkTarget(rawLink)
	if err != nil {
		return ResolvedPath{}, &PathError{Kind: KindOf(err), Op: OpLink, Sphere: vault.Sphere, Path: note.Path, Link: rawLink, Err: err}
	}
	if target == "" {
		target = note.Path
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(note.Path), target)
	}
	resolved, err := resolveCandidate(vault, target, OpLink)
	if err != nil {
		var pathErr *PathError
		if asPathError(err, &pathErr) {
			pathErr.Link = rawLink
		}
		return ResolvedPath{}, err
	}
	return resolved, nil
}

func (r Resolver) vault(sphere Sphere) (Vault, error) {
	if r.config == nil {
		return Vault{}, &PathError{Kind: ErrorInvalidConfig, Sphere: sphere, Err: fmt.Errorf("config is nil")}
	}
	vault, ok := r.config.Vault(sphere)
	if !ok {
		return Vault{}, &PathError{Kind: ErrorUnknownVault, Sphere: normalizeSphere(sphere)}
	}
	return vault, nil
}

func resolveCandidate(vault Vault, rawPath string, op PathOp) (ResolvedPath, error) {
	clean := filepath.Clean(rawPath)
	if !filepath.IsAbs(clean) {
		abs, err := filepath.Abs(clean)
		if err != nil {
			return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Op: op, Sphere: vault.Sphere, Path: rawPath, Err: err}
		}
		clean = filepath.Clean(abs)
	}
	if err := checkPathAllowed(vault, clean, op); err != nil {
		return ResolvedPath{}, err
	}
	if evaluated, ok, err := evalExisting(clean); err != nil {
		return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Op: op, Sphere: vault.Sphere, Path: clean, Err: err}
	} else if ok {
		if err := checkPathAllowed(vault, evaluated, op); err != nil {
			return ResolvedPath{}, err
		}
	}
	rel, err := filepath.Rel(vault.Root, clean)
	if err != nil {
		return ResolvedPath{}, &PathError{Kind: ErrorInvalidConfig, Op: op, Sphere: vault.Sphere, Path: clean, Err: err}
	}
	return ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: vault.BrainRoot(), Path: clean, Rel: rel}, nil
}

func checkPathAllowed(vault Vault, path string, op PathOp) error {
	if !isWithin(vault.Root, path) {
		return &PathError{Kind: ErrorOutOfVault, Op: op, Sphere: vault.Sphere, Path: path}
	}
	for _, exclude := range vault.Exclude {
		excluded := filepath.Join(vault.Root, exclude)
		if isWithin(excluded, path) {
			return &PathError{Kind: ErrorExcludedPath, Op: op, Sphere: vault.Sphere, Path: path}
		}
	}
	return nil
}

func cleanLinkTarget(raw string) (string, error) {
	target := strings.TrimSpace(raw)
	target = strings.TrimPrefix(strings.TrimSuffix(target, ">"), "<")
	if target == "" {
		return "", &PathError{Kind: ErrorUnsupportedLink, Err: fmt.Errorf("empty link")}
	}
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target = target[:i]
	}
	if hasURLScheme(target) {
		return "", &PathError{Kind: ErrorUnsupportedLink, Err: fmt.Errorf("external link")}
	}
	unescaped, err := url.PathUnescape(target)
	if err != nil {
		return "", &PathError{Kind: ErrorUnsupportedLink, Err: err}
	}
	return filepath.FromSlash(unescaped), nil
}

func hasURLScheme(target string) bool {
	if runtime.GOOS == "windows" && len(target) >= 2 && target[1] == ':' {
		return false
	}
	colon := strings.IndexByte(target, ':')
	if colon <= 0 {
		return false
	}
	for _, r := range target[:colon] {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func evalExisting(path string) (string, bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false, err
	}
	return filepath.Clean(evaluated), true, nil
}

func asPathError(err error, target **PathError) bool {
	pathErr, ok := err.(*PathError)
	if ok {
		*target = pathErr
	}
	return ok
}
