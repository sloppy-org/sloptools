package meetings

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultShortMemoSeconds is the cutoff used to classify a voice memo as
// "short" (quick-commitment) versus "long" (full meeting workflow) when
// the per-sphere config does not override it.
const DefaultShortMemoSeconds = 60

// SphereConfig is the per-sphere section of the meetings config block.
// All paths are absolute after Load (relative paths are resolved against
// the user's home directory). OwnerAliases keys are stored lower-cased.
type SphereConfig struct {
	Sphere            string
	Inbox             string
	MeetingsRoot      string
	CanonicalHost     string
	ShortMemoSeconds  int
	OwnerAliases      map[string]string
	TranscribeCommand []string
	RenderCommand     []string
}

// Config is the parsed `[meetings.<sphere>]` map keyed by lower-case sphere.
type Config struct {
	bySphere map[string]SphereConfig
}

type configFile struct {
	Meetings map[string]rawSphereConfig `toml:"meetings"`
}

type rawSphereConfig struct {
	Inbox             string            `toml:"inbox"`
	MeetingsRoot      string            `toml:"meetings_root"`
	CanonicalHost     string            `toml:"canonical_host"`
	ShortMemoSeconds  int               `toml:"short_memo_seconds"`
	OwnerAliases      map[string]string `toml:"owner_aliases"`
	TranscribeCommand []string          `toml:"transcribe_command"`
	RenderCommand     []string          `toml:"render_command"`
}

// Load reads the meetings configuration from path. A missing file when
// path was not explicitly requested by the caller returns an empty Config
// without error so callers can opt into meetings without forcing a file
// to exist. Relative inbox/meetings_root values are resolved against the
// user's home directory; the resulting paths are filepath.Clean'd.
func Load(path string, explicit bool) (Config, error) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return Config{bySphere: map[string]SphereConfig{}}, nil
	}
	var raw configFile
	if _, err := toml.DecodeFile(clean, &raw); err != nil {
		if !explicit && os.IsNotExist(err) {
			return Config{bySphere: map[string]SphereConfig{}}, nil
		}
		return Config{}, fmt.Errorf("load meetings config %s: %w", clean, err)
	}
	out := Config{bySphere: map[string]SphereConfig{}}
	for sphere, entry := range raw.Meetings {
		key := strings.ToLower(strings.TrimSpace(sphere))
		if key == "" {
			continue
		}
		cfg, err := normalizeSphere(key, entry)
		if err != nil {
			return Config{}, err
		}
		out.bySphere[key] = cfg
	}
	return out, nil
}

// Sphere returns the parsed sphere config; ok is false when no entry
// exists for the sphere.
func (c Config) Sphere(sphere string) (SphereConfig, bool) {
	if c.bySphere == nil {
		return SphereConfig{}, false
	}
	cfg, ok := c.bySphere[strings.ToLower(strings.TrimSpace(sphere))]
	return cfg, ok
}

// Spheres returns the configured sphere keys sorted alphabetically.
func (c Config) Spheres() []string {
	keys := make([]string, 0, len(c.bySphere))
	for key := range c.bySphere {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ResolveAlias returns the canonical owner name for an alias, or the
// trimmed input when no alias is registered. Lookup is case-insensitive
// on the alias key; the returned canonical name preserves the casing in
// the configuration value.
func (c SphereConfig) ResolveAlias(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return ""
	}
	if c.OwnerAliases == nil {
		return clean
	}
	if mapped, ok := c.OwnerAliases[strings.ToLower(clean)]; ok && strings.TrimSpace(mapped) != "" {
		return strings.TrimSpace(mapped)
	}
	return clean
}

func normalizeSphere(sphere string, raw rawSphereConfig) (SphereConfig, error) {
	cfg := SphereConfig{
		Sphere:            sphere,
		Inbox:             cleanPath(raw.Inbox),
		MeetingsRoot:      cleanPath(raw.MeetingsRoot),
		CanonicalHost:     strings.TrimSpace(raw.CanonicalHost),
		ShortMemoSeconds:  raw.ShortMemoSeconds,
		TranscribeCommand: append([]string(nil), raw.TranscribeCommand...),
		RenderCommand:     append([]string(nil), raw.RenderCommand...),
	}
	if cfg.ShortMemoSeconds <= 0 {
		cfg.ShortMemoSeconds = DefaultShortMemoSeconds
	}
	if len(raw.OwnerAliases) > 0 {
		cfg.OwnerAliases = make(map[string]string, len(raw.OwnerAliases))
		for alias, canonical := range raw.OwnerAliases {
			key := strings.ToLower(strings.TrimSpace(alias))
			value := strings.TrimSpace(canonical)
			if key == "" || value == "" {
				continue
			}
			cfg.OwnerAliases[key] = value
		}
	}
	return cfg, nil
}

func cleanPath(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return ""
	}
	if strings.HasPrefix(clean, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			clean = filepath.Join(home, strings.TrimPrefix(clean, "~/"))
		}
	}
	return filepath.Clean(clean)
}
