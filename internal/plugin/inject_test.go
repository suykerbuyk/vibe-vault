package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallToCache_CreatesFiles(t *testing.T) {
	setupHome(t)
	installPath, err := InstallToCache("1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		filepath.Join(".claude-plugin", "plugin.json"),
		".mcp.json",
	} {
		p := filepath.Join(installPath, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist", p)
		}
	}
}

func TestInstallToCache_CorrectPath(t *testing.T) {
	home := setupHome(t)
	installPath, err := InstallToCache("1.2.3")
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(home, ".claude", "plugins", "cache", "vibe-vault-local", "vibe-vault", "1.2.3")
	if installPath != want {
		t.Errorf("installPath = %q, want %q", installPath, want)
	}
}

func TestInstallToCache_PathRelativeBinary(t *testing.T) {
	setupHome(t)
	installPath, err := InstallToCache("1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, filepath.Join(installPath, ".mcp.json"))
	entry, ok := m["vibe-vault"].(map[string]any)
	if !ok {
		t.Fatal(".mcp.json missing vibe-vault entry")
	}
	cmd, _ := entry["command"].(string)
	// InstallToCache writes "vv" (PATH-relative) — see Generate's doc comment.
	if cmd != "vv" {
		t.Errorf(".mcp.json command = %q, want %q", cmd, "vv")
	}

	// env block must propagate ANTHROPIC_API_KEY so vv_wrap_dispatch and
	// other LLM-backed handlers see the operator's live shell key.
	env, ok := entry["env"].(map[string]any)
	if !ok {
		t.Fatal(".mcp.json missing env block")
	}
	if got := env["ANTHROPIC_API_KEY"]; got != "${ANTHROPIC_API_KEY}" {
		t.Errorf("env.ANTHROPIC_API_KEY = %q, want %q", got, "${ANTHROPIC_API_KEY}")
	}
}

func TestRemoveFromCache_Cleans(t *testing.T) {
	setupHome(t)
	if _, err := InstallToCache("1.0.0"); err != nil {
		t.Fatal(err)
	}

	if err := RemoveFromCache(); err != nil {
		t.Fatal(err)
	}

	cacheDir := filepath.Join(ClaudePluginsDir(), "cache", MarketplaceName)
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Error("cache directory should not exist after RemoveFromCache")
	}
}

func TestRemoveFromCache_NotPresent(t *testing.T) {
	setupHome(t)
	if err := RemoveFromCache(); err != nil {
		t.Errorf("RemoveFromCache on missing directory should not error, got: %v", err)
	}
}

func TestRegisterKnownMarketplace_Fresh(t *testing.T) {
	setupHome(t)
	mktDir := "/tmp/test-marketplace"

	if err := RegisterKnownMarketplace(mktDir); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, KnownMarketplacesPath())
	entry, ok := m[MarketplaceName].(map[string]any)
	if !ok {
		t.Fatal("missing marketplace entry")
	}
	src, ok := entry["source"].(map[string]any)
	if !ok {
		t.Fatal("missing source in entry")
	}
	if src["source"] != "directory" {
		t.Errorf("source.source = %q, want %q", src["source"], "directory")
	}
	if src["path"] != mktDir {
		t.Errorf("source.path = %q, want %q", src["path"], mktDir)
	}
	if entry["installLocation"] != mktDir {
		t.Errorf("installLocation = %q, want %q", entry["installLocation"], mktDir)
	}

	// Verify file permissions.
	info, err := os.Stat(KnownMarketplacesPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestRegisterKnownMarketplace_Existing(t *testing.T) {
	setupHome(t)

	// Write an existing entry.
	existing := map[string]any{
		"other-marketplace": map[string]any{
			"source": map[string]any{"source": "git", "url": "https://example.com"},
		},
	}
	if err := os.MkdirAll(filepath.Dir(KnownMarketplacesPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(KnownMarketplacesPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RegisterKnownMarketplace("/tmp/test"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, KnownMarketplacesPath())
	if _, ok := m["other-marketplace"]; !ok {
		t.Error("existing marketplace entry was removed")
	}
	if _, ok := m[MarketplaceName]; !ok {
		t.Error("our marketplace entry was not added")
	}
}

func TestRegisterKnownMarketplace_Idempotent(t *testing.T) {
	setupHome(t)

	if err := RegisterKnownMarketplace("/tmp/test"); err != nil {
		t.Fatal(err)
	}
	if err := RegisterKnownMarketplace("/tmp/test"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, KnownMarketplacesPath())
	if _, ok := m[MarketplaceName]; !ok {
		t.Error("marketplace entry missing after second call")
	}
	// Verify file is still valid JSON (not corrupted).
	data, err := os.ReadFile(KnownMarketplacesPath())
	if err != nil {
		t.Fatal(err)
	}
	var check map[string]any
	if err := json.Unmarshal(data, &check); err != nil {
		t.Errorf("file corrupted after idempotent call: %v", err)
	}
}

func TestUnregisterKnownMarketplace_Removes(t *testing.T) {
	setupHome(t)

	// Set up with our entry and another.
	existing := map[string]any{
		MarketplaceName:     map[string]any{"source": "test"},
		"other-marketplace": map[string]any{"source": "other"},
	}
	if err := os.MkdirAll(filepath.Dir(KnownMarketplacesPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(KnownMarketplacesPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := UnregisterKnownMarketplace(); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, KnownMarketplacesPath())
	if _, ok := m[MarketplaceName]; ok {
		t.Error("our entry should be removed")
	}
	if _, ok := m["other-marketplace"]; !ok {
		t.Error("other entry should be preserved")
	}
}

func TestUnregisterKnownMarketplace_NotPresent(t *testing.T) {
	setupHome(t)
	if err := UnregisterKnownMarketplace(); err != nil {
		t.Errorf("UnregisterKnownMarketplace on missing file should not error, got: %v", err)
	}
}

func TestRegisterInstalledPlugin_Fresh(t *testing.T) {
	setupHome(t)

	if err := RegisterInstalledPlugin("/tmp/install", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, InstalledPluginsPath())

	// Check version field.
	ver, ok := m["version"].(float64)
	if !ok || ver != 2 {
		t.Errorf("version = %v, want 2", m["version"])
	}

	// Check our entry.
	entries, ok := m[QualifiedName].([]any)
	if !ok || len(entries) == 0 {
		t.Fatal("missing plugin entry")
	}
	entry := entries[0].(map[string]any)
	if entry["scope"] != "user" {
		t.Errorf("scope = %q, want %q", entry["scope"], "user")
	}
	if entry["installPath"] != "/tmp/install" {
		t.Errorf("installPath = %q, want %q", entry["installPath"], "/tmp/install")
	}
	if entry["version"] != "1.0.0" {
		t.Errorf("version = %q, want %q", entry["version"], "1.0.0")
	}

	// Verify file permissions.
	info, err := os.Stat(InstalledPluginsPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestRegisterInstalledPlugin_Existing(t *testing.T) {
	setupHome(t)

	// Write an existing file with another plugin.
	existing := map[string]any{
		"version": float64(2),
		"playwright@claude-plugins-official": []any{
			map[string]any{
				"scope":       "user",
				"installPath": "/tmp/playwright",
				"version":     "0.1.0",
			},
		},
	}
	if err := os.MkdirAll(filepath.Dir(InstalledPluginsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(InstalledPluginsPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RegisterInstalledPlugin("/tmp/install", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, InstalledPluginsPath())
	// Existing plugin preserved.
	if _, ok := m["playwright@claude-plugins-official"]; !ok {
		t.Error("existing plugin entry was removed")
	}
	// Our entry added.
	if _, ok := m[QualifiedName]; !ok {
		t.Error("our plugin entry was not added")
	}
	// Version field preserved.
	if v, _ := m["version"].(float64); v != 2 {
		t.Errorf("version = %v, want 2", m["version"])
	}
}

func TestRegisterInstalledPlugin_UpdatesExisting(t *testing.T) {
	setupHome(t)

	if err := RegisterInstalledPlugin("/tmp/install-v1", "1.0.0"); err != nil {
		t.Fatal(err)
	}

	// Re-register with a new version.
	if err := RegisterInstalledPlugin("/tmp/install-v2", "2.0.0"); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, InstalledPluginsPath())
	entries, ok := m[QualifiedName].([]any)
	if !ok || len(entries) == 0 {
		t.Fatal("missing plugin entry after update")
	}
	entry := entries[0].(map[string]any)
	if entry["version"] != "2.0.0" {
		t.Errorf("version = %q, want %q after update", entry["version"], "2.0.0")
	}
	if entry["installPath"] != "/tmp/install-v2" {
		t.Errorf("installPath = %q, want %q after update", entry["installPath"], "/tmp/install-v2")
	}
}

func TestUnregisterInstalledPlugin_Removes(t *testing.T) {
	setupHome(t)

	// Set up with our entry and another.
	existing := map[string]any{
		"version": float64(2),
		QualifiedName: []any{
			map[string]any{"scope": "user", "version": "1.0.0"},
		},
		"playwright@claude-plugins-official": []any{
			map[string]any{"scope": "user", "version": "0.1.0"},
		},
	}
	if err := os.MkdirAll(filepath.Dir(InstalledPluginsPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(InstalledPluginsPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := UnregisterInstalledPlugin(); err != nil {
		t.Fatal(err)
	}

	m := readJSON(t, InstalledPluginsPath())
	if _, ok := m[QualifiedName]; ok {
		t.Error("our entry should be removed")
	}
	if _, ok := m["playwright@claude-plugins-official"]; !ok {
		t.Error("other plugin entry should be preserved")
	}
}

func TestUnregisterInstalledPlugin_NotPresent(t *testing.T) {
	setupHome(t)
	if err := UnregisterInstalledPlugin(); err != nil {
		t.Errorf("UnregisterInstalledPlugin on missing file should not error, got: %v", err)
	}
}
