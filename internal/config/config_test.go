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
