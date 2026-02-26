package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir returns the vibe-vault config directory path.
// Uses $XDG_CONFIG_HOME/vibe-vault if set, otherwise ~/.config/vibe-vault.
func ConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "vibe-vault")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "vibe-vault")
}

// WriteDefault writes a default config.toml pointing to vaultPath.
// Returns the config file path. Skips if config.toml already exists.
func WriteDefault(vaultPath string) (string, error) {
	dir := ConfigDir()
	path := filepath.Join(dir, "config.toml")

	if _, err := os.Stat(path); err == nil {
		return path, nil // already exists
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	portablePath := CompressHome(vaultPath)

	content := fmt.Sprintf(`vault_path = %q

[domains]
work = "~/work"
personal = "~/personal"
opensource = "~/opensource"

[enrichment]
enabled = false
timeout_seconds = 10
provider = "openai"
model = "grok-3-mini-fast"
api_key_env = "XAI_API_KEY"
base_url = "https://api.x.ai/v1"

[archive]
compress = true
`, portablePath)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}

	return path, nil
}

// CompressHome replaces $HOME prefix with ~/ for portable config values.
func CompressHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~/" + path[len(home)+1:]
	}
	if path == home {
		return "~"
	}
	return path
}
