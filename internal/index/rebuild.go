// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Rebuild walks the Projects directory and dispatches one
// AggregateProject pass per project subdirectory, then merges the
// per-project results into a single index. Layout coverage matches the
// per-project aggregator:
//
//   - Flat (legacy):   Projects/<p>/sessions/<file>.md
//   - Per-host (β2):   Projects/<p>/sessions/<host>/<date>/<file>.md
//   - Archive (β2):    Projects/<p>/sessions/_pre-staging-archive/<file>.md
//
// Phase 4 of vault-two-tier collapsed the historical inline walk into
// AggregateProject so per-host bucketing, fast-path index reads, and
// per-host attribution all live in one place. Rebuild is now a thin
// orchestrator: enumerate projects, aggregate each, reconcile fields
// the notes do not carry (TranscriptPath, ToolCounts) against the prior
// on-disk index, and merge with cross-project collision detection.
//
// Malformed notes are logged and skipped by the aggregator. A
// SessionID that appears in two projects (a fixture / corruption case)
// surfaces as an error — defense-in-depth, since SessionIDs are UUIDs.
func Rebuild(projectsDir, stateDir string) (*Index, int, error) {
	// Load existing index to preserve fields the notes do not carry
	// (TranscriptPath, ToolCounts). Errors collapse to an empty oldIdx;
	// a missing prior index is the common first-run case.
	oldIdx, _ := Load(stateDir)

	idx := &Index{
		path:    filepath.Join(stateDir, "session-index.json"),
		Entries: make(map[string]SessionEntry),
	}

	count := 0

	// AggregateProject expects vaultPath = parent(projectsDir). The
	// historical contract for Rebuild is that projectsDir is always
	// `<vaultPath>/Projects`; the aggregator joins back the same
	// `Projects/<project>/sessions/...` shape.
	vaultPath := filepath.Dir(projectsDir)

	projectEntries, readErr := os.ReadDir(projectsDir)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return idx, 0, nil
		}
		return nil, 0, fmt.Errorf("read projects dir %s: %w", projectsDir, readErr)
	}

	for _, pe := range projectEntries {
		if !pe.IsDir() {
			continue
		}
		projectIdx, aggErr := AggregateProject(vaultPath, pe.Name())
		if aggErr != nil {
			return nil, 0, fmt.Errorf("aggregate %s: %w", pe.Name(), aggErr)
		}
		for sid, entry := range projectIdx.Entries {
			if existing, ok := idx.Entries[sid]; ok {
				// Cross-project SessionID collisions are a corruption /
				// migration artifact (e.g. two clones of the same
				// project with overlapping captures). The aggregator's
				// per-project pass already enforces uniqueness within
				// a project's per-host subtrees; cross-project we WARN
				// and let last-write-win, preserving the historical
				// inline-walker behavior so existing operator vaults
				// are not bricked by Phase 4. The intra-project check
				// is the load-bearing one; cross-project would rather
				// be addressed by a project rename / dedupe operation
				// outside of `vv index`.
				log.Printf("rebuild: session_id %s collides between projects %s and %s; keeping last",
					sid, existing.Project, entry.Project)
			}
			if old, ok := oldIdx.Entries[sid]; ok {
				if old.TranscriptPath != "" {
					entry.TranscriptPath = old.TranscriptPath
				}
				if len(old.ToolCounts) > 0 {
					entry.ToolCounts = old.ToolCounts
				}
			}
			idx.Entries[sid] = entry
			count++
		}
	}

	return idx, count, nil
}
