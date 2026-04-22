// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package stats

import (
	"fmt"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/narrative"
)

// ToolMetric holds effectiveness data for a single tool.
type ToolMetric struct {
	Name       string
	Uses       int
	Errors     int
	Recoveries int // errors followed by successful retry
	SuccessRate float64
}

// StrugglePattern identifies repeated file editing cycles.
type StrugglePattern struct {
	File   string
	Cycles int // number of Read→Edit cycles
}

// ToolEffectiveness holds the full tool analysis for a session.
type ToolEffectiveness struct {
	Tools     []ToolMetric
	Struggles []StrugglePattern
}

// AnalyzeTools computes tool effectiveness metrics from narrative segments.
// Returns nil if there are no interesting patterns to report.
func AnalyzeTools(segments []narrative.Segment) *ToolEffectiveness {
	toolMap := make(map[string]*ToolMetric)

	// Track file access patterns for struggle detection
	// fileOps stores alternating Read/Edit sequences per file
	fileOps := make(map[string][]string)

	for _, seg := range segments {
		for _, a := range seg.Activities {
			if a.Tool == "" {
				continue
			}

			tm, ok := toolMap[a.Tool]
			if !ok {
				tm = &ToolMetric{Name: a.Tool}
				toolMap[a.Tool] = tm
			}
			tm.Uses++
			if a.IsError {
				tm.Errors++
			}
			if a.Recovered {
				tm.Recoveries++
			}

			// Track file access patterns
			if a.Tool == "Read" || a.Tool == "Edit" || a.Tool == "Write" {
				file := extractFilePath(a.Description)
				if file != "" {
					fileOps[file] = append(fileOps[file], a.Tool)
				}
			}
		}
	}

	if len(toolMap) == 0 {
		return nil
	}

	// Compute success rates
	var tools []ToolMetric
	for _, tm := range toolMap {
		if tm.Uses > 0 {
			tm.SuccessRate = float64(tm.Uses-tm.Errors) / float64(tm.Uses) * 100
		}
		tools = append(tools, *tm)
	}

	// Detect struggle patterns: 3+ Read→Edit/Write cycles on same file
	var struggles []StrugglePattern
	for file, ops := range fileOps {
		cycles := countEditCycles(ops)
		if cycles >= 3 {
			struggles = append(struggles, StrugglePattern{File: file, Cycles: cycles})
		}
	}

	// Only return if there are interesting patterns (errors or struggles)
	hasErrors := false
	for _, tm := range tools {
		if tm.Errors > 0 {
			hasErrors = true
			break
		}
	}

	if !hasErrors && len(struggles) == 0 {
		return nil
	}

	return &ToolEffectiveness{
		Tools:     tools,
		Struggles: struggles,
	}
}

// RenderToolEffectiveness formats the analysis as a markdown section body.
func RenderToolEffectiveness(te *ToolEffectiveness) string {
	if te == nil {
		return ""
	}

	var b strings.Builder

	// Tools with errors
	hasErrorTools := false
	for _, tm := range te.Tools {
		if tm.Errors > 0 {
			if !hasErrorTools {
				b.WriteString("| Tool | Uses | Errors | Success Rate |\n")
				b.WriteString("|------|------|--------|--------------|\n")
				hasErrorTools = true
			}
			fmt.Fprintf(&b, "| %s | %d | %d | %.0f%% |\n",
				tm.Name, tm.Uses, tm.Errors, tm.SuccessRate)
		}
	}

	if hasErrorTools {
		b.WriteString("\n")
	}

	// Struggles
	if len(te.Struggles) > 0 {
		b.WriteString("**Struggle patterns detected:**\n\n")
		for _, s := range te.Struggles {
			fmt.Fprintf(&b, "- `%s` — %d edit cycles\n", s.File, s.Cycles)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// countEditCycles counts Read→(Edit|Write) transitions in the ops sequence.
func countEditCycles(ops []string) int {
	cycles := 0
	for i := 1; i < len(ops); i++ {
		if ops[i-1] == "Read" && (ops[i] == "Edit" || ops[i] == "Write") {
			cycles++
		}
	}
	return cycles
}

// extractFilePath pulls the file path from an activity description.
// Descriptions look like: "Created `internal/auth/handler.go`" or "Modified `main.go`"
func extractFilePath(desc string) string {
	start := strings.Index(desc, "`")
	if start < 0 {
		return ""
	}
	end := strings.Index(desc[start+1:], "`")
	if end < 0 {
		return ""
	}
	return desc[start+1 : start+1+end]
}
