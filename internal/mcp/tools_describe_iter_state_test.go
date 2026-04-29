// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// initGitRepo runs `git init` in dir so the directory is a valid git
// working tree. All tests using this helper should use t.Chdir / t.Setenv
// discipline.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	cmds := [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "t@t"},
		{"git", "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		cmd.Env = envs
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", c, string(out))
		}
	}
}

// commitAllInRepo stages and commits every file in dir as the initial
// commit, so subsequent `git status --porcelain` reports clean.
func commitAllInRepo(t *testing.T, dir, subject string) {
	t.Helper()
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	add := exec.Command("git", "add", "-A")
	add.Dir = dir
	add.Env = envs
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s", out)
	}
	commit := exec.Command("git", "commit", "-q", "--allow-empty", "-m", subject)
	commit.Dir = dir
	commit.Env = envs
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s", out)
	}
}

// gitCommit creates one commit in dir with the given subject + body.
// Returns the resulting commit SHA.
func gitCommit(t *testing.T, dir, subject, body string) string {
	t.Helper()
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	// Touch a file so the commit isn't empty.
	tag := strings.ReplaceAll(subject, " ", "_")
	if err := os.WriteFile(filepath.Join(dir, "f-"+tag+".txt"), []byte(subject), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = dir
	add.Env = envs
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s", out)
	}
	msg := subject
	if body != "" {
		msg = subject + "\n\n" + body
	}
	commit := exec.Command("git", "commit", "-q", "-m", msg)
	commit.Dir = dir
	commit.Env = envs
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s", out)
	}
	rev := exec.Command("git", "rev-parse", "HEAD")
	rev.Dir = dir
	rev.Env = envs
	out, err := rev.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse: %s", out)
	}
	return strings.TrimSpace(string(out))
}

func TestDescribeIterState_Basic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	// Initialise the vault as a clean git repo (commit the seeded
	// session-index.json so vault_has_uncommitted_writes is honestly false).
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	// Run inside a project directory the tool can read.
	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "initial commit", "")
	t.Chdir(projDir)

	tool := NewDescribeIterStateTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var res describeIterStateResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
	}

	if res.IterN < 1 {
		t.Errorf("iter_n = %d, want >= 1", res.IterN)
	}
	if res.Branch == "" {
		t.Errorf("branch should be non-empty in a git repo")
	}
	if res.VaultHasUncommittedWrites {
		t.Errorf("clean vault should have vault_has_uncommitted_writes=false")
	}
	if res.LastIterAnchorSha != "" {
		t.Errorf("fresh project should have empty last_iter_anchor_sha; got %q", res.LastIterAnchorSha)
	}
}

func TestDescribeIterState_DirtyVault(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)

	// Add an uncommitted file in the vault.
	dirty := filepath.Join(cfg.VaultPath, "dirty.txt")
	if err := os.WriteFile(dirty, []byte("pending"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "initial commit", "")
	t.Chdir(projDir)

	tool := NewDescribeIterStateTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var res describeIterStateResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !res.VaultHasUncommittedWrites {
		t.Errorf("dirty vault should report vault_has_uncommitted_writes=true")
	}
}

func TestDescribeIterState_PriorIterAnchorFound(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "chore: initial", "")
	priorSHA := gitCommit(t, projDir, "feat: ship something",
		"This shipped iter 41.\n\n## Iteration 41\n")
	gitCommit(t, projDir, "docs: update notes", "")

	t.Chdir(projDir)

	// Seed iterations.md so iter_n derives to 42.
	iterPath := filepath.Join(cfg.VaultPath, "Projects", "myproj", "agentctx", "iterations.md")
	if err := os.MkdirAll(filepath.Dir(iterPath), 0o755); err != nil {
		t.Fatalf("mkdir iterations.md parent: %v", err)
	}
	iterContent := "# Iterations\n\n## Iteration Narratives\n\n" +
		"### Iteration 40 — earlier work (2026-04-26)\n\nbody\n\n" +
		"### Iteration 41 — ship something (2026-04-27)\n\nbody\n"
	if err := os.WriteFile(iterPath, []byte(iterContent), 0o644); err != nil {
		t.Fatalf("write iterations.md: %v", err)
	}

	tool := NewDescribeIterStateTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var res describeIterStateResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.IterN != 42 {
		t.Errorf("iter_n = %d, want 42", res.IterN)
	}
	if res.LastIterAnchorSha != priorSHA {
		t.Errorf("last_iter_anchor_sha = %q, want %q", res.LastIterAnchorSha, priorSHA)
	}
}

func TestDescribeIterState_BranchDetection(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")

	// Switch to a feature branch.
	envs := []string{
		"HOME=" + projDir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	cb := exec.Command("git", "checkout", "-q", "-b", "feature/wibble")
	cb.Dir = projDir
	cb.Env = envs
	if out, err := cb.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %s", out)
	}
	t.Chdir(projDir)

	tool := NewDescribeIterStateTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var res describeIterStateResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.Branch != "feature/wibble" {
		t.Errorf("branch = %q, want feature/wibble", res.Branch)
	}
}

// TestDescribeIterState_InvalidProjectName ensures explicit invalid
// project names are rejected upstream by the resolveProject validator.
func TestDescribeIterState_InvalidProjectName(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewDescribeIterStateTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../etc"}`))
	if err == nil {
		t.Fatal("want project-validation error, got nil")
	}
}

// TestDescribeIterState_NoVaultGit asserts the tool tolerates a vault
// path that is not a git repo: it returns false for
// vault_has_uncommitted_writes rather than raising an error.
func TestDescribeIterState_NoVaultGit(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	// No git init on the vault.
	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	tool := NewDescribeIterStateTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var res describeIterStateResult
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.VaultHasUncommittedWrites {
		t.Errorf("non-git vault should report vault_has_uncommitted_writes=false")
	}
}

// TestLastIterAnchorSha_NoMatch asserts the helper returns ("", nil) when
// the targetIter has no matching commit footer.
func TestLastIterAnchorSha_NoMatch(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "feat: only commit", "no iter footer here")

	got, err := lastIterAnchorSha(dir, 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string for no match", got)
	}
}

// TestLastIterAnchorSha_TargetIterZero asserts iter <= 0 short-circuits.
func TestLastIterAnchorSha_TargetIterZero(t *testing.T) {
	got, err := lastIterAnchorSha(".", 0)
	if err != nil || got != "" {
		t.Errorf("got (%q, %v), want (\"\", nil)", got, err)
	}
}

// TestGitCmdRunner_Defaulted ensures the test seam exists and the default
// implementation works on a real git directory.
func TestGitCmdRunner_Defaulted(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "test", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := gitCmdRunner(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	got := strings.TrimSpace(out)
	if got != "main" {
		t.Errorf("branch = %q, want main", got)
	}
}

// TestDescribeIterStateTool_OutputJSONShape locks the on-the-wire JSON
// shape: required fields present + omitempty behaviour for null SHA.
func TestDescribeIterStateTool_OutputJSONShape(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "first", "")
	t.Chdir(projDir)

	tool := NewDescribeIterStateTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(result), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"iter_n", "branch", "vault_has_uncommitted_writes"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("output missing required field %q (got %v)", key, raw)
		}
	}
	// last_iter_anchor_sha is omitempty when no anchor; here we expect omit.
	if _, ok := raw["last_iter_anchor_sha"]; ok {
		t.Errorf("last_iter_anchor_sha should be omitted when empty; got %v", raw["last_iter_anchor_sha"])
	}
}

func TestNextIterFromIterationsMD(t *testing.T) {
	tests := []struct {
		name     string
		content  string // empty means do not write the file
		want     int
	}{
		{name: "missing file", content: "", want: 1},
		{name: "no headers", content: "# Iterations\n\nno entries yet\n", want: 1},
		{name: "single header", content: "### Iteration 1 — first (2026-01-01)\n", want: 2},
		{name: "many headers", content: "### Iteration 40 — a\n### Iteration 41 — b\n### Iteration 168 — z\n", want: 169},
		{name: "out of order", content: "### Iteration 168 — z\n### Iteration 40 — a\n", want: 169},
		{name: "h2 ignored", content: "## Iteration 999 — wrong level\n### Iteration 7 — right\n", want: 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vaultRoot := t.TempDir()
			projAgentctx := filepath.Join(vaultRoot, "Projects", "myproj", "agentctx")
			if err := os.MkdirAll(projAgentctx, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if tc.content != "" {
				if err := os.WriteFile(filepath.Join(projAgentctx, "iterations.md"), []byte(tc.content), 0o644); err != nil {
					t.Fatalf("write iterations.md: %v", err)
				}
			}
			got, err := nextIterFromIterationsMD(vaultRoot, "myproj")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNextIterFromIterationsMD_EmptyArgs(t *testing.T) {
	if got, err := nextIterFromIterationsMD("", "myproj"); err != nil || got != 1 {
		t.Errorf("empty vault path: got (%d, %v), want (1, nil)", got, err)
	}
	if got, err := nextIterFromIterationsMD(t.TempDir(), ""); err != nil || got != 1 {
		t.Errorf("empty project: got (%d, %v), want (1, nil)", got, err)
	}
}
