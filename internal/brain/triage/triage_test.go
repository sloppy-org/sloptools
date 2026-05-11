package triage

import (
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/evidence"
)

func TestBuildPacketSizeLimit(t *testing.T) {
	// Build a packet with lots of data and verify it stays under 3KB.
	opts := Opts{
		RunID:          "run1",
		ActivityDigest: "## Activity digest 2026-05-11\n### Meetings\n- 09:00 Vito JF\n",
		Entries: func() []evidence.Entry {
			var e []evidence.Entry
			for i := 0; i < 50; i++ {
				e = append(e, evidence.Entry{
					Entity:  "people/Person" + string(rune('A'+i%26)) + ".md",
					Claim:   "Some claim about this person that is fairly long and descriptive",
					Verdict: evidence.VerdictVerified,
				})
			}
			return e
		}(),
		EntityPaths: func() []string {
			var p []string
			for i := 0; i < 100; i++ {
				p = append(p, "people/Entity"+string(rune('A'+i%26))+".md")
			}
			return p
		}(),
		Now: time.Now().UTC(),
	}
	packet := buildPacket(opts)
	if len(packet) > 3*1024 {
		t.Errorf("packet too large: %d bytes (want ≤ 3072)", len(packet))
	}
}

func TestParseResponse(t *testing.T) {
	raw := `[{"entity":"people/Alice.md","reason":"has conflicting evidence","priority":9,"evidence_ids":[]},{"entity":"folders/proj/X.md","reason":"stale","priority":5,"evidence_ids":[]}]`
	items, err := parseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Entity != "people/Alice.md" {
		t.Errorf("expected people/Alice.md, got %q", items[0].Entity)
	}
	if items[0].Priority != 9 {
		t.Errorf("expected priority 9, got %d", items[0].Priority)
	}
}

func TestParseResponseFenced(t *testing.T) {
	raw := "```json\n[{\"entity\":\"people/Bob.md\",\"reason\":\"test\",\"priority\":7,\"evidence_ids\":[]}]\n```"
	items, err := parseResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Entity != "people/Bob.md" {
		t.Fatalf("unexpected items: %v", items)
	}
}

func TestParseResponseInvalid(t *testing.T) {
	_, err := parseResponse("this is not JSON at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFallback(t *testing.T) {
	entries := []evidence.Entry{
		{Entity: "people/Alice.md", Verdict: evidence.VerdictConflicting},
		{Entity: "people/Alice.md", Verdict: evidence.VerdictVerified},
		{Entity: "people/Bob.md", Verdict: evidence.VerdictSkipped},
	}
	paths := []string{"people/Charlie.md", "people/Dave.md", "people/Eve.md", "people/Frank.md"}
	items := fallback(entries, paths)
	// Alice has 2 entries (one conflicting, one verified) → priority 2
	// Skipped entries (Bob) excluded
	// Fill from paths
	if len(items) == 0 {
		t.Fatal("expected non-empty fallback")
	}
	if items[0].Entity != "people/Alice.md" {
		t.Errorf("expected Alice first, got %q", items[0].Entity)
	}
	// Should have at most 5
	if len(items) > 5 {
		t.Errorf("expected ≤ 5 items, got %d", len(items))
	}
}
