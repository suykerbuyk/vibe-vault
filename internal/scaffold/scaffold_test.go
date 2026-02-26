package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit_CreatesVault(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "my-vault")

	if err := Init(target, Options{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Verify key files exist.
	for _, rel := range []string{
		"README.md",
		"CLAUDE.md",
		"LICENSE",
		".gitignore",
		".pii-patterns",
		".obsidian/app.json",
		".obsidian/appearance.json",
		"Sessions/.gitkeep",
		"Knowledge/decisions/.gitkeep",
		"Dashboards/sessions.md",
		"Templates/session-summary.md",
		"scripts/install-hooks.sh",
		".githooks/pre-push",
		".github/workflows/vault-health.yml",
		".claude/commands/distill.md",
		".claude/skills/README.md",
		"docs/architecture.md",
		"docs/examples/session-note-enriched.md",
	} {
		path := filepath.Join(target, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", rel)
		}
	}
}

func TestInit_RefusesExistingObsidian(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing")
	os.MkdirAll(filepath.Join(target, ".obsidian"), 0o755)

	err := Init(target, Options{})
	if err == nil {
		t.Fatal("expected error for existing .obsidian/")
	}
	if want := "already contains .obsidian/"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want substring %q", err, want)
	}
}

func TestInit_RefusesExistingVibeVault(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing")
	os.MkdirAll(filepath.Join(target, ".vibe-vault"), 0o755)

	err := Init(target, Options{})
	if err == nil {
		t.Fatal("expected error for existing .vibe-vault/")
	}
	if want := "already contains .vibe-vault/"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want substring %q", err, want)
	}
}

func TestInit_VaultNameReplacement(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "test-vault")

	if err := Init(target, Options{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(target, "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}

	if want := "# test-vault"; !contains(string(data), want) {
		t.Errorf("README.md does not contain %q, got:\n%s", want, string(data)[:200])
	}
	if contains(string(data), "{{VAULT_NAME}}") {
		t.Error("README.md still contains {{VAULT_NAME}} placeholder")
	}
}

func TestInit_ExecutablePermissions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "perm-vault")

	if err := Init(target, Options{}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, rel := range []string{
		"scripts/install-hooks.sh",
		"scripts/pii-check.sh",
		".githooks/pre-push",
	} {
		path := filepath.Join(target, rel)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat %s: %v", rel, err)
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s should be executable, got %o", rel, info.Mode().Perm())
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsHelper(s, substr)
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
