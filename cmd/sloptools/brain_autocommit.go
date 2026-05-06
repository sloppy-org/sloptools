package main

import (
	"fmt"

	"github.com/sloppy-org/sloptools/internal/brain"
)

func brainAutoCommit(cfg *brain.Config, sphere brain.Sphere, message string) error {
	if cfg == nil {
		return fmt.Errorf("brain auto-commit: nil config")
	}
	vault, ok := cfg.Vault(sphere)
	if !ok {
		return fmt.Errorf("brain auto-commit: unknown vault %q", sphere)
	}
	return brain.CommitBrainGit(vault.BrainRoot(), message)
}
