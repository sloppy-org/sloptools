package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunReturnsUpToDateWithoutDownloadingAssets(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[]}`))
	}))
	defer srv.Close()

	exePath := filepath.Join(t.TempDir(), "sloptools")
	if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}

	res, err := Run(Options{
		CurrentVersion: "1.2.3",
		ExecutablePath: exePath,
		GOOS:           "linux",
		GOARCH:         "amd64",
		APIBaseURL:     srv.URL,
		RepoOwner:      "o",
		RepoName:       "r",
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Updated {
		t.Fatalf("expected no update, got updated=true")
	}
	if res.CurrentVersion != "v1.2.3" {
		t.Fatalf("current version = %q, want v1.2.3", res.CurrentVersion)
	}
	if res.LatestVersion != "v1.2.3" {
		t.Fatalf("latest version = %q, want v1.2.3", res.LatestVersion)
	}
}

func TestRunUpdatesExecutableFromTarGzWithChecksumVerification(t *testing.T) {
	t.Parallel()

	archiveName := "sloptools_1.2.4_linux_amd64.tar.gz"
	binaryPayload := []byte("new-binary-payload")
	archivePayload := mustTarGzBinary(t, "sloptools", binaryPayload)
	archiveChecksum := sha256Hex(archivePayload)
	checksumPayload := []byte(fmt.Sprintf("%s  %s\n", archiveChecksum, archiveName))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"tag_name":"v1.2.4","assets":[{"name":"%s","browser_download_url":"%s/asset/archive"},{"name":"checksums.txt","browser_download_url":"%s/asset/checksums"}]}`,
				archiveName,
				"http://"+r.Host,
				"http://"+r.Host,
			)))
		case "/asset/archive":
			_, _ = w.Write(archivePayload)
		case "/asset/checksums":
			_, _ = w.Write(checksumPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	exePath := filepath.Join(t.TempDir(), "sloptools")
	if err := os.WriteFile(exePath, []byte("old-binary-payload"), 0o755); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}

	res, err := Run(Options{
		CurrentVersion: "1.2.3",
		ExecutablePath: exePath,
		GOOS:           "linux",
		GOARCH:         "amd64",
		APIBaseURL:     srv.URL,
		RepoOwner:      "o",
		RepoName:       "r",
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected updated=true")
	}
	if res.CurrentVersion != "v1.2.3" || res.LatestVersion != "v1.2.4" {
		t.Fatalf("unexpected versions: current=%q latest=%q", res.CurrentVersion, res.LatestVersion)
	}
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read updated executable: %v", err)
	}
	if !bytes.Equal(got, binaryPayload) {
		t.Fatalf("updated executable payload mismatch")
	}
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("expected backup executable to be removed, err=%v", err)
	}
}

func TestRunUpdatesExecutableFromZipOnWindowsAndRemovesBackup(t *testing.T) {
	t.Parallel()

	archiveName := "sloptools_1.2.4_windows_amd64.zip"
	binaryPayload := []byte("new-windows-binary-payload")
	archivePayload := mustZipBinary(t, "sloptools.exe", binaryPayload)
	archiveChecksum := sha256Hex(archivePayload)
	checksumPayload := []byte(fmt.Sprintf("%s  %s\n", archiveChecksum, archiveName))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"tag_name":"v1.2.4","assets":[{"name":"%s","browser_download_url":"%s/asset/archive"},{"name":"checksums.txt","browser_download_url":"%s/asset/checksums"}]}`,
				archiveName,
				"http://"+r.Host,
				"http://"+r.Host,
			)))
		case "/asset/archive":
			_, _ = w.Write(archivePayload)
		case "/asset/checksums":
			_, _ = w.Write(checksumPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	exePath := filepath.Join(t.TempDir(), "sloptools.exe")
	if err := os.WriteFile(exePath, []byte("old-binary-payload"), 0o755); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}

	res, err := Run(Options{
		CurrentVersion: "1.2.3",
		ExecutablePath: exePath,
		GOOS:           "windows",
		GOARCH:         "amd64",
		APIBaseURL:     srv.URL,
		RepoOwner:      "o",
		RepoName:       "r",
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !res.Updated {
		t.Fatalf("expected updated=true")
	}
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatalf("read updated executable: %v", err)
	}
	if !bytes.Equal(got, binaryPayload) {
		t.Fatalf("updated executable payload mismatch")
	}
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Fatalf("expected backup executable to be removed, err=%v", err)
	}
}

func TestRunRejectsChecksumMismatchAndKeepsOriginalExecutable(t *testing.T) {
	t.Parallel()

	archiveName := "sloptools_1.2.4_linux_amd64.tar.gz"
	archivePayload := mustTarGzBinary(t, "sloptools", []byte("new-binary-payload"))
	checksumPayload := []byte(strings.Repeat("0", 64) + "  " + archiveName + "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"tag_name":"v1.2.4","assets":[{"name":"%s","browser_download_url":"%s/asset/archive"},{"name":"checksums.txt","browser_download_url":"%s/asset/checksums"}]}`,
				archiveName,
				"http://"+r.Host,
				"http://"+r.Host,
			)))
		case "/asset/archive":
			_, _ = w.Write(archivePayload)
		case "/asset/checksums":
			_, _ = w.Write(checksumPayload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	exePath := filepath.Join(t.TempDir(), "sloptools")
	originalPayload := []byte("old-binary-payload")
	if err := os.WriteFile(exePath, originalPayload, 0o755); err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}

	_, err := Run(Options{
		CurrentVersion: "1.2.3",
		ExecutablePath: exePath,
		GOOS:           "linux",
		GOARCH:         "amd64",
		APIBaseURL:     srv.URL,
		RepoOwner:      "o",
		RepoName:       "r",
		HTTPClient:     srv.Client(),
	})
	if err == nil {
		t.Fatalf("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %q, want checksum mismatch", err)
	}
	got, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatalf("read original executable: %v", readErr)
	}
	if !bytes.Equal(got, originalPayload) {
		t.Fatalf("original executable changed after failed update")
	}
}

func TestSelectReleaseAssetFallbackDoesNotPartialMatchArch(t *testing.T) {
	t.Parallel()

	_, err := selectReleaseAsset([]githubAsset{
		{Name: "sloptools_latest_linux_arm64.tar.gz"},
	}, "1.2.4", "linux", "arm")
	if err == nil {
		t.Fatalf("expected no matching asset for linux/arm when only arm64 exists")
	}
	if !strings.Contains(err.Error(), "no release asset found") {
		t.Fatalf("error = %q, want no release asset found", err)
	}
}

func TestSelectReleaseAssetFallbackMatchesTokenizedArch(t *testing.T) {
	t.Parallel()

	asset, err := selectReleaseAsset([]githubAsset{
		{Name: "sloptools_latest_linux_arm.tar.gz"},
	}, "1.2.4", "linux", "arm")
	if err != nil {
		t.Fatalf("selectReleaseAsset() error = %v", err)
	}
	if asset.Name != "sloptools_latest_linux_arm.tar.gz" {
		t.Fatalf("asset name = %q, want sloptools_latest_linux_arm.tar.gz", asset.Name)
	}
}

func mustTarGzBinary(t *testing.T, name string, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	header := &tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("tar write header: %v", err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatalf("tar write payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func mustZipBinary(t *testing.T, name string, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip create entry: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("zip write payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
