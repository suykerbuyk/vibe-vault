// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/help"
	"github.com/johns/vibe-vault/internal/plugin"
)

const hookCommand = "vv hook"

// hookEvents are the Claude Code events vv registers for.
var hookEvents = []string{"SessionEnd", "Stop", "PreCompact"}

// SettingsPath returns the path to ~/.claude/settings.json.
func SettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// Install adds vv hook entries to ~/.claude/settings.json.
// Idempotent: returns nil (exit 0) even when already installed.
func Install() error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	if isInstalled(settings) {
		fmt.Fprintf(os.Stderr, "vv hook already configured in %s\n", config.CompressHome(path))
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	addHooks(settings)

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "vv hook installed in %s\n", config.CompressHome(path))
	return nil
}

// Uninstall removes vv hook entries from ~/.claude/settings.json.
// Idempotent: returns nil (exit 0) even when not installed.
func Uninstall() error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	if !hasAnyVVHook(settings) {
		fmt.Fprintf(os.Stderr, "vv hook not found in %s\n", config.CompressHome(path))
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	removeHooks(settings)

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "vv hook removed from %s\n", config.CompressHome(path))
	return nil
}

const mcpServerName = "vibe-vault"
const mcpCommand = "vv"

// InstallMCP adds the vibe-vault MCP server entry to ~/.claude/settings.json.
// Idempotent: returns nil when already installed.
func InstallMCP() error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	if isMCPInstalled(settings) {
		fmt.Fprintf(os.Stderr, "vibe-vault MCP server already configured in %s\n", config.CompressHome(path))
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	addMCP(settings)

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "vibe-vault MCP server installed in %s\n", config.CompressHome(path))
	fmt.Fprintf(os.Stderr, "Restart Claude Code to activate.\n")
	return nil
}

// UninstallMCP removes the vibe-vault MCP server entry from ~/.claude/settings.json.
// Idempotent: returns nil when not installed.
func UninstallMCP() error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	if !isMCPInstalled(settings) {
		fmt.Fprintf(os.Stderr, "vibe-vault MCP server not found in %s\n", config.CompressHome(path))
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	removeMCP(settings)

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "vibe-vault MCP server removed from %s\n", config.CompressHome(path))
	return nil
}

// ZedSettingsPath returns the path to ~/.config/zed/settings.json.
func ZedSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "zed", "settings.json"), nil
}

// InstallMCPZed adds the vibe-vault MCP server entry to Zed's settings.json.
// Idempotent: returns nil when already installed.
func InstallMCPZed() error {
	path, err := ZedSettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	if isMCPZedInstalled(settings) {
		fmt.Fprintf(os.Stderr, "vibe-vault MCP server already configured in %s\n", config.CompressHome(path))
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	addMCPZed(settings)

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "vibe-vault MCP server installed in %s\n", config.CompressHome(path))
	fmt.Fprintf(os.Stderr, "Restart Zed to activate.\n")
	return nil
}

// UninstallMCPZed removes the vibe-vault MCP server entry from Zed's settings.json.
// Idempotent: returns nil when not installed.
func UninstallMCPZed() error {
	path, err := ZedSettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	if !isMCPZedInstalled(settings) {
		fmt.Fprintf(os.Stderr, "vibe-vault MCP server not found in %s\n", config.CompressHome(path))
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	removeMCPZed(settings)

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "vibe-vault MCP server removed from %s\n", config.CompressHome(path))
	return nil
}

// isMCPZedInstalled returns true when context_servers contains a vibe-vault entry.
func isMCPZedInstalled(settings map[string]any) bool {
	servers, ok := settings["context_servers"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = servers[mcpServerName]
	return ok
}

// addMCPZed adds the vibe-vault entry to Zed's context_servers.
func addMCPZed(settings map[string]any) {
	servers, ok := settings["context_servers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
		settings["context_servers"] = servers
	}
	servers[mcpServerName] = map[string]any{
		"command": mcpCommand,
		"args":    []any{"mcp"},
	}
}

// removeMCPZed removes the vibe-vault entry from Zed's context_servers.
// Cleans up the context_servers map if empty.
func removeMCPZed(settings map[string]any) {
	servers, ok := settings["context_servers"].(map[string]any)
	if !ok {
		return
	}
	delete(servers, mcpServerName)
	if len(servers) == 0 {
		delete(settings, "context_servers")
	}
}

// isMCPInstalled returns true when mcpServers contains a vibe-vault entry.
func isMCPInstalled(settings map[string]any) bool {
	servers, ok := settings["mcpServers"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = servers[mcpServerName]
	return ok
}

// addMCP adds the vibe-vault MCP server entry.
func addMCP(settings map[string]any) {
	servers, ok := settings["mcpServers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
		settings["mcpServers"] = servers
	}
	servers[mcpServerName] = map[string]any{
		"command": mcpCommand,
		"args":    []any{"mcp"},
	}
}

// removeMCP removes the vibe-vault MCP server entry.
// Cleans up the mcpServers map if empty.
func removeMCP(settings map[string]any) {
	servers, ok := settings["mcpServers"].(map[string]any)
	if !ok {
		return
	}
	delete(servers, mcpServerName)
	if len(servers) == 0 {
		delete(settings, "mcpServers")
	}
}

// readSettings reads and parses the settings file.
// Returns an empty map if the file doesn't exist or is empty.
func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", config.CompressHome(path), err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return make(map[string]any), nil
	}

	var settings map[string]any
	if err := json.Unmarshal(stripJSONC(data), &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", config.CompressHome(path), err)
	}
	return settings, nil
}

// stripJSONC removes // line comments, /* block comments */, and trailing
// commas from JSONC input so it can be parsed by encoding/json. Handles
// comments inside strings correctly (they are preserved).
func stripJSONC(data []byte) []byte {
	var out []byte
	i := 0
	for i < len(data) {
		// String literal — copy verbatim (including any // or /* inside).
		if data[i] == '"' {
			out = append(out, data[i])
			i++
			for i < len(data) {
				out = append(out, data[i])
				if data[i] == '\\' {
					i++
					if i < len(data) {
						out = append(out, data[i])
					}
				} else if data[i] == '"' {
					break
				}
				i++
			}
			i++
			continue
		}

		// Line comment.
		if i+1 < len(data) && data[i] == '/' && data[i+1] == '/' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}

		// Block comment.
		if i+1 < len(data) && data[i] == '/' && data[i+1] == '*' {
			i += 2
			for i+1 < len(data) && (data[i] != '*' || data[i+1] != '/') {
				i++
			}
			i += 2
			continue
		}

		out = append(out, data[i])
		i++
	}

	// Strip trailing commas before } or ].
	result := make([]byte, 0, len(out))
	for j := 0; j < len(out); j++ {
		if out[j] == ',' {
			// Look ahead past whitespace for } or ].
			k := j + 1
			for k < len(out) && (out[k] == ' ' || out[k] == '\t' || out[k] == '\n' || out[k] == '\r') {
				k++
			}
			if k < len(out) && (out[k] == '}' || out[k] == ']') {
				continue // skip trailing comma
			}
		}
		result = append(result, out[j])
	}
	return result
}

// writeSettings writes the settings map as pretty-printed JSON.
// Creates the parent directory if needed.
func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", config.CompressHome(path), err)
	}
	return nil
}

// backup copies the settings file to path.vv.bak. No-op if source doesn't exist.
func backup(path string) error {
	src, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", config.CompressHome(path), err)
	}
	defer src.Close()

	dst, err := os.Create(path + ".vv.bak")
	if err != nil {
		return fmt.Errorf("backup: create %s.vv.bak: %w", config.CompressHome(path), err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("backup: copy: %w", err)
	}
	return nil
}

// isInstalled returns true when both SessionEnd and Stop have a vv hook entry.
func isInstalled(settings map[string]any) bool {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	for _, event := range hookEvents {
		if !eventHasVVHook(hooksMap, event) {
			return false
		}
	}
	return true
}

// hasAnyVVHook returns true when any event has a vv hook entry.
func hasAnyVVHook(settings map[string]any) bool {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	for _, event := range hookEvents {
		if eventHasVVHook(hooksMap, event) {
			return true
		}
	}
	return false
}

// addHooks ensures both events have a vv hook entry.
func addHooks(settings map[string]any) {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooksMap = make(map[string]any)
		settings["hooks"] = hooksMap
	}

	for _, event := range hookEvents {
		if eventHasVVHook(hooksMap, event) {
			continue
		}

		entry := map[string]any{
			"matcher": "",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": hookCommand,
				},
			},
		}

		eventArray, ok := hooksMap[event].([]any)
		if !ok {
			eventArray = []any{}
		}
		hooksMap[event] = append(eventArray, entry)
	}
}

// removeHooks removes entries containing "vv hook" from both events.
// Cleans up empty arrays and empty hooks map.
func removeHooks(settings map[string]any) {
	hooksMap, ok := settings["hooks"].(map[string]any)
	if !ok {
		return
	}

	for _, event := range hookEvents {
		eventArray, ok := hooksMap[event].([]any)
		if !ok {
			continue
		}

		var kept []any
		for _, entry := range eventArray {
			if !entryContainsVVHook(entry) {
				kept = append(kept, entry)
			}
		}

		if len(kept) == 0 {
			delete(hooksMap, event)
		} else {
			hooksMap[event] = kept
		}
	}

	if len(hooksMap) == 0 {
		delete(settings, "hooks")
	}
}

// eventHasVVHook checks whether the given event has a "vv hook" command entry.
func eventHasVVHook(hooksMap map[string]any, event string) bool {
	eventArray, ok := hooksMap[event].([]any)
	if !ok {
		return false
	}
	for _, entry := range eventArray {
		if entryContainsVVHook(entry) {
			return true
		}
	}
	return false
}

// claudeDetected returns true when the Claude Code settings directory exists.
// Detection must precede install calls because writeSettings creates parent
// directories via MkdirAll.
func claudeDetected() bool {
	p, err := SettingsPath()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Dir(p))
	return err == nil && info.IsDir()
}

// zedDetected returns true when the Zed settings directory exists.
func zedDetected() bool {
	p, err := ZedSettingsPath()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Dir(p))
	return err == nil && info.IsDir()
}

// InstallMCPAll installs the MCP server into all detected editors.
// Pass claudeOnly or zedOnly to restrict to a single editor.
func InstallMCPAll(claudeOnly, zedOnly bool) error {
	var errs []error
	if !zedOnly {
		if claudeDetected() {
			if err := InstallMCP(); err != nil {
				errs = append(errs, err)
			} else {
				fmt.Fprintf(os.Stderr, "note: if Claude Code tools don't appear, try: vv mcp install --claude-plugin\n")
			}
		} else {
			fmt.Fprintf(os.Stderr, "Claude Code: skipped (~/.claude/ not found)\n")
			fmt.Fprintf(os.Stderr, "  Run 'vv mcp install' after installing Claude Code.\n")
		}
	}
	if !claudeOnly {
		if zedDetected() {
			if err := InstallMCPZed(); err != nil {
				errs = append(errs, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Zed: skipped (~/.config/zed/ not found)\n")
			fmt.Fprintf(os.Stderr, "  Run 'vv mcp install' after installing Zed.\n")
		}
	}
	return errors.Join(errs...)
}

// UninstallMCPAll removes the MCP server from all detected editors.
// Pass claudeOnly or zedOnly to restrict to a single editor.
func UninstallMCPAll(claudeOnly, zedOnly bool) error {
	var errs []error
	if !zedOnly {
		if claudeDetected() {
			if err := UninstallMCP(); err != nil {
				errs = append(errs, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Claude Code: skipped (~/.claude/ not found)\n")
		}
	}
	if !claudeOnly {
		if zedDetected() {
			if err := UninstallMCPZed(); err != nil {
				errs = append(errs, err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Zed: skipped (~/.config/zed/ not found)\n")
		}
	}
	return errors.Join(errs...)
}

// InstallClaudePlugin deploys vibe-vault as a Claude Code plugin, working
// around the tool registration bug (#2682) in user-added MCP servers.
// Does NOT remove existing mcpServers entries — both can coexist safely.
func InstallClaudePlugin() error {
	if !claudeDetected() {
		return fmt.Errorf("claude Code not detected (~/.claude/ not found)")
	}

	mktDir, err := plugin.Generate(help.Version)
	if err != nil {
		return fmt.Errorf("generate plugin: %w", err)
	}

	path, err := SettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	if isPluginInstalled(settings) {
		fmt.Fprintf(os.Stderr, "vibe-vault plugin already configured in %s\n", config.CompressHome(path))
		return nil
	}

	if err := backup(path); err != nil {
		return err
	}

	addPluginMarketplace(settings, mktDir)
	addPluginEnabled(settings)

	if err := writeSettings(path, settings); err != nil {
		return err
	}

	// Direct injection into Claude Code's internal files (belt and suspenders).
	var cacheDetail string
	installPath, cacheErr := plugin.InstallToCache(help.Version)
	if cacheErr != nil {
		fmt.Fprintf(os.Stderr, "  warning: cache install: %v\n", cacheErr)
		cacheDetail = "(cache install failed)"
	} else {
		if err := plugin.RegisterKnownMarketplace(mktDir); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: known_marketplaces: %v\n", err)
		}
		if err := plugin.RegisterInstalledPlugin(installPath, help.Version); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: installed_plugins: %v\n", err)
		}
		cacheDetail = config.CompressHome(installPath)
	}

	fmt.Fprintf(os.Stderr, "vibe-vault plugin installed:\n")
	fmt.Fprintf(os.Stderr, "  Plugin files: %s\n", config.CompressHome(mktDir))
	fmt.Fprintf(os.Stderr, "  Plugin cache: %s\n", cacheDetail)
	fmt.Fprintf(os.Stderr, "  Settings:     %s\n", config.CompressHome(path))
	fmt.Fprintf(os.Stderr, "Restart Claude Code to activate.\n")
	return nil
}

// UninstallClaudePlugin removes the vibe-vault plugin configuration and files.
// Idempotent: returns nil when not installed.
func UninstallClaudePlugin() error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}

	settings, err := readSettings(path)
	if err != nil {
		return err
	}

	hadPlugin := isPluginInstalled(settings)

	if hadPlugin {
		if err := backup(path); err != nil {
			return err
		}

		removePluginMarketplace(settings)
		removePluginEnabled(settings)

		if err := writeSettings(path, settings); err != nil {
			return err
		}
	}

	if err := plugin.Remove(); err != nil {
		return fmt.Errorf("remove plugin directory: %w", err)
	}

	// Clean up Claude Code internal files (ignore errors — best effort).
	_ = plugin.UnregisterInstalledPlugin()
	_ = plugin.UnregisterKnownMarketplace()
	_ = plugin.RemoveFromCache()

	if !hadPlugin && !plugin.IsInstalled() {
		fmt.Fprintf(os.Stderr, "vibe-vault plugin not found in %s\n", config.CompressHome(path))
		return nil
	}

	fmt.Fprintf(os.Stderr, "vibe-vault plugin removed from %s\n", config.CompressHome(path))
	return nil
}

// isPluginInstalled returns true when both marketplace and enabledPlugins
// entries are present.
func isPluginInstalled(settings map[string]any) bool {
	return isPluginMarketplaceInstalled(settings) && isPluginEnabled(settings)
}

// isPluginMarketplaceInstalled checks for our marketplace entry.
func isPluginMarketplaceInstalled(settings map[string]any) bool {
	mkts, ok := settings["extraKnownMarketplaces"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = mkts[plugin.MarketplaceName]
	return ok
}

// addPluginMarketplace adds the local marketplace directory source.
func addPluginMarketplace(settings map[string]any, marketplaceDir string) {
	mkts, ok := settings["extraKnownMarketplaces"].(map[string]any)
	if !ok {
		mkts = make(map[string]any)
		settings["extraKnownMarketplaces"] = mkts
	}
	mkts[plugin.MarketplaceName] = map[string]any{
		"source": map[string]any{
			"source": "directory",
			"path":   marketplaceDir,
		},
	}
}

// removePluginMarketplace removes our marketplace entry.
// Cleans up the extraKnownMarketplaces map if empty.
func removePluginMarketplace(settings map[string]any) {
	mkts, ok := settings["extraKnownMarketplaces"].(map[string]any)
	if !ok {
		return
	}
	delete(mkts, plugin.MarketplaceName)
	if len(mkts) == 0 {
		delete(settings, "extraKnownMarketplaces")
	}
}

// isPluginEnabled checks for our enabledPlugins entry.
func isPluginEnabled(settings map[string]any) bool {
	plugins, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = plugins[plugin.QualifiedName]
	return ok
}

// addPluginEnabled adds the plugin to enabledPlugins.
func addPluginEnabled(settings map[string]any) {
	plugins, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		plugins = make(map[string]any)
		settings["enabledPlugins"] = plugins
	}
	plugins[plugin.QualifiedName] = true
}

// removePluginEnabled removes our plugin from enabledPlugins.
// Cleans up the enabledPlugins map if empty.
func removePluginEnabled(settings map[string]any) {
	plugins, ok := settings["enabledPlugins"].(map[string]any)
	if !ok {
		return
	}
	delete(plugins, plugin.QualifiedName)
	if len(plugins) == 0 {
		delete(settings, "enabledPlugins")
	}
}

// entryContainsVVHook checks whether a single hook entry contains "vv hook".
// It walks the nested JSON structure looking for a hooks array with a
// command matching hookCommand.
func entryContainsVVHook(entry any) bool {
	entryMap, ok := entry.(map[string]any)
	if !ok {
		return false
	}

	innerHooks, ok := entryMap["hooks"].([]any)
	if !ok {
		return false
	}

	for _, h := range innerHooks {
		hMap, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hMap["command"].(string)
		if strings.Contains(cmd, hookCommand) {
			return true
		}
	}
	return false
}
