package document

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseSyncTeXRoundTripsSourceAndPDFLocation(t *testing.T) {
	ensureCommands(t, "pdflatex", "synctex")
	dir := t.TempDir()
	texPath := filepath.Join(dir, "main.tex")
	source := "\\documentclass{article}\n\\begin{document}\nHello world.\n\nSecond line.\n\\end{document}\n"
	if err := os.WriteFile(texPath, []byte(source), 0o644); err != nil {
		t.Fatalf("WriteFile(main.tex): %v", err)
	}
	cmd := exec.Command("pdflatex", "-synctex=1", "-interaction=nonstopmode", "main.tex")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pdflatex: %v\n%s", err, output)
	}

	syncPath := filepath.Join(dir, "main.synctex.gz")
	mapping, err := ParseSyncTeX(syncPath)
	if err != nil {
		t.Fatalf("ParseSyncTeX() error = %v", err)
	}

	page, x, y, err := mapping.PDFLocation(texPath, 3)
	if err != nil {
		t.Fatalf("PDFLocation() error = %v", err)
	}
	if page != 1 {
		t.Fatalf("page = %d, want 1", page)
	}
	if x <= 0 || y <= 0 {
		t.Fatalf("PDFLocation() = (%d, %f, %f), want positive coordinates", page, x, y)
	}

	file, line, err := mapping.SourceLocation(page, x, y)
	if err != nil {
		t.Fatalf("SourceLocation() error = %v", err)
	}
	if file != texPath {
		t.Fatalf("file = %q, want %q", file, texPath)
	}
	if line != 3 {
		t.Fatalf("line = %d, want 3", line)
	}
}

func TestParseSyncTeXRejectsInvalidHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.synctex")
	if err := os.WriteFile(path, []byte("not synctex\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	if _, err := ParseSyncTeX(path); err == nil {
		t.Fatal("ParseSyncTeX() error = nil, want error")
	}
}

func TestParsePandocSourceMapTracksExplicitPageBreaks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paper.md")
	content := "# Heading\n\nAlpha\nBeta\n\\newpage\nGamma\nDelta\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	mapping, err := ParsePandocSourceMapWithOptions(path, 10)
	if err != nil {
		t.Fatalf("ParsePandocSourceMapWithOptions() error = %v", err)
	}

	page, x, y, err := mapping.PDFLocation(path, 6)
	if err != nil {
		t.Fatalf("PDFLocation() error = %v", err)
	}
	if page != 2 {
		t.Fatalf("page = %d, want 2", page)
	}
	if x <= 0 || y < 0 {
		t.Fatalf("PDFLocation() = (%d, %f, %f), want positive x and non-negative y", page, x, y)
	}

	file, line, err := mapping.SourceLocation(2, x, 1)
	if err != nil {
		t.Fatalf("SourceLocation() error = %v", err)
	}
	if file != path {
		t.Fatalf("file = %q, want %q", file, path)
	}
	if line != 6 {
		t.Fatalf("line = %d, want 6", line)
	}
}

func TestParsePandocSourceMapUsesLineBucketsWithoutExplicitBreaks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	content := "one\ntwo\nthree\nfour\nfive\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	mapping, err := ParsePandocSourceMapWithOptions(path, 2)
	if err != nil {
		t.Fatalf("ParsePandocSourceMapWithOptions() error = %v", err)
	}

	page, _, _, err := mapping.PDFLocation(path, 5)
	if err != nil {
		t.Fatalf("PDFLocation() error = %v", err)
	}
	if page != 3 {
		t.Fatalf("page = %d, want 3", page)
	}

	_, line, err := mapping.SourceLocation(3, 0, 0)
	if err != nil {
		t.Fatalf("SourceLocation() error = %v", err)
	}
	if line != 5 {
		t.Fatalf("line = %d, want 5", line)
	}
}

func ensureCommands(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			t.Skipf("%s not available", name)
		}
	}
}
