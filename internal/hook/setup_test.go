package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func settingsPath(home string) string {
	return filepath.Join(home, ".claude", "settings.json")
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func hasEvent(settings map[string]any, event string) bool {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	return eventHasVVHook(hooks, event)
}

func TestInstall_NoFile(t *testing.T) {
	home := setupHome(t)

	if err := Install(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if !hasEvent(settings, "SessionEnd") {
		t.Error("missing SessionEnd hook")
	}
	if !hasEvent(settings, "Stop") {
		t.Error("missing Stop hook")
	}

	// No backup should exist (no source file existed)
	if _, err := os.Stat(path + ".vv.bak"); !os.IsNotExist(err) {
		t.Error("backup should not exist for fresh install")
	}
}

func TestInstall_EmptyFile(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Install(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if !hasEvent(settings, "SessionEnd") {
		t.Error("missing SessionEnd hook")
	}
	if !hasEvent(settings, "Stop") {
		t.Error("missing Stop hook")
	}
}

func TestInstall_ExistingSettingsNoHooks(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"permissions": map[string]any{"allow": true},
		"verbose":     false,
	})

	if err := Install(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if !hasEvent(settings, "SessionEnd") {
		t.Error("missing SessionEnd hook")
	}
	// Verify existing keys preserved
	if _, ok := settings["permissions"]; !ok {
		t.Error("existing 'permissions' key was lost")
	}
	if _, ok := settings["verbose"]; !ok {
		t.Error("existing 'verbose' key was lost")
	}
}

func TestInstall_PreservesExistingHooks(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"hooks": map[string]any{
			"SessionEnd": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "other-tool"},
					},
				},
			},
		},
	})

	if err := Install(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	hooks := settings["hooks"].(map[string]any)
	sessionEnd := hooks["SessionEnd"].([]any)
	if len(sessionEnd) != 2 {
		t.Errorf("expected 2 SessionEnd entries, got %d", len(sessionEnd))
	}
}

func TestInstall_Idempotent(t *testing.T) {
	home := setupHome(t)

	// First install
	if err := Install(); err != nil {
		t.Fatal(err)
	}

	// Read the content after first install for comparison
	path := settingsPath(home)
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Second install — should be a no-op
	if installErr := Install(); installErr != nil {
		t.Fatal(installErr)
	}

	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Error("idempotent install modified the file")
	}

	// Verify no duplicates
	settings := readJSON(t, path)
	hooks := settings["hooks"].(map[string]any)
	sessionEnd := hooks["SessionEnd"].([]any)
	if len(sessionEnd) != 1 {
		t.Errorf("expected 1 SessionEnd entry after idempotent install, got %d", len(sessionEnd))
	}
}

func TestInstall_PartialHooks(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)

	// Only SessionEnd configured
	writeJSON(t, path, map[string]any{
		"hooks": map[string]any{
			"SessionEnd": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
			},
		},
	})

	if err := Install(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if !hasEvent(settings, "Stop") {
		t.Error("partial install should have added Stop hook")
	}
	if !hasEvent(settings, "SessionEnd") {
		t.Error("partial install should have preserved SessionEnd hook")
	}
}

func TestInstall_CreatesBackup(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)

	original := map[string]any{"existing": "data"}
	writeJSON(t, path, original)

	origContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if installErr := Install(); installErr != nil {
		t.Fatal(installErr)
	}

	backupContent, err := os.ReadFile(path + ".vv.bak")
	if err != nil {
		t.Fatal("backup file should exist")
	}

	if string(origContent) != string(backupContent) {
		t.Error("backup content should match original file")
	}
}

func TestInstall_MalformedJSON(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{invalid json}"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Install()
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parsing, got: %v", err)
	}

	// File should not be modified
	content, _ := os.ReadFile(path)
	if string(content) != "{invalid json}" {
		t.Error("malformed JSON file should not be modified")
	}

	// No backup should be created
	if _, err := os.Stat(path + ".vv.bak"); !os.IsNotExist(err) {
		t.Error("no backup should be created for malformed JSON")
	}
}

func TestUninstall_RemovesHooks(t *testing.T) {
	home := setupHome(t)

	if err := Install(); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if hasEvent(settings, "SessionEnd") {
		t.Error("SessionEnd hook should be removed")
	}
	if hasEvent(settings, "Stop") {
		t.Error("Stop hook should be removed")
	}
}

func TestUninstall_PreservesOtherHooks(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)

	writeJSON(t, path, map[string]any{
		"hooks": map[string]any{
			"SessionEnd": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "other-tool"},
					},
				},
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
			},
			"Stop": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
			},
		},
	})

	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	hooks := settings["hooks"].(map[string]any)

	// SessionEnd should still exist with the other-tool entry
	sessionEnd, ok := hooks["SessionEnd"].([]any)
	if !ok || len(sessionEnd) != 1 {
		t.Errorf("expected 1 SessionEnd entry (other-tool), got %v", hooks["SessionEnd"])
	}

	// Stop should be cleaned up (was only vv hook)
	if _, ok := hooks["Stop"]; ok {
		t.Error("empty Stop array should be removed")
	}
}

func TestUninstall_NotInstalled(t *testing.T) {
	setupHome(t)

	// No settings file — should print info and return nil
	err := Uninstall()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestUninstall_CleansEmptyHooksMap(t *testing.T) {
	home := setupHome(t)

	// Install then uninstall — hooks map should be completely gone
	if err := Install(); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if _, ok := settings["hooks"]; ok {
		t.Error("empty hooks map should be removed entirely")
	}
}

// --- MCP install/uninstall tests ---

func hasMCP(settings map[string]any) bool {
	servers, ok := settings["mcpServers"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = servers[mcpServerName]
	return ok
}

func TestInstallMCP_NoFile(t *testing.T) {
	home := setupHome(t)

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if !hasMCP(settings) {
		t.Error("missing vibe-vault MCP server entry")
	}
}

func TestInstallMCP_ExistingSettings(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"permissions": map[string]any{"allow": true},
	})

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if !hasMCP(settings) {
		t.Error("missing vibe-vault MCP server entry")
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("existing 'permissions' key was lost")
	}
}

func TestInstallMCP_PreservesExistingServers(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{
			"other-tool": map[string]any{
				"command": "other",
				"args":    []any{"serve"},
			},
		},
	})

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if !hasMCP(settings) {
		t.Error("missing vibe-vault MCP server entry")
	}
	servers := settings["mcpServers"].(map[string]any)
	if _, ok := servers["other-tool"]; !ok {
		t.Error("existing MCP server was lost")
	}
}

func TestInstallMCP_Idempotent(t *testing.T) {
	home := setupHome(t)

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	first, _ := os.ReadFile(path)

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}

	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("idempotent install modified the file")
	}
}

func TestInstallMCP_CreatesBackup(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{"existing": "data"})

	origContent, _ := os.ReadFile(path)

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}

	backupContent, err := os.ReadFile(path + ".vv.bak")
	if err != nil {
		t.Fatal("backup file should exist")
	}
	if string(origContent) != string(backupContent) {
		t.Error("backup content should match original file")
	}
}

func TestUninstallMCP_Removes(t *testing.T) {
	home := setupHome(t)

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}
	if err := UninstallMCP(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if hasMCP(settings) {
		t.Error("vibe-vault MCP server should be removed")
	}
}

func TestUninstallMCP_PreservesOtherServers(t *testing.T) {
	home := setupHome(t)
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{
			"vibe-vault": map[string]any{
				"command": "vv",
				"args":    []any{"mcp"},
			},
			"other-tool": map[string]any{
				"command": "other",
			},
		},
	})

	if err := UninstallMCP(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	servers := settings["mcpServers"].(map[string]any)
	if _, ok := servers["vibe-vault"]; ok {
		t.Error("vibe-vault should be removed")
	}
	if _, ok := servers["other-tool"]; !ok {
		t.Error("other-tool should be preserved")
	}
}

func TestUninstallMCP_CleansEmptyMap(t *testing.T) {
	home := setupHome(t)

	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}
	if err := UninstallMCP(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if _, ok := settings["mcpServers"]; ok {
		t.Error("empty mcpServers map should be removed entirely")
	}
}

func TestUninstallMCP_NotInstalled(t *testing.T) {
	setupHome(t)

	err := UninstallMCP()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestInstallMCP_WithHooks(t *testing.T) {
	home := setupHome(t)

	// Install hooks first, then MCP — both should coexist
	if err := Install(); err != nil {
		t.Fatal(err)
	}
	if err := InstallMCP(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if !hasMCP(settings) {
		t.Error("missing vibe-vault MCP server entry")
	}
	if !hasEvent(settings, "SessionEnd") {
		t.Error("hook should still be present")
	}
}

// --- Zed MCP install/uninstall tests ---

func zedSettingsPath(home string) string {
	return filepath.Join(home, ".config", "zed", "settings.json")
}

func hasMCPZed(settings map[string]any) bool {
	servers, ok := settings["context_servers"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = servers[mcpServerName]
	return ok
}

func TestInstallMCPZed_NoFile(t *testing.T) {
	home := setupHome(t)

	if err := InstallMCPZed(); err != nil {
		t.Fatal(err)
	}

	path := zedSettingsPath(home)
	settings := readJSON(t, path)
	if !hasMCPZed(settings) {
		t.Error("missing vibe-vault Zed context_servers entry")
	}

	// Verify structure: context_servers.vibe-vault.command = "vv"
	servers := settings["context_servers"].(map[string]any)
	vv := servers["vibe-vault"].(map[string]any)
	if vv["command"] != "vv" {
		t.Errorf("expected command = vv, got %v", vv["command"])
	}
}

func TestInstallMCPZed_Existing(t *testing.T) {
	home := setupHome(t)
	path := zedSettingsPath(home)
	writeJSON(t, path, map[string]any{
		"theme": "One Dark",
		"context_servers": map[string]any{
			"other-tool": map[string]any{
				"command": map[string]any{"path": "other", "args": []any{"serve"}},
			},
		},
	})

	if err := InstallMCPZed(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if !hasMCPZed(settings) {
		t.Error("missing vibe-vault Zed entry")
	}
	// Existing settings preserved
	if settings["theme"] != "One Dark" {
		t.Error("existing theme setting was lost")
	}
	servers := settings["context_servers"].(map[string]any)
	if _, ok := servers["other-tool"]; !ok {
		t.Error("existing context server was lost")
	}
}

func TestInstallMCPZed_Idempotent(t *testing.T) {
	home := setupHome(t)

	if err := InstallMCPZed(); err != nil {
		t.Fatal(err)
	}

	path := zedSettingsPath(home)
	first, _ := os.ReadFile(path)

	if err := InstallMCPZed(); err != nil {
		t.Fatal(err)
	}

	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("idempotent install modified the file")
	}
}

func TestUninstallMCPZed(t *testing.T) {
	home := setupHome(t)

	if err := InstallMCPZed(); err != nil {
		t.Fatal(err)
	}
	if err := UninstallMCPZed(); err != nil {
		t.Fatal(err)
	}

	path := zedSettingsPath(home)
	settings := readJSON(t, path)
	if hasMCPZed(settings) {
		t.Error("vibe-vault Zed entry should be removed")
	}
	// Empty context_servers map should be cleaned up
	if _, ok := settings["context_servers"]; ok {
		t.Error("empty context_servers map should be removed entirely")
	}
}

func TestUninstallMCPZed_PreservesOtherServers(t *testing.T) {
	home := setupHome(t)
	path := zedSettingsPath(home)
	writeJSON(t, path, map[string]any{
		"context_servers": map[string]any{
			"vibe-vault": map[string]any{
				"command": "vv",
				"args":    []any{"mcp"},
			},
			"other-tool": map[string]any{
				"command": "other",
			},
		},
	})

	if err := UninstallMCPZed(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	servers := settings["context_servers"].(map[string]any)
	if _, ok := servers["vibe-vault"]; ok {
		t.Error("vibe-vault should be removed")
	}
	if _, ok := servers["other-tool"]; !ok {
		t.Error("other-tool should be preserved")
	}
}

func TestUninstallMCPZed_NotInstalled(t *testing.T) {
	setupHome(t)

	err := UninstallMCPZed()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

// --- Unified MCP install/uninstall tests ---

func TestInstallMCPAll_BothDetected(t *testing.T) {
	home := setupHome(t)
	// Create both editor directories
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".config", "zed"), 0o755)

	if err := InstallMCPAll(false, false); err != nil {
		t.Fatal(err)
	}

	claudePath := settingsPath(home)
	cs := readJSON(t, claudePath)
	if !hasMCP(cs) {
		t.Error("Claude Code MCP server not installed")
	}

	zedPath := zedSettingsPath(home)
	zs := readJSON(t, zedPath)
	if !hasMCPZed(zs) {
		t.Error("Zed MCP server not installed")
	}
}

func TestInstallMCPAll_OnlyClaude(t *testing.T) {
	home := setupHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	// No Zed directory

	if err := InstallMCPAll(false, false); err != nil {
		t.Fatal(err)
	}

	claudePath := settingsPath(home)
	cs := readJSON(t, claudePath)
	if !hasMCP(cs) {
		t.Error("Claude Code MCP server not installed")
	}

	zedPath := zedSettingsPath(home)
	if _, err := os.Stat(zedPath); !os.IsNotExist(err) {
		t.Error("Zed settings file should not exist")
	}
}

func TestInstallMCPAll_OnlyZed(t *testing.T) {
	home := setupHome(t)
	os.MkdirAll(filepath.Join(home, ".config", "zed"), 0o755)
	// No Claude directory

	if err := InstallMCPAll(false, false); err != nil {
		t.Fatal(err)
	}

	claudePath := settingsPath(home)
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Error("Claude settings file should not exist")
	}

	zedPath := zedSettingsPath(home)
	zs := readJSON(t, zedPath)
	if !hasMCPZed(zs) {
		t.Error("Zed MCP server not installed")
	}
}

func TestInstallMCPAll_NeitherDetected(t *testing.T) {
	setupHome(t)
	// No editor directories

	err := InstallMCPAll(false, false)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestInstallMCPAll_ClaudeOnlyFlag(t *testing.T) {
	home := setupHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".config", "zed"), 0o755)

	if err := InstallMCPAll(true, false); err != nil {
		t.Fatal(err)
	}

	claudePath := settingsPath(home)
	cs := readJSON(t, claudePath)
	if !hasMCP(cs) {
		t.Error("Claude Code MCP server not installed")
	}

	zedPath := zedSettingsPath(home)
	if _, err := os.Stat(zedPath); !os.IsNotExist(err) {
		t.Error("Zed should not be installed with --claude-only")
	}
}

func TestInstallMCPAll_ZedOnlyFlag(t *testing.T) {
	home := setupHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".config", "zed"), 0o755)

	if err := InstallMCPAll(false, true); err != nil {
		t.Fatal(err)
	}

	claudePath := settingsPath(home)
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Error("Claude should not be installed with --zed-only")
	}

	zedPath := zedSettingsPath(home)
	zs := readJSON(t, zedPath)
	if !hasMCPZed(zs) {
		t.Error("Zed MCP server not installed")
	}
}

func TestUninstallMCPAll_BothDetected(t *testing.T) {
	home := setupHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".config", "zed"), 0o755)

	// Install both first
	if err := InstallMCPAll(false, false); err != nil {
		t.Fatal(err)
	}
	// Then uninstall both
	if err := UninstallMCPAll(false, false); err != nil {
		t.Fatal(err)
	}

	claudePath := settingsPath(home)
	cs := readJSON(t, claudePath)
	if hasMCP(cs) {
		t.Error("Claude Code MCP server should be removed")
	}

	zedPath := zedSettingsPath(home)
	zs := readJSON(t, zedPath)
	if hasMCPZed(zs) {
		t.Error("Zed MCP server should be removed")
	}
}

func TestInstallMCPAll_Idempotent(t *testing.T) {
	home := setupHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(home, ".config", "zed"), 0o755)

	if err := InstallMCPAll(false, false); err != nil {
		t.Fatal(err)
	}
	// Second install should succeed without error
	if err := InstallMCPAll(false, false); err != nil {
		t.Fatal(err)
	}

	// Verify both still installed
	cs := readJSON(t, settingsPath(home))
	if !hasMCP(cs) {
		t.Error("Claude Code MCP server not installed after idempotent call")
	}
	zs := readJSON(t, zedSettingsPath(home))
	if !hasMCPZed(zs) {
		t.Error("Zed MCP server not installed after idempotent call")
	}
}

// --- JSONC stripping tests ---

func TestStripJSONC_LineComments(t *testing.T) {
	input := []byte(`// header comment
{
  // inline comment
  "key": "value"
}`)
	var m map[string]any
	if err := json.Unmarshal(stripJSONC(input), &m); err != nil {
		t.Fatalf("failed to parse after stripping: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("expected key=value, got %v", m["key"])
	}
}

func TestStripJSONC_BlockComments(t *testing.T) {
	input := []byte(`{
  /* block comment */
  "key": "value"
}`)
	var m map[string]any
	if err := json.Unmarshal(stripJSONC(input), &m); err != nil {
		t.Fatalf("failed to parse after stripping: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("expected key=value, got %v", m["key"])
	}
}

func TestStripJSONC_TrailingCommas(t *testing.T) {
	input := []byte(`{
  "a": 1,
  "b": [1, 2, 3,],
}`)
	var m map[string]any
	if err := json.Unmarshal(stripJSONC(input), &m); err != nil {
		t.Fatalf("failed to parse after stripping: %v", err)
	}
	if m["a"] != float64(1) {
		t.Errorf("expected a=1, got %v", m["a"])
	}
}

func TestStripJSONC_CommentsInsideStrings(t *testing.T) {
	input := []byte(`{
  "url": "https://example.com/path",
  "note": "this has // slashes and /* stars */"
}`)
	var m map[string]any
	if err := json.Unmarshal(stripJSONC(input), &m); err != nil {
		t.Fatalf("failed to parse after stripping: %v", err)
	}
	if m["url"] != "https://example.com/path" {
		t.Errorf("URL was corrupted: %v", m["url"])
	}
	if m["note"] != "this has // slashes and /* stars */" {
		t.Errorf("string content was corrupted: %v", m["note"])
	}
}

func TestStripJSONC_ZedStyleSettings(t *testing.T) {
	// Realistic Zed settings.json with comments before the object
	input := []byte(`// Zed settings
//
// For information on how to configure Zed, see the Zed
// documentation: https://zed.dev/docs/configuring-zed
{
  "theme": "One Dark",
  "terminal": {
    "dock": "right",
  },
}`)
	var m map[string]any
	if err := json.Unmarshal(stripJSONC(input), &m); err != nil {
		t.Fatalf("failed to parse Zed-style settings: %v", err)
	}
	if m["theme"] != "One Dark" {
		t.Errorf("expected theme=One Dark, got %v", m["theme"])
	}
}

func TestStripJSONC_StrictJSON(t *testing.T) {
	// Standard JSON (no comments, no trailing commas) passes through unchanged.
	input := []byte(`{"key": "value", "num": 42}`)
	var m map[string]any
	if err := json.Unmarshal(stripJSONC(input), &m); err != nil {
		t.Fatalf("strict JSON broke after stripping: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("expected key=value, got %v", m["key"])
	}
}

func TestInstallMCPZed_WithJSONC(t *testing.T) {
	home := setupHome(t)
	path := zedSettingsPath(home)

	// Write a JSONC file (with comments and trailing commas) like Zed generates.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonc := []byte(`// Zed settings
{
  "theme": "One Dark",
  "terminal": {
    "dock": "right",
  },
}`)
	if err := os.WriteFile(path, jsonc, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := InstallMCPZed(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if !hasMCPZed(settings) {
		t.Error("missing vibe-vault Zed entry")
	}
	if settings["theme"] != "One Dark" {
		t.Error("existing theme was lost")
	}
}

// --- Claude Plugin install/uninstall tests ---

func hasPluginMarketplace(settings map[string]any) bool {
	return isPluginMarketplaceInstalled(settings)
}

func hasPluginEnabled(settings map[string]any) bool {
	return isPluginEnabled(settings)
}

func TestInstallClaudePlugin_Fresh(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	// Create ~/.claude/ so claudeDetected() passes.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if !hasPluginMarketplace(settings) {
		t.Error("missing extraKnownMarketplaces entry")
	}
	if !hasPluginEnabled(settings) {
		t.Error("missing enabledPlugins entry")
	}

	// Verify cache directory was created.
	cacheDir := filepath.Join(home, ".claude", "plugins", "cache", "vibe-vault-local", "vibe-vault")
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Errorf("expected cache directory to exist: %v", err)
	} else if len(entries) == 0 {
		t.Error("expected at least one version directory in cache")
	}

	// Verify installed_plugins.json has our entry.
	ipPath := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	ipData := readJSON(t, ipPath)
	if _, ok := ipData["vibe-vault@vibe-vault-local"]; !ok {
		t.Error("installed_plugins.json missing our entry")
	}
}

func TestInstallClaudePlugin_ExistingSettings(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"theme": "dark",
		"hooks": map[string]any{},
	})

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	if settings["theme"] != "dark" {
		t.Error("existing settings were lost")
	}
	if !hasPluginMarketplace(settings) {
		t.Error("missing extraKnownMarketplaces entry")
	}
}

func TestInstallClaudePlugin_Idempotent(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}
	// Second call should succeed silently.
	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallClaudePlugin_PreservesMcpServers(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"mcpServers": map[string]any{
			"vibe-vault": map[string]any{
				"command": "vv",
				"args":    []any{"mcp"},
			},
		},
	})

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	// Per review: mcpServers should NOT be auto-removed.
	if !isMCPInstalled(settings) {
		t.Error("mcpServers entry was removed — should be preserved")
	}
	if !hasPluginMarketplace(settings) {
		t.Error("missing extraKnownMarketplaces entry")
	}
}

func TestInstallClaudePlugin_PreservesExistingMarketplaces(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"extraKnownMarketplaces": map[string]any{
			"other-marketplace": map[string]any{
				"source": map[string]any{"source": "directory", "path": "/tmp/other"},
			},
		},
	})

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	mkts := settings["extraKnownMarketplaces"].(map[string]any)
	if _, ok := mkts["other-marketplace"]; !ok {
		t.Error("existing marketplace was removed")
	}
	if !hasPluginMarketplace(settings) {
		t.Error("vibe-vault marketplace not added")
	}
}

func TestInstallClaudePlugin_PreservesExistingPlugins(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"enabledPlugins": map[string]any{
			"playwright@claude-plugins-official": true,
		},
	})

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	plugins := settings["enabledPlugins"].(map[string]any)
	if _, ok := plugins["playwright@claude-plugins-official"]; !ok {
		t.Error("existing plugin was removed")
	}
	if !hasPluginEnabled(settings) {
		t.Error("vibe-vault plugin not enabled")
	}
}

func TestInstallClaudePlugin_CreatesBackup(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{"existing": true})

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".vv.bak"); err != nil {
		t.Errorf("expected backup file, got: %v", err)
	}
}

func TestUninstallClaudePlugin_RemovesAll(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	if err := UninstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if hasPluginMarketplace(settings) {
		t.Error("extraKnownMarketplaces entry should be removed")
	}
	if hasPluginEnabled(settings) {
		t.Error("enabledPlugins entry should be removed")
	}

	// Verify cache directory was cleaned up.
	cacheDir := filepath.Join(home, ".claude", "plugins", "cache", "vibe-vault-local")
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Error("cache directory should be removed after uninstall")
	}

	// Verify installed_plugins.json entry removed.
	ipPath := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	if _, err := os.Stat(ipPath); err == nil {
		ipData := readJSON(t, ipPath)
		if _, ok := ipData["vibe-vault@vibe-vault-local"]; ok {
			t.Error("installed_plugins.json should not have our entry after uninstall")
		}
	}
}

func TestInstallClaudePlugin_CacheFailureIsNonFatal(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Make ~/.claude/plugins/ unwritable so cache install fails.
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(pluginsDir, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(pluginsDir, 0o755) // cleanup

	// Install should still succeed (settings.json written, cache is soft failure).
	if err := InstallClaudePlugin(); err != nil {
		t.Fatalf("expected install to succeed despite cache failure, got: %v", err)
	}

	// Verify settings.json was written correctly.
	path := settingsPath(home)
	settings := readJSON(t, path)
	if !hasPluginMarketplace(settings) {
		t.Error("missing extraKnownMarketplaces entry")
	}
	if !hasPluginEnabled(settings) {
		t.Error("missing enabledPlugins entry")
	}
}

func TestUninstallClaudePlugin_PreservesOtherEntries(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	path := settingsPath(home)
	writeJSON(t, path, map[string]any{
		"extraKnownMarketplaces": map[string]any{
			"other-marketplace": map[string]any{"source": "test"},
			"vibe-vault-local":  map[string]any{"source": "test"},
		},
		"enabledPlugins": map[string]any{
			"playwright@claude-plugins-official": true,
			"vibe-vault@vibe-vault-local":        true,
		},
	})

	if err := UninstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	settings := readJSON(t, path)
	mkts := settings["extraKnownMarketplaces"].(map[string]any)
	if _, ok := mkts["other-marketplace"]; !ok {
		t.Error("other marketplace was removed")
	}
	plugins := settings["enabledPlugins"].(map[string]any)
	if _, ok := plugins["playwright@claude-plugins-official"]; !ok {
		t.Error("other plugin was removed")
	}
	if hasPluginMarketplace(settings) {
		t.Error("vibe-vault marketplace should be removed")
	}
	if hasPluginEnabled(settings) {
		t.Error("vibe-vault plugin should be removed")
	}
}

func TestUninstallClaudePlugin_NotInstalled(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := UninstallClaudePlugin(); err != nil {
		t.Error(err)
	}
}

func TestUninstallClaudePlugin_CleansEmptyMaps(t *testing.T) {
	home := setupHome(t)
	t.Setenv("XDG_DATA_HOME", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := InstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}
	if err := UninstallClaudePlugin(); err != nil {
		t.Fatal(err)
	}

	path := settingsPath(home)
	settings := readJSON(t, path)
	if _, ok := settings["extraKnownMarketplaces"]; ok {
		t.Error("empty extraKnownMarketplaces should be removed")
	}
	if _, ok := settings["enabledPlugins"]; ok {
		t.Error("empty enabledPlugins should be removed")
	}
}
