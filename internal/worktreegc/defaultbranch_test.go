// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import (
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

func TestResolveDefaultBranch_MasterRepo(t *testing.T) {
	repo := gitx.InitTestRepoWithDefault(t, "master")
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, repo, "origin", bare)
	// Push master so origin has a master ref.
	gitx.GitRun(t, repo, "push", "origin", "master")
	// Set origin/HEAD -> origin/master.
	gitx.GitRun(t, repo, "remote", "set-head", "origin", "master")

	got := resolveDefaultBranch(repo)
	if got != "master" {
		t.Errorf("resolveDefaultBranch = %q, want master", got)
	}
}

func TestResolveDefaultBranch_NoOrigin_FallsBackToMain(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	got := resolveDefaultBranch(repo)
	if got != "main" {
		t.Errorf("resolveDefaultBranch = %q, want main", got)
	}
}

func TestResolveDefaultBranch_OriginHEADUnset_FallsBackToMain(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, repo, "origin", bare)
	// Note: deliberately do NOT call `remote set-head` — origin/HEAD is unset.
	got := resolveDefaultBranch(repo)
	if got != "main" {
		t.Errorf("resolveDefaultBranch = %q, want main fallback when origin/HEAD unset", got)
	}
}
