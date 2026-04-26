// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// wrap-trace analyses a Claude Code transcript JSONL file and emits a
// per-phase cost decomposition of the /wrap session it contains.
//
// Usage:
//
//	wrap-trace <transcript.jsonl>
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/transcript"
)

// phaseRow is one row of the decomposition table.
type phaseRow struct {
	label     string
	wallSec   float64
	outTokens int
	inTokens  int
	toolTrips int
}

// MeasureResult holds the per-phase decomposition for a single wrap session.
type MeasureResult struct {
	Rows  []phaseRow
	Total phaseRow
}

// Wrap-relevant tool name prefixes.
const (
	toolWrite  = "Write"
	toolEdit   = "Edit"
	toolRead   = "Read"
	toolBash   = "Bash"
	toolSearch = "ToolSearch"
)

// canonicalTool normalises MCP tool names to short labels.
// e.g. "mcp__plugin_vibe-vault_vibe-vault__vv_append_iteration" → "vv_append_iteration"
func canonicalTool(name string) string {
	const pfxMCP = "mcp__plugin_vibe-vault_vibe-vault__"
	if strings.HasPrefix(name, pfxMCP) {
		return name[len(pfxMCP):]
	}
	return name
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: wrap-trace <transcript.jsonl>")
		os.Exit(1)
	}
	path := os.Args[1]

	result, err := Measure(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	printTable(result.Rows, result.Total, path)
}

// Measure parses a transcript and returns the wrap-phase decomposition.
func Measure(path string) (*MeasureResult, error) {
	tr, err := transcript.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse transcript: %w", err)
	}
	return analyse(tr)
}

// analyse finds the wrap window and produces per-phase rows.
func analyse(tr *transcript.Transcript) (*MeasureResult, error) {
	// Collect assistant turns that contain at least one tool call.
	type turn struct {
		ts     time.Time
		end    time.Time // timestamp of next turn's start (for wall-clock)
		tools  []string
		outTok int
		inTok  int
	}

	var turns []turn
	for _, e := range tr.Entries {
		if e.Message == nil || e.Message.Role != "assistant" {
			continue
		}
		tools := transcript.ToolUses(e.Message)
		if len(tools) == 0 {
			continue
		}
		var names []string
		for _, t := range tools {
			names = append(names, t.Name)
		}
		var outTok, inTok int
		if u := e.Message.Usage; u != nil {
			outTok = u.OutputTokens
			inTok = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
		}
		turns = append(turns, turn{
			ts:     e.Timestamp,
			tools:  names,
			outTok: outTok,
			inTok:  inTok,
		})
	}

	if len(turns) == 0 {
		return nil, fmt.Errorf("no assistant turns with tool calls found")
	}

	// Fill in end time (= next turn's start) for wall-clock calculations.
	for i := 0; i < len(turns)-1; i++ {
		turns[i].end = turns[i+1].ts
	}
	turns[len(turns)-1].end = turns[len(turns)-1].ts

	// Identify the wrap window: starts at the first wrap-tool call,
	// ends at the last wrap-tool or commit.msg write.
	// Wrap tools: vv_append_iteration, vv_update_resume, vv_capture_session.
	wrapStart, wrapEnd := -1, -1
	for i, t := range turns {
		for _, name := range t.tools {
			cn := canonicalTool(name)
			isWrap := cn == "vv_append_iteration" ||
				cn == "vv_update_resume" ||
				cn == "vv_capture_session"
			if isWrap {
				if wrapStart == -1 {
					wrapStart = i
				}
				wrapEnd = i
			}
		}
	}
	if wrapStart == -1 {
		return nil, fmt.Errorf("no wrap session detected: no vv_append_iteration or vv_update_resume found")
	}

	wrapTurns := turns[wrapStart : wrapEnd+1]

	// Phase buckets (mapped to rezbldr table rows where possible):
	type phaseBucket int
	const (
		phIterations phaseBucket = iota // vv_append_iteration
		phResume                         // vv_update_resume
		phCommitMsg                      // Write/Edit on commit.msg
		phReadWrite                      // Read on resume/commit.msg before write
		phStage                          // Bash git stage/commit
		phToolSearch                     // ToolSearch schema loads
		phCapture                        // vv_capture_session
		phOther                          // everything else
		numPhases
	)

	phaseNames := [numPhases]string{
		"Compose iterations.md narrative",
		"Compose Open Threads / resume",
		"Compose commit.msg body",
		"Read-before-write commit.msg ceremony",
		"Stage project files",
		"Mid-flow ToolSearch schema loads",
		"vv_capture_session retelling",
		"Other (context/reads/misc)",
	}

	type bucket struct {
		outTok    int
		inTok     int
		toolTrips int
		start     time.Time
		end       time.Time
	}
	buckets := [numPhases]bucket{}

	for _, t := range wrapTurns {
		// Determine the best phase for this turn (priority: capture > iterations > resume > commit > ...)
		ph := phOther
		for _, name := range t.tools {
			cn := canonicalTool(name)
			switch {
			case cn == "vv_capture_session":
				ph = phCapture
			case cn == "vv_append_iteration":
				if ph > phIterations {
					ph = phIterations
				}
			case cn == "vv_update_resume":
				if ph > phResume {
					ph = phResume
				}
			case name == toolWrite || name == toolEdit:
				if ph > phCommitMsg {
					ph = phCommitMsg
				}
			case name == toolBash:
				if ph == phOther {
					ph = phStage
				}
			case name == toolSearch:
				if ph == phOther {
					ph = phToolSearch
				}
			case name == toolRead:
				if ph == phOther {
					ph = phReadWrite
				}
			}
		}

		b := &buckets[ph]
		b.outTok += t.outTok
		b.inTok += t.inTok
		b.toolTrips += len(t.tools)
		if b.start.IsZero() || t.ts.Before(b.start) {
			b.start = t.ts
		}
		end := t.end
		if end.IsZero() {
			end = t.ts
		}
		if b.end.IsZero() || end.After(b.end) {
			b.end = end
		}
	}

	// Build rows (skip empty phases).
	var rows []phaseRow
	var totalRow phaseRow
	totalRow.label = "TOTAL (wrap window)"
	wrapWindowStart := wrapTurns[0].ts
	wrapWindowEnd := wrapTurns[len(wrapTurns)-1].end
	if wrapWindowEnd.IsZero() {
		wrapWindowEnd = wrapTurns[len(wrapTurns)-1].ts
	}
	totalRow.wallSec = wrapWindowEnd.Sub(wrapWindowStart).Seconds()

	for i, b := range buckets {
		if b.toolTrips == 0 {
			continue
		}
		wall := b.end.Sub(b.start).Seconds()
		row := phaseRow{
			label:     phaseNames[i],
			wallSec:   wall,
			outTokens: b.outTok,
			inTokens:  b.inTok,
			toolTrips: b.toolTrips,
		}
		rows = append(rows, row)
		totalRow.outTokens += b.outTok
		totalRow.inTokens += b.inTok
		totalRow.toolTrips += b.toolTrips
	}

	return &MeasureResult{Rows: rows, Total: totalRow}, nil
}

func printTable(rows []phaseRow, total phaseRow, path string) {
	fmt.Printf("## Wrap trace: %s\n\n", path)
	fmt.Printf("Total window: %.0fs | output tokens: %d | tool round-trips: %d\n\n",
		total.wallSec, total.outTokens, total.toolTrips)

	fmt.Printf("| Phase | Wall (s) | Out tokens | %% out | Tool trips | In tokens |\n")
	fmt.Printf("|-------|----------|------------|-------|------------|----------|\n")

	for _, r := range rows {
		pct := 0.0
		if total.outTokens > 0 {
			pct = float64(r.outTokens) / float64(total.outTokens) * 100
		}
		fmt.Printf("| %-40s | %8.0f | %10d | %5.1f%% | %10d | %9d |\n",
			r.label, r.wallSec, r.outTokens, pct, r.toolTrips, r.inTokens)
	}

	fmt.Printf("| %-40s | %8.0f | %10d | %5.1f%% | %10d | %9d |\n",
		total.label, total.wallSec, total.outTokens, 100.0, total.toolTrips, total.inTokens)
}

// MarshalJSON implements json.Marshaler for MeasureResult.
func (m *MeasureResult) MarshalJSON() ([]byte, error) {
	type row struct {
		Label     string  `json:"label"`
		WallSec   float64 `json:"wall_sec"`
		OutTokens int     `json:"out_tokens"`
		InTokens  int     `json:"in_tokens"`
		ToolTrips int     `json:"tool_trips"`
	}
	type result struct {
		Rows  []row `json:"rows"`
		Total row   `json:"total"`
	}
	r := result{
		Total: row{
			Label:     m.Total.label,
			WallSec:   m.Total.wallSec,
			OutTokens: m.Total.outTokens,
			InTokens:  m.Total.inTokens,
			ToolTrips: m.Total.toolTrips,
		},
	}
	for _, ro := range m.Rows {
		r.Rows = append(r.Rows, row{
			Label:     ro.label,
			WallSec:   ro.wallSec,
			OutTokens: ro.outTokens,
			InTokens:  ro.inTokens,
			ToolTrips: ro.toolTrips,
		})
	}
	return json.Marshal(r)
}
