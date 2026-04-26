// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
)

// carriedWriteResult is the JSON shape returned by vv_carried_* tools.
type carriedWriteResult struct {
	VaultPath    string `json:"vault_path"`
	ProjectPath  string `json:"project_path"`
	BytesWritten int    `json:"bytes_written"`
	Slug         string `json:"slug"`
}

// carriedPromoteResult extends carriedWriteResult with task fields.
type carriedPromoteResult struct {
	VaultPath    string `json:"vault_path"`
	ProjectPath  string `json:"project_path"`
	BytesWritten int    `json:"bytes_written"`
	Slug         string `json:"slug"`
	NewTaskSlug  string `json:"new_task_slug"`
	TaskPath     string `json:"task_path"`
}

// writeCarriedResume atomically writes updated resume content and returns JSON.
func writeCarriedResume(cfg config.Config, absPath, project, updated, slug string) (string, error) {
	if err := mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write resume: %w", err)
	}
	res := carriedWriteResult{
		VaultPath:    absPath,
		ProjectPath:  filepath.Join(cfg.VaultPath, "Projects", project),
		BytesWritten: len(updated),
		Slug:         slug,
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	return string(data) + "\n", nil
}

// NewCarriedAddTool creates the vv_carried_add tool.
func NewCarriedAddTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_carried_add",
			Description: "Insert a new bullet at the bottom of the ### Carried forward sub-section " +
				"inside ## Open Threads in resume.md. " +
				"The bullet is emitted in canonical form: `- **{slug}** — {title} {body}`. " +
				"Returns an error if the slug already exists (case-insensitive match).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"slug": {
						"type": "string",
						"description": "Unique identifier for the carried-forward item (e.g. 'dry-run-coverage-gap')."
					},
					"title": {
						"type": "string",
						"description": "Short one-line description of the item."
					},
					"body": {
						"type": "string",
						"description": "Longer prose detail (optional). Appended after the title."
					}
				},
				"required": ["slug", "title"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
				Slug    string `json:"slug"`
				Title   string `json:"title"`
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
			if args.Title == "" {
				return "", fmt.Errorf("title is required")
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			content, absPath, err := readResume(cfg, project)
			if err != nil {
				return "", err
			}

			updated, err := mdutil.AddCarriedBullet(content, openThreadsSection, args.Slug, args.Title, args.Body)
			if err != nil {
				return "", err
			}

			return writeCarriedResume(cfg, absPath, project, updated, args.Slug)
		},
	}
}

// NewCarriedRemoveTool creates the vv_carried_remove tool.
func NewCarriedRemoveTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_carried_remove",
			Description: "Remove a bullet from the ### Carried forward sub-section of resume.md. " +
				"The slug match is case-insensitive. Returns a hard error if the slug is not " +
				"found, listing all available slugs.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"slug": {
						"type": "string",
						"description": "Slug of the carried-forward bullet to remove (case-insensitive)."
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

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			content, absPath, err := readResume(cfg, project)
			if err != nil {
				return "", err
			}

			updated, err := mdutil.RemoveCarriedBullet(content, openThreadsSection, args.Slug)
			if err != nil {
				return "", err
			}

			return writeCarriedResume(cfg, absPath, project, updated, args.Slug)
		},
	}
}

// NewCarriedPromoteToTaskTool creates the vv_carried_promote_to_task tool.
func NewCarriedPromoteToTaskTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_carried_promote_to_task",
			Description: "Move a ### Carried forward bullet to a new task file " +
				"(agentctx/tasks/{new_task_slug}.md) and remove it from the carried list. " +
				"The task file is created with a minimal frontmatter header and the bullet body " +
				"verbatim. Hard error if the slug is not found or if the target task already exists.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"slug": {
						"type": "string",
						"description": "Slug of the carried-forward bullet to promote (case-insensitive)."
					},
					"new_task_slug": {
						"type": "string",
						"description": "Filename slug for the new task file (without .md extension)."
					}
				},
				"required": ["slug", "new_task_slug"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project     string `json:"project"`
				Slug        string `json:"slug"`
				NewTaskSlug string `json:"new_task_slug"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Slug == "" {
				return "", fmt.Errorf("slug is required")
			}
			if args.NewTaskSlug == "" {
				return "", fmt.Errorf("new_task_slug is required")
			}
			if err := validateTaskName(args.NewTaskSlug); err != nil {
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

			// Find the bullet to promote.
			bullet, err := mdutil.GetCarriedBullet(content, openThreadsSection, args.Slug)
			if err != nil {
				return "", err
			}

			// Check target task does not already exist.
			tasksDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "tasks")
			if _, vaultErr := vaultPrefixCheck(tasksDir, cfg.VaultPath); vaultErr != nil {
				return "", vaultErr
			}
			taskPath := filepath.Join(tasksDir, args.NewTaskSlug+".md")
			if _, statErr := os.Stat(taskPath); statErr == nil {
				return "", fmt.Errorf("task %q already exists at %s", args.NewTaskSlug, taskPath)
			}

			// Build task file content: minimal frontmatter + bullet body.
			taskContent := buildPromotedTaskContent(args.NewTaskSlug, bullet.Slug, bullet.Body)

			// Write task file atomically.
			if writeErr := mdutil.AtomicWriteFile(taskPath, []byte(taskContent), 0o644); writeErr != nil {
				return "", fmt.Errorf("create task file: %w", writeErr)
			}

			// Remove the bullet from carried forward.
			updated, removeErr := mdutil.RemoveCarriedBullet(content, openThreadsSection, args.Slug)
			if removeErr != nil {
				// Task was written but resume update failed — report actionable error.
				return "", fmt.Errorf("task created at %s but failed to remove carried bullet: %w", taskPath, removeErr)
			}

			// Write updated resume atomically.
			if resumeErr := mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644); resumeErr != nil {
				return "", fmt.Errorf("task created at %s but failed to write resume: %w", taskPath, resumeErr)
			}

			res := carriedPromoteResult{
				VaultPath:    absPath,
				ProjectPath:  filepath.Join(cfg.VaultPath, "Projects", project),
				BytesWritten: len(updated),
				Slug:         args.Slug,
				NewTaskSlug:  args.NewTaskSlug,
				TaskPath:     taskPath,
			}
			data, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal result: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// buildPromotedTaskContent constructs the task file content for a promoted
// carried-forward bullet. The frontmatter mirrors the style used in
// vv_manage_task create operations.
func buildPromotedTaskContent(taskSlug, bulletSlug, bulletBody string) string {
	header := fmt.Sprintf("# Task: %s\n\n**Status:** Draft\n**Source:** Promoted from `### Carried forward` bullet `%s`\n\n## Description\n\n",
		taskSlug, bulletSlug)
	if bulletBody != "" {
		return header + bulletBody + "\n"
	}
	return header + "(no body — add details here)\n"
}
