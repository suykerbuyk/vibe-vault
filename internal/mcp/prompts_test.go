// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/templates"
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

func TestSessionGuidelinesPrompt_MidSessionSafety(t *testing.T) {
	prompt := NewSessionGuidelinesPrompt()
	result, err := prompt.Handler(nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	text := result.Messages[0].Content.Text
	if !strings.Contains(text, "Do NOT call vv_capture_session on this") {
		t.Error("prompt should explicitly defer vv_capture_session on mid-session invocation")
	}
	if !strings.Contains(text, "user has confirmed they are done") {
		t.Error("prompt should require user confirmation before capture")
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

func TestRestartPrompt_Definition(t *testing.T) {
	p := NewRestartPrompt()
	if p.Definition.Name != "vv_restart" {
		t.Errorf("name = %q, want vv_restart", p.Definition.Name)
	}
	if p.Definition.Description == "" {
		t.Error("description should not be empty")
	}
	if len(p.Definition.Arguments) != 0 {
		t.Errorf("expected 0 arguments, got %d", len(p.Definition.Arguments))
	}
}

func TestRestartPrompt_BodyMatchesTemplate(t *testing.T) {
	p := NewRestartPrompt()
	result, err := p.Handler(nil)
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

	want, ok := templates.New().DefaultContent("agentctx/commands/restart.md")
	if !ok {
		t.Fatal("template lookup failed: agentctx/commands/restart.md")
	}
	if msg.Content.Text != string(want) {
		t.Errorf("prompt body diverges from template content (len got=%d, want=%d)",
			len(msg.Content.Text), len(want))
	}
}

func TestWrapPrompt_Definition(t *testing.T) {
	p := NewWrapPrompt()
	if p.Definition.Name != "vv_wrap" {
		t.Errorf("name = %q, want vv_wrap", p.Definition.Name)
	}
	if p.Definition.Description == "" {
		t.Error("description should not be empty")
	}
	if len(p.Definition.Arguments) != 0 {
		t.Errorf("expected 0 arguments, got %d", len(p.Definition.Arguments))
	}
}

func TestWrapPrompt_BodyMatchesTemplate(t *testing.T) {
	p := NewWrapPrompt()
	result, err := p.Handler(nil)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	want, ok := templates.New().DefaultContent("agentctx/commands/wrap.md")
	if !ok {
		t.Fatal("template lookup failed: agentctx/commands/wrap.md")
	}
	if result.Messages[0].Content.Text != string(want) {
		t.Errorf("prompt body diverges from template content (len got=%d, want=%d)",
			len(result.Messages[0].Content.Text), len(want))
	}
}

func TestTemplatePrompt_NotFound(t *testing.T) {
	p := templatePrompt("vv_bogus", "agentctx/commands/does-not-exist.md", "bogus")
	_, err := p.Handler(nil)
	if err == nil {
		t.Fatal("expected error for missing template, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist.md") {
		t.Errorf("error should name the missing path, got %q", err.Error())
	}
}

func TestRegisterAllTools_PromptCompleteness(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	srv := NewServer(ServerInfo{Name: "test", Version: "0.0.0"}, logger)
	srv.RegisterPrompt(NewSessionGuidelinesPrompt())
	srv.RegisterPrompt(NewRestartPrompt())
	srv.RegisterPrompt(NewWrapPrompt())

	want := []string{"vv_session_guidelines", "vv_restart", "vv_wrap"}
	for _, name := range want {
		if _, ok := srv.prompts[name]; !ok {
			t.Errorf("expected prompt %q to be registered", name)
		}
	}
	if len(srv.prompts) != len(want) {
		t.Errorf("registered %d prompts, want %d", len(srv.prompts), len(want))
	}
}
