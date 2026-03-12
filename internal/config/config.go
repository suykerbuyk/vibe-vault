// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultSessionTag is the default tag applied to all session notes.
const DefaultSessionTag = "vv-session"

// Config holds all vibe-vault configuration.
type Config struct {
	VaultPath string `toml:"vault_path"`

	Domains    DomainsConfig    `toml:"domains"`
	Tags       TagsConfig       `toml:"tags"`
	Enrichment EnrichmentConfig `toml:"enrichment"`
	Archive    ArchiveConfig    `toml:"archive"`
	Friction   FrictionConfig   `toml:"friction"`
	Pricing    PricingConfig    `toml:"pricing"`
	History    HistoryConfig    `toml:"history"`
	MCP        MCPConfig        `toml:"mcp"`
	Zed        ZedConfig        `toml:"zed"`
}

// ZedConfig controls Zed integration behavior.
type ZedConfig struct {
	DBPath          string `toml:"db_path"`          // override threads.db location
	DebounceMinutes int    `toml:"debounce_minutes"` // quiet period for watch (default 5)
	AutoCapture     bool   `toml:"auto_capture"`     // auto-capture threads via MCP watcher
}

// MCPConfig controls MCP server behavior.
type MCPConfig struct {
	DefaultMaxTokens int `toml:"default_max_tokens"`
}

// HistoryConfig controls history.md pruning and decay behavior.
type HistoryConfig struct {
	TimelineRecentDays   int `toml:"timeline_recent_days"`   // full detail (default 7)
	TimelineWindowDays   int `toml:"timeline_window_days"`   // condensed (default 30)
	DecisionStaleDays    int `toml:"decision_stale_days"`    // decay threshold (default 90)
	KeyFilesRecencyBoost int `toml:"key_files_recency_boost"` // multiplier for recent sessions (default 3)
}

// TagsConfig controls tags applied to session notes.
type TagsConfig struct {
	Session string   `toml:"session"` // base tag for all session notes (default: "vv-session")
	Extra   []string `toml:"extra"`   // additional tags applied to all sessions
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

type FrictionConfig struct {
	AlertThreshold int `toml:"alert_threshold"`
}

// PricingConfig holds per-model cost estimation settings.
type PricingConfig struct {
	Enabled bool           `toml:"enabled"`
	Models  []PricingModel `toml:"models"`
}

// PricingModel defines token rates for a model name pattern.
type PricingModel struct {
	Pattern             string  `toml:"pattern"`              // glob pattern, e.g. "claude-*"
	InputPerMillion     float64 `toml:"input_per_million"`    // USD per 1M input tokens
	OutputPerMillion    float64 `toml:"output_per_million"`   // USD per 1M output tokens
	CacheReadPerMillion float64 `toml:"cache_read_per_million"`
	CacheWritePerMillion float64 `toml:"cache_write_per_million"`
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
		Tags: TagsConfig{
			Session: DefaultSessionTag,
		},
		Friction: FrictionConfig{
			AlertThreshold: 40,
		},
		History: HistoryConfig{
			TimelineRecentDays:   7,
			TimelineWindowDays:   30,
			DecisionStaleDays:    90,
			KeyFilesRecencyBoost: 3,
		},
		MCP: MCPConfig{
			DefaultMaxTokens: 4000,
		},
		Zed: ZedConfig{
			DebounceMinutes: 5,
			AutoCapture:     true,
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
	cfg.Zed.DBPath = expandHome(cfg.Zed.DBPath)

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

// SessionTag returns the configured session tag, defaulting to DefaultSessionTag.
func (c Config) SessionTag() string {
	if c.Tags.Session != "" {
		return c.Tags.Session
	}
	return DefaultSessionTag
}

// SessionTags returns all tags for a session note: session tag + extra + activity tag.
func (c Config) SessionTags(activityTag string) []string {
	tags := []string{c.SessionTag()}
	tags = append(tags, c.Tags.Extra...)
	if activityTag != "" {
		tags = append(tags, activityTag)
	}
	return tags
}

// Overlay applies a project-local config.toml on top of this config.
// Only non-zero values in the overlay replace the base config.
// Returns the original config unchanged if the file doesn't exist.
func (c Config) Overlay(projectConfigPath string) Config {
	if _, err := os.Stat(projectConfigPath); err != nil {
		return c
	}
	// Decode into a fresh struct so we can detect which fields were set.
	// TOML decoder only populates fields present in the file.
	var overlay Config
	md, err := toml.DecodeFile(projectConfigPath, &overlay)
	if err != nil {
		return c
	}

	// Apply only keys that were explicitly set in the overlay file
	if md.IsDefined("tags", "session") && overlay.Tags.Session != "" {
		c.Tags.Session = overlay.Tags.Session
	}
	if md.IsDefined("tags", "extra") {
		c.Tags.Extra = overlay.Tags.Extra
	}
	if md.IsDefined("enrichment", "enabled") {
		c.Enrichment.Enabled = overlay.Enrichment.Enabled
	}
	if md.IsDefined("enrichment", "timeout_seconds") {
		c.Enrichment.TimeoutSeconds = overlay.Enrichment.TimeoutSeconds
	}
	if md.IsDefined("enrichment", "provider") {
		c.Enrichment.Provider = overlay.Enrichment.Provider
	}
	if md.IsDefined("enrichment", "model") {
		c.Enrichment.Model = overlay.Enrichment.Model
	}
	if md.IsDefined("enrichment", "api_key_env") {
		c.Enrichment.APIKeyEnv = overlay.Enrichment.APIKeyEnv
	}
	if md.IsDefined("enrichment", "base_url") {
		c.Enrichment.BaseURL = overlay.Enrichment.BaseURL
	}
	if md.IsDefined("archive", "compress") {
		c.Archive.Compress = overlay.Archive.Compress
	}
	if md.IsDefined("friction", "alert_threshold") {
		c.Friction.AlertThreshold = overlay.Friction.AlertThreshold
	}
	if md.IsDefined("pricing", "enabled") {
		c.Pricing.Enabled = overlay.Pricing.Enabled
	}
	if md.IsDefined("pricing", "models") {
		c.Pricing.Models = overlay.Pricing.Models
	}
	if md.IsDefined("history", "timeline_recent_days") {
		c.History.TimelineRecentDays = overlay.History.TimelineRecentDays
	}
	if md.IsDefined("history", "timeline_window_days") {
		c.History.TimelineWindowDays = overlay.History.TimelineWindowDays
	}
	if md.IsDefined("history", "decision_stale_days") {
		c.History.DecisionStaleDays = overlay.History.DecisionStaleDays
	}
	if md.IsDefined("history", "key_files_recency_boost") {
		c.History.KeyFilesRecencyBoost = overlay.History.KeyFilesRecencyBoost
	}
	if md.IsDefined("mcp", "default_max_tokens") {
		c.MCP.DefaultMaxTokens = overlay.MCP.DefaultMaxTokens
	}
	if md.IsDefined("zed", "db_path") {
		c.Zed.DBPath = overlay.Zed.DBPath
	}
	if md.IsDefined("zed", "debounce_minutes") {
		c.Zed.DebounceMinutes = overlay.Zed.DebounceMinutes
	}
	if md.IsDefined("zed", "auto_capture") {
		c.Zed.AutoCapture = overlay.Zed.AutoCapture
	}

	return c
}

// ProjectConfigPath returns the path to a project's local config overlay.
func (c Config) ProjectConfigPath(project string) string {
	return filepath.Join(c.VaultPath, "Projects", project, "agentctx", "config.toml")
}

// WithProjectOverlay loads and applies a project-local config overlay.
func (c Config) WithProjectOverlay(project string) Config {
	return c.Overlay(c.ProjectConfigPath(project))
}

// ProjectsDir returns the vault's Projects directory.
func (c Config) ProjectsDir() string {
	return filepath.Join(c.VaultPath, "Projects")
}

// StateDir returns the .vibe-vault state directory inside the vault.
func (c Config) StateDir() string {
	return filepath.Join(c.VaultPath, ".vibe-vault")
}
