// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"testing"

	"github.com/johns/vibe-vault/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		Domains: config.DomainsConfig{
			Work:       "/home/user/work",
			Personal:   "/home/user/personal",
			Opensource: "/home/user/opensource",
		},
	}
}

func TestDetectProject_ValidSnapshot(t *testing.T) {
	thread := parseTestThread(t,
		withModel("anthropic", "claude-sonnet-4-5-20250514"),
		withSnapshot("/home/user/work/my-project", "feature-x", ""),
		withRawMessages(rawUserMsg(t, "test")),
	)

	info := DetectProject(thread, testConfig())

	if info.Project != "my-project" {
		t.Errorf("Project = %q, want %q", info.Project, "my-project")
	}
	if info.Branch != "feature-x" {
		t.Errorf("Branch = %q, want %q", info.Branch, "feature-x")
	}
	if info.SessionID != "zed:test-id" {
		t.Errorf("SessionID = %q, want %q", info.SessionID, "zed:test-id")
	}
	if info.Model != "anthropic/claude-sonnet-4-5-20250514" {
		t.Errorf("Model = %q, want %q", info.Model, "anthropic/claude-sonnet-4-5-20250514")
	}
	if info.CWD != "/home/user/work/my-project" {
		t.Errorf("CWD = %q, want %q", info.CWD, "/home/user/work/my-project")
	}
	if info.Domain != "work" {
		t.Errorf("Domain = %q, want %q", info.Domain, "work")
	}
}

func TestDetectProject_NilSnapshot(t *testing.T) {
	thread := parseTestThread(t, withRawMessages(rawUserMsg(t, "test")))

	info := DetectProject(thread, testConfig())

	if info.Project != "_unknown" {
		t.Errorf("Project = %q, want %q", info.Project, "_unknown")
	}
	if info.CWD != "" {
		t.Errorf("CWD = %q, want empty", info.CWD)
	}
}

func TestDetectProject_BranchFallback(t *testing.T) {
	thread := parseTestThread(t,
		withSnapshot("/home/user/project", "", ""), // no branch in snapshot
		withRawMessages(rawUserMsg(t, "test")),
	)
	thread.WorktreeBranch = "fallback-branch"

	info := DetectProject(thread, testConfig())

	if info.Branch != "fallback-branch" {
		t.Errorf("Branch = %q, want %q", info.Branch, "fallback-branch")
	}
}

func TestDetectProject_DomainDetection(t *testing.T) {
	tests := []struct {
		name     string
		cwd      string
		expected string
	}{
		{"work", "/home/user/work/project", "work"},
		{"personal", "/home/user/personal/stuff", "personal"},
		{"opensource", "/home/user/opensource/repo", "opensource"},
		{"unknown", "/tmp/random", "personal"},
		{"empty", "", "personal"},
	}

	cfg := testConfig()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thread := parseTestThread(t,
				withSnapshot(tt.cwd, "", ""),
				withRawMessages(rawUserMsg(t, "test")),
			)

			info := DetectProject(thread, cfg)
			if info.Domain != tt.expected {
				t.Errorf("Domain = %q, want %q", info.Domain, tt.expected)
			}
		})
	}
}

func TestDetectProject_EmptyWorktrees(t *testing.T) {
	thread := parseTestThread(t, withRawMessages(rawUserMsg(t, "test")))
	thread.ProjectSnapshot = &ProjectSnapshot{WorktreeSnapshots: nil}

	info := DetectProject(thread, testConfig())

	if info.Project != "_unknown" {
		t.Errorf("Project = %q, want %q", info.Project, "_unknown")
	}
}

func TestDetectProject_SnapshotBranchPrecedence(t *testing.T) {
	thread := parseTestThread(t,
		withSnapshot("/home/user/project", "snapshot-branch", ""),
		withRawMessages(rawUserMsg(t, "test")),
	)
	thread.WorktreeBranch = "db-branch" // should be overridden by snapshot

	info := DetectProject(thread, testConfig())

	if info.Branch != "snapshot-branch" {
		t.Errorf("Branch = %q, want %q (snapshot should take precedence)", info.Branch, "snapshot-branch")
	}
}
