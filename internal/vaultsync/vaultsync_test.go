// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package vaultsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		path string
		want FileClass
	}{
		// Regenerable
		{"Projects/foo/history.md", Regenerable},
		{"history.md", Regenerable},
		{".vibe-vault/session-index.json", Regenerable},
		{".vibe-vault/session-index.json.bak", Regenerable},

		// AppendOnly
		{"Projects/foo/sessions/2026-03-26/123-session.md", AppendOnly},
		{"Projects/bar/sessions/note.md", AppendOnly},

		// ConfigFile
		{"Templates/agentctx/CLAUDE.md", ConfigFile},
		{"Templates/agentctx/commands/restart.md", ConfigFile},
		{".vibe-vault/config.toml", ConfigFile},

		// Manual (everything else)
		{"Projects/foo/knowledge.md", Manual},
		{"Projects/foo/agentctx/resume.md", Manual},
		{"Projects/foo/agentctx/iterations.md", Manual},
		{"Projects/foo/agentctx/tasks/my-task.md", Manual},
		{"Dashboards/overview.md", Manual},
		{"README.md", Manual},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := Classify(tt.path)
			if got != tt.want {
				t.Errorf("Classify(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

func TestGetStatus_CleanRepo(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	s, err := GetStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Clean {
		t.Error("expected clean repo")
	}
	if s.Branch != "main" {
		t.Errorf("branch = %q, want main", s.Branch)
	}
	if s.HasRemote() {
		t.Error("expected no remote")
	}
}

func TestGetStatus_DirtyRepo(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("dirty"), 0o644)

	s, err := GetStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Clean {
		t.Error("expected dirty repo")
	}
}

func TestGetStatus_MultipleRemotes(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare1 := gitx.InitBareRemote(t)
	bare2 := gitx.InitBareRemote(t)

	gitx.AddRemote(t, dir, "github", bare1)
	gitx.AddRemote(t, dir, "vault", bare2)

	// Push to both so remote refs exist
	gitx.GitRun(t, dir, "push", "github", "main")
	gitx.GitRun(t, dir, "push", "vault", "main")

	s, err := GetStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasRemote() {
		t.Fatal("expected remotes")
	}
	if len(s.Remotes) != 2 {
		t.Fatalf("got %d remotes, want 2", len(s.Remotes))
	}

	names := map[string]bool{}
	for _, r := range s.Remotes {
		names[r.Name] = true
		if r.Ahead != 0 || r.Behind != 0 {
			t.Errorf("remote %s: ahead=%d behind=%d, want 0/0", r.Name, r.Ahead, r.Behind)
		}
	}
	if !names["github"] || !names["vault"] {
		t.Errorf("expected remotes github and vault, got %v", names)
	}
}

func TestGetStatus_AheadBehind(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "github", bare)
	gitx.GitRun(t, dir, "push", "github", "main")

	// Create a local commit (ahead by 1)
	os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local"), 0o644)
	gitx.GitRun(t, dir, "add", ".")
	gitx.GitRun(t, dir, "commit", "-m", "local change")

	s, err := GetStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Remotes) != 1 {
		t.Fatalf("got %d remotes, want 1", len(s.Remotes))
	}
	if s.Remotes[0].Ahead != 1 {
		t.Errorf("ahead = %d, want 1", s.Remotes[0].Ahead)
	}
}

func TestListRemotes_None(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	remotes, err := listRemotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 0 {
		t.Errorf("got %d remotes, want 0", len(remotes))
	}
}

func TestListRemotes_Multiple(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	gitx.AddRemote(t, dir, "github", "https://example.com/a.git")
	gitx.AddRemote(t, dir, "vault", "https://example.com/b.git")

	remotes, err := listRemotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 2 {
		t.Fatalf("got %d remotes, want 2", len(remotes))
	}
	names := map[string]bool{}
	for _, r := range remotes {
		names[r] = true
	}
	if !names["github"] || !names["vault"] {
		t.Errorf("expected github and vault, got %v", names)
	}
}

func TestCommitAndPush_NoRemote(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644)

	_, err := CommitAndPush(dir, "test commit")
	if err == nil {
		t.Fatal("expected error when no remote configured")
	}
	if got := err.Error(); !contains(got, "no git remote") {
		t.Errorf("error = %q, want mention of no remote", got)
	}
}

func TestCommitAndPush_NothingToCommit(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	gitx.AddRemote(t, dir, "github", "https://example.com/test.git")

	result, err := CommitAndPush(dir, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if result.AnyPushed() {
		t.Error("expected no push when nothing to commit")
	}
	if result.CommitSHA != "" {
		t.Error("expected no commit SHA")
	}
}

func TestCommitAndPush_MultipleRemotes(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare1 := gitx.InitBareRemote(t)
	bare2 := gitx.InitBareRemote(t)

	gitx.AddRemote(t, dir, "github", bare1)
	gitx.AddRemote(t, dir, "vault", bare2)

	// Push initial commit to both so remote refs exist
	gitx.GitRun(t, dir, "push", "github", "main")
	gitx.GitRun(t, dir, "push", "vault", "main")

	// Create a new file to commit
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("data"), 0o644)

	result, err := CommitAndPush(dir, "multi-remote push")
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitSHA == "" {
		t.Error("expected a commit SHA")
	}
	if !result.AllPushed() {
		t.Error("expected all remotes pushed")
	}
	if len(result.RemoteResults) != 2 {
		t.Errorf("got %d remote results, want 2", len(result.RemoteResults))
	}
	for name, pushErr := range result.RemoteResults {
		if pushErr != nil {
			t.Errorf("remote %s: unexpected error: %v", name, pushErr)
		}
	}
}

func TestCommitAndPush_NoIdentity(t *testing.T) {
	gitx.SandboxNoIdentity(t)
	dir := gitx.InitTestRepoNoIdentity(t)

	// Create a file so there's something stage-able. Probe should
	// fail before staging anyway, but having a file proves no commit
	// was created post-failure.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := CommitAndPush(dir, "should fail")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no git identity") {
		t.Errorf("error missing 'no git identity': %q", msg)
	}
	if !strings.Contains(msg, "git config --global user.email") {
		t.Errorf("error missing remediation hint: %q", msg)
	}

	// Verify no commit was created. `git log` on a repo with no
	// commits exits non-zero with "does not have any commits yet".
	// Use exec.Command directly to avoid gitx.GitRun's identity injection.
	cmd := exec.Command("git", "-C", dir, "log", "--oneline")
	cmd.Env = []string{"HOME=" + t.TempDir()}
	out, _ := cmd.CombinedOutput()
	if len(out) > 0 && !strings.Contains(string(out), "does not have any commits") &&
		!strings.Contains(string(out), "bad default revision") {
		t.Errorf("expected no commits, got: %s", out)
	}
}

func TestPull_NoIdentity(t *testing.T) {
	gitx.SandboxNoIdentity(t)
	dir := gitx.InitTestRepoNoIdentity(t)

	// Configure a remote so the early no-remotes guard would otherwise
	// pass and Pull would proceed to rebase. The probe must reject
	// before listRemotes is even consulted.
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)

	_, err := Pull(dir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "no git identity") {
		t.Errorf("error missing 'no git identity': %q", msg)
	}
	if !strings.Contains(msg, "git config --global user.email") {
		t.Errorf("error missing remediation hint: %q", msg)
	}

	// Verify no commit was created. `git log` on a repo with no
	// commits exits non-zero with "does not have any commits yet".
	// Use exec.Command directly to avoid gitx.GitRun's identity injection.
	cmd := exec.Command("git", "-C", dir, "log", "--oneline")
	cmd.Env = []string{"HOME=" + t.TempDir()}
	out, _ := cmd.CombinedOutput()
	if len(out) > 0 && !strings.Contains(string(out), "does not have any commits") &&
		!strings.Contains(string(out), "bad default revision") {
		t.Errorf("expected no commits, got: %s", out)
	}
}

func TestPull_NoRemote(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	_, err := Pull(dir)
	if err == nil {
		t.Fatal("expected error when no remote configured")
	}
	if got := err.Error(); !contains(got, "no git remote") {
		t.Errorf("error = %q, want mention of no remote", got)
	}
}

func TestEnsureRemote(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	if err := EnsureRemote(dir); err == nil {
		t.Error("expected error when no remote")
	}

	gitx.AddRemote(t, dir, "github", "https://example.com/test.git")

	if err := EnsureRemote(dir); err != nil {
		t.Errorf("unexpected error after adding remote: %v", err)
	}
}

func TestEnsureRemote_NonOriginName(t *testing.T) {
	dir := gitx.InitTestRepo(t)

	// Add a remote with a non-"origin" name — should still pass
	gitx.AddRemote(t, dir, "vault", "https://example.com/test.git")

	if err := EnsureRemote(dir); err != nil {
		t.Errorf("unexpected error with non-origin remote: %v", err)
	}
}

func TestPushResult_AllPushed(t *testing.T) {
	r := &PushResult{
		CommitSHA:     "abc123",
		RemoteResults: map[string]error{"a": nil, "b": nil},
	}
	if !r.AllPushed() {
		t.Error("expected AllPushed true")
	}
	if !r.AnyPushed() {
		t.Error("expected AnyPushed true")
	}
}

func TestPushResult_PartialPush(t *testing.T) {
	r := &PushResult{
		CommitSHA: "abc123",
		RemoteResults: map[string]error{
			"a": nil,
			"b": os.ErrNotExist,
		},
	}
	if r.AllPushed() {
		t.Error("expected AllPushed false")
	}
	if !r.AnyPushed() {
		t.Error("expected AnyPushed true")
	}
}

func TestPushResult_NoPush(t *testing.T) {
	r := &PushResult{
		CommitSHA:     "abc123",
		RemoteResults: map[string]error{"a": os.ErrNotExist},
	}
	if r.AllPushed() {
		t.Error("expected AllPushed false")
	}
	if r.AnyPushed() {
		t.Error("expected AnyPushed false")
	}
}

func TestPushResult_Empty(t *testing.T) {
	r := &PushResult{}
	if r.AllPushed() {
		t.Error("expected AllPushed false for empty results")
	}
	if r.AnyPushed() {
		t.Error("expected AnyPushed false for empty results")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// barePushUnrelated places an unrelated commit at refs/heads/main on
// the given bare repo by cloning it to a scratch tempdir, committing
// a unique file, and force-pushing back. Returns the SHA at which the
// bare's main now points.
//
// Inlined per test in the plan; the recipe is repeated five times
// across the new convergence tests. If a sixth call site appears,
// extract to gitx.
func barePushUnrelated(t *testing.T, bareDir, fileName, content string) string {
	t.Helper()
	scratch := t.TempDir()
	gitx.GitRun(t, scratch, "clone", "-b", "main", bareDir, ".")
	gitx.GitRun(t, scratch, "config", "user.email", "test@test.com")
	gitx.GitRun(t, scratch, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(scratch, fileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", fileName, err)
	}
	gitx.GitRun(t, scratch, "add", ".")
	gitx.GitRun(t, scratch, "commit", "-m", "unrelated: "+fileName)
	gitx.GitRun(t, scratch, "push", "origin", "main")
	sha := strings.TrimSpace(gitx.GitRun(t, scratch, "rev-parse", "HEAD"))
	return sha
}

// bareRefSHA reads refs/heads/main from a bare repo and returns the
// resolved SHA.
func bareRefSHA(t *testing.T, bareDir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", bareDir, "rev-parse", "refs/heads/main")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse refs/heads/main on %s: %s: %v", bareDir, out, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCommitAndPush_SHADivergenceConvergence_GithubFirst(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bareGithub := gitx.InitBareRemote(t)
	bareVault := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "github", bareGithub)
	gitx.AddRemote(t, dir, "vault", bareVault)

	// Seed both bares with the initial commit so neither rejects on
	// the first push attempt for an empty ref.
	gitx.GitRun(t, dir, "push", "github", "main")
	gitx.GitRun(t, dir, "push", "vault", "main")

	// Plant an unrelated commit on vault's bare so vault rejects the
	// next fast-forward push from `dir`. github accepts (FF), vault
	// rejects → fetch → rebase → push → convergence force-with-leases
	// github back into alignment.
	barePushUnrelated(t, bareVault, "vault-unrelated.txt", "vault unrelated state")

	// Stage a new local commit in `dir`.
	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local change"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := CommitAndPush(dir, "convergence test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.AllPushed() {
		t.Fatalf("expected AllPushed; got results=%v", result.RemoteResults)
	}

	githubSHA := bareRefSHA(t, bareGithub)
	vaultSHA := bareRefSHA(t, bareVault)
	if githubSHA != vaultSHA {
		t.Errorf("remotes diverged: github=%s vault=%s", githubSHA, vaultSHA)
	}
}

func TestCommitAndPush_SHADivergenceConvergence_RejecterFirst(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	// Names chosen so alphabetical iteration order puts the rejecter
	// first: the listRemotes output is sorted by `git remote`.
	bareReject := gitx.InitBareRemote(t)
	bareAccept := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "aaa-rejecter", bareReject)
	gitx.AddRemote(t, dir, "zzz-acceptor", bareAccept)

	gitx.GitRun(t, dir, "push", "aaa-rejecter", "main")
	gitx.GitRun(t, dir, "push", "zzz-acceptor", "main")

	// Plant unrelated state on the rejecter so the FIRST iterated
	// remote is the one that rejects.
	barePushUnrelated(t, bareReject, "reject-first.txt", "rejecter state")

	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local change"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := CommitAndPush(dir, "rejecter-first convergence")
	if err != nil {
		t.Fatal(err)
	}
	if !result.AllPushed() {
		t.Fatalf("expected AllPushed; got results=%v", result.RemoteResults)
	}

	rejectSHA := bareRefSHA(t, bareReject)
	acceptSHA := bareRefSHA(t, bareAccept)
	if rejectSHA != acceptSHA {
		t.Errorf("remotes diverged: rejecter=%s acceptor=%s", rejectSHA, acceptSHA)
	}
}

func TestCommitAndPush_LeaseRejectsConcurrentWriter(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bareGithub := gitx.InitBareRemote(t)
	bareVault := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "github", bareGithub)
	gitx.AddRemote(t, dir, "vault", bareVault)

	gitx.GitRun(t, dir, "push", "github", "main")
	gitx.GitRun(t, dir, "push", "vault", "main")

	// vault has unrelated state to force the rebase path.
	barePushUnrelated(t, bareVault, "vault-unrelated.txt", "vault unrelated")

	// Capture the third-party SHA that will be planted on github
	// AFTER our push to github but BEFORE the convergence
	// force-with-lease — the lease must reject and leave the bare at
	// this state.
	var thirdPartySHA string

	prev := afterPushHook
	t.Cleanup(func() { afterPushHook = prev })
	afterPushHook = func(remote string) {
		if remote != "github" {
			return
		}
		// Mid-flight: plant a concurrent commit on github's bare via
		// a scratch clone. This moves github's main off the SHA we
		// just recorded, so the convergence lease will reject.
		thirdPartySHA = barePushUnrelated(t, bareGithub, "github-concurrent.txt", "third-party writer")
		// One-shot: don't fire on subsequent hook calls within this
		// test (e.g., the post-rebase push to vault).
		afterPushHook = func(string) {}
	}

	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local change"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := CommitAndPush(dir, "lease rejection test")
	if err != nil {
		t.Fatal(err)
	}

	githubErr := result.RemoteResults["github"]
	if githubErr == nil {
		t.Fatal("expected non-nil error for github (lease should have rejected)")
	}
	if !strings.Contains(githubErr.Error(), "convergence rejected") {
		t.Errorf("github error missing 'convergence rejected': %v", githubErr)
	}
	if result.AllPushed() {
		t.Error("expected AllPushed false when lease rejects")
	}

	// github bare should be left at the third-party state — no
	// overwrite.
	githubSHA := bareRefSHA(t, bareGithub)
	if githubSHA != thirdPartySHA {
		t.Errorf("github bare overwritten by lease: got %s, want third-party %s",
			githubSHA, thirdPartySHA)
	}
}

func TestCommitAndPush_BothRemotesRebase(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bareGithub := gitx.InitBareRemote(t)
	bareVault := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "github", bareGithub)
	gitx.AddRemote(t, dir, "vault", bareVault)

	gitx.GitRun(t, dir, "push", "github", "main")
	gitx.GitRun(t, dir, "push", "vault", "main")

	// Both bares carry distinct unrelated commits — sequential rebase
	// chain.
	barePushUnrelated(t, bareGithub, "github-unrelated.txt", "github state")
	barePushUnrelated(t, bareVault, "vault-unrelated.txt", "vault state")

	// Capture pre-push HEAD so we can assert post-loop refresh moved
	// CommitSHA off it.
	originalHEADfull := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local change"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := CommitAndPush(dir, "both-rebase test")
	if err != nil {
		t.Fatal(err)
	}
	if !result.AllPushed() {
		t.Fatalf("expected AllPushed; got results=%v", result.RemoteResults)
	}

	// Both remotes must end at the same SHA.
	githubSHA := bareRefSHA(t, bareGithub)
	vaultSHA := bareRefSHA(t, bareVault)
	if githubSHA != vaultSHA {
		t.Errorf("remotes diverged: github=%s vault=%s", githubSHA, vaultSHA)
	}

	// CommitSHA must reflect post-loop HEAD, not the pre-push commit.
	postHEAD := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "--short", "HEAD"))
	if result.CommitSHA != postHEAD {
		t.Errorf("CommitSHA = %q, want post-loop HEAD %q",
			result.CommitSHA, postHEAD)
	}
	// Sanity: the post-loop HEAD must differ from the pre-push HEAD
	// (a rebase happened — distinct from the pre-rebase commit SHA).
	if strings.HasPrefix(originalHEADfull, result.CommitSHA) {
		t.Errorf("CommitSHA %q still matches pre-push HEAD %q — rebase did not refresh",
			result.CommitSHA, originalHEADfull)
	}
}

func TestCommitAndPush_ThreeRemotesSecondCascade(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bareA := gitx.InitBareRemote(t)
	bareB := gitx.InitBareRemote(t)
	bareC := gitx.InitBareRemote(t)
	// Names chosen so alphabetical iteration order is A, B, C.
	gitx.AddRemote(t, dir, "aaa-first", bareA)
	gitx.AddRemote(t, dir, "bbb-second", bareB)
	gitx.AddRemote(t, dir, "ccc-third", bareC)

	gitx.GitRun(t, dir, "push", "aaa-first", "main")
	gitx.GitRun(t, dir, "push", "bbb-second", "main")
	gitx.GitRun(t, dir, "push", "ccc-third", "main")

	// B and C carry distinct unrelated commits. A is empty (FF
	// accepts). The loop:
	//   1. push A (FF) → succeeds; remoteSHA[A] = X.
	//   2. push B → reject → rebase → push B → succeeds at X' (parent
	//      Y_B); converge A from X to X'.
	//   3. push C → reject (C's tip is Y_C, not X') → rebase →
	//      push C → succeeds at X'' (parent Y_C); converge A and B
	//      from X' to X'' (the SECOND CASCADE).
	barePushUnrelated(t, bareB, "b-unrelated.txt", "B state")
	barePushUnrelated(t, bareC, "c-unrelated.txt", "C state")

	if err := os.WriteFile(filepath.Join(dir, "local.txt"), []byte("local change"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := CommitAndPush(dir, "three-remote cascade")
	if err != nil {
		t.Fatal(err)
	}
	if !result.AllPushed() {
		t.Fatalf("expected AllPushed; got results=%v", result.RemoteResults)
	}
	for name, perr := range result.RemoteResults {
		if perr != nil {
			t.Errorf("remote %s: unexpected error: %v", name, perr)
		}
	}

	// All three bares must converge.
	shaA := bareRefSHA(t, bareA)
	shaB := bareRefSHA(t, bareB)
	shaC := bareRefSHA(t, bareC)
	if shaA != shaB || shaB != shaC {
		t.Errorf("three remotes diverged: A=%s B=%s C=%s", shaA, shaB, shaC)
	}

	// CommitSHA must reflect post-loop HEAD.
	postHEAD := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "--short", "HEAD"))
	if result.CommitSHA != postHEAD {
		t.Errorf("CommitSHA = %q, want post-loop HEAD %q",
			result.CommitSHA, postHEAD)
	}
}

func TestAfterPushHook_DefaultIsNoOp(t *testing.T) {
	if afterPushHook == nil {
		t.Fatal("afterPushHook is nil — must be a no-op default")
	}
	// Calling the default must not panic and must not have observable
	// side effects beyond returning. A no-op is observable only by
	// not panicking.
	afterPushHook("any-remote-name")
}
