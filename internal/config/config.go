package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config holds all vibe-vault configuration.
type Config struct {
	VaultPath string `toml:"vault_path"`

	Domains    DomainsConfig    `toml:"domains"`
	Enrichment EnrichmentConfig `toml:"enrichment"`
	Archive    ArchiveConfig    `toml:"archive"`
}

type DomainsConfig struct {
	Work       string `toml:"work"`
	Personal   string `toml:"personal"`
	Opensource string `toml:"opensource"`
}

type EnrichmentConfig struct {
	Enabled        bool   `toml:"enabled"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
	Provider       string `toml:"provider"`
	Model          string `toml:"model"`
	APIKeyEnv      string `toml:"api_key_env"`
	BaseURL        string `toml:"base_url"`
}

type ArchiveConfig struct {
	Compress bool `toml:"compress"`
}

// DefaultConfig returns config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		VaultPath: "~/obsidian/vibe-vault",
		Domains: DomainsConfig{
			Work:       "~/work",
			Personal:   "~/personal",
			Opensource: "~/opensource",
		},
		Enrichment: EnrichmentConfig{
			Enabled:        false,
			TimeoutSeconds: 10,
			Provider:       "openai",
			Model:          "grok-3-mini-fast",
			APIKeyEnv:      "XAI_API_KEY",
			BaseURL:        "https://api.x.ai/v1",
		},
		Archive: ArchiveConfig{
			Compress: true,
		},
	}
}

// Load reads config from the standard path, falling back to defaults.
func Load() (Config, error) {
	cfg := DefaultConfig()

	paths := configPaths()
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			if _, err := toml.DecodeFile(p, &cfg); err != nil {
				return cfg, fmt.Errorf("parse config %s: %w", p, err)
			}
			break
		}
	}

	// Expand ~ in paths
	cfg.VaultPath = expandHome(cfg.VaultPath)
	cfg.Domains.Work = expandHome(cfg.Domains.Work)
	cfg.Domains.Personal = expandHome(cfg.Domains.Personal)
	cfg.Domains.Opensource = expandHome(cfg.Domains.Opensource)

	return cfg, nil
}

func configPaths() []string {
	var paths []string

	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "vibe-vault", "config.toml"))
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		paths = append(paths, filepath.Join(home, ".config", "vibe-vault", "config.toml"))
	}

	return paths
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// SessionsDir returns the vault's Sessions directory.
func (c Config) SessionsDir() string {
	return filepath.Join(c.VaultPath, "Sessions")
}

// StateDir returns the .vibe-vault state directory inside the vault.
func (c Config) StateDir() string {
	return filepath.Join(c.VaultPath, ".vibe-vault")
}
