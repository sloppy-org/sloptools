package document

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *SyncTeX) normalizeRequestedInput(file string) string {
	clean := strings.TrimSpace(file)
	if clean == "" {
		return ""
	}
	if mapped, ok := s.inputs[clean]; ok {
		return mapped
	}
	abs, err := filepath.Abs(clean)
	if err == nil {
		if mapped, ok := s.inputs[abs]; ok {
			return mapped
		}
		if mapped, ok := s.inputs[filepath.Clean(abs)]; ok {
			return mapped
		}
	}
	if rel, err := filepath.Rel(s.directory, clean); err == nil {
		rel = filepath.ToSlash(rel)
		if mapped, ok := s.inputs[rel]; ok {
			return mapped
		}
	}
	return clean
}

func (s *SyncTeX) normalizeReturnedInput(file string) string {
	clean := filepath.Clean(strings.TrimSpace(file))
	if !filepath.IsAbs(clean) {
		clean = filepath.Clean(filepath.Join(s.directory, clean))
	}
	resolvedDir, errDir := filepath.EvalSymlinks(s.directory)
	resolvedClean, errClean := filepath.EvalSymlinks(clean)
	if errDir == nil && errClean == nil {
		if rel, err := filepath.Rel(resolvedDir, resolvedClean); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Clean(filepath.Join(s.directory, rel))
		}
	}
	return clean
}

func openSyncTeXReader(file *os.File, path string) (io.Reader, func() error, error) {
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(file)
		if err != nil {
			return nil, nil, err
		}
		return gz, gz.Close, nil
	}
	return file, func() error {
		return nil
	}, nil
}

func parseSyncTeXInputs(reader io.Reader) (map[string]string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	inputs := map[string]string{}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return nil, ErrSyncTeXFormat
	}
	if !strings.HasPrefix(scanner.Text(), "SyncTeX Version:") {
		return nil, ErrSyncTeXFormat
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "Content:" {
			break
		}
		raw, ok := strings.CutPrefix(line, "Input:")
		if !ok {
			continue
		}
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			return nil, ErrSyncTeXFormat
		}
		path := strings.TrimSpace(parts[1])
		if path == "" {
			continue
		}
		registerSyncTeXInput(inputs, path)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return inputs, nil
}

func registerSyncTeXInput(inputs map[string]string, path string) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" {
		return
	}
	values := []string{path, clean, filepath.ToSlash(clean), filepath.Base(clean)}
	if abs, err := filepath.Abs(clean); err == nil {
		values = append(values, abs, filepath.Clean(abs))
	}
	for _, key := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := inputs[key]; !exists {
			inputs[key] = path
		}
	}
}

func syncTeXOutputPath(path string) (string, error) {
	switch {
	case strings.HasSuffix(path, ".synctex.gz"):
		return strings.TrimSuffix(path, ".synctex.gz") + ".pdf", nil
	case strings.HasSuffix(path, ".synctex"):
		return strings.TrimSuffix(path, ".synctex") + ".pdf", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrSyncTeXFormat, path)
	}
}

type syncTeXViewResult struct {
	page int
	x    float64
	y    float64
}

func parseSyncTeXViewResult(raw string) (syncTeXViewResult, error) {
	values := map[string]string{}
	if err := collectSyncTeXResultValues(raw, values, "Page", "x", "y"); err != nil {
		return syncTeXViewResult{}, err
	}
	page, err := strconv.Atoi(values["Page"])
	if err != nil {
		return syncTeXViewResult{}, err
	}
	x, err := strconv.ParseFloat(values["x"], 64)
	if err != nil {
		return syncTeXViewResult{}, err
	}
	y, err := strconv.ParseFloat(values["y"], 64)
	if err != nil {
		return syncTeXViewResult{}, err
	}
	return syncTeXViewResult{page: page, x: x, y: y}, nil
}

type syncTeXEditResult struct {
	input string
	line  int
}

func parseSyncTeXEditResult(raw string) (syncTeXEditResult, error) {
	values := map[string]string{}
	if err := collectSyncTeXResultValues(raw, values, "Input", "Line"); err != nil {
		return syncTeXEditResult{}, err
	}
	line, err := strconv.Atoi(values["Line"])
	if err != nil {
		return syncTeXEditResult{}, err
	}
	return syncTeXEditResult{input: values["Input"], line: line}, nil
}

func collectSyncTeXResultValues(raw string, values map[string]string, keys ...string) error {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	inResult := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "SyncTeX result begin":
			inResult = true
			continue
		case "SyncTeX result end":
			if hasSyncTeXKeys(values, keys...) {
				return nil
			}
			inResult = false
			continue
		}
		if !inResult {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !syncTeXKeyRequested(key, keys...) {
			continue
		}
		values[key] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if hasSyncTeXKeys(values, keys...) {
		return nil
	}
	return ErrSyncTeXResultMissing
}

func hasSyncTeXKeys(values map[string]string, keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(values[key]) == "" {
			return false
		}
	}
	return true
}

func syncTeXKeyRequested(key string, keys ...string) bool {
	for _, candidate := range keys {
		if key == candidate {
			return true
		}
	}
	return false
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
