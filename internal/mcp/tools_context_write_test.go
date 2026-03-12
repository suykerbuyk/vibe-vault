// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/index"
)

// --- vv_update_resume tests ---

func TestUpdateResumeBasic(t *testing.T) {
	resume := "# Resume\n\n## Current Focus\n\nOld focus content.\n\n## Open Threads\n\nSome threads.\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/resume.md": resume,
	})

	tool := NewUpdateResumeTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","section":"Current Focus","content":"New focus content here."}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "Updated section") {
		t.Errorf("result = %q, want success message", result)
	}

	// Verify file was updated
	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "resume.md"))
	content := string(data)
	if !strings.Contains(content, "New focus content here.") {
		t.Errorf("file should contain new content, got:\n%s", content)
	}
	if strings.Contains(content, "Old focus content") {
		t.Errorf("file should not contain old content, got:\n%s", content)
	}
}

func TestUpdateResumeSectionNotFound(t *testing.T) {
	resume := "# Resume\n\n## Current Focus\n\nSome content.\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/resume.md": resume,
	})

	tool := NewUpdateResumeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","section":"Nonexistent Section","content":"stuff"}`))
	if err == nil {
		t.Fatal("expected error for missing section")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

func TestUpdateResumeFileNotFound(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewUpdateResumeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","section":"Focus","content":"stuff"}`))
	if err == nil {
		t.Fatal("expected error for missing resume.md")
	}
	if !strings.Contains(err.Error(), "resume.md not found") {
		t.Errorf("error = %v, want 'resume.md not found'", err)
	}
}

func TestUpdateResumePreservesOtherSections(t *testing.T) {
	resume := "# Resume\n\n## Section A\n\nA content.\n\n## Section B\n\nB content.\n\n## Section C\n\nC content.\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/resume.md": resume,
	})

	tool := NewUpdateResumeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","section":"Section B","content":"Updated B."}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "resume.md"))
	content := string(data)
	if !strings.Contains(content, "A content.") {
		t.Error("Section A content should be preserved")
	}
	if !strings.Contains(content, "Updated B.") {
		t.Error("Section B should have new content")
	}
	if strings.Contains(content, "B content.") {
		t.Error("Section B old content should be replaced")
	}
	if !strings.Contains(content, "C content.") {
		t.Error("Section C content should be preserved")
	}
}

func TestUpdateResumeLastSection(t *testing.T) {
	resume := "# Resume\n\n## First\n\nFirst content.\n\n## Last\n\nLast content.\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/resume.md": resume,
	})

	tool := NewUpdateResumeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","section":"Last","content":"New last content."}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "resume.md"))
	content := string(data)
	if !strings.Contains(content, "New last content.") {
		t.Error("Last section should have new content")
	}
	if !strings.Contains(content, "First content.") {
		t.Error("First section should be preserved")
	}
}

func TestUpdateResumePathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewUpdateResumeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../etc","section":"Focus","content":"stuff"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

// --- vv_append_iteration tests ---

func TestAppendIterationAutoIncrement(t *testing.T) {
	iterations := "# Iterations\n\n### Iteration 1 — Setup (2026-03-01)\n\nInitial setup.\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": iterations,
	})

	tool := NewAppendIterationTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","title":"Phase 2","narrative":"Added features.","date":"2026-03-12"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "iteration 2") {
		t.Errorf("result = %q, want 'iteration 2'", result)
	}

	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))
	content := string(data)
	if !strings.Contains(content, "### Iteration 2 — Phase 2 (2026-03-12)") {
		t.Errorf("file should contain iteration 2 heading, got:\n%s", content)
	}
	if !strings.Contains(content, "Added features.") {
		t.Errorf("file should contain narrative, got:\n%s", content)
	}
}

func TestAppendIterationExplicitNumber(t *testing.T) {
	iterations := "# Iterations\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": iterations,
	})

	tool := NewAppendIterationTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","iteration":5,"title":"Jump Ahead","narrative":"Skipped some.","date":"2026-03-12"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "iteration 5") {
		t.Errorf("result = %q, want 'iteration 5'", result)
	}
}

func TestAppendIterationDuplicateNumber(t *testing.T) {
	iterations := "# Iterations\n\n### Iteration 3 — Existing (2026-03-01)\n\nContent.\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": iterations,
	})

	tool := NewAppendIterationTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","iteration":3,"title":"Dupe","narrative":"Nope.","date":"2026-03-12"}`))
	if err == nil {
		t.Fatal("expected error for duplicate iteration number")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want 'already exists'", err)
	}
}

func TestAppendIterationCreatesFile(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/resume.md": "# Resume\n",
	})

	tool := NewAppendIterationTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","title":"First","narrative":"Starting fresh.","date":"2026-03-12"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "# Iterations") {
		t.Error("file should have header")
	}
	if !strings.Contains(content, "### Iteration 1") {
		t.Error("first iteration should be 1")
	}
}

func TestAppendIterationInvalidDate(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})

	tool := NewAppendIterationTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","title":"Bad","narrative":"Nope.","date":"March 12"}`))
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Errorf("error = %v, want 'invalid date format'", err)
	}
}

func TestAppendIterationDefaultDate(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/iterations.md": "# Iterations\n",
	})

	tool := NewAppendIterationTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","title":"Today","narrative":"Using default date."}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "iterations.md"))
	content := string(data)
	// Should contain today's date in the heading
	if !strings.Contains(content, "### Iteration 1 — Today") {
		t.Errorf("missing iteration heading, got:\n%s", content)
	}
}

// --- iteration helpers ---

func TestIterationHeadingRoundTrip(t *testing.T) {
	// Verify that iterationHeading output is parseable by scanIterationNumbers.
	// This catches format drift between the writer and the parser.
	for _, num := range []int{1, 5, 42, 100} {
		heading := iterationHeading(num, "Some Title", "2026-03-12")
		got := scanIterationNumbers(heading)
		if len(got) != 1 || got[0] != num {
			t.Errorf("iterationHeading(%d) produced %q, scanIterationNumbers returned %v — want [%d]",
				num, heading, got, num)
		}
	}
}

func TestScanIterationNumbers(t *testing.T) {
	content := "# Iterations\n\n### Iteration 1 — Setup (2026-03-01)\n\nContent.\n\n### Iteration 3 — Phase 3 (2026-03-10)\n\nMore.\n"
	got := scanIterationNumbers(content)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Errorf("scanIterationNumbers = %v, want [1, 3]", got)
	}
}

// --- vv_manage_task tests ---

func TestManageTaskCreate(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/.keep": "",
	})

	tool := NewManageTaskTool(cfg)
	content := "# New Task\nStatus: pending\nPriority: high\n\nDescription."
	params := map[string]string{"project": "testproj", "task": "new-task", "action": "create", "content": content}
	data, _ := json.Marshal(params)
	result, err := tool.Handler(json.RawMessage(data))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "Created task") {
		t.Errorf("result = %q, want success message", result)
	}

	// Verify file
	fileData, err := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "tasks", "new-task.md"))
	if err != nil {
		t.Fatalf("file should exist: %v", err)
	}
	if string(fileData) != content {
		t.Errorf("file content = %q, want %q", string(fileData), content)
	}
}

func TestManageTaskCreateAlreadyExists(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/existing.md": "# Existing\n",
	})

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"existing","action":"create","content":"stuff"}`))
	if err == nil {
		t.Fatal("expected error for existing task")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %v, want 'already exists'", err)
	}
}

func TestManageTaskCreateNoContent(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"no-content","action":"create"}`))
	if err == nil {
		t.Fatal("expected error for missing content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Errorf("error = %v, want 'content is required'", err)
	}
}

func TestManageTaskUpdateStatus(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/my-task.md": "# My Task\nStatus: pending\nPriority: high\n\nDetails.\n",
	})

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"my-task","action":"update_status","status":"in-progress"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "tasks", "my-task.md"))
	content := string(data)
	if !strings.Contains(content, "Status: in-progress") {
		t.Errorf("status should be updated, got:\n%s", content)
	}
	if strings.Contains(content, "Status: pending") {
		t.Error("old status should be replaced")
	}
}

func TestManageTaskUpdateStatusNoStatus(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/my-task.md": "# My Task\nStatus: pending\n",
	})

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"my-task","action":"update_status"}`))
	if err == nil {
		t.Fatal("expected error for missing status")
	}
	if !strings.Contains(err.Error(), "status is required") {
		t.Errorf("error = %v, want 'status is required'", err)
	}
}

func TestManageTaskUpdateStatusNotFound(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"nonexistent","action":"update_status","status":"done"}`))
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

func TestManageTaskRetire(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/old-task.md": "# Old Task\nStatus: in-progress\nPriority: low\n\nDone now.\n",
	})

	tool := NewManageTaskTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"old-task","action":"retire"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "Retired") {
		t.Errorf("result = %q, want 'Retired'", result)
	}

	// Original should be gone
	if _, statErr := os.Stat(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "tasks", "old-task.md")); !os.IsNotExist(statErr) {
		t.Error("original task file should be removed")
	}

	// Should exist in done/
	data, err := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects", "testproj", "agentctx", "tasks", "done", "old-task.md"))
	if err != nil {
		t.Fatalf("retired task should exist in done/: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "Status: Done") {
		t.Errorf("retired task should have Status: Done, got:\n%s", content)
	}
}

func TestManageTaskRetireNotFound(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"nonexistent","action":"retire"}`))
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

func TestManageTaskUnknownAction(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"my-task","action":"delete"}`))
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("error = %v, want 'unknown action'", err)
	}
}

func TestManageTaskPathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewManageTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj","task":"../../../etc/passwd","action":"create","content":"hack"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

// --- vv_refresh_index tests ---

func TestRefreshIndexBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/sessions/2026-03-12-01.md": "---\nsession_id: test-session-001\nproject: testproj\ndate: 2026-03-12\n---\n# Session\n\nDid things.\n",
	})

	tool := NewRefreshIndexTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		SessionsIndexed int      `json:"sessions_indexed"`
		ProjectsUpdated int      `json:"projects_updated"`
		Projects        []string `json:"projects"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
	}
	if parsed.SessionsIndexed < 1 {
		t.Errorf("sessions_indexed = %d, want >= 1", parsed.SessionsIndexed)
	}
}

func TestRefreshIndexEmptyVault(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/.keep": "",
	})

	tool := NewRefreshIndexTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		SessionsIndexed int      `json:"sessions_indexed"`
		Projects        []string `json:"projects"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
	}
	if parsed.SessionsIndexed != 0 {
		t.Errorf("sessions_indexed = %d, want 0", parsed.SessionsIndexed)
	}
	if len(parsed.Projects) != 0 {
		t.Errorf("projects = %v, want empty", parsed.Projects)
	}
}

// --- replaceStatus tests ---

func TestReplaceStatusPlainFormat(t *testing.T) {
	input := "# Task\nStatus: pending\nPriority: high\n"
	got := replaceStatus(input, "done")
	if !strings.Contains(got, "Status: done") {
		t.Errorf("expected 'Status: done', got:\n%s", got)
	}
	if strings.Contains(got, "## Status") {
		t.Error("should preserve plain format, not heading format")
	}
}

func TestReplaceStatusHeadingFormat(t *testing.T) {
	input := "# Task\n## Status: pending\n## Priority: high\n"
	got := replaceStatus(input, "done")
	if !strings.Contains(got, "## Status: done") {
		t.Errorf("expected '## Status: done', got:\n%s", got)
	}
}
