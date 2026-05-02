// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import (
	"os/exec"
	"strings"
)

// resolveDefaultBranch returns the short name of the upstream default
// branch (typically "main" or "master"), determined from the current
// origin/HEAD symbolic-ref. If origin/HEAD is unset or the remote does
// not exist, falls back to "main".
func resolveDefaultBranch(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short",
		"refs/remotes/origin/HEAD").Output()
	if err != nil {
		return "main"
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "origin/")
}
