package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func extractBinaryFromArchive(archiveData []byte, assetName, targetOS string) ([]byte, error) {
	if strings.HasSuffix(strings.ToLower(assetName), ".zip") {
		return extractFromZip(archiveData, binaryNameForOS(targetOS))
	}
	return extractFromTarGz(archiveData, binaryNameForOS(targetOS))
}

func binaryNameForOS(targetOS string) string {
	if targetOS == "windows" {
		return "sloptools.exe"
	}
	return "sloptools"
}

func extractFromTarGz(archiveData []byte, wantName string) ([]byte, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(archiveData))
	if err != nil {
		return nil, fmt.Errorf("open tar.gz archive: %w", err)
	}
	defer gzReader.Close()
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != wantName {
			continue
		}
		data, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, fmt.Errorf("read tar entry %s: %w", header.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("binary %q not found in tar.gz asset", wantName)
}

func extractFromZip(archiveData []byte, wantName string) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		return nil, fmt.Errorf("open zip archive: %w", err)
	}
	for _, file := range reader.File {
		if filepath.Base(file.Name) != wantName {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", file.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %s: %w", file.Name, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("binary %q not found in zip asset", wantName)
}

func currentExecutableMode(path string) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("inspect executable: %w", err)
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o755
	}
	return mode, nil
}

func replaceExecutable(path string, binaryData []byte, mode os.FileMode, targetOS string) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(path), "sloptools-update-*")
	if err != nil {
		return fmt.Errorf("create temp executable: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmpFile.Write(binaryData); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp executable: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp executable: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil && targetOS != "windows" {
		return fmt.Errorf("chmod temp executable: %w", err)
	}
	backupPath := path + ".old"
	_ = os.Remove(backupPath)
	if err := os.Rename(path, backupPath); err != nil {
		return fmt.Errorf("backup current executable: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Rename(backupPath, path)
		return fmt.Errorf("activate updated executable: %w", err)
	}
	_ = os.Remove(backupPath)
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
