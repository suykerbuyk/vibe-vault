// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// porcelainBlock represents one block emitted by
// `git worktree list --porcelain`.
type porcelainBlock struct {
	Worktree string // path
	HEAD     string // sha
	Branch   string // <name> (refs/heads/ stripped)
	Locked   string // reason
	Detached bool   // literal `detached` line seen
	Bare     bool   // literal `bare` line seen
}

// parsePorcelain consumes the porcelain output of `git worktree list
// --porcelain` and returns one block per worktree. Blocks are separated
// by blank lines; trailing blank lines are tolerated. Lines are
// space-split key/value pairs (`worktree`, `HEAD`, `branch`, `locked`)
// or single keywords (`detached`, `bare`).
func parsePorcelain(r io.Reader) ([]porcelainBlock, error) {
	// bufio.Scanner default 64 KB buffer accommodates ~200 worktree blocks
	// (~320 bytes each); raise via Scanner.Buffer if a real workload exceeds.
	scanner := bufio.NewScanner(r)

	var (
		blocks  []porcelainBlock
		cur     porcelainBlock
		hasData bool
	)

	flush := func() {
		if hasData && cur.Worktree != "" {
			blocks = append(blocks, cur)
		}
		cur = porcelainBlock{}
		hasData = false
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		hasData = true

		// Single-keyword lines.
		switch line {
		case "detached":
			cur.Detached = true
			continue
		case "bare":
			cur.Bare = true
			continue
		}

		// Key/value lines: split on first space.
		key, value, ok := strings.Cut(line, " ")
		if !ok {
			// Unknown single-token line; ignore.
			continue
		}
		switch key {
		case "worktree":
			cur.Worktree = value
		case "HEAD":
			cur.HEAD = value
		case "branch":
			cur.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "locked":
			cur.Locked = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// EOF mid-block: flush remaining accumulator.
	flush()

	return blocks, nil
}

// runWorktreeListPorcelain runs `git -C cwd worktree list --porcelain`
// and parses its stdout. Bubble up exec errors with stderr context.
func runWorktreeListPorcelain(cwd string) ([]porcelainBlock, error) {
	cmd := exec.Command("git", "-C", cwd, "worktree", "list", "--porcelain")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git worktree list --porcelain: %w: %s",
			err, strings.TrimSpace(stderr.String()))
	}
	return parsePorcelain(&stdout)
}
