package meetings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigParsesPerSphereTablesWithDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.toml")
	body := `[meetings.work]
inbox = "/tmp/inbox"
meetings_root = "/tmp/MEETINGS"
canonical_host = "mailuefterl"
short_memo_seconds = 90
[meetings.work.owner_aliases]
chris = "Christopher Albert"
ada   = "Ada Lovelace"

[meetings.private]
inbox = "/tmp/private-inbox"
meetings_root = "/tmp/PRIVATE"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	work, ok := cfg.Sphere("WORK")
	if !ok {
		t.Fatalf("expected work entry: %#v", cfg)
	}
	if work.Inbox != "/tmp/inbox" || work.MeetingsRoot != "/tmp/MEETINGS" {
		t.Fatalf("paths = %+v", work)
	}
	if work.CanonicalHost != "mailuefterl" || work.ShortMemoSeconds != 90 {
		t.Fatalf("scalar fields = %+v", work)
	}
	if work.OwnerAliases["chris"] != "Christopher Albert" || work.OwnerAliases["ada"] != "Ada Lovelace" {
		t.Fatalf("owner_aliases = %#v", work.OwnerAliases)
	}
	priv, ok := cfg.Sphere("private")
	if !ok || priv.ShortMemoSeconds != DefaultShortMemoSeconds {
		t.Fatalf("private fallback short_memo_seconds = %#v", priv)
	}
}

func TestLoadConfigMissingFileReturnsEmptyWhenNotExplicit(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.toml")
	cfg, err := Load(missing, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Spheres()) != 0 {
		t.Fatalf("expected empty config, got %v", cfg.Spheres())
	}
}

func TestSphereResolveAliasFallsBackToInputName(t *testing.T) {
	cfg := SphereConfig{OwnerAliases: map[string]string{"chris": "Christopher Albert"}}
	if got := cfg.ResolveAlias("Chris"); got != "Christopher Albert" {
		t.Fatalf("alias hit = %q, want Christopher Albert", got)
	}
	if got := cfg.ResolveAlias("Babbage"); got != "Babbage" {
		t.Fatalf("alias miss = %q, want Babbage", got)
	}
	if got := cfg.ResolveAlias(""); got != "" {
		t.Fatalf("empty input must return empty, got %q", got)
	}
}
