package brain

import (
	"testing"
	"time"
)

func TestIsProtectedPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"brain/commitments/test.md", true},
		{"brain/commitments", true},
		{"brain/gtd/anything.md", true},
		{"brain/glossary/term.md", true},
		{"brain/people/X.md", false},
		{"brain/projects/Y.md", false},
		{"plasma/x", false},
	}
	for _, tc := range cases {
		if got := IsProtectedPath(tc.path); got != tc.want {
			t.Errorf("IsProtectedPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestIsProtectedStatus(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"open", true},
		{"active", true},
		{"deferred", true},
		{"waiting", true},
		{"in_progress", true},
		{"in-progress", true},
		{"started", true},
		{"  Active  ", true},
		{"archived", false},
		{"stale", false},
		{"done", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsProtectedStatus(tc.status); got != tc.want {
			t.Errorf("IsProtectedStatus(%q) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestHasTODOMarkers(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{"plain prose with no markers", false},
		{"TODO: do this", true},
		{"some FIXME tag", true},
		{"XXX something", true},
		{"- [ ] checkbox", true},
		{"- [x] done checkbox", true},
		{"prose\n  - [ ] indented checkbox", true},
		{"deferred: 2026-09-01\nbody", true},
		{"todoism (no boundary)", false},
		{"FIXMEx (no boundary)", false},
	}
	for _, tc := range cases {
		if got := HasTODOMarkers(tc.body); got != tc.want {
			t.Errorf("HasTODOMarkers(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestLineRewriteTouchesNonLinkText(t *testing.T) {
	cases := []struct {
		old, new string
		want     bool
	}{
		{"see [[old]] for context", "see [[new]] for context", false},
		{"see [[old|alias]] etc", "see [[new|alias]] etc", false},
		{"TODO read [[old]]", "TODO read [[new]]", false},
		{"TODO read [[old]]", "DONE read [[new]]", true},
		{"prose changes outside link too [[old]]", "prose mutated outside [[new]]", true},
	}
	for _, tc := range cases {
		if got := LineRewriteTouchesNonLinkText(tc.old, tc.new); got != tc.want {
			t.Errorf("LineRewriteTouchesNonLinkText(%q -> %q) = %v, want %v", tc.old, tc.new, got, tc.want)
		}
	}
}

func TestPlanMoveRefusesProtectedFromPath(t *testing.T) {
	cfg := testConfig(t)
	now := time.Now()
	writeBrainNote(t, cfg, SphereWork, "brain/commitments/test.md", "---\nstatus: open\n---\n# T\n", now)
	if _, err := PlanMove(cfg, SphereWork, "brain/commitments/test.md", "brain/elsewhere/test.md"); err == nil {
		t.Fatalf("PlanMove should refuse protected from path")
	}
}
