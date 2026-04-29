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

	"github.com/suykerbuyk/vibe-vault/internal/config"
	vvcontext "github.com/suykerbuyk/vibe-vault/internal/context"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// NewUpdateResumeTool creates the vv_update_resume tool.
func NewUpdateResumeTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_update_resume",
			Description: "Update a section of resume.md for a project. Replaces the body of an existing ## section. On v10+ projects, writes to 'Current State' must contain only single-line invariant bullets (**Key:** value, ≤200 runes trailing, whitelisted first word); narrative or capability prose belongs in agentctx/features.md.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"section": {
						"type": "string",
						"description": "The section heading to update (e.g. 'Current State' or 'Open Threads'). Must already exist in resume.md."
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

			// On v10+ projects, the Current State section is under the
			// invariants-only contract. Reject narrative prose; route it to
			// features.md. Pre-v10 projects are grandfathered via the silent
			// skip on ReadVersion failure or SchemaVersion < 10.
			if args.Section == vvcontext.CurrentStateSection {
				agentctxDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx")
				vf, verr := vvcontext.ReadVersion(agentctxDir)
				if verr == nil && vf.SchemaVersion >= 10 {
					if badLine, ok := vvcontext.ValidateCurrentStateBody(args.Content); !ok {
						return "", fmt.Errorf(
							"vv_update_resume: Current State under v10 contract accepts "+
								"only single-line invariant bullets (**Key:** value, ≤200 "+
								"runes trailing, whitelisted first word); rejected line: %q "+
								"— route narrative or capability prose to agentctx/features.md",
							truncateForError(badLine))
					}
				}
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

			updated, err := mdutil.ReplaceSectionBody(string(data), args.Section, args.Content)
			if err != nil {
				return "", err
			}

			if err := mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644); err != nil {
				return "", fmt.Errorf("write resume: %w", err)
			}

			// D4b auto-heal: re-render marker-bounded state-derived
			// sub-regions of resume.md from filesystem ground truth.
			// This preserves the Step-9 ApplyBundle semantics now that
			// the bundle path is being retired in favour of the
			// surgical canonical path.
			if healErr := autoHealResumeStateBlocks(cfg, project); healErr != nil {
				return "", fmt.Errorf("auto-heal: %w", healErr)
			}

			return fmt.Sprintf("Updated section %q in resume.md for project %q", args.Section, project), nil
		},
	}
}

// truncateForError shortens a rejected-content line for inclusion in an error
// message. Rune-based so UTF-8 content cannot split a code point.
func truncateForError(s string) string {
	const maxRunes = 120
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

var iterationRegexp = regexp.MustCompile(`^### Iteration (\d+)`)

// iterationHeading builds the canonical heading line for an iteration.
func iterationHeading(num int, title, date string) string {
	return fmt.Sprintf("### Iteration %d — %s (%s)", num, title, date)
}

// provenanceTrailer returns an HTML-comment trailer suitable for appending
// after an iteration narrative, or "" if all stamped fields are empty.
// Format: "\n\n<!-- recorded: host=H user=U cwd=C origin=P -->" with each
// token omitted when its value is empty.
func provenanceTrailer(p meta.Provenance, vaultPath string) string {
	cwd := meta.SanitizeCWDForEmit(p.CWD, vaultPath)
	var origin string
	if cwd != "" {
		origin = session.DetectProject(p.CWD)
	}

	var parts []string
	if p.Host != "" {
		parts = append(parts, "host="+p.Host)
	}
	if p.User != "" {
		parts = append(parts, "user="+p.User)
	}
	if cwd != "" {
		parts = append(parts, "cwd="+cwd)
	}
	if origin != "" {
		parts = append(parts, "origin="+origin)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n<!-- recorded: " + strings.Join(parts, " ") + " -->"
}

// BuildIterationBlock constructs the canonical iteration block string
// (heading + narrative + provenance trailer) that vv_append_iteration writes
// to iterations.md. The returned string is ready for direct appending after a
// newline-terminated document. It does NOT include a leading blank line.
//
// iterNum must be > 0. date is YYYY-MM-DD; if empty today's date is used.
// vaultPath is passed to provenanceTrailer for CWD sanitisation.
func BuildIterationBlock(iterNum int, title, narrative, date, vaultPath string) string {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	heading := iterationHeading(iterNum, title, date)
	body := strings.TrimRight(narrative, "\n")
	trailer := provenanceTrailer(meta.Stamp(), vaultPath)
	return fmt.Sprintf("\n%s\n\n%s%s\n", heading, body, trailer)
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
			narrative := strings.TrimRight(args.Narrative, "\n")
			trailer := provenanceTrailer(meta.Stamp(), cfg.VaultPath)
			block := fmt.Sprintf("\n%s\n\n%s%s\n", heading, narrative, trailer)

			// Ensure content ends with newline before appending
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += block

			if err := mdutil.AtomicWriteFile(absPath, []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("write iterations: %w", err)
			}

			// D4b auto-heal: re-render marker-bounded state-derived
			// sub-regions of resume.md from filesystem ground truth.
			// Iteration counts and project history both pull from
			// iterations.md, so the marker block converges with the
			// freshly-appended iteration on every call.
			if healErr := autoHealResumeStateBlocks(cfg, project); healErr != nil {
				return "", fmt.Errorf("auto-heal: %w", healErr)
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
				if err := mdutil.AtomicWriteFile(taskPath, []byte(args.Content), 0o644); err != nil {
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
				if err := mdutil.AtomicWriteFile(taskPath, []byte(updated), 0o644); err != nil {
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
				if err := mdutil.AtomicWriteFile(donePath, []byte(updated), 0o644); err != nil {
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
				if err := mdutil.AtomicWriteFile(cancelledPath, []byte(updated), 0o644); err != nil {
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
