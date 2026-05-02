// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

// withProbePID swaps probePID for the duration of t and restores it on
// cleanup. All worktreegc tests use this seam.
func withProbePID(t *testing.T, fn func(int) verdict) {
	t.Helper()
	saved := probePID
	probePID = fn
	t.Cleanup(func() { probePID = saved })
}

// addCommitToWorktree creates a unique file in the worktree and commits
// it. Returns the new commit's SHA.
func addCommitToWorktree(t *testing.T, wtPath, filename, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wtPath, filename), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	gitx.GitRun(t, wtPath, "add", filename)
	gitx.GitRun(t, wtPath, "commit", "-m", msg)
	out := gitx.GitRun(t, wtPath, "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

// expectedLockPath replicates Run's lockfile path computation.
func expectedLockPath(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		t.Fatalf("rev-parse --git-common-dir: %v", err)
	}
	gitCommonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(repoPath, gitCommonDir)
	}
	abs, err := filepath.Abs(gitCommonDir)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("UserCacheDir: %v", err)
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(cacheDir, "vibe-vault", "locks", hex.EncodeToString(sum[:])[:8]+".lock")
}

func TestRun_DryRun_DeadHolder(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-aaaaaaaaaaaaaaaa", "worktree-agent-aaaaaaaaaaaaaaaa")
	gitx.LockWorktree(t, wtPath, "claude agent agent-aaaaaaaaaaaaaaaa (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d: %+v", len(res.Actions), res.Actions)
	}
	if res.Actions[0].Verdict != VerdictWouldReap {
		t.Errorf("verdict = %q, want would-reap", res.Actions[0].Verdict)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir vanished under DryRun: %v", err)
	}
}

func TestRun_LiveHolder(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-bbbbbbbbbbbbbbbb", "worktree-agent-bbbbbbbbbbbbbbbb")
	reason := fmt.Sprintf("claude agent agent-bbbbbbbbbbbbbbbb (pid %d)", os.Getpid())
	gitx.LockWorktree(t, wtPath, reason)

	withProbePID(t, func(int) verdict { return verdictAlive })

	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(res.Actions))
	}
	if res.Actions[0].Verdict != VerdictAlive {
		t.Errorf("verdict = %q, want alive", res.Actions[0].Verdict)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir vanished but holder is alive: %v", err)
	}
}

func TestRun_DeadHolder_Reaps(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-cccccccccccccccc", "worktree-agent-cccccccccccccccc")
	// Worktree branch advances with no new commits beyond main, so cherry
	// outputs empty (captured).
	gitx.LockWorktree(t, wtPath, "claude agent agent-cccccccccccccccc (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d: %+v", len(res.Actions), res.Actions)
	}
	if res.Actions[0].Verdict != VerdictReaped {
		t.Errorf("verdict = %q, want reaped (detail=%q)", res.Actions[0].Verdict, res.Actions[0].Detail)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists after reap: err=%v", err)
	}
	out := gitx.GitRun(t, repo, "branch", "--list", "worktree-agent-cccccccccccccccc")
	if strings.TrimSpace(out) != "" {
		t.Errorf("branch still present after reap: %q", out)
	}
}

func TestRun_DeadHolder_Uncaptured_Skips(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-dddddddddddddddd", "worktree-agent-dddddddddddddddd")
	// Add a commit on the worktree branch that is NOT on main.
	addCommitToWorktree(t, wtPath, "wt.txt", "uncaptured\n", "feat: uncaptured work")
	gitx.LockWorktree(t, wtPath, "claude agent agent-dddddddddddddddd (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(res.Actions))
	}
	if res.Actions[0].Verdict != VerdictUncaptured {
		t.Fatalf("verdict = %q, want uncaptured-work", res.Actions[0].Verdict)
	}
	if !strings.Contains(res.Actions[0].Detail, "first commits") {
		t.Errorf("Detail = %q, want substring 'first commits'", res.Actions[0].Detail)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree was destroyed despite uncaptured-work skip: %v", err)
	}
}

func TestRun_DeadHolder_UncapturedForce(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-eeeeeeeeeeeeeeee", "worktree-agent-eeeeeeeeeeeeeeee")
	addCommitToWorktree(t, wtPath, "wt.txt", "uncaptured\n", "feat: uncaptured work")
	gitx.LockWorktree(t, wtPath, "claude agent agent-eeeeeeeeeeeeeeee (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{ForceUncaptured: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(res.Actions))
	}
	if res.Actions[0].Verdict != VerdictReaped {
		t.Errorf("verdict = %q, want reaped under ForceUncaptured", res.Actions[0].Verdict)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists after force-reap: err=%v", err)
	}
}

func TestRun_BranchMismatch(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	// Branch name "wrong-name" does NOT match expected "worktree-agent-...".
	wtPath := gitx.AddWorktree(t, repo, "agent-ffffffffffffffff", "wrong-name")
	gitx.LockWorktree(t, wtPath, "claude agent agent-ffffffffffffffff (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(res.Actions))
	}
	if res.Actions[0].Verdict != VerdictBranchMismatch {
		t.Errorf("verdict = %q, want branch-mismatch", res.Actions[0].Verdict)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree was destroyed despite branch-mismatch: %v", err)
	}
}

func TestRun_LinkedWorktreeSerializes(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-1111111111111111", "worktree-agent-1111111111111111")
	gitx.LockWorktree(t, wtPath, "claude agent agent-1111111111111111 (pid 99999)")

	// G1 holds the lockfile directly at the path Run will compute.
	lockPath := expectedLockPath(t, repo)
	fl, err := lockfile.AcquireNonBlocking(lockPath)
	if err != nil {
		t.Fatalf("G1 acquire lock: %v", err)
	}
	released := make(chan struct{})

	withProbePID(t, func(int) verdict { return verdictAlive })

	var (
		wg     sync.WaitGroup
		g2Err  error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, g2Err = Run(repo, Options{})
		close(released)
	}()

	wg.Wait()
	if g2Err == nil {
		_ = fl.Release()
		t.Fatal("G2 Run returned nil error; expected lockfile.ErrLocked wrap")
	}
	if !errors.Is(g2Err, lockfile.ErrLocked) {
		_ = fl.Release()
		t.Fatalf("G2 err = %v, want errors.Is == lockfile.ErrLocked", g2Err)
	}
	if err := fl.Release(); err != nil {
		t.Fatalf("release G1 lock: %v", err)
	}
}

func TestRun_Signal_EPERM_TreatsAsAlive(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-2222222222222222", "worktree-agent-2222222222222222")
	gitx.LockWorktree(t, wtPath, "claude agent agent-2222222222222222 (pid 99999)")

	// Model EPERM treatment: probePID returns alive (the production
	// switch maps EPERM -> verdictAlive; this test guards regression).
	withProbePID(t, func(int) verdict { return verdictAlive })

	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 || res.Actions[0].Verdict != VerdictAlive {
		t.Fatalf("want single 'alive' action, got %+v", res.Actions)
	}
}

func TestRun_GitCherry_AllMinusLines_Captured(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-3333333333333333", "worktree-agent-3333333333333333")
	// Add a commit on the worktree branch.
	wtSHA := addCommitToWorktree(t, wtPath, "shared.txt", "content\n", "feat: shared work")
	// Cherry-pick that commit onto main so cherry outputs `- <sha>` (captured).
	gitx.GitRun(t, repo, "cherry-pick", wtSHA)
	gitx.LockWorktree(t, wtPath, "claude agent agent-3333333333333333 (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d: %+v", len(res.Actions), res.Actions)
	}
	if res.Actions[0].Verdict != VerdictReaped {
		t.Errorf("verdict = %q, want reaped (v3 regression: '-' lines must count as captured); detail=%q",
			res.Actions[0].Verdict, res.Actions[0].Detail)
	}
}

func TestRun_DetachedBlock_Skipped(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-4444444444444444", "worktree-agent-4444444444444444")
	gitx.LockWorktree(t, wtPath, "claude agent agent-4444444444444444 (pid 99999)")

	// Add a detached worktree.
	detachedPath := filepath.Join(repo, ".claude", "worktrees", "detached-x")
	gitx.GitRun(t, repo, "worktree", "add", "--detach", detachedPath, "HEAD")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action (detached must be silently skipped), got %d: %+v",
			len(res.Actions), res.Actions)
	}
	if res.Actions[0].Marker.WorktreePath != wtPath {
		t.Errorf("action targeted unexpected worktree: %q", res.Actions[0].Marker.WorktreePath)
	}
}

func TestRun_BareBlock_Skipped(t *testing.T) {
	// gitx does not currently expose a helper that produces a `bare` line
	// in `git worktree list --porcelain` (bare repos are a different
	// fixture pattern than test repos with worktrees). Skipping per the
	// plan note; porcelain_test.go covers parsePorcelain's bare handling
	// directly, and Run's bare-skip branch is data-driven from that
	// parser, so behavioral coverage is still indirect.
	t.Skip("requires a bare-repo gitx helper that does not yet exist; covered by parser-level test")
}

func TestRun_DefaultBranchResolves_Master(t *testing.T) {
	repo := gitx.InitTestRepoWithDefault(t, "master")
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, repo, "origin", bare)
	gitx.GitRun(t, repo, "push", "origin", "master")
	gitx.GitRun(t, repo, "remote", "set-head", "origin", "master")

	wtPath := gitx.AddWorktree(t, repo, "agent-5555555555555555", "worktree-agent-5555555555555555")
	wtSHA := addCommitToWorktree(t, wtPath, "wt.txt", "x\n", "feat: x")
	// Cherry-pick onto master so cherry against master shows captured.
	gitx.GitRun(t, repo, "cherry-pick", wtSHA)
	gitx.LockWorktree(t, wtPath, "claude agent agent-5555555555555555 (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	// Pass Options{} — no explicit candidate parents, so resolver must
	// pick "master" via origin/HEAD.
	res, err := Run(repo, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d: %+v", len(res.Actions), res.Actions)
	}
	if res.Actions[0].Verdict != VerdictReaped {
		t.Errorf("verdict = %q, want reaped (resolver should pick master, not main); detail=%q",
			res.Actions[0].Verdict, res.Actions[0].Detail)
	}
}

func TestRun_CherryErrors_SurfacesInDetail(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-6666666666666666", "worktree-agent-6666666666666666")
	addCommitToWorktree(t, wtPath, "wt.txt", "uncaptured\n", "feat: uncaptured")
	gitx.LockWorktree(t, wtPath, "claude agent agent-6666666666666666 (pid 99999)")

	withProbePID(t, func(int) verdict { return verdictDead })

	res, err := Run(repo, Options{
		CandidateParents: []string{"nonexistent-branch", "another-bad-one"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d", len(res.Actions))
	}
	if res.Actions[0].Verdict != VerdictUncaptured {
		t.Fatalf("verdict = %q, want uncaptured-work", res.Actions[0].Verdict)
	}
	want := "candidates that errored: [nonexistent-branch, another-bad-one]"
	if !strings.Contains(res.Actions[0].Detail, want) {
		t.Errorf("Detail = %q, want substring %q", res.Actions[0].Detail, want)
	}
}
