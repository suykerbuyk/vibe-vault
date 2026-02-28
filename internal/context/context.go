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
func Init(cfg config.Config, cwd string, opts Opts) (*InitResult, error) {
	if err := validateVault(cfg.VaultPath); err != nil {
		return nil, err
	}

	project, err := resolveProject(cwd, opts.Project)
	if err != nil {
		return nil, err
	}

	result := &InitResult{Project: project}
	vaultProject := filepath.Join(cfg.VaultPath, "Projects", project)
	compressedVault := config.CompressHome(cfg.VaultPath)

	// Vault-side directories
	for _, dir := range []string{
		vaultProject,
		filepath.Join(vaultProject, "tasks"),
		filepath.Join(vaultProject, "tasks", "done"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Vault-side templates
	vaultFiles := []struct {
		rel     string
		content string
	}{
		{"resume.md", generateResume(project)},
		{"iterations.md", generateIterations(project)},
	}
	for _, f := range vaultFiles {
		path := filepath.Join(vaultProject, f.rel)
		action := safeWrite(path, f.content, opts.Force)
		result.Actions = append(result.Actions, FileAction{
			Path:   filepath.Join("Projects", project, f.rel),
			Action: action,
		})
	}

	// Repo-side files
	repoFiles := []struct {
		rel     string
		content string
	}{
		{"CLAUDE.md", generateClaudeMD(compressedVault, project)},
		{filepath.Join(".claude", "commands", "restart.md"), generateRestartMD(compressedVault, project)},
		{filepath.Join(".claude", "commands", "wrap.md"), generateWrapMD(compressedVault, project)},
	}
	for _, f := range repoFiles {
		path := filepath.Join(cwd, f.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", f.rel, err)
		}
		action := safeWrite(path, f.content, opts.Force)
		result.Actions = append(result.Actions, FileAction{
			Path:   f.rel,
			Action: action,
		})
	}

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
		// Only report once if both were added
		result.Actions = append(result.Actions, FileAction{Path: ".gitignore", Action: giAction2})
	}

	return result, nil
}

// Migrate copies existing local context files to the vault, then performs
// the same repo-side updates as Init.
func Migrate(cfg config.Config, cwd string, opts Opts) (*MigrateResult, error) {
	if err := validateVault(cfg.VaultPath); err != nil {
		return nil, err
	}

	project, err := resolveProject(cwd, opts.Project)
	if err != nil {
		return nil, err
	}

	result := &MigrateResult{Project: project}
	vaultProject := filepath.Join(cfg.VaultPath, "Projects", project)
	compressedVault := config.CompressHome(cfg.VaultPath)

	// Ensure vault dirs exist
	for _, dir := range []string{
		vaultProject,
		filepath.Join(vaultProject, "tasks"),
		filepath.Join(vaultProject, "tasks", "done"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Copy local files to vault
	migrations := []struct {
		src string // relative to cwd
		dst string // relative to vaultProject
	}{
		{"RESUME.md", "resume.md"},
		{"HISTORY.md", "iterations.md"},
	}
	for _, m := range migrations {
		srcPath := filepath.Join(cwd, m.src)
		dstPath := filepath.Join(vaultProject, m.dst)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			result.Actions = append(result.Actions, FileAction{
				Path:   m.src + " → " + filepath.Join("Projects", project, m.dst),
				Action: "SKIP",
			})
			continue
		}
		action, err := copyFileAction(srcPath, dstPath, opts.Force)
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", m.src, err)
		}
		result.Actions = append(result.Actions, FileAction{
			Path:   m.src + " → " + filepath.Join("Projects", project, m.dst),
			Action: action,
		})
	}

	// Copy tasks/ directory
	srcTasks := filepath.Join(cwd, "tasks")
	dstTasks := filepath.Join(vaultProject, "tasks")
	if info, err := os.Stat(srcTasks); err == nil && info.IsDir() {
		actions, err := copyDir(srcTasks, dstTasks, opts.Force)
		if err != nil {
			return nil, fmt.Errorf("copy tasks/: %w", err)
		}
		for _, a := range actions {
			result.Actions = append(result.Actions, FileAction{
				Path:   "tasks/" + a.Path + " → " + filepath.Join("Projects", project, "tasks", a.Path),
				Action: a.Action,
			})
		}
	} else {
		result.Actions = append(result.Actions, FileAction{
			Path:   "tasks/ → " + filepath.Join("Projects", project, "tasks/"),
			Action: "SKIP",
		})
	}

	// Force-update repo-side files (must switch to vault pointer mode)
	repoFiles := []struct {
		rel     string
		content string
	}{
		{"CLAUDE.md", generateClaudeMD(compressedVault, project)},
		{filepath.Join(".claude", "commands", "restart.md"), generateRestartMD(compressedVault, project)},
		{filepath.Join(".claude", "commands", "wrap.md"), generateWrapMD(compressedVault, project)},
	}
	for _, f := range repoFiles {
		path := filepath.Join(cwd, f.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", f.rel, err)
		}
		// Always overwrite repo-side files on migrate
		action := safeWrite(path, f.content, true)
		result.Actions = append(result.Actions, FileAction{
			Path:   f.rel,
			Action: "UPDATE",
		})
		_ = action
	}

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

func generateClaudeMD(compressedVault, project string) string {
	return fmt.Sprintf(`# CLAUDE.md

## Project Context

All working context for this project lives in the vault:

- **Resume**: %s/Projects/%s/resume.md
- **Iterations**: %s/Projects/%s/iterations.md
- **Tasks**: %s/Projects/%s/tasks/
- **Sessions**: %s/Projects/%s/sessions/

Read resume.md at session start for full project context.

## Workflow Rules

- **Never commit without explicit human permission.** You may stage files
  and update commit.msg freely, but the actual git commit requires the
  human to say so.
- **Never commit AI context files.** CLAUDE.md, commit.msg, and anything
  under .claude/commands/ are local-only — they must never appear in git.
- **Git commit messages are the project's history.** Write them to be
  clear, detailed, and self-sufficient.
- On /wrap: stage files, update commit.msg, update vault-side resume.md
  and tasks, but do not commit.
`, compressedVault, project,
		compressedVault, project,
		compressedVault, project,
		compressedVault, project)
}

func generateRestartMD(compressedVault, project string) string {
	return fmt.Sprintf(`# Session Restart

Read the following files to restore context:

1. %s/Projects/%s/resume.md — current project state, architecture, decisions
2. %s/Projects/%s/iterations.md — iteration narratives and history
3. %s/Projects/%s/tasks/ — active task files (skip tasks/done/)

After reading, briefly summarize your understanding and ask what to work on.
`, compressedVault, project,
		compressedVault, project,
		compressedVault, project)
}

func generateWrapMD(compressedVault, project string) string {
	return fmt.Sprintf(`# Session Wrap

End-of-session procedure:

1. Stage all changed files (git add)
2. Write a descriptive commit message to commit.msg
3. Update %s/Projects/%s/resume.md:
   - Reflect current project state
   - Update "What Was Done" and "Open Threads" sections
   - Keep it concise but complete enough to resume cold
4. Move completed tasks from %s/Projects/%s/tasks/ to tasks/done/
5. Do NOT commit — the human will review and commit

Report what you staged and what changed in resume.md.
`, compressedVault, project,
		compressedVault, project)
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
