// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import (
	"strings"
	"testing"
)

func TestParsePorcelain_SingleLockedBlock(t *testing.T) {
	in := "worktree /repo/.claude/worktrees/agent-aaaaaaaaaaaaaaaa\n" +
		"HEAD 0123456789abcdef0123456789abcdef01234567\n" +
		"branch refs/heads/worktree-agent-aaaaaaaaaaaaaaaa\n" +
		"locked claude agent agent-aaaaaaaaaaaaaaaa (pid 1234)\n" +
		"\n"

	blocks, err := parsePorcelain(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d: %+v", len(blocks), blocks)
	}
	b := blocks[0]
	if b.Worktree != "/repo/.claude/worktrees/agent-aaaaaaaaaaaaaaaa" {
		t.Errorf("Worktree = %q", b.Worktree)
	}
	if b.HEAD != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("HEAD = %q", b.HEAD)
	}
	if b.Branch != "worktree-agent-aaaaaaaaaaaaaaaa" {
		t.Errorf("Branch = %q (refs/heads/ should be stripped)", b.Branch)
	}
	if b.Locked != "claude agent agent-aaaaaaaaaaaaaaaa (pid 1234)" {
		t.Errorf("Locked = %q", b.Locked)
	}
}

func TestParsePorcelain_MainAndLockedBlocks(t *testing.T) {
	in := "worktree /repo\n" +
		"HEAD aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /repo/.claude/worktrees/agent-aaaaaaaaaaaaaaaa\n" +
		"HEAD bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
		"branch refs/heads/worktree-agent-aaaaaaaaaaaaaaaa\n" +
		"locked claude agent agent-aaaaaaaaaaaaaaaa (pid 1234)\n" +
		"\n"

	blocks, err := parsePorcelain(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Locked != "" {
		t.Errorf("main block Locked = %q, want empty", blocks[0].Locked)
	}
	if blocks[1].Locked == "" {
		t.Errorf("subagent block Locked is empty")
	}
}

func TestParsePorcelain_StripsBranchPrefix(t *testing.T) {
	in := "worktree /x\nHEAD a\nbranch refs/heads/foo\n\n"
	blocks, err := parsePorcelain(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Branch != "foo" {
		t.Fatalf("Branch = %q", blocks[0].Branch)
	}
}

func TestParsePorcelain_EmptyInput(t *testing.T) {
	blocks, err := parsePorcelain(strings.NewReader(""))
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("want 0 blocks, got %d", len(blocks))
	}
}

func TestParsePorcelain_TrailingBlankLines(t *testing.T) {
	in := "worktree /x\nHEAD a\nbranch refs/heads/main\n\n\n\n"
	blocks, err := parsePorcelain(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("want 1 block (no spurious empty), got %d", len(blocks))
	}
}

func TestParsePorcelain_DetachedBlock(t *testing.T) {
	in := "worktree /path\nHEAD abc123\ndetached\n\n"
	blocks, err := parsePorcelain(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if !blocks[0].Detached {
		t.Errorf("Detached = false, want true")
	}
	if blocks[0].Branch != "" {
		t.Errorf("Branch = %q, want empty for detached", blocks[0].Branch)
	}
}

func TestParsePorcelain_BareBlock(t *testing.T) {
	in := "worktree /path\nbare\n\n"
	blocks, err := parsePorcelain(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parsePorcelain: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	if !blocks[0].Bare {
		t.Errorf("Bare = false, want true")
	}
	if blocks[0].HEAD != "" {
		t.Errorf("HEAD = %q, want empty for bare", blocks[0].HEAD)
	}
	if blocks[0].Branch != "" {
		t.Errorf("Branch = %q, want empty for bare", blocks[0].Branch)
	}
}
