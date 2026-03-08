package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSync_LegacyProject(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create a legacy agentctx (no .version)
	agentctxDir := filepath.Join(vault, "Projects", "legacy", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	os.MkdirAll(filepath.Join(agentctxDir, "tasks"), 0o755)
	os.WriteFile(filepath.Join(agentctxDir, "resume.md"), []byte("# Resume"), 0o644)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "legacy"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(result.Projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(result.Projects))
	}

	psr := result.Projects[0]
	if psr.FromVersion != 0 {
		t.Errorf("FromVersion = %d, want 0", psr.FromVersion)
	}
	if psr.ToVersion != LatestSchemaVersion {
		t.Errorf("ToVersion = %d, want %d", psr.ToVersion, LatestSchemaVersion)
	}

	// .version should exist
	vf, err := ReadVersion(agentctxDir)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if vf.SchemaVersion != LatestSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", vf.SchemaVersion, LatestSchemaVersion)
	}

	// agentctx symlink should exist at cwd
	linkPath := filepath.Join(cwd, "agentctx")
	if info, lstatErr := os.Lstat(linkPath); lstatErr != nil {
		t.Errorf("agentctx symlink not created: %v", lstatErr)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Error("agentctx should be a symlink")
	}

	// CLAUDE.md should use relative paths
	data, err := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if strings.Contains(string(data), vault) {
		t.Error("CLAUDE.md contains absolute vault path")
	}
}

func TestSync_AlreadyCurrent(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create a current agentctx
	agentctxDir := filepath.Join(vault, "Projects", "current", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	vf := newVersionFile(LatestSchemaVersion)
	WriteVersion(agentctxDir, vf)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "current"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	psr := result.Projects[0]
	if psr.FromVersion != LatestSchemaVersion {
		t.Errorf("FromVersion = %d, want %d", psr.FromVersion, LatestSchemaVersion)
	}
	if psr.ToVersion != LatestSchemaVersion {
		t.Errorf("ToVersion = %d, want %d", psr.ToVersion, LatestSchemaVersion)
	}
}

func TestSync_PartialMigration(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create a v1 agentctx
	agentctxDir := filepath.Join(vault, "Projects", "partial", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	vf := newVersionFile(1)
	WriteVersion(agentctxDir, vf)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "partial"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	psr := result.Projects[0]
	if psr.FromVersion != 1 {
		t.Errorf("FromVersion = %d, want 1", psr.FromVersion)
	}
	if psr.ToVersion != LatestSchemaVersion {
		t.Errorf("ToVersion = %d, want %d", psr.ToVersion, LatestSchemaVersion)
	}
}

func TestSync_DryRun(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create a legacy agentctx (no .version)
	agentctxDir := filepath.Join(vault, "Projects", "dryrun", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "dryrun", DryRun: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	psr := result.Projects[0]
	if psr.ToVersion != LatestSchemaVersion {
		t.Errorf("ToVersion = %d, want %d", psr.ToVersion, LatestSchemaVersion)
	}

	// No files should be modified
	if _, err := os.Stat(filepath.Join(agentctxDir, ".version")); !os.IsNotExist(err) {
		t.Error(".version should not exist in dry-run mode")
	}

	// Should have DRY-RUN actions
	hasDryRun := false
	for _, a := range psr.Actions {
		if a.Action == "DRY-RUN" {
			hasDryRun = true
		}
	}
	if !hasDryRun {
		t.Error("expected DRY-RUN actions")
	}
}

func TestSync_AllMode(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create two projects with agentctx
	for _, proj := range []string{"proj-a", "proj-b"} {
		dir := filepath.Join(vault, "Projects", proj, "agentctx")
		os.MkdirAll(filepath.Join(dir, "commands"), 0o755)
	}

	result, err := Sync(cfg, cwd, SyncOpts{All: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(result.Projects) != 2 {
		t.Errorf("expected 2 projects, got %d", len(result.Projects))
	}

	// All should be marked repo-skipped
	for _, psr := range result.Projects {
		if !psr.RepoSkipped {
			t.Errorf("project %s should be repo-skipped in --all mode", psr.Project)
		}
	}
}

func TestSync_PropagatesSharedCommands(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create project agentctx at latest version
	agentctxDir := filepath.Join(vault, "Projects", "cmdtest", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	vf := newVersionFile(LatestSchemaVersion)
	WriteVersion(agentctxDir, vf)

	// Create a shared command template
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "shared.md"), []byte("# Shared Command"), 0o644)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "cmdtest"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Shared command should be propagated
	sharedPath := filepath.Join(agentctxDir, "commands", "shared.md")
	if _, err := os.Stat(sharedPath); os.IsNotExist(err) {
		t.Error("shared command not propagated")
	} else {
		data, _ := os.ReadFile(sharedPath)
		if string(data) != "# Shared Command" {
			t.Errorf("shared command content = %q", string(data))
		}
	}

	// Should have CREATE action for shared.md
	found := false
	for _, psr := range result.Projects {
		for _, a := range psr.Actions {
			if a.Path == "commands/shared.md" && a.Action == "CREATE" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected CREATE action for commands/shared.md")
	}
}

func TestSync_ExistingCommandNotOverwritten(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create project agentctx with existing command
	agentctxDir := filepath.Join(vault, "Projects", "nooverwrite", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "shared.md"), []byte("project-specific"), 0o644)
	vf := newVersionFile(LatestSchemaVersion)
	WriteVersion(agentctxDir, vf)

	// Create template with same name
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "shared.md"), []byte("template version"), 0o644)

	_, err := Sync(cfg, cwd, SyncOpts{Project: "nooverwrite"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Project command should be preserved
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "shared.md"))
	if string(data) != "project-specific" {
		t.Errorf("project command was overwritten: %q", string(data))
	}
}

func TestSync_Idempotent(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create a legacy agentctx
	agentctxDir := filepath.Join(vault, "Projects", "idem", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	// First sync
	_, err := Sync(cfg, cwd, SyncOpts{Project: "idem"})
	if err != nil {
		t.Fatalf("Sync 1: %v", err)
	}

	// Second sync — should be a no-op (or close to it)
	result, err := Sync(cfg, cwd, SyncOpts{Project: "idem"})
	if err != nil {
		t.Fatalf("Sync 2: %v", err)
	}

	psr := result.Projects[0]
	if psr.FromVersion != LatestSchemaVersion {
		t.Errorf("FromVersion = %d after second sync, want %d", psr.FromVersion, LatestSchemaVersion)
	}
	if psr.ToVersion != LatestSchemaVersion {
		t.Errorf("ToVersion = %d after second sync, want %d", psr.ToVersion, LatestSchemaVersion)
	}
}

func TestMigrate0to1(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir, 0o755)

	ctx := MigrationContext{AgentctxPath: dir}
	actions, err := migrate0to1(ctx)
	if err != nil {
		t.Fatalf("migrate0to1: %v", err)
	}

	if len(actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(actions))
	}

	vf, err := ReadVersion(dir)
	if err != nil {
		t.Fatalf("ReadVersion: %v", err)
	}
	if vf.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", vf.SchemaVersion)
	}
}

func TestMigrate1to2_CreatesSymlink(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	ctx := MigrationContext{
		AgentctxPath: agentctxDir,
		RepoPath:     cwd,
		Project:      "test",
		VaultPath:    vault,
		Force:        true,
	}
	_, err := migrate1to2(ctx)
	if err != nil {
		t.Fatalf("migrate1to2: %v", err)
	}

	// agentctx symlink
	linkPath := filepath.Join(cwd, "agentctx")
	if info, err := os.Lstat(linkPath); err != nil {
		t.Errorf("agentctx symlink not created: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Error("agentctx should be a symlink")
	}
}

func TestMigrate1to2_RewritesCLAUDEMD(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	// Write old-style CLAUDE.md with absolute path
	os.WriteFile(filepath.Join(cwd, "CLAUDE.md"), []byte("old absolute path: "+vault), 0o644)

	ctx := MigrationContext{
		AgentctxPath: agentctxDir,
		RepoPath:     cwd,
		Project:      "test",
		VaultPath:    vault,
		Force:        true,
	}
	_, err := migrate1to2(ctx)
	if err != nil {
		t.Fatalf("migrate1to2: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	content := string(data)
	if strings.Contains(content, vault) {
		t.Error("CLAUDE.md still contains absolute vault path")
	}
	if !strings.Contains(content, "agentctx/") {
		t.Error("CLAUDE.md missing agentctx reference")
	}
}

func TestMigrate1to2_RelativeCommands(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	ctx := MigrationContext{
		AgentctxPath: agentctxDir,
		RepoPath:     cwd,
		Project:      "test",
		VaultPath:    vault,
		Force:        true,
	}
	_, err := migrate1to2(ctx)
	if err != nil {
		t.Fatalf("migrate1to2: %v", err)
	}

	cmdsPath := filepath.Join(cwd, ".claude", "commands")
	target, err := os.Readlink(cmdsPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != filepath.Join("..", "agentctx", "commands") {
		t.Errorf("commands symlink target = %q, want relative", target)
	}
}

func TestMigrate1to2_VaultOnlySkipsRepo(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	ctx := MigrationContext{
		AgentctxPath: agentctxDir,
		RepoPath:     "", // --all mode
		Project:      "test",
		VaultPath:    vault,
	}
	actions, err := migrate1to2(ctx)
	if err != nil {
		t.Fatalf("migrate1to2: %v", err)
	}

	// Should not have any repo-side actions (agentctx, CLAUDE.md, .claude/ subdirs)
	repoSidePaths := map[string]bool{
		"agentctx": true, "CLAUDE.md": true,
		".claude/commands": true, ".claude/rules": true,
		".claude/skills": true, ".claude/agents": true,
	}
	for _, a := range actions {
		if repoSidePaths[a.Path] {
			t.Errorf("unexpected repo-side action: %s %s", a.Action, a.Path)
		}
	}
}

func TestDiscoverProjects(t *testing.T) {
	vault := t.TempDir()

	// Create projects
	for _, proj := range []string{"alpha", "beta"} {
		os.MkdirAll(filepath.Join(vault, "Projects", proj, "agentctx"), 0o755)
	}
	// Create a project without agentctx (should not be discovered)
	os.MkdirAll(filepath.Join(vault, "Projects", "gamma", "sessions"), 0o755)

	projects := discoverProjects(vault)
	if len(projects) != 2 {
		t.Errorf("expected 2 projects, got %d: %v", len(projects), projects)
	}
}

func TestPropagateSharedCommands(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	// Create template commands
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "new-cmd.md"), []byte("# New"), 0o644)
	os.WriteFile(filepath.Join(tmplCmds, "existing.md"), []byte("# Template"), 0o644)

	// Create existing project command
	os.WriteFile(filepath.Join(agentctxDir, "commands", "existing.md"), []byte("# Project"), 0o644)

	actions := propagateSharedCommands(vault, agentctxDir, false)

	// Only new-cmd should be propagated
	if len(actions) != 1 {
		t.Errorf("expected 1 action, got %d", len(actions))
	}
	if len(actions) > 0 && actions[0].Path != "commands/new-cmd.md" {
		t.Errorf("action path = %q, want commands/new-cmd.md", actions[0].Path)
	}

	// existing.md should keep project content
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "existing.md"))
	if string(data) != "# Project" {
		t.Errorf("existing command was overwritten: %q", string(data))
	}

	// new-cmd.md should exist
	data, _ = os.ReadFile(filepath.Join(agentctxDir, "commands", "new-cmd.md"))
	if string(data) != "# New" {
		t.Errorf("new command content = %q", string(data))
	}
}

// testConfig is defined in context_test.go
