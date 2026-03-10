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

func TestDetectProject_NilSnapshot_NoMentions(t *testing.T) {
	thread := parseTestThread(t, withRawMessages(rawUserMsg(t, "test")))

	info := DetectProject(thread, testConfig())

	if info.Project != "zed" {
		t.Errorf("Project = %q, want %q", info.Project, "zed")
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

	if info.Project != "zed" {
		t.Errorf("Project = %q, want %q", info.Project, "zed")
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

// --- Path inference tests ---

func TestDetectProject_InferFromMentions(t *testing.T) {
	// Two file mentions from the same project → infer project name
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "read this", "/home/user/code/proteus/proteus-rs/src/main.rs"),
			rawUserMsgWithMention(t, "and this", "/home/user/code/proteus/proteus-hvpc/doc/README.md"),
		),
	)

	info := DetectProject(thread, testConfig())

	if info.Project != "proteus" {
		t.Errorf("Project = %q, want %q", info.Project, "proteus")
	}
	if info.CWD != "/home/user/code/proteus" {
		t.Errorf("CWD = %q, want %q", info.CWD, "/home/user/code/proteus")
	}
}

func TestDetectProject_InferFromToolPaths(t *testing.T) {
	// Agent reads files from different subdirs of a project — no mentions from user
	readTool := rawToolUse("read_file", "tool-1", map[string]interface{}{
		"file_path": "/home/user/code/vibe-vault/internal/zed/detect.go",
	})
	editTool := rawToolUse("edit_file", "tool-2", map[string]interface{}{
		"file_path": "/home/user/code/vibe-vault/cmd/vv/main.go",
	})
	agentMsg := rawAgentMsgWithTools(t, "Let me check", []interface{}{readTool, editTool}, map[string]interface{}{})

	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsg(t, "fix the bug"),
			agentMsg,
		),
	)

	info := DetectProject(thread, testConfig())

	if info.Project != "vibe-vault" {
		t.Errorf("Project = %q, want %q", info.Project, "vibe-vault")
	}
}

func TestDetectProject_MentionsPlusToolPaths(t *testing.T) {
	// Mentions and tool paths from same project → combined pool
	readTool := rawToolUse("read_file", "tool-1", map[string]interface{}{
		"file_path": "/home/user/code/myapp/pkg/server.go",
	})
	agentMsg := rawAgentMsgWithTools(t, "reading", []interface{}{readTool}, map[string]interface{}{})

	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "check this", "/home/user/code/myapp/cmd/main.go"),
			agentMsg,
		),
	)

	info := DetectProject(thread, testConfig())

	if info.Project != "myapp" {
		t.Errorf("Project = %q, want %q", info.Project, "myapp")
	}
}

func TestDetectProject_SinglePathFallsToZed(t *testing.T) {
	// Single mention — unreliable, should not guess
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "read", "/home/user/code/proteus/proteus-rs/src/main.rs"),
		),
	)

	info := DetectProject(thread, testConfig())

	if info.Project != "zed" {
		t.Errorf("Project = %q, want %q (single path should fall to zed)", info.Project, "zed")
	}
}

func TestDetectProject_DivergentPathsFallToZed(t *testing.T) {
	// Paths from two unrelated projects → common root too shallow → "zed"
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "this", "/home/user/code/proteus/src/main.rs"),
			rawUserMsgWithMention(t, "that", "/home/user/code/vibe-vault/cmd/main.go"),
		),
	)

	info := DetectProject(thread, testConfig())

	if info.Project != "zed" {
		t.Errorf("Project = %q, want %q (divergent paths should fall to zed)", info.Project, "zed")
	}
}

func TestDetectProject_SnapshotTakesPrecedence(t *testing.T) {
	// Snapshot has a valid worktree path — should use that, not mentions
	thread := parseTestThread(t,
		withSnapshot("/home/user/work/real-project", "main", ""),
		withRawMessages(
			rawUserMsgWithMention(t, "read", "/home/user/work/real-project/src/lib.rs"),
			rawUserMsgWithMention(t, "also", "/home/user/work/real-project/tests/test.rs"),
		),
	)

	info := DetectProject(thread, testConfig())

	if info.Project != "real-project" {
		t.Errorf("Project = %q, want %q", info.Project, "real-project")
	}
}

// --- collectAbsolutePaths tests ---

func TestCollectAbsolutePaths_Mentions(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "a", "/home/user/code/proj/src/a.go"),
			rawUserMsgWithMention(t, "b", "/home/user/code/proj/src/b.go"),
		),
	)

	paths := collectAbsolutePaths(thread)

	if len(paths) != 2 {
		t.Fatalf("got %d paths, want 2", len(paths))
	}
}

func TestCollectAbsolutePaths_FiltersSystemPaths(t *testing.T) {
	readTmp := rawToolUse("read_file", "t1", map[string]interface{}{
		"file_path": "/tmp/scratch.txt",
	})
	readReal := rawToolUse("read_file", "t2", map[string]interface{}{
		"file_path": "/home/user/code/proj/main.go",
	})
	agentMsg := rawAgentMsgWithTools(t, "", []interface{}{readTmp, readReal}, map[string]interface{}{})

	thread := parseTestThread(t, withRawMessages(rawUserMsg(t, "go"), agentMsg))

	paths := collectAbsolutePaths(thread)

	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1 (should filter /tmp)", len(paths))
	}
	if paths[0] != "/home/user/code/proj/main.go" {
		t.Errorf("path = %q, want /home/user/code/proj/main.go", paths[0])
	}
}

func TestCollectAbsolutePaths_SkipsRelativePaths(t *testing.T) {
	readRel := rawToolUse("read_file", "t1", map[string]interface{}{
		"file_path": "src/main.go",
	})
	agentMsg := rawAgentMsgWithTools(t, "", []interface{}{readRel}, map[string]interface{}{})

	thread := parseTestThread(t, withRawMessages(rawUserMsg(t, "go"), agentMsg))

	paths := collectAbsolutePaths(thread)

	if len(paths) != 0 {
		t.Fatalf("got %d paths, want 0 (should skip relative paths)", len(paths))
	}
}

func TestCollectAbsolutePaths_Deduplicates(t *testing.T) {
	thread := parseTestThread(t,
		withRawMessages(
			rawUserMsgWithMention(t, "a", "/home/user/code/proj/main.go"),
			rawUserMsgWithMention(t, "b", "/home/user/code/proj/main.go"),
		),
	)

	paths := collectAbsolutePaths(thread)

	if len(paths) != 1 {
		t.Fatalf("got %d paths, want 1 (should deduplicate)", len(paths))
	}
}

// --- commonProjectRoot tests ---

func TestCommonProjectRoot_MultiplePathsSameProject(t *testing.T) {
	paths := []string{
		"/home/user/code/vibe-vault/internal/zed/detect.go",
		"/home/user/code/vibe-vault/internal/session/capture.go",
		"/home/user/code/vibe-vault/cmd/vv/main.go",
	}

	project, dir := commonProjectRoot(paths)

	if project != "vibe-vault" {
		t.Errorf("project = %q, want %q", project, "vibe-vault")
	}
	if dir != "/home/user/code/vibe-vault" {
		t.Errorf("dir = %q, want %q", dir, "/home/user/code/vibe-vault")
	}
}

func TestCommonProjectRoot_MultiWorktree(t *testing.T) {
	paths := []string{
		"/home/user/code/proteus/proteus-rs/src/main.rs",
		"/home/user/code/proteus/proteus-hvpc-support/doc/arch.md",
		"/home/user/code/proteus/proteus-webui/index.html",
	}

	project, dir := commonProjectRoot(paths)

	if project != "proteus" {
		t.Errorf("project = %q, want %q", project, "proteus")
	}
	if dir != "/home/user/code/proteus" {
		t.Errorf("dir = %q, want %q", dir, "/home/user/code/proteus")
	}
}

func TestCommonProjectRoot_SinglePath(t *testing.T) {
	paths := []string{"/home/user/code/proj/src/main.go"}

	project, _ := commonProjectRoot(paths)

	if project != "" {
		t.Errorf("project = %q, want empty (single path is unreliable)", project)
	}
}

func TestCommonProjectRoot_Empty(t *testing.T) {
	project, _ := commonProjectRoot(nil)

	if project != "" {
		t.Errorf("project = %q, want empty", project)
	}
}

func TestCommonProjectRoot_DivergentProjects(t *testing.T) {
	paths := []string{
		"/home/user/code/projectA/src/main.go",
		"/home/user/code/projectB/src/main.go",
	}

	project, _ := commonProjectRoot(paths)

	// Common root is /home/user/code — only 1 segment below $HOME, too shallow
	if project != "" {
		t.Errorf("project = %q, want empty (divergent paths)", project)
	}
}

func TestCommonProjectRoot_TrivialRoot(t *testing.T) {
	paths := []string{
		"/home/user/a.txt",
		"/home/user/b.txt",
	}

	project, _ := commonProjectRoot(paths)

	if project != "" {
		t.Errorf("project = %q, want empty (trivial root)", project)
	}
}

func TestCommonProjectRoot_DeepNesting(t *testing.T) {
	paths := []string{
		"/home/user/code/org/repo/packages/core/src/index.ts",
		"/home/user/code/org/repo/packages/web/src/app.tsx",
	}

	project, dir := commonProjectRoot(paths)

	// Common root is /home/user/code/org/repo/packages — deep enough
	if project != "packages" {
		t.Errorf("project = %q, want %q", project, "packages")
	}
	if dir != "/home/user/code/org/repo/packages" {
		t.Errorf("dir = %q, want %q", dir, "/home/user/code/org/repo/packages")
	}
}

// --- isSystemPath tests ---

func TestIsSystemPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/tmp/file.txt", true},
		{"/etc/config", true},
		{"/dev/null", true},
		{"/proc/1/status", true},
		{"/sys/class", true},
		{"/var/log/syslog", true},
		{"/home/user/code/proj/main.go", false},
		{"/opt/tools/bin", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := isSystemPath(tt.path); got != tt.want {
				t.Errorf("isSystemPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
