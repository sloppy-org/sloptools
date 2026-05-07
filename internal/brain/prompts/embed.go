// Package prompts embeds the role-specific system prompts used by every
// brain-night stage. The Markdown files live alongside this Go file so
// they are part of the binary; callers extract them to a scratch dir
// before invoking the CLI.
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed *.md
var fsys embed.FS

// Read returns the embedded prompt content for a stage. stage is the
// filename without the .md extension (e.g. "folder-note", "scout").
func Read(stage string) ([]byte, error) {
	body, err := fsys.ReadFile(stage + ".md")
	if err != nil {
		return nil, fmt.Errorf("prompts: %s: %w", stage, err)
	}
	return body, nil
}

// Extract writes every embedded prompt as <dir>/<stage>.md and returns
// the directory path. Useful for tests and for the bench command which
// hands the directory to the bench runner.
func Extract(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("prompts: mkdir: %w", err)
	}
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		body, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, path), body, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("prompts: walk: %w", err)
	}
	return dir, nil
}
