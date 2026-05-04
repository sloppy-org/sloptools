package kickoff

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/meetings"
	"github.com/sloppy-org/sloptools/internal/zulip"
)

type fakeZulipProvider struct {
	gotParams zulip.MessagesParams
	messages  []zulip.Message
	err       error
}

func (f *fakeZulipProvider) Messages(_ context.Context, params zulip.MessagesParams) ([]zulip.Message, error) {
	f.gotParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.messages, nil
}

func writeKickoffVaultConfig(t *testing.T, root string) (*brain.Config, string) {
	t.Helper()
	path := filepath.Join(root, "vaults.toml")
	body := "[[vault]]\nsphere = \"work\"\nroot = \"" + filepath.ToSlash(filepath.Join(root, "work")) + "\"\nbrain = \"brain\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write vaults.toml: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "work", "brain"), 0o755); err != nil {
		t.Fatalf("mkdir vault: %v", err)
	}
	cfg, err := brain.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg, path
}

func writeKickoffSourcesFile(t *testing.T, root, body string) string {
	t.Helper()
	path := filepath.Join(root, "sources.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write sources.toml: %v", err)
	}
	return path
}

func TestRunResolvesMeetingIDAndClustersBreakouts(t *testing.T) {
	tmp := t.TempDir()
	cfg, _ := writeKickoffVaultConfig(t, tmp)
	cutoff := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	sourcesBody := "[meetings.work]\nmeetings_root = \"" + filepath.ToSlash(filepath.Join(tmp, "work", "brain", "meetings")) + "\"\n\n[meetings.work.zulip]\nbase_url = \"https://zulip.example.org\"\nemail = \"bot@example.org\"\napi_key = \"secret\"\ntopic_format = \"{date} sync\"\n\n[meetings.work.meeting_series.plasma-orga]\nstream = \"plasma-orga\"\n"
	sourcesPath := writeKickoffSourcesFile(t, tmp, sourcesBody)

	priorRel := "brain/meetings/2026-04-27-plasma-orga.md"
	priorAbs := filepath.Join(tmp, "work", filepath.FromSlash(priorRel))
	if err := os.MkdirAll(filepath.Dir(priorAbs), 0o755); err != nil {
		t.Fatalf("mkdir prior: %v", err)
	}
	if err := os.WriteFile(priorAbs, []byte("# 2026-04-27 plasma orga\n\n## Decisions\n\n- Adopt the new netidee scope.\n- [x] Move the kickoff to Tuesday.\n\n## Action Checklist\n- [ ] Draft proposal.\n"), 0o644); err != nil {
		t.Fatalf("write prior: %v", err)
	}

	provider := &fakeZulipProvider{messages: []zulip.Message{
		{ID: 1, SenderName: "Ada Example", Topic: "2026-05-04 sync", Stream: "plasma-orga", Timestamp: cutoff.Add(-3 * time.Hour), Content: "Sync with @**Bo Coder** about grant numbers."},
		{ID: 2, SenderName: "Bo Coder", Topic: "2026-05-04 sync", Stream: "plasma-orga", Timestamp: cutoff.Add(-2 * time.Hour), Content: "Need to align with @**Ada Example** before review."},
		{ID: 3, SenderName: "Cy Reviewer", Topic: "2026-05-04 sync", Stream: "plasma-orga", Timestamp: cutoff.Add(-time.Hour), Content: "Want to talk to @**Dee Lurker** about plot scaling."},
	}}
	factory := func(zcfg meetings.ZulipConfig) (zulip.MessagesProvider, error) {
		if zcfg.BaseURL != "https://zulip.example.org" || zcfg.Email != "bot@example.org" || zcfg.APIKey != "secret" {
			t.Fatalf("zulip cfg = %#v", zcfg)
		}
		return provider, nil
	}

	out, err := Run(HandleArgs{
		"sphere":          "work",
		"meeting_id":      "plasma-orga",
		"cutoff":          cutoff.Format(time.RFC3339),
		"questions":       []interface{}{"What blocks the next code drop?"},
		"prior_note_path": priorRel,
	}, cfg, sourcesPath, true, factory)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := provider.gotParams.Stream; got != "plasma-orga" {
		t.Fatalf("zulip stream = %q", got)
	}
	if got := provider.gotParams.Topic; got != "2026-05-04 sync" {
		t.Fatalf("zulip topic = %q", got)
	}
	if !provider.gotParams.After.Equal(cutoff.Add(-24 * time.Hour)) {
		t.Fatalf("zulip after = %v", provider.gotParams.After)
	}
	if !provider.gotParams.Before.Equal(cutoff) {
		t.Fatalf("zulip before = %v", provider.gotParams.Before)
	}

	frame := out["frame"].(map[string]interface{})
	if got := frame["questions"].([]string); !reflect.DeepEqual(got, []string{"What blocks the next code drop?"}) {
		t.Fatalf("frame questions = %#v", got)
	}
	wantDecisions := []string{"Adopt the new netidee scope.", "Move the kickoff to Tuesday."}
	if got := frame["decisions"].([]string); !reflect.DeepEqual(got, wantDecisions) {
		t.Fatalf("frame decisions = %#v, want %#v", got, wantDecisions)
	}

	breakouts := out["breakouts"].([]map[string]interface{})
	if len(breakouts) != 1 {
		t.Fatalf("breakouts = %#v, want 1", breakouts)
	}
	wantParticipants := []string{"Ada Example", "Bo Coder"}
	if got := breakouts[0]["participants"].([]string); !reflect.DeepEqual(got, wantParticipants) {
		t.Fatalf("participants = %#v, want %#v", got, wantParticipants)
	}

	pairs := out["pair_off_cycle"].([]map[string]interface{})
	if len(pairs) != 1 {
		t.Fatalf("pair_off_cycle = %#v, want 1", pairs)
	}
	if pairs[0]["poster"] != "Cy Reviewer" {
		t.Fatalf("pair poster = %#v", pairs[0]["poster"])
	}

	if got := out["prior_note_path"]; got != priorRel {
		t.Fatalf("prior_note_path = %#v, want %q", got, priorRel)
	}
	if got := out["topic"]; got != "2026-05-04 sync" {
		t.Fatalf("topic echo = %#v", got)
	}
}

func TestRunEmptyTopicReturnsFrameOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg, _ := writeKickoffVaultConfig(t, tmp)
	factory := func(meetings.ZulipConfig) (zulip.MessagesProvider, error) {
		return &fakeZulipProvider{messages: nil}, nil
	}
	out, err := Run(HandleArgs{
		"sphere": "work",
		"stream": "plasma-orga",
		"topic":  "2026-05-04 sync",
		"cutoff": "2026-05-04T09:00:00Z",
	}, cfg, "", false, factory)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(out["breakouts"].([]map[string]interface{})); got != 0 {
		t.Fatalf("breakouts = %d, want 0", got)
	}
	if got := len(out["pair_off_cycle"].([]map[string]interface{})); got != 0 {
		t.Fatalf("pair_off_cycle = %d, want 0", got)
	}
}

func TestRunRejectsUnknownMeetingID(t *testing.T) {
	tmp := t.TempDir()
	cfg, _ := writeKickoffVaultConfig(t, tmp)
	_, err := Run(HandleArgs{"sphere": "work", "meeting_id": "no-such-series"}, cfg, "", false, nil)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v, want not-configured error", err)
	}
}

func TestRunFailsWhenFactoryNotConfigured(t *testing.T) {
	tmp := t.TempDir()
	cfg, _ := writeKickoffVaultConfig(t, tmp)
	_, err := Run(HandleArgs{
		"sphere": "work",
		"stream": "plasma-orga",
		"topic":  "2026-05-04 sync",
		"cutoff": "2026-05-04T09:00:00Z",
	}, cfg, "", false, nil)
	if err == nil || !strings.Contains(err.Error(), "factory is not configured") {
		t.Fatalf("err = %v, want factory-not-configured error", err)
	}
}
