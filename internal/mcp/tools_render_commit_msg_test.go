// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// withFakeGit replaces the execGitStatus and execGitDiffCachedStat seams for
// the duration of the test and restores them via t.Cleanup.
func withFakeGit(t *testing.T, statusOut, diffOut string, statusErr, diffErr error) {
	t.Helper()
	origStatus := execGitStatus
	origDiff := execGitDiffCachedStat
	execGitStatus = func(_ string) (string, error) { return statusOut, statusErr }
	execGitDiffCachedStat = func(_ string) (string, error) { return diffOut, diffErr }
	t.Cleanup(func() {
		execGitStatus = origStatus
		execGitDiffCachedStat = origDiff
	})
}

// newRenderTool returns a NewRenderCommitMsgTool backed by a minimal test vault.
func newRenderTool(t *testing.T) Tool {
	t.Helper()
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/.keep": "",
	})
	return NewRenderCommitMsgTool(cfg)
}

// defaultArgs returns a base JSON payload with all required fields set and
// project_path pointing at a temp dir (so we don't hit real git).
func defaultArgs(t *testing.T, projectPath string) map[string]any {
	t.Helper()
	return map[string]any{
		"project":              "myproject",
		"project_path":         projectPath,
		"iteration":            5,
		"subject":              "feat(mcp): add vv_render_commit_msg scaffold tool",
		"prose_body":           "Phase 4 lands the render tool.\n\nIt assembles the commit message mechanically.",
		"unit_tests":           1620,
		"integration_subtests": 31,
		"lint_findings":        0,
	}
}

// ---- golden-string assertion ---------------------------------------------------

func TestRenderCommitMsg_Golden(t *testing.T) {
	withFakeGit(t,
		// git status --porcelain: one modified staged file
		"M  internal/mcp/tools_render_commit_msg.go\n",
		// git diff --cached --stat: summary line
		" 1 file changed, 150 insertions(+)\n",
		nil, nil,
	)

	tool := newRenderTool(t)
	tmp := t.TempDir()
	args := defaultArgs(t, tmp)
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got struct {
		Rendered string `json:"rendered"`
		Bytes    int    `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, result)
	}
	if got.Bytes != len(got.Rendered) {
		t.Errorf("bytes=%d but len(rendered)=%d", got.Bytes, len(got.Rendered))
	}

	// Golden assertions on structure.
	lines := strings.Split(got.Rendered, "\n")
	if lines[0] != "feat(mcp): add vv_render_commit_msg scaffold tool" {
		t.Errorf("line 0 = %q, want subject", lines[0])
	}
	if lines[1] != "" {
		t.Errorf("line 1 = %q, want blank separator", lines[1])
	}
	if !strings.Contains(got.Rendered, "Phase 4 lands the render tool.") {
		t.Errorf("rendered missing prose body")
	}
	if !strings.Contains(got.Rendered, "## Files changed") {
		t.Errorf("rendered missing ## Files changed")
	}
	if !strings.Contains(got.Rendered, "tools_render_commit_msg.go") {
		t.Errorf("rendered missing staged file name")
	}
	if !strings.Contains(got.Rendered, "## Test counts") {
		t.Errorf("rendered missing ## Test counts")
	}
	if !strings.Contains(got.Rendered, "- Unit tests: 1620") {
		t.Errorf("rendered missing unit test count")
	}
	if !strings.Contains(got.Rendered, "- Integration subtests: 31") {
		t.Errorf("rendered missing integration subtest count")
	}
	if !strings.Contains(got.Rendered, "- Lint findings: 0") {
		t.Errorf("rendered missing lint findings")
	}
	if !strings.Contains(got.Rendered, "## Iteration 5") {
		t.Errorf("rendered missing ## Iteration 5")
	}
}

// ---- empty stage area ----------------------------------------------------------

func TestRenderCommitMsg_NoStagedChanges(t *testing.T) {
	withFakeGit(t,
		// git status: only untracked files (not staged)
		"?? new_file.go\n",
		// git diff --cached --stat: empty (nothing staged)
		"",
		nil, nil,
	)

	tool := newRenderTool(t)
	tmp := t.TempDir()
	args := defaultArgs(t, tmp)
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got struct {
		Rendered string `json:"rendered"`
	}
	json.Unmarshal([]byte(result), &got)

	if !strings.Contains(got.Rendered, "(no staged changes)") {
		t.Errorf("rendered = %q, want '(no staged changes)'", got.Rendered)
	}
}

// ---- subject with newline → error ----------------------------------------------

func TestRenderCommitMsg_SubjectWithNewline(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newRenderTool(t)
	tmp := t.TempDir()
	args := defaultArgs(t, tmp)
	args["subject"] = "first line\nsecond line"
	params, _ := json.Marshal(args)

	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for subject with newline")
	}
	if !strings.Contains(err.Error(), "single line") {
		t.Errorf("error = %q, want mention of single line", err)
	}
}

// ---- prose with markdown preserved verbatim ------------------------------------

func TestRenderCommitMsg_ProseMarkdownPreserved(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newRenderTool(t)
	tmp := t.TempDir()
	args := defaultArgs(t, tmp)
	args["prose_body"] = "## Internal heading\n\n- bullet one\n- bullet **bold** item\n\n> blockquote here"
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got struct {
		Rendered string `json:"rendered"`
	}
	json.Unmarshal([]byte(result), &got)

	if !strings.Contains(got.Rendered, "## Internal heading") {
		t.Errorf("markdown heading not preserved")
	}
	if !strings.Contains(got.Rendered, "bullet **bold** item") {
		t.Errorf("markdown bold not preserved")
	}
	if !strings.Contains(got.Rendered, "> blockquote here") {
		t.Errorf("markdown blockquote not preserved")
	}
}

// ---- iteration zero/negative → error -------------------------------------------

func TestRenderCommitMsg_IterationZero(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newRenderTool(t)
	tmp := t.TempDir()
	args := defaultArgs(t, tmp)
	args["iteration"] = 0
	params, _ := json.Marshal(args)

	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for iteration=0")
	}
	if !strings.Contains(err.Error(), "positive integer") {
		t.Errorf("error = %q, want mention of 'positive integer'", err)
	}
}

func TestRenderCommitMsg_IterationNegative(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newRenderTool(t)
	tmp := t.TempDir()
	args := defaultArgs(t, tmp)
	args["iteration"] = -3
	params, _ := json.Marshal(args)

	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for negative iteration")
	}
	if !strings.Contains(err.Error(), "positive integer") {
		t.Errorf("error = %q, want mention of 'positive integer'", err)
	}
}

// ---- project_path provided → used directly -------------------------------------

func TestRenderCommitMsg_ProjectPathProvided(t *testing.T) {
	// Set up a real git repo so we can exercise the non-fake path.
	// We still use the fake seam to avoid flakiness from the actual repo state.
	withFakeGit(t,
		"A  newfile.go\n",
		" 1 file changed, 10 insertions(+)\n",
		nil, nil,
	)

	tool := newRenderTool(t)
	explicit := t.TempDir()
	args := defaultArgs(t, explicit)
	args["project_path"] = explicit
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got struct {
		Rendered string `json:"rendered"`
	}
	json.Unmarshal([]byte(result), &got)
	if !strings.Contains(got.Rendered, "newfile.go") {
		t.Errorf("rendered should contain staged file from project_path")
	}
}

// ---- project_path derived via meta.ProjectRoot ----------------------------------

func TestRenderCommitMsg_ProjectPathDerived(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	// Create a real temp git repo so meta.ProjectRoot() can walk up to it.
	repoDir := t.TempDir()
	gitCmd := exec.Command("git", "init", "-b", "main")
	gitCmd.Dir = repoDir
	if out, err := gitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/.keep": "",
	})
	tool := NewRenderCommitMsgTool(cfg)

	// Change cwd into the repo so meta.ProjectRoot() finds it.
	origDir, cwdErr := os.Getwd()
	if cwdErr != nil {
		t.Fatalf("getwd: %v", cwdErr)
	}
	if chdirErr := os.Chdir(repoDir); chdirErr != nil {
		t.Fatalf("chdir: %v", chdirErr)
	}
	t.Cleanup(func() { os.Chdir(origDir) }) //nolint:errcheck

	// Omit project_path — should derive via meta.ProjectRoot() using cwd.
	args := map[string]any{
		"project":              "myproject",
		"iteration":            1,
		"subject":              "chore: test derived root",
		"prose_body":           "Testing derived project root.",
		"unit_tests":           10,
		"integration_subtests": 2,
		"lint_findings":        0,
	}
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got struct {
		Rendered string `json:"rendered"`
	}
	json.Unmarshal([]byte(result), &got)
	if !strings.Contains(got.Rendered, "## Iteration 1") {
		t.Errorf("rendered missing iteration footer: %s", got.Rendered)
	}
}

// ---- project_path non-existent directory → error when git fails ----------------

func TestRenderCommitMsg_ProjectPathNonExistent(t *testing.T) {
	// Use real git seams (not fake) so that running git in a non-existent
	// directory actually fails.
	origStatus := execGitStatus
	origDiff := execGitDiffCachedStat
	execGitStatus = func(dir string) (string, error) {
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return "", fmt.Errorf("directory does not exist: %s", dir)
		}
		return origStatus(dir)
	}
	execGitDiffCachedStat = func(dir string) (string, error) {
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return "", fmt.Errorf("directory does not exist: %s", dir)
		}
		return origDiff(dir)
	}
	t.Cleanup(func() {
		execGitStatus = origStatus
		execGitDiffCachedStat = origDiff
	})

	tool := newRenderTool(t)
	nonExistent := filepath.Join(t.TempDir(), "does", "not", "exist")
	args := defaultArgs(t, nonExistent)
	params, _ := json.Marshal(args)

	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for non-existent project_path")
	}
	if !strings.Contains(err.Error(), "files changed section") {
		t.Errorf("error = %q, want mention of 'files changed section'", err)
	}
}

// ---- missing required fields ---------------------------------------------------

func TestRenderCommitMsg_SubjectRequired(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)
	tool := newRenderTool(t)

	params, _ := json.Marshal(map[string]any{
		"project_path":         t.TempDir(),
		"iteration":            1,
		"prose_body":           "some prose",
		"unit_tests":           0,
		"integration_subtests": 0,
		"lint_findings":        0,
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for missing subject")
	}
	if !strings.Contains(err.Error(), "subject is required") {
		t.Errorf("error = %q, want 'subject is required'", err)
	}
}

func TestRenderCommitMsg_ProseBodyRequired(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)
	tool := newRenderTool(t)

	params, _ := json.Marshal(map[string]any{
		"project_path":         t.TempDir(),
		"iteration":            1,
		"subject":              "chore: test",
		"unit_tests":           0,
		"integration_subtests": 0,
		"lint_findings":        0,
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("expected error for missing prose_body")
	}
	if !strings.Contains(err.Error(), "prose_body is required") {
		t.Errorf("error = %q, want 'prose_body is required'", err)
	}
}

// ---- renderCommitMsg unit tests ------------------------------------------------

func TestRenderCommitMsg_OutputStructure(t *testing.T) {
	filesSection := "- foo.go\n- bar.go\n"
	rendered := renderCommitMsg(
		"fix(auth): correct token expiry",
		"The token expiry was off by one day.\n\nThis fixes it properly.",
		filesSection,
		100, 10, 0, 42,
	)

	wantParts := []string{
		"fix(auth): correct token expiry\n",
		"The token expiry was off by one day.",
		"## Files changed",
		"- foo.go",
		"- bar.go",
		"## Test counts",
		"- Unit tests: 100",
		"- Integration subtests: 10",
		"- Lint findings: 0",
		"## Iteration 42",
	}
	for _, want := range wantParts {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered missing %q\nfull output:\n%s", want, rendered)
		}
	}

	// Subject must be the very first line.
	if !strings.HasPrefix(rendered, "fix(auth): correct token expiry\n") {
		t.Errorf("subject is not the first line:\n%s", rendered)
	}
}
