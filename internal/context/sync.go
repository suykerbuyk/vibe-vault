// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johns/vibe-vault/internal/config"
)

// SyncOpts holds options for the Sync command.
type SyncOpts struct {
	Project string
	All     bool
	DryRun  bool
	Force   bool
}

// SyncResult holds the outcome of a Sync operation.
type SyncResult struct {
	Projects []ProjectSyncResult
}

// ProjectSyncResult holds the sync outcome for a single project.
type ProjectSyncResult struct {
	Project     string
	FromVersion int
	ToVersion   int
	Actions     []FileAction
	RepoSkipped bool   // true in --all mode when repo path unknown
	RepoNote    string // reason for skipping repo ops
}

// Sync runs schema migrations and shared command propagation for one or all projects.
func Sync(cfg config.Config, cwd string, opts SyncOpts) (*SyncResult, error) {
	if err := validateVault(cfg.VaultPath); err != nil {
		return nil, err
	}

	var projects []string
	if opts.All {
		projects = discoverProjects(cfg.VaultPath)
	} else {
		project := opts.Project
		if project == "" {
			var err error
			project, err = resolveProject(cwd, "")
			if err != nil {
				return nil, err
			}
		}
		projects = []string{project}
	}

	result := &SyncResult{}
	for _, project := range projects {
		repoPath := cwd
		if opts.All {
			repoPath = "" // vault-only in --all mode
		}
		psr, err := syncProject(cfg, repoPath, project, opts)
		if err != nil {
			return nil, fmt.Errorf("sync %s: %w", project, err)
		}
		result.Projects = append(result.Projects, *psr)
	}

	return result, nil
}

func syncProject(cfg config.Config, repoPath, project string, opts SyncOpts) (*ProjectSyncResult, error) {
	agentctxPath := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx")

	// Ensure vault templates are seeded (idempotent — safeWrite never overwrites)
	EnsureVaultTemplates(cfg.VaultPath)

	// Read current version
	vf, err := ReadVersion(agentctxPath)
	if err != nil {
		return nil, err
	}

	psr := &ProjectSyncResult{
		Project:     project,
		FromVersion: vf.SchemaVersion,
		ToVersion:   vf.SchemaVersion,
	}

	// Apply migrations
	for _, m := range migrationsFrom(vf.SchemaVersion) {
		mctx := MigrationContext{
			AgentctxPath: agentctxPath,
			RepoPath:     repoPath,
			Project:      project,
			VaultPath:    cfg.VaultPath,
			Force:        opts.Force,
		}

		if opts.DryRun {
			psr.Actions = append(psr.Actions, FileAction{
				Path:     fmt.Sprintf("migration %d→%d", m.From, m.To),
				Action:   "DRY-RUN",
				Location: "",
			})
			psr.ToVersion = m.To
			continue
		}

		actions, err := m.Apply(mctx)
		if err != nil {
			return nil, fmt.Errorf("migration %d→%d: %w", m.From, m.To, err)
		}
		psr.Actions = append(psr.Actions, actions...)
		psr.ToVersion = m.To

		// Update .version after each migration
		newVF := VersionFile{
			SchemaVersion: m.To,
			CreatedBy:     vf.CreatedBy,
			CreatedAt:     vf.CreatedAt,
			UpdatedBy:     vvVersion(),
			UpdatedAt:     nowISO(),
		}
		if vf.CreatedBy == "" {
			newVF.CreatedBy = vvVersion()
			newVF.CreatedAt = newVF.UpdatedAt
		}
		if err := WriteVersion(agentctxPath, newVF); err != nil {
			return nil, err
		}
		vf = newVF
	}

	// Mark repo-skipped in --all mode
	if repoPath == "" {
		psr.RepoSkipped = true
		psr.RepoNote = "run `vv context sync` from repo root for repo-side updates"
	}

	// Propagate shared commands from vault templates
	if !opts.DryRun {
		cmdActions := propagateSharedCommands(cfg.VaultPath, agentctxPath, false)
		psr.Actions = append(psr.Actions, cmdActions...)

		// Deploy commands to repo (skip in --all mode)
		if repoPath != "" {
			deployActions := deployCommandsToRepo(repoPath, agentctxPath)
			psr.Actions = append(psr.Actions, deployActions...)
		}
	} else {
		cmdActions := propagateSharedCommands(cfg.VaultPath, agentctxPath, true)
		psr.Actions = append(psr.Actions, cmdActions...)
	}

	return psr, nil
}

// discoverProjects finds all projects that have an agentctx/ directory.
func discoverProjects(vaultPath string) []string {
	projectsDir := filepath.Join(vaultPath, "Projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	var projects []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentctxDir := filepath.Join(projectsDir, e.Name(), "agentctx")
		if info, err := os.Stat(agentctxDir); err == nil && info.IsDir() {
			projects = append(projects, e.Name())
		}
	}
	return projects
}

// propagateSharedCommands copies commands from Templates/agentctx/commands/
// to a project's agentctx/commands/, without overwriting existing files.
func propagateSharedCommands(vaultPath, agentctxPath string, dryRun bool) []FileAction {
	templatesDir := filepath.Join(vaultPath, "Templates", "agentctx", "commands")
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil // no templates dir, nothing to propagate
	}

	var actions []FileAction
	projectCmdsDir := filepath.Join(agentctxPath, "commands")

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		dstPath := filepath.Join(projectCmdsDir, e.Name())
		if _, err := os.Stat(dstPath); err == nil {
			// Check for .pinned marker — user chose to keep their version
			if _, pinErr := os.Stat(dstPath + ".pinned"); pinErr == nil {
				continue
			}
			// Compare contents
			srcPath := filepath.Join(templatesDir, e.Name())
			srcData, err := os.ReadFile(srcPath)
			if err != nil {
				continue
			}
			dstData, err := os.ReadFile(dstPath)
			if err != nil {
				continue
			}
			if bytes.Equal(bytes.TrimSpace(srcData), bytes.TrimSpace(dstData)) {
				continue // identical
			}
			// Write .pending sidecar with new version
			pendingPath := dstPath + ".pending"
			if !dryRun {
				if err := os.MkdirAll(projectCmdsDir, 0o755); err != nil {
					actions = append(actions, FileAction{
						Path:   "commands/" + e.Name() + ".pending",
						Action: "ERROR: " + err.Error(),
					})
					continue
				}
				if err := os.WriteFile(pendingPath, srcData, 0o644); err != nil {
					actions = append(actions, FileAction{
						Path:   "commands/" + e.Name() + ".pending",
						Action: "ERROR: " + err.Error(),
					})
					continue
				}
			}
			actions = append(actions, FileAction{
				Path:     "commands/" + e.Name(),
				Action:   "OUTDATED",
				Location: "vault",
			})
			continue
		}

		if dryRun {
			actions = append(actions, FileAction{
				Path:     "commands/" + e.Name(),
				Action:   "DRY-RUN",
				Location: "vault",
			})
			continue
		}

		srcPath := filepath.Join(templatesDir, e.Name())
		data, err := os.ReadFile(srcPath)
		if err != nil {
			actions = append(actions, FileAction{
				Path:   "commands/" + e.Name(),
				Action: "ERROR: " + err.Error(),
			})
			continue
		}
		if err := os.MkdirAll(projectCmdsDir, 0o755); err != nil {
			actions = append(actions, FileAction{
				Path:   "commands/" + e.Name(),
				Action: "ERROR: " + err.Error(),
			})
			continue
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			actions = append(actions, FileAction{
				Path:   "commands/" + e.Name(),
				Action: "ERROR: " + err.Error(),
			})
			continue
		}
		actions = append(actions, FileAction{
			Path:     "commands/" + e.Name(),
			Action:   "CREATE",
			Location: "vault",
		})
	}
	return actions
}

// migrate3to4 converts CLAUDE.md from a generated file to a symlink through
// agentctx, ensures vault-side CLAUDE.md exists, and creates vault-side
// directories and repo-side symlinks for all .claude/ subdirectories.
func migrate3to4(ctx MigrationContext) ([]FileAction, error) {
	var actions []FileAction

	// Ensure vault-side CLAUDE.md exists in agentctx
	claudeContent := readEmbedded("CLAUDE.md")
	claudeVault := filepath.Join(ctx.AgentctxPath, "CLAUDE.md")
	action := safeWrite(claudeVault, claudeContent, false)
	actions = append(actions, FileAction{Path: "agentctx/CLAUDE.md", Action: action, Location: "vault"})

	// Ensure vault-side directories for all .claude/ subdirs
	for _, sub := range claudeSubdirs {
		_ = os.MkdirAll(filepath.Join(ctx.AgentctxPath, sub), 0o755)
	}

	// Repo-side operations (skip if no repo path)
	if ctx.RepoPath == "" {
		return actions, nil
	}

	// Convert CLAUDE.md from file to symlink
	claudeMDPath := filepath.Join(ctx.RepoPath, "CLAUDE.md")
	if info, err := os.Lstat(claudeMDPath); err == nil && info.Mode()&os.ModeSymlink == 0 {
		os.Remove(claudeMDPath)
	}
	linkAction := safeSymlink(claudeMDPath, filepath.Join("agentctx", "CLAUDE.md"), true)
	actions = append(actions, FileAction{Path: "CLAUDE.md", Action: linkAction, Location: "repo"})

	// Create .claude/ subdirectory symlinks through agentctx
	dotClaude := filepath.Join(ctx.RepoPath, ".claude")
	_ = os.MkdirAll(dotClaude, 0o755)
	for _, sub := range claudeSubdirs {
		link := filepath.Join(dotClaude, sub)
		target := filepath.Join("..", "agentctx", sub)
		subAction := safeSymlink(link, target, true)
		actions = append(actions, FileAction{Path: ".claude/" + sub, Action: subAction, Location: "repo"})
	}

	return actions, nil
}

// migrate2to3 adds per-project config.toml overlay template.
func migrate2to3(ctx MigrationContext) ([]FileAction, error) {
	var actions []FileAction

	cfgPath := filepath.Join(ctx.AgentctxPath, "config.toml")
	action := safeWrite(cfgPath, config.ProjectConfigTemplate(), false)
	actions = append(actions, FileAction{Path: "config.toml", Action: action, Location: "vault"})

	return actions, nil
}

// migrate1to2 adds agentctx symlink at repo root, rewrites CLAUDE.md to
// relative paths, replaces .claude/commands with relative symlink, adds
// agentctx to .gitignore, and ensures vault templates are seeded.
func migrate1to2(ctx MigrationContext) ([]FileAction, error) {
	var actions []FileAction

	// Ensure vault templates
	tmplActions := EnsureVaultTemplates(ctx.VaultPath)
	actions = append(actions, tmplActions...)

	// Repo-side operations (skip if no repo path)
	if ctx.RepoPath == "" {
		return actions, nil
	}

	// 1. Create agentctx symlink at repo root
	agentctxLink := filepath.Join(ctx.RepoPath, "agentctx")
	linkAction := safeSymlink(agentctxLink, ctx.AgentctxPath, ctx.Force)
	actions = append(actions, FileAction{Path: "agentctx", Action: linkAction, Location: "repo"})

	// 2. Rewrite CLAUDE.md to relative paths
	claudeMDPath := filepath.Join(ctx.RepoPath, "CLAUDE.md")
	claudeContent := readEmbedded("CLAUDE.md")
	safeWrite(claudeMDPath, claudeContent, true)
	actions = append(actions, FileAction{Path: "CLAUDE.md", Action: "UPDATE", Location: "repo"})

	// 3. Replace .claude/ subdirectories with relative symlinks through agentctx
	dotClaude := filepath.Join(ctx.RepoPath, ".claude")
	_ = os.MkdirAll(dotClaude, 0o755)
	for _, sub := range claudeSubdirs {
		link := filepath.Join(dotClaude, sub)
		target := filepath.Join("..", "agentctx", sub)
		safeSymlink(link, target, true)
		actions = append(actions, FileAction{Path: ".claude/" + sub, Action: "UPDATE", Location: "repo"})
	}

	// 4. Add agentctx to .gitignore
	giAction, err := gitignoreEnsure(filepath.Join(ctx.RepoPath, ".gitignore"), "/agentctx")
	if err != nil {
		return actions, err
	}
	if giAction != "" {
		actions = append(actions, FileAction{Path: ".gitignore", Action: giAction, Location: "repo"})
	}

	return actions, nil
}

// migrate4to5 converts repo-side symlinks to regular files/directories.
// The agentctx symlink is removed; CLAUDE.md, commit.msg, and .claude/
// subdirectories become regular files written from vault content.
// Also force-updates vault templates so new MCP-first content propagates.
func migrate4to5(ctx MigrationContext) ([]FileAction, error) {
	var actions []FileAction

	// Force-update vault templates with new MCP-first content
	tmplActions := forceUpdateVaultTemplates(ctx.VaultPath)
	actions = append(actions, tmplActions...)

	// Repo-side operations (skip if no repo path — --all mode)
	if ctx.RepoPath == "" {
		return actions, nil
	}

	vars := DefaultVars(ctx.Project)

	// 1. Convert CLAUDE.md from symlink to regular file
	claudeMDPath := filepath.Join(ctx.RepoPath, "CLAUDE.md")
	claudeContent := resolveTemplate(ctx.VaultPath, "CLAUDE.md", vars)
	if info, err := os.Lstat(claudeMDPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		os.Remove(claudeMDPath)
	}
	safeWrite(claudeMDPath, claudeContent, true)
	actions = append(actions, FileAction{Path: "CLAUDE.md", Action: "UPDATE", Location: "repo"})

	// 2. Convert .claude/ subdirectories from symlinks to real directories
	dotClaude := filepath.Join(ctx.RepoPath, ".claude")
	_ = os.MkdirAll(dotClaude, 0o755)
	for _, sub := range claudeSubdirs {
		link := filepath.Join(dotClaude, sub)
		// Read contents from vault before removing symlink
		vaultSubDir := filepath.Join(ctx.AgentctxPath, sub)
		var files map[string][]byte
		if info, err := os.Lstat(link); err == nil && info.Mode()&os.ModeSymlink != 0 {
			files = readDirFiles(vaultSubDir)
			os.Remove(link)
		} else if info != nil && info.IsDir() {
			// Already a real directory — read from vault to sync
			files = readDirFiles(vaultSubDir)
		}
		_ = os.MkdirAll(link, 0o755)
		for name, data := range files {
			_ = os.WriteFile(filepath.Join(link, name), data, 0o644)
		}
		actions = append(actions, FileAction{Path: ".claude/" + sub, Action: "UPDATE", Location: "repo"})
	}

	// 3. Convert commit.msg from symlink to regular file
	commitMsgPath := filepath.Join(ctx.RepoPath, "commit.msg")
	if info, err := os.Lstat(commitMsgPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		// Read current content before removing symlink
		content, _ := os.ReadFile(commitMsgPath)
		os.Remove(commitMsgPath)
		_ = os.WriteFile(commitMsgPath, content, 0o644)
	} else if os.IsNotExist(err) {
		_ = os.WriteFile(commitMsgPath, []byte(""), 0o644)
	}
	actions = append(actions, FileAction{Path: "commit.msg", Action: "UPDATE", Location: "repo"})

	// 4. Remove agentctx symlink from repo root
	agentctxLink := filepath.Join(ctx.RepoPath, "agentctx")
	if info, err := os.Lstat(agentctxLink); err == nil && info.Mode()&os.ModeSymlink != 0 {
		os.Remove(agentctxLink)
		actions = append(actions, FileAction{Path: "agentctx", Action: "REMOVE", Location: "repo"})
	}

	// 5. Remove /agentctx entries from .gitignore
	giPath := filepath.Join(ctx.RepoPath, ".gitignore")
	giUpdated := false
	for _, entry := range []string{"/agentctx", "/agentctx/commands"} {
		if removed, err := gitignoreRemove(giPath, entry); err == nil && removed {
			giUpdated = true
		}
	}
	if giUpdated {
		actions = append(actions, FileAction{Path: ".gitignore", Action: "UPDATE", Location: "repo"})
	}

	return actions, nil
}

// readDirFiles reads all .md files from a directory into a map.
func readDirFiles(dir string) map[string][]byte {
	result := make(map[string][]byte)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err == nil {
			result[e.Name()] = data
		}
	}
	return result
}

// deployCommandsToRepo copies vault agentctx/commands/*.md to repo .claude/commands/.
// This overwrites repo-side commands on every sync (vault is canonical).
func deployCommandsToRepo(repoPath, agentctxPath string) []FileAction {
	vaultCmdsDir := filepath.Join(agentctxPath, "commands")
	entries, err := os.ReadDir(vaultCmdsDir)
	if err != nil {
		return nil
	}

	repoCmdsDir := filepath.Join(repoPath, ".claude", "commands")
	if err := os.MkdirAll(repoCmdsDir, 0o755); err != nil {
		return nil
	}

	var actions []FileAction
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		// Skip .pending and .pinned sidecars
		if strings.HasSuffix(e.Name(), ".pending") || strings.HasSuffix(e.Name(), ".pinned") {
			continue
		}
		srcPath := filepath.Join(vaultCmdsDir, e.Name())
		dstPath := filepath.Join(repoCmdsDir, e.Name())
		data, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		// Check if identical
		existing, err := os.ReadFile(dstPath)
		if err == nil && bytes.Equal(existing, data) {
			continue
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			continue
		}
		actions = append(actions, FileAction{
			Path:     ".claude/commands/" + e.Name(),
			Action:   "UPDATE",
			Location: "repo",
		})
	}
	return actions
}

// gitignoreRemove removes an entry from .gitignore. Returns true if the entry was removed.
func gitignoreRemove(giPath, entry string) (bool, error) {
	data, err := os.ReadFile(giPath)
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(data), "\n")
	bare := strings.TrimPrefix(entry, "/")
	var filtered []string
	removed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == entry || trimmed == bare {
			removed = true
			continue
		}
		filtered = append(filtered, line)
	}

	if !removed {
		return false, nil
	}

	return true, os.WriteFile(giPath, []byte(strings.Join(filtered, "\n")), 0o644)
}

// forceUpdateVaultTemplates overwrites vault Templates/agentctx/ with embedded
// defaults. Used during v4→v5 migration to push MCP-first template content.
func forceUpdateVaultTemplates(vaultPath string) []FileAction {
	tmplDir := filepath.Join(vaultPath, "Templates", "agentctx")

	var actions []FileAction
	for relPath, content := range BuiltinTemplates() {
		path := filepath.Join(tmplDir, relPath)
		action := safeWrite(path, content, true) // force=true
		actions = append(actions, FileAction{
			Path:     filepath.Join("Templates", "agentctx", relPath),
			Action:   action,
			Location: "vault",
		})
	}
	return actions
}
