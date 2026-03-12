// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/index"
)

// --- vv_get_workflow tests ---

func TestGetWorkflowBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/workflow.md": "# Workflow\n\nDo the thing.",
	})

	tool := NewGetWorkflowTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "Do the thing") {
		t.Errorf("result = %q, want to contain 'Do the thing'", result)
	}
}

func TestGetWorkflowFallbackToTemplate(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewGetWorkflowTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"newproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	// Embedded template should have been returned with PROJECT substituted
	if result == "" {
		t.Error("expected non-empty fallback content")
	}
	if strings.Contains(result, "{{PROJECT}}") {
		t.Error("template placeholder {{PROJECT}} should have been replaced")
	}
}

func TestGetWorkflowPathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewGetWorkflowTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../etc/passwd"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

// --- vv_get_resume tests ---

func TestGetResumeBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/resume.md": "# Resume\n\nPick up where you left off.",
	})

	tool := NewGetResumeTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "Pick up where you left off") {
		t.Errorf("result = %q, want to contain resume content", result)
	}
}

func TestGetResumeMissing(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewGetResumeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"noproject"}`))
	if err == nil {
		t.Fatal("expected error for missing resume.md")
	}
	if !strings.Contains(err.Error(), "resume.md not found") {
		t.Errorf("error = %v, want 'resume.md not found'", err)
	}
}

func TestGetResumePathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewGetResumeTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../../etc"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "invalid project name") {
		t.Errorf("error = %v, want 'invalid project name'", err)
	}
}

// --- vv_list_tasks tests ---

func TestListTasksBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/implement-auth.md": "# Implement Auth\nStatus: in-progress\nPriority: high\n\nDetails here.",
		"Projects/testproj/agentctx/tasks/fix-bug.md":        "# Fix Login Bug\n## Status: blocked\n## Priority: critical\n\nMore info.",
	})

	tool := NewListTasksTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Project string      `json:"project"`
		Tasks   []taskEntry `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
	}
	if parsed.Project != "testproj" {
		t.Errorf("project = %v, want testproj", parsed.Project)
	}
	if len(parsed.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(parsed.Tasks))
	}

	// Tasks should be sorted by filename (alphabetical)
	if parsed.Tasks[0].Name != "fix-bug" {
		t.Errorf("first task = %v, want fix-bug", parsed.Tasks[0].Name)
	}
	if parsed.Tasks[0].Title != "Fix Login Bug" {
		t.Errorf("title = %v, want 'Fix Login Bug'", parsed.Tasks[0].Title)
	}
	if parsed.Tasks[0].Status != "blocked" {
		t.Errorf("status = %v, want blocked", parsed.Tasks[0].Status)
	}
	if parsed.Tasks[0].Priority != "critical" {
		t.Errorf("priority = %v, want critical", parsed.Tasks[0].Priority)
	}
}

func TestListTasksIncludeDone(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/active-task.md":          "# Active Task\nStatus: in-progress\nPriority: medium\n",
		"Projects/testproj/agentctx/tasks/done/old-task.md":        "# Old Task\nStatus: done\nPriority: low\n",
		"Projects/testproj/agentctx/tasks/cancelled/bad-task.md":   "# Bad Task\nStatus: cancelled\nPriority: low\n",
	})

	tool := NewListTasksTool(cfg)

	// Without include_done
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var parsed struct {
		Tasks []taskEntry `json:"tasks"`
	}
	json.Unmarshal([]byte(result), &parsed)
	if len(parsed.Tasks) != 1 {
		t.Errorf("without include_done: expected 1 task, got %d", len(parsed.Tasks))
	}

	// With include_done
	result, err = tool.Handler(json.RawMessage(`{"project":"testproj","include_done":true}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	json.Unmarshal([]byte(result), &parsed)
	if len(parsed.Tasks) != 3 {
		t.Errorf("with include_done: expected 3 tasks, got %d", len(parsed.Tasks))
	}

	// Verify done flag
	doneCount := 0
	for _, task := range parsed.Tasks {
		if task.Done {
			doneCount++
		}
	}
	if doneCount != 2 {
		t.Errorf("expected 2 done tasks, got %d", doneCount)
	}
}

func TestListTasksEmpty(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewListTasksTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Tasks []taskEntry `json:"tasks"`
	}
	json.Unmarshal([]byte(result), &parsed)
	if len(parsed.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(parsed.Tasks))
	}
}

func TestListTasksPathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewListTasksTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../etc/passwd"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestListTasksStatusFormats(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/plain.md":   "# Plain Format\nStatus: active\nPriority: high\n",
		"Projects/testproj/agentctx/tasks/heading.md":  "# Heading Format\n## Status: pending\n## Priority: low\n",
	})

	tool := NewListTasksTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Tasks []taskEntry `json:"tasks"`
	}
	json.Unmarshal([]byte(result), &parsed)
	if len(parsed.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(parsed.Tasks))
	}

	for _, task := range parsed.Tasks {
		if task.Status == "" {
			t.Errorf("task %q has empty status", task.Name)
		}
		if task.Priority == "" {
			t.Errorf("task %q has empty priority", task.Name)
		}
	}
}

// --- vv_get_task tests ---

func TestGetTaskBasic(t *testing.T) {
	content := "# My Task\nStatus: active\nPriority: high\n\nDetailed description here."
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/my-task.md": content,
	})

	tool := NewGetTaskTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"task":"my-task","project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result != content {
		t.Errorf("result = %q, want %q", result, content)
	}
}

func TestGetTaskFallbackToDone(t *testing.T) {
	content := "# Old Task\nStatus: done\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/done/old-task.md": content,
	})

	tool := NewGetTaskTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"task":"old-task","project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result != content {
		t.Errorf("result = %q, want %q", result, content)
	}
}

func TestGetTaskFallbackToCancelled(t *testing.T) {
	content := "# Cancelled Task\nStatus: cancelled\n"
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/cancelled/nope.md": content,
	})

	tool := NewGetTaskTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"task":"nope","project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result != content {
		t.Errorf("result = %q, want %q", result, content)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewGetTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"task":"nonexistent","project":"testproj"}`))
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

func TestGetTaskPathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewGetTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"task":"../../../etc/passwd","project":"testproj"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestGetTaskMissingName(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewGetTaskTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err == nil {
		t.Fatal("expected error for missing task name")
	}
	if !strings.Contains(err.Error(), "task name is required") {
		t.Errorf("error = %v, want 'task name is required'", err)
	}
}

// --- resolveProject tests ---

func TestResolveProjectExplicit(t *testing.T) {
	name, err := resolveProject("myproj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "myproj" {
		t.Errorf("name = %v, want myproj", name)
	}
}

func TestResolveProjectInvalidExplicit(t *testing.T) {
	_, err := resolveProject("../bad")
	if err == nil {
		t.Fatal("expected error for invalid project name")
	}
}

// --- validateTaskName tests ---

func TestValidateTaskName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"my-task", false},
		{"implement-auth", false},
		{"", true},
		{"../etc", true},
		{"foo/bar", true},
		{`foo\bar`, true},
		{"a..b", true},
	}
	for _, tt := range tests {
		err := validateTaskName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("validateTaskName(%q) err=%v, wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}
