// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeStaleFixture builds a temporary "repo" with the given top-level
// Go-convention directories, and a temporary "vault" with an agentctx
// dir for the given project. Returns (repoPath, vaultPath).
func makeStaleFixture(t *testing.T, project string, repoDirs []string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	for _, d := range repoDirs {
		if err := os.MkdirAll(filepath.Join(repo, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	vault := t.TempDir()
	agentctx := filepath.Join(vault, "Projects", project, "agentctx")
	if err := os.MkdirAll(filepath.Join(agentctx, "tasks"), 0o755); err != nil {
		t.Fatalf("mkdir agentctx: %v", err)
	}
	return repo, vault
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckStalePackageRefs_NilOnEmptyArgs(t *testing.T) {
	if r := CheckStalePackageRefs("", "p", "/tmp"); r != nil {
		t.Errorf("empty vaultPath: expected nil, got %+v", r)
	}
	if r := CheckStalePackageRefs("/tmp", "", "/tmp"); r != nil {
		t.Errorf("empty project: expected nil, got %+v", r)
	}
	if r := CheckStalePackageRefs("/tmp", "_unknown", "/tmp"); r != nil {
		t.Errorf("_unknown project: expected nil, got %+v", r)
	}
	if r := CheckStalePackageRefs("/tmp", "p", ""); r != nil {
		t.Errorf("empty repoPath: expected nil, got %+v", r)
	}
}

func TestCheckStalePackageRefs_NilOnNonGoRepo(t *testing.T) {
	repo, vault := makeStaleFixture(t, "p", nil) // no internal/ or cmd/
	if r := CheckStalePackageRefs(vault, "p", repo); r != nil {
		t.Errorf("non-Go repo: expected nil, got %+v", r)
	}
}

func TestCheckStalePackageRefs_NilOnMissingAgentctx(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "internal"), 0o755); err != nil {
		t.Fatalf("mkdir internal: %v", err)
	}
	vault := t.TempDir() // no Projects/p/agentctx
	if r := CheckStalePackageRefs(vault, "p", repo); r != nil {
		t.Errorf("missing agentctx: expected nil, got %+v", r)
	}
}

func TestCheckStalePackageRefs_PassOnCleanResume(t *testing.T) {
	repo, vault := makeStaleFixture(t, "p", []string{"internal/check", "internal/mcp", "cmd/vv"})
	resume := `# Project
Uses internal/check and internal/mcp. CLI lives in cmd/vv.
`
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "resume.md"), resume)
	r := CheckStalePackageRefs(vault, "p", repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Pass {
		t.Errorf("status: want Pass, got %s; detail: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "3 package refs") {
		t.Errorf("detail should mention 3 refs validated, got: %s", r.Detail)
	}
}

func TestCheckStalePackageRefs_WarnOnStaleResume(t *testing.T) {
	repo, vault := makeStaleFixture(t, "p", []string{"internal/check"})
	resume := `# Project
Uses internal/check today. Iter 162 added internal/wrapdispatch which is now gone.
`
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "resume.md"), resume)
	r := CheckStalePackageRefs(vault, "p", repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Warn {
		t.Errorf("status: want Warn, got %s; detail: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "internal/wrapdispatch") {
		t.Errorf("detail should mention stale internal/wrapdispatch, got: %s", r.Detail)
	}
}

func TestCheckStalePackageRefs_HistoryTailIgnored(t *testing.T) {
	repo, vault := makeStaleFixture(t, "p", []string{"internal/check"})
	// Stale refs only in the project-history-tail block — should be tolerated.
	resume := `# Project
Active text references internal/check only.

<!-- vv:project-history-tail:start -->
| 162 | 2026-04-27 | wrap-model-tiering shipped internal/wrapdispatch + internal/wrapbundlecache |
| 167 | 2026-04-28 | DESIGN #91 added internal/wrapbundlecache rotation |
<!-- vv:project-history-tail:end -->

More active text after.
`
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "resume.md"), resume)
	r := CheckStalePackageRefs(vault, "p", repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Pass {
		t.Errorf("status: want Pass (history-only drift is tolerated), got %s; detail: %s", r.Status, r.Detail)
	}
}

func TestCheckStalePackageRefs_WarnOnMixedActiveAndHistory(t *testing.T) {
	repo, vault := makeStaleFixture(t, "p", []string{"internal/check"})
	resume := `# Project
Active mentions internal/staletop which doesn't exist.

<!-- vv:project-history-tail:start -->
| 162 | 2026-04-27 | wrap-model-tiering shipped internal/wrapdispatch |
<!-- vv:project-history-tail:end -->
`
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "resume.md"), resume)
	r := CheckStalePackageRefs(vault, "p", repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Warn {
		t.Errorf("status: want Warn, got %s; detail: %s", r.Status, r.Detail)
	}
	// Should mention the active-section drift, NOT the history-block drift.
	if !strings.Contains(r.Detail, "internal/staletop") {
		t.Errorf("detail should mention active-section stale ref, got: %s", r.Detail)
	}
	if strings.Contains(r.Detail, "internal/wrapdispatch") {
		t.Errorf("detail should NOT mention history-block ref, got: %s", r.Detail)
	}
}

func TestCheckStalePackageRefs_TaskFilesIgnored(t *testing.T) {
	// Task files are forward-looking plans (mentioning future packages) and
	// motivation prose (citing retired packages as context). They're not
	// loaded on /restart and intentionally out of scope for this lint.
	repo, vault := makeStaleFixture(t, "p", []string{"internal/check"})
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "resume.md"),
		"# Clean resume, references internal/check.\n")
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "tasks", "future.md"),
		"Will create internal/notyet here as a deliverable.\n")
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "tasks", "retired.md"),
		"Cites internal/wrapdispatch as motivation example.\n")
	r := CheckStalePackageRefs(vault, "p", repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Pass {
		t.Errorf("status: want Pass (task files out of scope), got %s; detail: %s", r.Status, r.Detail)
	}
}

func TestCheckStalePackageRefs_SummarizeCaps(t *testing.T) {
	repo, vault := makeStaleFixture(t, "p", []string{"internal/check"})
	resume := `# Project
internal/aaa internal/bbb internal/ccc internal/ddd internal/eee
`
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "resume.md"), resume)
	r := CheckStalePackageRefs(vault, "p", repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if r.Status != Warn {
		t.Errorf("status: want Warn, got %s", r.Status)
	}
	if !strings.Contains(r.Detail, "5 stale") {
		t.Errorf("detail should report count of 5, got: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, "+2 more") {
		t.Errorf("detail should cap at 3 with +2 more, got: %s", r.Detail)
	}
}

func TestCheckStalePackageRefs_DedupesWithinFile(t *testing.T) {
	repo, vault := makeStaleFixture(t, "p", []string{"internal/check"})
	resume := `internal/missing once. internal/missing again. internal/missing thrice.`
	writeFile(t, filepath.Join(vault, "Projects", "p", "agentctx", "resume.md"), resume)
	r := CheckStalePackageRefs(vault, "p", repo)
	if r == nil {
		t.Fatal("expected Result, got nil")
	}
	if !strings.Contains(r.Detail, "1 stale") {
		t.Errorf("detail should dedupe to 1 stale ref, got: %s", r.Detail)
	}
}

func TestReadResumeActive_NoHistoryBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.md")
	content := "# Resume\n\nNo history markers here.\n"
	writeFile(t, path, content)
	got, ok := readResumeActive(path)
	if !ok {
		t.Fatal("ok should be true")
	}
	if got != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

func TestReadResumeActive_StripsHistoryBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.md")
	content := "before\n" + historyTailStartMarker + "\nrow1\nrow2\n" + historyTailEndMarker + "\nafter\n"
	writeFile(t, path, content)
	got, ok := readResumeActive(path)
	if !ok {
		t.Fatal("ok should be true")
	}
	if strings.Contains(got, "row1") || strings.Contains(got, "row2") {
		t.Errorf("history rows should be stripped, got: %q", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("surrounding content should be preserved, got: %q", got)
	}
}

func TestReadResumeActive_MissingEndMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume.md")
	content := "before\n" + historyTailStartMarker + "\nrow1\n(no end marker)\n"
	writeFile(t, path, content)
	got, ok := readResumeActive(path)
	if !ok {
		t.Fatal("ok should be true")
	}
	// Unmatched start marker: return the raw content (safer than truncating).
	if got != content {
		t.Errorf("unmatched start marker should return raw content")
	}
}

func TestReadResumeActive_MissingFile(t *testing.T) {
	got, ok := readResumeActive(filepath.Join(t.TempDir(), "nope.md"))
	if ok {
		t.Errorf("missing file should return false, got %q", got)
	}
}

func TestScanForStale_RegexBoundaries(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "internal", "check"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cases := []struct {
		name  string
		text  string
		stale []string
	}{
		{"bare", "see internal/check\n", nil},
		{"trailing slash", "see internal/check/foo.go\n", nil},
		{"backticked", "see `internal/check`\n", nil},
		{"missing", "see internal/missing\n", []string{"src:internal/missing"}},
		{"cmd path", "see cmd/missing\n", []string{"src:cmd/missing"}},
		{"no match for capital", "see Internal/Check\n", nil},
		{"module path skipped",
			"go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.0\n",
			nil},
		{"module path no false positive",
			"vendored at github.com/foo/bar/internal/baz when imported\n",
			nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stale, _ := scanForStale("src", tc.text, repo)
			if len(stale) != len(tc.stale) {
				t.Fatalf("want %d stale, got %d: %v", len(tc.stale), len(stale), stale)
			}
			for i, want := range tc.stale {
				if stale[i] != want {
					t.Errorf("stale[%d]: want %q, got %q", i, want, stale[i])
				}
			}
		})
	}
}

func TestSummarizeStale(t *testing.T) {
	if got := summarizeStale(nil, 3); got != "" {
		t.Errorf("nil: got %q, want empty", got)
	}
	if got := summarizeStale([]string{"a"}, 3); got != "a" {
		t.Errorf("single: got %q", got)
	}
	if got := summarizeStale([]string{"a", "b", "c"}, 3); got != "a, b, c" {
		t.Errorf("at cap: got %q", got)
	}
	got := summarizeStale([]string{"a", "b", "c", "d", "e"}, 3)
	if got != "a, b, c (+2 more)" {
		t.Errorf("over cap: got %q", got)
	}
}
