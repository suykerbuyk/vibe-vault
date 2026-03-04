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
	compressedVault := config.CompressHome(cfg.VaultPath)

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

	// Vault-side templates (inside agentctx/)
	vaultFiles := []struct {
		rel     string
		content string
	}{
		{"workflow.md", generateWorkflowMD(project)},
		{"resume.md", generateResume(project)},
		{"iterations.md", generateIterations(project)},
		{"commands/restart.md", generateRestartMD(compressedVault, project)},
		{"commands/wrap.md", generateWrapMD(compressedVault, project)},
	}
	for _, f := range vaultFiles {
		path := filepath.Join(agentctx, f.rel)
		action := safeWrite(path, f.content, opts.Force)
		result.Actions = append(result.Actions, FileAction{
			Path:   filepath.Join("Projects", project, "agentctx", f.rel),
			Action: action,
		})
	}

	// Repo-side CLAUDE.md (thin pointer to agentctx)
	claudeMDPath := filepath.Join(cwd, "CLAUDE.md")
	action := safeWrite(claudeMDPath, generateClaudeMD(compressedVault, project), opts.Force)
	result.Actions = append(result.Actions, FileAction{Path: "CLAUDE.md", Action: action})

	// Repo-side .claude/commands/ → symlink to agentctx/commands/
	dotClaude := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dotClaude, 0o755); err != nil {
		return nil, fmt.Errorf("create .claude/: %w", err)
	}
	cmdsLink := filepath.Join(dotClaude, "commands")
	cmdsTarget := filepath.Join(agentctx, "commands")
	linkAction := safeSymlink(cmdsLink, cmdsTarget, opts.Force)
	result.Actions = append(result.Actions, FileAction{
		Path:   ".claude/commands",
		Action: linkAction,
	})

	// .gitignore
	giAction, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), "CLAUDE.md")
	if err != nil {
		return nil, err
	}
	if giAction != "" {
		result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction})
	}
	giAction2, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), "commit.msg")
	if err != nil {
		return nil, err
	}
	if giAction2 != "" && giAction == "" {
		result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction2})
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
	compressedVault := config.CompressHome(cfg.VaultPath)

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
	migrations := []struct {
		src string // relative to cwd
		dst string // relative to agentctx
	}{
		{"RESUME.md", "resume.md"},
		{"HISTORY.md", "iterations.md"},
	}
	for _, m := range migrations {
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
	workflowPath := filepath.Join(agentctx, "workflow.md")
	safeWrite(workflowPath, generateWorkflowMD(project), true)
	result.Actions = append(result.Actions, FileAction{
		Path:   filepath.Join("Projects", project, "agentctx", "workflow.md"),
		Action: "UPDATE",
	})

	// Force-update vault-side commands (only if not already present from local copy)
	for _, cmd := range []struct {
		name    string
		content string
	}{
		{"restart.md", generateRestartMD(compressedVault, project)},
		{"wrap.md", generateWrapMD(compressedVault, project)},
	} {
		path := filepath.Join(agentctx, "commands", cmd.name)
		// Write default only if file doesn't exist (local copy takes precedence)
		action := safeWrite(path, cmd.content, false)
		if action != "SKIP" {
			result.Actions = append(result.Actions, FileAction{
				Path:   filepath.Join("Projects", project, "agentctx", "commands", cmd.name),
				Action: action,
			})
		}
	}

	// Force-update repo-side CLAUDE.md (thin pointer)
	claudeMDPath := filepath.Join(cwd, "CLAUDE.md")
	safeWrite(claudeMDPath, generateClaudeMD(compressedVault, project), true)
	result.Actions = append(result.Actions, FileAction{Path: "CLAUDE.md", Action: "UPDATE"})

	// Force-update repo-side .claude/commands/ → symlink to agentctx/commands/
	dotClaude := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(dotClaude, 0o755); err != nil {
		return nil, fmt.Errorf("create .claude/: %w", err)
	}
	cmdsLink := filepath.Join(dotClaude, "commands")
	cmdsTarget := filepath.Join(agentctx, "commands")
	safeSymlink(cmdsLink, cmdsTarget, true)
	result.Actions = append(result.Actions, FileAction{
		Path:   ".claude/commands",
		Action: "UPDATE",
	})

	// .gitignore
	giAction, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), "CLAUDE.md")
	if err != nil {
		return nil, err
	}
	if giAction != "" {
		result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction})
	}
	giAction2, err := gitignoreEnsure(filepath.Join(cwd, ".gitignore"), "commit.msg")
	if err != nil {
		return nil, err
	}
	if giAction2 != "" && giAction == "" {
		result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction2})
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
	for _, line := range lines {
		if strings.TrimSpace(line) == entry {
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
// the vault's agentctx directory. This is the only AI context file that
// lives in the source tree.
func generateClaudeMD(compressedVault, project string) string {
	return fmt.Sprintf(`# CLAUDE.md

All project context, behavioral rules, and workflow commands live in the vault:

  %s/Projects/%s/agentctx/

Read agentctx/workflow.md at session start for full context and behavioral rules.

Do not commit this file, commit.msg, or anything under .claude/.
`, compressedVault, project)
}

// generateWorkflowMD produces the vault-side workflow.md that lives inside
// agentctx/ and contains the behavioral rules, workflow standards, and file
// pointers. This is what the AI actually reads for context.
func generateWorkflowMD(project string) string {
	return fmt.Sprintf(`# %s — Workflow

## Files

- **resume.md** — project state, architecture, design decisions
- **iterations.md** — iteration narratives and project history
- **tasks/** — active tasks; **tasks/done/** — completed
- **commands/** — slash commands (/restart, /wrap)

## Workflow Rules

- **Never commit without explicit human permission.** Stage files and
  update commit.msg freely, but the actual git commit requires human approval.
- **Keep commit.msg in sync.** Always write commit.msg to both the repo
  root and the vault agentctx/ directory.
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

Read resume.md for current project state, architecture, and open threads.
`, project)
}

func generateRestartMD(compressedVault, project string) string {
	agentctx := fmt.Sprintf("%s/Projects/%s/agentctx", compressedVault, project)
	return fmt.Sprintf(`# Session Restart

Read the following files to restore full project context:

1. %s/workflow.md — behavioral rules and workflow standards
2. %s/resume.md — current project state, architecture, decisions
3. %s/tasks/ — active task files (skip tasks/done/)
4. Run `+"`vv inject`"+` via Bash to load live vault context (recent sessions,
   open threads, decisions, friction trends, knowledge). Include the
   full output verbatim in your context — do not summarize it.

After reading, briefly confirm what you loaded (test count, open tasks,
recent session activity from inject, what was last worked on) and ask
what to work on.
`, agentctx, agentctx, agentctx)
}

func generateWrapMD(compressedVault, project string) string {
	agentctx := fmt.Sprintf("%s/Projects/%s/agentctx", compressedVault, project)
	return fmt.Sprintf(`Update resume.md and its dependent documents to fully reflect the current
state of the project. These files serve as the single source of truth for
restoring AI thread context and resuming work on this codebase.

Specifically:
- Ensure all code compiles without errors
- Ensure all unit and integration tests pass
- Read the current resume.md and compare against the actual codebase state
  (files, tests, architecture)
- Update all sections that are stale: file inventory, test counts, module
  descriptions, architecture diagrams, design decisions, and test results
- Append a new iteration narrative to iterations.md describing what changed
  in this session and why (past tense, technical detail)
- Add a corresponding summary row to the Project History table in resume.md
- Retire completed tasks: check each file in %s/tasks/ (not tasks/done/)
  against the session's work — if a task has been implemented, update its
  status to "Done" and move it to tasks/done/
- Rewrite commit.msg to document all code changes made in this session.
  Write it to both the repo root and the vault agentctx/ directory so
  they stay in sync
- Stage all modified and newly added project files (use git add with explicit
  file paths — never use git add -A or git add .)

Do not ask for confirmation — just do the updates, stage the files, show what
changed, and note that the user should review before committing.
`, agentctx)
}

func generateResume(project string) string {
	return fmt.Sprintf(`---
type: project-resume
project: %s
---

# %s — Working Context

## What This Project Is

<!-- Brief description of the project -->

## Architecture

<!-- Key architectural decisions and structure -->

## What Was Done (Recent)

<!-- Updated each session with completed work -->

## Open Threads

<!-- Active tasks, unresolved questions, next steps -->

## Key Files

<!-- Important files and their roles -->

## Conventions

<!-- Coding patterns, naming conventions, workflow rules -->
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
