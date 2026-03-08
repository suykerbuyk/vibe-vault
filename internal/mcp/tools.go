// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/inject"
	"github.com/johns/vibe-vault/internal/trends"
)

// NewGetProjectContextTool creates the get_project_context tool.
func NewGetProjectContextTool(stateDir string) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "get_project_context",
			Description: "Get condensed project context including recent sessions, open threads, decisions, and friction trends. Use this to understand what has been happening in a project.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, returns context for all projects."
					},
					"sections": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Sections to include: summary, sessions, threads, decisions, friction. Default: all."
					},
					"max_tokens": {
						"type": "integer",
						"description": "Token budget for output. Default: 2000."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project   string   `json:"project"`
				Sections  []string `json:"sections"`
				MaxTokens int      `json:"max_tokens"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			if args.MaxTokens <= 0 {
				args.MaxTokens = 2000
			}

			idx, err := index.Load(stateDir)
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			trendResult := trends.Compute(idx.Entries, args.Project, 4)

			opts := inject.Opts{
				Project:   args.Project,
				Format:    "json",
				Sections:  args.Sections,
				MaxTokens: args.MaxTokens,
			}

			result := inject.Build(idx.Entries, trendResult, opts)
			output, err := inject.Render(result, opts)
			if err != nil {
				return "", fmt.Errorf("render: %w", err)
			}
			return output, nil
		},
	}
}

// NewListProjectsTool creates the list_projects tool.
func NewListProjectsTool(stateDir string) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "list_projects",
			Description: "List all projects in the vault with session counts and date ranges.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			idx, err := index.Load(stateDir)
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			type projectInfo struct {
				Name             string `json:"name"`
				SessionCount     int    `json:"session_count"`
				FirstSession     string `json:"first_session"`
				LastSession      string `json:"last_session"`
				FrictionDirection string `json:"friction_direction,omitempty"`
			}

			projectMap := make(map[string]*projectInfo)
			for _, e := range idx.Entries {
				pi, ok := projectMap[e.Project]
				if !ok {
					pi = &projectInfo{Name: e.Project}
					projectMap[e.Project] = pi
				}
				pi.SessionCount++
				if pi.FirstSession == "" || e.Date < pi.FirstSession {
					pi.FirstSession = e.Date
				}
				if e.Date > pi.LastSession {
					pi.LastSession = e.Date
				}
			}

			var projects []projectInfo
			for _, pi := range projectMap {
				tr := trends.Compute(idx.Entries, pi.Name, 4)
				for _, m := range tr.Metrics {
					if m.Name == "Friction" {
						pi.FrictionDirection = m.Direction
						break
					}
				}
				projects = append(projects, *pi)
			}

			sort.Slice(projects, func(i, j int) bool {
				return strings.ToLower(projects[i].Name) < strings.ToLower(projects[j].Name)
			})

			data, err := json.MarshalIndent(projects, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}
