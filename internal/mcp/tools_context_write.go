// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/index"
)

// atomicWriteFile writes data to path via a temp file + rename for crash safety.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".vv-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		// Clean up on failure
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	tmpPath = "" // prevent cleanup
	return nil
}

// NewUpdateResumeTool creates the vv_update_resume tool.
func NewUpdateResumeTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_update_resume",
			Description: "Update a section of resume.md for a project. Replaces the body of an existing ## section.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"section": {
						"type": "string",
						"description": "The section heading to update (e.g. 'Current Focus'). Must already exist in resume.md."
					},
					"content": {
						"type": "string",
						"description": "New body content for the section (replaces everything between this heading and the next ## heading or EOF)."
					}
				},
				"required": ["section", "content"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
				Section string `json:"section"`
				Content string `json:"content"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Section == "" {
				return "", fmt.Errorf("section is required")
			}
			if args.Content == "" {
				return "", fmt.Errorf("content is required")
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

			updated, err := replaceSectionBody(string(data), args.Section, args.Content)
			if err != nil {
				return "", err
			}

			if err := atomicWriteFile(absPath, []byte(updated), 0o644); err != nil {
				return "", fmt.Errorf("write resume: %w", err)
			}

			return fmt.Sprintf("Updated section %q in resume.md for project %q", args.Section, project), nil
		},
	}
}

// replaceSectionBody finds ## {heading} and replaces body up to next ## or EOF.
func replaceSectionBody(doc, heading, newBody string) (string, error) {
	lines := strings.Split(doc, "\n")
	target := "## " + heading
	startIdx := -1

	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		return "", fmt.Errorf("section %q not found in resume.md", heading)
	}

	// Find the end of this section (next ## heading or EOF)
	endIdx := len(lines)
	for i := startIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			endIdx = i
			break
		}
	}

	// Build result: heading line + blank line + new content + blank line
	var result []string
	result = append(result, lines[:startIdx+1]...)
	result = append(result, "")
	// Trim trailing newlines from content, then add it
	body := strings.TrimRight(newBody, "\n")
	result = append(result, body)
	result = append(result, "")
	result = append(result, lines[endIdx:]...)

	return strings.Join(result, "\n"), nil
}

var iterationRegexp = regexp.MustCompile(`^### Iteration (\d+)`)

// iterationHeading builds the canonical heading line for an iteration.
func iterationHeading(num int, title, date string) string {
	return fmt.Sprintf("### Iteration %d — %s (%s)", num, title, date)
}

// scanIterationNumbers parses all iteration numbers from an iterations.md body.
// Returns them in document order.
func scanIterationNumbers(content string) []int {
	var nums []int
	for _, line := range strings.Split(content, "\n") {
		if m := iterationRegexp.FindStringSubmatch(line); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				nums = append(nums, n)
			}
		}
	}
	return nums
}

// NewAppendIterationTool creates the vv_append_iteration tool.
func NewAppendIterationTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_append_iteration",
			Description: "Append a new iteration block to iterations.md for a project.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"iteration": {
						"type": "integer",
						"description": "Iteration number. If omitted, auto-increments from highest existing."
					},
					"title": {
						"type": "string",
						"description": "Short title for this iteration."
					},
					"narrative": {
						"type": "string",
						"description": "Narrative description of what happened in this iteration."
					},
					"date": {
						"type": "string",
						"description": "Date in YYYY-MM-DD format. Defaults to today."
					}
				},
				"required": ["title", "narrative"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project   string `json:"project"`
				Iteration *int   `json:"iteration"`
				Title     string `json:"title"`
				Narrative string `json:"narrative"`
				Date      string `json:"date"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Title == "" {
				return "", fmt.Errorf("title is required")
			}
			if args.Narrative == "" {
				return "", fmt.Errorf("narrative is required")
			}

			// Validate/default date
			date := args.Date
			if date == "" {
				date = time.Now().Format("2006-01-02")
			} else {
				if _, err := time.Parse("2006-01-02", date); err != nil {
					return "", fmt.Errorf("invalid date format %q — expected YYYY-MM-DD", date)
				}
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			path := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "iterations.md")
			absPath, err := vaultPrefixCheck(path, cfg.VaultPath)
			if err != nil {
				return "", err
			}

			data, err := os.ReadFile(absPath)
			if err != nil && !os.IsNotExist(err) {
				return "", fmt.Errorf("read iterations: %w", err)
			}

			content := string(data)
			if os.IsNotExist(err) {
				content = "# Iterations\n"
			}

			existing := scanIterationNumbers(content)

			// Determine iteration number
			highest := 0
			for _, n := range existing {
				if n > highest {
					highest = n
				}
			}
			iterNum := highest + 1
			if args.Iteration != nil {
				iterNum = *args.Iteration
				for _, n := range existing {
					if n == iterNum {
						return "", fmt.Errorf("iteration %d already exists", iterNum)
					}
				}
			}

			// Build the iteration block
			heading := iterationHeading(iterNum, args.Title, date)
			block := fmt.Sprintf("\n%s\n\n%s\n",
				heading, strings.TrimRight(args.Narrative, "\n"))

			// Ensure content ends with newline before appending
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += block

			if err := atomicWriteFile(absPath, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("write iterations: %w", err)
			}

			return fmt.Sprintf("Appended iteration %d to iterations.md for project %q", iterNum, project), nil
		},
	}
}

// NewManageTaskTool creates the vv_manage_task tool.
func NewManageTaskTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_manage_task",
			Description: "Create, update status, or retire a task in the agentctx/tasks directory.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"task": {
						"type": "string",
						"description": "Task slug (filename without .md extension)."
					},
					"action": {
						"type": "string",
						"enum": ["create", "update_status", "retire", "cancel"],
						"description": "Action to perform on the task."
					},
					"status": {
						"type": "string",
						"description": "New status value (required for update_status)."
					},
					"content": {
						"type": "string",
						"description": "Full markdown content for the task file (required for create)."
					}
				},
				"required": ["task", "action"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
				Task    string `json:"task"`
				Action  string `json:"action"`
				Status  string `json:"status"`
				Content string `json:"content"`
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
			if _, err := vaultPrefixCheck(tasksDir, cfg.VaultPath); err != nil {
				return "", err
			}
			taskPath := filepath.Join(tasksDir, args.Task+".md")

			switch args.Action {
			case "create":
				if args.Content == "" {
					return "", fmt.Errorf("content is required for create action")
				}
				if _, err := os.Stat(taskPath); err == nil {
					return "", fmt.Errorf("task %q already exists", args.Task)
				}
				if err := atomicWriteFile(taskPath, []byte(args.Content), 0o644); err != nil {
					return "", fmt.Errorf("create task: %w", err)
				}
				return fmt.Sprintf("Created task %q in project %q", args.Task, project), nil

			case "update_status":
				if args.Status == "" {
					return "", fmt.Errorf("status is required for update_status action")
				}
				data, err := os.ReadFile(taskPath)
				if err != nil {
					if os.IsNotExist(err) {
						return "", fmt.Errorf("task %q not found in project %q", args.Task, project)
					}
					return "", fmt.Errorf("read task: %w", err)
				}
				updated := replaceStatus(string(data), args.Status)
				if err := atomicWriteFile(taskPath, []byte(updated), 0o644); err != nil {
					return "", fmt.Errorf("update task: %w", err)
				}
				return fmt.Sprintf("Updated status of task %q to %q in project %q", args.Task, args.Status, project), nil

			case "retire":
				data, err := os.ReadFile(taskPath)
				if err != nil {
					if os.IsNotExist(err) {
						return "", fmt.Errorf("task %q not found in project %q", args.Task, project)
					}
					return "", fmt.Errorf("read task: %w", err)
				}
				// Update status to Done
				updated := replaceStatus(string(data), "Done")
				// Write to done/ directory
				doneDir := filepath.Join(tasksDir, "done")
				donePath := filepath.Join(doneDir, args.Task+".md")
				if err := atomicWriteFile(donePath, []byte(updated), 0o644); err != nil {
					return "", fmt.Errorf("write retired task: %w", err)
				}
				// Remove original
				if err := os.Remove(taskPath); err != nil {
					return "", fmt.Errorf("remove original task: %w", err)
				}
				return fmt.Sprintf("Retired task %q in project %q (moved to done/)", args.Task, project), nil

			case "cancel":
				data, err := os.ReadFile(taskPath)
				if err != nil {
					if os.IsNotExist(err) {
						return "", fmt.Errorf("task %q not found in project %q", args.Task, project)
					}
					return "", fmt.Errorf("read task: %w", err)
				}
				// Update status to Cancelled
				updated := replaceStatus(string(data), "Cancelled")
				// Write to cancelled/ directory
				cancelledDir := filepath.Join(tasksDir, "cancelled")
				cancelledPath := filepath.Join(cancelledDir, args.Task+".md")
				if err := atomicWriteFile(cancelledPath, []byte(updated), 0o644); err != nil {
					return "", fmt.Errorf("write cancelled task: %w", err)
				}
				// Remove original
				if err := os.Remove(taskPath); err != nil {
					return "", fmt.Errorf("remove original task: %w", err)
				}
				return fmt.Sprintf("Cancelled task %q in project %q (moved to cancelled/)", args.Task, project), nil

			default:
				return "", fmt.Errorf("unknown action %q — expected create, update_status, retire, or cancel", args.Action)
			}
		},
	}
}

// replaceStatus replaces the Status: line in a task file, preserving format.
func replaceStatus(content, newStatus string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if statusRegexp.MatchString(line) {
			// Preserve the format (plain "Status:" vs "## Status:")
			if strings.HasPrefix(strings.TrimSpace(line), "## ") {
				lines[i] = "## Status: " + newStatus
			} else {
				lines[i] = "Status: " + newStatus
			}
			return strings.Join(lines, "\n")
		}
	}
	// No status line found — prepend after title
	for i, line := range lines {
		if titleRegexp.MatchString(line) {
			// Insert status after the title line
			rest := make([]string, len(lines[i+1:]))
			copy(rest, lines[i+1:])
			lines = append(lines[:i+1], "Status: "+newStatus)
			lines = append(lines, rest...)
			return strings.Join(lines, "\n")
		}
	}
	// No title either — just prepend
	return "Status: " + newStatus + "\n" + content
}

// NewRefreshIndexTool creates the vv_refresh_index tool.
func NewRefreshIndexTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_refresh_index",
			Description: "Rebuild the session index and regenerate per-project history.md context files.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name (informational only — rebuild always indexes all projects)."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			idx, count, err := index.Rebuild(cfg.ProjectsDir(), cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("rebuild index: %w", err)
			}

			if saveErr := idx.Save(); saveErr != nil {
				return "", fmt.Errorf("save index: %w", saveErr)
			}

			opts := index.ContextOptions{
				AlertThreshold:       cfg.Friction.AlertThreshold,
				TimelineRecentDays:   cfg.History.TimelineRecentDays,
				TimelineWindowDays:   cfg.History.TimelineWindowDays,
				DecisionStaleDays:    cfg.History.DecisionStaleDays,
				KeyFilesRecencyBoost: cfg.History.KeyFilesRecencyBoost,
			}
			genResult, err := index.GenerateContext(idx, cfg.VaultPath, opts)
			if err != nil {
				return "", fmt.Errorf("generate context: %w", err)
			}

			projects := idx.Projects()

			result := struct {
				SessionsIndexed int      `json:"sessions_indexed"`
				ProjectsUpdated int      `json:"projects_updated"`
				Projects        []string `json:"projects"`
			}{
				SessionsIndexed: count,
				ProjectsUpdated: genResult.ProjectsUpdated,
				Projects:        projects,
			}
			if result.Projects == nil {
				result.Projects = []string{}
			}

			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal result: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}
