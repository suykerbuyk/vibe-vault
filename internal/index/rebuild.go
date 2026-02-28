package index

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/johns/vibe-vault/internal/noteparse"
)

// Rebuild walks the Sessions directory, parses each note via noteparse,
// and builds an enriched index from scratch. It skips files prefixed with
// underscore and logs malformed notes.
func Rebuild(sessionsDir, stateDir string) (*Index, int, error) {
	// Load existing index to preserve TranscriptPaths (not stored in notes)
	oldIdx, _ := Load(stateDir)

	idx := &Index{
		path:    filepath.Join(stateDir, "session-index.json"),
		Entries: make(map[string]SessionEntry),
	}

	count := 0

	err := filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Only process .md files
		if filepath.Ext(path) != ".md" {
			return nil
		}

		// Skip underscore-prefixed files
		if strings.HasPrefix(info.Name(), "_") {
			return nil
		}

		note, parseErr := noteparse.ParseFile(path)
		if parseErr != nil {
			log.Printf("rebuild: skip %s: %v", path, parseErr)
			return nil
		}

		// Must have a session_id to be valid
		if note.SessionID == "" {
			log.Printf("rebuild: skip %s: no session_id", path)
			return nil
		}

		// Build relative path from parent of sessionsDir
		// sessionsDir is like /vault/Sessions, we want Sessions/project/file.md
		vaultRoot := filepath.Dir(sessionsDir)
		relPath, _ := filepath.Rel(vaultRoot, path)

		// Detect project from directory structure: Sessions/<project>/file.md
		project := note.Project
		if project == "" {
			// Fall back to directory name
			dir := filepath.Dir(path)
			project = filepath.Base(dir)
		}

		iteration := 0
		if note.Iteration != "" {
			iteration, _ = strconv.Atoi(note.Iteration)
		}

		entry := SessionEntry{
			SessionID:    note.SessionID,
			NotePath:     relPath,
			Project:      project,
			Domain:       note.Domain,
			Date:         note.Date,
			Iteration:    iteration,
			Title:        note.Frontmatter["type"],
			Model:        note.Model,
			Summary:      note.Summary,
			Decisions:    note.Decisions,
			OpenThreads:  note.OpenThreads,
			Tag:          note.Tag,
			FilesChanged: note.FilesChanged,
			Branch:       note.Branch,
			Commits:      note.Commits,
		}

		// Extract title from frontmatter or first heading
		if t, ok := note.Frontmatter["title"]; ok && t != "" {
			entry.Title = t
		} else {
			// Use summary as title fallback
			entry.Title = note.Summary
		}

		// Parse duration
		if d, ok := note.Frontmatter["duration_minutes"]; ok {
			entry.Duration, _ = strconv.Atoi(d)
		}

		// Parse created_at from date + approximate time
		if note.Date != "" {
			t, err := time.Parse("2006-01-02", note.Date)
			if err == nil {
				entry.CreatedAt = t
			}
		}

		// Parse tool_uses from frontmatter
		if tu, ok := note.Frontmatter["tool_uses"]; ok {
			entry.ToolUses, _ = strconv.Atoi(tu)
		}

		// Parse token/message counts from frontmatter
		if ti, ok := note.Frontmatter["tokens_in"]; ok {
			entry.TokensIn, _ = strconv.Atoi(ti)
		}
		if to, ok := note.Frontmatter["tokens_out"]; ok {
			entry.TokensOut, _ = strconv.Atoi(to)
		}
		if msgs, ok := note.Frontmatter["messages"]; ok {
			entry.Messages, _ = strconv.Atoi(msgs)
		}

		// Parse friction fields from frontmatter
		if fs, ok := note.Frontmatter["friction_score"]; ok {
			entry.FrictionScore, _ = strconv.Atoi(fs)
		}

		// Parse status: checkpoint flag
		if status, ok := note.Frontmatter["status"]; ok && status == "checkpoint" {
			entry.Checkpoint = true
		}

		// Preserve fields from old index that aren't stored in notes
		if old, ok := oldIdx.Entries[note.SessionID]; ok {
			if old.TranscriptPath != "" {
				entry.TranscriptPath = old.TranscriptPath
			}
			if len(old.ToolCounts) > 0 {
				entry.ToolCounts = old.ToolCounts
			}
		}

		idx.Entries[note.SessionID] = entry
		count++
		return nil
	})

	if err != nil {
		return nil, 0, fmt.Errorf("walk sessions: %w", err)
	}

	return idx, count, nil
}
