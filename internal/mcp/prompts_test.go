// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"strings"
	"testing"
)

func TestSessionGuidelinesPrompt_NoProject(t *testing.T) {
	prompt := NewSessionGuidelinesPrompt()
	result, err := prompt.Handler(nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	msg := result.Messages[0]
	if msg.Role != "user" {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if msg.Content.Type != "text" {
		t.Errorf("content type = %q, want text", msg.Content.Type)
	}
	if !strings.Contains(msg.Content.Text, "vv_capture_session") {
		t.Error("prompt text should mention vv_capture_session tool")
	}
	if strings.Contains(msg.Content.Text, "Project context") {
		t.Error("prompt text should not contain project context when no project given")
	}
}

func TestSessionGuidelinesPrompt_WithProject(t *testing.T) {
	prompt := NewSessionGuidelinesPrompt()
	result, err := prompt.Handler(map[string]string{"project": "myproject"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	text := result.Messages[0].Content.Text
	if !strings.Contains(text, "vv_capture_session") {
		t.Error("prompt text should mention vv_capture_session tool")
	}
	if !strings.Contains(text, "myproject") {
		t.Error("prompt text should mention the project name")
	}
	if !strings.Contains(text, "Project context") {
		t.Error("prompt text should contain project context section")
	}
}

func TestSessionGuidelinesPrompt_Description(t *testing.T) {
	prompt := NewSessionGuidelinesPrompt()
	if prompt.Definition.Name != "vv_session_guidelines" {
		t.Errorf("name = %q, want vv_session_guidelines", prompt.Definition.Name)
	}
	if prompt.Definition.Description == "" {
		t.Error("description should not be empty")
	}
	if len(prompt.Definition.Arguments) != 1 {
		t.Fatalf("expected 1 argument, got %d", len(prompt.Definition.Arguments))
	}
	if prompt.Definition.Arguments[0].Name != "project" {
		t.Errorf("argument name = %q, want project", prompt.Definition.Arguments[0].Name)
	}
}
