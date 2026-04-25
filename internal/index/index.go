// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// SessionEntry represents one session in the index.
type SessionEntry struct {
	SessionID        string            `json:"session_id"`
	NotePath         string            `json:"note_path"` // Relative to vault root
	Project          string            `json:"project"`
	Domain           string            `json:"domain"`
	Date             string            `json:"date"`      // YYYY-MM-DD
	Iteration        int               `json:"iteration"` // Day iteration counter
	Title            string            `json:"title"`
	Model            string            `json:"model,omitempty"`
	Duration         int               `json:"duration_minutes,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	Summary          string            `json:"summary,omitempty"`
	Decisions        []string          `json:"decisions,omitempty"`
	OpenThreads      []string          `json:"open_threads,omitempty"`
	Tag              string            `json:"tag,omitempty"`
	FilesChanged     []string          `json:"files_changed,omitempty"`
	Commits          []string          `json:"commits,omitempty"`
	Branch           string            `json:"branch,omitempty"`
	TranscriptPath   string            `json:"transcript_path,omitempty"`
	Checkpoint       bool              `json:"checkpoint,omitempty"`
	ToolCounts       map[string]int    `json:"tool_counts,omitempty"`
	ToolUses         int               `json:"tool_uses,omitempty"`
	TokensIn         int               `json:"tokens_in,omitempty"`
	TokensOut        int               `json:"tokens_out,omitempty"`
	Messages         int               `json:"messages,omitempty"`
	Corrections      int               `json:"corrections,omitempty"`
	FrictionScore    int               `json:"friction_score,omitempty"`
	EstimatedCostUSD float64           `json:"estimated_cost_usd,omitempty"`
	ParentUUID       string            `json:"parent_uuid,omitempty"` // external entry UUID (continuation)
	Source           string            `json:"source,omitempty"`      // "zed", etc.; empty = "claude-code"
	Context          *ContextAvailable `json:"context,omitempty"`     // what context was available at capture time
}

// ContextAvailable records what project context existed when a session was captured.
// Used for measuring whether context availability correlates with session outcomes.
type ContextAvailable struct {
	HasHistory      bool `json:"has_history"`                // history.md existed for the project
	HasKnowledge    bool `json:"has_knowledge"`              // knowledge.md existed and was non-empty
	HistorySessions int  `json:"history_sessions,omitempty"` // number of sessions in the index for this project at capture time
}

// SourceName returns the human-readable source name.
// Returns Source if set, otherwise "claude-code" for backward compatibility.
func (e SessionEntry) SourceName() string {
	if e.Source != "" {
		return e.Source
	}
	return "claude-code"
}

// Index manages the session-index.json file.
type Index struct {
	path    string
	Entries map[string]SessionEntry `json:"entries"` // keyed by session_id
}

// Load reads the index from disk, creating an empty one if it doesn't exist.
func Load(stateDir string) (*Index, error) {
	path := filepath.Join(stateDir, "session-index.json")

	idx := &Index{
		path:    path,
		Entries: make(map[string]SessionEntry),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return idx, nil
		}
		return nil, fmt.Errorf("read index: %w", err)
	}

	if err := json.Unmarshal(data, &idx.Entries); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}

	return idx, nil
}

// Save writes the index to disk.
func (idx *Index) Save() error {
	if err := os.MkdirAll(filepath.Dir(idx.path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	data, err := json.MarshalIndent(idx.Entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	return os.WriteFile(idx.path, data, 0o644)
}

// Add inserts or updates a session entry.
func (idx *Index) Add(entry SessionEntry) {
	idx.Entries[entry.SessionID] = entry
}

// Has checks if a session is already indexed.
func (idx *Index) Has(sessionID string) bool {
	_, ok := idx.Entries[sessionID]
	return ok
}

// NextIteration returns the next iteration number for a project on a given date.
func (idx *Index) NextIteration(project, date string) int {
	max := 0
	for _, e := range idx.Entries {
		if e.Project == project && e.Date == date && e.Iteration > max {
			max = e.Iteration
		}
	}
	return max + 1
}

// FilterBySource returns entries matching the given source name.
// Empty or "all" returns entries unchanged.
func FilterBySource(entries map[string]SessionEntry, source string) map[string]SessionEntry {
	if source == "" || source == "all" {
		return entries
	}
	filtered := make(map[string]SessionEntry)
	for id, e := range entries {
		if e.SourceName() == source {
			filtered[id] = e
		}
	}
	return filtered
}

// PreviousSession returns the most recent session for a project before the given time.
func (idx *Index) PreviousSession(project string, before time.Time) *SessionEntry {
	var candidates []SessionEntry
	for _, e := range idx.Entries {
		if e.Project == project && e.CreatedAt.Before(before) {
			candidates = append(candidates, e)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
	})

	return &candidates[0]
}

// ProjectSessionCount returns the number of sessions for a given project.
func (idx *Index) ProjectSessionCount(project string) int {
	count := 0
	for _, e := range idx.Entries {
		if e.Project == project {
			count++
		}
	}
	return count
}

// BackfillContextResult holds counts from a BackfillContext operation.
type BackfillContextResult struct {
	Updated int
	Skipped int
}

// BackfillContext populates ContextAvailable on entries that lack it,
// using (Date, Iteration) ordering to compute HistorySessions.
// HasHistory/HasKnowledge are set false (not derivable from index).
// If overwrite is true, re-computes even entries that already have Context.
func (idx *Index) BackfillContext(overwrite bool) BackfillContextResult {
	// Collect all entries into a slice with their IDs
	type idEntry struct {
		id    string
		entry SessionEntry
	}
	all := make([]idEntry, 0, len(idx.Entries))
	for id, e := range idx.Entries {
		all = append(all, idEntry{id: id, entry: e})
	}

	// Sort by (Project, Date, Iteration) ascending
	sort.Slice(all, func(i, j int) bool {
		if all[i].entry.Project != all[j].entry.Project {
			return all[i].entry.Project < all[j].entry.Project
		}
		if all[i].entry.Date != all[j].entry.Date {
			return all[i].entry.Date < all[j].entry.Date
		}
		return all[i].entry.Iteration < all[j].entry.Iteration
	})

	var result BackfillContextResult
	counts := make(map[string]int) // per-project running count

	for _, ie := range all {
		count := counts[ie.entry.Project]

		if ie.entry.Context != nil && !overwrite {
			// Already has context, skip but still increment
			counts[ie.entry.Project] = count + 1
			result.Skipped++
			continue
		}

		ie.entry.Context = &ContextAvailable{
			HasHistory:      false,
			HasKnowledge:    false,
			HistorySessions: count,
		}
		idx.Entries[ie.id] = ie.entry
		counts[ie.entry.Project] = count + 1
		result.Updated++
	}

	return result
}
