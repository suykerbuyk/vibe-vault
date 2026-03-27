// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package vaultsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

// initTestRepo creates a git repo with an initial commit in a temp directory.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")

	return dir
}

func TestGetStatus_CleanRepo(t *testing.T) {
	dir := initTestRepo(t)

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
	if s.HasRemote {
		t.Error("expected no remote")
	}
}

func TestGetStatus_DirtyRepo(t *testing.T) {
	dir := initTestRepo(t)

	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("dirty"), 0o644)

	s, err := GetStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Clean {
		t.Error("expected dirty repo")
	}
}

func TestCommitAndPush_NoRemote(t *testing.T) {
	dir := initTestRepo(t)

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
	dir := initTestRepo(t)

	// Add a remote so EnsureRemote passes (doesn't need to be reachable for this test)
	cmd := exec.Command("git", "remote", "add", "origin", "https://example.com/test.git")
	cmd.Dir = dir
	cmd.Run()

	result, err := CommitAndPush(dir, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if result.Pushed {
		t.Error("expected no push when nothing to commit")
	}
	if result.CommitSHA != "" {
		t.Error("expected no commit SHA")
	}
}

func TestPull_NoRemote(t *testing.T) {
	dir := initTestRepo(t)

	_, err := Pull(dir)
	if err == nil {
		t.Fatal("expected error when no remote configured")
	}
	if got := err.Error(); !contains(got, "no git remote") {
		t.Errorf("error = %q, want mention of no remote", got)
	}
}

func TestEnsureRemote(t *testing.T) {
	dir := initTestRepo(t)

	if err := EnsureRemote(dir); err == nil {
		t.Error("expected error when no remote")
	}

	cmd := exec.Command("git", "remote", "add", "origin", "https://example.com/test.git")
	cmd.Dir = dir
	cmd.Run()

	if err := EnsureRemote(dir); err != nil {
		t.Errorf("unexpected error after adding remote: %v", err)
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
