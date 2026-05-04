// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"fmt"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/vaultsync"
)

// Commit stages and commits absPath in the staging git repo rooted at
// stagingDir. Two fork-execs (`git add` then `git commit`) gated through
// vaultsync.GitCommand — the same hardened wrapper the vault uses, with
// gitTimeout per call.
//
// No-op-safe: a clean working tree (e.g. caller wrote a byte-identical
// file, or pre-staged the file already) returns nil without invoking
// `git commit` (which would fail on "nothing to commit"). Probe via
// `git status --porcelain` after `git add`.
//
// No remote, no push, no batching. Per-fire single-file staging
// commits intentionally bypass the wrap-time CommitAndPushPaths
// surface to keep the hook fast and decoupled from vault remotes.
//
// Target ≤100ms warm; benchmarked in tests.
func Commit(stagingDir, absPath, msg string) error {
	if stagingDir == "" {
		return fmt.Errorf("staging.Commit: stagingDir is empty")
	}
	if absPath == "" {
		return fmt.Errorf("staging.Commit: absPath is empty")
	}
	if msg == "" {
		return fmt.Errorf("staging.Commit: msg is empty")
	}

	// `git add -- <absPath>` accepts absolute paths inside the repo.
	if _, err := vaultsync.GitCommand(stagingDir, gitTimeout, "add", "--", absPath); err != nil {
		return fmt.Errorf("git add %s: %w", absPath, err)
	}

	// Probe before commit: if nothing is staged (re-add of identical
	// content), skip the commit rather than fail with "nothing to
	// commit, working tree clean".
	porcelain, err := vaultsync.GitCommand(stagingDir, gitTimeout, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(porcelain) == "" {
		return nil // no changes to commit; not an error
	}

	if _, err := vaultsync.GitCommand(stagingDir, gitTimeout, "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}
	return nil
}
