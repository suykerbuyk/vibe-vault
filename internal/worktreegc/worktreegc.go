// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package worktreegc reaps stale subagent worktrees safely. The Run
// entry point inspects every locked worktree under a repository,
// dispatches its lock-reason marker through harness-specific
// detectors, probes the holder PID for liveness, verifies the worktree
// branch's commits are captured by an authoritative parent, and then
// destructively removes the worktree (unless DryRun is set).
//
// The package is host-local: PID-liveness checks use os.Kill(pid, 0),
// which only works for processes on the same kernel. A repo-level
// lockfile keyed by absolute git-common-dir serializes concurrent
// invocations across linked worktrees of the same repository.
package worktreegc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
)

// Verdict constants. Stable string literals; consumed by CLI output
// and tests.
const (
	VerdictAlive          = "alive"
	VerdictReaped         = "reaped"
	VerdictWouldReap      = "would-reap"
	VerdictUncaptured     = "uncaptured-work"
	VerdictBranchMismatch = "branch-mismatch"
	VerdictUnknownHarness = "unknown-harness"
	VerdictSkipped        = "skipped"
	VerdictError          = "error"
)

// Marker captures the per-worktree state Run extracts from
// `git worktree list --porcelain` plus the parsed lock-reason.
type Marker struct {
	WorktreePath string
	HEAD         string
	BranchName   string
	Harness      string
	Reason       string
	PID          int
}

// Action records one decision Run made about one worktree.
type Action struct {
	Marker  Marker
	Verdict string
	Detail  string
}

// Result aggregates the per-invocation summary.
type Result struct {
	Actions []Action
	Reaped  int
	Alive   int
	Skipped int
	Errors  int
}

// Options tunes Run.
type Options struct {
	// DryRun, when true, suppresses destructive operations and emits
	// would-reap verdicts for worktrees that would otherwise be reaped.
	DryRun bool
	// Detectors, when nil, uses the package defaultDetectors slice.
	Detectors []Detector
	// CandidateParents, when nil, is resolved at runtime to the
	// dedupe of [resolveDefaultBranch(repoPath), <current HEAD branch>].
	CandidateParents []string
	// ForceUncaptured, when true, reaps even if the worktree branch
	// has commits not present on any candidate parent.
	ForceUncaptured bool
}

// verdict is the internal liveness-probe outcome enum used by the
// probePID test seam.
type verdict int

const (
	verdictAlive verdict = iota
	verdictDead
)

// probePID is the host-local PID-liveness probe. It is a package-level
// var so tests can substitute it; this is the only function-pointer
// test seam in the package.
//
// Justified: foreign-UID PIDs (the EPERM branch) cannot be reliably
// synthesized in single-user CI environments.
var probePID = func(pid int) verdict {
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil:
		return verdictAlive
	case errors.Is(err, syscall.ESRCH):
		return verdictDead
	case errors.Is(err, syscall.EPERM):
		return verdictAlive
	default:
		return verdictAlive // unknown error; fail-closed
	}
}

// Run scans every locked worktree under repoPath and reaps those whose
// holder process is dead and whose branch is captured by an
// authoritative parent. See package doc for the full algorithm.
func Run(repoPath string, opts Options) (Result, error) {
	// 1. Resolve absolute git-common-dir. Main worktree returns relative
	// `.git`; linked worktrees return absolute. filepath.Abs normalizes
	// so the lockfile-path hash is identical across siblings.
	gitCommonDirRaw, err := exec.Command("git", "-C", repoPath, "rev-parse",
		"--git-common-dir").Output()
	if err != nil {
		return Result{}, fmt.Errorf("worktreegc: rev-parse --git-common-dir: %w", err)
	}
	gitCommonDir := strings.TrimSpace(string(gitCommonDirRaw))
	if !filepath.IsAbs(gitCommonDir) {
		// Relative paths are resolved against repoPath (git's cwd).
		gitCommonDir = filepath.Join(repoPath, gitCommonDir)
	}
	absGitCommonDir, err := filepath.Abs(gitCommonDir)
	if err != nil {
		return Result{}, fmt.Errorf("worktreegc: absolutize git-common-dir: %w", err)
	}

	// 2. Acquire lockfile keyed by sha256(abs-git-common-dir)[:8].
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return Result{}, fmt.Errorf("worktreegc: resolve user cache dir: %w", err)
	}
	sum := sha256.Sum256([]byte(absGitCommonDir))
	lockName := hex.EncodeToString(sum[:])[:8] + ".lock"
	lockPath := filepath.Join(cacheDir, "vibe-vault", "locks", lockName)
	fl, err := lockfile.AcquireNonBlocking(lockPath)
	if err != nil {
		if errors.Is(err, lockfile.ErrLocked) {
			return Result{}, fmt.Errorf("worktreegc: another invocation in progress: %w", err)
		}
		return Result{}, fmt.Errorf("worktreegc: acquire lock: %w", err)
	}
	defer fl.Release()

	// 3. Enumerate worktrees.
	blocks, err := runWorktreeListPorcelain(repoPath)
	if err != nil {
		return Result{}, fmt.Errorf("worktreegc: enumerate worktrees: %w", err)
	}

	// 4. Resolve detectors.
	detectors := opts.Detectors
	if detectors == nil {
		detectors = defaultDetectors
	}

	// 5. Resolve candidate parents.
	candidateParents := opts.CandidateParents
	if candidateParents == nil {
		defaultParent := resolveDefaultBranch(repoPath)
		curRaw, _ := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD").Output()
		currentBranch := strings.TrimSpace(string(curRaw))
		candidateParents = dedupeNonEmpty([]string{defaultParent, currentBranch})
	}

	res := Result{}

	// 6. Per-block decision loop.
	for _, block := range blocks {
		if block.Bare || block.Detached || block.Locked == "" {
			continue
		}

		worktreeName := filepath.Base(block.Worktree)
		marker := Marker{
			WorktreePath: block.Worktree,
			HEAD:         block.HEAD,
			BranchName:   block.Branch,
			Reason:       block.Locked,
		}

		// 6a. ParseMarker.
		harness, pid, det := ParseMarker(block.Locked, detectors)
		if det == nil {
			res.Actions = append(res.Actions, Action{Marker: marker, Verdict: VerdictUnknownHarness})
			res.Skipped++
			continue
		}
		marker.Harness = harness
		marker.PID = pid

		// 6b. PID liveness probe.
		if probePID(pid) == verdictAlive {
			res.Actions = append(res.Actions, Action{Marker: marker, Verdict: VerdictAlive})
			res.Alive++
			continue
		}

		// 6c. Branch-name guard.
		expected := det.ExpectedBranch(worktreeName)
		if block.Branch != expected {
			res.Actions = append(res.Actions, Action{
				Marker:  marker,
				Verdict: VerdictBranchMismatch,
				Detail:  "expected " + expected + ", got " + block.Branch,
			})
			res.Skipped++
			continue
		}

		// 6d. Capture verification via `git cherry`.
		branchCaptured := false
		var erroredParents []string
		var firstNonErroredParent string
		for _, parent := range candidateParents {
			out, cherryErr := runCherry(repoPath, parent, block.Branch)
			if cherryErr != nil {
				erroredParents = append(erroredParents, parent)
				continue
			}
			if firstNonErroredParent == "" {
				firstNonErroredParent = parent
			}
			// Parse output line by line; any `+` line means uncaptured commit
			// against THIS parent.
			lines := strings.Split(out, "\n")
			anyPlus := false
			for _, ln := range lines {
				if strings.HasPrefix(ln, "+") {
					anyPlus = true
					break
				}
			}
			if anyPlus {
				branchCaptured = false
				break
			}
			branchCaptured = true
			break
		}

		if !branchCaptured && !opts.ForceUncaptured {
			detail := "branch contains commits not in any candidate parent"
			if len(erroredParents) > 0 {
				detail += "; candidates that errored: [" + strings.Join(erroredParents, ", ") + "]"
			}
			if firstNonErroredParent != "" {
				if subjects := firstFiveSubjects(repoPath, firstNonErroredParent, block.Branch); subjects != "" {
					detail += "; first commits: " + subjects
				}
			}
			detail += "; use --force-uncaptured to reap anyway"
			res.Actions = append(res.Actions, Action{
				Marker:  marker,
				Verdict: VerdictUncaptured,
				Detail:  detail,
			})
			res.Skipped++
			continue
		}

		// 6e. Dry-run path.
		if opts.DryRun {
			res.Actions = append(res.Actions, Action{Marker: marker, Verdict: VerdictWouldReap})
			continue
		}

		// 6f. Reap. Order: unlock, force remove, delete branch.
		// git's --force flag does not always override claude-agent locks
		// (iter-180 carried thread observed two empirical failures); unlock
		// first guarantees removal succeeds.
		if errOut, err := runGitCapture(repoPath, "worktree", "unlock", block.Worktree); err != nil {
			res.Actions = append(res.Actions, Action{
				Marker:  marker,
				Verdict: VerdictError,
				Detail:  "unlock: " + err.Error() + ": " + errOut,
			})
			res.Errors++
			continue
		}
		if errOut, err := runGitCapture(repoPath, "worktree", "remove", "--force", block.Worktree); err != nil {
			res.Actions = append(res.Actions, Action{
				Marker:  marker,
				Verdict: VerdictError,
				Detail:  "remove: " + err.Error() + ": " + errOut,
			})
			res.Errors++
			continue
		}
		if errOut, err := runGitCapture(repoPath, "branch", "-D", block.Branch); err != nil {
			res.Actions = append(res.Actions, Action{
				Marker:  marker,
				Verdict: VerdictError,
				Detail:  "branch -D: " + err.Error() + ": " + errOut,
			})
			res.Errors++
			continue
		}

		res.Actions = append(res.Actions, Action{Marker: marker, Verdict: VerdictReaped})
		res.Reaped++
	}

	return res, nil
}

// dedupeNonEmpty preserves order, drops empty strings, and removes
// duplicates (case-sensitive).
func dedupeNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// runCherry invokes `git -C cwd cherry parent branch` and returns
// stdout. Errors propagate so the caller can route the parent into
// erroredParents.
func runCherry(cwd, parent, branch string) (string, error) {
	cmd := exec.Command("git", "-C", cwd, "cherry", parent, branch)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// firstFiveSubjects returns up to five oneline commit subjects from
// `git -C cwd log --oneline parent..branch`, joined by ", ". Best-
// effort: returns "" on any error.
func firstFiveSubjects(cwd, parent, branch string) string {
	cmd := exec.Command("git", "-C", cwd, "log", "--oneline",
		parent+".."+branch)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	// Filter empties (e.g. when output was empty entirely).
	pruned := lines[:0]
	for _, ln := range lines {
		if ln != "" {
			pruned = append(pruned, ln)
		}
	}
	return strings.Join(pruned, ", ")
}

// runGitCapture runs `git args...` with cwd=cwd, capturing stderr. On
// failure, returns the trimmed stderr text and the underlying error.
func runGitCapture(cwd string, args ...string) (string, error) {
	full := append([]string{"-C", cwd}, args...)
	cmd := exec.Command("git", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return strings.TrimSpace(stderr.String()), err
	}
	return "", nil
}
