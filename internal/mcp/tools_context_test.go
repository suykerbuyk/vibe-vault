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

// mkdirAllHelper and writeFileHelper are trivial wrappers used by the
// end-to-end "drop file and list" integration test. Kept local so test
// intent stays readable without context-switching into a util file.
func mkdirAllHelper(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeFileHelper(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

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
		"Projects/testproj/agentctx/tasks/active-task.md":        "# Active Task\nStatus: in-progress\nPriority: medium\n",
		"Projects/testproj/agentctx/tasks/done/old-task.md":      "# Old Task\nStatus: done\nPriority: low\n",
		"Projects/testproj/agentctx/tasks/cancelled/bad-task.md": "# Bad Task\nStatus: cancelled\nPriority: low\n",
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
		"Projects/testproj/agentctx/tasks/heading.md": "# Heading Format\n## Status: pending\n## Priority: low\n",
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

// TestListTasksOmitsEmptyMetadata verifies that taskEntry fields with
// omitempty tags are absent from the serialized JSON when their values are
// the zero value. Most active tasks in real projects have no frontmatter
// metadata; suppressing the empty lines is the primary lever for trimming
// vv_bootstrap_context payload size. Covers the Phase 1 behavior change.
func TestListTasksOmitsEmptyMetadata(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/bare.md":      "# Bare Task\n\nNo metadata whatsoever.",
		"Projects/testproj/agentctx/tasks/populated.md": "# Populated Task\nStatus: active\nPriority: high\n",
	})

	tool := NewListTasksTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Locate the bare task's object block and assert empty fields omitted.
	bareIdx := strings.Index(result, `"name": "bare"`)
	if bareIdx < 0 {
		t.Fatalf("bare task not found in output:\n%s", result)
	}
	bareBlockEnd := strings.Index(result[bareIdx:], "}")
	if bareBlockEnd < 0 {
		t.Fatalf("unterminated task block starting at %d", bareIdx)
	}
	bareBlock := result[bareIdx : bareIdx+bareBlockEnd]

	if strings.Contains(bareBlock, `"status"`) {
		t.Errorf("bare task block still contains status key:\n%s", bareBlock)
	}
	if strings.Contains(bareBlock, `"priority"`) {
		t.Errorf("bare task block still contains priority key:\n%s", bareBlock)
	}
	if strings.Contains(bareBlock, `"done"`) {
		t.Errorf("bare task block still contains done:false key:\n%s", bareBlock)
	}

	// Populated task must still carry its metadata.
	popIdx := strings.Index(result, `"name": "populated"`)
	if popIdx < 0 {
		t.Fatalf("populated task not found in output:\n%s", result)
	}
	popBlockEnd := strings.Index(result[popIdx:], "}")
	popBlock := result[popIdx : popIdx+popBlockEnd]

	if !strings.Contains(popBlock, `"status": "active"`) {
		t.Errorf("populated task missing status: %s", popBlock)
	}
	if !strings.Contains(popBlock, `"priority": "high"`) {
		t.Errorf("populated task missing priority: %s", popBlock)
	}

	// Struct unmarshaling must still yield zero values for omitted fields
	// (backward-compat guarantee for consumers).
	var parsed struct {
		Tasks []taskEntry `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(parsed.Tasks))
	}
	for _, task := range parsed.Tasks {
		if task.Name == "bare" {
			if task.Status != "" || task.Priority != "" || task.Done {
				t.Errorf("bare task unmarshaled with non-zero fields: %+v", task)
			}
		}
	}
}

// TestListTasksIncludeDoneRetainsTrueFlag confirms the omitempty on Done
// still lets through the `true` value, which is the only case consumers
// actually need to distinguish.
func TestListTasksIncludeDoneRetainsTrueFlag(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/tasks/done/retired.md": "# Retired\nStatus: done\n",
	})

	tool := NewListTasksTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","include_done":true}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, `"done": true`) {
		t.Errorf("expected done:true in output:\n%s", result)
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

// --- vv_bootstrap_context tests ---

func TestBootstrapContextBasic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{
		"s1": {
			SessionID: "s1", Project: "testproj", Date: "2026-03-10",
			Title: "Recent Session", Summary: "Did some work.",
			Decisions: []string{"Used Go"}, OpenThreads: []string{"Fix tests"},
		},
	}, map[string]string{
		"Projects/testproj/agentctx/workflow.md":             "# Workflow\n\nStep 1: do things.",
		"Projects/testproj/agentctx/resume.md":               "# Resume\n\nPick up here.",
		"Projects/testproj/agentctx/tasks/implement-auth.md": "# Implement Auth\nStatus: in-progress\nPriority: high\n",
	})

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Project     string      `json:"project"`
		Workflow    string      `json:"workflow"`
		Resume      string      `json:"resume"`
		ActiveTasks []taskEntry `json:"active_tasks"`
		Context     string      `json:"context"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
	}
	if parsed.Project != "testproj" {
		t.Errorf("project = %v, want testproj", parsed.Project)
	}
	if !strings.Contains(parsed.Workflow, "Step 1") {
		t.Errorf("workflow missing expected content, got: %s", parsed.Workflow)
	}
	if !strings.Contains(parsed.Resume, "Pick up here") {
		t.Errorf("resume missing expected content, got: %s", parsed.Resume)
	}
	if len(parsed.ActiveTasks) != 1 {
		t.Fatalf("expected 1 active task, got %d", len(parsed.ActiveTasks))
	}
	if parsed.ActiveTasks[0].Name != "implement-auth" {
		t.Errorf("task name = %v, want implement-auth", parsed.ActiveTasks[0].Name)
	}
	if parsed.Context == "" {
		t.Error("context should not be empty")
	}
}

func TestBootstrapContextMissingResume(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/workflow.md": "# Workflow",
	})

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Resume string `json:"resume"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.Resume != "" {
		t.Errorf("resume = %q, want empty string for missing resume", parsed.Resume)
	}
}

func TestBootstrapContextWorkflowFallback(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"newproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Workflow string `json:"workflow"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.Workflow == "" {
		t.Error("expected non-empty workflow from template fallback")
	}
	if strings.Contains(parsed.Workflow, "{{PROJECT}}") {
		t.Error("template placeholder {{PROJECT}} should have been replaced")
	}
}

func TestBootstrapContextNoTasks(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/workflow.md": "# Workflow",
	})

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		ActiveTasks []taskEntry `json:"active_tasks"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed.ActiveTasks) != 0 {
		t.Errorf("expected 0 active tasks, got %d", len(parsed.ActiveTasks))
	}
}

func TestBootstrapContextTokenBudget(t *testing.T) {
	// Create enough session data that inject output would be large
	entries := map[string]index.SessionEntry{}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("s%d", i)
		entries[id] = index.SessionEntry{
			SessionID:   id,
			Project:     "testproj",
			Date:        fmt.Sprintf("2026-03-%02d", (i%28)+1),
			Title:       fmt.Sprintf("Session %d with lots of context and detail", i),
			Summary:     fmt.Sprintf("Did work item %d involving multiple files and complex refactoring across the codebase.", i),
			Decisions:   []string{fmt.Sprintf("Decision %d: chose approach A over B for performance reasons", i)},
			OpenThreads: []string{fmt.Sprintf("Thread %d: need to follow up on edge case handling", i)},
		}
	}

	cfg := writeTestVault(t, entries, map[string]string{
		"Projects/testproj/agentctx/workflow.md": "# Workflow",
	})

	tool := NewBootstrapContextTool(cfg)
	// Use a small token budget to force truncation
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj","max_tokens":100}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// The context should be present but truncated (sections dropped)
	if parsed.Context == "" {
		t.Error("context should not be empty even with small budget")
	}
}

func TestBootstrapContextPathTraversal(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewBootstrapContextTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"project":"../etc/passwd"}`))
	if err == nil {
		t.Fatal("expected error for path traversal")
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

// --- vv_list_learnings tests ---

// validLearningFile is the minimal well-formed learning file body used
// by the MCP-layer tests. Kept local to this file so the knowledge
// package remains the single source of truth for format expectations.
func validLearningFile(name, desc, typ string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\ntype: " + typ + "\n---\n\nbody\n"
}

func TestListLearningsEmptyDirectoryReturnsEmptyArray(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewListLearningsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var parsed []map[string]string
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, result)
	}
	if len(parsed) != 0 {
		t.Errorf("expected empty array, got %v", parsed)
	}
}

func TestListLearningsReturnsMetadata(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Knowledge/learnings/testing-philosophy.md": validLearningFile("Testing philosophy", "proven end-to-end", "user"),
		"Knowledge/learnings/resume-phrasing.md":    validLearningFile("Resume phrasing", "precise years", "user"),
		"Knowledge/learnings/parallel-feedback.md":  validLearningFile("Parallel feedback", "ack asap", "feedback"),
	})

	tool := NewListLearningsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var parsed []struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(parsed), parsed)
	}
	// Sorted alphabetically by slug.
	wantOrder := []string{"parallel-feedback", "resume-phrasing", "testing-philosophy"}
	for i, w := range wantOrder {
		if parsed[i].Slug != w {
			t.Errorf("entry %d slug = %q, want %q", i, parsed[i].Slug, w)
		}
	}
}

func TestListLearningsFilterType(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Knowledge/learnings/u1.md": validLearningFile("U1", "a", "user"),
		"Knowledge/learnings/f1.md": validLearningFile("F1", "b", "feedback"),
	})

	tool := NewListLearningsTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"filter_type":"feedback"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var parsed []struct {
		Slug string `json:"slug"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Type != "feedback" {
		t.Errorf("expected one feedback entry, got %v", parsed)
	}
}

// --- vv_get_learning tests ---

func TestGetLearningReturnsFullContent(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Knowledge/learnings/testing.md": "---\nname: Testing\ndescription: end-to-end only\ntype: user\n---\n\nThe full body text lives here.\n",
	})

	tool := NewGetLearningTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"slug":"testing"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var parsed struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.Slug != "testing" || parsed.Name != "Testing" || parsed.Type != "user" {
		t.Errorf("metadata = %+v", parsed)
	}
	if !strings.Contains(parsed.Content, "full body text") {
		t.Errorf("content missing body text: %q", parsed.Content)
	}
}

func TestGetLearningUnknownSlug(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Knowledge/learnings/alpha.md": validLearningFile("Alpha", "a", "user"),
	})

	tool := NewGetLearningTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{"slug":"missing"}`))
	if err == nil {
		t.Fatal("expected error for unknown slug")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should list available slugs, got: %q", err.Error())
	}
}

func TestGetLearningMissingSlugParam(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewGetLearningTool(cfg)
	_, err := tool.Handler(json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing slug")
	}
}

// --- vv_bootstrap_context knowledge_learnings_available extension ---

func TestBootstrapContextOmitsLearningsFieldWhenEmpty(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/workflow.md": "# Workflow",
	})

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	// Assert on the raw string so we catch "field present but null"
	// regressions that a typed unmarshal would mask.
	if strings.Contains(result, "knowledge_learnings_available") {
		t.Errorf("expected the field to be omitted when no learnings exist, got: %s", result)
	}
}

func TestBootstrapContextEmitsLearningsFieldWhenPopulated(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/workflow.md": "# Workflow",
		"Knowledge/learnings/testing.md":         validLearningFile("Testing", "x", "user"),
		"Knowledge/learnings/feedback-loop.md":   validLearningFile("Feedback loop", "y", "feedback"),
	})

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var parsed struct {
		KnowledgeLearningsAvailable *struct {
			Count int    `json:"count"`
			Hint  string `json:"hint"`
		} `json:"knowledge_learnings_available"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.KnowledgeLearningsAvailable == nil {
		t.Fatal("expected field to be present with populated learnings dir")
	}
	if parsed.KnowledgeLearningsAvailable.Count != 2 {
		t.Errorf("count = %d, want 2", parsed.KnowledgeLearningsAvailable.Count)
	}
	if !strings.Contains(parsed.KnowledgeLearningsAvailable.Hint, "vv_list_learnings") {
		t.Errorf("hint should mention vv_list_learnings, got: %q", parsed.KnowledgeLearningsAvailable.Hint)
	}
}

// TestEndToEndDropFileAndList is the integration-style check required
// by the phase spec: drop a learning file on disk, call the MCP tool's
// handler, and confirm the file shows up in the result — exercising
// the wiring end-to-end inside one process.
func TestEndToEndDropFileAndList(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	// Drop a learning file after the vault is set up, to prove the
	// tool does not cache state across the write.
	learningDir := cfg.VaultPath + "/Knowledge/learnings"
	if err := mkdirAllHelper(learningDir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeFileHelper(learningDir+"/dropped.md", validLearningFile("Dropped", "just dropped", "user")); err != nil {
		t.Fatalf("write: %v", err)
	}

	listTool := NewListLearningsTool(cfg)
	result, err := listTool.Handler(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(result, "dropped") {
		t.Errorf("expected 'dropped' in list result, got: %s", result)
	}

	getTool := NewGetLearningTool(cfg)
	getRes, err := getTool.Handler(json.RawMessage(`{"slug":"dropped"}`))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(getRes, "just dropped") {
		t.Errorf("expected description 'just dropped' in get result, got: %s", getRes)
	}

	// And the bootstrap hint should now appear.
	bootstrapTool := NewBootstrapContextTool(cfg)
	bootRes, err := bootstrapTool.Handler(json.RawMessage(`{"project":"bootproj"}`))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !strings.Contains(bootRes, "knowledge_learnings_available") {
		t.Errorf("bootstrap should emit learnings field once a file exists, got: %s", bootRes)
	}
}

// ----------------------------------------------------------------------
// Phase 4 (session-slot-multihost-disambiguation) — vv_bootstrap_context
// surfaces wrap-iter drift in response.Warnings on default branch;
// emits no warning on feature branch.
// ----------------------------------------------------------------------

// initBootstrapDriftRepo seeds a real git repo at <dir> with default
// branch `main` and a single empty commit so check.CheckWrapIterDrift
// can resolve current/default branch. Returns the repo path.
func initBootstrapDriftRepo(t *testing.T, dir string) {
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
		{"git", "commit", "-q", "--allow-empty", "-m", "init"},
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

// chdirT changes cwd to dir for the duration of the test, restoring on
// cleanup.
func chdirT(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if chErr := os.Chdir(dir); chErr != nil {
		t.Fatalf("chdir(%q): %v", dir, chErr)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestBootstrapContext_DriftWarning(t *testing.T) {
	repo := t.TempDir()
	initBootstrapDriftRepo(t, repo)
	// Local stamp says iter 5; vault iterations.md has up to iter 8 → behind.
	if err := os.MkdirAll(filepath.Join(repo, ".vibe-vault"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".vibe-vault", "last-iter"), []byte("5\n"), 0o644); err != nil {
		t.Fatalf("write last-iter: %v", err)
	}

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/workflow.md":   "# Workflow",
		"Projects/testproj/agentctx/iterations.md": "### Iteration 6\n### Iteration 7\n### Iteration 8\n",
	})
	chdirT(t, repo)

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Warnings []string `json:"warnings"`
	}
	if jsonErr := json.Unmarshal([]byte(result), &parsed); jsonErr != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", jsonErr, result)
	}
	if len(parsed.Warnings) == 0 {
		t.Fatalf("expected at least one warning, got 0; result:\n%s", result)
	}
	joined := strings.Join(parsed.Warnings, " ")
	if !strings.Contains(joined, "behind") {
		t.Errorf("warnings should contain 'behind', got %q", parsed.Warnings)
	}
}

func TestBootstrapContext_NoWarning_FeatureBranch(t *testing.T) {
	repo := t.TempDir()
	initBootstrapDriftRepo(t, repo)
	// Switch to a feature branch — drift check should SKIP (Pass with
	// "skipped" detail), so the warnings field stays empty.
	co := exec.Command("git", "-C", repo, "checkout", "-q", "-b", "feature/foo")
	if out, coErr := co.CombinedOutput(); coErr != nil {
		t.Fatalf("checkout: %s", out)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".vibe-vault"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".vibe-vault", "last-iter"), []byte("5\n"), 0o644); err != nil {
		t.Fatalf("write last-iter: %v", err)
	}

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/testproj/agentctx/workflow.md":   "# Workflow",
		"Projects/testproj/agentctx/iterations.md": "### Iteration 6\n### Iteration 7\n### Iteration 8\n",
	})
	chdirT(t, repo)

	tool := NewBootstrapContextTool(cfg)
	result, err := tool.Handler(json.RawMessage(`{"project":"testproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var parsed struct {
		Warnings []string `json:"warnings"`
	}
	if jsonErr := json.Unmarshal([]byte(result), &parsed); jsonErr != nil {
		t.Fatalf("invalid JSON: %v", jsonErr)
	}
	if len(parsed.Warnings) != 0 {
		t.Errorf("expected no warnings on feature branch, got %v", parsed.Warnings)
	}
	// Confirm omitempty kept the field out of the wire payload entirely
	// when no warnings present.
	if strings.Contains(result, `"warnings":`) {
		t.Errorf("warnings field should be omitted (omitempty) on feature branch, got: %s", result)
	}
}
