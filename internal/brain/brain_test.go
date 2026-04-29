package brain

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadConfigResolvesWorkAndPrivateRoots(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "vaults.toml")
	workRoot := filepath.Join(root, "work")
	privateRoot := filepath.Join(root, "private")
	writeFile(t, configPath, `[[vault]]
sphere = "work"
root = "`+filepath.ToSlash(workRoot)+`"
brain = "brain"
hub = true
exclude = ["archive"]

[[vault]]
sphere = "private"
root = "`+filepath.ToSlash(privateRoot)+`"
brain = "notes"
`)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	work, ok := cfg.Vault(SphereWork)
	if !ok {
		t.Fatal("work vault missing")
	}
	if work.Root != workRoot {
		t.Fatalf("work root = %q, want %q", work.Root, workRoot)
	}
	if work.BrainRoot() != filepath.Join(workRoot, "brain") {
		t.Fatalf("work brain root = %q", work.BrainRoot())
	}
	if !work.Hub {
		t.Fatal("work hub flag not loaded")
	}
	private, ok := cfg.Vault(SpherePrivate)
	if !ok {
		t.Fatal("private vault missing")
	}
	if private.BrainRoot() != filepath.Join(privateRoot, "notes") {
		t.Fatalf("private brain root = %q", private.BrainRoot())
	}
}

func TestResolveLinkAcceptsInVaultMarkdownLink(t *testing.T) {
	cfg := testConfig(t)
	resolver := cfg.Resolver()
	note := writeVaultFile(t, cfg, SphereWork, "brain/projects/project.md")
	target := writeVaultFile(t, cfg, SphereWork, "brain/people/alice.md")

	resolved, err := resolver.ResolveLink(SphereWork, note, "../people/alice.md#Context")
	if err != nil {
		t.Fatalf("ResolveLink() error: %v", err)
	}
	if resolved.Path != target {
		t.Fatalf("resolved path = %q, want %q", resolved.Path, target)
	}
	if resolved.Rel != filepath.Join("brain", "people", "alice.md") {
		t.Fatalf("resolved rel = %q", resolved.Rel)
	}
}

func TestResolveLinkRejectsOutOfVaultPath(t *testing.T) {
	cfg := testConfig(t)
	resolver := cfg.Resolver()
	note := writeVaultFile(t, cfg, SpherePrivate, "brain/projects/project.md")

	_, err := resolver.ResolveLink(SpherePrivate, note, "../../../outside.md")
	if KindOf(err) != ErrorOutOfVault {
		t.Fatalf("ResolveLink() kind = %q, err %v", KindOf(err), err)
	}
}

func TestResolveLinkRejectsWorkPersonalGuardrail(t *testing.T) {
	cfg := testConfig(t)
	resolver := cfg.Resolver()
	note := writeVaultFile(t, cfg, SphereWork, "brain/projects/project.md")
	writeVaultFile(t, cfg, SphereWork, "personal/secret.md")

	_, err := resolver.ResolveLink(SphereWork, note, "../../personal/secret.md")
	if KindOf(err) != ErrorExcludedPath {
		t.Fatalf("ResolveLink() kind = %q, err %v", KindOf(err), err)
	}
	for _, op := range []PathOp{OpRead, OpList, OpIndex, OpMetadata} {
		_, err := resolver.ResolvePath(SphereWork, filepath.Join(cfg.mustVault(t, SphereWork).Root, "personal", "secret.md"), op)
		if KindOf(err) != ErrorExcludedPath {
			t.Fatalf("ResolvePath(%s) kind = %q, err %v", op, KindOf(err), err)
		}
	}
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	outside := filepath.Join(t.TempDir(), "outside.md")
	writeFile(t, outside, "outside")
	link := filepath.Join(work.BrainRoot(), "outside.md")
	mkdirParent(t, link)
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := cfg.Resolver().ResolvePath(SphereWork, "outside.md", OpRead)
	if KindOf(err) != ErrorOutOfVault {
		t.Fatalf("ResolvePath() kind = %q, err %v", KindOf(err), err)
	}
}

func TestResolvePathRejectsSymlinkToWorkPersonal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	cfg := testConfig(t)
	work := cfg.mustVault(t, SphereWork)
	secret := writeVaultFile(t, cfg, SphereWork, "personal/secret.md")
	link := filepath.Join(work.BrainRoot(), "secret.md")
	mkdirParent(t, link)
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := cfg.Resolver().ResolvePath(SphereWork, "secret.md", OpRead)
	if KindOf(err) != ErrorExcludedPath {
		t.Fatalf("ResolvePath() kind = %q, err %v", KindOf(err), err)
	}
}

func testConfig(t *testing.T) *Config {
	t.Helper()
	root := t.TempDir()
	cfg, err := NewConfig([]Vault{
		{Sphere: SphereWork, Root: filepath.Join(root, "work"), Brain: "brain"},
		{Sphere: SpherePrivate, Root: filepath.Join(root, "private"), Brain: "brain"},
	})
	if err != nil {
		t.Fatalf("NewConfig() error: %v", err)
	}
	return cfg
}

func (c *Config) mustVault(t *testing.T, sphere Sphere) Vault {
	t.Helper()
	vault, ok := c.Vault(sphere)
	if !ok {
		t.Fatalf("missing vault %s", sphere)
	}
	return vault
}

func writeVaultFile(t *testing.T, cfg *Config, sphere Sphere, rel string) string {
	t.Helper()
	vault := cfg.mustVault(t, sphere)
	path := filepath.Join(vault.Root, filepath.FromSlash(rel))
	writeFile(t, path, "content")
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdirParent(t, path)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mkdirParent(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
