// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package stats

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// ExportEntry is a flattened session record for export.
type ExportEntry struct {
	Date         string  `json:"date"`
	Project      string  `json:"project"`
	SessionID    string  `json:"session_id"`
	Title        string  `json:"title"`
	Tag          string  `json:"tag"`
	Model        string  `json:"model"`
	Branch       string  `json:"branch"`
	Duration     int     `json:"duration_minutes"`
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	Messages     int     `json:"messages"`
	ToolUses     int     `json:"tool_uses"`
	FrictionScore int   `json:"friction_score"`
	Corrections  int     `json:"corrections"`
	CostUSD      float64 `json:"estimated_cost_usd,omitempty"`
}

// csvHeaders are the CSV column names.
var csvHeaders = []string{
	"date", "project", "session_id", "title", "tag", "model", "branch",
	"duration_minutes", "tokens_in", "tokens_out", "messages", "tool_uses",
	"friction_score", "corrections", "estimated_cost_usd",
}

// ExportEntries converts index entries to export entries, sorted by date.
func ExportEntries(entries map[string]index.SessionEntry, project string) []ExportEntry {
	var result []ExportEntry
	for _, e := range entries {
		if project != "" && e.Project != project {
			continue
		}
		result = append(result, ExportEntry{
			Date:          e.Date,
			Project:       e.Project,
			SessionID:     e.SessionID,
			Title:         e.Title,
			Tag:           e.Tag,
			Model:         e.Model,
			Branch:        e.Branch,
			Duration:      e.Duration,
			TokensIn:      e.TokensIn,
			TokensOut:     e.TokensOut,
			Messages:      e.Messages,
			ToolUses:      e.ToolUses,
			FrictionScore: e.FrictionScore,
			Corrections:   e.Corrections,
			CostUSD:       e.EstimatedCostUSD,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Date != result[j].Date {
			return result[i].Date < result[j].Date
		}
		return result[i].SessionID < result[j].SessionID
	})

	return result
}

// ExportJSON renders entries as a JSON array.
func ExportJSON(entries []ExportEntry) (string, error) {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal json: %w", err)
	}
	return string(data) + "\n", nil
}

// ExportCSV renders entries as CSV with a header row.
func ExportCSV(entries []ExportEntry) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)

	if err := w.Write(csvHeaders); err != nil {
		return "", fmt.Errorf("write csv header: %w", err)
	}

	for _, e := range entries {
		row := []string{
			e.Date,
			e.Project,
			e.SessionID,
			e.Title,
			e.Tag,
			e.Model,
			e.Branch,
			fmt.Sprintf("%d", e.Duration),
			fmt.Sprintf("%d", e.TokensIn),
			fmt.Sprintf("%d", e.TokensOut),
			fmt.Sprintf("%d", e.Messages),
			fmt.Sprintf("%d", e.ToolUses),
			fmt.Sprintf("%d", e.FrictionScore),
			fmt.Sprintf("%d", e.Corrections),
			fmt.Sprintf("%.2f", e.CostUSD),
		}
		if err := w.Write(row); err != nil {
			return "", fmt.Errorf("write csv row: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", fmt.Errorf("csv flush: %w", err)
	}

	return b.String(), nil
}
