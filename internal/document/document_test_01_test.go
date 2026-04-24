package document

import (
	"context"
	"errors"
	"os"
	"os/exec"
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
	if err := os.MkdirAll(filepath.Join(sourceDir, ".sloptools"), 0o755); err != nil {
		t.Fatalf("mkdir .sloptools: %v", err)
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
	if !strings.Contains(filepath.ToSlash(result.PDFPath), "/.sloptools/artifacts/documents/") {
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
	content := strings.Join([]string{"---", "bibliography: refs.bib", "csl: ieee.csl", "---", "", "# Notes", "", "[@demo]"}, "\n")
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

func TestExtractFiguresWritesArtifactsFromPDFImages(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "pdfimages"), `#!/bin/sh
set -eu
if [ "$1" = "-list" ]; then
cat <<'EOF'
page   num  type   width height color comp bpc  enc interp  object ID x-ppi y-ppi size ratio
--------------------------------------------------------------------------------------------
   1     0 image     640   480  rgb    3   8  image  no         7  0    72    72  12K 1.3%
   2     1 image     320   240  rgb    3   8  image  no         8  0    72    72   8K 1.1%
EOF
exit 0
fi
if [ "$1" = "-png" ]; then
prefix="$3"
printf 'PNG1' > "${prefix}-000.png"
printf 'PNG2' > "${prefix}-001.png"
exit 0
fi
echo "unexpected args: $*" >&2
exit 1
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	pdfPath := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("WriteFile(paper.pdf): %v", err)
	}
	outputDir := filepath.Join(t.TempDir(), ".sloptools", "artifacts", "figures", "paper")
	figures, err := ExtractFiguresWithOptions(pdfPath, FigureExtractOptions{OutputDir: outputDir})
	if err != nil {
		t.Fatalf("ExtractFiguresWithOptions() error = %v", err)
	}
	if len(figures) != 2 {
		t.Fatalf("len(figures) = %d, want 2", len(figures))
	}
	if figures[0].Page != 1 || figures[1].Page != 2 {
		t.Fatalf("pages = [%d %d], want [1 2]", figures[0].Page, figures[1].Page)
	}
	if figures[0].Index != 1 || figures[1].Index != 2 {
		t.Fatalf("indexes = [%d %d], want [1 2]", figures[0].Index, figures[1].Index)
	}
	if !strings.Contains(figures[0].Caption, "Figure 1 from paper.pdf") {
		t.Fatalf("caption = %q, want figure caption", figures[0].Caption)
	}
	for _, figure := range figures {
		if !strings.HasPrefix(filepath.ToSlash(figure.ImagePath), filepath.ToSlash(outputDir)+"/") {
			t.Fatalf("image path = %q, want output dir prefix", figure.ImagePath)
		}
		bytes, err := os.ReadFile(figure.ImagePath)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", figure.ImagePath, err)
		}
		if !strings.HasPrefix(string(bytes), "PNG") {
			t.Fatalf("image bytes = %q, want stub PNG content", string(bytes))
		}
	}
}

func TestExtractFiguresRequiresPDFImagesBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	pdfPath := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("WriteFile(paper.pdf): %v", err)
	}
	_, err := ExtractFigures(pdfPath)
	if !errors.Is(err, ErrPDFImagesBinaryMissing) {
		t.Fatalf("ExtractFigures() error = %v, want ErrPDFImagesBinaryMissing", err)
	}
}

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
