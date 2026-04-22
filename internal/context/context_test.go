package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
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

	// Vault-side files (inside agentctx/)
	for _, rel := range []string{
		"Projects/myproject/agentctx/workflow.md",
		"Projects/myproject/agentctx/resume.md",
		"Projects/myproject/agentctx/iterations.md",
		"Projects/myproject/agentctx/commands/restart.md",
		"Projects/myproject/agentctx/commands/wrap.md",
	} {
		path := filepath.Join(vault, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("vault file not created: %s", rel)
		}
	}

	// Vault-side dirs
	for _, rel := range []string{
		"Projects/myproject/agentctx/tasks",
		"Projects/myproject/agentctx/tasks/done",
		"Projects/myproject/agentctx/commands",
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

	// CLAUDE.md should be a regular file (not symlink)
	claudeInfo, err2 := os.Lstat(filepath.Join(cwd, "CLAUDE.md"))
	if err2 != nil {
		t.Error("repo CLAUDE.md not created")
	} else if claudeInfo.Mode()&os.ModeSymlink != 0 {
		t.Error("CLAUDE.md should be a regular file, not a symlink")
	}

	// Commands should be accessible through the real directory
	for _, name := range []string{"restart.md", "wrap.md"} {
		path := filepath.Join(cwd, ".claude", "commands", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("command not accessible: .claude/commands/%s", name)
		}
	}
}

func TestInit_ClaudeSubdirsRealDirs(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// All .claude/ subdirectories should be real directories (not symlinks)
	for _, sub := range []string{"commands", "rules", "skills", "agents"} {
		link := filepath.Join(cwd, ".claude", sub)
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatalf(".claude/%s not created: %v", sub, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf(".claude/%s should be a real directory, not a symlink", sub)
		}
		if !info.IsDir() {
			t.Fatalf(".claude/%s should be a directory, got mode %v", sub, info.Mode())
		}
	}

	// Commands should be accessible
	for _, name := range []string{"restart.md", "wrap.md"} {
		path := filepath.Join(cwd, ".claude", "commands", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("command %s not accessible through directory", name)
		}
	}
}

func TestInit_WorkflowMD(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "agentctx", "workflow.md"))
	if err != nil {
		t.Fatalf("read agentctx/workflow.md: %v", err)
	}
	content := string(data)

	// Should contain behavioral rules
	if !strings.Contains(content, "Pair Programming") {
		t.Error("agentctx/workflow.md missing pair programming section")
	}
	if !strings.Contains(content, "Plan Mode") {
		t.Error("agentctx/workflow.md missing plan mode section")
	}
	if !strings.Contains(content, "resume.md") {
		t.Error("agentctx/workflow.md missing file references")
	}
	if !strings.Contains(content, "myproject") {
		t.Error("agentctx/workflow.md missing project name")
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

	// Force init
	result, err := Init(cfg, cwd, Opts{Project: "myproject", Force: true})
	if err != nil {
		t.Fatalf("Init force: %v", err)
	}

	// All actions should be CREATE or UPDATE
	for _, a := range result.Actions {
		if a.Action != "CREATE" && a.Action != "UPDATE" {
			t.Errorf("expected CREATE or UPDATE, got %s for %s", a.Action, a.Path)
		}
	}

	// CLAUDE.md should be a regular file (not symlink)
	info, err := os.Lstat(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatal("CLAUDE.md missing after force init")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("CLAUDE.md should be a regular file after force init")
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

	// Files should be under custom-name/agentctx/
	path := filepath.Join(vault, "Projects", "custom-name", "agentctx", "resume.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("resume.md not created under custom-name/agentctx/")
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

	// CLAUDE.md should be a regular file (not symlink)
	info, err := os.Lstat(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("lstat CLAUDE.md: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("CLAUDE.md should be a regular file, not a symlink")
	}

	// Content should be readable and reference MCP bootstrap
	data, err := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "vv_bootstrap_context") {
		t.Error("CLAUDE.md missing vv_bootstrap_context reference")
	}
	// Should NOT contain absolute vault path
	if strings.Contains(content, vault) {
		t.Error("CLAUDE.md contains absolute vault path")
	}
	// Should NOT contain full behavioral rules (those are in agentctx/workflow.md)
	if strings.Contains(content, "Pair Programming") {
		t.Error("CLAUDE.md should be thin pointer, not contain behavioral rules")
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

	// Should not have gitignore UPDATE action when all entries already present
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

	// Vault file should exist in agentctx/ with same content
	data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "agentctx", "resume.md"))
	if err != nil {
		t.Fatalf("read vault agentctx/resume.md: %v", err)
	}
	if string(data) != "# My Resume\nProject state." {
		t.Errorf("vault agentctx/resume.md content = %q", string(data))
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

	data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "agentctx", "iterations.md"))
	if err != nil {
		t.Fatalf("read vault agentctx/iterations.md: %v", err)
	}
	if string(data) != "# History" {
		t.Errorf("vault agentctx/iterations.md content = %q", string(data))
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

	// Check vault files in agentctx/tasks/
	data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "agentctx", "tasks", "001-feature.md"))
	if err != nil {
		t.Fatalf("read vault task: %v", err)
	}
	if string(data) != "task 1" {
		t.Errorf("vault task content = %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(vault, "Projects", "myproject", "agentctx", "tasks", "done", "000-setup.md"))
	if err != nil {
		t.Fatalf("read vault done task: %v", err)
	}
	if string(data) != "done task" {
		t.Errorf("vault done task content = %q", string(data))
	}
}

func TestMigrate_CopiesLocalCommands(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create local .claude/commands/ as a real directory with regular files
	cmdDir := filepath.Join(cwd, ".claude", "commands")
	os.MkdirAll(cmdDir, 0o755)
	os.WriteFile(filepath.Join(cmdDir, "restart.md"), []byte("# Rich restart command"), 0o644)
	os.WriteFile(filepath.Join(cmdDir, "wrap.md"), []byte("# Rich wrap command"), 0o644)
	os.WriteFile(filepath.Join(cmdDir, "custom.md"), []byte("# Custom command"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Local commands should be copied to agentctx/commands/
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"restart.md", "# Rich restart command"},
		{"wrap.md", "# Rich wrap command"},
		{"custom.md", "# Custom command"},
	} {
		data, err := os.ReadFile(filepath.Join(vault, "Projects", "myproject", "agentctx", "commands", tc.name))
		if err != nil {
			t.Errorf("read vault %s: %v", tc.name, err)
			continue
		}
		if string(data) != tc.content {
			t.Errorf("vault %s content = %q, want %q", tc.name, string(data), tc.content)
		}
	}

	// All .claude/ subdirs should be real directories (not symlinks) after migrate
	for _, sub := range []string{"commands", "rules", "skills", "agents"} {
		link := filepath.Join(cwd, ".claude", sub)
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("lstat .claude/%s: %v", sub, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf(".claude/%s should be a real directory after migrate, not a symlink", sub)
		}
		if !info.IsDir() {
			t.Errorf(".claude/%s should be a directory", sub)
		}
	}

	// All commands should be accessible
	cmdsPath := filepath.Join(cwd, ".claude", "commands")
	for _, name := range []string{"restart.md", "wrap.md", "custom.md"} {
		if _, err := os.Stat(filepath.Join(cmdsPath, name)); os.IsNotExist(err) {
			t.Errorf("command %s not accessible in .claude/commands/", name)
		}
	}
}

func TestMigrate_HandlesExistingDirsGracefully(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// First init creates the real directories
	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Now migrate — should handle the existing directories gracefully
	_, err = Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// All .claude/ subdirs should still be real directories
	for _, sub := range []string{"commands", "rules", "skills", "agents"} {
		link := filepath.Join(cwd, ".claude", sub)
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("lstat .claude/%s: %v", sub, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf(".claude/%s should be a real directory", sub)
		}
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
	vaultResume := filepath.Join(vault, "Projects", "myproject", "agentctx", "resume.md")
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
	vaultResume := filepath.Join(vault, "Projects", "myproject", "agentctx", "resume.md")
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

	// Write old-style CLAUDE.md as a regular file
	os.WriteFile(filepath.Join(cwd, "CLAUDE.md"), []byte("old content"), 0o644)

	_, err := Migrate(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// CLAUDE.md should be a regular file (not symlink)
	info, err := os.Lstat(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("lstat CLAUDE.md: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("CLAUDE.md should be a regular file after migrate")
	}

	// Content should be readable and reference MCP bootstrap
	data, _ := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	content := string(data)
	if !strings.Contains(content, "vv_bootstrap_context") {
		t.Error("CLAUDE.md not updated to MCP-first content")
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

// --- Schema v5 tests: no symlinks ---

func TestInit_NoSymlinks(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// No agentctx symlink should exist
	if _, lstatErr := os.Lstat(filepath.Join(cwd, "agentctx")); !os.IsNotExist(lstatErr) {
		t.Error("agentctx symlink should not exist")
	}

	// CLAUDE.md should be a regular file
	info, err := os.Lstat(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("CLAUDE.md should be a regular file")
	}

	// commit.msg should be a regular file
	info, err = os.Lstat(filepath.Join(cwd, "commit.msg"))
	if err != nil {
		t.Fatalf("commit.msg not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("commit.msg should be a regular file")
	}

	// .claude/ subdirs should be real directories
	for _, sub := range []string{"commands", "rules", "skills", "agents"} {
		info, err := os.Lstat(filepath.Join(cwd, ".claude", sub))
		if err != nil {
			t.Fatalf(".claude/%s not created: %v", sub, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf(".claude/%s should be a real directory", sub)
		}
		if !info.IsDir() {
			t.Errorf(".claude/%s should be a directory", sub)
		}
	}
}

func TestInit_GitignoreNoAgentctx(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cwd, ".gitignore"))
	content := string(data)
	if strings.Contains(content, "agentctx") {
		t.Error(".gitignore should NOT contain agentctx entry")
	}
	if !strings.Contains(content, "CLAUDE.md") {
		t.Error(".gitignore should contain CLAUDE.md entry")
	}
	if !strings.Contains(content, "commit.msg") {
		t.Error(".gitignore should contain commit.msg entry")
	}
}

func TestInit_VersionFile(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	_, err := Init(cfg, cwd, Opts{Project: "myproject"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	agentctxPath := filepath.Join(vault, "Projects", "myproject", "agentctx")
	vf, err := ReadVersion(agentctxPath)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if vf.SchemaVersion != LatestSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", vf.SchemaVersion, LatestSchemaVersion)
	}
	if vf.CreatedBy == "" {
		t.Error("CreatedBy is empty")
	}
}
