// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/identity"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// claudeSubdirs are the .claude/ subdirectories symlinked through agentctx.
// Each becomes .claude/{name} → ../agentctx/{name}.
var claudeSubdirs = []string{"commands", "rules", "skills", "agents"}

// Opts holds options for Init and Migrate.
type Opts struct {
	Project string // override auto-detected project name
	Force   bool   // overwrite existing files
}

// FileAction describes what happened to a file.
type FileAction struct {
	Path     string // relative or display path
	Action   string // "CREATE", "SKIP", "UPDATE", "DRY-RUN", "ERROR", "OUTDATED"
	Location string // "vault", "repo", or "" (for migrations/meta actions)
}

// InitResult holds the outcome of Init.
type InitResult struct {
	Project string
	Actions []FileAction
}

// MigrateResult holds the outcome of Migrate.
type MigrateResult struct {
	Project string
	Actions []FileAction
}

// Init scaffolds vault-resident context for a project and writes repo-side
// bootstrap files (CLAUDE.md, .claude/commands/).
//
// Vault layout:
//
//	Projects/{project}/agentctx/       — all AI context for this project
//	  CLAUDE.md                        — MCP-first instructions
//	  workflow.md                      — behavioral rules and workflow standards
//	  resume.md                        — project state
//	  iterations.md                    — iteration history
//	  commands/{restart,wrap}.md       — slash commands
//	  tasks/, tasks/done/              — task tracking
//	Projects/{project}/sessions/       — auto-generated (by vv hook)
//	Projects/{project}/history.md      — auto-generated (by vv index)
//
// Repo layout:
//
//	CLAUDE.md                          — regular file (MCP-first instructions)
//	.claude/{commands,rules,skills,agents}/ — regular directories with files
func Init(cfg config.Config, cwd string, opts Opts) (*InitResult, error) {
	if err := validateVault(cfg.VaultPath); err != nil {
		return nil, err
	}

	project, err := resolveProject(cwd, opts.Project)
	if err != nil {
		return nil, err
	}

	result := &InitResult{Project: project}
	agentctx := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx")

	// Ensure vault templates exist (seeds Templates/agentctx/ if missing)
	EnsureVaultTemplates(cfg.VaultPath)

	// Vault-side directories
	for _, dir := range []string{
		agentctx,
		filepath.Join(agentctx, "commands"),
		filepath.Join(agentctx, "rules"),
		filepath.Join(agentctx, "skills"),
		filepath.Join(agentctx, "agents"),
		filepath.Join(agentctx, "tasks"),
		filepath.Join(agentctx, "tasks", "done"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Vault-side templates (inside agentctx/) — resolve from vault templates first
	vars := DefaultVars(project)
	vaultFiles := []string{
		"CLAUDE.md",
		"workflow.md",
		"resume.md",
		"iterations.md",
		"commands/restart.md",
		"commands/wrap.md",
		"commands/license.md",
		"commands/makefile.md",
	}
	for _, rel := range vaultFiles {
		content := resolveTemplate(cfg.VaultPath, rel, vars)
		path := filepath.Join(agentctx, rel)
		action := safeWrite(path, content, opts.Force)
		result.Actions = append(result.Actions, FileAction{
			Path:     filepath.Join("Projects", project, "agentctx", rel),
			Action:   action,
			Location: "vault",
		})
	}

	// Write project config overlay template
	projCfgPath := filepath.Join(agentctx, "config.toml")
	cfgAction := safeWrite(projCfgPath, config.ProjectConfigTemplate(), opts.Force)
	result.Actions = append(result.Actions, FileAction{
		Path:     filepath.Join("Projects", project, "agentctx", "config.toml"),
		Action:   cfgAction,
		Location: "vault",
	})

	// Write .version file
	vf := newVersionFile(LatestSchemaVersion)
	if err := WriteVersion(agentctx, vf); err != nil {
		return nil, fmt.Errorf("write .version: %w", err)
	}
	result.Actions = append(result.Actions, FileAction{
		Path:     filepath.Join("Projects", project, "agentctx", ".version"),
		Action:   "CREATE",
		Location: "vault",
	})

	// Propagate shared content (skills, etc.) from vault Templates to project agentctx
	for _, sub := range propagateDirs {
		subActions := propagateSharedSubdir(cfg.VaultPath, agentctx, sub, false, false)
		result.Actions = append(result.Actions, subActions...)
	}

	// Repo-side CLAUDE.md as regular file
	claudeMDPath := filepath.Join(cwd, "CLAUDE.md")
	claudeContent := resolveTemplate(cfg.VaultPath, "CLAUDE.md", vars)
	claudeMDAction := safeWrite(claudeMDPath, claudeContent, opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: "CLAUDE.md", Action: claudeMDAction, Location: "repo"})

	// Repo-side commit.msg as regular file
	commitMsgVault := filepath.Join(agentctx, "commit.msg")
	safeWrite(commitMsgVault, "", false) // ensure vault-side file exists
	commitMsgPath := filepath.Join(cwd, "commit.msg")
	cmAction := safeWrite(commitMsgPath, "", opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: "commit.msg", Action: cmAction, Location: "repo"})

	// Repo-side .claude/ directory with real subdirectories
	dotClaude := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dotClaude, 0o755); err != nil {
		return nil, fmt.Errorf("create .claude/: %w", err)
	}
	for _, sub := range claudeSubdirs {
		subDir := filepath.Join(dotClaude, sub)
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			return nil, fmt.Errorf("create .claude/%s: %w", sub, err)
		}
		// Deploy vault content to repo
		vaultSubDir := filepath.Join(agentctx, sub)
		entries, _ := os.ReadDir(vaultSubDir)
		for _, e := range entries {
			if e.IsDir() {
				if _, cpErr := copyDir(filepath.Join(vaultSubDir, e.Name()), filepath.Join(subDir, e.Name()), opts.Force); cpErr != nil {
					return nil, fmt.Errorf("copy %s/%s to .claude: %w", sub, e.Name(), cpErr)
				}
				continue
			}
			// For commands: only .md files. For other subdirs: all non-sidecar files.
			if sub == "commands" && !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if isSidecar(e.Name()) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(vaultSubDir, e.Name()))
			if err != nil {
				continue
			}
			safeWrite(filepath.Join(subDir, e.Name()), string(data), opts.Force)
		}
		result.Actions = append(result.Actions, FileAction{
			Path:     ".claude/" + sub,
			Action:   "CREATE",
			Location: "repo",
		})
	}

	// .gitignore (no /agentctx entry needed)
	for _, entry := range []string{"/CLAUDE.md", "/commit.msg"} {
		giAction, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), entry)
		if err != nil {
			return nil, err
		}
		if giAction != "" {
			result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction, Location: "repo"})
		}
	}

	// Repo-side .vibe-vault.toml identity file (commented-out template)
	idPath := filepath.Join(cwd, identity.FileName())
	idAction := safeWrite(idPath, identity.Template(project), opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: identity.FileName(), Action: idAction, Location: "repo"})

	return result, nil
}

// Migrate copies existing local context files to the vault's agentctx/
// directory, then performs the same repo-side updates as Init.
func Migrate(cfg config.Config, cwd string, opts Opts) (*MigrateResult, error) {
	if err := validateVault(cfg.VaultPath); err != nil {
		return nil, err
	}

	project, err := resolveProject(cwd, opts.Project)
	if err != nil {
		return nil, err
	}

	result := &MigrateResult{Project: project}
	agentctx := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx")

	// Ensure vault templates exist
	EnsureVaultTemplates(cfg.VaultPath)

	// Ensure vault dirs exist
	for _, dir := range []string{
		agentctx,
		filepath.Join(agentctx, "commands"),
		filepath.Join(agentctx, "rules"),
		filepath.Join(agentctx, "skills"),
		filepath.Join(agentctx, "agents"),
		filepath.Join(agentctx, "tasks"),
		filepath.Join(agentctx, "tasks", "done"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Copy local files to vault agentctx/
	fileMigrations := []struct {
		src string // relative to cwd
		dst string // relative to agentctx
	}{
		{"RESUME.md", "resume.md"},
		{"HISTORY.md", "iterations.md"},
	}
	for _, m := range fileMigrations {
		srcPath := filepath.Join(cwd, m.src)
		dstPath := filepath.Join(agentctx, m.dst)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			result.Actions = append(result.Actions, FileAction{
				Path:   m.src + " → " + filepath.Join("Projects", project, "agentctx", m.dst),
				Action: "SKIP",
			})
			continue
		}
		action, err := copyFileAction(srcPath, dstPath, opts.Force)
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", m.src, err)
		}
		result.Actions = append(result.Actions, FileAction{
			Path:   m.src + " → " + filepath.Join("Projects", project, "agentctx", m.dst),
			Action: action,
		})
	}

	// Copy tasks/ directory
	srcTasks := filepath.Join(cwd, "tasks")
	dstTasks := filepath.Join(agentctx, "tasks")
	if info, err := os.Stat(srcTasks); err == nil && info.IsDir() {
		actions, err := copyDir(srcTasks, dstTasks, opts.Force)
		if err != nil {
			return nil, fmt.Errorf("copy tasks/: %w", err)
		}
		for _, a := range actions {
			result.Actions = append(result.Actions, FileAction{
				Path:   "tasks/" + a.Path + " → " + filepath.Join("Projects", project, "agentctx", "tasks", a.Path),
				Action: a.Action,
			})
		}
	} else {
		result.Actions = append(result.Actions, FileAction{
			Path:   "tasks/ → " + filepath.Join("Projects", project, "agentctx", "tasks/"),
			Action: "SKIP",
		})
	}

	// Copy local commands to vault agentctx/commands/ (if they're regular files)
	// then remove the directory so it can be replaced with a symlink.
	localCmds := filepath.Join(cwd, ".claude", "commands")
	if info, err := os.Lstat(localCmds); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		entries, _ := os.ReadDir(localCmds)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			srcPath := filepath.Join(localCmds, entry.Name())
			// Skip symlinks — they're already vault-managed
			if fi, err := os.Lstat(srcPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				continue
			}
			dstPath := filepath.Join(agentctx, "commands", entry.Name())
			action, err := copyFileAction(srcPath, dstPath, opts.Force)
			if err != nil {
				return nil, fmt.Errorf("copy command %s: %w", entry.Name(), err)
			}
			result.Actions = append(result.Actions, FileAction{
				Path:   ".claude/commands/" + entry.Name() + " → " + filepath.Join("Projects", project, "agentctx", "commands", entry.Name()),
				Action: action,
			})
		}
		// Remove the directory (and any remaining files) so it can be
		// replaced with a symlink to agentctx/commands/
		os.RemoveAll(localCmds)
	}

	// Write agentctx/workflow.md (behavioral rules)
	vars := DefaultVars(project)
	workflowContent := resolveTemplate(cfg.VaultPath, "workflow.md", vars)
	workflowPath := filepath.Join(agentctx, "workflow.md")
	safeWrite(workflowPath, workflowContent, true)
	result.Actions = append(result.Actions, FileAction{
		Path:     filepath.Join("Projects", project, "agentctx", "workflow.md"),
		Action:   "UPDATE",
		Location: "vault",
	})

	// Write agentctx/CLAUDE.md
	claudeContent := resolveTemplate(cfg.VaultPath, "CLAUDE.md", vars)
	claudePath := filepath.Join(agentctx, "CLAUDE.md")
	safeWrite(claudePath, claudeContent, true)
	result.Actions = append(result.Actions, FileAction{
		Path:     filepath.Join("Projects", project, "agentctx", "CLAUDE.md"),
		Action:   "UPDATE",
		Location: "vault",
	})

	// Force-update vault-side commands (only if not already present from local copy)
	for _, cmd := range []string{
		"commands/restart.md",
		"commands/wrap.md",
		"commands/license.md",
		"commands/makefile.md",
	} {
		content := resolveTemplate(cfg.VaultPath, cmd, vars)
		path := filepath.Join(agentctx, cmd)
		// Write default only if file doesn't exist (local copy takes precedence)
		action := safeWrite(path, content, false)
		if action != "SKIP" {
			result.Actions = append(result.Actions, FileAction{
				Path:     filepath.Join("Projects", project, "agentctx", cmd),
				Action:   action,
				Location: "vault",
			})
		}
	}

	// Write .version file
	vf := newVersionFile(LatestSchemaVersion)
	if err := WriteVersion(agentctx, vf); err != nil {
		return nil, fmt.Errorf("write .version: %w", err)
	}

	// Repo-side CLAUDE.md as regular file
	claudeMDPath := filepath.Join(cwd, "CLAUDE.md")
	// Remove existing symlink before writing regular file
	if info, err := os.Lstat(claudeMDPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		os.Remove(claudeMDPath)
	}
	safeWrite(claudeMDPath, claudeContent, true)
	result.Actions = append(result.Actions, FileAction{Path: "CLAUDE.md", Action: "UPDATE", Location: "repo"})

	// Force-update repo-side .claude/ as real directories
	dotClaude := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dotClaude, 0o755); err != nil {
		return nil, fmt.Errorf("create .claude/: %w", err)
	}
	for _, sub := range claudeSubdirs {
		subPath := filepath.Join(dotClaude, sub)
		// Remove symlink if present
		if info, err := os.Lstat(subPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			os.Remove(subPath)
		}
		_ = os.MkdirAll(subPath, 0o755)
		// Deploy vault content
		vaultSubDir := filepath.Join(agentctx, sub)
		entries, _ := os.ReadDir(vaultSubDir)
		for _, e := range entries {
			if e.IsDir() {
				if _, cpErr := copyDir(filepath.Join(vaultSubDir, e.Name()), filepath.Join(subPath, e.Name()), true); cpErr != nil {
					return nil, fmt.Errorf("copy %s/%s to agentctx: %w", sub, e.Name(), cpErr)
				}
				continue
			}
			// For commands: only .md files. For other subdirs: all non-sidecar files.
			if sub == "commands" && !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if isSidecar(e.Name()) {
				continue
			}
			data, readErr := os.ReadFile(filepath.Join(vaultSubDir, e.Name()))
			if readErr != nil {
				continue
			}
			_ = os.WriteFile(filepath.Join(subPath, e.Name()), data, 0o644)
		}
		result.Actions = append(result.Actions, FileAction{
			Path:     ".claude/" + sub,
			Action:   "UPDATE",
			Location: "repo",
		})
	}

	// Repo-side commit.msg as regular file
	commitMsgVault := filepath.Join(agentctx, "commit.msg")
	safeWrite(commitMsgVault, "", false) // ensure vault-side file exists
	commitMsgPath := filepath.Join(cwd, "commit.msg")
	// Remove symlink if present
	if info, err := os.Lstat(commitMsgPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		os.Remove(commitMsgPath)
	}
	safeWrite(commitMsgPath, "", true)
	result.Actions = append(result.Actions, FileAction{Path: "commit.msg", Action: "UPDATE", Location: "repo"})

	// Remove agentctx symlink if present (migration from v4)
	agentctxLink := filepath.Join(cwd, "agentctx")
	if info, err := os.Lstat(agentctxLink); err == nil && info.Mode()&os.ModeSymlink != 0 {
		os.Remove(agentctxLink)
	}

	// .gitignore (no /agentctx entry needed)
	for _, entry := range []string{"/CLAUDE.md", "/commit.msg"} {
		giAction, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), entry)
		if err != nil {
			return nil, err
		}
		if giAction != "" {
			result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction, Location: "repo"})
		}
	}

	// Repo-side .vibe-vault.toml identity file (commented-out template)
	idPath := filepath.Join(cwd, identity.FileName())
	idAction := safeWrite(idPath, identity.Template(project), opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: identity.FileName(), Action: idAction, Location: "repo"})

	return result, nil
}

// --- helpers ---

func validateVault(vaultPath string) error {
	info, err := os.Stat(vaultPath)
	if err != nil {
		return fmt.Errorf("vault not found at %s: %w", vaultPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("vault path %s is not a directory", vaultPath)
	}
	return nil
}

// ResolveProjectPublic resolves the project name from the working directory.
func ResolveProjectPublic(cwd string) (string, error) {
	return resolveProject(cwd, "")
}

func resolveProject(cwd, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	project := session.DetectProject(cwd)
	if project == "_unknown" {
		return "", fmt.Errorf("could not detect project name from %s; use --project to specify", cwd)
	}
	return project, nil
}

// safeWrite writes content to path. Returns "CREATE" if written, "SKIP" if
// file exists and force is false, "CREATE" (overwrite) if force is true.
func safeWrite(path, content string, force bool) string {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return "SKIP"
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "SKIP"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "SKIP"
	}
	return "CREATE"
}

// safeSymlink creates a symlink at linkPath pointing to target.
// Returns "CREATE" if created, "SKIP" if it already exists and force is false,
// "CREATE" (overwrite) if force is true. Handles existing files, symlinks, and
// directories (empty directories are removed; non-empty directories are skipped
// to avoid data loss — use migrate to move contents first).
func safeSymlink(linkPath, target string, force bool) string {
	if !force {
		if _, err := os.Lstat(linkPath); err == nil {
			return "SKIP"
		}
	}
	// Remove existing file, symlink, or empty directory before creating
	if info, err := os.Lstat(linkPath); err == nil {
		if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			// Real directory — only remove if empty
			if err := os.Remove(linkPath); err != nil {
				return "SKIP" // non-empty dir, don't destroy contents
			}
		} else {
			os.Remove(linkPath)
		}
	}
	if err := os.Symlink(target, linkPath); err != nil {
		return "SKIP"
	}
	return "CREATE"
}

// gitignoreEnsure appends entry to gitignore if not already present.
// Returns "UPDATE" if modified, "" if already present or on error.
func gitignoreEnsure(giPath, entry string) (string, error) {
	data, err := os.ReadFile(giPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read .gitignore: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	bare := strings.TrimPrefix(entry, "/")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == entry || trimmed == bare {
			return "", nil
		}
	}

	// Append
	f, err := os.OpenFile(giPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("open .gitignore: %w", err)
	}
	defer f.Close()

	// Add newline before entry if file doesn't end with one
	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	if _, err := fmt.Fprintf(f, "%s%s\n", prefix, entry); err != nil {
		return "", fmt.Errorf("write .gitignore: %w", err)
	}

	return "UPDATE", nil
}

// copyFileAction copies src to dst. Returns action and error.
func copyFileAction(src, dst string, force bool) (string, error) {
	if !force {
		if _, err := os.Stat(dst); err == nil {
			return "SKIP", nil
		}
	}
	if err := copyFile(src, dst); err != nil {
		return "", err
	}
	return "MIGRATE", nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()

	df, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer df.Close()

	if _, err := io.Copy(df, sf); err != nil {
		return err
	}
	return nil
}

// copyDir copies files from src to dst recursively. Returns actions for each file.
func copyDir(src, dst string, force bool) ([]FileAction, error) {
	var actions []FileAction

	return actions, filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		dstPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		action, err := copyFileAction(path, dstPath, force)
		if err != nil {
			return err
		}
		actions = append(actions, FileAction{Path: rel, Action: action})
		return nil
	})
}
