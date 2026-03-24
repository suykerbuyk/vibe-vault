// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package plugin

import (
	"os"
	"path/filepath"

	"github.com/johns/vibe-vault/internal/config"
)

const (
	// MarketplaceName is the name used in extraKnownMarketplaces.
	MarketplaceName = "vibe-vault-local"

	// QualifiedName is the enabledPlugins key ("plugin@marketplace").
	QualifiedName = "vibe-vault@vibe-vault-local"

	pluginName = "vibe-vault"
)

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
