package document

import (
	"bufio"
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
	pdfImagesLookPath         = exec.LookPath
	pdfImagesCommandContext   = exec.CommandContext
)

type Figure struct {
	ImagePath string
	Page      int
	Caption   string
	Index     int
}

type FigureExtractOptions struct{ OutputDir string }

type pdfImageEntry struct{ Page int }

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
		figures = append(figures, Figure{ImagePath: path, Page: entries[i].Page, Caption: fmt.Sprintf("Figure %d from %s (page %d)", i+1, baseName, entries[i].Page), Index: i + 1})
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

const (
	DefaultPandocLinesPerPage = 45
	DefaultPDFPageHeight      = 792.0
	defaultPDFXOffset         = 72.0
)

type PandocSourceMap struct {
	sourcePath   string
	linesPerPage int
	pageHeight   float64
	pages        []pandocPage
}

type pandocPage struct {
	number    int
	startLine int
	endLine   int
}

func ParsePandocSourceMap(path string) (*PandocSourceMap, error) {
	return ParsePandocSourceMapWithOptions(path, DefaultPandocLinesPerPage)
}

func ParsePandocSourceMapWithOptions(path string, linesPerPage int) (*PandocSourceMap, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("parse pandoc source map: missing path")
	}
	if linesPerPage < 1 {
		return nil, fmt.Errorf("lines per page must be >= 1")
	}
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	pages, err := buildPandocPages(file, linesPerPage)
	if err != nil {
		return nil, err
	}
	return &PandocSourceMap{sourcePath: absPath, linesPerPage: linesPerPage, pageHeight: DefaultPDFPageHeight, pages: pages}, nil
}

func (m *PandocSourceMap) SourceLocation(page int, _ float64, y float64) (file string, line int, err error) {
	target, ok := m.page(page)
	if !ok {
		return "", 0, fmt.Errorf("page %d is out of range", page)
	}
	span := target.endLine - target.startLine + 1
	if span < 1 {
		return m.sourcePath, target.startLine, nil
	}
	fraction := clamp(y/m.pageHeight, 0, 0.999999)
	line = target.startLine + int(float64(span)*fraction)
	if line > target.endLine {
		line = target.endLine
	}
	return m.sourcePath, line, nil
}

func (m *PandocSourceMap) PDFLocation(file string, line int) (page int, x, y float64, err error) {
	if line < 1 {
		return 0, 0, 0, fmt.Errorf("line must be >= 1")
	}
	if !samePath(file, m.sourcePath) {
		return 0, 0, 0, fmt.Errorf("source map does not cover %q", file)
	}
	target, ok := m.pageForLine(line)
	if !ok {
		return 0, 0, 0, fmt.Errorf("line %d is out of range", line)
	}
	span := target.endLine - target.startLine + 1
	if span < 1 {
		return target.number, defaultPDFXOffset, 0, nil
	}
	offset := line - target.startLine
	y = (float64(offset) / float64(span)) * m.pageHeight
	return target.number, defaultPDFXOffset, y, nil
}

func (m *PandocSourceMap) page(number int) (pandocPage, bool) {
	for _, page := range m.pages {
		if page.number == number {
			return page, true
		}
	}
	return pandocPage{}, false
}

func (m *PandocSourceMap) pageForLine(line int) (pandocPage, bool) {
	for _, page := range m.pages {
		if line >= page.startLine && line <= page.endLine {
			return page, true
		}
	}
	return pandocPage{}, false
}

func buildPandocPages(file *os.File, linesPerPage int) ([]pandocPage, error) {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	var pages []pandocPage
	currentPage := 1
	pageStart := 1
	lineNumber := 0
	visibleLines := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if isPandocPageBreak(line) {
			pages = appendPandocPage(pages, currentPage, pageStart, max(pageStart, lineNumber-1))
			currentPage++
			pageStart = lineNumber + 1
			visibleLines = 0
			continue
		}
		visibleLines++
		if visibleLines >= linesPerPage {
			pages = appendPandocPage(pages, currentPage, pageStart, lineNumber)
			currentPage++
			pageStart = lineNumber + 1
			visibleLines = 0
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if lineNumber == 0 {
		return []pandocPage{{number: 1, startLine: 1, endLine: 1}}, nil
	}
	if pageStart <= lineNumber {
		pages = appendPandocPage(pages, currentPage, pageStart, lineNumber)
	}
	if len(pages) == 0 {
		pages = append(pages, pandocPage{number: 1, startLine: 1, endLine: lineNumber})
	}
	return pages, nil
}

func appendPandocPage(pages []pandocPage, number, startLine, endLine int) []pandocPage {
	if startLine > endLine {
		return pages
	}
	return append(pages, pandocPage{number: number, startLine: startLine, endLine: endLine})
}

func isPandocPageBreak(line string) bool {
	clean := strings.ToLower(strings.TrimSpace(line))
	switch clean {
	case "\\newpage", "\\pagebreak", "<div class=\"pagebreak\">", "<div style=\"page-break-after: always;\">", "::: pagebreak":
		return true
	default:
		return false
	}
}

func samePath(left, right string) bool {
	cleanLeft := filepath.Clean(strings.TrimSpace(left))
	cleanRight := filepath.Clean(strings.TrimSpace(right))
	if cleanLeft == cleanRight {
		return true
	}
	absLeft, errLeft := filepath.Abs(cleanLeft)
	absRight, errRight := filepath.Abs(cleanRight)
	return errLeft == nil && errRight == nil && absLeft == absRight
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

var (
	ErrSyncTeXFormat        = errors.New("invalid synctex file")
	ErrSyncTeXBinaryMissing = errors.New("synctex binary not found")
	ErrSyncTeXResultMissing = errors.New("synctex result not found")
)

type SyncTeX struct {
	path      string
	directory string
	outputPDF string
	inputs    map[string]string
}

func ParseSyncTeX(path string) (*SyncTeX, error) {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("parse synctex: missing path")
	}
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, closeReader, err := openSyncTeXReader(file, absPath)
	if err != nil {
		return nil, err
	}
	defer closeReader()
	inputs, err := parseSyncTeXInputs(reader)
	if err != nil {
		return nil, err
	}
	outputPDF, err := syncTeXOutputPath(absPath)
	if err != nil {
		return nil, err
	}
	return &SyncTeX{path: absPath, directory: filepath.Dir(absPath), outputPDF: outputPDF, inputs: inputs}, nil
}

func (s *SyncTeX) SourceLocation(page int, x, y float64) (file string, line int, err error) {
	if page < 1 {
		return "", 0, fmt.Errorf("page must be >= 1")
	}
	output, err := s.run("edit", "-o", fmt.Sprintf("%d:%s:%s:%s", page, formatFloat(x), formatFloat(y), s.outputPDF), "-d", s.directory)
	if err != nil {
		return "", 0, err
	}
	result, err := parseSyncTeXEditResult(output)
	if err != nil {
		return "", 0, err
	}
	return s.normalizeReturnedInput(result.input), result.line, nil
}

func (s *SyncTeX) PDFLocation(file string, line int) (page int, x, y float64, err error) {
	if line < 1 {
		return 0, 0, 0, fmt.Errorf("line must be >= 1")
	}
	input := s.normalizeRequestedInput(file)
	if input == "" {
		return 0, 0, 0, fmt.Errorf("missing input file")
	}
	output, err := s.run("view", "-i", fmt.Sprintf("%d:0:%s", line, input), "-o", s.outputPDF, "-d", s.directory)
	if err != nil {
		return 0, 0, 0, err
	}
	result, err := parseSyncTeXViewResult(output)
	if err != nil {
		return 0, 0, 0, err
	}
	return result.page, result.x, result.y, nil
}

func (s *SyncTeX) run(subcommand string, args ...string) (string, error) {
	if _, err := exec.LookPath("synctex"); err != nil {
		return "", ErrSyncTeXBinaryMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "synctex", append([]string{subcommand}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			return "", err
		}
		return "", fmt.Errorf("synctex %s failed: %s", subcommand, message)
	}
	return string(out), nil
}
