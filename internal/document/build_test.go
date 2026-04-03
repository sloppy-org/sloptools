package document

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildWorkspaceDocumentDetectsLatexMainFileAndWritesArtifact(t *testing.T) {
	sourceDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "commands.log")
	writeExecutable(t, filepath.Join(binDir, "xelatex"), `#!/bin/sh
echo "xelatex $*" >> "$TEST_LOG"
for last; do true; done
base="${last%.tex}"
printf 'PDF via xelatex\n' > "${base}.pdf"
printf 'aux\n' > "${base}.aux"
`)
	writeExecutable(t, filepath.Join(binDir, "bibtex"), `#!/bin/sh
echo "bibtex $*" >> "$TEST_LOG"
`)
	t.Setenv("TEST_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := os.MkdirAll(filepath.Join(sourceDir, ".sloppy"), 0o755); err != nil {
		t.Fatalf("mkdir .sloppy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, buildConfigRelPath), []byte(`{"builder":"latex","main_file":"paper.tex","engine":"xelatex"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "paper.tex"), []byte("\\documentclass{article}\n\\begin{document}\nHello\n\\end{document}\n"), 0o644); err != nil {
		t.Fatalf("write paper.tex: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "refs.bib"), []byte("@book{demo,title={Demo}}\n"), 0o644); err != nil {
		t.Fatalf("write refs.bib: %v", err)
	}

	result, err := BuildWorkspaceDocument(context.Background(), sourceDir, "")
	if err != nil {
		t.Fatalf("BuildWorkspaceDocument() error = %v", err)
	}
	if result.Builder != "latex" {
		t.Fatalf("builder = %q, want latex", result.Builder)
	}
	if result.MainFile != "paper.tex" {
		t.Fatalf("main file = %q, want paper.tex", result.MainFile)
	}
	if !strings.Contains(filepath.ToSlash(result.PDFPath), "/.sloppy/artifacts/documents/") {
		t.Fatalf("pdf path = %q", result.PDFPath)
	}
	bytes, err := os.ReadFile(result.PDFPath)
	if err != nil {
		t.Fatalf("read artifact pdf: %v", err)
	}
	if string(bytes) != "PDF via xelatex\n" {
		t.Fatalf("artifact pdf = %q", string(bytes))
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logBytes)
	if strings.Count(logText, "xelatex ") != 3 {
		t.Fatalf("xelatex calls = %d, want 3\n%s", strings.Count(logText, "xelatex "), logText)
	}
	if strings.Count(logText, "bibtex ") != 1 {
		t.Fatalf("bibtex calls = %d, want 1\n%s", strings.Count(logText, "bibtex "), logText)
	}
}

func TestBuildWorkspaceDocumentAutoDetectsPreferredWorkspaceSource(t *testing.T) {
	sourceDir := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "pdflatex"), `#!/bin/sh
for last; do true; done
base="${last%.tex}"
printf 'PDF via pdflatex\n' > "${base}.pdf"
printf 'aux\n' > "${base}.aux"
`)
	writeExecutable(t, filepath.Join(binDir, "bibtex"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := os.WriteFile(filepath.Join(sourceDir, "main.tex"), []byte("\\documentclass{article}\n\\begin{document}\nHello\n\\end{document}\n"), 0o644); err != nil {
		t.Fatalf("write main.tex: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "README.md"), []byte("# Notes\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	result, err := BuildWorkspaceDocument(context.Background(), sourceDir, "")
	if err != nil {
		t.Fatalf("BuildWorkspaceDocument() error = %v", err)
	}
	if result.Builder != "latex" {
		t.Fatalf("builder = %q, want latex", result.Builder)
	}
	if result.MainFile != "main.tex" {
		t.Fatalf("main file = %q, want main.tex", result.MainFile)
	}
}

func TestBuildWorkspaceDocumentUsesPandocForMarkdownWithFrontMatter(t *testing.T) {
	sourceDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "pandoc.log")
	writeExecutable(t, filepath.Join(binDir, "pandoc"), `#!/bin/sh
echo "pandoc $*" >> "$TEST_LOG"
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
  fi
  prev="$arg"
done
printf 'PDF via pandoc\n' > "$out"
`)
	t.Setenv("TEST_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	content := strings.Join([]string{
		"---",
		"bibliography: refs.bib",
		"csl: ieee.csl",
		"---",
		"",
		"# Notes",
		"",
		"[@demo]",
	}, "\n")
	if err := os.WriteFile(filepath.Join(sourceDir, "notes.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}

	result, err := BuildWorkspaceDocument(context.Background(), sourceDir, "notes.md")
	if err != nil {
		t.Fatalf("BuildWorkspaceDocument() error = %v", err)
	}
	if result.Builder != "pandoc" {
		t.Fatalf("builder = %q, want pandoc", result.Builder)
	}
	if result.MainFile != "notes.md" {
		t.Fatalf("main file = %q, want notes.md", result.MainFile)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, "--citeproc") {
		t.Fatalf("pandoc log = %q, want --citeproc", logText)
	}
	bytes, err := os.ReadFile(result.PDFPath)
	if err != nil {
		t.Fatalf("read built artifact: %v", err)
	}
	if string(bytes) != "PDF via pandoc\n" {
		t.Fatalf("artifact pdf = %q", string(bytes))
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
