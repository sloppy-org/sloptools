package edit

import (
	"testing"

	"github.com/sloppy-org/sloptools/internal/brain/evidence"
	"github.com/sloppy-org/sloptools/internal/brain/triage"
)

func TestBuildEntityPacketSize(t *testing.T) {
	item := triage.Item{Entity: "people/Alice.md", Reason: "test", Priority: 8}
	entries := []evidence.Entry{
		{Entity: "people/Alice.md", Claim: "Works at TU Graz", Verdict: evidence.VerdictVerified, Source: "https://tugraz.at", Confidence: 0.9},
		{Entity: "people/Alice.md", Claim: "Title changed to Univ.-Prof.", Verdict: evidence.VerdictConflicting, Confidence: 0.85, SuggestedEdit: "Update title field"},
	}
	digest := "## Activity digest 2026-05-11\n### Meetings\n- 09:00 Alice JF\n"

	packet := buildEntityPacket(t.TempDir(), item, entries, digest)
	if len(packet) > 4*1024 {
		t.Errorf("packet too large: %d bytes (want ≤ 4096)", len(packet))
	}
	if packet == "" {
		t.Error("expected non-empty packet")
	}
}

func TestEntityShortName(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"people/Alice Smith.md", "Alice Smith"},
		{"folders/proj/finanzen/2026.md", "2026"},
		{"people/Christian Jostmann.md", "Christian Jostmann"},
	}
	for _, tc := range cases {
		if got := entityShortName(tc.in); got != tc.out {
			t.Errorf("entityShortName(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestExtractMentions(t *testing.T) {
	digest := "## Activity digest\n### Meetings\n- 09:00 Alice JF\n- 10:00 Plasma Seminar\n### Mail\n- [Later] Application from Alice Smith\n"
	mentions := extractMentions(digest, "Alice Smith")
	if mentions == "" {
		t.Error("expected mentions for Alice Smith")
	}
	// Should NOT include Plasma Seminar line
	if contains(mentions, "Plasma") {
		t.Error("unexpected Plasma in Alice's mentions")
	}
}

func TestExtractMentionsNoMatch(t *testing.T) {
	digest := "## Activity digest\n- 09:00 Vito JF\n"
	mentions := extractMentions(digest, "Alice Smith")
	if mentions != "" {
		t.Errorf("expected no mentions, got: %q", mentions)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}
