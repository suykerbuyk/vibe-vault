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
	SessionID   string    `json:"session_id"`
	NotePath    string    `json:"note_path"`              // Relative to vault root
	Project     string    `json:"project"`
	Domain      string    `json:"domain"`
	Date        string    `json:"date"`                   // YYYY-MM-DD
	Iteration   int       `json:"iteration"`              // Day iteration counter
	Title       string    `json:"title"`
	Model       string    `json:"model,omitempty"`
	Duration    int       `json:"duration_minutes,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Summary     string   `json:"summary,omitempty"`
	Decisions   []string `json:"decisions,omitempty"`
	OpenThreads []string `json:"open_threads,omitempty"`
	Tag         string   `json:"tag,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Commits        []string       `json:"commits,omitempty"`
	Branch         string         `json:"branch,omitempty"`
	TranscriptPath string         `json:"transcript_path,omitempty"`
	Checkpoint     bool           `json:"checkpoint,omitempty"`
	ToolCounts     map[string]int `json:"tool_counts,omitempty"`
	ToolUses       int            `json:"tool_uses,omitempty"`
	TokensIn       int            `json:"tokens_in,omitempty"`
	TokensOut      int            `json:"tokens_out,omitempty"`
	Messages       int            `json:"messages,omitempty"`
	Corrections    int            `json:"corrections,omitempty"`
	FrictionScore  int            `json:"friction_score,omitempty"`
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
