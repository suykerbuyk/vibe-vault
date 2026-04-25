package context

import (
	"bytes"
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

	// agentctx symlink should NOT exist at cwd (removed by v5 migration)
	linkPath := filepath.Join(cwd, "agentctx")
	if _, lstatErr := os.Lstat(linkPath); !os.IsNotExist(lstatErr) {
		t.Error("agentctx symlink should not exist after v5 migration")
	}

	// CLAUDE.md should be a regular file with MCP-first content
	info, err := os.Lstat(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("CLAUDE.md not created: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("CLAUDE.md should be a regular file")
	}
	data, err := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	if strings.Contains(string(data), vault) {
		t.Error("CLAUDE.md contains absolute vault path")
	}
	if !strings.Contains(string(data), "vv_bootstrap_context") {
		t.Error("CLAUDE.md should contain MCP-first content")
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

	// migrate1to2 still creates agentctx symlink (removed later by migrate4to5)
	linkPath := filepath.Join(cwd, "agentctx")
	if info, err := os.Lstat(linkPath); err != nil {
		t.Errorf("agentctx symlink not created: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Error("agentctx should be a symlink after migrate1to2")
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
	if !strings.Contains(content, "vv_bootstrap_context") {
		t.Error("CLAUDE.md missing vv_bootstrap_context reference")
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

	// Create existing project command (no baseline → legacy)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "existing.md"), []byte("# Project"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	// Should have CREATE for new-cmd and CUSTOMIZED for existing (legacy, no baseline)
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d: %v", len(actions), actions)
	}

	createFound, customizedFound := false, false
	for _, a := range actions {
		if a.Path == "commands/new-cmd.md" && a.Action == "CREATE" {
			createFound = true
		}
		if a.Path == "commands/existing.md" && a.Action == "CUSTOMIZED" {
			customizedFound = true
		}
	}
	if !createFound {
		t.Error("expected CREATE action for new-cmd.md")
	}
	if !customizedFound {
		t.Error("expected CUSTOMIZED action for existing.md")
	}

	// existing.md should keep project content (not overwritten)
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "existing.md"))
	if string(data) != "# Project" {
		t.Errorf("existing command was overwritten: %q", string(data))
	}

	// new-cmd.md should exist with .baseline
	data, _ = os.ReadFile(filepath.Join(agentctxDir, "commands", "new-cmd.md"))
	if string(data) != "# New" {
		t.Errorf("new command content = %q", string(data))
	}
	baseline, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "new-cmd.md.baseline"))
	if string(baseline) != "# New" {
		t.Errorf("baseline not written for new-cmd.md")
	}
}

func TestSync_PropagateFromV0(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create a v0 (legacy) project — no .version file
	agentctxDir := filepath.Join(vault, "Projects", "v0prop", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	os.MkdirAll(filepath.Join(agentctxDir, "tasks"), 0o755)
	os.WriteFile(filepath.Join(agentctxDir, "resume.md"), []byte("# Resume"), 0o644)

	// Create a shared command template
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "shared.md"), []byte("# Shared"), 0o644)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "v0prop"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify migrations ran (v0 -> latest)
	psr := result.Projects[0]
	if psr.FromVersion != 0 {
		t.Errorf("FromVersion = %d, want 0", psr.FromVersion)
	}
	if psr.ToVersion != LatestSchemaVersion {
		t.Errorf("ToVersion = %d, want %d", psr.ToVersion, LatestSchemaVersion)
	}

	// Verify file exists on disk
	sharedPath := filepath.Join(agentctxDir, "commands", "shared.md")
	data, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatalf("shared command not on disk: %v", err)
	}
	if string(data) != "# Shared" {
		t.Errorf("shared command content = %q, want %q", string(data), "# Shared")
	}

	// Verify CREATE action reported
	found := false
	for _, a := range psr.Actions {
		if a.Path == "commands/shared.md" && a.Action == "CREATE" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CREATE action for commands/shared.md, got actions: %v", psr.Actions)
	}
}

func TestPropagateSharedCommands_ErrorSurfaced(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root to trigger permission errors")
	}

	vault := t.TempDir()

	// Create a template command
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "cmd.md"), []byte("# Cmd"), 0o644)

	// Create agentctx dir but make it read-only so MkdirAll for commands/ fails
	agentctxDir := filepath.Join(vault, "Projects", "errtest", "agentctx")
	os.MkdirAll(agentctxDir, 0o755)
	os.Chmod(agentctxDir, 0o555)
	t.Cleanup(func() { os.Chmod(agentctxDir, 0o755) })

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	if len(actions) == 0 {
		t.Fatal("expected ERROR action, got empty slice")
	}
	if !strings.HasPrefix(actions[0].Action, "ERROR:") {
		t.Errorf("action = %q, want ERROR: prefix", actions[0].Action)
	}
	if actions[0].Path != "commands/cmd.md" {
		t.Errorf("path = %q, want commands/cmd.md", actions[0].Path)
	}
}

func TestPropagateSharedCommands_CustomizedDetected(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	// Create template and project command with different content (no baseline → legacy)
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Template Version"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# Old Project Version"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	// Should have CUSTOMIZED action (legacy file, no baseline, content differs)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
	if actions[0].Action != "CUSTOMIZED" {
		t.Errorf("action = %q, want CUSTOMIZED", actions[0].Action)
	}

	// Project file should NOT be overwritten
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# Old Project Version" {
		t.Errorf("project file was overwritten: %q", string(data))
	}
}

func TestPropagateSharedCommands_BaselineAutoUpdate(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)

	// Template changed, project matches baseline → auto-update
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Template"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# Old Template"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.baseline"), []byte("# Old Template"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
	if actions[0].Action != "UPDATE" {
		t.Errorf("action = %q, want UPDATE", actions[0].Action)
	}

	// Project file should be updated
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# New Template" {
		t.Errorf("project file = %q, want # New Template", string(data))
	}

	// Baseline should be updated
	baseline, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md.baseline"))
	if string(baseline) != "# New Template" {
		t.Errorf("baseline = %q, want # New Template", string(baseline))
	}
}

func TestPropagateSharedCommands_BaselineConflict(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)

	// Template changed AND user edited → conflict
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Template"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# User Edit"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.baseline"), []byte("# Original"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
	if actions[0].Action != "CONFLICT" {
		t.Errorf("action = %q, want CONFLICT", actions[0].Action)
	}

	// Project file should NOT be overwritten
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# User Edit" {
		t.Errorf("project file was overwritten: %q", string(data))
	}
}

func TestPropagateSharedCommands_BaselineConflictForce(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)

	// Template changed AND user edited, but --force
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Template"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# User Edit"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.baseline"), []byte("# Original"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, true) // force=true

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
	if actions[0].Action != "UPDATE" {
		t.Errorf("action = %q, want UPDATE", actions[0].Action)
	}

	// Project file SHOULD be overwritten
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# New Template" {
		t.Errorf("project file = %q, want # New Template", string(data))
	}
}

func TestPropagateSharedCommands_TemplateUnchanged(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)

	// Template matches baseline — no action even if project is customized
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# Original"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# User Customized"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.baseline"), []byte("# Original"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	if len(actions) != 0 {
		t.Errorf("expected 0 actions (template unchanged), got %d: %v", len(actions), actions)
	}
}

func TestPropagateSharedCommands_IdenticalBackfillsBaseline(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	content := "# Same Content"
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte(content), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte(content), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	// No actions — content is identical (baseline backfilled silently)
	if len(actions) != 0 {
		t.Errorf("expected 0 actions for identical content, got %d: %v", len(actions), actions)
	}

	// .baseline should be backfilled
	baseline, err := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md.baseline"))
	if err != nil {
		t.Fatal("baseline should be backfilled for identical content")
	}
	if string(baseline) != content {
		t.Errorf("baseline = %q, want %q", string(baseline), content)
	}
}

func TestPropagateSharedCommands_PinnedSkipped(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	// Different content but .pinned marker exists
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Version"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# My Custom"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.pinned"), []byte("pinned\n"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	// No actions — pinned file is skipped
	if len(actions) != 0 {
		t.Errorf("expected 0 actions for pinned file, got %d: %v", len(actions), actions)
	}

	// No .pending file
	pendingPath := filepath.Join(agentctxDir, "commands", "wrap.md.pending")
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Error("pending file should not exist for pinned command")
	}
}

func TestPropagateSharedCommands_ForceOverwritesLegacy(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Template"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# Old Template"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, true) // force

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
	if actions[0].Action != "UPDATE" {
		t.Errorf("action = %q, want UPDATE", actions[0].Action)
	}

	// Project file should be overwritten
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# New Template" {
		t.Errorf("project file not overwritten, got %q", string(data))
	}

	// .baseline should be written
	baseline, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md.baseline"))
	if string(baseline) != "# New Template" {
		t.Errorf("baseline not written on force update")
	}
}

func TestPropagateSharedCommands_ForceUpdatePinnedSkipped(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Version"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# My Custom"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.pinned"), []byte("pinned\n"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, true)

	if len(actions) != 0 {
		t.Errorf("expected 0 actions for pinned file, got %d: %v", len(actions), actions)
	}

	// Project file should NOT be overwritten
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# My Custom" {
		t.Errorf("pinned file was overwritten: %q", string(data))
	}
}

func TestPropagateSharedCommands_ForceUpdateCleansStale(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# Latest"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# Old"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.pending"), []byte("# Stale Pending"), 0o644)

	propagateSharedSubdir(vault, agentctxDir, "commands", false, true)

	// Stale .pending should be removed
	if _, err := os.Stat(filepath.Join(agentctxDir, "commands", "wrap.md.pending")); !os.IsNotExist(err) {
		t.Error("stale .pending should be removed during forceUpdate")
	}

	// Project file should have latest content
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# Latest" {
		t.Errorf("project file = %q, want # Latest", string(data))
	}
}

func TestPropagateSharedCommands_ForceUpdateIdenticalSkipped(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	content := "# Same Content"
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte(content), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte(content), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, true)

	if len(actions) != 0 {
		t.Errorf("expected 0 actions for identical content, got %d: %v", len(actions), actions)
	}
}

func TestSyncProject_EnsuresVaultTemplates(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create a project at latest schema — no vault Templates/ dir yet
	agentctxDir := filepath.Join(vault, "Projects", "tmpltest", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	vf := newVersionFile(LatestSchemaVersion)
	WriteVersion(agentctxDir, vf)

	_, err := Sync(cfg, cwd, SyncOpts{Project: "tmpltest"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Vault Templates/agentctx/commands/ should now exist (seeded by EnsureVaultTemplates)
	tmplDir := filepath.Join(vault, "Templates", "agentctx", "commands")
	if _, err := os.Stat(tmplDir); os.IsNotExist(err) {
		t.Error("vault templates not seeded during sync")
	}
}

func TestSync_PropagatesThroughSymlink(t *testing.T) {
	vault := t.TempDir()
	repoDir := t.TempDir()
	cfg := testConfig(vault)

	// Create project agentctx in the vault at latest version
	agentctxDir := filepath.Join(vault, "Projects", "symtest", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	vf := newVersionFile(LatestSchemaVersion)
	WriteVersion(agentctxDir, vf)

	// Create a symlink from the repo to the vault agentctx (mimics real setup)
	symlink := filepath.Join(repoDir, "agentctx")
	if err := os.Symlink(agentctxDir, symlink); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Create a shared command template
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "shared.md"), []byte("# Shared Command"), 0o644)

	result, err := Sync(cfg, repoDir, SyncOpts{Project: "symtest"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify CREATE action reported
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

	// Verify file exists at the vault path (direct)
	vaultFile := filepath.Join(agentctxDir, "commands", "shared.md")
	data, err := os.ReadFile(vaultFile)
	if err != nil {
		t.Fatalf("file not on disk at vault path: %v", err)
	}
	if string(data) != "# Shared Command" {
		t.Errorf("vault content = %q", string(data))
	}

	// Verify file is accessible through the repo symlink
	symlinkFile := filepath.Join(symlink, "commands", "shared.md")
	data, err = os.ReadFile(symlinkFile)
	if err != nil {
		t.Fatalf("file not accessible through symlink: %v", err)
	}
	if string(data) != "# Shared Command" {
		t.Errorf("symlink content = %q", string(data))
	}
}

func TestPropagateSharedCommands_CreateErrorSurfaced(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root to trigger permission errors")
	}

	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	cmdsDir := filepath.Join(agentctxDir, "commands")
	os.MkdirAll(cmdsDir, 0o755)

	// Template has a file that doesn't exist in project → CREATE path
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "newcmd.md"), []byte("# New"), 0o644)

	// Make commands dir read-only so create fails
	os.Chmod(cmdsDir, 0o555)
	t.Cleanup(func() { os.Chmod(cmdsDir, 0o755) })

	actions := propagateSharedSubdir(vault, agentctxDir, "commands", false, false)

	if len(actions) == 0 {
		t.Fatal("expected ERROR action for write failure, got empty slice")
	}
	if !strings.HasPrefix(actions[0].Action, "ERROR:") {
		t.Errorf("action = %q, want ERROR: prefix", actions[0].Action)
	}
}

func TestMigrate4to5(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "restart.md"), []byte("# Restart"), 0o644)

	// Set up v4 layout: symlinks
	os.Symlink(agentctxDir, filepath.Join(cwd, "agentctx"))
	os.Symlink(filepath.Join("agentctx", "CLAUDE.md"), filepath.Join(cwd, "CLAUDE.md"))
	dotClaude := filepath.Join(cwd, ".claude")
	os.MkdirAll(dotClaude, 0o755)
	for _, sub := range claudeSubdirs {
		os.Symlink(filepath.Join("..", "agentctx", sub), filepath.Join(dotClaude, sub))
	}
	os.Symlink(filepath.Join("agentctx", "commit.msg"), filepath.Join(cwd, "commit.msg"))
	os.WriteFile(filepath.Join(agentctxDir, "commit.msg"), []byte("old msg"), 0o644)
	os.WriteFile(filepath.Join(cwd, ".gitignore"), []byte("/CLAUDE.md\n/commit.msg\n/agentctx\n/agentctx/commands\n"), 0o644)

	// Seed vault templates so resolveTemplate works
	EnsureVaultTemplates(vault)

	ctx := MigrationContext{
		AgentctxPath: agentctxDir,
		RepoPath:     cwd,
		Project:      "test",
		VaultPath:    vault,
		Force:        true,
	}
	actions, err := migrate4to5(ctx)
	if err != nil {
		t.Fatalf("migrate4to5: %v", err)
	}
	if len(actions) == 0 {
		t.Fatal("expected actions from migrate4to5")
	}

	// agentctx symlink should be removed
	if _, lstatErr := os.Lstat(filepath.Join(cwd, "agentctx")); !os.IsNotExist(lstatErr) {
		t.Error("agentctx symlink should be removed")
	}

	// CLAUDE.md should be a regular file
	info, lstatErr := os.Lstat(filepath.Join(cwd, "CLAUDE.md"))
	if lstatErr != nil {
		t.Fatalf("CLAUDE.md missing: %v", lstatErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("CLAUDE.md should be a regular file")
	}
	data, _ := os.ReadFile(filepath.Join(cwd, "CLAUDE.md"))
	if !strings.Contains(string(data), "vv_bootstrap_context") {
		t.Error("CLAUDE.md should have MCP-first content")
	}

	// .claude/commands should be a real directory
	cmdsInfo, cmdsErr := os.Lstat(filepath.Join(cwd, ".claude", "commands"))
	if cmdsErr != nil {
		t.Fatalf(".claude/commands missing: %v", cmdsErr)
	}
	if cmdsInfo.Mode()&os.ModeSymlink != 0 {
		t.Error(".claude/commands should be a real directory")
	}
	if !cmdsInfo.IsDir() {
		t.Error(".claude/commands should be a directory")
	}
	// Commands should be deployed
	if _, statErr := os.Stat(filepath.Join(cwd, ".claude", "commands", "restart.md")); os.IsNotExist(statErr) {
		t.Error("restart.md not deployed to .claude/commands/")
	}

	// commit.msg should be a regular file
	cmInfo, err := os.Lstat(filepath.Join(cwd, "commit.msg"))
	if err != nil {
		t.Fatalf("commit.msg missing: %v", err)
	}
	if cmInfo.Mode()&os.ModeSymlink != 0 {
		t.Error("commit.msg should be a regular file")
	}
	cmData, _ := os.ReadFile(filepath.Join(cwd, "commit.msg"))
	if string(cmData) != "old msg" {
		t.Errorf("commit.msg content = %q, want %q", string(cmData), "old msg")
	}

	// .gitignore should not contain /agentctx
	giData, _ := os.ReadFile(filepath.Join(cwd, ".gitignore"))
	if strings.Contains(string(giData), "agentctx") {
		t.Error(".gitignore should not contain agentctx after v5 migration")
	}
	if !strings.Contains(string(giData), "CLAUDE.md") {
		t.Error(".gitignore should still contain CLAUDE.md")
	}
}

func TestMigrate4to5_VaultOnlySkipsRepo(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	ctx := MigrationContext{
		AgentctxPath: agentctxDir,
		RepoPath:     "", // --all mode
		Project:      "test",
		VaultPath:    vault,
	}
	actions, err := migrate4to5(ctx)
	if err != nil {
		t.Fatalf("migrate4to5: %v", err)
	}

	// Should not have any repo-side actions
	for _, a := range actions {
		if a.Location == "repo" {
			t.Errorf("unexpected repo-side action: %s %s", a.Action, a.Path)
		}
	}

	// Should have vault-side template updates
	if len(actions) == 0 {
		t.Error("expected vault-side template update actions")
	}
}

func TestDeployCommandsToRepo(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()

	// Set up vault commands
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	cmdsDir := filepath.Join(agentctxDir, "commands")
	os.MkdirAll(cmdsDir, 0o755)
	os.WriteFile(filepath.Join(cmdsDir, "restart.md"), []byte("# Restart MCP"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md"), []byte("# Wrap MCP"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "wrap.md.pending"), []byte("pending"), 0o644) // should be skipped

	// Set up repo commands dir
	repoCmdsDir := filepath.Join(cwd, ".claude", "commands")
	os.MkdirAll(repoCmdsDir, 0o755)
	os.WriteFile(filepath.Join(repoCmdsDir, "restart.md"), []byte("# Old Restart"), 0o644)

	actions := deployCommandsToRepo(cwd, agentctxDir)

	// restart.md should be updated (different content)
	// wrap.md should be deployed (new)
	if len(actions) != 2 {
		t.Errorf("expected 2 actions, got %d: %v", len(actions), actions)
	}

	// Verify content
	data, _ := os.ReadFile(filepath.Join(repoCmdsDir, "restart.md"))
	if string(data) != "# Restart MCP" {
		t.Errorf("restart.md not updated: %q", string(data))
	}
	data, _ = os.ReadFile(filepath.Join(repoCmdsDir, "wrap.md"))
	if string(data) != "# Wrap MCP" {
		t.Errorf("wrap.md not deployed: %q", string(data))
	}

	// .pending should NOT be deployed
	if _, err := os.Stat(filepath.Join(repoCmdsDir, "wrap.md.pending")); !os.IsNotExist(err) {
		t.Error(".pending sidecar should not be deployed to repo")
	}
}

func TestGitignoreRemove(t *testing.T) {
	dir := t.TempDir()
	giPath := filepath.Join(dir, ".gitignore")
	os.WriteFile(giPath, []byte("/CLAUDE.md\n/commit.msg\n/agentctx\n"), 0o644)

	removed, err := gitignoreRemove(giPath, "/agentctx")
	if err != nil {
		t.Fatalf("gitignoreRemove: %v", err)
	}
	if !removed {
		t.Error("expected entry to be removed")
	}

	data, _ := os.ReadFile(giPath)
	content := string(data)
	if strings.Contains(content, "agentctx") {
		t.Error("agentctx still in .gitignore")
	}
	if !strings.Contains(content, "CLAUDE.md") {
		t.Error("CLAUDE.md missing from .gitignore")
	}
	if !strings.Contains(content, "commit.msg") {
		t.Error("commit.msg missing from .gitignore")
	}
}

func TestGitignoreRemove_NotPresent(t *testing.T) {
	dir := t.TempDir()
	giPath := filepath.Join(dir, ".gitignore")
	os.WriteFile(giPath, []byte("/CLAUDE.md\n/commit.msg\n"), 0o644)

	removed, err := gitignoreRemove(giPath, "/agentctx")
	if err != nil {
		t.Fatalf("gitignoreRemove: %v", err)
	}
	if removed {
		t.Error("expected false when entry not present")
	}
}

func TestPropagateSharedSubdir_SkillsDirectory(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "skills"), 0o755)

	// Create a skill directory in templates
	skillDir := filepath.Join(vault, "Templates", "agentctx", "skills", "my-skill")
	os.MkdirAll(filepath.Join(skillDir, "references"), 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# My Skill"), 0o644)
	os.WriteFile(filepath.Join(skillDir, "references", "data.md"), []byte("# Data"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "skills", false, false)

	// Should have CREATE action for the skill directory
	found := false
	for _, a := range actions {
		if a.Path == "skills/my-skill" && a.Action == "CREATE" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CREATE action for skills/my-skill, got: %v", actions)
	}

	// Verify files copied
	data, err := os.ReadFile(filepath.Join(agentctxDir, "skills", "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md not copied: %v", err)
	}
	if string(data) != "# My Skill" {
		t.Errorf("SKILL.md content = %q", string(data))
	}
	data, err = os.ReadFile(filepath.Join(agentctxDir, "skills", "my-skill", "references", "data.md"))
	if err != nil {
		t.Fatalf("references/data.md not copied: %v", err)
	}
	if string(data) != "# Data" {
		t.Errorf("references/data.md content = %q", string(data))
	}
}

func TestPropagateSharedSubdir_SkillsExistingUpdatedIfDifferent(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")

	// Create existing skill in project
	projSkillDir := filepath.Join(agentctxDir, "skills", "my-skill")
	os.MkdirAll(projSkillDir, 0o755)
	os.WriteFile(filepath.Join(projSkillDir, "SKILL.md"), []byte("# Custom"), 0o644)

	// Create same skill in templates with different content
	tmplSkillDir := filepath.Join(vault, "Templates", "agentctx", "skills", "my-skill")
	os.MkdirAll(tmplSkillDir, 0o755)
	os.WriteFile(filepath.Join(tmplSkillDir, "SKILL.md"), []byte("# Template"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "skills", false, false)

	// Should update — content differs and not pinned
	updateFound := false
	for _, a := range actions {
		if a.Path == "skills/my-skill" && a.Action == "UPDATE" {
			updateFound = true
		}
	}
	if !updateFound {
		t.Errorf("expected UPDATE action for changed skill dir, got %v", actions)
	}

	// Content should be updated from template
	data, _ := os.ReadFile(filepath.Join(projSkillDir, "SKILL.md"))
	if string(data) != "# Template" {
		t.Errorf("skill file not updated: %q", string(data))
	}
}

func TestPropagateSharedSubdir_SkillsExistingIdenticalSkipped(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")

	// Create existing skill matching template
	projSkillDir := filepath.Join(agentctxDir, "skills", "my-skill")
	os.MkdirAll(projSkillDir, 0o755)
	os.WriteFile(filepath.Join(projSkillDir, "SKILL.md"), []byte("# Same"), 0o644)

	tmplSkillDir := filepath.Join(vault, "Templates", "agentctx", "skills", "my-skill")
	os.MkdirAll(tmplSkillDir, 0o755)
	os.WriteFile(filepath.Join(tmplSkillDir, "SKILL.md"), []byte("# Same"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "skills", false, false)

	// No actions — identical content
	if len(actions) != 0 {
		t.Errorf("expected 0 actions for identical skill, got %d: %v", len(actions), actions)
	}
}

func TestPropagateSharedSubdir_SkillsPinned(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")

	// Create existing skill with .pinned marker
	projSkillDir := filepath.Join(agentctxDir, "skills", "my-skill")
	os.MkdirAll(projSkillDir, 0o755)
	os.WriteFile(filepath.Join(projSkillDir, "SKILL.md"), []byte("# Pinned"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "skills", "my-skill.pinned"), []byte("pinned\n"), 0o644)

	// Create template
	tmplSkillDir := filepath.Join(vault, "Templates", "agentctx", "skills", "my-skill")
	os.MkdirAll(tmplSkillDir, 0o755)
	os.WriteFile(filepath.Join(tmplSkillDir, "SKILL.md"), []byte("# Template"), 0o644)

	// Even with forceUpdate, pinned should be skipped
	actions := propagateSharedSubdir(vault, agentctxDir, "skills", false, true)

	if len(actions) != 0 {
		t.Errorf("expected 0 actions for pinned skill, got %d: %v", len(actions), actions)
	}

	// Original preserved
	data, _ := os.ReadFile(filepath.Join(projSkillDir, "SKILL.md"))
	if string(data) != "# Pinned" {
		t.Errorf("pinned skill was overwritten: %q", string(data))
	}
}

func TestPropagateSharedSubdir_SkillsForceUpdate(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")

	// Create existing skill
	projSkillDir := filepath.Join(agentctxDir, "skills", "my-skill")
	os.MkdirAll(projSkillDir, 0o755)
	os.WriteFile(filepath.Join(projSkillDir, "SKILL.md"), []byte("# Old"), 0o644)

	// Create template with different content
	tmplSkillDir := filepath.Join(vault, "Templates", "agentctx", "skills", "my-skill")
	os.MkdirAll(tmplSkillDir, 0o755)
	os.WriteFile(filepath.Join(tmplSkillDir, "SKILL.md"), []byte("# New"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "skills", false, true)

	// Should have UPDATE action
	found := false
	for _, a := range actions {
		if a.Path == "skills/my-skill" && a.Action == "UPDATE" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected UPDATE action for skills/my-skill, got: %v", actions)
	}

	// Content should be overwritten
	data, _ := os.ReadFile(filepath.Join(projSkillDir, "SKILL.md"))
	if string(data) != "# New" {
		t.Errorf("skill not overwritten, got %q", string(data))
	}
}

func TestPropagateSharedSubdir_SkillFile(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "skills"), 0o755)

	// Create a .skill file in templates (non-.md file)
	tmplSkills := filepath.Join(vault, "Templates", "agentctx", "skills")
	os.MkdirAll(tmplSkills, 0o755)
	os.WriteFile(filepath.Join(tmplSkills, "analyst.skill"), []byte("skill-data"), 0o644)

	actions := propagateSharedSubdir(vault, agentctxDir, "skills", false, false)

	// .skill file should be propagated (not filtered by .md)
	found := false
	for _, a := range actions {
		if a.Path == "skills/analyst.skill" && a.Action == "CREATE" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected CREATE action for skills/analyst.skill, got: %v", actions)
	}

	data, err := os.ReadFile(filepath.Join(agentctxDir, "skills", "analyst.skill"))
	if err != nil {
		t.Fatalf("analyst.skill not copied: %v", err)
	}
	if string(data) != "skill-data" {
		t.Errorf("analyst.skill content = %q", string(data))
	}
}

func TestDeploySubdirToRepo_Skills(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()

	// Set up vault skills
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	skillDir := filepath.Join(agentctxDir, "skills", "my-skill")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Skill"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "skills", "analyst.skill"), []byte("skill-data"), 0o644)

	// Set up repo skills dir
	repoSkills := filepath.Join(cwd, ".claude", "skills")
	os.MkdirAll(repoSkills, 0o755)

	actions := deploySubdirToRepo(cwd, agentctxDir, "skills")

	if len(actions) == 0 {
		t.Fatal("expected actions from deploy")
	}

	// Verify skill directory deployed
	data, err := os.ReadFile(filepath.Join(repoSkills, "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("SKILL.md not deployed: %v", err)
	}
	if string(data) != "# Skill" {
		t.Errorf("SKILL.md content = %q", string(data))
	}

	// Verify .skill file deployed
	data, err = os.ReadFile(filepath.Join(repoSkills, "analyst.skill"))
	if err != nil {
		t.Fatalf("analyst.skill not deployed: %v", err)
	}
	if string(data) != "skill-data" {
		t.Errorf("analyst.skill content = %q", string(data))
	}
}

func TestMigrate6to7(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()

	// Create project agentctx
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "skills"), 0o755)

	// Create skill template
	tmplSkillDir := filepath.Join(vault, "Templates", "agentctx", "skills", "test-skill")
	os.MkdirAll(tmplSkillDir, 0o755)
	os.WriteFile(filepath.Join(tmplSkillDir, "SKILL.md"), []byte("# Test Skill"), 0o644)

	// Set up repo .claude/skills
	os.MkdirAll(filepath.Join(cwd, ".claude", "skills"), 0o755)

	ctx := MigrationContext{
		AgentctxPath: agentctxDir,
		RepoPath:     cwd,
		Project:      "test",
		VaultPath:    vault,
	}
	actions, err := migrate6to7(ctx)
	if err != nil {
		t.Fatalf("migrate6to7: %v", err)
	}

	if len(actions) == 0 {
		t.Fatal("expected actions from migrate6to7")
	}

	// Verify skill propagated to vault project
	data, err := os.ReadFile(filepath.Join(agentctxDir, "skills", "test-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill not propagated to vault: %v", err)
	}
	if string(data) != "# Test Skill" {
		t.Errorf("vault skill content = %q", string(data))
	}

	// Verify skill deployed to repo
	data, err = os.ReadFile(filepath.Join(cwd, ".claude", "skills", "test-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill not deployed to repo: %v", err)
	}
	if string(data) != "# Test Skill" {
		t.Errorf("repo skill content = %q", string(data))
	}
}

func TestInit_DeploysSkillDirectories(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Pre-create skill in vault Templates (as if user had added it)
	tmplSkillDir := filepath.Join(vault, "Templates", "agentctx", "skills", "test-skill")
	os.MkdirAll(tmplSkillDir, 0o755)
	os.WriteFile(filepath.Join(tmplSkillDir, "SKILL.md"), []byte("# Test"), 0o644)

	// Also add a .skill file
	tmplSkills := filepath.Join(vault, "Templates", "agentctx", "skills")
	os.WriteFile(filepath.Join(tmplSkills, "test.skill"), []byte("packed"), 0o644)

	// Create .vibe-vault.toml so resolveProject works
	os.WriteFile(filepath.Join(cwd, ".vibe-vault.toml"), []byte("project = \"skilltest\"\n"), 0o644)

	// Init the project
	_, err := Init(cfg, cwd, Opts{Project: "skilltest"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Verify skill directory deployed to repo .claude/skills/
	data, err := os.ReadFile(filepath.Join(cwd, ".claude", "skills", "test-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill dir not deployed to repo: %v", err)
	}
	if string(data) != "# Test" {
		t.Errorf("skill content = %q", string(data))
	}

	// Verify .skill file deployed
	data, err = os.ReadFile(filepath.Join(cwd, ".claude", "skills", "test.skill"))
	if err != nil {
		t.Fatalf(".skill file not deployed to repo: %v", err)
	}
	if string(data) != "packed" {
		t.Errorf(".skill content = %q", string(data))
	}
}

func TestSync_ThreeWayBaseline(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	// Create project at latest schema — no migration will run.
	agentctxDir := filepath.Join(vault, "Projects", "baselinetest", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	vf := newVersionFile(LatestSchemaVersion)
	WriteVersion(agentctxDir, vf)

	// Use a custom command name that won't be overwritten by forceUpdateVaultTemplates
	// (which writes Go-embedded templates like wrap.md, restart.md, etc.)
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "custom-test.md"), []byte("# Original"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "custom-test.md"), []byte("# Original"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "custom-test.md.baseline"), []byte("# Original"), 0o644)

	// First sync: template unchanged → no action for our custom file.
	result, err := Sync(cfg, cwd, SyncOpts{Project: "baselinetest"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	for _, psr := range result.Projects {
		for _, a := range psr.Actions {
			if a.Path == "commands/custom-test.md" {
				t.Errorf("unexpected action for unchanged template: %v", a)
			}
		}
	}

	// Update the template AFTER the sync (so forceUpdateVaultTemplates won't clobber it).
	os.WriteFile(filepath.Join(tmplCmds, "custom-test.md"), []byte("# Updated Template"), 0o644)

	// Second sync: template changed, project untouched → auto UPDATE.
	result, err = Sync(cfg, cwd, SyncOpts{Project: "baselinetest"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	var foundUpdate bool
	for _, psr := range result.Projects {
		for _, a := range psr.Actions {
			if a.Path == "commands/custom-test.md" && a.Action == "UPDATE" {
				foundUpdate = true
			}
		}
	}
	if !foundUpdate {
		t.Error("expected UPDATE action for changed template + untouched project")
	}

	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "custom-test.md"))
	if string(data) != "# Updated Template" {
		t.Errorf("project file = %q, want '# Updated Template'", string(data))
	}

	// Now user customizes the file AND template changes again.
	os.WriteFile(filepath.Join(agentctxDir, "commands", "custom-test.md"), []byte("# User Edit"), 0o644)
	os.WriteFile(filepath.Join(tmplCmds, "custom-test.md"), []byte("# Even Newer Template"), 0o644)

	// Third sync: both changed → CONFLICT.
	result, err = Sync(cfg, cwd, SyncOpts{Project: "baselinetest"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	var foundConflict bool
	for _, psr := range result.Projects {
		for _, a := range psr.Actions {
			if a.Path == "commands/custom-test.md" && a.Action == "CONFLICT" {
				foundConflict = true
			}
		}
	}
	if !foundConflict {
		t.Error("expected CONFLICT action for both-changed case")
	}

	// Project should still have user's content.
	data, _ = os.ReadFile(filepath.Join(agentctxDir, "commands", "custom-test.md"))
	if string(data) != "# User Edit" {
		t.Errorf("project file should not be overwritten on conflict: %q", string(data))
	}

	// Fourth sync with --force: override conflict.
	os.WriteFile(filepath.Join(tmplCmds, "custom-test.md"), []byte("# Even Newer Template"), 0o644)
	result, err = Sync(cfg, cwd, SyncOpts{Project: "baselinetest", Force: true})
	if err != nil {
		t.Fatalf("Sync --force: %v", err)
	}
	foundUpdate = false
	for _, psr := range result.Projects {
		for _, a := range psr.Actions {
			if a.Path == "commands/custom-test.md" && a.Action == "UPDATE" {
				foundUpdate = true
			}
		}
	}
	if !foundUpdate {
		t.Error("expected UPDATE action with --force")
	}
	data, _ = os.ReadFile(filepath.Join(agentctxDir, "commands", "custom-test.md"))
	if string(data) != "# Even Newer Template" {
		t.Errorf("project file = %q, want '# Even Newer Template'", string(data))
	}
}

func TestSync_ForceRespectsPin(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	agentctxDir := filepath.Join(vault, "Projects", "pintest", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)
	vf := newVersionFile(LatestSchemaVersion)
	WriteVersion(agentctxDir, vf)

	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# Custom"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md.pinned"), []byte("pinned\n"), 0o644)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "pintest", Force: true})
	if err != nil {
		t.Fatalf("Sync --force: %v", err)
	}

	// Pinned file should produce no actions.
	for _, psr := range result.Projects {
		for _, a := range psr.Actions {
			if a.Path == "commands/wrap.md" {
				t.Errorf("pinned file should not have any action, got %q", a.Action)
			}
		}
	}

	// Content should be preserved.
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "commands", "wrap.md"))
	if string(data) != "# Custom" {
		t.Errorf("pinned file was overwritten: %q", string(data))
	}
}

// testConfig is defined in context_test.go

// --- Top-level (workflow.md) sync tests ---

// seedTopLevelFixture creates a minimal vault + project layout with a
// Templates/agentctx/workflow.md and optionally a project-side workflow.md
// and baseline. Returns the vault root and the project's agentctx dir.
func seedTopLevelFixture(t *testing.T, tmplContent, projContent, baselineContent string) (string, string) {
	t.Helper()
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	tmplDir := filepath.Join(vault, "Templates", "agentctx")
	if err := os.MkdirAll(agentctxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmplDir, "workflow.md"), []byte(tmplContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if projContent != "" {
		if err := os.WriteFile(filepath.Join(agentctxDir, "workflow.md"), []byte(projContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if baselineContent != "" {
		if err := os.WriteFile(filepath.Join(agentctxDir, "workflow.md.baseline"), []byte(baselineContent), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return vault, agentctxDir
}

// Missing project file → CREATE + baseline written.
func TestPropagateTopLevel_CreatesOnFirstSync(t *testing.T) {
	vault, agentctxDir := seedTopLevelFixture(t, "# compressed workflow", "", "")

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 1 || actions[0].Action != "CREATE" {
		t.Fatalf("actions = %v, want single CREATE", actions)
	}

	data, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md"))
	if string(data) != "# compressed workflow" {
		t.Errorf("project file = %q, want template content", string(data))
	}
	if _, err := os.Stat(filepath.Join(agentctxDir, "workflow.md.baseline")); err != nil {
		t.Error("baseline not created on first-time sync")
	}
}

// Stock project file matching template + no baseline → silent backfill.
func TestPropagateTopLevel_BackfillsBaselineWhenIdentical(t *testing.T) {
	stock := "# stock workflow"
	vault, agentctxDir := seedTopLevelFixture(t, stock, stock, "")

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 0 {
		t.Errorf("expected no actions for identical content; got %v", actions)
	}

	baseline, err := os.ReadFile(filepath.Join(agentctxDir, "workflow.md.baseline"))
	if err != nil {
		t.Fatalf("baseline not backfilled: %v", err)
	}
	if string(baseline) != stock {
		t.Errorf("baseline = %q, want %q", string(baseline), stock)
	}
}

// Drifted project file + no baseline → CUSTOMIZED, no overwrite.
// This is the exact scenario for a pre-Phase-2d project picking up the
// compressed workflow.md template: user didn't ask for the change, so sync
// surfaces it as CUSTOMIZED rather than silently rewriting.
func TestPropagateTopLevel_DriftedReportsCustomized(t *testing.T) {
	vault, agentctxDir := seedTopLevelFixture(t, "# compressed", "# verbose", "")

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 1 || actions[0].Action != "CUSTOMIZED" {
		t.Fatalf("actions = %v, want single CUSTOMIZED", actions)
	}

	data, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md"))
	if string(data) != "# verbose" {
		t.Errorf("project file was overwritten: %q", string(data))
	}
}

// Drifted project file + no baseline + --force → UPDATE with baseline seeded.
// This is the recipe for adopting a drifted template: user inspects the
// CUSTOMIZED report, decides to accept the new template, reruns with --force.
func TestPropagateTopLevel_DriftedForceOverwrites(t *testing.T) {
	vault, agentctxDir := seedTopLevelFixture(t, "# compressed", "# verbose", "")

	actions := propagateTopLevel(vault, agentctxDir, false, true)
	if len(actions) != 1 || actions[0].Action != "UPDATE" {
		t.Fatalf("actions = %v, want single UPDATE", actions)
	}

	data, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md"))
	if string(data) != "# compressed" {
		t.Errorf("project file not updated: %q", string(data))
	}
	baseline, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md.baseline"))
	if string(baseline) != "# compressed" {
		t.Errorf("baseline = %q, want template content", string(baseline))
	}
}

// Baseline + template unchanged from baseline → no action.
func TestPropagateTopLevel_NoopWhenTemplateUnchanged(t *testing.T) {
	vault, agentctxDir := seedTopLevelFixture(t, "# stable", "# user edit", "# stable")

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 0 {
		t.Errorf("expected no actions when template unchanged; got %v", actions)
	}
}

// Template changed + user-clean (project matches baseline) → auto-UPDATE.
func TestPropagateTopLevel_AutoUpdateCleanUser(t *testing.T) {
	vault, agentctxDir := seedTopLevelFixture(t, "# new", "# old", "# old")

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 1 || actions[0].Action != "UPDATE" {
		t.Fatalf("actions = %v, want single UPDATE", actions)
	}

	data, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md"))
	if string(data) != "# new" {
		t.Errorf("project file = %q, want # new", string(data))
	}
}

// Template changed AND user changed (both diverge from baseline) → CONFLICT.
func TestPropagateTopLevel_ConflictOnBothSidesChanged(t *testing.T) {
	vault, agentctxDir := seedTopLevelFixture(t, "# new tmpl", "# user edit", "# original")

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 1 || actions[0].Action != "CONFLICT" {
		t.Fatalf("actions = %v, want single CONFLICT", actions)
	}

	data, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md"))
	if string(data) != "# user edit" {
		t.Errorf("project file was overwritten: %q", string(data))
	}
}

// Missing vault template → skip silently (no action).
func TestPropagateTopLevel_MissingTemplateSkips(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(agentctxDir, 0o755)

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 0 {
		t.Errorf("expected no actions when template missing; got %v", actions)
	}
}

// Template tokens ({{PROJECT}}, {{DATE}}) must be substituted before write.
// Regression guard: pre-substitution, synced workflow.md contained literal
// "{{PROJECT}}" in the heading because propagateFile skipped applyVars.
func TestPropagateTopLevel_SubstitutesProjectToken(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "myproj", "agentctx")
	tmplDir := filepath.Join(vault, "Templates", "agentctx")
	os.MkdirAll(agentctxDir, 0o755)
	os.MkdirAll(tmplDir, 0o755)
	os.WriteFile(filepath.Join(tmplDir, "workflow.md"),
		[]byte("# {{PROJECT}} — Workflow\n"), 0o644)

	actions := propagateTopLevel(vault, agentctxDir, false, false)
	if len(actions) != 1 || actions[0].Action != "CREATE" {
		t.Fatalf("actions = %v, want single CREATE", actions)
	}

	data, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md"))
	if strings.Contains(string(data), "{{PROJECT}}") {
		t.Errorf("synced workflow.md still contains literal {{PROJECT}}: %q", string(data))
	}
	if !strings.Contains(string(data), "myproj") {
		t.Errorf("synced workflow.md missing substituted project name: %q", string(data))
	}
}

// End-to-end: full Sync() pipeline propagates the embedded workflow.md
// template to a project whose workflow.md matches its baseline (user-clean).
//
// Sync() calls forceUpdateVaultTemplates() which rewrites Tier 2 from the Go
// embeds on every invocation, so the test cannot stub Tier 2 — it must work
// against whatever the embedded workflow.md actually is. The assertion is
// structural: (a) project file changed, (b) baseline was refreshed to match
// project file, (c) an UPDATE action fired for workflow.md.
func TestSync_PropagatesTopLevelWorkflow(t *testing.T) {
	vault := t.TempDir()
	cwd := t.TempDir()
	cfg := testConfig(vault)

	agentctxDir := filepath.Join(vault, "Projects", "wftest", "agentctx")
	os.MkdirAll(agentctxDir, 0o755)
	WriteVersion(agentctxDir, newVersionFile(LatestSchemaVersion))

	// Seed project + baseline with stub content that won't match the
	// embedded workflow.md, simulating a pre-template-change project.
	stubContent := []byte("# stub workflow — outdated")
	os.WriteFile(filepath.Join(agentctxDir, "workflow.md"), stubContent, 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "workflow.md.baseline"), stubContent, 0o644)

	result, err := Sync(cfg, cwd, SyncOpts{Project: "wftest"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Project file should have changed.
	data, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md"))
	if bytes.Equal(data, stubContent) {
		t.Error("workflow.md was not updated by Sync")
	}

	// Baseline should be refreshed to match the new project content.
	base, _ := os.ReadFile(filepath.Join(agentctxDir, "workflow.md.baseline"))
	if !bytes.Equal(bytes.TrimSpace(data), bytes.TrimSpace(base)) {
		t.Error("workflow.md.baseline was not refreshed to match project file")
	}

	// An UPDATE action should be present for workflow.md at the top level.
	found := false
	for _, psr := range result.Projects {
		for _, a := range psr.Actions {
			if a.Path == "workflow.md" && a.Action == "UPDATE" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected UPDATE action for workflow.md in Sync result")
	}
}
