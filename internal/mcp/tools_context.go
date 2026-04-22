// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/inject"
	"github.com/suykerbuyk/vibe-vault/internal/knowledge"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/trends"
	"github.com/suykerbuyk/vibe-vault/templates"
)

var (
	titleRegexp    = regexp.MustCompile(`^#+\s+(.+)`)
	statusRegexp   = regexp.MustCompile(`^(?:##\s+)?Status:\s*(.+)`)
	priorityRegexp = regexp.MustCompile(`^(?:##\s+)?Priority:\s*(.+)`)
)

// resolveProject returns the project name from an explicit arg or CWD detection.
func resolveProject(explicit string) (string, error) {
	if explicit != "" {
		return explicit, validateProjectName(explicit)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("detect project: %w", err)
	}
	name := session.DetectProject(cwd)
	if name == "" || name == "_unknown" {
		return "", fmt.Errorf("could not detect project from working directory; pass \"project\" explicitly")
	}
	if err := validateProjectName(name); err != nil {
		return "", fmt.Errorf("detected project name is invalid: %w", err)
	}
	return name, nil
}

// vaultPrefixCheck resolves absPath and verifies it lives under the vault root.
func vaultPrefixCheck(path, vaultPath string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	absVault, _ := filepath.Abs(vaultPath)
	if !strings.HasPrefix(absPath, absVault+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal rejected")
	}
	return absPath, nil
}

// NewGetWorkflowTool creates the vv_get_workflow tool.
func NewGetWorkflowTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_workflow",
			Description: "Get the workflow instructions for a project. Returns the agentctx/workflow.md content, falling back to the embedded default template if no project-specific file exists.",
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

			path := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "workflow.md")
			absPath, err := vaultPrefixCheck(path, cfg.VaultPath)
			if err != nil {
				return "", err
			}

			data, err := os.ReadFile(absPath)
			if err != nil {
				if os.IsNotExist(err) {
					// Fall back to embedded template
					tmpl, tmplErr := fs.ReadFile(templates.AgentctxFS(), "agentctx/workflow.md")
					if tmplErr != nil {
						return "", fmt.Errorf("read embedded workflow template: %w", tmplErr)
					}
					content := strings.ReplaceAll(string(tmpl), "{{PROJECT}}", project)
					return content, nil
				}
				return "", fmt.Errorf("read workflow: %w", err)
			}
			return string(data), nil
		},
	}
}

// NewGetResumeTool creates the vv_get_resume tool.
func NewGetResumeTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_resume",
			Description: "Get the resume.md for a project, containing session-start context and behavioral rules.",
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

			path := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "resume.md")
			absPath, err := vaultPrefixCheck(path, cfg.VaultPath)
			if err != nil {
				return "", err
			}

			data, err := os.ReadFile(absPath)
			if err != nil {
				if os.IsNotExist(err) {
					return "", fmt.Errorf("resume.md not found for project %q — run `vv context init` first", project)
				}
				return "", fmt.Errorf("read resume: %w", err)
			}
			return string(data), nil
		},
	}
}

// taskEntry represents a single task in the list output.
// Status, Priority, and Done use omitempty: most active tasks have no
// frontmatter metadata, so suppressing empty fields removes ~60 bytes per
// task from vv_bootstrap_context and vv_list_tasks payloads. Consumers that
// read the JSON into a struct with default zero values are unaffected
// (absence and empty string are semantically equivalent).
type taskEntry struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
	Done     bool   `json:"done,omitempty"`
}

// parseTaskHeader reads the first 10 lines of a task file and extracts title, status, priority.
func parseTaskHeader(path string) (title, status, priority string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for i := 0; i < 10 && scanner.Scan(); i++ {
		line := scanner.Text()
		if title == "" {
			if m := titleRegexp.FindStringSubmatch(line); m != nil {
				title = m[1]
			}
		}
		if status == "" {
			if m := statusRegexp.FindStringSubmatch(line); m != nil {
				status = strings.TrimSpace(m[1])
			}
		}
		if priority == "" {
			if m := priorityRegexp.FindStringSubmatch(line); m != nil {
				priority = strings.TrimSpace(m[1])
			}
		}
	}
	return title, status, priority
}

// scanTaskDir reads .md files from a directory (non-recursive) and appends to tasks.
func scanTaskDir(dir string, done bool, tasks *[]taskEntry) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		title, status, priority := parseTaskHeader(filepath.Join(dir, e.Name()))
		*tasks = append(*tasks, taskEntry{
			Name:     slug,
			Title:    title,
			Status:   status,
			Priority: priority,
			Done:     done,
		})
	}
	return nil
}

// NewListTasksTool creates the vv_list_tasks tool.
func NewListTasksTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_list_tasks",
			Description: "List tasks for a project from the agentctx/tasks directory. Returns task names, titles, statuses, and priorities.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"include_done": {
						"type": "boolean",
						"description": "Include tasks from done/ and cancelled/ subdirectories. Default: false."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project     string `json:"project"`
				IncludeDone bool   `json:"include_done"`
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

			tasksDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "tasks")
			if _, pfxErr := vaultPrefixCheck(tasksDir, cfg.VaultPath); pfxErr != nil {
				return "", pfxErr
			}

			var tasks []taskEntry
			if scanErr := scanTaskDir(tasksDir, false, &tasks); scanErr != nil {
				return "", fmt.Errorf("scan tasks: %w", scanErr)
			}

			if args.IncludeDone {
				if doneErr := scanTaskDir(filepath.Join(tasksDir, "done"), true, &tasks); doneErr != nil {
					return "", fmt.Errorf("scan done tasks: %w", doneErr)
				}
				if cancelErr := scanTaskDir(filepath.Join(tasksDir, "cancelled"), true, &tasks); cancelErr != nil {
					return "", fmt.Errorf("scan cancelled tasks: %w", cancelErr)
				}
			}

			result := struct {
				Project string      `json:"project"`
				Tasks   []taskEntry `json:"tasks"`
			}{
				Project: project,
				Tasks:   tasks,
			}
			if result.Tasks == nil {
				result.Tasks = []taskEntry{}
			}

			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// validateTaskName rejects task names that could escape the tasks directory.
func validateTaskName(name string) error {
	if name == "" {
		return fmt.Errorf("task name is required")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid task name: %q", name)
	}
	return nil
}

// NewGetTaskTool creates the vv_get_task tool.
func NewGetTaskTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_task",
			Description: "Get the full content of a specific task file by name.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"task": {
						"type": "string",
						"description": "Task name (filename without .md extension)."
					},
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					}
				},
				"required": ["task"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Task    string `json:"task"`
				Project string `json:"project"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if err := validateTaskName(args.Task); err != nil {
				return "", err
			}
			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			tasksDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "tasks")

			// Try tasks/{task}.md, then done/, then cancelled/
			candidates := []string{
				filepath.Join(tasksDir, args.Task+".md"),
				filepath.Join(tasksDir, "done", args.Task+".md"),
				filepath.Join(tasksDir, "cancelled", args.Task+".md"),
			}

			for _, candidate := range candidates {
				absPath, err := vaultPrefixCheck(candidate, cfg.VaultPath)
				if err != nil {
					return "", err
				}
				data, err := os.ReadFile(absPath)
				if err != nil {
					if os.IsNotExist(err) {
						continue
					}
					return "", fmt.Errorf("read task: %w", err)
				}
				return string(data), nil
			}

			return "", fmt.Errorf("task %q not found in project %q", args.Task, project)
		},
	}
}

// NewBootstrapContextTool creates the vv_bootstrap_context tool.
// It composes existing read operations into a single one-shot session bootstrap,
// returning workflow, resume, active tasks, and inject context as structured JSON.
func NewBootstrapContextTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_bootstrap_context",
			Description: "Bootstrap full session context in one call. Returns workflow instructions, resume state, active tasks, and project context (sessions, threads, decisions, friction). Replaces the multi-file bootstrap chain with a single MCP call.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"max_tokens": {
						"type": "integer",
						"description": "Token budget for the context section. Default: 8000. Workflow, resume, and tasks are always included in full."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project   string `json:"project"`
				MaxTokens int    `json:"max_tokens"`
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

			if args.MaxTokens <= 0 {
				args.MaxTokens = 8000
			}

			agentctxDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx")

			// 1. Read workflow (with template fallback)
			workflowPath := filepath.Join(agentctxDir, "workflow.md")
			absWorkflow, err := vaultPrefixCheck(workflowPath, cfg.VaultPath)
			if err != nil {
				return "", err
			}
			var workflow string
			if data, readErr := os.ReadFile(absWorkflow); readErr != nil {
				if os.IsNotExist(readErr) {
					tmpl, tmplErr := fs.ReadFile(templates.AgentctxFS(), "agentctx/workflow.md")
					if tmplErr != nil {
						return "", fmt.Errorf("read embedded workflow template: %w", tmplErr)
					}
					workflow = strings.ReplaceAll(string(tmpl), "{{PROJECT}}", project)
				} else {
					return "", fmt.Errorf("read workflow: %w", readErr)
				}
			} else {
				workflow = string(data)
			}

			// 2. Read resume (allow missing)
			resumePath := filepath.Join(agentctxDir, "resume.md")
			absResume, err := vaultPrefixCheck(resumePath, cfg.VaultPath)
			if err != nil {
				return "", err
			}
			var resume string
			if data, readErr := os.ReadFile(absResume); readErr == nil {
				resume = string(data)
			}

			// 3. Scan active tasks
			tasksDir := filepath.Join(agentctxDir, "tasks")
			if _, pfxErr := vaultPrefixCheck(tasksDir, cfg.VaultPath); pfxErr != nil {
				return "", pfxErr
			}
			var tasks []taskEntry
			if scanErr := scanTaskDir(tasksDir, false, &tasks); scanErr != nil {
				return "", fmt.Errorf("scan tasks: %w", scanErr)
			}

			// 4. Build inject context
			idx, err := index.Load(cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}
			trendResult := trends.Compute(idx.Entries, project, 4)
			injectOpts := inject.Opts{
				Project:   project,
				Format:    "json",
				MaxTokens: args.MaxTokens,
			}
			injectResult := inject.Build(idx.Entries, trendResult, injectOpts)
			contextOutput, err := inject.Render(injectResult, injectOpts)
			if err != nil {
				return "", fmt.Errorf("render context: %w", err)
			}

			// 5. Compose response
			if tasks == nil {
				tasks = []taskEntry{}
			}
			response := struct {
				Project                     string                    `json:"project"`
				Workflow                    string                    `json:"workflow"`
				Resume                      string                    `json:"resume"`
				ActiveTasks                 []taskEntry               `json:"active_tasks"`
				Context                     string                    `json:"context"`
				KnowledgeLearningsAvailable *learningsAvailableHint   `json:"knowledge_learnings_available,omitempty"`
			}{
				Project:     project,
				Workflow:    workflow,
				Resume:      resume,
				ActiveTasks: tasks,
				Context:     contextOutput,
			}

			// 6. Cross-project learnings hint: emit only when the
			// Knowledge/learnings/ directory has ≥1 valid entry. This
			// keeps the bootstrap payload lean (zero tokens when the
			// vault has no learnings yet) while still signaling to the
			// model that the on-demand tools are worth calling.
			if count, cerr := knowledge.Count(cfg.VaultPath); cerr == nil && count > 0 {
				response.KnowledgeLearningsAvailable = &learningsAvailableHint{
					Count: count,
					Hint:  "call vv_list_learnings when planning, vv_get_learning(slug) to fetch one",
				}
			}

			data, err := json.MarshalIndent(response, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// learningsAvailableHint is the structure emitted in
// vv_bootstrap_context's optional knowledge_learnings_available field.
// Kept at package scope so the JSON shape is testable and stable.
type learningsAvailableHint struct {
	Count int    `json:"count"`
	Hint  string `json:"hint"`
}

// NewListLearningsTool creates the vv_list_learnings tool. Walks
// VibeVault/Knowledge/learnings/*.md, parses frontmatter only, and
// returns metadata entries sorted alphabetically by slug. Files with
// malformed frontmatter or the disallowed "type: project" are skipped
// with a warning on the server's stderr (never surfaced inline) so the
// consumer contract stays uniform.
func NewListLearningsTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_list_learnings",
			Description: "List cross-project learnings stored in VibeVault/Knowledge/learnings/. Returns metadata only (slug, name, description, type) so the caller can pick one to load via vv_get_learning. Files with malformed frontmatter or type=project are skipped with a stderr warning.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"filter_type": {
						"type": "string",
						"description": "When set, only entries with this type are returned. Valid values: user, feedback, reference."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				FilterType string `json:"filter_type"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			entries, err := knowledge.List(cfg.VaultPath, args.FilterType)
			if err != nil {
				return "", fmt.Errorf("list learnings: %w", err)
			}
			data, err := json.MarshalIndent(entries, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// NewGetLearningTool creates the vv_get_learning tool. Returns the full
// frontmatter + body of a single learning. Unknown slugs produce an
// error whose message lists the slugs that ARE available, so the
// caller can correct a typo without round-tripping to vv_list_learnings
// first.
func NewGetLearningTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_learning",
			Description: "Get the full content of a cross-project learning by slug (filename without .md). Returns metadata plus the markdown body. Unknown slugs produce an error that lists available slugs.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"slug": {
						"type": "string",
						"description": "Learning slug (filename without .md extension)."
					}
				},
				"required": ["slug"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Slug string `json:"slug"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			learning, err := knowledge.Get(cfg.VaultPath, args.Slug)
			if err != nil {
				return "", err
			}
			data, err := json.MarshalIndent(learning, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}
