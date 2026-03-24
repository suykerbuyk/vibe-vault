// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
func Generate(version string) (string, error) {
	binaryPath, err := resolveBinary()
	if err != nil {
		return "", fmt.Errorf("resolve vv binary: %w", err)
	}

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

	// Write MCP config.
	mcpConfig := map[string]any{
		pluginName: map[string]any{
			"command": binaryPath,
			"args":    []any{"mcp"},
		},
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

// resolveBinary returns the absolute path to the vv binary.
// Prefers exec.LookPath (matches how Claude Code's Bun spawn resolves
// commands), falling back to os.Executable if LookPath fails.
func resolveBinary() (string, error) {
	if p, err := exec.LookPath("vv"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs, nil
		}
		return p, nil
	}

	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate vv binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved, nil
	}
	return p, nil
}

// writeJSON marshals v as pretty-printed JSON and writes it atomically.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
