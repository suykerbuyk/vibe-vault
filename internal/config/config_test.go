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

func TestDefaultAPIKeyEnv(t *testing.T) {
	cases := []struct {
		provider string
		want     string
	}{
		{"openai", "OPENAI_API_KEY"},
		{"", "OPENAI_API_KEY"},
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"google", "GOOGLE_API_KEY"},
		{"unknown", ""},
	}
	for _, tc := range cases {
		got := DefaultAPIKeyEnv(tc.provider)
		if got != tc.want {
			t.Errorf("DefaultAPIKeyEnv(%q) = %q, want %q", tc.provider, got, tc.want)
		}
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

// TestLoad_PartialMapOverride verifies BurntSushi/toml's automatic map merge
// in the Load() path: pre-populating cfg.Wrap.Tiers with defaults BEFORE
// toml.DecodeFile means an overlay that overrides a single tier merges into
// the defaults rather than replacing the whole map.
//
// This is the H3-v3 "(a) Load() automatic merge" path — the gotcha is the
// difference vs. Overlay(), which decodes into a fresh struct and so needs
// explicit map-merge code (covered by TestOverlay_PartialMapMerge).
func TestLoad_PartialMapOverride(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())
	configDir := filepath.Join(xdg, "vibe-vault")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tomlContent := `[wrap.tiers]
sonnet = "anthropic:claude-sonnet-4-6-pinned"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// All three default tier keys must survive the overlay (BurntSushi merge).
	if got := cfg.Wrap.Tiers["sonnet"]; got != "anthropic:claude-sonnet-4-6-pinned" {
		t.Errorf("Tiers[sonnet] = %q, want overridden value", got)
	}
	if got := cfg.Wrap.Tiers["haiku"]; got != "anthropic:claude-haiku-4-5" {
		t.Errorf("Tiers[haiku] = %q, want default (merge should preserve)", got)
	}
	if got := cfg.Wrap.Tiers["opus"]; got != "anthropic:claude-opus-4-7" {
		t.Errorf("Tiers[opus] = %q, want default (merge should preserve)", got)
	}
}

// TestOverlay_PartialMapMerge verifies the new explicit merge code in
// Overlay(). Without that code, Tiers would whole-replace and the base
// haiku/opus entries would silently disappear.
func TestOverlay_PartialMapMerge(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[wrap.tiers]
sonnet = "anthropic:claude-sonnet-4-6-pinned"
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	base := DefaultConfig()
	result := base.Overlay(cfgPath)

	if got := result.Wrap.Tiers["sonnet"]; got != "anthropic:claude-sonnet-4-6-pinned" {
		t.Errorf("Tiers[sonnet] = %q, want overridden value", got)
	}
	if got := result.Wrap.Tiers["haiku"]; got != "anthropic:claude-haiku-4-5" {
		t.Errorf("Tiers[haiku] = %q, want default (Overlay merge should preserve)", got)
	}
	if got := result.Wrap.Tiers["opus"]; got != "anthropic:claude-opus-4-7" {
		t.Errorf("Tiers[opus] = %q, want default (Overlay merge should preserve)", got)
	}
	// Base map must not have been mutated by the merge.
	if base.Wrap.Tiers["sonnet"] != "anthropic:claude-sonnet-4-6" {
		t.Error("base config Tiers map was mutated")
	}
}

// TestOverlay_WrapDefaultModelAndLadder covers the simpler scalar/slice
// fields in the Wrap overlay path.
func TestOverlay_WrapDefaultModelAndLadder(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[wrap]
default_model = "sonnet"
escalation_ladder = ["sonnet", "opus"]
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	base := DefaultConfig()
	result := base.Overlay(cfgPath)

	if result.Wrap.DefaultModel != "sonnet" {
		t.Errorf("DefaultModel = %q, want sonnet", result.Wrap.DefaultModel)
	}
	if len(result.Wrap.EscalationLadder) != 2 || result.Wrap.EscalationLadder[0] != "sonnet" {
		t.Errorf("EscalationLadder = %v, want [sonnet opus]", result.Wrap.EscalationLadder)
	}
}

// TestValidate_HappyPath confirms a well-formed config passes validation.
func TestValidate_HappyPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Wrap.DefaultModel = "sonnet"
	cfg.Wrap.EscalationLadder = []string{"sonnet", "opus"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// TestValidate_EscalationLadder asserts a ladder entry not in Tiers errors.
func TestValidate_EscalationLadder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Wrap.EscalationLadder = []string{"nonexistent"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for undefined ladder tier")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error %q should name the offending tier", err)
	}
}

// TestValidate_DefaultModelUndefined asserts a DefaultModel not in Tiers errors.
func TestValidate_DefaultModelUndefined(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Wrap.DefaultModel = "phantom"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for undefined default_model")
	}
	if !strings.Contains(err.Error(), "phantom") {
		t.Errorf("error %q should name the offending tier", err)
	}
}

// TestValidate_NonAnthropicProviderRejected asserts v1's anthropic-only
// invariant is enforced fail-fast at config load.
func TestValidate_NonAnthropicProviderRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Wrap.Tiers["fast"] = "openai:gpt-4o"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for non-anthropic provider")
	}
	if !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("error %q should mention v1 anthropic-only restriction", err)
	}
}

// TestValidate_BadTierFormat asserts a malformed "provider:model" value is rejected.
func TestValidate_BadTierFormat(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Wrap.Tiers["fast"] = "no-colon-here"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for malformed tier value")
	}
}

// TestValidate_EmptyWrapSection allows empty Tiers (operator opts out).
func TestValidate_EmptyWrapSection(t *testing.T) {
	cfg := Config{}
	if err := cfg.Validate(); err != nil {
		t.Errorf("empty Wrap section should validate, got: %v", err)
	}
}

// TestDefaultConfig_WrapSection covers the new defaults block.
func TestDefaultConfig_WrapSection(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Wrap.DefaultModel != "opus" {
		t.Errorf("DefaultModel = %q, want opus", cfg.Wrap.DefaultModel)
	}
	if len(cfg.Wrap.EscalationLadder) != 0 {
		t.Errorf("EscalationLadder = %v, want empty", cfg.Wrap.EscalationLadder)
	}
	for _, tier := range []string{"haiku", "sonnet", "opus"} {
		if _, ok := cfg.Wrap.Tiers[tier]; !ok {
			t.Errorf("default Tiers missing %q", tier)
		}
	}
}

// TestLoad_ProvidersSection covers happy-path TOML deserialization for all
// three providers in [providers.<P>].api_key.
func TestLoad_ProvidersSection(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	configDir := filepath.Join(xdg, "vibe-vault")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tomlContent := `vault_path = "/custom/vault"

[providers.anthropic]
api_key = "sk-ant-test"

[providers.openai]
api_key = "sk-openai-test"

[providers.google]
api_key = "g-test-key"
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := cfg.Providers.Anthropic.APIKey; got != "sk-ant-test" {
		t.Errorf("Providers.Anthropic.APIKey = %q, want sk-ant-test", got)
	}
	if got := cfg.Providers.OpenAI.APIKey; got != "sk-openai-test" {
		t.Errorf("Providers.OpenAI.APIKey = %q, want sk-openai-test", got)
	}
	if got := cfg.Providers.Google.APIKey; got != "g-test-key" {
		t.Errorf("Providers.Google.APIKey = %q, want g-test-key", got)
	}
}

// TestLoad_NoProvidersSection asserts a config without [providers] silently
// defaults to an empty ProvidersConfig (forward-compat for old configs).
func TestLoad_NoProvidersSection(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	configDir := filepath.Join(xdg, "vibe-vault")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"),
		[]byte(`vault_path = "/custom/vault"`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Providers.Anthropic.APIKey != "" {
		t.Errorf("Anthropic.APIKey = %q, want empty (default)", cfg.Providers.Anthropic.APIKey)
	}
	if cfg.Providers.OpenAI.APIKey != "" {
		t.Errorf("OpenAI.APIKey = %q, want empty (default)", cfg.Providers.OpenAI.APIKey)
	}
	if cfg.Providers.Google.APIKey != "" {
		t.Errorf("Google.APIKey = %q, want empty (default)", cfg.Providers.Google.APIKey)
	}
}

// TestLoad_EmptyProvidersSection asserts that a config which declares
// [providers] but no sub-sections loads cleanly with all keys empty.
func TestLoad_EmptyProvidersSection(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	configDir := filepath.Join(xdg, "vibe-vault")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tomlContent := `vault_path = "/custom/vault"

[providers]
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Providers.Anthropic.APIKey != "" {
		t.Errorf("Anthropic.APIKey = %q, want empty", cfg.Providers.Anthropic.APIKey)
	}
	if cfg.Providers.OpenAI.APIKey != "" {
		t.Errorf("OpenAI.APIKey = %q, want empty", cfg.Providers.OpenAI.APIKey)
	}
	if cfg.Providers.Google.APIKey != "" {
		t.Errorf("Google.APIKey = %q, want empty", cfg.Providers.Google.APIKey)
	}
}

// TestOverlay_ProvidersAllThreeMerge asserts that an overlay setting keys for
// all three providers produces a merged result with all three present.
func TestOverlay_ProvidersAllThreeMerge(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[providers.anthropic]
api_key = "sk-ant-overlay"

[providers.openai]
api_key = "sk-openai-overlay"

[providers.google]
api_key = "g-overlay"
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	base := DefaultConfig()
	result := base.Overlay(cfgPath)

	if got := result.Providers.Anthropic.APIKey; got != "sk-ant-overlay" {
		t.Errorf("Anthropic.APIKey = %q, want sk-ant-overlay", got)
	}
	if got := result.Providers.OpenAI.APIKey; got != "sk-openai-overlay" {
		t.Errorf("OpenAI.APIKey = %q, want sk-openai-overlay", got)
	}
	if got := result.Providers.Google.APIKey; got != "g-overlay" {
		t.Errorf("Google.APIKey = %q, want g-overlay", got)
	}
	// Base must be unchanged.
	if base.Providers.Anthropic.APIKey != "" {
		t.Error("base config Providers.Anthropic was mutated")
	}
}

// TestOverlay_ProvidersFieldByField asserts that an overlay setting only one
// provider's key preserves keys set on other providers in the base config.
// Mirrors the Wrap.Tiers field-by-field merge expectation.
func TestOverlay_ProvidersFieldByField(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`[providers.openai]
api_key = "Y"
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	base := DefaultConfig()
	base.Providers.Anthropic.APIKey = "X"

	result := base.Overlay(cfgPath)

	if got := result.Providers.Anthropic.APIKey; got != "X" {
		t.Errorf("Anthropic.APIKey = %q, want X (overlay should preserve base)", got)
	}
	if got := result.Providers.OpenAI.APIKey; got != "Y" {
		t.Errorf("OpenAI.APIKey = %q, want Y (overlay should set)", got)
	}
	if got := result.Providers.Google.APIKey; got != "" {
		t.Errorf("Google.APIKey = %q, want empty (untouched)", got)
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
