package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleFilesDecodesEncodedNestedPath(t *testing.T) {
	tmp := t.TempDir()
	docDir := filepath.Join(tmp, "docs")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	target := filepath.Join(docDir, "test.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.4\n%test\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	app := NewApp(tmp, "")
	req := httptest.NewRequest(http.MethodGet, "/files/docs%2Ftest.pdf", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestHandleFilesRejectsEncodedTraversal(t *testing.T) {
	app := NewApp(t.TempDir(), "")
	req := httptest.NewRequest(http.MethodGet, "/files/%2e%2e%2Fsecret.pdf", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestHandleFilesResolvesProjectAbsolutePathPrefix(t *testing.T) {
	tmp := t.TempDir()
	docDir := filepath.Join(tmp, ".sloptools", "artifacts", "pdf-smoke")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	target := filepath.Join(docDir, "pandoc-smoke.pdf")
	if err := os.WriteFile(target, []byte("%PDF-1.4\n%test\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	app := NewApp(tmp, "")
	absLike := strings.TrimPrefix(filepath.ToSlash(target), "/")
	req := httptest.NewRequest(http.MethodGet, "/files/"+absLike, nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}
