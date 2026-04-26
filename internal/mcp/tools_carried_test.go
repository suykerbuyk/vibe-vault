// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// resumeWithCarried is a minimal resume.md fixture with a populated
// ### Carried forward section (mirrors real resume.md formatting).
const resumeWithCarried = `# resume

## Current State

**Project:** test

## Open Threads

<!-- Active tasks, unresolved questions, next steps -->

### Active tasks (1)

- **wrap-acceleration-epic** — six-phase epic.

### Carried forward

- **mcp subtest** — tool-count assertion brittle.
- **dry-run coverage gap** — outer syncProject short-circuits.
- **session synthesis agent** — enabled by default, inert without LLM
  provider. Broader real-world validation pending.

### Resolved earlier (archived inline)

Nothing yet.

## Project History

nothing here
`

// resumeEmptyCarried has a ### Carried forward with no bullets.
const resumeEmptyCarried = `# resume

## Open Threads

### Carried forward

### Resolved earlier (archived inline)

Nothing.

## Project History

nothing
`

// resumeSingleCarried has exactly one bullet.
const resumeSingleCarried = `# resume

## Open Threads

### Carried forward

- **only-bullet** — the sole carried item.

## Project History

nothing
`

// ── vv_carried_add ────────────────────────────────────────────────────────────

func TestCarriedAdd_ToEmpty(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeEmptyCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "new-item",
		"title":   "a new carried item",
		"body":    "detail text here",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res["bytes_written"].(float64) == 0 {
		t.Error("bytes_written should be non-zero")
	}
	if res["slug"].(string) != "new-item" {
		t.Errorf("slug: got %q, want new-item", res["slug"])
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if !strings.Contains(got, "**new-item**") {
		t.Error("new bullet missing")
	}
	if !strings.Contains(got, "a new carried item") {
		t.Error("title missing")
	}
}

func TestCarriedAdd_ToSingle(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeSingleCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "new-item",
		"title":   "new title",
		"body":    "",
	})
	_, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if !strings.Contains(got, "**new-item**") {
		t.Error("new bullet missing")
	}
	// Original bullet preserved.
	if !strings.Contains(got, "**only-bullet**") {
		t.Error("original bullet missing")
	}
	// New bullet appears after original.
	origIdx := strings.Index(got, "**only-bullet**")
	newIdx := strings.Index(got, "**new-item**")
	if newIdx <= origIdx {
		t.Errorf("new bullet should be after original: orig=%d new=%d", origIdx, newIdx)
	}
}

func TestCarriedAdd_ToMulti(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "new-item",
		"title":   "new title",
		"body":    "body text",
	})
	_, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if !strings.Contains(got, "**new-item**") {
		t.Error("new bullet missing")
	}
	// All originals preserved.
	if !strings.Contains(got, "**mcp subtest**") {
		t.Error("first original missing")
	}
	if !strings.Contains(got, "**dry-run coverage gap**") {
		t.Error("second original missing")
	}
	if !strings.Contains(got, "**session synthesis agent**") {
		t.Error("third original missing")
	}
}

func TestCarriedAdd_SlugAlreadyExists(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "mcp subtest",
		"title":   "dup",
		"body":    "",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want already-exists error, got %v", err)
	}
}

func TestCarriedAdd_SlugAlreadyExists_CaseInsensitive(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "MCP SUBTEST",
		"title":   "dup",
		"body":    "",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want already-exists (case-insensitive) error, got %v", err)
	}
}

func TestCarriedAdd_MissingSlug(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "",
		"title":   "title",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Fatalf("want slug-required error, got %v", err)
	}
}

func TestCarriedAdd_MissingTitle(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "new-slug",
		"title":   "",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("want title-required error, got %v", err)
	}
}

func TestCarriedAdd_ProjectNotFound(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "ghost",
		"slug":    "new",
		"title":   "title",
	})
	_, err := tool.Handler(params)
	if err == nil {
		t.Fatal("want error for missing project")
	}
}

func TestCarriedAdd_CanonicalBulletForm(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeEmptyCarried,
	})
	tool := NewCarriedAddTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "my-slug",
		"title":   "my title",
		"body":    "",
	})
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	// Canonical form: "- **my-slug**"
	if !strings.Contains(got, "- **my-slug**") {
		t.Errorf("not in canonical form, doc:\n%s", got)
	}
}

// ── vv_carried_remove ─────────────────────────────────────────────────────────

func TestCarriedRemove_Single(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeSingleCarried,
	})
	tool := NewCarriedRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "only-bullet",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if strings.Contains(got, "**only-bullet**") {
		t.Error("removed bullet should be gone")
	}
	if !strings.Contains(got, "### Carried forward") {
		t.Error("Carried forward heading should be preserved")
	}
}

func TestCarriedRemove_Multi_First(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "mcp subtest",
	})
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if strings.Contains(got, "**mcp subtest**") {
		t.Error("removed bullet should be gone")
	}
	if !strings.Contains(got, "**dry-run coverage gap**") {
		t.Error("second bullet should be preserved")
	}
}

func TestCarriedRemove_Multi_Last(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "session synthesis agent",
	})
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	got := string(data)
	if strings.Contains(got, "**session synthesis agent**") {
		t.Error("removed bullet should be gone")
	}
	if !strings.Contains(got, "**mcp subtest**") {
		t.Error("first bullet should be preserved")
	}
}

func TestCarriedRemove_CaseInsensitive(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeSingleCarried,
	})
	tool := NewCarriedRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "ONLY-BULLET",
	})
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("case-insensitive remove failed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	if strings.Contains(string(data), "**only-bullet**") {
		t.Error("removed bullet should be gone")
	}
}

func TestCarriedRemove_SlugNotFound(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "ghost-slug",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "ghost-slug") {
		t.Fatalf("want not-found error, got %v", err)
	}
	// Error should list available slugs.
	if !strings.Contains(err.Error(), "mcp subtest") {
		t.Errorf("error should list available slugs: %v", err)
	}
}

func TestCarriedRemove_MissingSlug(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedRemoveTool(c)
	params, _ := json.Marshal(map[string]any{
		"project": "proj",
		"slug":    "",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Fatalf("want slug-required error, got %v", err)
	}
}

// ── vv_carried_promote_to_task ────────────────────────────────────────────────

func TestCarriedPromote_Basic(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedPromoteToTaskTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":       "proj",
		"slug":          "mcp subtest",
		"new_task_slug": "fix-mcp-subtest",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if unmarshalErr := json.Unmarshal([]byte(result), &res); unmarshalErr != nil {
		t.Fatalf("invalid JSON: %v", unmarshalErr)
	}
	// Check result fields.
	if res["slug"].(string) != "mcp subtest" {
		t.Errorf("slug: got %q", res["slug"])
	}
	if res["new_task_slug"].(string) != "fix-mcp-subtest" {
		t.Errorf("new_task_slug: got %q", res["new_task_slug"])
	}
	taskPath, _ := res["task_path"].(string)
	if taskPath == "" {
		t.Error("task_path missing from result")
	}

	// Task file created.
	taskData, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatalf("task file not created: %v", err)
	}
	taskContent := string(taskData)
	if !strings.Contains(taskContent, "fix-mcp-subtest") {
		t.Error("task file missing task slug")
	}
	if !strings.Contains(taskContent, "mcp subtest") {
		t.Error("task file missing source slug")
	}
	if !strings.Contains(taskContent, "## Description") {
		t.Error("task file missing Description section")
	}

	// Bullet removed from resume.
	resumeData, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	resumeContent := string(resumeData)
	if strings.Contains(resumeContent, "**mcp subtest**") {
		t.Error("promoted bullet should be removed from resume")
	}
	// Other bullets preserved.
	if !strings.Contains(resumeContent, "**dry-run coverage gap**") {
		t.Error("other bullets should be preserved")
	}
}

func TestCarriedPromote_SingleBullet(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeSingleCarried,
	})
	tool := NewCarriedPromoteToTaskTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":       "proj",
		"slug":          "only-bullet",
		"new_task_slug": "only-bullet-task",
	})
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resumeData, _ := os.ReadFile(filepath.Join(c.VaultPath, "Projects", "proj", "agentctx", "resume.md"))
	if strings.Contains(string(resumeData), "**only-bullet**") {
		t.Error("promoted bullet should be removed")
	}
}

func TestCarriedPromote_SlugNotFound(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedPromoteToTaskTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":       "proj",
		"slug":          "ghost-slug",
		"new_task_slug": "ghost-task",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "ghost-slug") {
		t.Fatalf("want not-found error, got %v", err)
	}
	if !strings.Contains(err.Error(), "mcp subtest") {
		t.Errorf("error should list available slugs: %v", err)
	}
}

func TestCarriedPromote_TaskAlreadyExists(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md":             resumeWithCarried,
		"Projects/proj/agentctx/tasks/existing-task.md": "# existing task\n",
	})
	tool := NewCarriedPromoteToTaskTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":       "proj",
		"slug":          "mcp subtest",
		"new_task_slug": "existing-task",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want task-already-exists error, got %v", err)
	}
}

func TestCarriedPromote_MissingSlug(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedPromoteToTaskTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":       "proj",
		"slug":          "",
		"new_task_slug": "task",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "slug is required") {
		t.Fatalf("want slug-required error, got %v", err)
	}
}

func TestCarriedPromote_MissingNewTaskSlug(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeWithCarried,
	})
	tool := NewCarriedPromoteToTaskTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":       "proj",
		"slug":          "mcp subtest",
		"new_task_slug": "",
	})
	_, err := tool.Handler(params)
	if err == nil || !strings.Contains(err.Error(), "new_task_slug is required") {
		t.Fatalf("want new_task_slug-required error, got %v", err)
	}
}

func TestCarriedPromote_TaskFrontmatterShape(t *testing.T) {
	c := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/proj/agentctx/resume.md": resumeSingleCarried,
	})
	tool := NewCarriedPromoteToTaskTool(c)
	params, _ := json.Marshal(map[string]any{
		"project":       "proj",
		"slug":          "only-bullet",
		"new_task_slug": "my-new-task",
	})
	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	taskPath := res["task_path"].(string)
	data, _ := os.ReadFile(taskPath)
	content := string(data)
	// Check frontmatter shape.
	if !strings.Contains(content, "# Task:") {
		t.Error("task file missing # Task: heading")
	}
	if !strings.Contains(content, "**Status:**") {
		t.Error("task file missing Status field")
	}
	if !strings.Contains(content, "**Source:**") {
		t.Error("task file missing Source field")
	}
	if !strings.Contains(content, "## Description") {
		t.Error("task file missing ## Description section")
	}
	// Bullet body verbatim.
	if !strings.Contains(content, "the sole carried item") {
		t.Error("task file missing bullet body text")
	}
}
