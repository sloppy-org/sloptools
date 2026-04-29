package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const defaultBrainDir = "brain"

type Sphere string

const (
	SphereWork    Sphere = "work"
	SpherePrivate Sphere = "private"
)

type Config struct {
	Vaults []Vault `toml:"vault"`
	byKey  map[Sphere]Vault
}

type Vault struct {
	Sphere  Sphere   `toml:"sphere"`
	Root    string   `toml:"root"`
	Brain   string   `toml:"brain"`
	Label   string   `toml:"label"`
	Hub     bool     `toml:"hub"`
	Exclude []string `toml:"exclude"`
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	return filepath.Join(home, ".config", "sloptools", "vaults.toml"), nil
}

func LoadConfig(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return nil, err
		}
		path = defaultPath
	} else {
		path = expandHomePath(path)
	}
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, &PathError{Kind: ErrorInvalidConfig, Path: path, Err: err}
	}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func expandHomePath(path string) string {
	clean := strings.TrimSpace(path)
	if clean != "~" && !strings.HasPrefix(clean, "~/") {
		return clean
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return clean
	}
	if clean == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(clean, "~/"))
}

func NewConfig(vaults []Vault) (*Config, error) {
	cfg := &Config{Vaults: append([]Vault(nil), vaults...)}
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Vault(sphere Sphere) (Vault, bool) {
	if c == nil || c.byKey == nil {
		return Vault{}, false
	}
	vault, ok := c.byKey[normalizeSphere(sphere)]
	return vault, ok
}

func (c *Config) Resolver() Resolver {
	return Resolver{config: c}
}

func (v Vault) BrainRoot() string {
	if filepath.IsAbs(v.Brain) {
		return filepath.Clean(v.Brain)
	}
	return filepath.Join(v.Root, v.Brain)
}

func (c *Config) normalize() error {
	c.byKey = map[Sphere]Vault{}
	for i := range c.Vaults {
		vault, err := normalizeVault(c.Vaults[i])
		if err != nil {
			return err
		}
		if _, exists := c.byKey[vault.Sphere]; exists {
			return &PathError{Kind: ErrorInvalidConfig, Sphere: vault.Sphere, Err: fmt.Errorf("duplicate vault")}
		}
		c.Vaults[i] = vault
		c.byKey[vault.Sphere] = vault
	}
	return nil
}

func normalizeVault(vault Vault) (Vault, error) {
	vault.Sphere = normalizeSphere(vault.Sphere)
	if vault.Sphere != SphereWork && vault.Sphere != SpherePrivate {
		return Vault{}, &PathError{Kind: ErrorInvalidConfig, Sphere: vault.Sphere, Err: fmt.Errorf("unsupported sphere")}
	}
	if strings.TrimSpace(vault.Root) == "" {
		return Vault{}, &PathError{Kind: ErrorInvalidConfig, Sphere: vault.Sphere, Err: fmt.Errorf("root is required")}
	}
	root, err := filepath.Abs(vault.Root)
	if err != nil {
		return Vault{}, &PathError{Kind: ErrorInvalidConfig, Sphere: vault.Sphere, Path: vault.Root, Err: err}
	}
	vault.Root = filepath.Clean(root)
	vault.Brain = strings.TrimSpace(vault.Brain)
	if vault.Brain == "" {
		vault.Brain = defaultBrainDir
	}
	if !isWithin(vault.Root, vault.BrainRoot()) {
		return Vault{}, &PathError{Kind: ErrorInvalidConfig, Sphere: vault.Sphere, Path: vault.BrainRoot(), Err: fmt.Errorf("brain root must be inside vault root")}
	}
	vault.Exclude = normalizeExcludes(vault.Sphere, vault.Exclude)
	return vault, nil
}

func normalizeSphere(sphere Sphere) Sphere {
	return Sphere(strings.ToLower(strings.TrimSpace(string(sphere))))
}

func normalizeExcludes(sphere Sphere, excludes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(excludes)+1)
	if sphere == SphereWork {
		out = append(out, "personal")
		seen["personal"] = true
	}
	for _, raw := range excludes {
		clean := filepath.Clean(strings.TrimSpace(raw))
		if clean == "" || clean == "." || filepath.IsAbs(clean) {
			continue
		}
		if !seen[clean] {
			out = append(out, clean)
			seen[clean] = true
		}
	}
	return out
}
