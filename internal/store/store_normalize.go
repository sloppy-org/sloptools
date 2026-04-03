package store

import (
	"path/filepath"
	"strings"
)

func normalizeWorkspaceName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeWorkspacePath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return filepath.Clean(abs)
}

func normalizeActorName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeActorKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case ActorKindHuman:
		return ActorKindHuman
	case ActorKindAgent:
		return ActorKindAgent
	default:
		return ""
	}
}

func normalizeActorEmail(email string) string {
	clean := strings.ToLower(strings.TrimSpace(email))
	if clean == "" {
		return ""
	}
	return clean
}

func normalizeActorProvider(provider string) string {
	clean := strings.TrimSpace(provider)
	switch {
	case clean == "":
		return ""
	case strings.EqualFold(clean, "manual"):
		return "manual"
	default:
		return normalizeExternalAccountProvider(clean)
	}
}
