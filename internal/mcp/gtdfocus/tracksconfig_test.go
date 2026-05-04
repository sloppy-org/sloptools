package gtdfocus

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/braincatalog"
)

func TestLoadTracksConfigParsesPerTrackWIPLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gtd.toml")
	body := `[[track]]
sphere = "work"
name = "research"
wip_limit = 5

[[track]]
sphere = "work"
name = "teaching"
wip_limit = 3

[[track]]
sphere = "private"
name = "personal"
wip_limit = 4
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write gtd.toml: %v", err)
	}
	cfg, err := LoadTracksConfig(path)
	if err != nil {
		t.Fatalf("LoadTracksConfig: %v", err)
	}
	research, ok := cfg.Lookup("work", "research")
	if !ok || research.WIPLimit != 5 {
		t.Fatalf("research lookup = %#v ok=%v, want wip_limit=5", research, ok)
	}
	teaching, ok := cfg.Lookup("WORK", "  Teaching ")
	if !ok || teaching.WIPLimit != 3 {
		t.Fatalf("teaching case-insensitive lookup = %#v ok=%v", teaching, ok)
	}
	personal, ok := cfg.Lookup("private", "personal")
	if !ok || personal.WIPLimit != 4 {
		t.Fatalf("personal lookup = %#v ok=%v", personal, ok)
	}
	if got := cfg.SphereTracks("work"); len(got) != 2 {
		t.Fatalf("work tracks = %d, want 2", len(got))
	}
}

func TestLoadTracksConfigMissingFileIsEmpty(t *testing.T) {
	cfg, err := LoadTracksConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("LoadTracksConfig missing: %v", err)
	}
	if _, ok := cfg.Lookup("work", "research"); ok {
		t.Fatalf("missing config should not surface lookups")
	}
	if got := cfg.SphereTracks("work"); len(got) != 0 {
		t.Fatalf("missing config should return no tracks, got %d", len(got))
	}
}

func TestLoadTracksConfigRejectsNegativeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gtd.toml")
	body := `[[track]]
sphere = "work"
name = "research"
wip_limit = -1
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadTracksConfig(path); err == nil {
		t.Fatalf("LoadTracksConfig accepted negative wip_limit")
	}
}

func TestWIPStatusClassification(t *testing.T) {
	cases := []struct {
		count, limit int
		want         string
	}{
		{0, 0, ""},
		{0, 5, WIPStatusUnder},
		{4, 5, WIPStatusUnder},
		{5, 5, WIPStatusAt},
		{6, 5, WIPStatusOver},
	}
	for _, tc := range cases {
		if got := WIPStatus(tc.count, tc.limit); got != tc.want {
			t.Fatalf("WIPStatus(%d,%d) = %q, want %q", tc.count, tc.limit, got, tc.want)
		}
	}
}

func TestDashboardWIPRowsCountsNextQueueOnly(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "gtd.toml")
	body := `[[track]]
sphere = "work"
name = "research"
wip_limit = 2
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadTracksConfig(path)
	if err != nil {
		t.Fatalf("LoadTracksConfig: %v", err)
	}
	items := []braincatalog.GTDListItem{
		{Title: "A", Status: "next", Track: "research"},
		{Title: "B", Status: "next", Track: "research"},
		{Title: "C", Status: "next", Track: "research"},
		{Title: "D", Status: "waiting", Track: "research"},
		{Title: "E", Status: "deferred", Track: "research", FollowUp: "2099-01-01"},
		{Title: "F", Status: "deferred", Track: "research", FollowUp: "2026-01-01"},
		{Title: "G", Status: "next", Track: "teaching"}, // teaching has no limit, ignored
	}
	rows := DashboardWIPRows(items, "work", cfg, now)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want exactly research", rows)
	}
	row := rows[0]
	if row.Track != "research" || row.Limit != 2 {
		t.Fatalf("row = %#v, want research/2", row)
	}
	// 3 explicit "next" + 1 deferred-with-elapsed-followup = 4
	if row.Count != 4 {
		t.Fatalf("count = %d, want 4 (3 next + 1 elapsed-deferred)", row.Count)
	}
	if row.Status != WIPStatusOver {
		t.Fatalf("status = %q, want over", row.Status)
	}
}
