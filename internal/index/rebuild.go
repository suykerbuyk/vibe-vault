// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/noteparse"
	"github.com/suykerbuyk/vibe-vault/internal/render"
)

// sessionsSegment is the path-segment marker the walker uses to detect
// session notes. We require an exact "/sessions/" segment (slash-bounded)
// so a project literally named "sessions" or a path fragment like
// "my-sessions/" never falsely matches. The walker normalizes filepath
// separators to '/' before checking.
const sessionsSegment = "/sessions/"

// Rebuild walks the Projects directory, parses each note via noteparse,
// and builds an enriched index from scratch. It processes .md files whose
// path contains a "/sessions/" segment (relative to the project root); the
// parser confirms candidacy by reading frontmatter. This generalization
// (Phase 1.5 of vault-two-tier) covers all three layouts simultaneously:
//
//   - Flat (legacy):   Projects/<p>/sessions/<file>.md
//   - Per-host (β2):   Projects/<p>/sessions/<host>/<date>/<file>.md
//   - Archive (β2):    Projects/<p>/sessions/_pre-staging-archive/<file>.md
//
// Malformed notes are logged and skipped.
func Rebuild(projectsDir, stateDir string) (*Index, int, error) {
	// Load existing index to preserve TranscriptPaths (not stored in notes)
	oldIdx, _ := Load(stateDir)

	idx := &Index{
		path:    filepath.Join(stateDir, "session-index.json"),
		Entries: make(map[string]SessionEntry),
	}

	count := 0

	err := filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
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

		// Only process files whose path contains a /sessions/ segment.
		// Path-containment (vs the legacy parent-name check) covers flat,
		// per-host, and _pre-staging-archive/ layouts in one rule. We
		// normalize separators so the same check works on Windows.
		normalized := filepath.ToSlash(path)
		if !strings.Contains(normalized, sessionsSegment) {
			return nil
		}

		// Mechanism-1 compat (session-slot-multihost-disambiguation):
		// recognize all three filename shapes — legacy counter,
		// plain timestamp, and timestamp-with-retry-suffix. We do not
		// derive any field from the filename; iteration comes from
		// frontmatter (frontmatter is authoritative). The match is used
		// only to skip files that aren't session notes (e.g., README.md).
		if _, _, _, ok := render.ParseSessionFilename(filepath.Base(path)); !ok {
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

		// Build relative path from parent of projectsDir
		// projectsDir is like /vault/Projects, we want Projects/project/sessions/file.md
		vaultRoot := filepath.Dir(projectsDir)
		relPath, _ := filepath.Rel(vaultRoot, path)

		// Frontmatter is the sole source of truth for the project name.
		// render.SessionNote() writes `project:` unconditionally, so a
		// missing field signals a corrupt or hand-edited note; treat it
		// the same way as a missing session_id (skip + log) rather than
		// falling back to a path-derived guess. The legacy grandparent
		// fallback was dead under the flat layout AND a foot-gun under
		// the per-host layout (would resolve to <host> or <date>, not
		// the project). Removed in Phase 1.5 of vault-two-tier.
		if note.Project == "" {
			log.Printf("rebuild: skip %s: no project in frontmatter", path)
			return nil
		}
		project := note.Project

		// Frontmatter is authoritative for iteration.
		// session-slot-multihost-disambiguation v8 / Mechanism 1: with timestamp
		// filenames there is no counter to recover from the filename; the
		// frontmatter `iteration:` field is the sole source of truth on rebuild.
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
		if corr, ok := note.Frontmatter["corrections"]; ok {
			entry.Corrections, _ = strconv.Atoi(corr)
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
		return nil, 0, fmt.Errorf("walk projects: %w", err)
	}

	return idx, count, nil
}
