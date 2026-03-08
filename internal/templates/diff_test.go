// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiffIdentical(t *testing.T) {
	result := unifiedDiff("a", "b", "hello\nworld\n", "hello\nworld\n")
	if result != "" {
		t.Errorf("expected empty diff for identical content, got:\n%s", result)
	}
}

func TestUnifiedDiffAdded(t *testing.T) {
	result := unifiedDiff("a", "b", "line1\nline3\n", "line1\nline2\nline3\n")
	if !strings.Contains(result, "+line2") {
		t.Errorf("expected +line2 in diff:\n%s", result)
	}
	if strings.Contains(result, "-line") {
		t.Errorf("unexpected removal in diff:\n%s", result)
	}
}

func TestUnifiedDiffRemoved(t *testing.T) {
	result := unifiedDiff("a", "b", "line1\nline2\nline3\n", "line1\nline3\n")
	if !strings.Contains(result, "-line2") {
		t.Errorf("expected -line2 in diff:\n%s", result)
	}
}

func TestUnifiedDiffModified(t *testing.T) {
	result := unifiedDiff("a", "b", "hello\nworld\n", "hello\nearth\n")
	if !strings.Contains(result, "-world") {
		t.Errorf("expected -world in diff:\n%s", result)
	}
	if !strings.Contains(result, "+earth") {
		t.Errorf("expected +earth in diff:\n%s", result)
	}
}

func TestUnifiedDiffPreservesTemplateVars(t *testing.T) {
	a := "# {{PROJECT}}\nold content\n"
	b := "# {{PROJECT}}\nnew content\n"
	result := unifiedDiff("a", "b", a, b)
	if !strings.Contains(result, "{{PROJECT}}") {
		t.Errorf("expected {{PROJECT}} preserved in diff:\n%s", result)
	}
}

func TestUnifiedDiffHasHunkHeaders(t *testing.T) {
	result := unifiedDiff("a/file", "b/file", "line1\nline2\n", "line1\nline3\n")
	if !strings.Contains(result, "@@") {
		t.Errorf("expected @@ hunk header in diff:\n%s", result)
	}
	if !strings.HasPrefix(result, "--- a/file\n+++ b/file\n") {
		t.Errorf("expected file header, got:\n%s", result)
	}
}

func TestDiffReturnsErrorForUnknown(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	_, err := reg.Diff(dir, "nonexistent.md")
	if err == nil {
		t.Error("expected error for unknown template")
	}
}

func TestDiffReturnsErrorForMissingVault(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	_, err := reg.Diff(dir, "agentctx/workflow.md")
	if err == nil {
		t.Error("expected error for missing vault file")
	}
}

func TestDiffIdenticalReturnsEmpty(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	const relPath = "session-summary.md"
	content, _ := reg.DefaultContent(relPath)
	os.MkdirAll(filepath.Dir(filepath.Join(dir, relPath)), 0o755)
	os.WriteFile(filepath.Join(dir, relPath), content, 0o644)

	d, err := reg.Diff(dir, relPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != "" {
		t.Errorf("expected empty diff for identical file, got:\n%s", d)
	}
}

func TestDiffCustomizedReturnsOutput(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	const relPath = "session-summary.md"
	os.MkdirAll(filepath.Dir(filepath.Join(dir, relPath)), 0o755)
	os.WriteFile(filepath.Join(dir, relPath), []byte("custom"), 0o644)

	d, err := reg.Diff(dir, relPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == "" {
		t.Error("expected non-empty diff for customized file")
	}
	if !strings.Contains(d, "@@") {
		t.Errorf("expected hunk header in diff:\n%s", d)
	}
}

func TestDiffAll(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	// Write all at default, then customize two
	reg.ResetAll(dir)

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("custom readme"), 0o644)
	os.WriteFile(filepath.Join(dir, "session-summary.md"), []byte("custom summary"), 0o644)

	result := reg.DiffAll(dir)
	if result == "" {
		t.Fatal("expected non-empty DiffAll output")
	}

	// Should contain diffs for both customized files
	if !strings.Contains(result, "README.md") {
		t.Error("expected README.md in DiffAll output")
	}
	if !strings.Contains(result, "session-summary.md") {
		t.Error("expected session-summary.md in DiffAll output")
	}

	// Should NOT contain diff for non-customized files
	if strings.Contains(result, "agentctx/workflow.md") {
		t.Error("unexpected agentctx/workflow.md in DiffAll output (not customized)")
	}
}

func TestDiffAllEmpty(t *testing.T) {
	reg := New()
	dir := t.TempDir()

	// Write all at default
	reg.ResetAll(dir)

	result := reg.DiffAll(dir)
	if result != "" {
		t.Errorf("expected empty DiffAll when all match defaults, got:\n%s", result)
	}
}
