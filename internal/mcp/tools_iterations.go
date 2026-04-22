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

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// iterationHeadingRegexp matches a full iteration heading line:
//
//	### Iteration N — Title (YYYY-MM-DD)
//
// Title may itself contain em dashes (iter 118's title does), so the date in
// parentheses is the anchor and the title capture is non-greedy.
var iterationHeadingRegexp = regexp.MustCompile(`^### Iteration (\d+)\s*—\s*(.+?)\s*\((\d{4}-\d{2}-\d{2})\)\s*$`)

// Iteration is one parsed entry from iterations.md. Narrative uses omitempty
// so the compact "table" response format can drop narrative bodies cleanly.
type Iteration struct {
	Number    int    `json:"number"`
	Date      string `json:"date"`
	Title     string `json:"title"`
	Narrative string `json:"narrative,omitempty"`
}

// parseIterations walks an iterations.md body and returns structured entries
// in document order. Content before the first "### Iteration N" heading is
// preamble and ignored. Each entry's narrative is everything between its
// heading and the next heading (or EOF), with leading/trailing blank lines
// trimmed.
func parseIterations(content string) []Iteration {
	var out []Iteration
	var current *Iteration
	var buf strings.Builder

	flush := func() {
		if current == nil {
			return
		}
		current.Narrative = strings.TrimSpace(buf.String())
		out = append(out, *current)
		buf.Reset()
		current = nil
	}

	for _, line := range strings.Split(content, "\n") {
		if m := iterationHeadingRegexp.FindStringSubmatch(line); m != nil {
			flush()
			num, _ := strconv.Atoi(m[1])
			current = &Iteration{
				Number: num,
				Title:  strings.TrimSpace(m[2]),
				Date:   m[3],
			}
			continue
		}
		if current != nil {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()
	return out
}

// NewGetIterationsTool creates the vv_get_iterations tool.
func NewGetIterationsTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_iterations",
			Description: "Get iteration narratives from a project's iterations.md. Defaults to the 10 most recent entries in compact table format. Use format=\"full\" to include narrative bodies; use since_iteration to fetch a specific range.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"limit": {
						"type": "integer",
						"description": "Maximum number of iterations to return, newest-first. Default: 10."
					},
					"since_iteration": {
						"type": "integer",
						"description": "Only return iterations with number >= this value. Limit still applies to the filtered set."
					},
					"format": {
						"type": "string",
						"enum": ["table", "full"],
						"description": "\"table\" returns {number,date,title} only (compact). \"full\" includes the narrative body. Default: \"table\"."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project        string `json:"project"`
				Limit          *int   `json:"limit"`
				SinceIteration *int   `json:"since_iteration"`
				Format         string `json:"format"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			limit := 10
			if args.Limit != nil {
				limit = *args.Limit
				if limit < 1 {
					return "", fmt.Errorf("limit must be >= 1")
				}
			}

			format := args.Format
			if format == "" {
				format = "table"
			}
			if format != "table" && format != "full" {
				return "", fmt.Errorf("invalid format %q — must be \"table\" or \"full\"", format)
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
			if err != nil {
				if os.IsNotExist(err) {
					return "", fmt.Errorf("iterations.md not found for project %q — run `vv context init` first", project)
				}
				return "", fmt.Errorf("read iterations: %w", err)
			}

			all := parseIterations(string(data))
			total := len(all)

			if args.SinceIteration != nil {
				filtered := all[:0]
				for _, it := range all {
					if it.Number >= *args.SinceIteration {
						filtered = append(filtered, it)
					}
				}
				all = filtered
			}

			// Newest-first
			for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
				all[i], all[j] = all[j], all[i]
			}

			if len(all) > limit {
				all = all[:limit]
			}

			if format == "table" {
				for i := range all {
					all[i].Narrative = ""
				}
			}

			result := struct {
				Project    string      `json:"project"`
				Total      int         `json:"total"`
				Returned   int         `json:"returned"`
				Iterations []Iteration `json:"iterations"`
			}{
				Project:    project,
				Total:      total,
				Returned:   len(all),
				Iterations: all,
			}
			if result.Iterations == nil {
				result.Iterations = []Iteration{}
			}

			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
