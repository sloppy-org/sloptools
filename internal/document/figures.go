package document

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrPDFImagesBinaryMissing = errors.New("pdfimages binary not found")

	pdfImagesLookPath       = exec.LookPath
	pdfImagesCommandContext = exec.CommandContext
)

type Figure struct {
	ImagePath string
	Page      int
	Caption   string
	Index     int
}

type FigureExtractOptions struct {
	OutputDir string
}

type pdfImageEntry struct {
	Page int
}

func ExtractFigures(pdfPath string) ([]Figure, error) {
	return ExtractFiguresWithOptions(pdfPath, FigureExtractOptions{})
}

func ExtractFiguresWithOptions(pdfPath string, opts FigureExtractOptions) ([]Figure, error) {
	cleanPDFPath, err := cleanedFigurePDFPath(pdfPath)
	if err != nil {
		return nil, err
	}
	if _, err := pdfImagesLookPath("pdfimages"); err != nil {
		return nil, ErrPDFImagesBinaryMissing
	}
	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = defaultFigureOutputDir(cleanPDFPath)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}

	entries, err := listPDFImages(cleanPDFPath)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	prefix := filepath.Join(outputDir, sanitizeFigureStem(strings.TrimSuffix(filepath.Base(cleanPDFPath), filepath.Ext(cleanPDFPath))))
	if err := clearExtractedFigureOutputs(prefix); err != nil {
		return nil, err
	}
	if err := extractPDFImages(cleanPDFPath, prefix); err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(prefix + "-*")
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) != len(entries) {
		return nil, fmt.Errorf("pdfimages produced %d file(s) for %d embedded image(s)", len(paths), len(entries))
	}

	baseName := filepath.Base(cleanPDFPath)
	figures := make([]Figure, 0, len(paths))
	for i, path := range paths {
		figures = append(figures, Figure{
			ImagePath: path,
			Page:      entries[i].Page,
			Caption:   fmt.Sprintf("Figure %d from %s (page %d)", i+1, baseName, entries[i].Page),
			Index:     i + 1,
		})
	}
	return figures, nil
}

func cleanedFigurePDFPath(pdfPath string) (string, error) {
	clean := strings.TrimSpace(pdfPath)
	if clean == "" {
		return "", errors.New("pdf path is required")
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("pdf path must be a file: %s", abs)
	}
	return abs, nil
}

func defaultFigureOutputDir(pdfPath string) string {
	return filepath.Join(filepath.Dir(pdfPath), ".sloptools", "artifacts", "figures", sanitizeFigureStem(strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))))
}

func sanitizeFigureStem(raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	if clean == "" {
		return "figures"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range clean {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	clean = strings.Trim(b.String(), "-")
	if clean == "" {
		return "figures"
	}
	return clean
}

func clearExtractedFigureOutputs(prefix string) error {
	paths, err := filepath.Glob(prefix + "-*")
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func listPDFImages(pdfPath string) ([]pdfImageEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := pdfImagesCommandContext(ctx, "pdfimages", "-list", pdfPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return nil, err
		}
		return nil, fmt.Errorf("pdfimages -list failed: %s", message)
	}
	return parsePDFImagesList(string(output))
}

func extractPDFImages(pdfPath, prefix string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := pdfImagesCommandContext(ctx, "pdfimages", "-png", pdfPath, prefix)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("pdfimages extraction failed: %s", message)
	}
	return nil
}

func parsePDFImagesList(raw string) ([]pdfImageEntry, error) {
	lines := strings.Split(raw, "\n")
	entries := make([]pdfImageEntry, 0)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "page") || strings.HasPrefix(trimmed, "---") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 3 {
			continue
		}
		page, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		if _, err := strconv.Atoi(fields[1]); err != nil {
			continue
		}
		if fields[2] != "image" {
			continue
		}
		entries = append(entries, pdfImageEntry{Page: page})
	}
	return entries, nil
}
