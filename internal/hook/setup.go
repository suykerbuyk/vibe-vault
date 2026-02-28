package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/johns/vibe-vault/internal/config"
)

const hookCommand = "vv hook"

// hookEvents are the Claude Code events vv registers for.
var hookEvents = []string{"SessionEnd", "Stop"}

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
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse %s: %w", config.CompressHome(path), err)
	}
	return settings, nil
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
