// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"strings"
	"testing"
)

func TestExtractDialogue_NilThread(t *testing.T) {
	result := ExtractDialogue(nil)
	if result != nil {
		t.Error("expected nil for nil thread")
	}
}

func TestExtractDialogue_EmptyMessages(t *testing.T) {
	thread := parseTestThread(t, withRawMessages())
	result := ExtractDialogue(thread)
	if result != nil {
		t.Error("expected nil for empty messages")
	}
}

func TestExtractDialogue_BasicConversation(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Fix the bug in main.go"),
			rawAgentMsg(t, "I'll look at the code and fix the issue."),
			rawUserMsg(t, "Thanks, now add tests"),
			rawAgentMsg(t, "I'll add comprehensive tests for the fix."),
		),
	)

	result := ExtractDialogue(thread)
	if result == nil {
		t.Fatal("expected non-nil dialogue")
	}

	if len(result.Sections) != 2 {
		t.Fatalf("Sections = %d, want 2", len(result.Sections))
	}

	if result.Sections[0].UserRequest != "Fix the bug in main.go" {
		t.Errorf("Section[0].UserRequest = %q", result.Sections[0].UserRequest)
	}
	if result.Sections[1].UserRequest != "Thanks, now add tests" {
		t.Errorf("Section[1].UserRequest = %q", result.Sections[1].UserRequest)
	}
}

func TestExtractDialogue_ThinkingExcluded(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Explain this"),
			rawAgentMsgWithThinking(t, "Let me think about this carefully...", "Here's my explanation."),
		),
	)

	result := ExtractDialogue(thread)
	if result == nil {
		t.Fatal("expected non-nil dialogue")
	}

	// Thinking should not appear in output
	for _, sec := range result.Sections {
		for _, elem := range sec.Elements {
			if elem.Turn != nil && strings.Contains(elem.Turn.Text, "think about this carefully") {
				t.Error("thinking content should not appear in dialogue")
			}
		}
	}

	// But the text response should
	found := false
	for _, sec := range result.Sections {
		for _, elem := range sec.Elements {
			if elem.Turn != nil && elem.Turn.Text == "Here's my explanation." {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected assistant text to be present")
	}
}

func TestExtractDialogue_ToolMarkers(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Create a new file"),
			rawAgentMsgWithTools(t, "I'll create the file for you.",
				[]interface{}{
					rawToolUse("create_file", "tu-1", map[string]interface{}{"file_path": "/src/handler.go"}),
					rawToolUse("terminal", "tu-2", map[string]interface{}{"command": "go test ./..."}),
				},
				map[string]interface{}{},
			),
		),
	)

	result := ExtractDialogue(thread)
	if result == nil {
		t.Fatal("expected non-nil dialogue")
	}

	markers := 0
	for _, sec := range result.Sections {
		for _, elem := range sec.Elements {
			if elem.Marker != nil {
				markers++
			}
		}
	}

	if markers != 2 {
		t.Errorf("markers = %d, want 2", markers)
	}
}

func TestExtractDialogue_MentionsAsAtPath(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "Fix this file", "/home/user/src/main.go"),
			rawAgentMsg(t, "Done."),
		),
	)

	result := ExtractDialogue(thread)
	if result == nil {
		t.Fatal("expected non-nil dialogue")
	}

	found := false
	for _, sec := range result.Sections {
		for _, elem := range sec.Elements {
			if elem.Turn != nil && elem.Turn.Role == "user" && strings.Contains(elem.Turn.Text, "@/home/user/src/main.go") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected user turn to contain @/home/user/src/main.go mention")
	}
}

func TestExtractDialogue_FillerFilter(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "Edit the file"),
			// Short text + tool use → text should be filtered as filler
			rawAgentMsgWithTools(t, "Sure.",
				[]interface{}{
					rawToolUse("edit_file", "tu-1", map[string]interface{}{"file_path": "main.go"}),
				},
				map[string]interface{}{},
			),
		),
	)

	result := ExtractDialogue(thread)
	if result == nil {
		t.Fatal("expected non-nil dialogue")
	}

	for _, sec := range result.Sections {
		for _, elem := range sec.Elements {
			if elem.Turn != nil && elem.Turn.Role == "assistant" && elem.Turn.Text == "Sure." {
				t.Error("short filler text should be filtered when tool uses are present")
			}
		}
	}
}

func TestExtractDialogue_LongUserTextCapped(t *testing.T) {
	longText := strings.Repeat("x", 600)
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, longText),
			rawAgentMsg(t, "Got it."),
		),
	)

	result := ExtractDialogue(thread)
	if result == nil {
		t.Fatal("expected non-nil dialogue")
	}

	for _, sec := range result.Sections {
		for _, elem := range sec.Elements {
			if elem.Turn != nil && elem.Turn.Role == "user" {
				if len(elem.Turn.Text) > proseUserMaxChars+10 {
					t.Errorf("user text length = %d, should be capped around %d", len(elem.Turn.Text), proseUserMaxChars)
				}
				if !strings.HasSuffix(elem.Turn.Text, "[...]") {
					t.Error("capped user text should end with [...]")
				}
			}
		}
	}
}

func TestExtractDialogue_BashMarkers(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantNil bool
		want    string
	}{
		{"test", "go test ./...", false, "Ran tests"},
		{"commit", "git commit -m \"fix\"", false, "Committed"},
		{"push", "git push origin main", false, "Pushed to remote"},
		{"other", "ls -la", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			marker := classifyBashToolMarker(tt.cmd, false)
			if tt.wantNil {
				if marker != nil {
					t.Errorf("expected nil marker, got %q", marker.Text)
				}
				return
			}
			if marker == nil {
				t.Fatal("expected non-nil marker")
			}
			if !strings.Contains(marker.Text, tt.want) {
				t.Errorf("marker = %q, should contain %q", marker.Text, tt.want)
			}
		})
	}
}

func TestExtractDialogue_ErrorMarker(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "do something"),
			rawAgentMsgWithTools(t, "",
				[]interface{}{
					rawToolUse("diagnostics", "tu-1", nil),
				},
				map[string]interface{}{
					"tu-1": rawToolResult("tu-1", "diagnostics", "compilation error", true),
				},
			),
		),
	)

	result := ExtractDialogue(thread)
	if result == nil {
		t.Fatal("expected non-nil dialogue")
	}

	foundError := false
	for _, sec := range result.Sections {
		for _, elem := range sec.Elements {
			if elem.Marker != nil && strings.Contains(elem.Marker.Text, "Error") {
				foundError = true
			}
		}
	}
	if !foundError {
		t.Error("expected error marker for failed tool")
	}
}
