// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package vaultsync manages git synchronization of the vault repository
// across machines. The vault is owned entirely by vv — all git operations
// within it are safe and autonomous.
package vaultsync

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// afterPushHook runs immediately after a successful push records its
// SHA in remoteSHA, before the convergence loop. Production code must
// leave this as a no-op; tests override it to inject mid-flight state
// changes (e.g., a concurrent writer mutating a bare remote between
// the recorded push and the convergence force-with-lease).
var afterPushHook = func(remote string) {}

// FileClass determines conflict resolution strategy during rebase.
type FileClass int

const (
	// Regenerable files can be rebuilt by vv index (history.md, session-index.json).
	Regenerable FileClass = iota
	// AppendOnly files have unique timestamps and near-zero conflict risk (session notes).
	AppendOnly
	// Manual files require human review if conflicted (knowledge.md, resume.md, tasks).
	Manual
	// ConfigFile templates and internal config — keep local on conflict
	// (templates change rarely; config is host-local-adjacent).
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

// DroppedUpstream describes an upstream-side commit whose content for a
// Manual-class file was dropped during rebase conflict resolution. The
// resolver keeps local across all classes; for Manual class it records
// the upstream-side commit responsible for the conflicting file so the
// operator can recover it via `vv vault recover`.
type DroppedUpstream struct {
	Path               string    // vault-relative path
	DroppedSHA         string    // upstream commit that last touched Path between merge-base and rebase target
	DroppedSubject     string    // commit subject line
	DroppedCommittedAt time.Time // committer date of the dropped commit
}

// PullResult reports what happened during a Pull operation.
type PullResult struct {
	Updated          bool              // any changes were pulled
	Regenerate       bool              // caller should run vv index to rebuild auto-generated files
	DroppedUpstream  []DroppedUpstream // Manual-class files whose upstream content was dropped during rebase conflict resolution
	StashPopConflict bool              // stash pop after rebase produced conflicts; operator must resolve manually
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
// Conflicts during rebase are auto-resolved by keeping the LOCAL side
// uniformly across all four file classes — local work is the most recent
// operator intent on this machine. Per-class side-effects:
//   - Regenerable: keep local; set Regenerate=true so the caller runs
//     vv index and overwrites whatever was kept.
//   - AppendOnly: keep local; unique-timestamp filenames make same-path
//     collisions near-impossible, so no warning surface is needed.
//   - ConfigFile: keep local; templates change rarely and config is
//     host-local-adjacent.
//   - Manual: keep local; record the upstream-side commit responsible for
//     the file in DroppedUpstream so the operator can inspect and recover
//     via `vv vault recover`.
//
// If a stash pop after rebase conflicts, StashPopConflict is set so the
// caller can surface a remediation message.
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
		resolved, err := resolveConflicts(vaultPath, rebaseTarget, result)
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
			result.StashPopConflict = true
		}
	}

	return result, nil
}

// CommitAndPush stages all dirty vault paths, commits with a
// machine-stamped message, and pushes to all configured remotes. It is
// a thin wrapper over CommitAndPushPaths: it enumerates the dirty
// working-tree paths via `git status --porcelain -z` and forwards them
// to the selective-staging entry point. Behaviour is identical to the
// pre-refactor catch-all `git add -A` for any working-tree state — the
// regression-locked test
// TestCommitAndPush_CatchAllParity_PreservesOriginalBehavior
// keeps that contract enforced.
//
// If the working tree is clean, returns (&PushResult{}, nil) without
// invoking CommitAndPushPaths so the caller does not see the
// "no paths specified" error from the explicit-intent guard.
//
// See CommitAndPushPaths for the full push / rebase / convergence
// contract.
func CommitAndPush(vaultPath, message string) (*PushResult, error) {
	if err := checkIdentity(vaultPath); err != nil {
		return nil, err
	}

	paths, err := dirtyPaths(vaultPath)
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	if len(paths) == 0 {
		// Nothing to commit — preserve the historical "empty result, no
		// error" semantics. Short-circuit BEFORE CommitAndPushPaths so
		// the explicit-intent empty-paths guard does not trigger.
		return &PushResult{}, nil
	}

	return CommitAndPushPaths(vaultPath, message, paths)
}

// CommitAndPushPaths stages only the supplied paths, commits with a
// machine-stamped message, and pushes to all configured remotes. Unlike
// CommitAndPush, dirty paths NOT in the supplied list are left dirty in
// the working tree — this is the contamination-safe entry point used by
// callers that know which files belong to their work unit.
//
// Empty or nil paths returns (nil, error) — explicit caller intent is
// required. Use CommitAndPush for the catch-all behaviour.
//
// Staging uses `git add -- <paths>...` with the `--` separator so paths
// beginning with `-` are treated as paths, not flags. Long path lists
// are batched under a conservative ~64 KB argv-byte budget per
// invocation to stay well clear of the Linux ~128 KB and macOS ~64 KB
// MAX_ARG_LEN ceilings; all batches must succeed before the commit
// step.
//
// Happy path is sequential push, unchanged. On a non-fast-forward
// rejection, the rejected remote is fetched and the local branch is
// rebased onto it; the rebased commit is then pushed to that remote.
// Fetch failures and rebase failures (after `rebase --abort`) surface
// directly via per-remote `RemoteResults` rather than masquerading as
// downstream errors.
//
// After every successful rebase-and-push, prior remotes whose
// last-known-good SHA differs from the new HEAD are converged via
// `git push --force-with-lease=refs/heads/<branch>:<expected> <remote>
// <branch>`. The lease is an atomic compare-and-swap: if any concurrent
// writer has moved the prior remote's ref since we recorded it, the
// push is rejected and the failure surfaces as
// `"convergence rejected (concurrent writer at <remote>): <err>"` in
// `PushResult.RemoteResults`. The lease is the only path in this
// package that uses `--force-with-lease`; naked `--force` is never
// used.
//
// `PushResult.CommitSHA` is refreshed to the post-loop HEAD if any
// rebase happened, so the printed SHA always exists at the converged
// remotes.
func CommitAndPushPaths(vaultPath, message string, paths []string) (*PushResult, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no paths specified")
	}

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

	// Stage only the supplied paths. Chunk under a conservative argv
	// byte budget to stay clear of MAX_ARG_LEN ceilings.
	if err := stageInBatches(vaultPath, paths); err != nil {
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
	remoteSHA := make(map[string]string, len(remotes))
	rebasedAny := false

	for _, remote := range remotes {
		curSHA, _ := gitCmd(vaultPath, 10*time.Second, "rev-parse", "HEAD")

		if _, pushErr := gitCmd(vaultPath, 60*time.Second, "push", remote, branch); pushErr == nil {
			// Happy path: fast-forward push succeeded.
			remoteSHA[remote] = curSHA
			result.RemoteResults[remote] = nil
			afterPushHook(remote)
			continue
		}

		// Rejection path (non-fast-forward or other push error).
		// Fetch failure surfaces directly — no rebase/converge.
		if _, fetchErr := gitCmd(vaultPath, 60*time.Second, "fetch", remote); fetchErr != nil {
			result.RemoteResults[remote] = fmt.Errorf("fetch %s: %w", remote, fetchErr)
			continue
		}

		// Rebase failure aborts cleanly so HEAD does not stay polluted
		// for the next remote in the loop.
		if _, rebaseErr := gitCmd(vaultPath, 60*time.Second, "rebase", remote+"/"+branch); rebaseErr != nil {
			_, _ = gitCmd(vaultPath, 10*time.Second, "rebase", "--abort")
			result.RemoteResults[remote] = fmt.Errorf("rebase against %s failed: %w", remote, rebaseErr)
			continue
		}

		rebasedAny = true
		curSHA, _ = gitCmd(vaultPath, 10*time.Second, "rev-parse", "HEAD")

		if _, pushErr := gitCmd(vaultPath, 60*time.Second, "push", remote, branch); pushErr != nil {
			// Non-NFF push error after rebase (auth, network, etc.).
			result.RemoteResults[remote] = pushErr
			continue
		}
		remoteSHA[remote] = curSHA
		result.RemoteResults[remote] = nil
		afterPushHook(remote)

		// Converge prior remotes whose recorded SHA != current HEAD.
		for priorRemote, priorSHA := range remoteSHA {
			if priorSHA == curSHA {
				continue
			}
			if leaseErr := forceWithLease(vaultPath, priorRemote, branch, priorSHA); leaseErr != nil {
				result.RemoteResults[priorRemote] = fmt.Errorf(
					"convergence rejected (concurrent writer at %s): %w",
					priorRemote, leaseErr)
				// Leave remoteSHA[priorRemote] unchanged — caller sees
				// divergent state via per-remote error.
				continue
			}
			remoteSHA[priorRemote] = curSHA
			// result.RemoteResults[priorRemote] stays nil (still success).
			log.Printf("vault: force-converged %s from %s\n", priorRemote, short(priorSHA))
		}
	}

	// Refresh CommitSHA so the CLI never prints a SHA that no longer
	// exists at any remote.
	if rebasedAny {
		if newSHA, err := gitCmd(vaultPath, 10*time.Second, "rev-parse", "--short", "HEAD"); err == nil {
			result.CommitSHA = newSHA
		}
	}

	return result, nil
}

// stageBatchByteBudget caps the per-`git add` argv path-byte budget at
// ~64 KB. Linux MAX_ARG_LEN is ~128 KB and macOS is ~64 KB; the lower
// figure governs. Each path costs len(path)+1 bytes (NUL terminator)
// when measured against the kernel argv ceiling.
const stageBatchByteBudget = 64 * 1024

// stageInBatches runs `git add -- <chunk>...` over `paths`, splitting
// into chunks whose combined argv-path bytes stay under
// stageBatchByteBudget. Always emits the `--` separator so paths
// beginning with `-` are treated as paths, not flags.
func stageInBatches(vaultPath string, paths []string) error {
	batch := make([]string, 0, len(paths))
	bytes := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		args := make([]string, 0, len(batch)+2)
		args = append(args, "add", "--")
		args = append(args, batch...)
		if _, err := gitCmd(vaultPath, 30*time.Second, args...); err != nil {
			return err
		}
		batch = batch[:0]
		bytes = 0
		return nil
	}
	for _, p := range paths {
		cost := len(p) + 1
		if len(batch) > 0 && bytes+cost > stageBatchByteBudget {
			if err := flush(); err != nil {
				return err
			}
		}
		batch = append(batch, p)
		bytes += cost
	}
	return flush()
}

// dirtyPaths enumerates the working-tree paths that differ from HEAD,
// using `git status --porcelain -z` to handle paths containing spaces,
// quotes, or shell-metacharacters correctly. The NUL-separated v1
// porcelain format encodes each entry as `XY <path>\0`. Renames /
// copies (status `R` / `C`) emit two records — `XY <new>\0<old>\0` —
// and we stage only `<new>` since the catch-all path always ends up
// with the new name in the index after `git add -A`.
func dirtyPaths(vaultPath string) ([]string, error) {
	// gitCmdRaw — porcelain output's leading space (e.g. " M file") is
	// part of the XY status field; gitCmd's strings.TrimSpace would
	// shift each entry left by one byte and corrupt the parse.
	out, err := gitCmdRaw(vaultPath, 10*time.Second, "status", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	fields := strings.Split(out, "\x00")
	paths := make([]string, 0, len(fields))
	i := 0
	for i < len(fields) {
		entry := fields[i]
		if entry == "" {
			i++
			continue
		}
		// Each XY status is exactly 2 bytes; entries shorter than
		// `XY <c>` (4 bytes) are malformed — skip defensively.
		if len(entry) < 4 {
			i++
			continue
		}
		statusXY := entry[:2]
		path := entry[3:]
		paths = append(paths, path)
		// Renames/copies emit a follow-up record holding the OLD path.
		// `R` and `C` may appear in either index (X) or worktree (Y)
		// position, though git in practice only reports renames in the
		// index. Skip the follow-up old-path field either way.
		if statusXY[0] == 'R' || statusXY[0] == 'C' || statusXY[1] == 'R' || statusXY[1] == 'C' {
			i += 2
			continue
		}
		i++
	}
	return paths, nil
}

// forceWithLease pushes branch to remote with a lease keyed to
// expectedSHA. The lease causes git to reject the push if the remote's
// branch ref has moved off expectedSHA since we last observed it —
// catching concurrent writers without resorting to naked --force.
func forceWithLease(vaultPath, remote, branch, expectedSHA string) error {
	lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", branch, expectedSHA)
	_, err := gitCmd(vaultPath, 60*time.Second, "push", lease, remote, branch)
	return err
}

// short returns the first 7 characters of a SHA for log breadcrumbs,
// or the original string if shorter.
func short(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

// resolveConflicts attempts to auto-resolve all conflicted files during an
// active rebase. Returns true if all conflicts were resolved.
//
// Policy: keep LOCAL across all classes on conflict — local work is the
// most recent operator intent on this machine. Note that during a
// `git rebase`, --ours and --theirs are inverted vs merge semantics:
// `--ours` = the rebase target (upstream branch) and `--theirs` = the
// commit being replayed (local work). So we use `git checkout --theirs`
// to keep the local-side content. Upstream-side dropped content for
// Manual class is recorded in result.DroppedUpstream so the operator
// can inspect via `vv vault recover`.
//
// rebaseTarget is the upstream branch the rebase is replaying onto
// (resolved by the caller). It is required so we can compute the
// merge-base against ORIG_HEAD and identify the upstream commit
// responsible for each conflicted file's upstream-side state.
func resolveConflicts(vaultPath, rebaseTarget string, result *PullResult) (bool, error) {
	// List conflicted files
	out, err := gitCmd(vaultPath, 10*time.Second, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false, err
	}
	if out == "" {
		return false, nil
	}

	// Compute merge-base ORIG_HEAD..rebaseTarget once so we can attribute
	// each conflicted file to the upstream commit responsible. ORIG_HEAD
	// is set by `git rebase` to the pre-rebase tip, so this resolves to
	// the fork point regardless of how many commits the rebase has
	// already replayed.
	base, baseErr := gitCmd(vaultPath, 10*time.Second, "merge-base", "ORIG_HEAD", rebaseTarget)
	if baseErr != nil {
		// Non-fatal: without a merge-base we cannot record dropped
		// upstream commits, but we can still resolve the conflicts.
		base = ""
	}

	for f := range strings.SplitSeq(out, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		class := Classify(f)

		// For Manual class, attribute the upstream-side content to the
		// commit that introduced it BEFORE we drop it via checkout.
		if class == Manual && base != "" {
			if dropped, ok := lookupDroppedUpstream(vaultPath, base, rebaseTarget, f); ok {
				result.DroppedUpstream = append(result.DroppedUpstream, dropped)
			}
		}

		// Keep LOCAL across all classes. During rebase, --theirs is the
		// commit being replayed (= local work).
		if _, coErr := gitCmd(vaultPath, 10*time.Second, "checkout", "--theirs", f); coErr != nil {
			return false, fmt.Errorf("checkout --theirs %s: %w", f, coErr)
		}
		if _, addErr := gitCmd(vaultPath, 10*time.Second, "add", f); addErr != nil {
			return false, fmt.Errorf("add %s: %w", f, addErr)
		}

		if class == Regenerable {
			result.Regenerate = true
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

// lookupDroppedUpstream queries git for the most recent upstream commit
// that touched path between base and rebaseTarget. Returns the populated
// DroppedUpstream record on success and false otherwise.
func lookupDroppedUpstream(vaultPath, base, rebaseTarget, path string) (DroppedUpstream, bool) {
	rng := base + ".." + rebaseTarget
	out, err := gitCmd(vaultPath, 10*time.Second,
		"log", "-1", "--format=%H%x00%s%x00%cI", rng, "--", path)
	if err != nil || out == "" {
		return DroppedUpstream{}, false
	}
	parts := strings.SplitN(out, "\x00", 3)
	if len(parts) != 3 {
		return DroppedUpstream{}, false
	}
	committedAt, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
	if parseErr != nil {
		// Tolerate unexpected formats by recording a zero time rather
		// than dropping the entry entirely; the SHA + subject still
		// give the operator a recovery handle.
		committedAt = time.Time{}
	}
	return DroppedUpstream{
		Path:               path,
		DroppedSHA:         strings.TrimSpace(parts[0]),
		DroppedSubject:     parts[1],
		DroppedCommittedAt: committedAt,
	}, true
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
// Output is whitespace-trimmed — convenient for SHA / branch-name
// parsing but unsafe for `--porcelain -z` style streams whose leading
// space is part of the status field. Use gitCmdRaw for those.
func gitCmd(dir string, timeout time.Duration, args ...string) (string, error) {
	out, err := gitCmdRaw(dir, timeout, args...)
	return strings.TrimSpace(out), err
}

// gitCmdRaw is the byte-faithful variant of gitCmd: identical
// invocation but the returned output is NOT whitespace-trimmed.
// Required for `git status --porcelain -z` and any other parser that
// treats leading/trailing whitespace as significant.
func gitCmdRaw(dir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Prevent interactive prompts
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	out, err := cmd.CombinedOutput()
	return string(out), err
}
