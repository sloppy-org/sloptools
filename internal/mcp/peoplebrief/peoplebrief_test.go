package peoplebrief

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

func parsePersonNote(t *testing.T, src string) *brain.MarkdownNote {
	t.Helper()
	note, diags := brain.ParseMarkdownNote(src, brain.MarkdownParseOptions{})
	if len(diags) != 0 {
		t.Fatalf("parse note: %#v", diags)
	}
	return note
}

func TestFrontmatterReturnsCanonicalFieldsAndDropsUnknowns(t *testing.T) {
	note := parsePersonNote(t, `---
kind: human
role: collaborator
supervision_role: postdoc co-advisor
focus: active
cadence: monthly
strategic: 4
enjoyment: 3
last_seen: 2026-04-15
affiliation: Example Lab
hobbies:
  - sailing
  - chess
---

# Ada Example
`)
	got := Frontmatter(note)
	for _, key := range FrontmatterFields {
		if _, ok := got[key]; !ok {
			t.Errorf("missing field %q in %#v", key, got)
		}
	}
	if _, leaked := got["hobbies"]; leaked {
		t.Errorf("non-canonical field leaked: %#v", got)
	}
	if got["affiliation"] != "Example Lab" {
		t.Errorf("affiliation = %#v", got["affiliation"])
	}
}

func TestStatusBulletsHonorsLimitAndFallbackHeadings(t *testing.T) {
	note := parsePersonNote(t, `---
kind: human
---

# Ada Example

## Recent context

- 2026-04-22: Aligned on plasma outline.
- 2026-03-10: Funding wording locked.
- 2026-02-01: Scoping call.
- 2025-12-12: Older bullet that should be trimmed.
`)
	bullets := StatusBullets(note, "", 3)
	if len(bullets) != 3 {
		t.Fatalf("len = %d, want 3: %#v", len(bullets), bullets)
	}
	if bullets[0].Date != "2026-04-22" || !strings.Contains(bullets[0].Text, "plasma outline") {
		t.Fatalf("newest = %#v", bullets[0])
	}
	for i := 1; i < len(bullets); i++ {
		if bullets[i].Date >= bullets[i-1].Date {
			t.Fatalf("not newest-first: %#v", bullets)
		}
	}
	custom := parsePersonNote(t, `---
kind: human
---

# Ada Example

## Status

- 2026-05-04: Custom section bullet.
`)
	if got := StatusBullets(custom, "Status", 3); len(got) != 1 || got[0].Date != "2026-05-04" {
		t.Fatalf("custom section = %#v", got)
	}
	if got := StatusBullets(custom, "", 3); len(got) != 1 {
		t.Fatalf("fallback to Status section returned %#v", got)
	}
}

func TestClassifyOpenLoopsBucketsByRelationshipAndDropsClosed(t *testing.T) {
	commitments := []Commitment{
		{Path: "brain/gtd/delegated.md", Title: "Delegated", Status: "delegated", DelegatedTo: "Ada Example", People: []string{"Ada Example"}},
		{Path: "brain/gtd/waiting.md", Title: "Waiting", Status: "waiting", WaitingFor: "Ada Example", People: []string{"Ada Example"}},
		{Path: "brain/gtd/owner.md", Title: "Ada owns this", Status: "next", Actor: "Ada Example", People: []string{"Ada Example"}},
		{Path: "brain/gtd/mentioned.md", Title: "Coordinate", Status: "next", People: []string{"Ada Example", "Charles Babbage"}},
		{Path: "brain/gtd/closed.md", Title: "Old", Status: "closed", DelegatedTo: "Ada Example", Closed: true},
		{Path: "brain/gtd/other.md", Title: "Other", Status: "next", People: []string{"Charles Babbage"}},
	}
	loops := ClassifyOpenLoops(commitments, "Ada Example")
	for bucket, want := range map[string]string{
		"delegated_to": "brain/gtd/delegated.md",
		"waiting":      "brain/gtd/waiting.md",
		"owner":        "brain/gtd/owner.md",
		"mentioned":    "brain/gtd/mentioned.md",
	} {
		got := loops[bucket]
		if len(got) != 1 || got[0].Path != want {
			t.Fatalf("loops[%s] = %#v, want %s", bucket, got, want)
		}
	}
	for _, group := range loops {
		for _, item := range group {
			if item.Path == "brain/gtd/closed.md" || item.Path == "brain/gtd/other.md" {
				t.Fatalf("unexpected leak: %#v", item)
			}
		}
	}
}

func TestLatestMeetingNotePicksNewestWikilinked(t *testing.T) {
	tmp := t.TempDir()
	brainRoot := filepath.Join(tmp, "brain")
	meetingsDir := filepath.Join(brainRoot, "meetings")
	if err := os.MkdirAll(meetingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := map[string]string{
		"2026-04-29-standup.md": "---\ntitle: Standup\ndate: 2026-04-29\n---\n\n- [[people/Ada Example]]\n",
		"2026-03-15-kickoff.md": "---\ntitle: Kickoff\ndate: 2026-03-15\n---\n\n- [[people/Ada Example]]\n",
		"2026-04-30-other.md":   "---\ntitle: Other\ndate: 2026-04-30\n---\n\n- [[people/Charles Babbage]]\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(meetingsDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	got, err := LatestMeetingNote(tmp, brainRoot, "Ada Example")
	if err != nil {
		t.Fatalf("LatestMeetingNote: %v", err)
	}
	if got == nil || got.Path != "brain/meetings/2026-04-29-standup.md" || got.Date != "2026-04-29" {
		t.Fatalf("got = %#v", got)
	}

	missing, err := LatestMeetingNote(tmp, brainRoot, "Nobody Else")
	if err != nil {
		t.Fatalf("missing person: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for unmatched person, got %#v", missing)
	}
}

func TestPersonEmailReadsFrontmatterThenBullet(t *testing.T) {
	canonical := parsePersonNote(t, "---\nemail: ada@example.com\n---\n# Ada\n")
	if got := PersonEmail(canonical, ""); got != "ada@example.com" {
		t.Fatalf("frontmatter email = %q", got)
	}
	bulletSrc := "---\nkind: human\n---\n# Ada\n\n- Email: ada@example.com\n"
	bulletNote := parsePersonNote(t, bulletSrc)
	if got := PersonEmail(bulletNote, bulletSrc); got != "ada@example.com" {
		t.Fatalf("bullet email = %q", got)
	}
	emptyNote := parsePersonNote(t, "---\nkind: human\n---\n# Ada\n")
	if got := PersonEmail(emptyNote, "# Ada\n"); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

type stubMailProvider struct {
	ids      []string
	messages map[string]*providerdata.EmailMessage
	err      error
	lastOpts email.SearchOptions
}

func (s *stubMailProvider) ListMessages(_ context.Context, opts email.SearchOptions) ([]string, error) {
	s.lastOpts = opts
	return append([]string(nil), s.ids...), s.err
}

func (s *stubMailProvider) GetMessages(_ context.Context, ids []string, _ string) ([]*providerdata.EmailMessage, error) {
	out := make([]*providerdata.EmailMessage, 0, len(ids))
	for _, id := range ids {
		if msg, ok := s.messages[id]; ok {
			clone := *msg
			out = append(out, &clone)
		}
	}
	return out, nil
}

func TestLatestPersonMailQueriesFromAndPicksNewest(t *testing.T) {
	older := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 4, 28, 14, 30, 0, 0, time.UTC)
	provider := &stubMailProvider{
		ids: []string{"old", "new"},
		messages: map[string]*providerdata.EmailMessage{
			"old": {ID: "old", ThreadID: "t1", Subject: "Older", Sender: "ada@example.com", Date: older, Folder: "INBOX"},
			"new": {ID: "new", ThreadID: "t2", Subject: "Newer", Sender: "ada@example.com", Date: newer, Folder: "INBOX"},
		},
	}
	got, err := LatestPersonMail(context.Background(), provider, 42, "ada@example.com")
	if err != nil {
		t.Fatalf("LatestPersonMail: %v", err)
	}
	if got == nil || got.MessageID != "new" || got.AccountID != 42 || got.Subject != "Newer" {
		t.Fatalf("got = %#v", got)
	}
	if provider.lastOpts.From != "ada@example.com" {
		t.Fatalf("From option = %q", provider.lastOpts.From)
	}

	if _, err := LatestPersonMail(context.Background(), provider, 42, ""); err == nil {
		t.Fatalf("expected error for empty email")
	}

	empty := &stubMailProvider{}
	if got, err := LatestPersonMail(context.Background(), empty, 42, "nobody@example.com"); err != nil || got != nil {
		t.Fatalf("empty provider: got=%#v err=%v", got, err)
	}
}

func TestCommitmentFromCommitmentMapsLocalOverlay(t *testing.T) {
	c := braingtd.Commitment{
		Title:        "Outcome",
		Status:       "next",
		Outcome:      "Outcome",
		Due:          "2026-05-09",
		FollowUp:     "2026-05-04",
		People:       []string{"Ada"},
		WaitingFor:   "",
		DelegatedTo:  "Ada",
		LocalOverlay: braingtd.LocalOverlay{Status: "delegated"},
	}
	got := CommitmentFromCommitment("brain/gtd/x.md", c, false)
	if got.Status != "delegated" || got.Title != "Outcome" || got.DelegatedTo != "Ada" {
		t.Fatalf("got = %#v", got)
	}
	if got.Closed {
		t.Fatalf("closed flag should remain false: %#v", got)
	}
}
