package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/config"
)

func testConfig(vaultPath string) config.Config {
	return config.Config{VaultPath: vaultPath}
}

// --- Init tests ---

func TestInit_CreatesVaultFiles(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	result, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if result.Project != "myproject" {
		t.Errorf("project = %q, want %q", result.Project, "myproject")
	}

	// Vault-side files
	for _, rel := range []string{
		"Projects/myproject/resume.md",
		"Projects/myproject/iterations.md",
	} {
		path := filepath.Join(vault, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("vault file not created: %s", rel)
		}
	}

	// Vault-side dirs
	for _, rel := range []string{
		"Projects/myproject/tasks",
		"Projects/myproject/tasks/done",
	} {
		path := filepath.Join(vault, rel)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("vault dir not created: %s", rel)
		} else if !info.IsDir() {
			t.Errorf("expected %s to be a directory", rel)
		}
	}
}

func TestInit_CreatesRepoFiles(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, rel := range []string{
		"CLAUDE.md",
		".claude/commands/restart.md",
		".claude/commands/wrap.md",
	} {
		path := filepath.Join(cwd, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("repo file not created: %s", rel)
		}
	}
}

func TestInit_IdempotentSkip(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// First init
	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init 1: %v", err)
	}

	// Write sentinel to CLAUDE.md
	claudePath := filepath.Join(cwd, "CLAUDE.md")
	os.WriteFile(claudePath, []byte("custom content"), 0o644)

	// Second init without force
	result, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init 2: %v", err)
	}

	// Should have SKIP actions
	skipCount := 0
	for _, a := range result.Actions {
		if a.Action == "SKIP" {
			skipCount++
		}
	}
	if skipCount == 0 {
		t.Error("expected SKIP actions on second init, got none")
	}

	// CLAUDE.md should keep custom content
	data, _ := os.ReadFile(claudePath)
	if string(data) != "custom content" {
		t.Error("CLAUDE.md was overwritten without --force")
	}
}

func TestInit_ForceOverwrite(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// First init
	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init 1: %v", err)
	}

	// Write sentinel
	claudePath := filepath.Join(cwd, "CLAUDE.md")
	os.WriteFile(claudePath, []byte("custom content"), 0o644)

	// Force init
	result, err := Init(cfg, cwd, Opts{Project: "myproject", Force: true})
	if err != nil {
		t.Fatalf("Init force: %v", err)
	}

	// All actions should be CREATE
	for _, a := range result.Actions {
		if a.Action != "CREATE" && a.Action != "UPDATE" {
			t.Errorf("expected CREATE or UPDATE, got %s for %s", a.Action, a.Path)
		}
	}

	// CLAUDE.md should be overwritten
	data, _ := os.ReadFile(claudePath)
	if string(data) == "custom content" {
		t.Error("CLAUDE.md was not overwritten with --force")
	}
}

func TestInit_ProjectOverride(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	result, err := Init(cfg, cwd, Opts{Project: "custom-name"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if result.Project != "custom-name" {
		t.Errorf("project = %q, want %q", result.Project, "custom-name")
	}

	// Files should be under custom-name
	path := filepath.Join(vault, "Projects", "custom-name", "resume.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("resume.md not created under custom-name project")
	}
}

func TestInit_VaultNotFound(t *testing.T) {
	cwd := t.TempDir()
	cfg := testConfig("/nonexistent/vault/path")

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err == nil {
		t.Fatal("expected error for nonexistent vault")
	}
	if !strings.Contains(err.Error(), "vault not found") {
		t.Errorf("error = %q, want vault not found", err)
	}
}

func TestInit_ClaudeMDContent(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "resume.md") {
		t.Error("CLAUDE.md missing resume.md reference")
	}
	if !strings.Contains(content, "myproject") {
		t.Error("CLAUDE.md missing project name")
	}
	if !strings.Contains(content, "Never commit") {
		t.Error("CLAUDE.md missing workflow rules")
	}
}

func TestInit_GitignoreUpdated(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create existing .gitignore
	giPath := filepath.Join(cwd, ".gitignore")
	os.WriteFile(giPath, []byte("node_modules/\n"), 0o644)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, _ := os.ReadFile(giPath)
	content := string(data)

	if !strings.Contains(content, "CLAUDE.md") {
		t.Error(".gitignore missing CLAUDE.md entry")
	}
	if !strings.Contains(content, "commit.msg") {
		t.Error(".gitignore missing commit.msg entry")
	}
	if !strings.Contains(content, "node_modules/") {
		t.Error(".gitignore lost existing content")
	}
}

func TestInit_GitignoreIdempotent(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	giPath := filepath.Join(cwd, ".gitignore")
	os.WriteFile(giPath, []byte("CLAUDE.md\ncommit.msg\n"), 0o644)

	result, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Should not have gitignore UPDATE action
	for _, a := range result.Actions {
		if a.Path == ".gitignore" && a.Action == "UPDATE" {
			t.Error(".gitignore should not be updated when entries already present")
		}
	}
}

func TestInit_ProjectDetection(t *testing.T) {
	vault := t.TempDir()
	// Use a named dir so DetectProject returns the name
	cwd := filepath.Join(t.TempDir(), "my-detected-project")
	os.MkdirAll(cwd, 0o755)
	cfg := testConfig(vault)

	result, err := Init(cfg, cwd, Opts{})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if result.Project != "my-detected-project" {
		t.Errorf("detected project = %q, want %q", result.Project, "my-detected-project")
	}
}

// --- Migrate tests ---

func TestMigrate_CopiesResume(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create local RESUME.md
	os.WriteFile(filepath.Join(cwd, "RESUME.md"), []byte("# My Resume\nProject state."), 0o644)

	result, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if result.Project != "myproject" {
		t.Errorf("project = %q, want %q", result.Project, "myproject")
	}

	// Vault file should exist with same content
	data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "resume.md"))
	if err != nil {
		t.Fatalf("read vault resume.md: %v", err)
	}
	if string(data) != "# My Resume\nProject state." {
		t.Errorf("vault resume.md content = %q", string(data))
	}

	// Original preserved
	if _, err := os.Stat(filepath.Join(cwd, "RESUME.md")); os.IsNotExist(err) {
		t.Error("local RESUME.md was deleted")
	}
}

func TestMigrate_CopiesHistory(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	os.WriteFile(filepath.Join(cwd, "HISTORY.md"), []byte("# History"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "iterations.md"))
	if err != nil {
		t.Fatalf("read vault iterations.md: %v", err)
	}
	if string(data) != "# History" {
		t.Errorf("vault iterations.md content = %q", string(data))
	}
}

func TestMigrate_CopiesTasks(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create tasks/ dir with files
	tasksDir := filepath.Join(cwd, "tasks")
	os.MkdirAll(filepath.Join(tasksDir, "done"), 0o755)
	os.WriteFile(filepath.Join(tasksDir, "001-feature.md"), []byte("task 1"), 0o644)
	os.WriteFile(filepath.Join(tasksDir, "done", "000-setup.md"), []byte("done task"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Check vault files
	data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "tasks", "001-feature.md"))
	if err != nil {
		t.Fatalf("read vault task: %v", err)
	}
	if string(data) != "task 1" {
		t.Errorf("vault task content = %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(vault, "Projects", "myproject", "tasks", "done", "000-setup.md"))
	if err != nil {
		t.Fatalf("read vault done task: %v", err)
	}
	if string(data) != "done task" {
		t.Errorf("vault done task content = %q", string(data))
	}
}

func TestMigrate_SkipsMissing(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// No local files exist
	result, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	skipCount := 0
	for _, a := range result.Actions {
		if a.Action == "SKIP" {
			skipCount++
		}
	}
	// Should skip RESUME.md, HISTORY.md, tasks/
	if skipCount < 3 {
		t.Errorf("expected at least 3 SKIP actions, got %d", skipCount)
	}
}

func TestMigrate_SkipsExistingVaultFiles(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create local and vault files
	os.WriteFile(filepath.Join(cwd, "RESUME.md"), []byte("local resume"), 0o644)
	vaultResume := filepath.Join(vault, "Projects", "myproject", "resume.md")
	os.MkdirAll(filepath.Dir(vaultResume), 0o755)
	os.WriteFile(vaultResume, []byte("vault resume"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Vault file should keep original content
	data, _ := os.ReadFile(vaultResume)
	if string(data) != "vault resume" {
		t.Errorf("vault resume was overwritten without --force: %q", string(data))
	}
}

func TestMigrate_ForceOverwrite(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	os.WriteFile(filepath.Join(cwd, "RESUME.md"), []byte("local resume"), 0o644)
	vaultResume := filepath.Join(vault, "Projects", "myproject", "resume.md")
	os.MkdirAll(filepath.Dir(vaultResume), 0o755)
	os.WriteFile(vaultResume, []byte("vault resume"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject", Force: true})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	data, _ := os.ReadFile(vaultResume)
	if string(data) != "local resume" {
		t.Errorf("vault resume not overwritten with --force: %q", string(data))
	}
}

func TestMigrate_UpdatesRepoFiles(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Write old-style CLAUDE.md
	os.WriteFile(filepath.Join(cwd, "CLAUDE.md"), []byte("old content"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// CLAUDE.md should be force-updated to vault pointer
	data, _ := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	content := string(data)
	if !strings.Contains(content, "resume.md") {
		t.Error("CLAUDE.md not updated to vault pointer")
	}
	if strings.Contains(content, "old content") {
		t.Error("CLAUDE.md still has old content")
	}
}

func TestMigrate_PreservesOriginals(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	os.WriteFile(filepath.Join(cwd, "RESUME.md"), []byte("local"), 0o644)
	os.WriteFile(filepath.Join(cwd, "HISTORY.md"), []byte("local"), 0o644)
	tasksDir := filepath.Join(cwd, "tasks")
	os.MkdirAll(tasksDir, 0o755)
	os.WriteFile(filepath.Join(tasksDir, "task.md"), []byte("local"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// All originals should still exist
	for _, path := range []string{
		filepath.Join(cwd, "RESUME.md"),
		filepath.Join(cwd, "HISTORY.md"),
		filepath.Join(cwd, "tasks", "task.md"),
	} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("original deleted: %s", path)
		}
	}
}
