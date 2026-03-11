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

	// Should have CREATE for new-cmd and OUTDATED for existing
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d: %v", len(actions), actions)
	}

	createFound, outdatedFound := false, false
	for _, a := range actions {
		if a.Path == "commands/new-cmd.md" && a.Action == "CREATE" {
			createFound = true
		}
		if a.Path == "commands/existing.md" && a.Action == "OUTDATED" {
			outdatedFound = true
		}
	}
	if !createFound {
		t.Error("expected CREATE action for new-cmd.md")
	}
	if !outdatedFound {
		t.Error("expected OUTDATED action for existing.md")
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

	actions := propagateSharedCommands(vault, agentctxDir, false)

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

func TestPropagateSharedCommands_OutdatedDetected(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	// Create template and project command with different content
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte("# New Template Version"), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte("# Old Project Version"), 0o644)

	actions := propagateSharedCommands(vault, agentctxDir, false)

	// Should have OUTDATED action
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %v", len(actions), actions)
	}
	if actions[0].Action != "OUTDATED" {
		t.Errorf("action = %q, want OUTDATED", actions[0].Action)
	}
	if actions[0].Path != "commands/wrap.md" {
		t.Errorf("path = %q, want commands/wrap.md", actions[0].Path)
	}

	// .pending file should be written
	pendingPath := filepath.Join(agentctxDir, "commands", "wrap.md.pending")
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		t.Fatalf("pending file not written: %v", err)
	}
	if string(data) != "# New Template Version" {
		t.Errorf("pending content = %q", string(data))
	}
}

func TestPropagateSharedCommands_IdenticalSkipped(t *testing.T) {
	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	os.MkdirAll(filepath.Join(agentctxDir, "commands"), 0o755)

	content := "# Same Content"
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "wrap.md"), []byte(content), 0o644)
	os.WriteFile(filepath.Join(agentctxDir, "commands", "wrap.md"), []byte(content), 0o644)

	actions := propagateSharedCommands(vault, agentctxDir, false)

	// No actions — content is identical
	if len(actions) != 0 {
		t.Errorf("expected 0 actions for identical content, got %d: %v", len(actions), actions)
	}

	// No .pending file
	pendingPath := filepath.Join(agentctxDir, "commands", "wrap.md.pending")
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Error("pending file should not exist for identical content")
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

	actions := propagateSharedCommands(vault, agentctxDir, false)

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

func TestPropagateSharedCommands_PendingErrorSurfaced(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("test requires non-root to trigger permission errors")
	}

	vault := t.TempDir()
	agentctxDir := filepath.Join(vault, "Projects", "test", "agentctx")
	cmdsDir := filepath.Join(agentctxDir, "commands")
	os.MkdirAll(cmdsDir, 0o755)

	// Create template and project command with different content (triggers OUTDATED)
	tmplCmds := filepath.Join(vault, "Templates", "agentctx", "commands")
	os.MkdirAll(tmplCmds, 0o755)
	os.WriteFile(filepath.Join(tmplCmds, "cmd.md"), []byte("# New Version"), 0o644)
	os.WriteFile(filepath.Join(cmdsDir, "cmd.md"), []byte("# Old Version"), 0o644)

	// Make commands dir read-only so .pending write fails
	os.Chmod(cmdsDir, 0o555)
	t.Cleanup(func() { os.Chmod(cmdsDir, 0o755) })

	actions := propagateSharedCommands(vault, agentctxDir, false)

	if len(actions) == 0 {
		t.Fatal("expected ERROR action for .pending write, got empty slice")
	}
	if !strings.HasPrefix(actions[0].Action, "ERROR:") {
		t.Errorf("action = %q, want ERROR: prefix", actions[0].Action)
	}
}

// testConfig is defined in context_test.go
