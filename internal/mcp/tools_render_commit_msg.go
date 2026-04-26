// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
)

// execGitStatus is a test seam for running "git status --porcelain" in a
// directory. Replace in tests to avoid real git invocations.
var execGitStatus = func(dir string) (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git status --porcelain: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// execGitDiffCachedStat is a test seam for running "git diff --cached --stat"
// in a directory. Replace in tests to avoid real git invocations.
var execGitDiffCachedStat = func(dir string) (string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--stat")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff --cached --stat: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// NewRenderCommitMsgTool creates the vv_render_commit_msg tool.
//
// The tool assembles a commit message string from AI-supplied content and
// mechanical sections derived from the git working tree. It returns the
// rendered string for AI inspection; it does NOT write any file. The AI
// passes the returned string to vv_set_commit_msg to persist it.
//
// subject is required and must be a single line (no newlines). prose_body is
// the AI's 2–3 paragraph narrative of what happened and why. The three
// integer counts (unit_tests, integration_subtests, lint_findings) are the
// AI-supplied pass/finding totals from the current iteration.
//
// project_path overrides automatic project-root resolution when provided.
// If omitted, meta.ProjectRoot() is called with the agent CWD.
func NewRenderCommitMsgTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_render_commit_msg",
			Description: "Assemble a commit message string from AI-supplied subject, prose body, " +
				"and integer test counts. Returns {rendered, bytes} for AI inspection — " +
				"does NOT write any file. Pass the rendered string to vv_set_commit_msg to persist. " +
				"subject must be a single line; prose_body is verbatim. " +
				"Files changed section is derived from git status/diff in the project root.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"project_path": {
						"type": "string",
						"description": "Absolute path to the project root. If omitted, derived via meta.ProjectRoot()."
					},
					"iteration": {
						"type": "integer",
						"description": "Current iteration number. Must be a positive integer."
					},
					"subject": {
						"type": "string",
						"description": "Single-line commit subject (the first line of the commit message). Required. Must not contain newlines."
					},
					"prose_body": {
						"type": "string",
						"description": "AI-authored narrative body: 2-3 paragraphs describing what happened and why."
					},
					"unit_tests": {
						"type": "integer",
						"description": "Total unit test count for this iteration."
					},
					"integration_subtests": {
						"type": "integer",
						"description": "Total integration subtest count for this iteration."
					},
					"lint_findings": {
						"type": "integer",
						"description": "Total lint finding count for this iteration (0 is the goal)."
					}
				},
				"required": ["iteration", "subject", "prose_body", "unit_tests", "integration_subtests", "lint_findings"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project             string `json:"project"`
				ProjectPath         string `json:"project_path"`
				Iteration           int    `json:"iteration"`
				Subject             string `json:"subject"`
				ProseBody           string `json:"prose_body"`
				UnitTests           int    `json:"unit_tests"`
				IntegrationSubtests int    `json:"integration_subtests"`
				LintFindings        int    `json:"lint_findings"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			// Validate required fields.
			if args.Subject == "" {
				return "", fmt.Errorf("subject is required")
			}
			if strings.Contains(args.Subject, "\n") {
				return "", fmt.Errorf("subject must be a single line (no newlines)")
			}
			if args.ProseBody == "" {
				return "", fmt.Errorf("prose_body is required")
			}
			if args.Iteration <= 0 {
				return "", fmt.Errorf("iteration must be a positive integer, got %d", args.Iteration)
			}

			// Resolve project root.
			projectRoot, err := resolveProjectRoot(args.ProjectPath, cfg.VaultPath)
			if err != nil {
				return "", err
			}

			// Derive files-changed section from git.
			filesSection, err := buildFilesChangedSection(projectRoot)
			if err != nil {
				return "", fmt.Errorf("files changed section: %w", err)
			}

			// Assemble the rendered commit message.
			rendered := renderCommitMsg(
				args.Subject,
				args.ProseBody,
				filesSection,
				args.UnitTests,
				args.IntegrationSubtests,
				args.LintFindings,
				args.Iteration,
			)

			result := struct {
				Rendered string `json:"rendered"`
				Bytes    int    `json:"bytes"`
			}{
				Rendered: rendered,
				Bytes:    len(rendered),
			}
			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}

// resolveProjectRoot returns the project root: explicit path if provided,
// otherwise derived via meta.ProjectRoot() using the agent CWD.
func resolveProjectRoot(explicit, vaultPath string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	root, err := meta.ProjectRoot(cwd, vaultPath)
	if err != nil {
		return "", fmt.Errorf("project root resolution failed (cwd=%q): %w", cwd, err)
	}
	return root, nil
}

// buildFilesChangedSection runs git status and git diff --cached --stat in
// dir and returns the formatted ## Files changed section body (without the
// heading line).
func buildFilesChangedSection(dir string) (string, error) {
	statusOut, err := execGitStatus(dir)
	if err != nil {
		return "", err
	}

	diffOut, err := execGitDiffCachedStat(dir)
	if err != nil {
		return "", err
	}

	// Collect staged file paths from "git status --porcelain".
	// Lines starting with 'X ' where X is not space indicate staged changes.
	var staged []string
	for _, line := range strings.Split(statusOut, "\n") {
		if len(line) < 3 {
			continue
		}
		indexStatus := line[0]
		// Staged if index column is not space or '?'
		if indexStatus != ' ' && indexStatus != '?' {
			// Path starts at column 3.
			path := strings.TrimSpace(line[3:])
			// Handle renames: "old -> new" — take the destination.
			if idx := strings.Index(path, " -> "); idx >= 0 {
				path = path[idx+4:]
			}
			staged = append(staged, path)
		}
	}

	var sb strings.Builder
	if len(staged) == 0 {
		sb.WriteString("(no staged changes)")
	} else {
		for _, f := range staged {
			sb.WriteString("- ")
			sb.WriteString(f)
			sb.WriteString("\n")
		}
		// Append git diff stat summary if non-empty.
		stat := strings.TrimSpace(diffOut)
		if stat != "" {
			sb.WriteString("\n")
			sb.WriteString(stat)
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

// renderCommitMsg assembles the final commit message string.
func renderCommitMsg(
	subject, proseBody, filesSection string,
	unitTests, integrationSubtests, lintFindings, iteration int,
) string {
	var sb strings.Builder

	// 1. Subject line.
	sb.WriteString(subject)
	sb.WriteString("\n")

	// 2. Blank line.
	sb.WriteString("\n")

	// 3. Prose body (verbatim).
	sb.WriteString(proseBody)
	// Ensure prose ends with exactly one newline before the blank separator.
	if !strings.HasSuffix(proseBody, "\n") {
		sb.WriteString("\n")
	}

	// 4. Blank line.
	sb.WriteString("\n")

	// 5. Files changed section.
	sb.WriteString("## Files changed\n")
	sb.WriteString("\n")
	sb.WriteString(filesSection)
	if !strings.HasSuffix(filesSection, "\n") {
		sb.WriteString("\n")
	}

	// 6. Blank line.
	sb.WriteString("\n")

	// 7. Test counts section.
	sb.WriteString("## Test counts\n")
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "- Unit tests: %d\n", unitTests)
	fmt.Fprintf(&sb, "- Integration subtests: %d\n", integrationSubtests)
	fmt.Fprintf(&sb, "- Lint findings: %d\n", lintFindings)

	// 8. Blank line.
	sb.WriteString("\n")

	// 9. Iteration footer.
	fmt.Fprintf(&sb, "## Iteration %d\n", iteration)

	return sb.String()
}
