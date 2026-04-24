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
