package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
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
// Returns (configPath, action, error) where action is one of:
//   - "created"   — new config file was written
//   - "updated"   — existing config had a different vault_path, now changed
//   - "unchanged" — existing config already points to vaultPath
func WriteDefault(vaultPath string) (string, string, error) {
	dir := ConfigDir()
	path := filepath.Join(dir, "config.toml")

	if _, err := os.Stat(path); err == nil {
		updated, err := updateVaultPath(path, vaultPath)
		if err != nil {
			return path, "", fmt.Errorf("update vault_path: %w", err)
		}
		if updated {
			return path, "updated", nil
		}
		return path, "unchanged", nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create config dir: %w", err)
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
		return "", "", fmt.Errorf("write config: %w", err)
	}

	return path, "created", nil
}

// updateVaultPath updates the vault_path in an existing config file if it
// differs from newVaultPath. Returns true if the file was modified.
// Preserves all other config content (domains, enrichment, comments, formatting).
func updateVaultPath(configPath, newVaultPath string) (bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, err
	}

	// Parse existing vault_path
	var parsed struct {
		VaultPath string `toml:"vault_path"`
	}
	if _, err := toml.Decode(string(data), &parsed); err != nil {
		return false, fmt.Errorf("parse config: %w", err)
	}

	// Compare using expanded paths (handles ~/x vs /home/user/x)
	existingExpanded := expandHome(parsed.VaultPath)
	newExpanded := expandHome(newVaultPath)
	// Also expand the new path in case it's relative or absolute
	if abs, err := filepath.Abs(newVaultPath); err == nil {
		newExpanded = abs
	}
	if existingExpanded == newExpanded {
		return false, nil
	}

	portablePath := CompressHome(newVaultPath)
	content := string(data)

	// Line-level replacement of vault_path
	re := regexp.MustCompile(`(?m)^vault_path\s*=\s*.*$`)
	if re.MatchString(content) {
		content = re.ReplaceAllString(content, fmt.Sprintf("vault_path = %q", portablePath))
	} else {
		// vault_path key missing — prepend it
		content = fmt.Sprintf("vault_path = %q\n", portablePath) + content
	}

	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		return false, err
	}

	return true, nil
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
