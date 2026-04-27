// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/suykerbuyk/vibe-vault/internal/meta"
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
	Synthesis  SynthesisConfig  `toml:"synthesis"`
	Wrap       WrapConfig       `toml:"wrap"`
	Providers  ProvidersConfig  `toml:"providers"`
}

// ProvidersConfig holds per-provider configuration. Phase 1 of the
// dispatch-api-key-resolution task introduces this section to give operators a
// config-first place to store API keys; resolution + factory plumbing land in
// later phases.
type ProvidersConfig struct {
	Anthropic ProviderConfig `toml:"anthropic"`
	OpenAI    ProviderConfig `toml:"openai"`
	Google    ProviderConfig `toml:"google"`
}

// ProviderConfig is the per-provider settings block. APIKey is the provider's
// API key; empty by default. Layered resolution (config → env → actionable
// error) lands in Phase 2 with internal/llm/keyresolver.go.
type ProviderConfig struct {
	APIKey string `toml:"api_key"`
}

// WrapConfig holds the [wrap] section of config.toml.
//
// Phase 4 of wrap-model-tiering lifts tier→model resolution from a hardcoded
// map in internal/mcp/tools_wrap_dispatch.go into operator-controlled config.
//
// DefaultModel is the tier label /wrap starts with when no flag overrides it.
// EscalationLadder is walked starting at DefaultModel; each entry must appear
// as a key in Tiers. Tiers maps a tier label (e.g. "sonnet") to a
// "provider:model" string (e.g. "anthropic:claude-sonnet-4-6"). v1 supports
// only the "anthropic:" provider; Validate enforces this fail-fast at load.
//
// When the [wrap] section is absent from config.toml, Load() pre-populates
// the three default tiers and DefaultModel="opus" — preserving the iter-157
// inline-Opus behavior.
type WrapConfig struct {
	DefaultModel     string            `toml:"default_model"`     // e.g. "sonnet" / "opus" / "haiku"
	EscalationLadder []string          `toml:"escalation_ladder"` // e.g. ["sonnet", "opus"]
	Tiers            map[string]string `toml:"tiers"`             // tier name -> "provider:model"
}

// SynthesisConfig controls the end-of-session synthesis agent.
type SynthesisConfig struct {
	Enabled        bool `toml:"enabled"`
	TimeoutSeconds int  `toml:"timeout_seconds"`
}

// DefaultAPIKeyEnv returns the conventional environment variable name for a
// provider's API key. Used as a fallback when api_key_env is not set in config.
func DefaultAPIKeyEnv(provider string) string {
	switch provider {
	case "openai", "":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "google":
		return "GOOGLE_API_KEY"
	default:
		return ""
	}
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
	TimelineRecentDays   int `toml:"timeline_recent_days"`    // full detail (default 7)
	TimelineWindowDays   int `toml:"timeline_window_days"`    // condensed (default 30)
	DecisionStaleDays    int `toml:"decision_stale_days"`     // decay threshold (default 90)
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
	Pattern              string  `toml:"pattern"`            // glob pattern, e.g. "claude-*"
	InputPerMillion      float64 `toml:"input_per_million"`  // USD per 1M input tokens
	OutputPerMillion     float64 `toml:"output_per_million"` // USD per 1M output tokens
	CacheReadPerMillion  float64 `toml:"cache_read_per_million"`
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
			// BaseURL left empty — each provider's NewX constructor falls back
			// to its own canonical URL (OpenAI → api.openai.com/v1,
			// Anthropic → api.anthropic.com, Google → its default). A hardcoded
			// xAI URL here was leaking into Anthropic/Google constructions when
			// users switched Provider without also setting base_url.
			BaseURL: "",
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
		Synthesis: SynthesisConfig{
			Enabled:        true,
			TimeoutSeconds: 60,
		},
		Wrap: WrapConfig{
			DefaultModel:     "opus",
			EscalationLadder: []string{},
			Tiers: map[string]string{
				"haiku":  "anthropic:claude-haiku-4-5",
				"sonnet": "anthropic:claude-sonnet-4-6",
				"opus":   "anthropic:claude-opus-4-7",
			},
		},
	}
}

// wrapTierValueRe matches a "provider:model" string. v1 only accepts
// "anthropic:" providers; Validate enforces that policy fail-fast at
// config load (rather than at first dispatch).
var wrapTierValueRe = regexp.MustCompile(`^[a-z]+:.+$`)

// Validate checks the configuration for internal consistency.
// Currently it covers the Wrap section: DefaultModel and EscalationLadder
// must reference defined tiers, and each tier value must use a supported
// provider prefix. Returns nil when the configuration is well-formed.
func (c Config) Validate() error {
	// Wrap.Tiers may legitimately be empty (e.g. when the operator opts out
	// of vv_wrap_dispatch entirely); skip wrap validation in that case.
	if len(c.Wrap.Tiers) == 0 {
		if c.Wrap.DefaultModel != "" {
			return fmt.Errorf("[wrap] default_model %q set but [wrap.tiers] is empty",
				c.Wrap.DefaultModel)
		}
		if len(c.Wrap.EscalationLadder) > 0 {
			return fmt.Errorf("[wrap] escalation_ladder set but [wrap.tiers] is empty")
		}
		return nil
	}

	for tier, providerModel := range c.Wrap.Tiers {
		if !wrapTierValueRe.MatchString(providerModel) {
			return fmt.Errorf("[wrap.tiers] %q value %q must be \"provider:model\"",
				tier, providerModel)
		}
		// v1 anthropic-only: fail fast at load so operators get a clear
		// error rather than a runtime EscalateReason later.
		provider := strings.SplitN(providerModel, ":", 2)[0]
		if provider != "anthropic" {
			return fmt.Errorf("[wrap.tiers] %q provider %q unsupported "+
				"(v1 supports only \"anthropic:\")", tier, provider)
		}
	}

	if c.Wrap.DefaultModel != "" {
		if _, ok := c.Wrap.Tiers[c.Wrap.DefaultModel]; !ok {
			return fmt.Errorf("[wrap] default_model %q not defined in [wrap.tiers]",
				c.Wrap.DefaultModel)
		}
	}

	for _, tier := range c.Wrap.EscalationLadder {
		if _, ok := c.Wrap.Tiers[tier]; !ok {
			return fmt.Errorf("[wrap] escalation_ladder references undefined tier %q",
				tier)
		}
	}
	return nil
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

	home, _ := meta.HomeDir()
	if home != "" {
		paths = append(paths, filepath.Join(home, ".config", "vibe-vault", "config.toml"))
	}

	return paths
}

func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := meta.HomeDir()
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
	if md.IsDefined("synthesis", "enabled") {
		c.Synthesis.Enabled = overlay.Synthesis.Enabled
	}
	if md.IsDefined("synthesis", "timeout_seconds") {
		c.Synthesis.TimeoutSeconds = overlay.Synthesis.TimeoutSeconds
	}
	if md.IsDefined("wrap", "default_model") {
		c.Wrap.DefaultModel = overlay.Wrap.DefaultModel
	}
	if md.IsDefined("wrap", "escalation_ladder") {
		// Slice replacement: matches the existing Pricing.Models pattern
		// at line ~290. Operators express the full ladder explicitly.
		c.Wrap.EscalationLadder = overlay.Wrap.EscalationLadder
	}
	if md.IsDefined("wrap", "tiers") {
		// H3-v3: map MERGE semantics, not whole-replacement. The Load()
		// path gets this for free from BurntSushi/toml's automatic map
		// merge, but Overlay() decodes into a fresh struct, so we have to
		// merge manually here. Without this an overlay that sets only
		// `[wrap.tiers] sonnet = "..."` would silently nuke the haiku and
		// opus defaults.
		//
		// We must also clone the base map before merging — even though
		// Overlay has a value receiver, c.Wrap.Tiers is a map header that
		// shares the same backing storage as the caller's Config. Writing
		// straight into it would mutate the caller's defaults.
		merged := make(map[string]string, len(c.Wrap.Tiers)+len(overlay.Wrap.Tiers))
		for k, v := range c.Wrap.Tiers {
			merged[k] = v
		}
		for k, v := range overlay.Wrap.Tiers {
			merged[k] = v
		}
		c.Wrap.Tiers = merged
	}
	// Providers: struct-of-structs, merged field-by-field via md.IsDefined().
	// Mirrors the Wrap.Tiers approach for the same reason — Overlay() decodes
	// into a fresh struct, so any provider sub-section the operator omits
	// must keep the base config's value.
	if md.IsDefined("providers", "anthropic", "api_key") {
		c.Providers.Anthropic.APIKey = overlay.Providers.Anthropic.APIKey
	}
	if md.IsDefined("providers", "openai", "api_key") {
		c.Providers.OpenAI.APIKey = overlay.Providers.OpenAI.APIKey
	}
	if md.IsDefined("providers", "google", "api_key") {
		c.Providers.Google.APIKey = overlay.Providers.Google.APIKey
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
