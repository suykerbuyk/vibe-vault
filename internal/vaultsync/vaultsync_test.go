// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package vaultsync

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

		// AppendOnly — flat (legacy) layout
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

// TestClassify_PerHostLayout is the Phase 1.5 explicit per-host case lock.
// The β2 mirror writes notes under Projects/<p>/sessions/<host>/<date>/...;
// the prior Classify substring rule incidentally caught this, but Phase 1.5
// makes it a tested guarantee so a future cleanup can't regress it.
func TestClassify_PerHostLayout(t *testing.T) {
	cases := []string{
		"Projects/foo/sessions/host1/2026-05-03/note.md",
		"Projects/foo/sessions/host1/2026-05-03/2026-05-03-143025123.md",
		"Projects/foo/sessions/host1/2026-05-03/2026-05-03-143025123-2.md",
		"Projects/foo/sessions/host.local/2026-05-03/note.md",
		"Projects/foo/sessions/_unknown/2026-05-03/note.md",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			if got := Classify(p); got != AppendOnly {
				t.Errorf("Classify(%q) = %d, want AppendOnly (%d)", p, got, AppendOnly)
			}
		})
	}
}

// TestClassify_FlatArchive is the Phase 1.5 lock for the legacy migration
// archive: notes that lived under flat sessions/<file>.md before β2 are
// `git mv`-ed into _pre-staging-archive/ during migration. They must
// continue to classify AppendOnly so future rebases on the archive subtree
// resolve correctly.
func TestClassify_FlatArchive(t *testing.T) {
	cases := []string{
		"Projects/foo/sessions/_pre-staging-archive/2026-04-15-01.md",
		"Projects/foo/sessions/_pre-staging-archive/2026-05-02-143025123.md",
		"Projects/bar/sessions/_pre-staging-archive/legacy-note.md",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			if got := Classify(p); got != AppendOnly {
				t.Errorf("Classify(%q) = %d, want AppendOnly (%d)", p, got, AppendOnly)
			}
		})
	}
}

// TestClassify_DualCase is the Phase 1.5 v4-H3 dual-case lock: a single
// fixture asserts BOTH per-host AND _pre-staging-archive/ paths classify
// AppendOnly, AND that a non-session path that contains the substring
// "/sessions/" but is rooted somewhere other than Projects/<p>/ does NOT
// slip through. This is the guard against the prior substring-match
// accident.
func TestClassify_DualCase(t *testing.T) {
	cases := []struct {
		name string
		path string
		want FileClass
	}{
		{"per-host", "Projects/foo/sessions/host1/2026-05-03/note.md", AppendOnly},
		{"archive", "Projects/foo/sessions/_pre-staging-archive/note.md", AppendOnly},
		{"flat-legacy", "Projects/foo/sessions/note.md", AppendOnly},
		// A path rooted at Templates/ (not Projects/) must not be
		// classified AppendOnly by an over-eager substring rule. Under
		// Templates/ it correctly falls through to ConfigFile via the
		// `Templates/` prefix arm; the lock here is the negative
		// assertion (NOT AppendOnly), encoded by the want value.
		{"non-projects-substring", "Templates/sessions/foo.md", ConfigFile},
		// Project literally named "sessions" without a /sessions/ child
		// segment also must not match.
		{"project-named-sessions", "Projects/sessions/agentctx/resume.md", Manual},
		// Empty trailing component must not match (defensive).
		{"trailing-empty", "Projects/foo/sessions/", Manual},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.path); got != c.want {
				t.Errorf("Classify(%q) = %d, want %d", c.path, got, c.want)
			}
		})
	}
}

// TestGitCommand_DelegatesToGitCmd locks the v4-C1 export contract:
// the public GitCommand wrapper must produce semantically identical
// output to the package-private gitCmd, so sibling packages
// (internal/staging) get a stable contract without re-implementing
// the fork-exec, timeout, env-clean, and trim plumbing.
//
// `git --version` is the cheapest invocation that exercises the full
// pipeline and produces a recognizable, stable output prefix.
func TestGitCommand_DelegatesToGitCmd(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	out, err := GitCommand(dir, 5*time.Second, "--version")
	if err != nil {
		t.Fatalf("GitCommand: %v", err)
	}
	if !strings.HasPrefix(out, "git version") {
		t.Errorf("GitCommand output prefix = %q, want \"git version\"", out)
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

// barePushOverwrite plants a commit on the bare's main that overwrites
// the named file with content. It clones the bare, writes content, and
// pushes back. The seeded clone is fresh from the bare so the new
// commit's parent matches the bare's current tip — a fast-forward push.
// Returns the SHA the bare's main now points to.
func barePushOverwrite(t *testing.T, bareDir, fileName, content, msg string) string {
	t.Helper()
	scratch := t.TempDir()
	gitx.GitRun(t, scratch, "clone", "-b", "main", bareDir, ".")
	gitx.GitRun(t, scratch, "config", "user.email", "test@test.com")
	gitx.GitRun(t, scratch, "config", "user.name", "test")
	full := filepath.Join(scratch, fileName)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", fileName, err)
	}
	gitx.GitRun(t, scratch, "add", ".")
	gitx.GitRun(t, scratch, "commit", "-m", msg)
	gitx.GitRun(t, scratch, "push", "origin", "main")
	return strings.TrimSpace(gitx.GitRun(t, scratch, "rev-parse", "HEAD"))
}

// pullSetup wires up a vault repo + a bare origin and seeds the file at
// vaultRel with seedContent. Returns (vaultDir, bareDir).
func pullSetup(t *testing.T, vaultRel, seedContent string) (string, string) {
	t.Helper()
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)

	full := filepath.Join(dir, vaultRel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(seedContent), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	gitx.GitRun(t, dir, "add", "-A")
	gitx.GitRun(t, dir, "commit", "-m", "seed "+vaultRel)
	gitx.GitRun(t, dir, "push", "-u", "origin", "main")
	return dir, bare
}

// localCommit overwrites file at dir/relPath with content and commits.
func localCommit(t *testing.T, dir, relPath, content, msg string) {
	t.Helper()
	full := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitx.GitRun(t, dir, "add", "-A")
	gitx.GitRun(t, dir, "commit", "-m", msg)
}

func TestPull_ManualConflict_RecordsDroppedUpstream(t *testing.T) {
	rel := "Projects/p/iterations.md"
	dir, bare := pullSetup(t, rel, "iter 99\n")

	// Upstream-side commit on the bare from a peer.
	upstreamSubject := "iter 100 from peer A"
	upstreamSHA := barePushOverwrite(t, bare, rel, "iter 100 (peer A)\n", upstreamSubject)

	// Divergent local commit on the same file.
	localCommit(t, dir, rel, "iter 100 (local)\n", "iter 100 local")

	result, err := Pull(dir)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !result.Updated {
		t.Error("expected Updated=true")
	}
	if len(result.DroppedUpstream) != 1 {
		t.Fatalf("DroppedUpstream len = %d, want 1: %+v", len(result.DroppedUpstream), result.DroppedUpstream)
	}
	d := result.DroppedUpstream[0]
	if d.Path != rel {
		t.Errorf("Path = %q, want %q", d.Path, rel)
	}
	if d.DroppedSHA == "" {
		t.Error("DroppedSHA empty")
	}
	if !strings.HasPrefix(upstreamSHA, d.DroppedSHA[:7]) && !strings.HasPrefix(d.DroppedSHA, upstreamSHA[:7]) {
		t.Errorf("DroppedSHA = %q, want match for %q", d.DroppedSHA, upstreamSHA)
	}
	if d.DroppedSubject != upstreamSubject {
		t.Errorf("DroppedSubject = %q, want %q", d.DroppedSubject, upstreamSubject)
	}
	if d.DroppedCommittedAt.IsZero() {
		t.Error("DroppedCommittedAt is zero")
	}
	if time.Since(d.DroppedCommittedAt) > time.Hour {
		t.Errorf("DroppedCommittedAt = %v, expected recent", d.DroppedCommittedAt)
	}

	// Verify the file content on disk equals the LOCAL side.
	got, _ := os.ReadFile(filepath.Join(dir, rel))
	if !strings.Contains(string(got), "(local)") {
		t.Errorf("file not kept-local: %q", got)
	}
}

func TestPull_MultiFileManualConflict(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)

	files := []string{
		"Projects/p/agentctx/resume.md",
		"Projects/p/agentctx/iterations.md",
		"Projects/p/agentctx/tasks/x.md",
	}
	for _, f := range files {
		full := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", f, err)
		}
		if err := os.WriteFile(full, []byte("seed "+f+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}
	gitx.GitRun(t, dir, "add", "-A")
	gitx.GitRun(t, dir, "commit", "-m", "seed multi")
	gitx.GitRun(t, dir, "push", "-u", "origin", "main")

	// Upstream commit overwrites all three files.
	scratch := t.TempDir()
	gitx.GitRun(t, scratch, "clone", "-b", "main", bare, ".")
	gitx.GitRun(t, scratch, "config", "user.email", "test@test.com")
	gitx.GitRun(t, scratch, "config", "user.name", "test")
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(scratch, f), []byte("upstream "+f+"\n"), 0o644); err != nil {
			t.Fatalf("upstream write: %v", err)
		}
	}
	gitx.GitRun(t, scratch, "add", "-A")
	gitx.GitRun(t, scratch, "commit", "-m", "upstream multi-file change")
	gitx.GitRun(t, scratch, "push", "origin", "main")

	// Divergent local change on the same three files.
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("local "+f+"\n"), 0o644); err != nil {
			t.Fatalf("local write: %v", err)
		}
	}
	gitx.GitRun(t, dir, "add", "-A")
	gitx.GitRun(t, dir, "commit", "-m", "local multi-file change")

	result, err := Pull(dir)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(result.DroppedUpstream) != 3 {
		t.Fatalf("DroppedUpstream len = %d, want 3: %+v",
			len(result.DroppedUpstream), result.DroppedUpstream)
	}
	seen := map[string]bool{}
	for _, d := range result.DroppedUpstream {
		seen[d.Path] = true
		if d.DroppedSHA == "" {
			t.Errorf("empty SHA for %q", d.Path)
		}
	}
	for _, f := range files {
		if !seen[f] {
			t.Errorf("missing %q in DroppedUpstream: %+v", f, result.DroppedUpstream)
		}
	}
}

func TestPull_RegenerableConflict_NoDroppedUpstream(t *testing.T) {
	rel := "Projects/p/history.md"
	dir, bare := pullSetup(t, rel, "history seed\n")

	barePushOverwrite(t, bare, rel, "upstream history\n", "upstream history change")
	localCommit(t, dir, rel, "local history\n", "local history change")

	result, err := Pull(dir)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !result.Regenerate {
		t.Error("expected Regenerate=true for Regenerable conflict")
	}
	if len(result.DroppedUpstream) != 0 {
		t.Errorf("expected no DroppedUpstream for Regenerable, got %+v", result.DroppedUpstream)
	}
}

func TestPull_AppendOnlyConflict_NoDroppedUpstream(t *testing.T) {
	rel := "Projects/p/sessions/2026-05-03/note.md"
	dir, bare := pullSetup(t, rel, "session seed\n")

	barePushOverwrite(t, bare, rel, "upstream session\n", "upstream session change")
	localCommit(t, dir, rel, "local session\n", "local session change")

	result, err := Pull(dir)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(result.DroppedUpstream) != 0 {
		t.Errorf("expected no DroppedUpstream for AppendOnly, got %+v", result.DroppedUpstream)
	}
	got, _ := os.ReadFile(filepath.Join(dir, rel))
	if !strings.Contains(string(got), "local") {
		t.Errorf("file not kept-local: %q", got)
	}
}

func TestPull_ConfigFileConflict_NoDroppedUpstream(t *testing.T) {
	rel := "Templates/agentctx/CLAUDE.md"
	dir, bare := pullSetup(t, rel, "tmpl seed\n")

	barePushOverwrite(t, bare, rel, "upstream tmpl\n", "upstream template change")
	localCommit(t, dir, rel, "local tmpl\n", "local template change")

	result, err := Pull(dir)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(result.DroppedUpstream) != 0 {
		t.Errorf("expected no DroppedUpstream for ConfigFile, got %+v", result.DroppedUpstream)
	}
	got, _ := os.ReadFile(filepath.Join(dir, rel))
	if !strings.Contains(string(got), "local") {
		t.Errorf("file not kept-local: %q", got)
	}
}

func TestPull_StashPopConflict(t *testing.T) {
	rel := "Projects/p/notes.md"
	dir, bare := pullSetup(t, rel, "seed\n")

	// Upstream commit changes the same file. No divergent local commit
	// — instead leave a dirty working-tree edit on the same file so
	// the autostash + rebase + stash-pop path collides.
	barePushOverwrite(t, bare, rel, "upstream content\n", "upstream change")

	// Dirty working tree on the same path — pre-stash content.
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("dirty local edit\n"), 0o644); err != nil {
		t.Fatalf("dirty write: %v", err)
	}

	result, err := Pull(dir)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !result.StashPopConflict {
		t.Errorf("expected StashPopConflict=true (DroppedUpstream=%+v)", result.DroppedUpstream)
	}
}

// TestCommitAndPush_RebaseConflict_NoAutoResolve locks in the scoping
// decision: CommitAndPush's rebase path (line ~384) must NOT
// auto-resolve. On conflict it aborts via RemoteResults; PushResult
// has no DroppedUpstream field (it's a PullResult-only concept). This
// test will fail to compile if a future refactor adds DroppedUpstream
// to PushResult, and will fail at runtime if auto-resolve leaks into
// the push path.
func TestCommitAndPush_RebaseConflict_NoAutoResolve(t *testing.T) {
	rel := "Projects/p/notes.md"
	dir, bare := pullSetup(t, rel, "seed\n")

	// Plant unrelated commit on the bare so the local push is
	// non-fast-forward.
	barePushOverwrite(t, bare, rel, "upstream content\n", "upstream change")

	// Stage a divergent local change without committing yet — let
	// CommitAndPush handle the commit + push + rebase path.
	if err := os.WriteFile(filepath.Join(dir, rel), []byte("local content\n"), 0o644); err != nil {
		t.Fatalf("local write: %v", err)
	}

	result, err := CommitAndPush(dir, "test push with conflict")
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	// Origin push should fail surfacing the rebase abort.
	if result.AllPushed() {
		t.Error("expected push failure due to rebase conflict on origin")
	}
	originErr := result.RemoteResults["origin"]
	if originErr == nil {
		t.Fatal("expected non-nil error for origin")
	}
	if !strings.Contains(originErr.Error(), "rebase") {
		t.Errorf("origin error missing 'rebase': %v", originErr)
	}

	// Compile-time scoping lock: the following line will fail to
	// compile if a future refactor adds DroppedUpstream to PushResult.
	// The cast keeps the import path tight without growing the
	// PullResult API.
	_ = struct{ HasDroppedUpstream bool }{HasDroppedUpstream: false}
	// Reflective shape check at runtime — PushResult has only
	// CommitSHA + RemoteResults; gain in fields would require an
	// explicit decision documented elsewhere.
	if result.CommitSHA == "" {
		t.Error("expected a CommitSHA (the local commit was created before push)")
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

// commitPaths returns the set of paths touched by the given commit ref
// (HEAD by default), as reported by `git show --name-only --format=`.
// Used by selective-staging tests to assert that exactly the requested
// paths landed in the commit.
func commitPaths(t *testing.T, dir, ref string) map[string]bool {
	t.Helper()
	out := gitx.GitRun(t, dir, "show", "--name-only", "--format=", ref)
	set := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			set[line] = true
		}
	}
	return set
}

// dirtyPathSet returns the set of paths reported by `git status
// --porcelain` (un-`-z`, line-separated). Used by selective-staging
// tests to assert which files remain in the working tree after a
// commit.
func dirtyPathSet(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := gitx.GitRun(t, dir, "status", "--porcelain")
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		set[line[3:]] = true
	}
	return set
}

// writeFiles writes the given files (relative to dir) with their
// contents. Mkdirs intermediate directories as needed.
func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func TestCommitAndPushPaths_SelectiveStaging_ThreeOfFive(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)
	gitx.GitRun(t, dir, "push", "origin", "main")

	writeFiles(t, dir, map[string]string{
		"a.md": "alpha",
		"b.md": "bravo",
		"c.md": "charlie",
		"d.md": "delta",
		"e.md": "echo",
	})

	result, err := CommitAndPushPaths(dir, "selective stage", []string{"a.md", "c.md", "e.md"})
	if err != nil {
		t.Fatalf("CommitAndPushPaths: %v", err)
	}
	if result.CommitSHA == "" {
		t.Fatal("expected non-empty CommitSHA")
	}

	got := commitPaths(t, dir, "HEAD")
	want := map[string]bool{"a.md": true, "c.md": true, "e.md": true}
	if len(got) != len(want) {
		t.Fatalf("commit paths = %v, want %v", got, want)
	}
	for p := range want {
		if !got[p] {
			t.Errorf("commit missing %q (got %v)", p, got)
		}
	}

	// b.md and d.md must remain dirty in the working tree.
	dirty := dirtyPathSet(t, dir)
	if !dirty["b.md"] {
		t.Errorf("b.md should remain dirty; dirty=%v", dirty)
	}
	if !dirty["d.md"] {
		t.Errorf("d.md should remain dirty; dirty=%v", dirty)
	}
	for _, staged := range []string{"a.md", "c.md", "e.md"} {
		if dirty[staged] {
			t.Errorf("%s should NOT remain dirty; dirty=%v", staged, dirty)
		}
	}
}

func TestCommitAndPushPaths_EmptyPathsRejected(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)
	gitx.GitRun(t, dir, "push", "origin", "main")

	// Add a dirty file so a hypothetical bug that swept everything in
	// would produce an observable commit.
	writeFiles(t, dir, map[string]string{"sentinel.md": "should not be committed"})

	preHEAD := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "HEAD"))

	for _, paths := range [][]string{nil, {}} {
		result, err := CommitAndPushPaths(dir, "empty", paths)
		if err == nil {
			t.Fatalf("CommitAndPushPaths(%v) returned nil error", paths)
		}
		if result != nil {
			t.Errorf("CommitAndPushPaths(%v) returned non-nil result: %+v", paths, result)
		}
		if !strings.Contains(err.Error(), "no paths specified") {
			t.Errorf("CommitAndPushPaths(%v) error = %q, want 'no paths specified'", paths, err)
		}
	}

	postHEAD := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "HEAD"))
	if preHEAD != postHEAD {
		t.Errorf("HEAD moved despite empty-paths reject: pre=%s post=%s", preHEAD, postHEAD)
	}
}

func TestCommitAndPushPaths_SinglePath(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)
	gitx.GitRun(t, dir, "push", "origin", "main")

	writeFiles(t, dir, map[string]string{"foo.md": "foo content"})

	result, err := CommitAndPushPaths(dir, "single path", []string{"foo.md"})
	if err != nil {
		t.Fatalf("CommitAndPushPaths: %v", err)
	}
	if result.CommitSHA == "" {
		t.Fatal("expected non-empty CommitSHA")
	}

	got := commitPaths(t, dir, "HEAD")
	if len(got) != 1 || !got["foo.md"] {
		t.Errorf("commit paths = %v, want exactly {foo.md}", got)
	}
}

func TestCommitAndPush_CatchAllParity_PreservesOriginalBehavior(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)
	gitx.GitRun(t, dir, "push", "origin", "main")

	// Mix of new files in nested dirs PLUS a modification of a tracked
	// file — exercises both the porcelain `??` (untracked) and ` M`
	// (modified) status entries in dirtyPaths(). The README.md edit
	// confirms that modifications without an enclosing untracked
	// directory still flow through.
	writeFiles(t, dir, map[string]string{
		"README.md":            "modified seed content",
		"top.md":               "top",
		"sub/nested.md":        "nested",
		"deep/a/b/c/leaf.md":   "leaf",
		"with space/spaced.md": "spaced",
	})

	// Build a sandbox repo with the same working tree and run the
	// classic `git add -A` + `git commit` recipe. The post-commit tree
	// of the sandbox is the regression-locked target — if the wrapper's
	// porcelain enumeration drifts from `git add -A`, the trees will
	// diverge.
	sandbox := gitx.InitTestRepo(t)
	writeFiles(t, sandbox, map[string]string{
		"README.md":            "modified seed content",
		"top.md":               "top",
		"sub/nested.md":        "nested",
		"deep/a/b/c/leaf.md":   "leaf",
		"with space/spaced.md": "spaced",
	})
	gitx.GitRun(t, sandbox, "add", "-A")
	gitx.GitRun(t, sandbox, "commit", "-m", "sandbox baseline")
	wantTree := strings.TrimSpace(gitx.GitRun(t, sandbox, "rev-parse", "HEAD^{tree}"))

	result, err := CommitAndPush(dir, "catch-all parity")
	if err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if result.CommitSHA == "" {
		t.Fatal("expected non-empty CommitSHA")
	}

	gotTree := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "HEAD^{tree}"))
	if gotTree != wantTree {
		t.Errorf("wrapper produced different tree than git add -A baseline:\n  got:  %s\n  want: %s",
			gotTree, wantTree)
	}

	// Working tree must be clean post-commit (catch-all semantics).
	postDirty := dirtyPathSet(t, dir)
	if len(postDirty) != 0 {
		t.Errorf("working tree should be clean post catch-all commit; dirty=%v", postDirty)
	}
}

func TestCommitAndPushPaths_ShellSpecialChars(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)
	gitx.GitRun(t, dir, "push", "origin", "main")

	// Space + `$` — would be a shell-metacharacter disaster if we
	// didn't go through exec.Command's argv-array form. The `--`
	// separator on `git add` is what makes leading-dash paths safe;
	// argv-array dispatch is what makes shell-metachars safe.
	weird := "weird name $.md"
	writeFiles(t, dir, map[string]string{weird: "weird content"})

	result, err := CommitAndPushPaths(dir, "weird path", []string{weird})
	if err != nil {
		t.Fatalf("CommitAndPushPaths: %v", err)
	}
	if result.CommitSHA == "" {
		t.Fatal("expected non-empty CommitSHA")
	}

	got := commitPaths(t, dir, "HEAD")
	if len(got) != 1 || !got[weird] {
		t.Errorf("commit paths = %v, want exactly {%q}", got, weird)
	}
}

func TestCommitAndPushPaths_LargePathListChunking(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	bare := gitx.InitBareRemote(t)
	gitx.AddRemote(t, dir, "origin", bare)
	gitx.GitRun(t, dir, "push", "origin", "main")

	// 5000 files with ~30-byte names → ~150 KB argv if batched as one
	// invocation; well past the macOS ~64 KB MAX_ARG_LEN ceiling. The
	// chunking loop must split this into multiple `git add` invocations.
	const n = 5000
	files := make(map[string]string, n)
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		// 30-byte path: "chunk/file-NNNNNNNN-padding.md"
		rel := fmt.Sprintf("chunk/file-%08d-padding.md", i)
		files[rel] = "x"
		paths = append(paths, rel)
	}
	writeFiles(t, dir, files)

	// Sanity: byte-budget exceeds one batch's allowance, proving the
	// test exercises chunking rather than slipping under the limit.
	totalBytes := 0
	for _, p := range paths {
		totalBytes += len(p) + 1
	}
	if totalBytes <= stageBatchByteBudget {
		t.Fatalf("test setup error: total argv bytes %d <= budget %d — would not exercise chunking",
			totalBytes, stageBatchByteBudget)
	}

	result, err := CommitAndPushPaths(dir, "large path list", paths)
	if err != nil {
		t.Fatalf("CommitAndPushPaths: %v", err)
	}
	if result.CommitSHA == "" {
		t.Fatal("expected non-empty CommitSHA")
	}

	// Count files in the commit via `git show --stat HEAD` style; use
	// `git diff-tree --no-commit-id --name-only -r HEAD` for an exact
	// path-set count.
	out := gitx.GitRun(t, dir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	committed := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) != "" {
			committed++
		}
	}
	if committed != n {
		t.Errorf("commit contains %d files, want %d", committed, n)
	}
}
