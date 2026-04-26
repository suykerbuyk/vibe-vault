// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
)

const openThreadsSection = "Open Threads"

// carriedForwardSlug is the Phase 3.5 slug that Phase 3 tools must not touch.
const carriedForwardSlug = "Carried forward"

// rejectCarriedForward returns a non-nil error when slug matches
// "Carried forward", directing callers to use vv_carried_* tools.
func rejectCarriedForward(slug string) error {
	if slug == carriedForwardSlug {
		return fmt.Errorf("refusing to remove Carried forward sub-section; use vv_carried_* tools when available")
	}
	return nil
}

// rejectCarriedForwardReplace returns a non-nil error when slug matches
// "Carried forward", directing callers to use vv_carried_* tools.
func rejectCarriedForwardReplace(slug string) error {
	if slug == carriedForwardSlug {
		return fmt.Errorf("refusing to replace Carried forward sub-section; use vv_carried_* tools when available")
	}
	return nil
}

// threadWriteResult is the JSON shape returned by all vv_thread_* tools.
type threadWriteResult struct {
	VaultPath          string `json:"vault_path"`
	ProjectPath        string `json:"project_path"`
	BytesWritten       int    `json:"bytes_written"`
	Position           string `json:"position,omitempty"`
	Slug               string `json:"slug,omitempty"`
	CandidatesWarning  string `json:"candidates_warning,omitempty"`
}

// readResume reads resume.md for a project and returns its content + abs path.
func readResume(cfg config.Config, project string) (content, absPath string, err error) {
	p := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "resume.md")
	abs, err := vaultPrefixCheck(p, cfg.VaultPath)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("resume.md not found for project %q — run `vv context init` first", project)
		}
		return "", "", fmt.Errorf("read resume: %w", err)
	}
	return string(data), abs, nil
}

// writeResume atomically writes updated resume content and returns the result JSON.
func writeResume(cfg config.Config, absPath, project, updated, slug, positionLabel string, warning string) (string, error) {
	if err := mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write resume: %w", err)
	}
	res := threadWriteResult{
		VaultPath:    absPath,
		ProjectPath:  filepath.Join(cfg.VaultPath, "Projects", project),
		BytesWritten: len(updated),
		Slug:         slug,
	}
	if positionLabel != "" {
		res.Position = positionLabel
	}
	if warning != "" {
		res.CandidatesWarning = warning
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(data) + "\n", nil
}

// extractCandidatesWarning splits the candidates_warning prefix from the
// modified document returned by ReplaceSubsectionBody / RemoveSubsection.
// Returns (doc, warningDetail) where warningDetail is non-empty only when
// a "candidates_warning:" prefix is present.
func extractCandidatesWarning(s string) (doc, warning string) {
	const pfx = "candidates_warning:"
	if !strings.HasPrefix(s, pfx) {
		return s, ""
	}
	rest := s[len(pfx):]
	nl := strings.Index(rest, "\n")
	if nl < 0 {
		return rest, ""
	}
	return rest[nl+1:], rest[:nl]
}

// NewThreadInsertTool creates the vv_thread_insert tool.
func NewThreadInsertTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_thread_insert",
			Description: "Insert a new ### slug block into the ## Open Threads section of resume.md. " +
				"The slug must not already exist. " +
				"The body you supply must NOT include the ### heading line — the tool emits that. " +
				"Note: the slug 'Carried forward' is reserved for vv_carried_* tools.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"position": {
						"type": "object",
						"description": "Where to insert. mode is one of: top, bottom, after, before. For after/before, provide anchor_slug.",
						"properties": {
							"mode": {"type": "string", "enum": ["top","bottom","after","before"]},
							"anchor_slug": {"type": "string", "description": "Slug of adjacent sub-heading (required for after/before modes)."}
						},
						"required": ["mode"]
					},
					"slug": {
						"type": "string",
						"description": "The slug for the new ### sub-heading. Must not already exist in Open Threads."
					},
					"body": {
						"type": "string",
						"description": "Body content for the new sub-section (everything after the ### heading line)."
					}
				},
				"required": ["position", "slug", "body"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project  string `json:"project"`
				Position struct {
					Mode       string `json:"mode"`
					AnchorSlug string `json:"anchor_slug"`
				} `json:"position"`
				Slug string `json:"slug"`
				Body string `json:"body"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Slug == "" {
				return "", fmt.Errorf("slug is required")
			}
			if args.Body == "" {
				return "", fmt.Errorf("body is required")
			}
			if args.Position.Mode == "" {
				return "", fmt.Errorf("position.mode is required")
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			content, absPath, err := readResume(cfg, project)
			if err != nil {
				return "", err
			}

			pos := mdutil.InsertPosition{
				Mode:       args.Position.Mode,
				AnchorSlug: args.Position.AnchorSlug,
			}
			updated, err := mdutil.InsertSubsection(content, openThreadsSection, pos, args.Slug, args.Body)
			if err != nil {
				return "", err
			}

			posLabel := args.Position.Mode
			if args.Position.AnchorSlug != "" {
				posLabel = args.Position.Mode + ":" + args.Position.AnchorSlug
			}
			return writeResume(cfg, absPath, project, updated, args.Slug, posLabel, "")
		},
	}
}

// NewThreadReplaceTool creates the vv_thread_replace tool.
func NewThreadReplaceTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_thread_replace",
			Description: "Replace the body of an existing ### slug block in the ## Open Threads section of resume.md. " +
				"The body you supply must NOT include the ### heading line — the tool preserves the original heading verbatim. " +
				"The slug 'Carried forward' is reserved; use vv_carried_* tools for it.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"slug": {
						"type": "string",
						"description": "The normalized slug of the ### sub-heading to replace (text up to first ' — ' or end-of-line)."
					},
					"body": {
						"type": "string",
						"description": "New body content for the sub-section (replaces everything after the heading line up to the next ### or ##)."
					}
				},
				"required": ["slug", "body"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
				Slug    string `json:"slug"`
				Body    string `json:"body"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Slug == "" {
				return "", fmt.Errorf("slug is required")
			}
			if args.Body == "" {
				return "", fmt.Errorf("body is required")
			}
			if err := rejectCarriedForwardReplace(args.Slug); err != nil {
				return "", err
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			content, absPath, err := readResume(cfg, project)
			if err != nil {
				return "", err
			}

			raw, err := mdutil.ReplaceSubsectionBody(content, openThreadsSection, args.Slug, args.Body)
			if err != nil {
				return "", err
			}

			updated, warning := extractCandidatesWarning(raw)
			return writeResume(cfg, absPath, project, updated, args.Slug, "", warning)
		},
	}
}

// NewThreadRemoveTool creates the vv_thread_remove tool.
func NewThreadRemoveTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_thread_remove",
			Description: "Remove a ### slug block from the ## Open Threads section of resume.md. " +
				"The slug 'Carried forward' is reserved; use vv_carried_* tools for it.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"slug": {
						"type": "string",
						"description": "The normalized slug of the ### sub-heading to remove."
					}
				},
				"required": ["slug"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
				Slug    string `json:"slug"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Slug == "" {
				return "", fmt.Errorf("slug is required")
			}
			if err := rejectCarriedForward(args.Slug); err != nil {
				return "", err
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			content, absPath, err := readResume(cfg, project)
			if err != nil {
				return "", err
			}

			raw, err := mdutil.RemoveSubsection(content, openThreadsSection, args.Slug)
			if err != nil {
				return "", err
			}

			updated, warning := extractCandidatesWarning(raw)
			return writeResume(cfg, absPath, project, updated, args.Slug, "", warning)
		},
	}
}
