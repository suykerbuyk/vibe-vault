// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// describeIterStateResult is the JSON shape returned by
// vv_describe_iter_state. Field semantics mirror Direction-C decision D6:
// the server returns only the four mechanically-computable facts; the
// slash command computes commits_since_last_iter, files_changed,
// task_deltas, and test_counts itself via git/filesystem.
type describeIterStateResult struct {
	IterN                     int    `json:"iter_n"`
	Branch                    string `json:"branch"`
	VaultHasUncommittedWrites bool   `json:"vault_has_uncommitted_writes"`
	LastIterAnchorSha         string `json:"last_iter_anchor_sha,omitempty"`
}

// gitCmdRunner runs a git command in dir and returns its stdout. Test seam.
var gitCmdRunner = func(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// iterAnchorRe matches the iteration footer that wrap commits include in
// their commit body. The slash command includes "## Iteration N" in commit
// messages (see internal/mcp/tools_render_commit_msg.go renderCommitMsg).
var iterAnchorRe = regexp.MustCompile(`(?m)^## Iteration (\d+)\s*$`)

// describeIterState computes the four-field state record for a project.
// It is the single source of truth for the vv_describe_iter_state tool.
func describeIterState(cfg config.Config, project string) (describeIterStateResult, error) {
	res := describeIterStateResult{}

	// iter_n: index.NextIteration() seeded with today's date.
	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		return res, fmt.Errorf("load index: %w", err)
	}
	today := time.Now().Format("2006-01-02")
	res.IterN = idx.NextIteration(project, today)

	// branch: git rev-parse --abbrev-ref HEAD in the agent CWD.
	cwd, err := os.Getwd()
	if err != nil {
		return res, fmt.Errorf("get working directory: %w", err)
	}
	res.Branch = detectBranch(cwd)

	// vault_has_uncommitted_writes: git status --porcelain in the vault repo.
	dirty, derr := vaultHasUncommittedWrites(cfg.VaultPath)
	if derr != nil {
		return res, fmt.Errorf("vault git status: %w", derr)
	}
	res.VaultHasUncommittedWrites = dirty

	// last_iter_anchor_sha: search project git log for the prior iter footer.
	sha, serr := lastIterAnchorSha(cwd, res.IterN-1)
	if serr != nil {
		return res, fmt.Errorf("last iter anchor sha: %w", serr)
	}
	res.LastIterAnchorSha = sha

	return res, nil
}

// vaultHasUncommittedWrites returns true iff `git status --porcelain` in
// vaultPath produces any output.  Returns false and a nil error when
// vaultPath is empty or not a git repo.
func vaultHasUncommittedWrites(vaultPath string) (bool, error) {
	if vaultPath == "" {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(vaultPath, ".git")); err != nil {
		// Not a git repo (or unreadable); treat as clean — matches the
		// "no signal available" interpretation in D6.
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	out, err := gitCmdRunner(ctx, vaultPath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// lastIterAnchorSha searches the project's git log for a commit whose body
// contains the canonical "## Iteration N" footer. Returns the full SHA of
// the matching commit; empty string when no match is found (e.g. iter 0
// or fresh project) — null in JSON via omitempty.
//
// targetIter <= 0 short-circuits to "" (no anchor for iter 0/1's previous).
func lastIterAnchorSha(cwd string, targetIter int) (string, error) {
	if targetIter <= 0 {
		return "", nil
	}
	if cwd == "" {
		return "", nil
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return "", nil
	}
	// Search the last 200 commits for the footer. 200 is generous —
	// real projects rarely have more than ~30 wraps between fetches.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := gitCmdRunner(ctx, cwd, "log", "-n", "200", "--format=%H%x00%B%x1e")
	if err != nil {
		// git log can fail on a brand-new repo with no commits; treat as
		// no-anchor rather than error so vv_describe_iter_state remains
		// robust on first-iter projects.
		return "", nil
	}
	// Records are separated by RS (0x1e); each record is "SHA\0BODY".
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		nul := strings.IndexByte(rec, '\x00')
		if nul < 0 {
			continue
		}
		sha := rec[:nul]
		body := rec[nul+1:]
		// Match against canonical "## Iteration N" line. The capture group
		// holds the iteration number; compare numerically rather than
		// stringly so trailing newlines/whitespace do not matter.
		if matches := iterAnchorRe.FindAllStringSubmatch(body, -1); matches != nil {
			for _, m := range matches {
				if len(m) >= 2 {
					n, err := strconv.Atoi(m[1])
					if err == nil && n == targetIter {
						return sha, nil
					}
				}
			}
		}
	}
	return "", nil
}

// NewDescribeIterStateTool creates the vv_describe_iter_state MCP tool.
//
// Per Direction-C decision D6, the server returns only the four
// mechanically-computable facts (iter_n, branch,
// vault_has_uncommitted_writes, last_iter_anchor_sha). Higher-level
// fields (commits_since_last_iter, files_changed, task_deltas,
// test_counts) are slash-command-computed and passed to
// vv_render_wrap_text directly.
func NewDescribeIterStateTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_describe_iter_state",
			Description: "Return a server-minimal iter-state record for the current project: " +
				"iter_n (next iteration number for today), branch (current git branch in agent CWD), " +
				"vault_has_uncommitted_writes (bool from `git status --porcelain` in the vault repo), " +
				"last_iter_anchor_sha (SHA of the previous iter's commit; null/omitted if not found). " +
				"The slash command computes commits_since_last_iter, files_changed, task_deltas, and " +
				"test_counts itself via git/filesystem and bundles them into vv_render_wrap_text.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			res, err := describeIterState(cfg, project)
			if err != nil {
				return "", err
			}

			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
