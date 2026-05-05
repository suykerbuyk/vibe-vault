// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitCmdRunner runs a git command in dir and returns its stdout. Test seam.
//
// H1-v6 hardening: pins GIT_TERMINAL_PROMPT=0 (no credential prompts) and
// GIT_EDITOR=true (no interactive editor) on cmd.Env. Mirrors the
// vaultsync.gitCmd wrapper pattern (internal/vaultsync/vaultsync.go) added
// in iter 216 (commit 8df6e09) to prevent hangs on hosts where core.editor
// is configured to an interactive command (vim, nano). Read-only operations
// rarely invoke the editor or credential helper, but defense-in-depth +
// parity with the canonical vaultsync wrapper is the lower-friction call.
var gitCmdRunner = func(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// vaultHasUncommittedWrites returns true iff `git status --porcelain` in
// vaultPath produces any output. Returns false and a nil error when
// vaultPath is empty or not a git repo.
//
// When project is non-empty, the probe is scoped to the project's own
// subtree at `Projects/<project>/` — only uncommitted files inside that
// subtree flip the result to true. This matches the per-project intent
// of the wrap-state collector + preflight: a sibling project's dirty
// state must not falsely trip the vault-dirty preflight warning for
// *this* project.
//
// When project is empty, the probe degrades to whole-vault behavior
// (back-compat for any non-wrap caller).
func vaultHasUncommittedWrites(vaultPath, project string) (bool, error) {
	if vaultPath == "" {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".git")); err != nil {
		// Not a git repo (or unreadable); treat as clean — matches the
		// "no signal available" interpretation in D6.
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	args := []string{"status", "--porcelain"}
	if project != "" {
		// `--` separates pathspec from refs and forces git to interpret
		// the trailing arg as a path. Trailing slash limits the match to
		// the subtree.
		args = append(args, "--", "Projects/"+project+"/")
	}
	out, err := gitCmdRunner(ctx, vaultPath, args...)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// projectHasUncommittedWrites returns true iff `git status --porcelain` in
// projectDir produces any output. Returns false and a nil error when
// projectDir is empty or not a git repo. Mirrors vaultHasUncommittedWrites
// for project-side cleanliness probing per C4 fix.
func projectHasUncommittedWrites(projectDir string) (bool, error) {
	if projectDir == "" {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".git")); err != nil {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	out, err := gitCmdRunner(ctx, projectDir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// lastIterAnchorSha returns the SHA of the most recent commit that
// touched the project's iter stamp file (.vibe-vault/last-iter).
// Empty string when the file is not yet tracked — the canonical
// "no prior wrap" signal handled by the wrap.md fallback.
//
// This replaces the deprecated commit-body footer search (DESIGN
// #93). The stamp file is written every wrap by vv_stamp_iter, so
// `git log -n 1 -- .vibe-vault/last-iter` is the canonical anchor.
func lastIterAnchorSha(cwd string) (string, error) {
	if cwd == "" {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := gitCmdRunner(ctx, cwd,
		"log", "-n", "1", "--format=%H", "--",
		".vibe-vault/last-iter")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}
