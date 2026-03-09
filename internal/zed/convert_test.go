// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/transcript"
)

func parseTestThread(t *testing.T, opts ...threadOpt) *Thread {
	t.Helper()
	data := makeThreadJSON(t, opts...)
	thread, err := ParseThread("test-id", "test", "2026-03-08T12:00:00Z", "", "", data)
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestConvert_BasicThread(t *testing.T) {
	thread := parseTestThread(t,
		withTitle("Basic Test"),
		withModel("anthropic", "claude-sonnet-4-5-20250514"),
		withRawMessages(
			rawUserMsg(t, "Hello"),
			rawAgentMsg(t, "Hi there!"),
		),
		withRequestTokenUsage(map[string]TokenUsage{
			"req-1": {InputTokens: 600, OutputTokens: 300, CacheReads: 100, CacheWrites: 50},
			"req-2": {InputTokens: 400, OutputTokens: 200, CacheReads: 100, CacheWrites: 50},
		}),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.SessionID != "zed:test-id" {
		t.Errorf("SessionID = %q, want %q", result.Stats.SessionID, "zed:test-id")
	}
	if result.Stats.Model != "anthropic/claude-sonnet-4-5-20250514" {
		t.Errorf("Model = %q, want %q", result.Stats.Model, "anthropic/claude-sonnet-4-5-20250514")
	}
	if result.Stats.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", result.Stats.InputTokens)
	}
	if result.Stats.OutputTokens != 500 {
		t.Errorf("OutputTokens = %d, want 500", result.Stats.OutputTokens)
	}
	if result.Stats.CacheReads != 200 {
		t.Errorf("CacheReads = %d, want 200", result.Stats.CacheReads)
	}
	if result.Stats.CacheWrites != 100 {
		t.Errorf("CacheWrites = %d, want 100", result.Stats.CacheWrites)
	}
	if result.Stats.EndTime != testTime() {
		t.Errorf("EndTime = %v, want %v", result.Stats.EndTime, testTime())
	}
	if len(result.Entries) != 2 {
		t.Errorf("Entries count = %d, want 2", len(result.Entries))
	}
}

func TestConvert_NilThread(t *testing.T) {
	result, err := Convert(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil result for nil thread")
	}
}

func TestConvert_ToolNormalization(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Fix this file"),
			rawAgentMsgWithTools(t, "I'll fix that.",
				[]interface{}{
					rawToolUse("terminal", "tu-1", map[string]interface{}{"command": "ls"}),
					rawToolUse("read_file", "tu-2", map[string]interface{}{"file_path": "main.go"}),
					rawToolUse("edit_file", "tu-3", map[string]interface{}{"file_path": "main.go"}),
					rawToolUse("find_path", "tu-4", map[string]interface{}{"pattern": "*.go"}),
					rawToolUse("list_directory", "tu-5", map[string]interface{}{"path": "."}),
					rawToolUse("create_file", "tu-6", map[string]interface{}{"file_path": "new.go"}),
				},
				map[string]interface{}{},
			),
		),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]int{
		"Bash":    1,
		"Read":    1,
		"Edit":    1,
		"Glob":    1,
		"ListDir": 1,
		"Write":   1,
	}

	for tool, count := range expected {
		if result.Stats.ToolCounts[tool] != count {
			t.Errorf("ToolCounts[%q] = %d, want %d", tool, result.Stats.ToolCounts[tool], count)
		}
	}
	if result.Stats.ToolUses != 6 {
		t.Errorf("ToolUses = %d, want 6", result.Stats.ToolUses)
	}
}

func TestConvert_TokenAggregation(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(rawUserMsg(t, "test")),
		withRequestTokenUsage(map[string]TokenUsage{
			"r1": {InputTokens: 2000, OutputTokens: 1000, CacheReads: 500, CacheWrites: 200},
			"r2": {InputTokens: 3000, OutputTokens: 2000, CacheReads: 500, CacheWrites: 300},
		}),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.InputTokens != 5000 {
		t.Errorf("InputTokens = %d, want 5000", result.Stats.InputTokens)
	}
	if result.Stats.OutputTokens != 3000 {
		t.Errorf("OutputTokens = %d, want 3000", result.Stats.OutputTokens)
	}
}

func TestConvert_NilTokenUsage(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(rawUserMsg(t, "test")),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0", result.Stats.InputTokens)
	}
}

func TestConvert_NilSnapshot(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(rawUserMsg(t, "test")),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.CWD != "" {
		t.Errorf("CWD = %q, want empty", result.Stats.CWD)
	}
}

func TestConvert_WithSnapshot(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(rawUserMsg(t, "test")),
		withSnapshot("/home/user/project", "feature-branch", ""),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want %q", result.Stats.CWD, "/home/user/project")
	}
	if result.Stats.GitBranch != "feature-branch" {
		t.Errorf("GitBranch = %q, want %q", result.Stats.GitBranch, "feature-branch")
	}
}

func TestConvert_ThinkingBlocks(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "think about this"),
			rawAgentMsgWithThinking(t, "Let me think...", "Here's my answer"),
		),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.ThinkingBlocks != 1 {
		t.Errorf("ThinkingBlocks = %d, want 1", result.Stats.ThinkingBlocks)
	}
}

func TestConvert_EmptyThread(t *testing.T) {
	thread := parseTestThread(t, withRawMessages())

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Entries) != 0 {
		t.Errorf("Entries = %d, want 0", len(result.Entries))
	}
}

func TestConvert_MentionInUserMessage(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "Fix this file", "/home/user/src/main.go"),
		),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1", len(result.Entries))
	}
	blocks := transcript.ContentBlocks(result.Entries[0].Message)
	found := false
	for _, b := range blocks {
		if b.Type == "text" && b.Text == "@/home/user/src/main.go" {
			found = true
		}
	}
	if !found {
		t.Error("expected @/home/user/src/main.go text block for mention")
	}
}

func TestConvert_ModelFormatting(t *testing.T) {
	tests := []struct {
		name     string
		model    *ZedModel
		expected string
	}{
		{"full", &ZedModel{Provider: "anthropic", Model: "claude-sonnet-4-5-20250514"}, "anthropic/claude-sonnet-4-5-20250514"},
		{"no provider", &ZedModel{Model: "gpt-4"}, "gpt-4"},
		{"nil", nil, "unknown"},
		{"no model", &ZedModel{Provider: "anthropic"}, "anthropic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := modelString(tt.model)
			if result != tt.expected {
				t.Errorf("modelString = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestConvert_BranchFallback(t *testing.T) {
	thread := parseTestThread(t, withRawMessages(rawUserMsg(t, "test")))
	thread.WorktreeBranch = "fallback-branch"

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.GitBranch != "fallback-branch" {
		t.Errorf("GitBranch = %q, want %q", result.Stats.GitBranch, "fallback-branch")
	}
}

func TestConvert_EndTime(t *testing.T) {
	thread := parseTestThread(t, withRawMessages(rawUserMsg(t, "test")))
	thread.UpdatedAt = time.Date(2026, 3, 8, 15, 30, 0, 0, time.UTC)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	if result.Stats.EndTime != thread.UpdatedAt {
		t.Errorf("EndTime = %v, want %v", result.Stats.EndTime, thread.UpdatedAt)
	}
	if !result.Stats.StartTime.IsZero() {
		t.Errorf("StartTime should be zero, got %v", result.Stats.StartTime)
	}
}

func TestConvert_ToolResultsOnAgentMessage(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Edit main.go"),
			rawAgentMsgWithTools(t, "Editing.",
				[]interface{}{
					rawToolUse("edit_file", "tu-1", map[string]interface{}{"file_path": "main.go"}),
				},
				map[string]interface{}{
					"tu-1": rawToolResult("tu-1", "edit_file", "Applied edit", false),
				},
			),
		),
	)

	result, err := Convert(thread)
	if err != nil {
		t.Fatal(err)
	}

	// Should have tool_result content block from agent's tool_results
	blocks := transcript.ContentBlocks(result.Entries[1].Message)
	foundResult := false
	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID == "tu-1" {
			foundResult = true
		}
	}
	if !foundResult {
		t.Error("expected tool_result block from agent's tool_results")
	}
}
