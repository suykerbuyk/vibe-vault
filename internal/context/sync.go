// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
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

	// Refresh vault templates from Go embeds (always overwrite — embeds are
	// the source of truth, vault templates are a propagation cache).
	forceUpdateVaultTemplates(cfg.VaultPath)

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
			DryRun:       opts.DryRun,
		}

		// NOTE: the outer short-circuit below bypasses m.Apply(mctx)
		// entirely when DryRun is set, so mctx.DryRun is currently only
		// exercised by unit tests that invoke migrations directly. Once
		// the per-migration dry-run reporting is ready to land (DESIGN.md
		// #50 follow-up), remove this short-circuit and let each migration
		// honour mctx.DryRun itself.
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

	// Propagate shared content from vault templates (commands, skills).
	// Force when migrations ran OR --force flag is set.
	force := psr.FromVersion != psr.ToVersion || opts.Force
	for _, sub := range propagateDirs {
		if !opts.DryRun {
			subActions := propagateSharedSubdir(cfg.VaultPath, agentctxPath, sub, false, force)
			psr.Actions = append(psr.Actions, subActions...)

			// Deploy to repo (skip in --all mode).
			// NOTE: deploy functions have no dry-run support — never call during dry-run.
			if repoPath != "" {
				deployActions := deploySubdirToRepo(repoPath, agentctxPath, sub)
				psr.Actions = append(psr.Actions, deployActions...)
			}
		} else {
			subActions := propagateSharedSubdir(cfg.VaultPath, agentctxPath, sub, true, force)
			psr.Actions = append(psr.Actions, subActions...)
		}
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

// propagateDirs lists the .claude/ subdirectories that are propagated from
// vault Templates/agentctx/ to per-project agentctx/ and then to repo .claude/.
var propagateDirs = []string{"commands", "skills"}

// propagateSharedCommands is a backward-compatible wrapper.
func propagateSharedCommands(vaultPath, agentctxPath string, dryRun, force bool) []FileAction {
	return propagateSharedSubdir(vaultPath, agentctxPath, "commands", dryRun, force)
}

// propagateSharedSubdir propagates entries from Templates/agentctx/{subdir}/
// to a project's agentctx/{subdir}/ using three-way baseline comparison.
//
// For each file, the logic compares template (source of truth), project file,
// and .baseline (record of last sync). This distinguishes user customizations
// from stale templates:
//   - Template unchanged since last sync → nothing to do
//   - Template changed, user didn't touch → auto-update
//   - Template changed, user also changed → CONFLICT (skip unless force)
//   - No baseline (legacy) + identical → backfill baseline
//   - No baseline (legacy) + different → treat as user-customized
//
// For "commands", only .md files are propagated.
// For other subdirs (skills, agents, rules), all non-sidecar files and
// directories are propagated.
func propagateSharedSubdir(vaultPath, agentctxPath, subdir string, dryRun, force bool) []FileAction {
	templatesDir := filepath.Join(vaultPath, "Templates", "agentctx", subdir)
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil // no templates dir, nothing to propagate
	}

	var actions []FileAction
	projectSubDir := filepath.Join(agentctxPath, subdir)

	for _, e := range entries {
		// Directory entries (e.g., skills/startup-analyst/)
		if e.IsDir() {
			actions = append(actions, propagateDir(templatesDir, projectSubDir, subdir, e.Name(), dryRun)...)
			continue
		}
		// For commands: only .md files. For other subdirs: all non-sidecar files.
		if subdir == "commands" && !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if isSidecar(e.Name()) {
			continue
		}

		srcPath := filepath.Join(templatesDir, e.Name())
		dstPath := filepath.Join(projectSubDir, e.Name())
		qualPath := subdir + "/" + e.Name()

		srcData, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}

		// File doesn't exist in project → CREATE + write .baseline
		if _, statErr := os.Stat(dstPath); statErr != nil {
			if dryRun {
				actions = append(actions, FileAction{Path: qualPath, Action: "DRY-RUN", Location: "vault"})
				continue
			}
			if mkErr := os.MkdirAll(projectSubDir, 0o755); mkErr != nil {
				actions = append(actions, FileAction{Path: qualPath, Action: "ERROR: " + mkErr.Error()})
				continue
			}
			if wErr := os.WriteFile(dstPath, srcData, 0o644); wErr != nil {
				actions = append(actions, FileAction{Path: qualPath, Action: "ERROR: " + wErr.Error()})
				continue
			}
			writeBaseline(dstPath, srcData)
			cleanPending(dstPath)
			actions = append(actions, FileAction{Path: qualPath, Action: "CREATE", Location: "vault"})
			continue
		}

		// .pinned → always skip
		if _, pinErr := os.Stat(dstPath + ".pinned"); pinErr == nil {
			continue
		}

		dstData, err := os.ReadFile(dstPath)
		if err != nil {
			continue
		}
		baselineData, hasBaseline := readBaseline(dstPath)

		tmpl := bytes.TrimSpace(srcData)
		proj := bytes.TrimSpace(dstData)
		base := bytes.TrimSpace(baselineData)

		if !hasBaseline {
			// Legacy file from before baseline tracking.
			if bytes.Equal(tmpl, proj) {
				// Identical — backfill .baseline silently
				if !dryRun {
					writeBaseline(dstPath, srcData)
					cleanPending(dstPath)
				}
				continue
			}
			// Different and no baseline — treat as user-customized.
			// With --force, overwrite anyway.
			if force {
				if !dryRun {
					if err := os.WriteFile(dstPath, srcData, 0o644); err != nil {
						actions = append(actions, FileAction{Path: qualPath, Action: "ERROR: " + err.Error()})
						continue
					}
					writeBaseline(dstPath, srcData)
					cleanPending(dstPath)
				}
				actions = append(actions, FileAction{Path: qualPath, Action: "UPDATE", Location: "vault"})
				continue
			}
			actions = append(actions, FileAction{Path: qualPath, Action: "CUSTOMIZED", Location: "vault"})
			continue
		}

		// Three-way comparison with baseline.
		if bytes.Equal(tmpl, base) {
			// Template unchanged since last sync — nothing to do.
			// (User may have customized, but template hasn't changed.)
			if !dryRun {
				cleanPending(dstPath)
			}
			continue
		}

		// Template changed since last sync.
		if bytes.Equal(proj, base) {
			// User hasn't touched the file → safe to auto-update.
			if dryRun {
				actions = append(actions, FileAction{Path: qualPath, Action: "DRY-RUN", Location: "vault"})
				continue
			}
			if err := os.WriteFile(dstPath, srcData, 0o644); err != nil {
				actions = append(actions, FileAction{Path: qualPath, Action: "ERROR: " + err.Error()})
				continue
			}
			writeBaseline(dstPath, srcData)
			cleanPending(dstPath)
			actions = append(actions, FileAction{Path: qualPath, Action: "UPDATE", Location: "vault"})
			continue
		}

		// Both template and user changed → conflict.
		if force {
			if !dryRun {
				if err := os.WriteFile(dstPath, srcData, 0o644); err != nil {
					actions = append(actions, FileAction{Path: qualPath, Action: "ERROR: " + err.Error()})
					continue
				}
				writeBaseline(dstPath, srcData)
				cleanPending(dstPath)
			}
			actions = append(actions, FileAction{Path: qualPath, Action: "UPDATE", Location: "vault"})
			continue
		}
		if !dryRun {
			cleanPending(dstPath)
		}
		actions = append(actions, FileAction{Path: qualPath, Action: "CONFLICT", Location: "vault"})
	}
	return actions
}

// isSidecar returns true for .pending, .pinned, and .baseline sidecar files.
func isSidecar(name string) bool {
	return strings.HasSuffix(name, ".pending") ||
		strings.HasSuffix(name, ".pinned") ||
		strings.HasSuffix(name, ".baseline")
}

// writeBaseline writes a .baseline sidecar with TrimSpace-normalized content.
func writeBaseline(dstPath string, data []byte) {
	_ = os.WriteFile(dstPath+".baseline", bytes.TrimSpace(data), 0o644)
}

// readBaseline reads the .baseline sidecar for a file.
// Returns the content and true if the baseline exists, nil and false otherwise.
func readBaseline(dstPath string) ([]byte, bool) {
	data, err := os.ReadFile(dstPath + ".baseline")
	if err != nil {
		return nil, false
	}
	return data, true
}

// cleanPending removes a legacy .pending sidecar if it exists.
func cleanPending(dstPath string) {
	os.Remove(dstPath + ".pending")
}

// propagateDir handles propagation of a directory entry (e.g., a skill directory).
// Directories use content-change detection: update only if template differs.
func propagateDir(templatesDir, projectSubDir, subdir, name string, dryRun bool) []FileAction {
	srcDir := filepath.Join(templatesDir, name)
	dstDir := filepath.Join(projectSubDir, name)

	// Check for .pinned marker
	if _, err := os.Stat(dstDir + ".pinned"); err == nil {
		return nil
	}

	if _, err := os.Stat(dstDir); err == nil {
		// Directory exists — update only if template content differs
		if !dirContentsChanged(srcDir, dstDir) {
			return nil
		}
		if dryRun {
			return []FileAction{{
				Path:     subdir + "/" + name,
				Action:   "DRY-RUN",
				Location: "vault",
			}}
		}
		dirActions, err := copyDir(srcDir, dstDir, true)
		if err != nil {
			return []FileAction{{
				Path:   subdir + "/" + name,
				Action: "ERROR: " + err.Error(),
			}}
		}
		result := []FileAction{{
			Path:     subdir + "/" + name,
			Action:   "UPDATE",
			Location: "vault",
		}}
		return append(result, convertDirActions(dirActions, subdir+"/"+name)...)
	}

	// Directory doesn't exist — create it
	if dryRun {
		return []FileAction{{
			Path:     subdir + "/" + name,
			Action:   "DRY-RUN",
			Location: "vault",
		}}
	}
	dirActions, err := copyDir(srcDir, dstDir, false)
	if err != nil {
		return []FileAction{{
			Path:   subdir + "/" + name,
			Action: "ERROR: " + err.Error(),
		}}
	}
	result := []FileAction{{
		Path:     subdir + "/" + name,
		Action:   "CREATE",
		Location: "vault",
	}}
	return append(result, convertDirActions(dirActions, subdir+"/"+name)...)
}

// convertDirActions prefixes copyDir actions with a base path for reporting.
func convertDirActions(actions []FileAction, base string) []FileAction {
	var result []FileAction
	for _, a := range actions {
		result = append(result, FileAction{
			Path:     base + "/" + a.Path,
			Action:   a.Action,
			Location: "vault",
		})
	}
	return result
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

// deployCommandsToRepo is a backward-compatible wrapper.
func deployCommandsToRepo(repoPath, agentctxPath string) []FileAction {
	return deploySubdirToRepo(repoPath, agentctxPath, "commands")
}

// deploySubdirToRepo copies vault agentctx/{subdir}/ to repo .claude/{subdir}/.
// This overwrites repo-side content on every sync (vault is canonical).
// For "commands", only .md files are deployed. For other subdirs, all
// non-sidecar files and directories are deployed.
func deploySubdirToRepo(repoPath, agentctxPath, subdir string) []FileAction {
	vaultSubDir := filepath.Join(agentctxPath, subdir)
	entries, err := os.ReadDir(vaultSubDir)
	if err != nil {
		return nil
	}

	repoSubDir := filepath.Join(repoPath, ".claude", subdir)
	if err := os.MkdirAll(repoSubDir, 0o755); err != nil {
		return nil
	}

	var actions []FileAction
	for _, e := range entries {
		// Directory entries — recursively deploy (always overwrite, vault is canonical)
		if e.IsDir() {
			srcDir := filepath.Join(vaultSubDir, e.Name())
			dstDir := filepath.Join(repoSubDir, e.Name())
			changed := dirContentsChanged(srcDir, dstDir)
			if !changed {
				continue // skip — repo already has identical content
			}
			if _, err := copyDir(srcDir, dstDir, true); err != nil {
				continue
			}
			actions = append(actions, FileAction{
				Path:     ".claude/" + subdir + "/" + e.Name(),
				Action:   "UPDATE",
				Location: "repo",
			})
			continue
		}
		// For commands: only .md files. For other subdirs: all non-sidecar files.
		if subdir == "commands" && !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		// Skip sidecar files (.pending, .pinned, .baseline)
		if isSidecar(e.Name()) {
			continue
		}
		srcPath := filepath.Join(vaultSubDir, e.Name())
		dstPath := filepath.Join(repoSubDir, e.Name())
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
			Path:     ".claude/" + subdir + "/" + e.Name(),
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

// dirContentsChanged walks src and dst and returns true if any file differs
// or is missing in dst. Used to avoid no-op UPDATE messages during deploy.
func dirContentsChanged(src, dst string) bool {
	changed := false
	_ = filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			changed = true
			return filepath.SkipAll
		}
		dstPath := filepath.Join(dst, rel)
		srcData, err := os.ReadFile(path)
		if err != nil {
			changed = true
			return filepath.SkipAll
		}
		dstData, err := os.ReadFile(dstPath)
		if err != nil || !bytes.Equal(srcData, dstData) {
			changed = true
			return filepath.SkipAll
		}
		return nil
	})
	return changed
}

// migrate5to6 force-updates vault templates with improved restart instructions
// (descriptive task slugs and auto-retirement).
func migrate5to6(ctx MigrationContext) ([]FileAction, error) {
	return forceUpdateVaultTemplates(ctx.VaultPath), nil
}

// migrate6to7 propagates skills from vault templates to all projects.
func migrate6to7(ctx MigrationContext) ([]FileAction, error) {
	actions := propagateSharedSubdir(ctx.VaultPath, ctx.AgentctxPath, "skills", false, true) // force=true
	if ctx.RepoPath != "" {
		actions = append(actions, deploySubdirToRepo(ctx.RepoPath, ctx.AgentctxPath, "skills")...)
	}
	return actions, nil
}

// migrate7to8 level-sets all projects: force-updates vault templates from Go
// embeds and overwrites all non-pinned project files with baselines.
// This establishes the .baseline tracking needed for three-way sync.
func migrate7to8(ctx MigrationContext) ([]FileAction, error) {
	// Vault templates were already refreshed at the top of syncProject.
	// Force-propagate to this project and write .baseline files.
	var actions []FileAction
	for _, sub := range propagateDirs {
		subActions := propagateSharedSubdir(ctx.VaultPath, ctx.AgentctxPath, sub, false, true) // force=true
		actions = append(actions, subActions...)
		if ctx.RepoPath != "" {
			actions = append(actions, deploySubdirToRepo(ctx.RepoPath, ctx.AgentctxPath, sub)...)
		}
	}
	return actions, nil
}

// forceUpdateVaultTemplates overwrites vault Templates/agentctx/ with embedded
// defaults. Used during schema migrations to push updated template content.
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
