package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	envLoadOnce sync.Once
	envLoadErr  error
)

func loadDefaultEnvFiles() error {
	envLoadOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			envLoadErr = fmt.Errorf("failed to resolve home directory: %w", err)
			return
		}
		for _, name := range []string{"env", "exchange-secrets.env"} {
			envPath := filepath.Join(home, ".config", "sloppy", name)
			if err := loadEnvFile(envPath); err != nil {
				envLoadErr = err
				return
			}
		}
	})
	return envLoadErr
}

func loadEnvFile(envPath string) error {
	file, err := os.Open(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to open %s: %w", envPath, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("failed to set env %s from %s: %w", key, envPath, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read env file: %w", err)
	}
	return nil
}
