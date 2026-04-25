// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/narrative"
)

func TestExtractNarrative_NilThread(t *testing.T) {
	result := ExtractNarrative(nil)
	if result != nil {
		t.Error("expected nil for nil thread")
	}
}

func TestExtractNarrative_EmptyMessages(t *testing.T) {
	thread := parseTestThread(t, withRawMessages())
	result := ExtractNarrative(thread)
	if result != nil {
		t.Error("expected nil for empty messages")
	}
}

func TestExtractNarrative_SummaryFromDBColumn(t *testing.T) {
	thread := parseTestThread(t,
		withTitle("Build a REST API"),
		withRawMessages(rawUserMsg(t, "Build me a REST API"), rawAgentMsg(t, "Sure.")),
	)
	// detailed_summary is null, so should fall back to DB summary
	thread.Summary = "Built a REST API with auth"

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	if result.Title != "Build a REST API" {
		t.Errorf("Title = %q, want %q", result.Title, "Build a REST API")
	}
	if result.Summary != "Built a REST API with auth" {
		t.Errorf("Summary = %q, want %q", result.Summary, "Built a REST API with auth")
	}
}

func TestExtractNarrative_DetailedSummaryPreferred(t *testing.T) {
	thread := parseTestThread(t,
		withDetailedSummary("Detailed summary text here"),
		withRawMessages(rawUserMsg(t, "test"), rawAgentMsg(t, "ok")),
	)
	thread.Summary = "DB summary"

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	if result.Summary != "Detailed summary text here" {
		t.Errorf("Summary = %q, want detailed_summary value", result.Summary)
	}
}

func TestExtractNarrative_SummaryCapped(t *testing.T) {
	longSummary := strings.Repeat("a", 3000)
	thread := parseTestThread(t,
		withRawMessages(rawUserMsg(t, "test"), rawAgentMsg(t, "ok")),
	)
	thread.Summary = longSummary

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}
	if len(result.Summary) != maxSummaryLen {
		t.Errorf("Summary length = %d, want %d", len(result.Summary), maxSummaryLen)
	}
}

func TestExtractNarrative_ToolActivities(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Fix this"),
			rawAgentMsgWithTools(t, "Let me fix it.",
				[]any{
					rawToolUse("edit_file", "tu-1", map[string]any{"file_path": "/src/main.go"}),
					rawToolUse("terminal", "tu-2", map[string]any{"command": "go test ./..."}),
					rawToolUse("read_file", "tu-3", map[string]any{"file_path": "/src/util.go"}),
				},
				map[string]any{},
			),
		),
	)

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	if len(result.Segments) != 1 {
		t.Fatalf("Segments = %d, want 1", len(result.Segments))
	}

	activities := result.Segments[0].Activities
	if len(activities) != 3 {
		t.Fatalf("Activities = %d, want 3", len(activities))
	}

	// Check tool normalization
	if activities[0].Tool != "Edit" {
		t.Errorf("activities[0].Tool = %q, want %q", activities[0].Tool, "Edit")
	}
	if activities[0].Kind != narrative.KindFileModify {
		t.Errorf("activities[0].Kind = %d, want KindFileModify", activities[0].Kind)
	}

	if activities[1].Tool != "Bash" {
		t.Errorf("activities[1].Tool = %q, want %q", activities[1].Tool, "Bash")
	}
	if activities[1].Kind != narrative.KindTestRun {
		t.Errorf("activities[1].Kind = %d, want KindTestRun", activities[1].Kind)
	}

	if activities[2].Tool != "Read" {
		t.Errorf("activities[2].Tool = %q, want %q", activities[2].Tool, "Read")
	}
	if activities[2].Kind != narrative.KindExplore {
		t.Errorf("activities[2].Kind = %d, want KindExplore", activities[2].Kind)
	}
}

func TestExtractNarrative_CommitExtraction(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "commit this"),
			rawAgentMsgWithTools(t, "Committing.",
				[]any{
					rawToolUse("terminal", "tu-1", map[string]any{"command": "git commit -m \"fix bug\""}),
				},
				map[string]any{
					"tu-1": rawToolResult("tu-1", "terminal", "[main abcdef1] fix bug\n 1 file changed", false),
				},
			),
		),
	)

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	if len(result.Commits) != 1 {
		t.Fatalf("Commits = %d, want 1", len(result.Commits))
	}
	if result.Commits[0].SHA != "abcdef1" {
		t.Errorf("SHA = %q, want %q", result.Commits[0].SHA, "abcdef1")
	}
	if result.Commits[0].Message != "fix bug" {
		t.Errorf("Message = %q, want %q", result.Commits[0].Message, "fix bug")
	}
}

func TestExtractNarrative_ErrorDetection(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "edit file"),
			rawAgentMsgWithTools(t, "Editing.",
				[]any{
					rawToolUse("edit_file", "tu-1", map[string]any{"file_path": "main.go"}),
				},
				map[string]any{
					"tu-1": rawToolResult("tu-1", "edit_file", "file not found", true),
				},
			),
		),
	)

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	activities := result.Segments[0].Activities
	if len(activities) != 1 {
		t.Fatalf("Activities = %d, want 1", len(activities))
	}
	if !activities[0].IsError {
		t.Error("expected IsError=true from tool result")
	}
}

func TestExtractNarrative_GitCommitActivity(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "commit"),
			rawAgentMsgWithTools(t, "",
				[]any{
					rawToolUse("terminal", "tu-1", map[string]any{"command": "git commit -m \"add feature\""}),
				},
				map[string]any{},
			),
		),
	)

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	activities := result.Segments[0].Activities
	found := false
	for _, a := range activities {
		if a.Kind == narrative.KindGitCommit {
			found = true
			if !strings.Contains(a.Description, "add feature") {
				t.Errorf("commit description should contain message, got %q", a.Description)
			}
		}
	}
	if !found {
		t.Error("expected KindGitCommit activity")
	}
}

func TestExtractNarrative_TagInference(t *testing.T) {
	tests := []struct {
		name     string
		tools    []any
		expected string
	}{
		{
			"build heavy",
			[]any{
				rawToolUse("create_file", "1", map[string]any{"file_path": "a.go"}),
				rawToolUse("create_file", "2", map[string]any{"file_path": "b.go"}),
			},
			"build",
		},
		{
			"explore heavy",
			[]any{
				rawToolUse("read_file", "1", nil),
				rawToolUse("grep", "2", nil),
				rawToolUse("find_path", "3", nil),
			},
			"explore",
		},
		{
			"test heavy",
			[]any{
				rawToolUse("terminal", "1", map[string]any{"command": "go test ./..."}),
				rawToolUse("edit_file", "2", map[string]any{"file_path": "x.go"}),
				rawToolUse("terminal", "3", map[string]any{"command": "make test"}),
				rawToolUse("terminal", "4", map[string]any{"command": "go test -run TestX"}),
			},
			"test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := parseTestThread(t,
				withRawMessages(
					rawUserMsg(t, "do stuff"),
					rawAgentMsgWithTools(t, "", tt.tools, map[string]any{}),
				),
			)
			result := ExtractNarrative(thread)
			if result == nil {
				t.Fatal("expected non-nil narrative")
			}
			if result.Tag != tt.expected {
				t.Errorf("Tag = %q, want %q", result.Tag, tt.expected)
			}
		})
	}
}

func TestExtractNarrative_FirstUserRequest(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Fix the authentication bug in the login handler"),
			rawAgentMsg(t, "I'll look into it."),
		),
	)

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	if result.Segments[0].UserRequest != "Fix the authentication bug in the login handler" {
		t.Errorf("UserRequest = %q", result.Segments[0].UserRequest)
	}
}

func TestExtractNarrative_WorkPerformed(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "test"),
			rawAgentMsgWithTools(t, "",
				[]any{
					rawToolUse("edit_file", "tu-1", map[string]any{"file_path": "/src/main.go"}),
				},
				map[string]any{},
			),
		),
	)

	result := ExtractNarrative(thread)
	if result == nil {
		t.Fatal("expected non-nil narrative")
	}

	if result.WorkPerformed == "" {
		t.Error("WorkPerformed should not be empty")
	}
}

func TestParseCommitOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSHA string
		wantMsg string
	}{
		{"valid", "[main abcdef1] fix bug", "abcdef1", "fix bug"},
		{"no brackets", "committed something", "", ""},
		{"short sha", "[main abc] fix", "", ""},
		{"no message", "[main abcdef1]", "abcdef1", ""},
		{"non-hex sha", "[main zzzzzzz] fix", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sha, msg := parseCommitOutput(tt.input)
			if sha != tt.wantSHA {
				t.Errorf("SHA = %q, want %q", sha, tt.wantSHA)
			}
			if msg != tt.wantMsg {
				t.Errorf("Msg = %q, want %q", msg, tt.wantMsg)
			}
		})
	}
}
