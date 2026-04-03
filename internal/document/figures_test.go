package document

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
