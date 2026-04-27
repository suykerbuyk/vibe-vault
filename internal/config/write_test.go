package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteDefault_CreatesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, action, err := WriteDefault("/home/user/obsidian/my-vault")
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	if action != "created" {
		t.Errorf("action = %q, want %q", action, "created")
	}

	want := filepath.Join(dir, "vibe-vault", "config.toml")
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "vault_path") {
		t.Error("config missing vault_path")
	}
	if !strings.Contains(content, "[domains]") {
		t.Error("config missing [domains] section")
	}
	if !strings.Contains(content, "[enrichment]") {
		t.Error("config missing [enrichment] section")
	}
	if !strings.Contains(content, "[archive]") {
		t.Error("config missing [archive] section")
	}
	if !strings.Contains(content, "[synthesis]") {
		t.Error("config missing [synthesis] section")
	}
}

func TestWriteDefault_UpdatesExistingVaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "vibe-vault")
	os.MkdirAll(configDir, 0o755)

	existing := filepath.Join(configDir, "config.toml")
	os.WriteFile(existing, []byte("vault_path = \"~/custom\"\n\n[domains]\nwork = \"~/mywork\"\n"), 0o644)

	path, action, err := WriteDefault("/some/other/path")
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	if action != "updated" {
		t.Errorf("action = %q, want %q", action, "updated")
	}

	if path != existing {
		t.Errorf("path = %q, want %q", path, existing)
	}

	data, _ := os.ReadFile(existing)
	content := string(data)

	if !strings.Contains(content, "/some/other/path") {
		t.Error("vault_path not updated to new path")
	}
	if strings.Contains(content, "~/custom") {
		t.Error("old vault_path still present")
	}
	if !strings.Contains(content, "[domains]") {
		t.Error("domains section was lost")
	}
	if !strings.Contains(content, "~/mywork") {
		t.Error("custom domain value was lost")
	}
}

func TestWriteDefault_UnchangedExisting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "vibe-vault")
	os.MkdirAll(configDir, 0o755)

	existing := filepath.Join(configDir, "config.toml")
	original := "vault_path = \"/some/path\"\n\n[domains]\nwork = \"~/work\"\n"
	os.WriteFile(existing, []byte(original), 0o644)

	_, action, err := WriteDefault("/some/path")
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	if action != "unchanged" {
		t.Errorf("action = %q, want %q", action, "unchanged")
	}

	data, _ := os.ReadFile(existing)
	if string(data) != original {
		t.Error("file was modified when it should have been unchanged")
	}
}

func TestWriteDefault_PreservesAllSections(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "vibe-vault")
	os.MkdirAll(configDir, 0o755)

	existing := filepath.Join(configDir, "config.toml")
	fullConfig := `vault_path = "~/old-vault"

[domains]
work = "~/company"
personal = "~/home-projects"
opensource = "~/oss"

[enrichment]
enabled = true
timeout_seconds = 30
provider = "openai"
model = "gpt-4"
api_key_env = "OPENAI_API_KEY"
base_url = "https://api.openai.com/v1"

[archive]
compress = false
`
	os.WriteFile(existing, []byte(fullConfig), 0o644)

	_, action, err := WriteDefault("/new/vault/path")
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	if action != "updated" {
		t.Errorf("action = %q, want %q", action, "updated")
	}

	data, _ := os.ReadFile(existing)
	content := string(data)

	// vault_path updated
	if !strings.Contains(content, "/new/vault/path") {
		t.Error("vault_path not updated")
	}
	if strings.Contains(content, "~/old-vault") {
		t.Error("old vault_path still present")
	}

	// All sections preserved
	for _, s := range []string{
		"[domains]",
		`work = "~/company"`,
		`personal = "~/home-projects"`,
		`opensource = "~/oss"`,
		"[enrichment]",
		"enabled = true",
		"timeout_seconds = 30",
		`model = "gpt-4"`,
		`api_key_env = "OPENAI_API_KEY"`,
		"[archive]",
		"compress = false",
	} {
		if !strings.Contains(content, s) {
			t.Errorf("config missing %q after update", s)
		}
	}
}

func TestWriteDefault_MissingVaultPathKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "vibe-vault")
	os.MkdirAll(configDir, 0o755)

	existing := filepath.Join(configDir, "config.toml")
	os.WriteFile(existing, []byte("[domains]\nwork = \"~/work\"\n"), 0o644)

	_, action, err := WriteDefault("/my/vault")
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	if action != "updated" {
		t.Errorf("action = %q, want %q", action, "updated")
	}

	data, _ := os.ReadFile(existing)
	content := string(data)

	if !strings.Contains(content, "/my/vault") {
		t.Error("vault_path not prepended")
	}
	if !strings.Contains(content, "[domains]") {
		t.Error("domains section was lost")
	}
	if !strings.Contains(content, "~/work") {
		t.Error("domain value was lost")
	}
}

// TestWriteDefault_PreservesProviderKeys locks the H3 acceptance criterion:
// re-running `vv init` against an existing config must not clobber an
// operator's stored [providers.<P>].api_key values. WriteDefault on an
// existing config delegates to updateVaultPath, which is key-blind by
// design — but the regression risk is permanent, so we lock the contract
// with a test.
func TestWriteDefault_PreservesProviderKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "vibe-vault")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existing := filepath.Join(configDir, "config.toml")
	pre := `vault_path = "/some/where"

[providers.anthropic]
api_key = "EXISTING_ANTHROPIC_KEY"

[providers.openai]
api_key = "EXISTING_OPENAI_KEY"

[providers.google]
api_key = "EXISTING_GOOGLE_KEY"
`
	if err := os.WriteFile(existing, []byte(pre), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	// Vault path differs so action will be "updated" — exercises the
	// updateVaultPath edit code path, not the unchanged short-circuit.
	if _, _, err := WriteDefault("/new/vault/path"); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	data, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		`api_key = "EXISTING_ANTHROPIC_KEY"`,
		`api_key = "EXISTING_OPENAI_KEY"`,
		`api_key = "EXISTING_GOOGLE_KEY"`,
		`[providers.anthropic]`,
		`[providers.openai]`,
		`[providers.google]`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q after WriteDefault re-run", want)
		}
	}

	// Vault path should still have been updated.
	if !strings.Contains(content, "/new/vault/path") {
		t.Error("vault_path was not updated")
	}
}

func TestCompressHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}

	tests := []struct {
		input string
		want  string
	}{
		{home + "/obsidian/vault", "~/obsidian/vault"},
		{home + "/foo", "~/foo"},
		{"/tmp/other", "/tmp/other"},
		{home, "~"},
	}

	for _, tt := range tests {
		got := CompressHome(tt.input)
		if got != tt.want {
			t.Errorf("CompressHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
