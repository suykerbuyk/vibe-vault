// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package vaultsync manages git synchronization of the vault repository
// across machines. The vault is owned entirely by vv — all git operations
// within it are safe and autonomous.
package vaultsync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// FileClass determines conflict resolution strategy during rebase.
type FileClass int

const (
	// Regenerable files can be rebuilt by vv index (history.md, session-index.json).
	Regenerable FileClass = iota
	// AppendOnly files have unique timestamps and near-zero conflict risk (session notes).
	AppendOnly
	// Manual files require human review if conflicted (knowledge.md, resume.md, tasks).
	Manual
	// ConfigFile templates and internal config — accept upstream on conflict.
	ConfigFile
)

// Status reports the vault repo's git state.
type Status struct {
	Clean     bool   // working tree has no uncommitted changes
	Branch    string // current branch name
	Remote    string // remote name (typically "origin")
	HasRemote bool   // remote is configured
	Ahead     int    // commits ahead of remote
	Behind    int    // commits behind remote
}

// PullResult reports what happened during a Pull operation.
type PullResult struct {
	Updated      bool     // any changes were pulled
	Regenerate   bool     // caller should run vv index to rebuild auto-generated files
	ManualReview []string // files that were resolved as upstream but need human review
}

// PushResult reports what happened during a CommitAndPush operation.
type PushResult struct {
	Pushed    bool   // successfully pushed to remote
	CommitSHA string // the commit SHA that was pushed (empty if nothing to commit)
}

// Classify returns the FileClass for a vault-relative path, determining
// how conflicts on that file should be resolved during rebase.
func Classify(relPath string) FileClass {
	// Regenerable: auto-generated files that vv index can rebuild
	if strings.HasSuffix(relPath, "/history.md") || relPath == "history.md" {
		return Regenerable
	}
	if strings.Contains(relPath, ".vibe-vault/session-index") {
		return Regenerable
	}

	// AppendOnly: session notes (unique timestamps per machine)
	if strings.Contains(relPath, "/sessions/") || strings.HasPrefix(relPath, "sessions/") {
		return AppendOnly
	}

	// ConfigFile: templates and internal config
	if strings.HasPrefix(relPath, "Templates/") {
		return ConfigFile
	}
	if strings.HasPrefix(relPath, ".vibe-vault/config") {
		return ConfigFile
	}

	// Everything else: knowledge.md, resume.md, iterations.md, tasks, etc.
	return Manual
}

// GetStatus reports the vault repo's git state.
func GetStatus(vaultPath string) (*Status, error) {
	s := &Status{Remote: "origin"}

	// Check branch name
	branch, err := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("not a git repo or no commits: %w", err)
	}
	s.Branch = branch

	// Check remote
	if _, err := gitCmd(vaultPath, 10*time.Second, "remote", "get-url", "origin"); err != nil {
		s.HasRemote = false
		// Still check working tree status
		porcelain, _ := gitCmd(vaultPath, 10*time.Second, "status", "--porcelain")
		s.Clean = porcelain == ""
		return s, nil
	}
	s.HasRemote = true

	// Working tree status
	porcelain, err := gitCmd(vaultPath, 10*time.Second, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	s.Clean = porcelain == ""

	// Ahead/behind counts
	revList, err := gitCmd(vaultPath, 10*time.Second, "rev-list", "--count", "--left-right", "@{u}...HEAD")
	if err != nil {
		// No upstream tracking — not an error, just no counts
		return s, nil
	}
	parts := strings.Fields(revList)
	if len(parts) == 2 {
		s.Behind, _ = strconv.Atoi(parts[0])
		s.Ahead, _ = strconv.Atoi(parts[1])
	}

	return s, nil
}

// EnsureRemote checks that a remote named "origin" exists.
func EnsureRemote(vaultPath string) error {
	_, err := gitCmd(vaultPath, 10*time.Second, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("no git remote 'origin' configured in vault %s", vaultPath)
	}
	return nil
}

// Pull fetches from origin and rebases local commits on top. Conflicts are
// resolved automatically based on file classification:
//   - Regenerable/ConfigFile/AppendOnly: accept upstream version
//   - Manual: accept upstream, but report for human review
//
// If Regenerate is true in the result, the caller should run vv index to
// rebuild auto-generated files.
func Pull(vaultPath string) (*PullResult, error) {
	result := &PullResult{}

	if err := EnsureRemote(vaultPath); err != nil {
		return nil, err
	}

	// Fetch latest
	if _, err := gitCmd(vaultPath, 60*time.Second, "fetch", "origin"); err != nil {
		return nil, fmt.Errorf("git fetch: %w", err)
	}

	// Check if we're behind
	branch, _ := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		branch = "main"
	}

	revList, err := gitCmd(vaultPath, 10*time.Second, "rev-list", "--count", "--left-right", "@{u}...HEAD")
	if err == nil {
		parts := strings.Fields(revList)
		if len(parts) == 2 {
			behind, _ := strconv.Atoi(parts[0])
			if behind == 0 {
				return result, nil // already up to date
			}
		}
	}

	// Stash if dirty
	porcelain, _ := gitCmd(vaultPath, 10*time.Second, "status", "--porcelain")
	stashed := false
	if porcelain != "" {
		if _, err := gitCmd(vaultPath, 10*time.Second, "stash", "push", "-m", "vv-vault-sync-autostash"); err != nil {
			return nil, fmt.Errorf("git stash: %w", err)
		}
		stashed = true
	}

	// Attempt rebase
	_, rebaseErr := gitCmd(vaultPath, 60*time.Second, "rebase", "origin/"+branch)
	if rebaseErr != nil {
		// Resolve conflicts by file classification
		resolved, err := resolveConflicts(vaultPath, result)
		if err != nil || !resolved {
			// Abort rebase — unresolvable
			gitCmd(vaultPath, 10*time.Second, "rebase", "--abort")
			if stashed {
				gitCmd(vaultPath, 10*time.Second, "stash", "pop")
			}
			if err != nil {
				return nil, fmt.Errorf("conflict resolution failed: %w", err)
			}
			return nil, fmt.Errorf("unresolvable conflicts during rebase — manual intervention required")
		}
	}

	result.Updated = true

	// Pop stash if we stashed
	if stashed {
		if _, err := gitCmd(vaultPath, 10*time.Second, "stash", "pop"); err != nil {
			// Stash pop conflict — report but don't fail
			result.ManualReview = append(result.ManualReview, "(stash pop conflict — run 'git stash pop' manually in vault)")
		}
	}

	return result, nil
}

// CommitAndPush stages all vault changes, commits with a machine-stamped
// message, and pushes to origin. If push is rejected (non-fast-forward),
// it pulls and retries once. Returns a descriptive error on final failure
// for interactive resolution.
func CommitAndPush(vaultPath, message string) (*PushResult, error) {
	result := &PushResult{}

	if err := EnsureRemote(vaultPath); err != nil {
		return nil, err
	}

	// Stage all changes (safe — vv owns the vault)
	if _, err := gitCmd(vaultPath, 10*time.Second, "add", "-A"); err != nil {
		return nil, fmt.Errorf("git add: %w", err)
	}

	// Check if anything to commit
	if _, err := gitCmd(vaultPath, 10*time.Second, "diff", "--cached", "--quiet"); err == nil {
		// Exit code 0 means no staged changes
		return result, nil
	}

	// Stamp with hostname
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	fullMsg := fmt.Sprintf("%s\n\n[%s]", message, hostname)

	// Commit
	if _, err := gitCmd(vaultPath, 10*time.Second, "commit", "-m", fullMsg); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Get commit SHA
	sha, _ := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--short", "HEAD")
	result.CommitSHA = sha

	// Push
	branch, _ := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		branch = "main"
	}

	_, pushErr := gitCmd(vaultPath, 60*time.Second, "push", "origin", branch)
	if pushErr == nil {
		result.Pushed = true
		return result, nil
	}

	// Push rejected — pull and retry once
	if _, err := Pull(vaultPath); err != nil {
		return result, fmt.Errorf("push rejected and pull failed: %w\nResolve manually in %s", err, vaultPath)
	}

	_, pushErr = gitCmd(vaultPath, 60*time.Second, "push", "origin", branch)
	if pushErr != nil {
		return result, fmt.Errorf("push failed after pull-retry: %w\nResolve manually in %s", pushErr, vaultPath)
	}

	result.Pushed = true
	return result, nil
}

// resolveConflicts attempts to auto-resolve all conflicted files during an
// active rebase. Returns true if all conflicts were resolved.
func resolveConflicts(vaultPath string, result *PullResult) (bool, error) {
	// List conflicted files
	out, err := gitCmd(vaultPath, 10*time.Second, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false, err
	}
	if out == "" {
		return false, nil
	}

	files := strings.Split(out, "\n")
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		class := Classify(f)
		// Accept upstream for all classes
		if _, err := gitCmd(vaultPath, 10*time.Second, "checkout", "--theirs", f); err != nil {
			return false, fmt.Errorf("checkout --theirs %s: %w", f, err)
		}
		if _, err := gitCmd(vaultPath, 10*time.Second, "add", f); err != nil {
			return false, fmt.Errorf("add %s: %w", f, err)
		}

		switch class {
		case Regenerable:
			result.Regenerate = true
		case Manual:
			result.ManualReview = append(result.ManualReview, f)
		}
	}

	// Continue the rebase
	_, err = gitCmd(vaultPath, 30*time.Second, "rebase", "--continue")
	if err != nil {
		// May need GIT_EDITOR=true to skip commit message edit
		os.Setenv("GIT_EDITOR", "true")
		_, err = gitCmd(vaultPath, 30*time.Second, "rebase", "--continue")
		os.Unsetenv("GIT_EDITOR")
	}

	return err == nil, err
}

// reNonFastForward matches git push rejection messages.
var reNonFastForward = regexp.MustCompile(`(?i)(non-fast-forward|rejected|failed to push)`)

// gitCmd runs a git command in the vault directory with a timeout.
func gitCmd(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Prevent interactive prompts
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
