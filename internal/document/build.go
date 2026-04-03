package document

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const buildConfigRelPath = ".sloptools/document.json"

type Builder interface {
	Build(ctx context.Context, sourceDir string, mainFile string) (pdfPath string, err error)
	SupportsSource(path string) bool
}

type BuildConfig struct {
	Builder      string `json:"builder,omitempty"`
	MainFile     string `json:"main_file,omitempty"`
	Engine       string `json:"engine,omitempty"`
	Bibliography string `json:"bibliography,omitempty"`
	CSL          string `json:"csl,omitempty"`
	Template     string `json:"template,omitempty"`
}

type BuildResult struct {
	PDFPath  string
	MainFile string
	Builder  string
}

type latexBuilder struct {
	cfg BuildConfig
}

type pandocBuilder struct {
	cfg BuildConfig
}

func BuildWorkspaceDocument(ctx context.Context, sourceDir, requestedPath string) (BuildResult, error) {
	root, err := filepath.Abs(strings.TrimSpace(sourceDir))
	if err != nil {
		return BuildResult{}, err
	}
	if strings.TrimSpace(root) == "" {
		return BuildResult{}, errors.New("source directory is required")
	}
	cfg, err := LoadBuildConfig(root)
	if err != nil {
		return BuildResult{}, err
	}
	mainFile, builderName, builder, err := resolveBuilder(root, strings.TrimSpace(requestedPath), cfg)
	if err != nil {
		return BuildResult{}, err
	}
	builtPDF, err := builder.Build(ctx, root, mainFile)
	if err != nil {
		return BuildResult{}, err
	}
	artifactPath, err := documentArtifactOutputPath(root, mainFile)
	if err != nil {
		return BuildResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return BuildResult{}, err
	}
	if err := copyFile(builtPDF, artifactPath); err != nil {
		return BuildResult{}, err
	}
	return BuildResult{
		PDFPath:  artifactPath,
		MainFile: filepath.ToSlash(mainFile),
		Builder:  builderName,
	}, nil
}

func LoadBuildConfig(sourceDir string) (BuildConfig, error) {
	root := strings.TrimSpace(sourceDir)
	if root == "" {
		return BuildConfig{}, errors.New("source directory is required")
	}
	path := filepath.Join(root, buildConfigRelPath)
	bytes, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return BuildConfig{}, nil
	}
	if err != nil {
		return BuildConfig{}, err
	}
	var cfg BuildConfig
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return BuildConfig{}, fmt.Errorf("decode %s: %w", buildConfigRelPath, err)
	}
	cfg.Builder = normalizeBuilderName(cfg.Builder)
	cfg.MainFile = filepath.ToSlash(strings.TrimSpace(cfg.MainFile))
	cfg.Engine = normalizeLatexEngine(cfg.Engine)
	cfg.Bibliography = strings.TrimSpace(cfg.Bibliography)
	cfg.CSL = strings.TrimSpace(cfg.CSL)
	cfg.Template = strings.TrimSpace(cfg.Template)
	return cfg, nil
}

func resolveBuilder(sourceDir, requestedPath string, cfg BuildConfig) (string, string, Builder, error) {
	builders := map[string]Builder{
		"latex":  latexBuilder{cfg: cfg},
		"pandoc": pandocBuilder{cfg: cfg},
	}
	if requestedPath != "" {
		mainFile, err := normalizeMainFile(sourceDir, requestedPath)
		if err != nil {
			return "", "", nil, err
		}
		for name, builder := range builders {
			if builder.SupportsSource(mainFile) {
				return mainFile, name, builder, nil
			}
		}
		return "", "", nil, fmt.Errorf("unsupported document source: %s", filepath.Base(mainFile))
	}
	if cfg.MainFile != "" {
		mainFile, err := normalizeMainFile(sourceDir, cfg.MainFile)
		if err != nil {
			return "", "", nil, err
		}
		builderName, builder, err := builderForFile(mainFile, cfg, builders)
		if err != nil {
			return "", "", nil, err
		}
		return mainFile, builderName, builder, nil
	}
	candidates, err := discoverDocumentCandidates(sourceDir)
	if err != nil {
		return "", "", nil, err
	}
	if len(candidates) == 0 {
		return "", "", nil, errors.New("no supported document source found in workspace")
	}
	if cfg.Builder != "" {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if builder, ok := builders[cfg.Builder]; ok && builder.SupportsSource(candidate) {
				filtered = append(filtered, candidate)
			}
		}
		candidates = filtered
	}
	if len(candidates) == 0 {
		return "", "", nil, fmt.Errorf("no %s document source found in workspace", cfg.Builder)
	}
	mainFile := candidates[0]
	builderName, builder, err := builderForFile(mainFile, cfg, builders)
	if err != nil {
		return "", "", nil, err
	}
	return mainFile, builderName, builder, nil
}

func builderForFile(mainFile string, cfg BuildConfig, builders map[string]Builder) (string, Builder, error) {
	if cfg.Builder != "" {
		builder, ok := builders[cfg.Builder]
		if !ok {
			return "", nil, fmt.Errorf("unsupported builder %q", cfg.Builder)
		}
		if !builder.SupportsSource(mainFile) {
			return "", nil, fmt.Errorf("%s does not support %s", cfg.Builder, filepath.Base(mainFile))
		}
		return cfg.Builder, builder, nil
	}
	for name, builder := range builders {
		if builder.SupportsSource(mainFile) {
			return name, builder, nil
		}
	}
	return "", nil, fmt.Errorf("unsupported document source: %s", filepath.Base(mainFile))
}

func normalizeMainFile(sourceDir, requestedPath string) (string, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(sourceDir))
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(requestedPath)
	if raw == "" {
		return "", errors.New("document main file is required")
	}
	var abs string
	if filepath.IsAbs(raw) {
		abs = filepath.Clean(raw)
	} else {
		abs = filepath.Clean(filepath.Join(rootAbs, raw))
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("document source escapes workspace: %s", requestedPath)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("document source %q is a directory", requestedPath)
	}
	return filepath.ToSlash(rel), nil
}

func discoverDocumentCandidates(sourceDir string) ([]string, error) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return nil, err
	}
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.EqualFold(filepath.Ext(name), ".tex"):
			candidates = append(candidates, filepath.ToSlash(name))
		case isPandocSourcePath(name):
			candidates = append(candidates, filepath.ToSlash(name))
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		leftRank := documentCandidateRank(candidates[i])
		rightRank := documentCandidateRank(candidates[j])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return strings.ToLower(candidates[i]) < strings.ToLower(candidates[j])
	})
	return candidates, nil
}

func documentCandidateRank(path string) int {
	base := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	switch base {
	case "main.tex", "paper.tex", "thesis.tex", "article.tex", "main.md", "readme.md", "index.md":
		return 0
	default:
		switch strings.ToLower(filepath.Ext(base)) {
		case ".tex":
			return 1
		default:
			return 2
		}
	}
}

func normalizeBuilderName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return ""
	case "latex", "pdflatex", "xelatex", "lualatex":
		return "latex"
	case "pandoc", "markdown", "md":
		return "pandoc"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

func (b latexBuilder) SupportsSource(path string) bool {
	return strings.EqualFold(strings.TrimSpace(filepath.Ext(path)), ".tex")
}

func (b latexBuilder) Build(ctx context.Context, sourceDir string, mainFile string) (string, error) {
	engine := detectLatexEngine(filepath.Join(sourceDir, filepath.FromSlash(mainFile)), b.cfg.Engine)
	if engine == "" {
		engine = "pdflatex"
	}
	args := []string{"-synctex=1", "-interaction=nonstopmode", mainFile}
	if _, err := runCommand(ctx, sourceDir, engine, args...); err != nil {
		return "", err
	}
	if needsBibTeX(sourceDir, mainFile, b.cfg) {
		auxBase := strings.TrimSuffix(mainFile, filepath.Ext(mainFile))
		if _, err := runCommand(ctx, sourceDir, "bibtex", auxBase); err != nil {
			return "", err
		}
	}
	if _, err := runCommand(ctx, sourceDir, engine, args...); err != nil {
		return "", err
	}
	if _, err := runCommand(ctx, sourceDir, engine, args...); err != nil {
		return "", err
	}
	output := filepath.Join(sourceDir, strings.TrimSuffix(filepath.FromSlash(mainFile), filepath.Ext(mainFile))+".pdf")
	if _, err := os.Stat(output); err != nil {
		return "", err
	}
	return output, nil
}

func normalizeLatexEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "xelatex":
		return "xelatex"
	case "lualatex":
		return "lualatex"
	case "pdflatex":
		return "pdflatex"
	default:
		return ""
	}
}

func detectLatexEngine(sourcePath, configured string) string {
	if normalized := normalizeLatexEngine(configured); normalized != "" {
		return normalized
	}
	bytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return "pdflatex"
	}
	for _, line := range strings.Split(string(bytes), "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		switch {
		case strings.Contains(lower, "xelatex"):
			return "xelatex"
		case strings.Contains(lower, "lualatex"):
			return "lualatex"
		case strings.Contains(lower, "pdflatex"):
			return "pdflatex"
		}
	}
	return "pdflatex"
}

func needsBibTeX(sourceDir, mainFile string, cfg BuildConfig) bool {
	if strings.TrimSpace(cfg.Bibliography) != "" {
		return true
	}
	matches, err := filepath.Glob(filepath.Join(sourceDir, "*.bib"))
	if err != nil {
		return false
	}
	if len(matches) > 0 {
		return true
	}
	sourceBytes, err := os.ReadFile(filepath.Join(sourceDir, filepath.FromSlash(mainFile)))
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(sourceBytes))
	return strings.Contains(lower, "\\bibliography{") || strings.Contains(lower, "\\addbibresource{")
}

func (b pandocBuilder) SupportsSource(path string) bool {
	return isPandocSourcePath(path)
}

func isPandocSourcePath(path string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func (b pandocBuilder) Build(ctx context.Context, sourceDir string, mainFile string) (string, error) {
	output := filepath.Join(sourceDir, strings.TrimSuffix(filepath.FromSlash(mainFile), filepath.Ext(mainFile))+".pdf")
	args := []string{mainFile, "-o", output}
	if requiresPandocCiteproc(filepath.Join(sourceDir, filepath.FromSlash(mainFile)), b.cfg) {
		args = append(args, "--citeproc")
	}
	if strings.TrimSpace(b.cfg.Bibliography) != "" {
		args = append(args, "--bibliography="+strings.TrimSpace(b.cfg.Bibliography))
	}
	if strings.TrimSpace(b.cfg.CSL) != "" {
		args = append(args, "--csl="+strings.TrimSpace(b.cfg.CSL))
	}
	if strings.TrimSpace(b.cfg.Template) != "" {
		args = append(args, "--template="+strings.TrimSpace(b.cfg.Template))
	}
	if _, err := runCommand(ctx, sourceDir, "pandoc", args...); err != nil {
		return "", err
	}
	if _, err := os.Stat(output); err != nil {
		return "", err
	}
	return output, nil
}

func requiresPandocCiteproc(sourcePath string, cfg BuildConfig) bool {
	if strings.TrimSpace(cfg.Bibliography) != "" || strings.TrimSpace(cfg.CSL) != "" {
		return true
	}
	frontMatter, ok := readYAMLFrontMatter(sourcePath)
	if !ok {
		return false
	}
	for _, key := range []string{"bibliography:", "references:", "csl:", "nocite:"} {
		if strings.Contains(frontMatter, key) {
			return true
		}
	}
	return false
}

func readYAMLFrontMatter(path string) (string, bool) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := string(bytes)
	if !strings.HasPrefix(text, "---\n") {
		return "", false
	}
	rest := strings.TrimPrefix(text, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return "", false
	}
	return strings.ToLower(rest[:idx]), true
}

func runCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, fmt.Errorf("%s is not installed", name)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", name, message)
	}
	return output, nil
}

func documentArtifactOutputPath(sourceDir, mainFile string) (string, error) {
	rootAbs, err := filepath.Abs(strings.TrimSpace(sourceDir))
	if err != nil {
		return "", err
	}
	rel := filepath.ToSlash(strings.TrimSpace(mainFile))
	if rel == "" {
		return "", errors.New("document main file is required")
	}
	sum := sha256.Sum256([]byte(rel))
	name := sanitizeDocumentArtifactName(mainFile)
	return filepath.Join(rootAbs, ".sloptools", "artifacts", "documents", fmt.Sprintf("%s-%x.pdf", name, sum[:6])), nil
}

func sanitizeDocumentArtifactName(path string) string {
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if base == "" {
		return "document"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(base) {
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
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "document"
	}
	return name
}

func copyFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}
