// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/session"
)

// Opts holds options for Init and Migrate.
type Opts struct {
	Project string // override auto-detected project name
	Force   bool   // overwrite existing files
}

// FileAction describes what happened to a file.
type FileAction struct {
	Path   string // relative or display path
	Action string // "CREATE", "SKIP", "MIGRATE", "UPDATE"
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
//	CLAUDE.md                          — thin pointer to agentctx
//	.claude/commands/                  — symlink to agentctx/commands/
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
		filepath.Join(agentctx, "tasks"),
		filepath.Join(agentctx, "tasks", "done"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Vault-side templates (inside agentctx/) — resolve from vault templates first
	vars := DefaultVars(project)
	vaultFiles := []struct {
		rel      string
		fallback func() string
	}{
		{"workflow.md", func() string { return generateWorkflowMD(project) }},
		{"resume.md", func() string { return generateResume(project) }},
		{"iterations.md", func() string { return generateIterations(project) }},
		{"commands/restart.md", generateRestartMD},
		{"commands/wrap.md", generateWrapMD},
		{"commands/license.md", generateLicenseMD},
		{"commands/makefile.md", generateMakefileMD},
	}
	for _, f := range vaultFiles {
		content := resolveTemplate(cfg.VaultPath, f.rel, vars, f.fallback)
		path := filepath.Join(agentctx, f.rel)
		action := safeWrite(path, content, opts.Force)
		result.Actions = append(result.Actions, FileAction{
			Path:   filepath.Join("Projects", project, "agentctx", f.rel),
			Action: action,
		})
	}

	// Write project config overlay template
	projCfgPath := filepath.Join(agentctx, "config.toml")
	cfgAction := safeWrite(projCfgPath, config.ProjectConfigTemplate(), opts.Force)
	result.Actions = append(result.Actions, FileAction{
		Path:   filepath.Join("Projects", project, "agentctx", "config.toml"),
		Action: cfgAction,
	})

	// Write .version file
	vf := newVersionFile(LatestSchemaVersion)
	if err := WriteVersion(agentctx, vf); err != nil {
		return nil, fmt.Errorf("write .version: %w", err)
	}
	result.Actions = append(result.Actions, FileAction{
		Path:   filepath.Join("Projects", project, "agentctx", ".version"),
		Action: "CREATE",
	})

	// Repo-side agentctx symlink → vault agentctx path
	agentctxLink := filepath.Join(cwd, "agentctx")
	aLinkAction := safeSymlink(agentctxLink, agentctx, opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: "agentctx", Action: aLinkAction})

	// Repo-side CLAUDE.md (thin pointer, relative paths only)
	claudeMDPath := filepath.Join(cwd, "CLAUDE.md")
	action := safeWrite(claudeMDPath, generateClaudeMD(), opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: "CLAUDE.md", Action: action})

	// Repo-side commit.msg → symlink through agentctx
	commitMsgVault := filepath.Join(agentctx, "commit.msg")
	safeWrite(commitMsgVault, "", false) // ensure vault-side file exists
	commitMsgLink := filepath.Join(cwd, "commit.msg")
	cmAction := safeSymlink(commitMsgLink, filepath.Join("agentctx", "commit.msg"), opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: "commit.msg", Action: cmAction})

	// Repo-side .claude/commands/ → relative symlink through agentctx
	dotClaude := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dotClaude, 0o755); err != nil {
		return nil, fmt.Errorf("create .claude/: %w", err)
	}
	cmdsLink := filepath.Join(dotClaude, "commands")
	cmdsTarget := filepath.Join("..", "agentctx", "commands")
	linkAction := safeSymlink(cmdsLink, cmdsTarget, opts.Force)
	result.Actions = append(result.Actions, FileAction{
		Path:   ".claude/commands",
		Action: linkAction,
	})

	// .gitignore
	for _, entry := range []string{"/CLAUDE.md", "/commit.msg", "/agentctx"} {
		giAction, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), entry)
		if err != nil {
			return nil, err
		}
		if giAction != "" {
			result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction})
		}
	}

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
	workflowContent := resolveTemplate(cfg.VaultPath, "workflow.md", vars, func() string { return generateWorkflowMD(project) })
	workflowPath := filepath.Join(agentctx, "workflow.md")
	safeWrite(workflowPath, workflowContent, true)
	result.Actions = append(result.Actions, FileAction{
		Path:   filepath.Join("Projects", project, "agentctx", "workflow.md"),
		Action: "UPDATE",
	})

	// Force-update vault-side commands (only if not already present from local copy)
	for _, cmd := range []struct {
		name     string
		fallback func() string
	}{
		{"restart.md", generateRestartMD},
		{"wrap.md", generateWrapMD},
		{"license.md", generateLicenseMD},
		{"makefile.md", generateMakefileMD},
	} {
		content := resolveTemplate(cfg.VaultPath, "commands/"+cmd.name, vars, cmd.fallback)
		path := filepath.Join(agentctx, "commands", cmd.name)
		// Write default only if file doesn't exist (local copy takes precedence)
		action := safeWrite(path, content, false)
		if action != "SKIP" {
			result.Actions = append(result.Actions, FileAction{
				Path:   filepath.Join("Projects", project, "agentctx", "commands", cmd.name),
				Action: action,
			})
		}
	}

	// Write .version file
	vf := newVersionFile(LatestSchemaVersion)
	if err := WriteVersion(agentctx, vf); err != nil {
		return nil, fmt.Errorf("write .version: %w", err)
	}

	// Repo-side agentctx symlink
	agentctxLink := filepath.Join(cwd, "agentctx")
	safeSymlink(agentctxLink, agentctx, true)
	result.Actions = append(result.Actions, FileAction{Path: "agentctx", Action: "UPDATE"})

	// Force-update repo-side CLAUDE.md (thin pointer, relative paths)
	claudeMDPath := filepath.Join(cwd, "CLAUDE.md")
	safeWrite(claudeMDPath, generateClaudeMD(), true)
	result.Actions = append(result.Actions, FileAction{Path: "CLAUDE.md", Action: "UPDATE"})

	// Force-update repo-side .claude/commands/ → relative symlink
	dotClaude := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dotClaude, 0o755); err != nil {
		return nil, fmt.Errorf("create .claude/: %w", err)
	}
	cmdsLink := filepath.Join(dotClaude, "commands")
	cmdsTarget := filepath.Join("..", "agentctx", "commands")
	safeSymlink(cmdsLink, cmdsTarget, true)
	result.Actions = append(result.Actions, FileAction{
		Path:   ".claude/commands",
		Action: "UPDATE",
	})

	// Repo-side commit.msg → symlink through agentctx
	commitMsgVault := filepath.Join(agentctx, "commit.msg")
	safeWrite(commitMsgVault, "", false) // ensure vault-side file exists
	commitMsgLink := filepath.Join(cwd, "commit.msg")
	safeSymlink(commitMsgLink, filepath.Join("agentctx", "commit.msg"), true)
	result.Actions = append(result.Actions, FileAction{Path: "commit.msg", Action: "UPDATE"})

	// .gitignore
	for _, entry := range []string{"/CLAUDE.md", "/commit.msg", "/agentctx"} {
		giAction, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), entry)
		if err != nil {
			return nil, err
		}
		if giAction != "" {
			result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction})
		}
	}

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

// --- template generators ---

// generateClaudeMD produces the thin repo-side CLAUDE.md that points to
// the vault's agentctx directory via the repo-root symlink. No absolute
// paths — works across machines.
func generateClaudeMD() string {
	return `# CLAUDE.md

Read agentctx/resume.md at session start for full context and behavioral rules.

Do not commit this file, commit.msg, or anything under .claude/.
`
}

// generateWorkflowMD produces the vault-side workflow.md that lives inside
// agentctx/ and contains the behavioral rules, workflow standards, and file
// pointers. This is what the AI actually reads for context.
func generateWorkflowMD(project string) string {
	return fmt.Sprintf(`# %s — Workflow

## Files

- **resume.md** — current project state, open threads, navigation (thin gateway)
- **iterations.md** — iteration narratives and project history (append-only archive)
- **tasks/** — active tasks; **tasks/done/** — completed
- **commands/** — slash commands (/restart, /wrap)
- **doc/** — stable project reference: architecture, design decisions, testing (source-controlled)

## Workflow Rules

- **Never commit without explicit human permission.** Stage files and
  update commit.msg freely, but the actual git commit requires human approval.
- **commit.msg is symlinked.** Write it once at the repo root — it
  resolves to the vault agentctx/ copy automatically.
- **Never commit AI context files.** CLAUDE.md, commit.msg, and anything
  under .claude/ are local-only.
- **Git commit messages are the project's history.** Write them to be
  clear, detailed, and self-sufficient.

## The Pair Programming Paradigm

### The AI's Role: Expert Implementation

- Expert coder with deep technical knowledge
- Investigate problems thoroughly BEFORE implementing fixes
- Present findings and action plans for review
- Implement solutions only after architectural approval

### The Human's Role: Architectural Vision

- Context that spans the entire project across many days and iterations
- Understanding of long-term maintainability goals
- Guide architectural decisions and approve implementation approaches

### Critical Anti-Pattern: Premature Implementation

Never jump to coding short-term fixes without investigation.

## Investigation-First Workflow

### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan immediately
- Write detailed specs upfront to reduce ambiguity

### 2. Subagent Strategy

- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- One tack per subagent for focused execution

### 3. Self-Improvement Loop

- After ANY correction from the user: update lessons with the pattern
- Write rules that prevent the same mistake
- Review lessons at session start

### 4. Verification Before Done

- Never mark a task complete without proving it works
- No task is "done" until the user says it is and does the actual commit
- Run tests, check logs, demonstrate correctness
- Ask yourself: "Would a staff engineer approve this?"
- No warnings or diagnostic messages in committed code

### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- Skip this for simple, obvious fixes — don't over-engineer

### 6. Autonomous Bug Fixing

- When given a bug report: generate a plan and review with the user
- Point at logs, errors, failing tests — then plan to resolve them
- Never fix a test without understanding the root cause

## Task Management

1. Write plan to tasks/<task_name>.md with checkable items
2. Check in before starting implementation
3. Track progress and explain changes at each step
4. Add review section to the task file
5. When complete: update and move to tasks/done/

## Core Principles

- **Simplicity First**: Make every change as simple as possible but no simpler
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary
- **Test Coverage**: Ensure close to 80%% unit test coverage for code changes

Read resume.md for current project state and open threads. Consult doc/ files
for stable reference material (architecture, design decisions, test inventory).
`, project)
}

func generateRestartMD() string {
	return `# Session Restart

Read the following files to restore full project context:

1. agentctx/workflow.md — behavioral rules and workflow standards
2. agentctx/resume.md — current project state and open threads (thin gateway)
3. agentctx/tasks/ — active task files, if any exist (skip tasks/done/)
4. Run ` + "`vv inject`" + ` via Bash to load live vault context (recent sessions,
   open threads, decisions, friction trends, knowledge). Include the
   full output verbatim in your context — do not summarize it.
5. doc/*.md — stable reference (architecture, design, testing) — read on demand when needed

After reading, briefly confirm what you loaded (test count, open tasks,
recent session activity from inject, what was last worked on) and ask
what to work on.
`
}

func generateWrapMD() string {
	return `Update resume.md and its dependent documents to reflect the current state.

resume.md is a THIN GATEWAY — not a diary. Keep it focused on current state,
open threads, and pointers. Stable reference material belongs in doc/ under
source control. Completed work details belong in iterations.md only.

Specifically:
- Ensure all code compiles without errors
- Ensure all unit and integration tests pass
- Update resume.md: current state (test count, iteration count), open threads.
  Do NOT add file inventories, architecture diagrams, design decisions, or
  module tables to resume.md — those belong in doc/ files
- If stable project documentation changed (architecture, design decisions,
  test structure), update the relevant doc/ file
- Append a new iteration narrative to iterations.md describing what changed
  in this session and why (past tense, technical detail)
- Retire completed tasks: check each file in agentctx/tasks/ (not tasks/done/)
  against the session's work — if a task has been implemented, update its
  status to "Done" and move it to tasks/done/
- Rewrite commit.msg to document all code changes made in this session
  (symlinked — write once at repo root, vault copy updates automatically)
- Stage all modified and newly added project files (use git add with explicit
  file paths — never use git add -A or git add .)

Do not add "Co-Authored-By" lines to commit messages or source files.

Do not ask for confirmation — just do the updates, stage the files, show what
changed, and note that the user should review before committing.
`
}

func generateLicenseMD() string {
	return `Add or update dual MIT/Apache-2.0 licensing for this project.

All rights attributed to: John Suykerbuyk <john@syketech.com> and SykeTech LTD

## Rules

Every step below MUST be idempotent — running this skill repeatedly produces
the identical result each time.  Never duplicate content, never append to
existing correct content.

## Step 1: Create/overwrite the single LICENSE file

Write a single ` + "`LICENSE`" + ` file in the project root containing, in order:

1. A preamble explaining dual licensing and the user's choice
2. ` + "`===`" + ` separator + full MIT license text (copyright line below)
3. ` + "`===`" + ` separator + full Apache 2.0 license text with copyright appendix

Copyright line (use the current year from the system):

    Copyright (c) YYYY John Suykerbuyk and SykeTech LTD

This file is canonical — always overwrite it entirely so the content matches
this template exactly.  There should be NO separate ` + "`LICENSE-MIT`" + ` or
` + "`LICENSE-APACHE`" + ` files; delete them if they exist.

## Step 2: Ask about source-file banners

Prompt the user:

> Should I add a small copyright + SPDX banner to the top of every source
> file that doesn't already have one?  (y/n)

If the user declines, stop here.

## Step 3: Add banners (idempotent)

### Banner templates by language

**C / C++ / Rust / Go / Java / JS / TS / CSS / SCSS** (line comments):
` + "```" + `
// Copyright (c) YYYY John Suykerbuyk and SykeTech LTD
// SPDX-License-Identifier: MIT OR Apache-2.0
` + "```" + `

**Python / Ruby / Shell / YAML / Toml / Makefile** (hash comments):
` + "```" + `
# Copyright (c) YYYY John Suykerbuyk and SykeTech LTD
# SPDX-License-Identifier: MIT OR Apache-2.0
` + "```" + `

**HTML / XML / SVG** (block comments):
` + "```" + `
<!-- Copyright (c) YYYY John Suykerbuyk and SykeTech LTD -->
<!-- SPDX-License-Identifier: MIT OR Apache-2.0 -->
` + "```" + `

**Lua / SQL** (double-dash):
` + "```" + `
-- Copyright (c) YYYY John Suykerbuyk and SykeTech LTD
-- SPDX-License-Identifier: MIT OR Apache-2.0
` + "```" + `

### File selection

Include:  all source files matching typical extensions for the detected language
(.h, .cpp, .c, .rs, .go, .py, .js, .ts, .java, .rb, .sh, .lua, .sql, etc.)

Exclude:
- vendor/, third_party/, node_modules/, target/, build/, dist/
- prototype/, .git/
- Generated files (*.pb.go, *_generated.*, etc.)
- Non-source files (LICENSE, README, Makefile unless it has project logic)

### Idempotency rules

1. If a file already starts with the exact banner (matching comment style and
   content), skip it — do not modify the file.
2. If a file has a banner with a **different year** or **different wording**,
   replace the old banner with the current one (same two lines, in place).
3. For files with a shebang (#!) on line 1, place the banner on lines 2-3
   (with a blank line between shebang and banner if not already present).
4. Always leave exactly one blank line between the banner and the first line
   of real code.

## Step 4: Validate

1. Confirm LICENSE exists and contains both license texts.
2. If banners were added, spot-check 2-3 files to confirm correct placement.
3. Build the project (if a build system exists) to confirm nothing broke.
4. Run tests (if they exist) to confirm nothing broke.

## Step 5: Report

Summarize:
- LICENSE file status (created / updated / unchanged)
- Number of files with banners added / updated / already correct
- Build + test results
`
}

func generateMakefileMD() string {
	return `Audit or create a Makefile facade for this project's native build system.

## Rules

1. **Discover** the native build system by checking for config files:
   - CMakeLists.txt -> CMake
   - Cargo.toml -> Cargo
   - go.mod -> Go
   - package.json -> Node (npm/yarn/pnpm)
   - meson.build -> Meson

2. **Read** any existing Makefile to preserve project-specific targets.

3. **Create or update** a Makefile with these standard targets:

   | Target           | Purpose                          | Safety    |
   |------------------|----------------------------------|-----------|
   | make             | Print help (.DEFAULT_GOAL)       | read-only |
   | make build       | Default build                    | mutates   |
   | make test        | Unit tests (builds first)        | mutates   |
   | make integration | Integration tests (builds first) | mutates   |
   | make install     | Install to PREFIX=~/.local       | mutates   |
   | make clean       | Remove build artifacts           | mutates   |

   Key constraint: **bare make MUST show help and NEVER mutate.** Use .DEFAULT_GOAL := help.

4. **Validate** by running make and confirming it only prints help (exit 0, no build side effects).

## Adaptation patterns

### CMake
` + "```makefile" + `
BUILD_DIR  ?= build
BUILD_TYPE ?= Release
PREFIX     ?= $(HOME)/.local
build:
	cmake -B $(BUILD_DIR) -G Ninja -DCMAKE_BUILD_TYPE=$(BUILD_TYPE) -DCMAKE_INSTALL_PREFIX=$(PREFIX)
	ninja -C $(BUILD_DIR)
test: build
	./$(BUILD_DIR)/*_tests "~[integration]~[benchmark]"
integration: build
	./$(BUILD_DIR)/*_tests "[integration]"
install: build
	cmake --install $(BUILD_DIR)
clean:
	rm -rf $(BUILD_DIR)
` + "```" + `

### Cargo
` + "```makefile" + `
build:
	cargo build --release
test:
	cargo test
integration:
	cargo test -- --ignored
install:
	cargo install --path .
clean:
	cargo clean
` + "```" + `

### Go
` + "```makefile" + `
build:
	go build ./...
test:
	go test ./...
integration:
	go test -tags=integration ./...
install:
	go install ./...
clean:
	go clean
` + "```" + `

### Node (npm)
` + "```makefile" + `
build:
	npm run build
test:
	npm test
install:
	npm ci
clean:
	rm -rf node_modules dist
` + "```" + `

### Meson
` + "```makefile" + `
BUILD_DIR ?= builddir
build:
	meson setup $(BUILD_DIR) --buildtype=release
	ninja -C $(BUILD_DIR)
test: build
	meson test -C $(BUILD_DIR)
install: build
	meson install -C $(BUILD_DIR)
clean:
	rm -rf $(BUILD_DIR)
` + "```" + `

## Help target template

The help target should list all targets, overridable variables, and include a "Quick start" recommendation pointing the user to the most common workflow (usually make build && make test).
`
}

func generateResume(project string) string {
	return fmt.Sprintf(`---
type: project-resume
project: %s
---

# %s — Working Context

<!-- KEEP THIS FILE THIN. resume.md is a gateway to project context, not a diary.
     - Stable architecture, design decisions, test inventories -> doc/ (source-controlled)
     - Completed iteration narratives -> iterations.md (append-only archive)
     - Active work items -> tasks/ directory
     Only current state, open threads, and pointers to deeper context belong here. -->

## What This Project Is

<!-- Brief description of the project, stack, build/test commands -->

## Current State

<!-- Iteration count, test count, what phase the project is in -->

## Open Threads

<!-- Active tasks, unresolved questions, next steps -->

## Reference Documents

| Document | Location | Purpose |
|----------|----------|---------|
| resume.md | agentctx/ | This file — current state and navigation |
| workflow.md | agentctx/ | AI workflow rules and pair programming paradigm |
| iterations.md | agentctx/ | Append-only archive of iteration narratives |
| tasks/ | agentctx/ | Active task files; tasks/done/ for completed |

<!-- Add doc/ entries as project documentation grows:
| ARCHITECTURE.md | doc/ | Data flow, module responsibilities |
| DESIGN.md | doc/ | Key design decisions with rationale |
| TESTING.md | doc/ | Test inventory and coverage |
-->
`, project, project)
}

func generateIterations(project string) string {
	return fmt.Sprintf(`---
type: project-iterations
project: %s
---

# %s — Iteration Narratives

<!-- Each iteration gets a section below, newest first.
     An "iteration" is a logical unit of work (feature, fix, refactor)
     that may span multiple sessions. -->
`, project, project)
}
