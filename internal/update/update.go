package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL = "https://api.github.com"
	defaultRepoOwner  = "krystophny"
	defaultRepoName   = "sloppy"
)

type Options struct {
	CurrentVersion string
	ExecutablePath string
	GOOS           string
	GOARCH         string
	APIBaseURL     string
	RepoOwner      string
	RepoName       string
	HTTPClient     *http.Client
}

type Result struct {
	CurrentVersion string
	LatestVersion  string
	Updated        bool
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type runConfig struct {
	currentVersion string
	executablePath string
	targetOS       string
	targetArch     string
	apiBaseURL     string
	repoOwner      string
	repoName       string
	httpClient     *http.Client
}

func Run(opts Options) (Result, error) {
	cfg, err := resolveOptions(opts)
	if err != nil {
		return Result{}, err
	}
	release, err := fetchLatestRelease(cfg)
	if err != nil {
		return Result{}, err
	}
	latestVersion := normalizeVersion(release.TagName)
	res := Result{
		CurrentVersion: cfg.currentVersion,
		LatestVersion:  latestVersion,
	}
	cmp, err := compareSemanticVersions(latestVersion, cfg.currentVersion)
	if err != nil {
		return Result{}, err
	}
	if cmp <= 0 {
		return res, nil
	}
	artifact, err := selectReleaseAsset(release.Assets, latestVersion, cfg.targetOS, cfg.targetArch)
	if err != nil {
		return Result{}, err
	}
	checksumAsset, err := selectChecksumAsset(release.Assets)
	if err != nil {
		return Result{}, err
	}
	checksums, err := fetchChecksums(cfg, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return Result{}, err
	}
	expectedChecksum, ok := checksums[artifact.Name]
	if !ok {
		return Result{}, fmt.Errorf("checksum missing for asset %q", artifact.Name)
	}
	archiveData, err := downloadAsset(cfg, artifact.BrowserDownloadURL)
	if err != nil {
		return Result{}, err
	}
	actualChecksum := sha256Hex(archiveData)
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return Result{}, fmt.Errorf("checksum mismatch for %s: got %s want %s", artifact.Name, actualChecksum, expectedChecksum)
	}
	binaryData, err := extractBinaryFromArchive(archiveData, artifact.Name, cfg.targetOS)
	if err != nil {
		return Result{}, err
	}
	mode, err := currentExecutableMode(cfg.executablePath)
	if err != nil {
		return Result{}, err
	}
	if err := replaceExecutable(cfg.executablePath, binaryData, mode, cfg.targetOS); err != nil {
		return Result{}, err
	}
	res.Updated = true
	return res, nil
}

func resolveOptions(opts Options) (runConfig, error) {
	cfg := runConfig{
		currentVersion: normalizeVersion(opts.CurrentVersion),
		executablePath: strings.TrimSpace(opts.ExecutablePath),
		targetOS:       strings.TrimSpace(opts.GOOS),
		targetArch:     strings.TrimSpace(opts.GOARCH),
		apiBaseURL:     strings.TrimSpace(opts.APIBaseURL),
		repoOwner:      strings.TrimSpace(opts.RepoOwner),
		repoName:       strings.TrimSpace(opts.RepoName),
		httpClient:     opts.HTTPClient,
	}
	if cfg.executablePath == "" {
		exe, err := os.Executable()
		if err != nil {
			return runConfig{}, fmt.Errorf("resolve executable path: %w", err)
		}
		cfg.executablePath = exe
	}
	if cfg.targetOS == "" {
		cfg.targetOS = runtime.GOOS
	}
	if cfg.targetArch == "" {
		cfg.targetArch = runtime.GOARCH
	}
	if cfg.apiBaseURL == "" {
		cfg.apiBaseURL = defaultAPIBaseURL
	}
	if cfg.repoOwner == "" {
		cfg.repoOwner = defaultRepoOwner
	}
	if cfg.repoName == "" {
		cfg.repoName = defaultRepoName
	}
	if cfg.httpClient == nil {
		cfg.httpClient = &http.Client{Timeout: 45 * time.Second}
	}
	return cfg, nil
}

func normalizeVersion(raw string) string {
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(strings.TrimPrefix(clean, "v"), "V")
	if clean == "" {
		clean = "0.0.0"
	}
	return "v" + clean
}

type semver struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func compareSemanticVersions(a, b string) (int, error) {
	av, err := parseSemanticVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseSemanticVersion(b)
	if err != nil {
		return 0, err
	}
	if av.major != bv.major {
		if av.major > bv.major {
			return 1, nil
		}
		return -1, nil
	}
	if av.minor != bv.minor {
		if av.minor > bv.minor {
			return 1, nil
		}
		return -1, nil
	}
	if av.patch != bv.patch {
		if av.patch > bv.patch {
			return 1, nil
		}
		return -1, nil
	}
	return comparePrerelease(av.prerelease, bv.prerelease), nil
}

func parseSemanticVersion(raw string) (semver, error) {
	clean := normalizeVersion(raw)
	clean = strings.TrimPrefix(clean, "v")
	core := clean
	pre := ""
	if idx := strings.Index(core, "+"); idx >= 0 {
		core = core[:idx]
	}
	if idx := strings.Index(core, "-"); idx >= 0 {
		pre = core[idx+1:]
		core = core[:idx]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("invalid semantic version %q", raw)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semantic version %q", raw)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semantic version %q", raw)
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, fmt.Errorf("invalid semantic version %q", raw)
	}
	return semver{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: pre,
	}, nil
}

func comparePrerelease(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	ap := strings.Split(a, ".")
	bp := strings.Split(b, ".")
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		if i >= len(ap) {
			return -1
		}
		if i >= len(bp) {
			return 1
		}
		ai, aErr := strconv.Atoi(ap[i])
		bi, bErr := strconv.Atoi(bp[i])
		switch {
		case aErr == nil && bErr == nil:
			if ai > bi {
				return 1
			}
			if ai < bi {
				return -1
			}
		case aErr == nil && bErr != nil:
			return -1
		case aErr != nil && bErr == nil:
			return 1
		default:
			if ap[i] > bp[i] {
				return 1
			}
			if ap[i] < bp[i] {
				return -1
			}
		}
	}
	return 0
}

func fetchLatestRelease(cfg runConfig) (githubRelease, error) {
	url := fmt.Sprintf(
		"%s/repos/%s/%s/releases/latest",
		strings.TrimRight(cfg.apiBaseURL, "/"),
		cfg.repoOwner,
		cfg.repoName,
	)
	resp, err := doRequest(cfg.httpClient, url)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return githubRelease{}, fmt.Errorf("fetch latest release failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode latest release: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, fmt.Errorf("latest release tag_name is empty")
	}
	return release, nil
}

func selectReleaseAsset(assets []githubAsset, version, goos, goarch string) (githubAsset, error) {
	trimmedVersion := strings.TrimPrefix(normalizeVersion(version), "v")
	baseName := fmt.Sprintf("sloppy_%s_%s_%s", trimmedVersion, goos, goarch)
	extensions := []string{".tar.gz", ".tgz"}
	if goos == "windows" {
		extensions = []string{".zip"}
	}
	for _, ext := range extensions {
		expected := baseName + ext
		for _, asset := range assets {
			if asset.Name == expected {
				return asset, nil
			}
		}
	}
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if !strings.HasPrefix(name, "sloppy_") {
			continue
		}
		if !assetNameHasToken(name, goos) || !assetNameHasToken(name, goarch) {
			continue
		}
		if goos == "windows" && strings.HasSuffix(name, ".zip") {
			return asset, nil
		}
		if goos != "windows" && (strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz")) {
			return asset, nil
		}
	}
	return githubAsset{}, fmt.Errorf("no release asset found for %s/%s", goos, goarch)
}

func assetNameHasToken(name, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return false
	}
	tokens := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}

func selectChecksumAsset(assets []githubAsset) (githubAsset, error) {
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, "checksum") && strings.HasSuffix(name, ".txt") {
			return asset, nil
		}
	}
	return githubAsset{}, fmt.Errorf("no checksums text asset found in release")
}

func fetchChecksums(cfg runConfig, url string) (map[string]string, error) {
	body, err := downloadAsset(cfg, url)
	if err != nil {
		return nil, err
	}
	checksums := map[string]string{}
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := strings.ToLower(strings.TrimSpace(fields[0]))
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if len(hash) == 64 {
			checksums[name] = hash
		}
	}
	if len(checksums) == 0 {
		return nil, fmt.Errorf("checksum file is empty or invalid")
	}
	return checksums, nil
}

func downloadAsset(cfg runConfig, url string) ([]byte, error) {
	resp, err := doRequest(cfg.httpClient, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read download body: %w", err)
	}
	return data, nil
}

func doRequest(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "sloppy-self-update")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	return resp, nil
}
