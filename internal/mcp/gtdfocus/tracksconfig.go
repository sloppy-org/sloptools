package gtdfocus

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	WIPStatusUnder = "under"
	WIPStatusAt    = "at"
	WIPStatusOver  = "over"
)

// TrackConfig is the configurable per-track signal layer that drives WIP
// limit reporting in brain.gtd.tracks, brain.gtd.review_list, and
// brain.gtd.dashboard. WIPLimit is a soft signal, not a gate.
type TrackConfig struct {
	Sphere   string `toml:"sphere"`
	Name     string `toml:"name"`
	WIPLimit int    `toml:"wip_limit"`
}

type tracksConfigFile struct {
	Tracks []TrackConfig `toml:"track"`
}

// TracksConfig is a sphere-scoped, name-keyed view of the loaded track
// configuration. Lookup is case-insensitive on track name and ignores
// surrounding whitespace.
type TracksConfig struct {
	bySphere map[string]map[string]TrackConfig
}

// LoadTracksConfig parses gtd.toml at path. A missing file resolves to an
// empty configuration so callers can treat absent config as the default.
func LoadTracksConfig(path string) (*TracksConfig, error) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return &TracksConfig{}, nil
	}
	if _, err := os.Stat(clean); errors.Is(err, os.ErrNotExist) {
		return &TracksConfig{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat %s: %w", clean, err)
	}
	var file tracksConfigFile
	if _, err := toml.DecodeFile(clean, &file); err != nil {
		return nil, fmt.Errorf("decode %s: %w", filepath.Base(clean), err)
	}
	cfg := &TracksConfig{bySphere: map[string]map[string]TrackConfig{}}
	for _, track := range file.Tracks {
		sphere := strings.ToLower(strings.TrimSpace(track.Sphere))
		name := strings.ToLower(strings.TrimSpace(track.Name))
		if sphere == "" || name == "" {
			return nil, fmt.Errorf("track entry requires sphere and name (got sphere=%q name=%q)", track.Sphere, track.Name)
		}
		if track.WIPLimit < 0 {
			return nil, fmt.Errorf("track %s/%s wip_limit must be >= 0, got %d", sphere, name, track.WIPLimit)
		}
		bucket, ok := cfg.bySphere[sphere]
		if !ok {
			bucket = map[string]TrackConfig{}
			cfg.bySphere[sphere] = bucket
		}
		bucket[name] = TrackConfig{Sphere: sphere, Name: name, WIPLimit: track.WIPLimit}
	}
	return cfg, nil
}

// Lookup returns the configured track for sphere/name, plus whether one
// exists. Both inputs are normalized like LoadTracksConfig stores them.
func (c *TracksConfig) Lookup(sphere, name string) (TrackConfig, bool) {
	if c == nil || c.bySphere == nil {
		return TrackConfig{}, false
	}
	bucket, ok := c.bySphere[strings.ToLower(strings.TrimSpace(sphere))]
	if !ok {
		return TrackConfig{}, false
	}
	track, ok := bucket[strings.ToLower(strings.TrimSpace(name))]
	return track, ok
}

// SphereTracks returns the configured track names for sphere in lookup order
// stored after normalization. Useful for callers that need to enumerate
// configured tracks even when no items reference them yet.
func (c *TracksConfig) SphereTracks(sphere string) []TrackConfig {
	if c == nil || c.bySphere == nil {
		return nil
	}
	bucket, ok := c.bySphere[strings.ToLower(strings.TrimSpace(sphere))]
	if !ok {
		return nil
	}
	out := make([]TrackConfig, 0, len(bucket))
	for _, track := range bucket {
		out = append(out, track)
	}
	return out
}

// WIPStatus classifies count against limit using the under/at/over labels.
// A non-positive limit means no signal, returning an empty string.
func WIPStatus(count, limit int) string {
	if limit <= 0 {
		return ""
	}
	switch {
	case count > limit:
		return WIPStatusOver
	case count == limit:
		return WIPStatusAt
	default:
		return WIPStatusUnder
	}
}
