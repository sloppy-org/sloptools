package document

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
	return &PandocSourceMap{
		sourcePath:   absPath,
		linesPerPage: linesPerPage,
		pageHeight:   DefaultPDFPageHeight,
		pages:        pages,
	}, nil
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
