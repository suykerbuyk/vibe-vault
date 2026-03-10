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
