// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Generate creates the Claude Code plugin directory structure.
// The two-level layout matches the official marketplace convention:
//
//	~/.local/share/vibe-vault/claude-plugin/       ← marketplace root
//	  .claude-plugin/marketplace.json              ← marketplace manifest
//	  vibe-vault/                                  ← plugin subdirectory
//	    .claude-plugin/plugin.json                 ← plugin manifest
//	    .mcp.json                                  ← MCP server config
//
// Returns the marketplace root path on success.
//
// The MCP config writes "vv" (PATH-relative) rather than an absolute binary
// path so the plugin always invokes whichever vv resolves first in PATH at
// session start. Baking an absolute path here is brittle: if the install was
// triggered by a stale binary on PATH (or one invoked explicitly), os.Executable
// can capture that stale path and pin the plugin to it across rebuilds. PATH
// lookup mirrors how settings.json mcpServers.vibe-vault is configured and
// converges on the operator's installed binary automatically.
func Generate(version string) (string, error) {
	mktDir := MarketplaceDir()
	plugDir := PluginDir()

	// Create directories.
	for _, dir := range []string{
		filepath.Join(mktDir, ".claude-plugin"),
		filepath.Join(plugDir, ".claude-plugin"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	// Write marketplace manifest.
	marketplace := map[string]any{
		"$schema":     "https://anthropic.com/claude-code/marketplace.schema.json",
		"name":        MarketplaceName,
		"description": "Local vibe-vault plugin marketplace",
		"owner":       map[string]any{"name": "vibe-vault", "email": "noreply@vibe-vault.dev"},
		"plugins": []any{
			map[string]any{
				"name":        pluginName,
				"description": "MCP server for session capture and project context",
				"source":      "./" + pluginName,
			},
		},
	}
	if err := writeJSON(MarketplaceManifestPath(), marketplace); err != nil {
		return "", fmt.Errorf("write marketplace manifest: %w", err)
	}

	// Write plugin manifest.
	plugin := map[string]any{
		"name":        pluginName,
		"version":     version,
		"description": "Session capture, knowledge management, and project context for AI coding agents",
		"author":      map[string]any{"name": "vibe-vault"},
	}
	if err := writeJSON(PluginManifestPath(), plugin); err != nil {
		return "", fmt.Errorf("write plugin manifest: %w", err)
	}

	// Write MCP config via the shared mcpServerEntry helper so this writer
	// and InstallToCache cannot drift. See plugin.go for the env-passthrough
	// rationale and Generate's doc comment for why "vv" is PATH-relative.
	mcpConfig := map[string]any{
		pluginName: mcpServerEntry(),
	}
	if err := writeJSON(MCPConfigPath(), mcpConfig); err != nil {
		return "", fmt.Errorf("write MCP config: %w", err)
	}

	return mktDir, nil
}

// Remove deletes the entire marketplace directory tree.
// Returns nil if the directory doesn't exist.
func Remove() error {
	mktDir := MarketplaceDir()
	if _, err := os.Stat(mktDir); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(mktDir)
}

// writeJSON marshals v as pretty-printed JSON and writes it atomically.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
