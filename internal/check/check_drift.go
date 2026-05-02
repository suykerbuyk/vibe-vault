// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// driftIterRe matches the H3 narrative header used in iterations.md
// (e.g., "### Iteration 168 — title (date)"). Mirrors
// internal/mcp/tools_describe_iter_state.go's iterNarrativeRe — the
// regex is duplicated here rather than imported because internal/mcp
// must not depend on internal/check and vice versa. Keep the two in
// sync if the narrative-header format ever changes.
var driftIterRe = regexp.MustCompile(`(?m)^### Iteration (\d+)\b`)

// CheckWrapIterDrift compares the project's just-stamped iter (from
// .vibe-vault/last-iter) with the vault's max iter (from
// iterations.md "### Iteration N" headers). PASS if equal, ahead, or
// either signal is unavailable; WARN if local < vault.
//
// Gated on default-branch identity. Returns Pass-with-skipped-detail
// on feature branches and detached HEAD — drift detection is
// meaningful only when the project tree is on its mainline. The
// gating mirrors the session-slot-multihost-disambiguation Phase 0a
// contract.
//
// Returns nil when scoping inputs are empty (vaultPath, project, or
// repoPath unset, or project resolves to "_unknown") — matches the
// existing project-scoped-check convention used by CheckMemoryLink
// and CheckCurrentStateInvariants.
func CheckWrapIterDrift(repoPath, vaultPath, project string) *Result {
	if repoPath == "" || vaultPath == "" || project == "" || project == "_unknown" {
		return nil
	}

	defaultBranch, ok := defaultBranch(repoPath)
	if !ok {
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Pass,
			Detail: "skipped (cannot determine default branch)",
		}
	}

	currentBranch, ok := currentBranch(repoPath)
	if !ok {
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Pass,
			Detail: "skipped (detached HEAD)",
		}
	}
	if currentBranch != defaultBranch {
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Pass,
			Detail: fmt.Sprintf("skipped (feature branch %s)", currentBranch),
		}
	}

	localIter, haveLocal := readLastIterStamp(repoPath)
	if !haveLocal {
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Pass,
			Detail: "no local stamp",
		}
	}

	vaultIter, haveVault := readVaultMaxIter(vaultPath, project)
	if !haveVault {
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Pass,
			Detail: "no vault iterations",
		}
	}

	switch {
	case localIter == vaultIter:
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Pass,
			Detail: fmt.Sprintf("in sync (iter %d)", localIter),
		}
	case localIter < vaultIter:
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Warn,
			Detail: fmt.Sprintf("local iter %d behind vault iter %d — run `vv vault pull && git pull`", localIter, vaultIter),
		}
	default:
		// localIter > vaultIter — normal between local wrap and vault push.
		return &Result{
			Name:   "wrap-iter-drift",
			Status: Pass,
			Detail: fmt.Sprintf("local iter %d ahead of vault %d (push pending)", localIter, vaultIter),
		}
	}
}

// defaultBranch returns the project's default branch name (e.g.,
// "main", "master") and a found-flag. Tries
// `git symbolic-ref --short refs/remotes/origin/HEAD` first (strips
// the "origin/" prefix); falls back to probing for "main" then
// "master" via `git rev-parse --verify`. Returns ("", false) when no
// default branch can be identified.
func defaultBranch(repoPath string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if out, err := cmd.Output(); err == nil {
		name := strings.TrimSpace(string(out))
		name = strings.TrimPrefix(name, "origin/")
		if name != "" {
			return name, true
		}
	}
	for _, candidate := range []string{"main", "master"} {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
		probe := exec.CommandContext(ctx2, "git", "-C", repoPath, "rev-parse", "--verify", candidate)
		err := probe.Run()
		cancel2()
		if err == nil {
			return candidate, true
		}
	}
	return "", false
}

// currentBranch returns the current branch name and a found-flag.
// Returns ("", false) on detached HEAD or any git failure.
func currentBranch(repoPath string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "symbolic-ref", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		// `symbolic-ref --short HEAD` exits non-zero on detached HEAD;
		// distinguishing detached from "not a git repo" isn't required
		// here — both produce SKIP. Caller treats false as detached.
		return "", false
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", false
	}
	return name, true
}

// readLastIterStamp reads <repoPath>/.vibe-vault/last-iter, trims
// whitespace, and parses it as an int. Returns (0, false) when the
// file is missing, empty, or non-numeric.
func readLastIterStamp(repoPath string) (int, bool) {
	path := filepath.Join(repoPath, ".vibe-vault", "last-iter")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// readVaultMaxIter parses <vaultPath>/Projects/<project>/agentctx/iterations.md
// for "### Iteration N" headers and returns max(N) plus a found-flag.
// Returns (0, false) when the file is missing or contains no matching
// headers.
//
// Mirrors internal/mcp/tools_describe_iter_state.go nextIterFromIterationsMD's
// parsing logic but returns max(N) instead of max(N)+1, since the
// drift contract compares last-stamped vs vault-max directly. The
// regex is duplicated (driftIterRe) rather than imported across the
// internal/mcp boundary.
func readVaultMaxIter(vaultPath, project string) (int, bool) {
	if vaultPath == "" || project == "" {
		return 0, false
	}
	path := filepath.Join(vaultPath, "Projects", project, "agentctx", "iterations.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	max := 0
	found := false
	for _, m := range driftIterRe.FindAllStringSubmatch(string(data), -1) {
		if len(m) < 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err == nil {
			found = true
			if n > max {
				max = n
			}
		}
	}
	if !found {
		return 0, false
	}
	return max, true
}
