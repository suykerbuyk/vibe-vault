package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.VaultPath != "~/obsidian/vibe-vault" {
		t.Errorf("VaultPath = %q", cfg.VaultPath)
	}
	if cfg.Domains.Work != "~/work" {
		t.Errorf("Domains.Work = %q", cfg.Domains.Work)
	}
	if cfg.Domains.Personal != "~/personal" {
		t.Errorf("Domains.Personal = %q", cfg.Domains.Personal)
	}
	if cfg.Domains.Opensource != "~/opensource" {
		t.Errorf("Domains.Opensource = %q", cfg.Domains.Opensource)
	}
	if cfg.Enrichment.Enabled {
		t.Error("Enrichment.Enabled should default to false")
	}
	if cfg.Enrichment.TimeoutSeconds != 10 {
		t.Errorf("Enrichment.TimeoutSeconds = %d", cfg.Enrichment.TimeoutSeconds)
	}
	if cfg.Enrichment.Provider != "openai" {
		t.Errorf("Enrichment.Provider = %q", cfg.Enrichment.Provider)
	}
	if cfg.Enrichment.Model != "grok-3-mini-fast" {
		t.Errorf("Enrichment.Model = %q", cfg.Enrichment.Model)
	}
	if cfg.Archive.Compress != true {
		t.Error("Archive.Compress should default to true")
	}
	if cfg.Friction.AlertThreshold != 40 {
		t.Errorf("Friction.AlertThreshold = %d, want 40", cfg.Friction.AlertThreshold)
	}
}

func TestLoad_NoConfig(t *testing.T) {
	// Point XDG to an empty dir so no config file is found
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Should have expanded defaults (VaultPath no longer starts with ~/)
	if strings.HasPrefix(cfg.VaultPath, "~/") {
		t.Errorf("VaultPath not expanded: %q", cfg.VaultPath)
	}
	if !strings.HasSuffix(cfg.VaultPath, "obsidian/vibe-vault") {
		t.Errorf("VaultPath = %q, want suffix obsidian/vibe-vault", cfg.VaultPath)
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	configDir := filepath.Join(xdg, "vibe-vault")
	os.MkdirAll(configDir, 0o755)

	tomlContent := `vault_path = "/custom/vault"

[domains]
work = "/my/work"
personal = "/my/personal"
opensource = "/my/oss"

[enrichment]
enabled = true
timeout_seconds = 30

[archive]
compress = false
`
	os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(tomlContent), 0o644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.VaultPath != "/custom/vault" {
		t.Errorf("VaultPath = %q", cfg.VaultPath)
	}
	if cfg.Domains.Work != "/my/work" {
		t.Errorf("Domains.Work = %q", cfg.Domains.Work)
	}
	if !cfg.Enrichment.Enabled {
		t.Error("Enrichment.Enabled should be true")
	}
	if cfg.Enrichment.TimeoutSeconds != 30 {
		t.Errorf("Enrichment.TimeoutSeconds = %d", cfg.Enrichment.TimeoutSeconds)
	}
	if cfg.Archive.Compress {
		t.Error("Archive.Compress should be false")
	}
}

func TestLoad_FrictionConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	configDir := filepath.Join(xdg, "vibe-vault")
	os.MkdirAll(configDir, 0o755)

	tomlContent := `vault_path = "/custom/vault"

[friction]
alert_threshold = 60
`
	os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(tomlContent), 0o644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Friction.AlertThreshold != 60 {
		t.Errorf("Friction.AlertThreshold = %d, want 60", cfg.Friction.AlertThreshold)
	}
}

func TestLoad_FrictionConfigAbsent(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	configDir := filepath.Join(xdg, "vibe-vault")
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(`vault_path = "/custom/vault"`), 0o644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Friction.AlertThreshold != 40 {
		t.Errorf("Friction.AlertThreshold = %d, want 40 (default)", cfg.Friction.AlertThreshold)
	}
}

func TestLoad_ExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	configDir := filepath.Join(xdg, "vibe-vault")
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(`vault_path = "~/my-vault"`), 0o644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := filepath.Join(home, "my-vault")
	if cfg.VaultPath != want {
		t.Errorf("VaultPath = %q, want %q", cfg.VaultPath, want)
	}
}

func TestLoad_XDGPriority(t *testing.T) {
	xdg := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", home)

	// Create config at XDG path
	xdgDir := filepath.Join(xdg, "vibe-vault")
	os.MkdirAll(xdgDir, 0o755)
	os.WriteFile(filepath.Join(xdgDir, "config.toml"), []byte(`vault_path = "/from-xdg"`), 0o644)

	// Also create config at ~/.config path
	homeDir := filepath.Join(home, ".config", "vibe-vault")
	os.MkdirAll(homeDir, 0o755)
	os.WriteFile(filepath.Join(homeDir, "config.toml"), []byte(`vault_path = "/from-home"`), 0o644)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.VaultPath != "/from-xdg" {
		t.Errorf("VaultPath = %q, want /from-xdg (XDG should take priority)", cfg.VaultPath)
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	configDir := filepath.Join(xdg, "vibe-vault")
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(`vault_path = [broken`), 0o644)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestOverlay_TagsOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	os.WriteFile(cfgPath, []byte(`[tags]
session = "my-custom-tag"
extra = ["team-x"]
`), 0o644)

	base := DefaultConfig()
	result := base.Overlay(cfgPath)

	if result.Tags.Session != "my-custom-tag" {
		t.Errorf("Tags.Session = %q, want my-custom-tag", result.Tags.Session)
	}
	if len(result.Tags.Extra) != 1 || result.Tags.Extra[0] != "team-x" {
		t.Errorf("Tags.Extra = %v, want [team-x]", result.Tags.Extra)
	}
	// Base should be unchanged
	if base.Tags.Session != DefaultSessionTag {
		t.Error("base config was mutated")
	}
}

func TestOverlay_PartialOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	os.WriteFile(cfgPath, []byte(`[friction]
alert_threshold = 80
`), 0o644)

	base := DefaultConfig()
	result := base.Overlay(cfgPath)

	if result.Friction.AlertThreshold != 80 {
		t.Errorf("Friction.AlertThreshold = %d, want 80", result.Friction.AlertThreshold)
	}
	// Other fields unchanged
	if result.Tags.Session != DefaultSessionTag {
		t.Errorf("Tags.Session = %q, want %q (unchanged)", result.Tags.Session, DefaultSessionTag)
	}
	if result.Enrichment.Model != "grok-3-mini-fast" {
		t.Errorf("Enrichment.Model = %q, want grok-3-mini-fast (unchanged)", result.Enrichment.Model)
	}
}

func TestOverlay_MCPConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	os.WriteFile(cfgPath, []byte(`[mcp]
default_max_tokens = 8000
`), 0o644)

	base := DefaultConfig()
	result := base.Overlay(cfgPath)

	if result.MCP.DefaultMaxTokens != 8000 {
		t.Errorf("MCP.DefaultMaxTokens = %d, want 8000", result.MCP.DefaultMaxTokens)
	}
	// Base should be unchanged
	if base.MCP.DefaultMaxTokens != 4000 {
		t.Error("base MCP config was mutated")
	}
}

func TestOverlay_MissingFile(t *testing.T) {
	base := DefaultConfig()
	result := base.Overlay("/nonexistent/config.toml")

	if result.Tags.Session != base.Tags.Session {
		t.Error("overlay from missing file should return base unchanged")
	}
}

func TestOverlay_FullyCommented(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	os.WriteFile(cfgPath, []byte(ProjectConfigTemplate()), 0o644)

	base := DefaultConfig()
	result := base.Overlay(cfgPath)

	// All-commented file should leave base unchanged
	if result.Tags.Session != base.Tags.Session {
		t.Errorf("Tags.Session changed to %q from all-commented overlay", result.Tags.Session)
	}
	if result.Friction.AlertThreshold != base.Friction.AlertThreshold {
		t.Errorf("Friction.AlertThreshold changed to %d from all-commented overlay", result.Friction.AlertThreshold)
	}
}

func TestWithProjectOverlay(t *testing.T) {
	dir := t.TempDir()
	base := Config{VaultPath: dir}

	// Create project config
	projDir := filepath.Join(dir, "Projects", "myproj", "agentctx")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "config.toml"), []byte(`[tags]
session = "proj-tag"
`), 0o644)

	result := base.WithProjectOverlay("myproj")
	if result.Tags.Session != "proj-tag" {
		t.Errorf("Tags.Session = %q, want proj-tag", result.Tags.Session)
	}
}

func TestSessionTag(t *testing.T) {
	// Default
	cfg := DefaultConfig()
	if cfg.SessionTag() != DefaultSessionTag {
		t.Errorf("SessionTag = %q, want %q", cfg.SessionTag(), DefaultSessionTag)
	}

	// Custom
	cfg.Tags.Session = "custom"
	if cfg.SessionTag() != "custom" {
		t.Errorf("SessionTag = %q, want custom", cfg.SessionTag())
	}

	// Empty falls back to default
	cfg.Tags.Session = ""
	if cfg.SessionTag() != DefaultSessionTag {
		t.Errorf("SessionTag = %q, want %q", cfg.SessionTag(), DefaultSessionTag)
	}
}

func TestSessionTags(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tags.Extra = []string{"team-x"}

	tags := cfg.SessionTags("debugging")
	if len(tags) != 3 {
		t.Fatalf("len = %d, want 3", len(tags))
	}
	if tags[0] != DefaultSessionTag {
		t.Errorf("tags[0] = %q", tags[0])
	}
	if tags[1] != "team-x" {
		t.Errorf("tags[1] = %q", tags[1])
	}
	if tags[2] != "debugging" {
		t.Errorf("tags[2] = %q", tags[2])
	}
}

func TestProjectsDir_StateDir(t *testing.T) {
	cfg := Config{VaultPath: "/home/user/vault"}

	projDir := cfg.ProjectsDir()
	if projDir != "/home/user/vault/Projects" {
		t.Errorf("ProjectsDir = %q", projDir)
	}

	stateDir := cfg.StateDir()
	if stateDir != "/home/user/vault/.vibe-vault" {
		t.Errorf("StateDir = %q", stateDir)
	}
}
