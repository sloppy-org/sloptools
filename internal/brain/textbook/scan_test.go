package textbook

import (
	"os"
	"path/filepath"
	"testing"
)

func writeNote(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCountsByVerdict(t *testing.T) {
	root := t.TempDir()
	// Pure textbook -> reject.
	writeNote(t, root, "topics/boltzmann-equation.md", "# Boltzmann equation\n\nThe Boltzmann equation is a textbook concept.\n")
	// Mixed -> compress.
	writeNote(t, root, "topics/banana-regime.md", "# Banana regime\n\nTextbook prose with [[projects/NEO-RT]] anchor.\n")
	// Canonical entity is auto-keep.
	writeNote(t, root, "people/winfried-kernbichler.md", "# Winfried Kernbichler\n\nAffiliation: TU Graz.\n")
	// Off deny-list -> keep.
	writeNote(t, root, "topics/local-jargon.md", "# Local jargon\n\nGroup-internal slang.\n")
	// Strategic override beats deny-list.
	writeNote(t, root, "topics/mhd.md", "---\nstrategic: true\n---\n\n# MHD\n\nGroup-strategic note.\n")

	c := New()
	s, err := c.Scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if s.Total != 5 {
		t.Fatalf("total=%d, want 5", s.Total)
	}
	if s.Reject != 1 {
		t.Fatalf("reject=%d, want 1", s.Reject)
	}
	if s.Compress != 1 {
		t.Fatalf("compress=%d, want 1", s.Compress)
	}
	if s.Keep != 3 {
		t.Fatalf("keep=%d, want 3", s.Keep)
	}
	if len(s.Rejects) != 1 || s.Rejects[0].Path != "topics/boltzmann-equation.md" {
		t.Fatalf("unexpected rejects: %#v", s.Rejects)
	}
	if len(s.Compress_) != 1 || s.Compress_[0].Path != "topics/banana-regime.md" {
		t.Fatalf("unexpected compress candidates: %#v", s.Compress_)
	}
}

func TestScanMissingDirsAreSkipped(t *testing.T) {
	root := t.TempDir()
	c := New()
	s, err := c.Scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if s.Total != 0 {
		t.Fatalf("expected total=0 on empty vault, got %d", s.Total)
	}
}

func TestSplitFrontMatter(t *testing.T) {
	cases := []struct {
		in       string
		wantFM   string
		wantBody string
	}{
		{"# only body\n", "", "# only body\n"},
		{"---\nstrategic: true\n---\n# body\n", "strategic: true", "# body\n"},
		{"---\nbroken", "", "---\nbroken"},
	}
	for _, tc := range cases {
		fm, body := splitFrontMatter(tc.in)
		if fm != tc.wantFM || body != tc.wantBody {
			t.Errorf("splitFrontMatter(%q) = %q, %q; want %q, %q", tc.in, fm, body, tc.wantFM, tc.wantBody)
		}
	}
}

func TestBuildNoteReadsStrategic(t *testing.T) {
	body := "---\nstrategic: true\nfocus: core\ncadence: daily\n---\n\n# Title\n"
	n := buildNote("topics/x.md", body)
	if !n.Strategic || n.Focus != "core" || n.Cadence != "daily" {
		t.Fatalf("frontmatter parse failed: %#v", n)
	}
	if n.Title != "Title" {
		t.Fatalf("title=%q", n.Title)
	}
	if n.Path != "topics/x" {
		t.Fatalf("path=%q", n.Path)
	}
}
