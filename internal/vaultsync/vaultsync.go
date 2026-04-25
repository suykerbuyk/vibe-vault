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

// RemoteStatus reports per-remote sync state.
type RemoteStatus struct {
	Name   string // remote name (e.g., "github", "vault")
	Ahead  int    // commits ahead of this remote's tracking ref
	Behind int    // commits behind this remote's tracking ref
}

// Status reports the vault repo's git state.
type Status struct {
	Clean   bool           // working tree has no uncommitted changes
	Branch  string         // current branch name
	Remotes []RemoteStatus // per-remote state (empty = no remotes configured)
}

// HasRemote returns true if at least one remote is configured.
func (s *Status) HasRemote() bool { return len(s.Remotes) > 0 }

// PullResult reports what happened during a Pull operation.
type PullResult struct {
	Updated      bool     // any changes were pulled
	Regenerate   bool     // caller should run vv index to rebuild auto-generated files
	ManualReview []string // files that were resolved as upstream but need human review
}

// PushResult reports what happened during a CommitAndPush operation.
type PushResult struct {
	CommitSHA     string           // the commit SHA that was pushed (empty if nothing to commit)
	RemoteResults map[string]error // per-remote push result (nil = success)
}

// AllPushed returns true if all remotes were pushed successfully.
func (r *PushResult) AllPushed() bool {
	if len(r.RemoteResults) == 0 {
		return false
	}
	for _, err := range r.RemoteResults {
		if err != nil {
			return false
		}
	}
	return true
}

// AnyPushed returns true if at least one remote was pushed successfully.
func (r *PushResult) AnyPushed() bool {
	for _, err := range r.RemoteResults {
		if err == nil {
			return true
		}
	}
	return false
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

// listRemotes discovers all configured git remotes.
func listRemotes(vaultPath string) ([]string, error) {
	out, err := gitCmd(vaultPath, 10*time.Second, "remote")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	var remotes []string
	for r := range strings.SplitSeq(out, "\n") {
		r = strings.TrimSpace(r)
		if r != "" {
			remotes = append(remotes, r)
		}
	}
	return remotes, nil
}

// GetStatus reports the vault repo's git state.
func GetStatus(vaultPath string) (*Status, error) {
	s := &Status{}

	// Check branch name
	branch, err := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("not a git repo or no commits: %w", err)
	}
	s.Branch = branch

	// Working tree status
	porcelain, err := gitCmd(vaultPath, 10*time.Second, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	s.Clean = porcelain == ""

	// Discover remotes and compute per-remote ahead/behind
	remotes, err := listRemotes(vaultPath)
	if err != nil || len(remotes) == 0 {
		return s, nil
	}

	for _, remote := range remotes {
		rs := RemoteStatus{Name: remote}
		ref := remote + "/" + branch
		revList, revErr := gitCmd(vaultPath, 10*time.Second, "rev-list", "--count", "--left-right", ref+"...HEAD")
		if revErr == nil {
			parts := strings.Fields(revList)
			if len(parts) == 2 {
				rs.Behind, _ = strconv.Atoi(parts[0])
				rs.Ahead, _ = strconv.Atoi(parts[1])
			}
		}
		s.Remotes = append(s.Remotes, rs)
	}

	return s, nil
}

// EnsureRemote checks that at least one git remote is configured.
func EnsureRemote(vaultPath string) error {
	remotes, err := listRemotes(vaultPath)
	if err != nil {
		return fmt.Errorf("listing remotes: %w", err)
	}
	if len(remotes) == 0 {
		return fmt.Errorf("no git remotes configured in vault %s", vaultPath)
	}
	return nil
}

// Pull fetches from all configured remotes and rebases local commits on top.
// Conflicts are resolved automatically based on file classification:
//   - Regenerable/ConfigFile/AppendOnly: accept upstream version
//   - Manual: accept upstream, but report for human review
//
// If Regenerate is true in the result, the caller should run vv index to
// rebuild auto-generated files.
func Pull(vaultPath string) (*PullResult, error) {
	result := &PullResult{}

	if err := checkIdentity(vaultPath); err != nil {
		return nil, err
	}

	remotes, err := listRemotes(vaultPath)
	if err != nil {
		return nil, fmt.Errorf("listing remotes: %w", err)
	}
	if len(remotes) == 0 {
		return nil, fmt.Errorf("no git remotes configured in vault %s", vaultPath)
	}

	// Fetch from all remotes
	for _, remote := range remotes {
		if _, fetchErr := gitCmd(vaultPath, 60*time.Second, "fetch", remote); fetchErr != nil {
			// Log but continue — one unreachable remote shouldn't block others
			fmt.Fprintf(os.Stderr, "warning: fetch %s failed: %v\n", remote, fetchErr)
		}
	}

	// Determine rebase target: tracking upstream, or first remote's branch
	branch, _ := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		branch = "main"
	}

	rebaseTarget, _ := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--abbrev-ref", "@{u}")
	if rebaseTarget == "" {
		rebaseTarget = remotes[0] + "/" + branch
	}

	// Check if we're behind the rebase target
	revList, err := gitCmd(vaultPath, 10*time.Second, "rev-list", "--count", "--left-right", rebaseTarget+"...HEAD")
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
	_, rebaseErr := gitCmd(vaultPath, 60*time.Second, "rebase", rebaseTarget)
	if rebaseErr != nil {
		// Resolve conflicts by file classification
		resolved, err := resolveConflicts(vaultPath, result)
		if err != nil || !resolved {
			// Abort rebase — unresolvable
			_, _ = gitCmd(vaultPath, 10*time.Second, "rebase", "--abort")
			if stashed {
				_, _ = gitCmd(vaultPath, 10*time.Second, "stash", "pop")
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
// message, and pushes to all configured remotes. If a push is rejected
// (non-fast-forward), it fetches and rebases from that remote and retries
// once. Returns per-remote results.
func CommitAndPush(vaultPath, message string) (*PushResult, error) {
	result := &PushResult{}

	if err := checkIdentity(vaultPath); err != nil {
		return nil, err
	}

	remotes, err := listRemotes(vaultPath)
	if err != nil {
		return nil, fmt.Errorf("listing remotes: %w", err)
	}
	if len(remotes) == 0 {
		return nil, fmt.Errorf("no git remotes configured in vault %s", vaultPath)
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

	// Push to all remotes
	branch, _ := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		branch = "main"
	}

	result.RemoteResults = make(map[string]error, len(remotes))
	for _, remote := range remotes {
		_, pushErr := gitCmd(vaultPath, 60*time.Second, "push", remote, branch)
		if pushErr != nil {
			// Retry: fetch from this remote, rebase, push again
			_, _ = gitCmd(vaultPath, 60*time.Second, "fetch", remote)
			_, _ = gitCmd(vaultPath, 60*time.Second, "rebase", remote+"/"+branch)
			_, pushErr = gitCmd(vaultPath, 60*time.Second, "push", remote, branch)
		}
		result.RemoteResults[remote] = pushErr
	}

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

	for f := range strings.SplitSeq(out, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		class := Classify(f)
		// Accept upstream for all classes
		if _, coErr := gitCmd(vaultPath, 10*time.Second, "checkout", "--theirs", f); coErr != nil {
			return false, fmt.Errorf("checkout --theirs %s: %w", f, coErr)
		}
		if _, addErr := gitCmd(vaultPath, 10*time.Second, "add", f); addErr != nil {
			return false, fmt.Errorf("add %s: %w", f, addErr)
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

// checkIdentity returns nil if git can resolve a committer identity
// from any source (.git/config, ~/.gitconfig, system gitconfig,
// $XDG_CONFIG_HOME/git/config, or GIT_AUTHOR_*/GIT_COMMITTER_* env
// vars). Returns an actionable error otherwise. Uses `git var
// GIT_AUTHOR_IDENT` — git's own identity-resolution check, mirroring
// what `git commit` does internally.
func checkIdentity(vaultPath string) error {
	if _, err := gitCmd(vaultPath, 5*time.Second, "var", "GIT_AUTHOR_IDENT"); err != nil {
		return fmt.Errorf(
			"no git identity configured for vault commits (HOME=%s). "+
				"Set with: git config --global user.email <addr> && "+
				"git config --global user.name <name>",
			os.Getenv("HOME"),
		)
	}
	return nil
}

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
