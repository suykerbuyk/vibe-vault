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

	path, err := WriteDefault("/home/user/obsidian/my-vault")
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
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
}

func TestWriteDefault_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir := filepath.Join(dir, "vibe-vault")
	os.MkdirAll(configDir, 0o755)

	existing := filepath.Join(configDir, "config.toml")
	os.WriteFile(existing, []byte("vault_path = \"~/custom\"\n"), 0o644)

	path, err := WriteDefault("/some/other/path")
	if err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}

	if path != existing {
		t.Errorf("path = %q, want %q", path, existing)
	}

	data, _ := os.ReadFile(existing)
	if !strings.Contains(string(data), "~/custom") {
		t.Error("existing config was overwritten")
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
