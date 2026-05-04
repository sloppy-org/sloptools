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
	Owner             string
	MailAccountID     int64
	ShortMemoSeconds  int
	OwnerAliases      map[string]string
	PeopleEmails      map[string]string
	TranscribeCommand []string
	RenderCommand     []string
	Share             ShareConfig
	Nextcloud         NextcloudConfig
	Zulip             ZulipConfig
}

// ZulipConfig captures the per-sphere Zulip realm credentials plus a
// MeetingSeries map that resolves a meeting id (e.g. "plasma-orga")
// to the Zulip stream that hosts the §5 pre-meeting topic. The
// TopicFormat is rendered with the literal `{date}` placeholder.
type ZulipConfig struct {
	BaseURL       string
	Email         string
	APIKey        string
	TopicFormat   string
	MeetingSeries map[string]ZulipMeetingSeries
}

// ZulipMeetingSeries names the Zulip stream for a meeting id and
// optionally overrides the realm-level TopicFormat for that series.
type ZulipMeetingSeries struct {
	ID          string
	Stream      string
	TopicFormat string
}

// ShareConfig captures the per-sphere defaults that the summary
// drafter and share verbs need. URLTemplate (when set) is rendered
// with the literal placeholder `{vault_relative_path}` replaced by
// the meeting note path so users get a deterministic link without
// running helpy. Permissions default to "edit" per the issue spec.
type ShareConfig struct {
	URLTemplate         string
	NoteLinkFallback    string
	Permissions         string
	ExpiryDays          int
	Password            bool
	DeleteOnArchive     bool
	NextcloudShareRoot  string
	NextcloudShareFiles bool
}

// Config is the parsed `[meetings.<sphere>]` map keyed by lower-case sphere.
type Config struct {
	bySphere map[string]SphereConfig
}

type configFile struct {
	Meetings map[string]rawSphereConfig `toml:"meetings"`
}

type rawSphereConfig struct {
	Inbox             string                      `toml:"inbox"`
	MeetingsRoot      string                      `toml:"meetings_root"`
	CanonicalHost     string                      `toml:"canonical_host"`
	Owner             string                      `toml:"owner"`
	MailAccountID     int64                       `toml:"mail_account_id"`
	ShortMemoSeconds  int                         `toml:"short_memo_seconds"`
	OwnerAliases      map[string]string           `toml:"owner_aliases"`
	PeopleEmails      map[string]string           `toml:"people_emails"`
	TranscribeCommand []string                    `toml:"transcribe_command"`
	RenderCommand     []string                    `toml:"render_command"`
	Share             rawShareConfig              `toml:"share"`
	Nextcloud         rawNextcloudConfig          `toml:"nextcloud"`
	Zulip             rawZulipConfig              `toml:"zulip"`
	MeetingSeries     map[string]rawMeetingSeries `toml:"meeting_series"`
}

type rawZulipConfig struct {
	BaseURL     string `toml:"base_url"`
	Email       string `toml:"email"`
	APIKey      string `toml:"api_key"`
	TopicFormat string `toml:"topic_format"`
}

type rawMeetingSeries struct {
	Stream      string `toml:"stream"`
	TopicFormat string `toml:"topic_format"`
}

type rawNextcloudConfig struct {
	BaseURL      string `toml:"base_url"`
	User         string `toml:"user"`
	AppPassword  string `toml:"app_password"`
	LocalSyncDir string `toml:"local_sync_dir"`
}

type rawShareConfig struct {
	URLTemplate         string `toml:"url_template"`
	NoteLinkFallback    string `toml:"note_link_fallback"`
	Permissions         string `toml:"permissions"`
	ExpiryDays          int    `toml:"expiry_days"`
	Password            bool   `toml:"password"`
	DeleteOnArchive     bool   `toml:"delete_on_meeting_archive"`
	NextcloudShareRoot  string `toml:"nextcloud_share_root"`
	NextcloudShareFiles bool   `toml:"nextcloud_share_single_files"`
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
		Owner:             strings.TrimSpace(raw.Owner),
		MailAccountID:     raw.MailAccountID,
		ShortMemoSeconds:  raw.ShortMemoSeconds,
		TranscribeCommand: append([]string(nil), raw.TranscribeCommand...),
		RenderCommand:     append([]string(nil), raw.RenderCommand...),
		Share:             normalizeShare(raw.Share),
		Nextcloud:         normalizeNextcloud(raw.Nextcloud),
		Zulip:             normalizeZulip(raw.Zulip, raw.MeetingSeries),
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
	if len(raw.PeopleEmails) > 0 {
		cfg.PeopleEmails = make(map[string]string, len(raw.PeopleEmails))
		for name, email := range raw.PeopleEmails {
			key := strings.ToLower(strings.TrimSpace(name))
			value := strings.TrimSpace(email)
			if key == "" || value == "" {
				continue
			}
			cfg.PeopleEmails[key] = value
		}
	}
	return cfg, nil
}

func normalizeNextcloud(raw rawNextcloudConfig) NextcloudConfig {
	cfg := NextcloudConfig{
		BaseURL:      strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/"),
		User:         strings.TrimSpace(raw.User),
		AppPassword:  strings.TrimSpace(raw.AppPassword),
		LocalSyncDir: cleanPath(raw.LocalSyncDir),
	}
	return cfg
}

func normalizeZulip(raw rawZulipConfig, series map[string]rawMeetingSeries) ZulipConfig {
	cfg := ZulipConfig{
		BaseURL:     strings.TrimRight(strings.TrimSpace(raw.BaseURL), "/"),
		Email:       strings.TrimSpace(raw.Email),
		APIKey:      strings.TrimSpace(raw.APIKey),
		TopicFormat: strings.TrimSpace(raw.TopicFormat),
	}
	if len(series) == 0 {
		return cfg
	}
	cfg.MeetingSeries = make(map[string]ZulipMeetingSeries, len(series))
	for id, entry := range series {
		key := strings.ToLower(strings.TrimSpace(id))
		stream := strings.TrimSpace(entry.Stream)
		if key == "" || stream == "" {
			continue
		}
		cfg.MeetingSeries[key] = ZulipMeetingSeries{
			ID:          key,
			Stream:      stream,
			TopicFormat: strings.TrimSpace(entry.TopicFormat),
		}
	}
	return cfg
}

// SeriesStream returns the Zulip stream and effective topic format for
// the given meeting id; ok is false when the id is not configured.
func (c ZulipConfig) SeriesStream(id string) (ZulipMeetingSeries, bool) {
	if c.MeetingSeries == nil {
		return ZulipMeetingSeries{}, false
	}
	series, ok := c.MeetingSeries[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return ZulipMeetingSeries{}, false
	}
	if strings.TrimSpace(series.TopicFormat) == "" {
		series.TopicFormat = c.TopicFormat
	}
	return series, true
}

func normalizeShare(raw rawShareConfig) ShareConfig {
	cfg := ShareConfig{
		URLTemplate:         strings.TrimSpace(raw.URLTemplate),
		NoteLinkFallback:    strings.TrimSpace(raw.NoteLinkFallback),
		Permissions:         strings.ToLower(strings.TrimSpace(raw.Permissions)),
		ExpiryDays:          raw.ExpiryDays,
		Password:            raw.Password,
		DeleteOnArchive:     raw.DeleteOnArchive,
		NextcloudShareRoot:  strings.TrimSpace(raw.NextcloudShareRoot),
		NextcloudShareFiles: raw.NextcloudShareFiles,
	}
	if cfg.Permissions == "" {
		cfg.Permissions = "edit"
	}
	return cfg
}

// PeopleEmail returns the configured override email for name, normalised
// case-insensitively, or "" when no override is registered.
func (c SphereConfig) PeopleEmail(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" || c.PeopleEmails == nil {
		return ""
	}
	if email, ok := c.PeopleEmails[strings.ToLower(clean)]; ok {
		return strings.TrimSpace(email)
	}
	return ""
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
