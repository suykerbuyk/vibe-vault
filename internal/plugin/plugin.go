// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package plugin

import (
	"os"
	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
)

const (
	// MarketplaceName is the name used in extraKnownMarketplaces.
	MarketplaceName = "vibe-vault-local"

	// QualifiedName is the enabledPlugins key ("plugin@marketplace").
	QualifiedName = "vibe-vault@vibe-vault-local"

	pluginName = "vibe-vault"
)

// mcpEnvPassthroughKeys lists environment variables propagated from the
// operator's shell into the MCP server subprocess via .mcp.json's env block.
// Claude Code expands ${VAR} references against the parent process env at
// spawn time, so the subprocess sees the operator's live key. Without this,
// vv_wrap_dispatch (and other LLM-backed handlers) cannot reach the provider.
var mcpEnvPassthroughKeys = []string{"ANTHROPIC_API_KEY"}

// mcpServerEntry returns the canonical .mcp.json entry for the vibe-vault
// MCP server. Single source of truth used by both Generate (marketplace
// source) and InstallToCache (Claude Code plugin cache) so the two configs
// can never drift.
func mcpServerEntry() map[string]any {
	env := make(map[string]any, len(mcpEnvPassthroughKeys))
	for _, key := range mcpEnvPassthroughKeys {
		env[key] = "${" + key + "}"
	}
	return map[string]any{
		"command": "vv",
		"args":    []any{"mcp"},
		"env":     env,
	}
}

// MarketplaceDir returns the marketplace root directory.
// This is the directory registered in extraKnownMarketplaces.
func MarketplaceDir() string {
	return filepath.Join(config.DataDir(), "claude-plugin")
}

// PluginDir returns the plugin subdirectory inside the marketplace.
func PluginDir() string {
	return filepath.Join(MarketplaceDir(), pluginName)
}

// MarketplaceManifestPath returns the path to the marketplace manifest.
func MarketplaceManifestPath() string {
	return filepath.Join(MarketplaceDir(), ".claude-plugin", "marketplace.json")
}

// PluginManifestPath returns the path to the plugin manifest.
func PluginManifestPath() string {
	return filepath.Join(PluginDir(), ".claude-plugin", "plugin.json")
}

// MCPConfigPath returns the path to the plugin's MCP config.
func MCPConfigPath() string {
	return filepath.Join(PluginDir(), ".mcp.json")
}

// ClaudePluginsDir returns the Claude Code plugins directory (~/.claude/plugins/).
func ClaudePluginsDir() string {
	home, _ := meta.HomeDir()
	return filepath.Join(home, ".claude", "plugins")
}

// CacheInstallDir returns the cache install directory for a specific version.
func CacheInstallDir(version string) string {
	return filepath.Join(ClaudePluginsDir(), "cache", MarketplaceName, pluginName, version)
}

// KnownMarketplacesPath returns the path to ~/.claude/plugins/known_marketplaces.json.
func KnownMarketplacesPath() string {
	return filepath.Join(ClaudePluginsDir(), "known_marketplaces.json")
}

// InstalledPluginsPath returns the path to ~/.claude/plugins/installed_plugins.json.
func InstalledPluginsPath() string {
	return filepath.Join(ClaudePluginsDir(), "installed_plugins.json")
}

// AnyCacheInstalled returns true if any version directory exists under the cache path.
func AnyCacheInstalled() bool {
	cacheDir := filepath.Join(ClaudePluginsDir(), "cache", MarketplaceName, pluginName)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
}

// IsInstalled returns true when all three required files exist.
func IsInstalled() bool {
	for _, p := range []string{
		MarketplaceManifestPath(),
		PluginManifestPath(),
		MCPConfigPath(),
	} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}
