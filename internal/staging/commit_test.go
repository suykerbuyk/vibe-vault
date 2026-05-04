// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCommit_HappyPath stages and commits a fresh note. Verifies
// the staging git log records the commit and the working tree is
// clean afterwards.
func TestCommit_HappyPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")

	notePath := filepath.Join(stagingDir, "2026-05-03-143025123.md")
	if err := os.WriteFile(notePath, []byte("# hello\n"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	if err := Commit(stagingDir, notePath, "session: demo/2026-05-03-143025123.md"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Working tree clean post-commit.
	porcelain := runGit(t, stagingDir, "status", "--porcelain")
	if strings.TrimSpace(porcelain) != "" {
		t.Errorf("working tree dirty after Commit: %q", porcelain)
	}

	// Commit landed in the log.
	logOut := runGit(t, stagingDir, "log", "--oneline", "-1")
	if !strings.Contains(logOut, "demo/2026-05-03-143025123.md") {
		t.Errorf("git log = %q, want commit subject containing the filename", logOut)
	}
}

// TestCommit_NoOpClean: re-staging an already-committed file produces
// no new commit. Without the porcelain probe in Commit, `git commit`
// would fail with "nothing to commit, working tree clean" and the
// hook would log a spurious WARN per fire after the first.
func TestCommit_NoOpClean(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")

	notePath := filepath.Join(stagingDir, "n.md")
	if err := os.WriteFile(notePath, []byte("body\n"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	if err := Commit(stagingDir, notePath, "first"); err != nil {
		t.Fatalf("first Commit: %v", err)
	}
	commitsBefore := runGit(t, stagingDir, "rev-list", "--count", "HEAD")
	if err := Commit(stagingDir, notePath, "second (should be no-op)"); err != nil {
		t.Errorf("no-op Commit returned error: %v", err)
	}
	commitsAfter := runGit(t, stagingDir, "rev-list", "--count", "HEAD")
	if strings.TrimSpace(commitsBefore) != strings.TrimSpace(commitsAfter) {
		t.Errorf("commit count changed on no-op: %q -> %q",
			strings.TrimSpace(commitsBefore), strings.TrimSpace(commitsAfter))
	}
}

// TestCommit_FailsOnNonRepo asserts the fail-safe contract from the
// caller's perspective: when stagingDir is not a git repo, Commit
// returns an error rather than panicking. session.CaptureFromParsed
// turns this into a logged WARN; the markdown file stays on disk.
func TestCommit_FailsOnNonRepo(t *testing.T) {
	dir := t.TempDir()
	notePath := filepath.Join(dir, "n.md")
	if err := os.WriteFile(notePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	err := Commit(dir, notePath, "msg")
	if err == nil {
		t.Error("Commit on non-repo returned nil, want error")
	}
}

// TestCommit_RejectsEmpty locks the parameter validation: empty
// inputs are programming errors, not silent no-ops.
func TestCommit_RejectsEmpty(t *testing.T) {
	if err := Commit("", "/tmp/x.md", "msg"); err == nil {
		t.Error("Commit empty stagingDir = nil, want error")
	}
	if err := Commit("/tmp", "", "msg"); err == nil {
		t.Error("Commit empty absPath = nil, want error")
	}
	if err := Commit("/tmp", "/tmp/x.md", ""); err == nil {
		t.Error("Commit empty msg = nil, want error")
	}
}

// BenchmarkCommit measures the warm-path commit cost. Plan target
// is ≤100ms warm; encode it as a t.Skip on regression so CI on slow
// hosts doesn't false-fail but the local benchmark surfaces the
// number for tuning.
func BenchmarkCommit(b *testing.B) {
	root := b.TempDir()
	b.Setenv("XDG_STATE_HOME", root)
	b.Setenv("VIBE_VAULT_HOSTNAME", "benchhost")
	if err := Init("demo"); err != nil {
		b.Fatalf("Init: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Unique filename per iteration so we measure the real
		// add+commit path, not the no-op fast path.
		when := time.Now()
		notePath := filepath.Join(stagingDir,
			when.Format("20060102-150405.000000000")+".md")
		if err := os.WriteFile(notePath, []byte("body\n"), 0o644); err != nil {
			b.Fatalf("write note: %v", err)
		}
		if err := Commit(stagingDir, notePath, "bench"); err != nil {
			b.Fatalf("Commit: %v", err)
		}
	}
}
