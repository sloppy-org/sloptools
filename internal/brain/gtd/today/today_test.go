package gtdtoday

import (
	"strings"
	"testing"
	"time"
)

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func TestSelectCapsAtHardLimit(t *testing.T) {
	items := make([]Item, 0, 12)
	for i := 0; i < 12; i++ {
		items = append(items, Item{ID: id("c", i), Title: "Item " + id("c", i), Queue: "next"})
	}
	got := Select(items, nil, false, 0)
	if len(got) != HardItemCap {
		t.Fatalf("len=%d, want %d", len(got), HardItemCap)
	}
}

func TestSelectDropsClosedItems(t *testing.T) {
	items := []Item{
		{ID: "a", Title: "Open", Queue: "next"},
		{ID: "b", Title: "Done", Queue: "done"},
		{ID: "c", Title: "Closed", Queue: "closed"},
	}
	got := Select(items, nil, false, 8)
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got=%#v, want only the open item", got)
	}
}

func TestSelectPinnedPathsAppearFirstInOrder(t *testing.T) {
	items := []Item{
		{ID: "a", Title: "Alpha", Path: "brain/gtd/a.md", Queue: "next"},
		{ID: "b", Title: "Beta", Path: "brain/gtd/b.md", Queue: "next"},
		{ID: "c", Title: "Gamma", Path: "brain/gtd/c.md", Queue: "next"},
	}
	got := Select(items, []string{"brain/gtd/c.md", "brain/gtd/a.md"}, false, 8)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if got[0].ID != "c" || !got[0].Pinned {
		t.Fatalf("first=%+v, want c pinned", got[0])
	}
	if got[1].ID != "a" || !got[1].Pinned {
		t.Fatalf("second=%+v, want a pinned", got[1])
	}
	if got[2].ID != "b" || got[2].Pinned {
		t.Fatalf("third=%+v, want b unpinned", got[2])
	}
}

func TestSelectFamilyFloorPullsCoreTrackBeforeFiller(t *testing.T) {
	items := []Item{
		{ID: "a", Title: "Filler 1", Path: "brain/gtd/a.md", Queue: "next"},
		{ID: "b", Title: "Filler 2", Path: "brain/gtd/b.md", Queue: "next"},
		{ID: "c", Title: "Family core", Path: "brain/gtd/family.md", Queue: "next", Track: "core"},
	}
	got := Select(items, nil, true, 8)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if got[0].ID != "c" {
		t.Fatalf("first=%+v, want family core item first", got[0])
	}
}

func TestSelectIgnoresUnmatchedPinnedPaths(t *testing.T) {
	items := []Item{{ID: "a", Title: "A", Path: "brain/gtd/a.md", Queue: "next"}}
	got := Select(items, []string{"brain/gtd/missing.md"}, false, 8)
	if len(got) != 1 || got[0].ID != "a" || got[0].Pinned {
		t.Fatalf("got=%#v, want unpinned a only", got)
	}
}

func TestRenderRoundTripsThroughParse(t *testing.T) {
	snap := Snapshot{
		Sphere:             "work",
		Date:               "2026-05-04",
		GeneratedAt:        "2026-05-04T07:00:00Z",
		IncludeFamilyFloor: true,
		PinnedPaths:        []string{"brain/gtd/a.md"},
		Items: []Item{
			{ID: "markdown:brain/gtd/a.md", Title: "Send report", Source: "markdown", Path: "brain/gtd/a.md", Queue: "next", Pinned: true},
			{ID: "markdown:brain/gtd/b.md", Title: "Review", Source: "markdown", Path: "brain/gtd/b.md", Queue: "next"},
		},
	}
	rendered, err := Render(snap)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(rendered, "# GTD Today: 2026-05-04") {
		t.Fatalf("missing heading:\n%s", rendered)
	}
	if !strings.Contains(rendered, "## Pinned (1)") {
		t.Fatalf("missing pinned section:\n%s", rendered)
	}
	parsed, err := Parse(rendered)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.Sphere != "work" || parsed.Date != "2026-05-04" {
		t.Fatalf("metadata mismatch: %+v", parsed)
	}
	if !parsed.IncludeFamilyFloor {
		t.Fatalf("family floor flag lost: %+v", parsed)
	}
	if len(parsed.Items) != 2 {
		t.Fatalf("items=%d, want 2: %+v", len(parsed.Items), parsed.Items)
	}
	if !parsed.Items[0].Pinned || parsed.Items[1].Pinned {
		t.Fatalf("pinned flags lost: %+v", parsed.Items)
	}
}

func TestParseRejectsUnfrozenSnapshot(t *testing.T) {
	src := "---\nkind: note\nsphere: work\ndate: \"2026-05-04\"\nfrozen: false\nitems: []\n---\n# x\n"
	if _, err := Parse(src); err == nil {
		t.Fatalf("expected error for unfrozen snapshot")
	}
}

func TestFormatDateAcceptsDateAndRFC3339(t *testing.T) {
	now := mustParseTime(t, "2026-05-04T12:00:00Z")
	cases := []struct {
		in   string
		want string
	}{
		{"", "2026-05-04"},
		{"2026-05-08", "2026-05-08"},
		{"2026-05-08T01:02:03Z", "2026-05-08"},
	}
	for _, tc := range cases {
		got, err := FormatDate(tc.in, now)
		if err != nil {
			t.Fatalf("FormatDate(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("FormatDate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFormatDateRejectsGarbage(t *testing.T) {
	if _, err := FormatDate("not-a-date", mustParseTime(t, "2026-05-04T00:00:00Z")); err == nil {
		t.Fatalf("expected error")
	}
}

func id(prefix string, i int) string {
	return prefix + "-" + string(rune('0'+i%10)) + string(rune('a'+i/10))
}
