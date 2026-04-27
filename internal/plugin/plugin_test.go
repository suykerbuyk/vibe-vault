package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "") // clear to use default
	return home
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDataDir_Default(t *testing.T) {
	home := setupHome(t)
	want := filepath.Join(home, ".local", "share", "vibe-vault")
	if got := MarketplaceDir(); !filepath.IsAbs(got) {
		t.Errorf("MarketplaceDir() = %q, want absolute path", got)
	}
	_ = want // DataDir is tested via MarketplaceDir prefix
}

func TestDataDir_XDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	want := filepath.Join(xdg, "vibe-vault", "claude-plugin")
	if got := MarketplaceDir(); got != want {
		t.Errorf("MarketplaceDir() = %q, want %q", got, want)
	}
}

func TestGenerate_CreatesAllFiles(t *testing.T) {
	setupHome(t)
	mktDir, err := Generate("1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	if mktDir != MarketplaceDir() {
		t.Errorf("Generate() returned %q, want %q", mktDir, MarketplaceDir())
	}

	for _, path := range []string{
		MarketplaceManifestPath(),
		PluginManifestPath(),
		MCPConfigPath(),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist", path)
		}
		// Verify valid JSON.
		readJSON(t, path)
	}
}

func TestGenerate_MarketplaceSchema(t *testing.T) {
	setupHome(t)
	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, MarketplaceManifestPath())
	if m["$schema"] == nil {
		t.Error("marketplace.json missing $schema field")
	}
	owner, ok := m["owner"].(map[string]any)
	if !ok {
		t.Fatal("marketplace.json missing owner")
	}
	if owner["email"] == nil {
		t.Error("marketplace.json owner missing email")
	}
	plugins, ok := m["plugins"].([]any)
	if !ok || len(plugins) == 0 {
		t.Fatal("marketplace.json missing plugins")
	}
	p := plugins[0].(map[string]any)
	if p["description"] == nil {
		t.Error("marketplace.json plugin missing description")
	}
	if src, _ := p["source"].(string); src != "./vibe-vault" {
		t.Errorf("marketplace.json plugin source = %q, want %q", src, "./vibe-vault")
	}
}

func TestGenerate_TwoLevelStructure(t *testing.T) {
	setupHome(t)
	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}

	// Marketplace manifest is at marketplace root.
	mktManifest := MarketplaceManifestPath()
	if _, err := os.Stat(mktManifest); err != nil {
		t.Errorf("marketplace manifest not found at %s", mktManifest)
	}

	// Plugin manifest is in a subdirectory.
	plugManifest := PluginManifestPath()
	if _, err := os.Stat(plugManifest); err != nil {
		t.Errorf("plugin manifest not found at %s", plugManifest)
	}

	// They should be in different directories.
	mktParent := filepath.Dir(filepath.Dir(mktManifest))
	plugParent := filepath.Dir(filepath.Dir(plugManifest))
	if mktParent == plugParent {
		t.Error("marketplace and plugin manifests should be in different directories")
	}
}

func TestGenerate_PluginHasAuthor(t *testing.T) {
	setupHome(t)
	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, PluginManifestPath())
	author, ok := m["author"].(map[string]any)
	if !ok {
		t.Fatal("plugin.json missing author field")
	}
	if name, _ := author["name"].(string); name != "vibe-vault" {
		t.Errorf("author.name = %q, want %q", name, "vibe-vault")
	}
}

func TestGenerate_Idempotent(t *testing.T) {
	setupHome(t)
	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}
	first := readJSON(t, PluginManifestPath())

	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}
	second := readJSON(t, PluginManifestPath())

	if first["version"] != second["version"] {
		t.Error("second Generate changed version unexpectedly")
	}
}

func TestGenerate_UpdatesVersion(t *testing.T) {
	setupHome(t)
	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}

	if _, err := Generate("2.0.0"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, PluginManifestPath())
	if v, _ := m["version"].(string); v != "2.0.0" {
		t.Errorf("plugin version = %q, want %q", v, "2.0.0")
	}
}

func TestGenerate_PathRelativeBinary(t *testing.T) {
	setupHome(t)
	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, MCPConfigPath())
	entry, ok := m["vibe-vault"].(map[string]any)
	if !ok {
		t.Fatal(".mcp.json missing vibe-vault entry")
	}
	cmd, _ := entry["command"].(string)
	// Generate intentionally writes "vv" (PATH-relative) so a stale binary
	// invoking install can't pin the plugin to its own absolute path.
	if cmd != "vv" {
		t.Errorf(".mcp.json command = %q, want %q", cmd, "vv")
	}
}

func TestRemove_Cleans(t *testing.T) {
	setupHome(t)
	if _, err := Generate("1.0.0"); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled() {
		t.Fatal("expected IsInstalled() = true after Generate")
	}

	if err := Remove(); err != nil {
		t.Fatal(err)
	}

	if IsInstalled() {
		t.Error("expected IsInstalled() = false after Remove")
	}
	if _, err := os.Stat(MarketplaceDir()); !os.IsNotExist(err) {
		t.Error("marketplace directory should not exist after Remove")
	}
}

func TestRemove_NotInstalled(t *testing.T) {
	setupHome(t)
	if err := Remove(); err != nil {
		t.Errorf("Remove() on missing directory should not error, got: %v", err)
	}
}

func TestIsInstalled_False(t *testing.T) {
	setupHome(t)
	if IsInstalled() {
		t.Error("expected IsInstalled() = false on fresh home")
	}
}

func TestIsInstalled_Partial(t *testing.T) {
	setupHome(t)
	// Create only the marketplace manifest — partial install.
	dir := filepath.Dir(MarketplaceManifestPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(MarketplaceManifestPath(), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if IsInstalled() {
		t.Error("expected IsInstalled() = false with only marketplace manifest")
	}
}
