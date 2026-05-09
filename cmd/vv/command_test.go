// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/templates"
)

func TestRunCommandGet_HappyPath(t *testing.T) {
	var buf bytes.Buffer
	if err := runCommand([]string{"get", "restart"}, &buf); err != nil {
		t.Fatalf("runCommand: %v", err)
	}

	got := buf.String()
	reg := templates.New()
	want, ok := reg.DefaultContent("agentctx/commands/restart.md")
	if !ok {
		t.Fatalf("templates.DefaultContent(agentctx/commands/restart.md) returned !ok — embedded templates regression")
	}
	if got != string(want) {
		t.Errorf("runCommand stdout != embedded restart.md body\n--- got %d bytes ---\n%s\n--- want %d bytes ---\n%s",
			len(got), got[:min(len(got), 200)], len(want), string(want[:min(len(want), 200)]))
	}
}

func TestRunCommandGet_AllKnownCommands(t *testing.T) {
	// Every .md under templates/agentctx/commands/ must be retrievable
	// via `vv command get <basename>`. This locks the slug → relPath
	// mapping and catches regressions if a command is renamed.
	cases := []string{
		"cancel-plan", "execute-plan", "features-split",
		"license", "makefile", "restart", "review-plan", "wrap",
	}
	reg := templates.New()
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := runCommand([]string{"get", name}, &buf); err != nil {
				t.Fatalf("runCommand get %s: %v", name, err)
			}
			want, ok := reg.DefaultContent("agentctx/commands/" + name + ".md")
			if !ok {
				t.Fatalf("templates.DefaultContent missing agentctx/commands/%s.md", name)
			}
			if buf.String() != string(want) {
				t.Errorf("runCommand get %s body mismatch", name)
			}
			if buf.Len() == 0 {
				t.Errorf("runCommand get %s wrote empty body", name)
			}
		})
	}
}

func TestRunCommandGet_MissingCommand(t *testing.T) {
	var buf bytes.Buffer
	err := runCommand([]string{"get", "this-does-not-exist"}, &buf)
	if err == nil {
		t.Fatal("runCommand: expected error for unknown command, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error message %q should mention 'unknown command'", err.Error())
	}
	if buf.Len() != 0 {
		t.Errorf("stdout should be empty on error, got %d bytes", buf.Len())
	}
}

func TestRunCommand_NoArgs(t *testing.T) {
	var buf bytes.Buffer
	err := runCommand(nil, &buf)
	if err == nil {
		t.Fatal("runCommand: expected error for no args, got nil")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("error message %q should mention 'usage:'", err.Error())
	}
}

func TestRunCommand_GetWithNoName(t *testing.T) {
	var buf bytes.Buffer
	err := runCommand([]string{"get"}, &buf)
	if err == nil {
		t.Fatal("runCommand: expected error for `get` with no name, got nil")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("error message %q should mention 'usage:'", err.Error())
	}
}

func TestRunCommand_UnknownSubcommand(t *testing.T) {
	var buf bytes.Buffer
	err := runCommand([]string{"bogus", "thing"}, &buf)
	if err == nil {
		t.Fatal("runCommand: expected error for unknown subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown subcommand") {
		t.Errorf("error message %q should mention 'unknown subcommand'", err.Error())
	}
	if !strings.Contains(err.Error(), "expected: get") {
		t.Errorf("error message %q should suggest 'get'", err.Error())
	}
}
