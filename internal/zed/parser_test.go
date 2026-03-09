// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"testing"
	"time"
)

func TestParseThread_Valid(t *testing.T) {
	data := makeThreadJSON(t,
		withTitle("Test Title"),
		withModel("anthropic", "claude-sonnet-4-5-20250514"),
		withRawMessages(
			rawUserMsg(t, "Hello"),
			rawAgentMsg(t, "Hi there!"),
		),
	)

	result, err := ParseThread("test-id", "Test summary", "2026-03-08T12:00:00Z", "", "", data)
	if err != nil {
		t.Fatal(err)
	}

	if result.ID != "test-id" {
		t.Errorf("ID = %q, want %q", result.ID, "test-id")
	}
	if result.Summary != "Test summary" {
		t.Errorf("Summary = %q, want %q", result.Summary, "Test summary")
	}
	if result.Title != "Test Title" {
		t.Errorf("Title = %q, want %q", result.Title, "Test Title")
	}
	if result.Model.Provider != "anthropic" {
		t.Errorf("Model.Provider = %q, want %q", result.Model.Provider, "anthropic")
	}
	if result.Model.Model != "claude-sonnet-4-5-20250514" {
		t.Errorf("Model.Model = %q, want %q", result.Model.Model, "claude-sonnet-4-5-20250514")
	}
	if len(result.Messages) != 2 {
		t.Errorf("Messages count = %d, want 2", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("Messages[0].Role = %q, want %q", result.Messages[0].Role, "user")
	}
	if result.Messages[1].Role != "assistant" {
		t.Errorf("Messages[1].Role = %q, want %q", result.Messages[1].Role, "assistant")
	}
	if result.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
	if result.Version != "0.3.0" {
		t.Errorf("Version = %q, want %q", result.Version, "0.3.0")
	}
}

func TestParseThread_CorruptData(t *testing.T) {
	_, err := ParseThread("bad-id", "", "", "", "", []byte("not valid zstd"))
	if err == nil {
		t.Error("expected error for corrupt data")
	}
}

func TestParseThread_InvalidJSON(t *testing.T) {
	data := compressJSONRaw(t, []byte(`{invalid json`))
	_, err := ParseThread("bad-json", "", "", "", "", data)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseDB_ValidThread(t *testing.T) {
	data := makeThreadJSON(t,
		withTitle("DB Thread"),
		withRawMessages(rawUserMsg(t, "test")),
	)

	dbPath := makeTestDB(t, testRow{
		ID:        "thread-1",
		Summary:   "A test thread",
		UpdatedAt: "2026-03-08T12:00:00Z",
		Data:      data,
	})

	threads, err := ParseDB(dbPath, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(threads))
	}
	if threads[0].ID != "thread-1" {
		t.Errorf("ID = %q, want %q", threads[0].ID, "thread-1")
	}
	if threads[0].Title != "DB Thread" {
		t.Errorf("Title = %q, want %q", threads[0].Title, "DB Thread")
	}
}

func TestParseDB_Empty(t *testing.T) {
	dbPath := makeTestDB(t)

	threads, err := ParseDB(dbPath, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Errorf("got %d threads, want 0", len(threads))
	}
}

func TestParseDB_FilterSince(t *testing.T) {
	data := makeThreadJSON(t, withTitle("Recent"))

	dbPath := makeTestDB(t,
		testRow{ID: "old", Summary: "old", UpdatedAt: "2026-01-01T00:00:00Z", Data: data},
		testRow{ID: "new", Summary: "new", UpdatedAt: "2026-03-08T12:00:00Z", Data: data},
	)

	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	threads, err := ParseDB(dbPath, ParseOpts{Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(threads))
	}
	if threads[0].ID != "new" {
		t.Errorf("ID = %q, want %q", threads[0].ID, "new")
	}
}

func TestParseDB_Limit(t *testing.T) {
	data := makeThreadJSON(t)

	dbPath := makeTestDB(t,
		testRow{ID: "t1", Summary: "t1", UpdatedAt: "2026-03-08T12:00:00Z", Data: data},
		testRow{ID: "t2", Summary: "t2", UpdatedAt: "2026-03-07T12:00:00Z", Data: data},
		testRow{ID: "t3", Summary: "t3", UpdatedAt: "2026-03-06T12:00:00Z", Data: data},
	)

	threads, err := ParseDB(dbPath, ParseOpts{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 2 {
		t.Fatalf("got %d threads, want 2", len(threads))
	}
}

func TestParseDB_SkipsCorruptThread(t *testing.T) {
	goodData := makeThreadJSON(t, withTitle("Good"))

	dbPath := makeTestDB(t,
		testRow{ID: "good", Summary: "good", UpdatedAt: "2026-03-08T12:00:00Z", Data: goodData},
		testRow{ID: "bad", Summary: "bad", UpdatedAt: "2026-03-07T12:00:00Z", Data: []byte("corrupt")},
	)

	threads, err := ParseDB(dbPath, ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("got %d threads, want 1", len(threads))
	}
	if threads[0].ID != "good" {
		t.Errorf("ID = %q, want %q", threads[0].ID, "good")
	}
}

func TestParseDB_NonexistentDB(t *testing.T) {
	_, err := ParseDB("/nonexistent/threads.db", ParseOpts{})
	if err == nil {
		t.Error("expected error for nonexistent DB")
	}
}

func TestParseThread_UpdatedAtParsing(t *testing.T) {
	data := makeThreadJSON(t)

	// RFC3339
	result, err := ParseThread("id", "", "2026-03-08T12:30:00Z", "", "", data)
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdatedAt.Hour() != 12 || result.UpdatedAt.Minute() != 30 {
		t.Errorf("UpdatedAt = %v, expected 12:30", result.UpdatedAt)
	}

	// RFC3339Nano (Zed's actual format)
	result, err = ParseThread("id2", "", "2026-03-08T12:30:00.123456789Z", "", "", data)
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdatedAt.Hour() != 12 || result.UpdatedAt.Minute() != 30 {
		t.Errorf("UpdatedAt = %v, expected 12:30 for nano format", result.UpdatedAt)
	}

	// Empty timestamp
	result, err = ParseThread("id3", "", "", "", "", data)
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt should be zero for empty string, got %v", result.UpdatedAt)
	}
}

func TestParseThread_MessageEnumParsing(t *testing.T) {
	data := makeThreadJSON(t,
		withRawMessages(
			rawUserMsg(t, "Hello"),
			rawAgentMsg(t, "Hi"),
			rawAgentMsgWithThinking(t, "thinking...", "response"),
		),
	)

	result, err := ParseThread("enum-test", "", "", "", "", data)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Messages) != 3 {
		t.Fatalf("Messages = %d, want 3", len(result.Messages))
	}

	// User message
	if result.Messages[0].Role != "user" {
		t.Errorf("msg[0].Role = %q, want user", result.Messages[0].Role)
	}
	if len(result.Messages[0].Content) != 1 || result.Messages[0].Content[0].Type != "text" {
		t.Error("msg[0] should have 1 text content block")
	}

	// Agent with thinking
	if result.Messages[2].Role != "assistant" {
		t.Errorf("msg[2].Role = %q, want assistant", result.Messages[2].Role)
	}
	hasThinking := false
	for _, c := range result.Messages[2].Content {
		if c.Type == "thinking" {
			hasThinking = true
			if c.Thinking != "thinking..." {
				t.Errorf("thinking text = %q", c.Thinking)
			}
		}
	}
	if !hasThinking {
		t.Error("msg[2] should have a thinking block")
	}
}

func TestParseThread_ToolUseAndResults(t *testing.T) {
	data := makeThreadJSON(t,
		withRawMessages(
			rawUserMsg(t, "Fix this"),
			rawAgentMsgWithTools(t, "Fixing.",
				[]interface{}{
					rawToolUse("edit_file", "tu-1", map[string]interface{}{"file_path": "main.go"}),
				},
				map[string]interface{}{
					"tu-1": rawToolResult("tu-1", "edit_file", "Edit applied", false),
				},
			),
		),
	)

	result, err := ParseThread("tool-test", "", "", "", "", data)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2", len(result.Messages))
	}

	agent := result.Messages[1]
	if agent.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", agent.Role)
	}

	// Check tool_use content
	foundToolUse := false
	for _, c := range agent.Content {
		if c.Type == "tool_use" && c.ToolName == "edit_file" && c.ToolID == "tu-1" {
			foundToolUse = true
		}
	}
	if !foundToolUse {
		t.Error("expected tool_use content block")
	}

	// Check tool results
	tr, ok := agent.ToolResults["tu-1"]
	if !ok {
		t.Fatal("expected tool result for tu-1")
	}
	if tr.ToolName != "edit_file" {
		t.Errorf("ToolName = %q, want edit_file", tr.ToolName)
	}
	if tr.IsError {
		t.Error("expected no error")
	}
}

func TestParseThread_ResumeMarkerSkipped(t *testing.T) {
	// Zed can have string markers like "Resume" in the messages array
	data := compressJSON(t, map[string]interface{}{
		"title":   "Test",
		"model":   map[string]string{"provider": "anthropic", "model": "claude-sonnet-4-5-20250514"},
		"version": "0.3.0",
		"messages": []interface{}{
			"Resume", // string marker
			map[string]interface{}{
				"User": map[string]interface{}{
					"id":      "u1",
					"content": []interface{}{map[string]string{"Text": "Hello"}},
				},
			},
		},
		"request_token_usage": map[string]interface{}{},
	})

	result, err := ParseThread("resume-test", "", "", "", "", data)
	if err != nil {
		t.Fatal(err)
	}

	// "Resume" string should be skipped, only the User message remains
	if len(result.Messages) != 1 {
		t.Fatalf("Messages = %d, want 1", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("Role = %q, want user", result.Messages[0].Role)
	}
}
