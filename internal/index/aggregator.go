// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"encoding/json"
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

// archiveDirName is the legacy flat archive subtree produced by Phase 1's
// migration (`vv staging migrate`). Treated as a non-host pseudo-segment
// — its contents are walked via the per-project flat fallback and never
// participate in per-host aggregation (no host attribution).
const archiveDirName = "_pre-staging-archive"

// perHostIndexFile is the filename written by Phase 3's mirror under each
// per-host subtree. The aggregator reads this as a fast path when its
// mtime is newer than every `.md` mtime in the same subtree.
const perHostIndexFile = "index.json"

// AggregateProject builds an Index covering every session note under a
// single project's `Projects/<project>/sessions/` tree. It unifies three
// concrete layouts produced by Phase 1–3 of vault-two-tier:
//
//   - Per-host (β2):   Projects/<p>/sessions/<host>/<date>/*.md
//   - Archive (β2):    Projects/<p>/sessions/_pre-staging-archive/*.md
//   - Flat (legacy):   Projects/<p>/sessions/*.md
//
// For per-host subtrees the aggregator first tries a fast path: read
// `<host>/index.json` and trust its `{session_id: relpath}` map IFF the
// index file's mtime is newer than every `.md` mtime under `<host>/`.
// Otherwise it falls back to a full walk via noteparse — the same logic
// `Rebuild` uses for the legacy flat layout, factored to share with the
// project-scoped aggregator.
//
// Every emitted SessionEntry's NotePath is vault-relative
// (`Projects/<p>/sessions/<host>/<rel>` or `Projects/<p>/sessions/<rel>`
// for archive/flat). Phase 2's hook layer writes ABSOLUTE staging paths
// into the index at capture time; once the operator runs `vv vault
// sync-sessions` followed by `vv index --rebuild`, the aggregator's
// vault-relative entries OVERWRITE those absolute paths, fully
// migrating the index to vault-relative form.
//
// Per-host attribution is recorded on each SessionEntry's Host field
// (added in Phase 4). Archive and flat entries leave Host empty — they
// pre-date per-host bucketing.
//
// SessionID collisions between hosts surface as a non-nil error naming
// both hostnames. SessionIDs are UUIDs and collisions are not expected
// in practice; defense-in-depth catches misconfigured fixtures and
// would-be silent overwrites.
//
// vaultPath must be the vault root (the directory containing
// `Projects/`). project must be the project's directory name under
// `Projects/`. A non-existent project tree returns an empty Index with
// no error — symmetric with `Rebuild` on an empty projects dir.
func AggregateProject(vaultPath, project string) (*Index, error) {
	if vaultPath == "" {
		return nil, fmt.Errorf("aggregator: vaultPath is empty")
	}
	if project == "" {
		return nil, fmt.Errorf("aggregator: project is empty")
	}

	idx := &Index{
		path:    filepath.Join(vaultPath, ".vibe-vault", "session-index.json"),
		Entries: make(map[string]SessionEntry),
	}

	sessionsDir := filepath.Join(vaultPath, "Projects", project, "sessions")
	st, err := os.Stat(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return idx, nil
		}
		return nil, fmt.Errorf("stat sessions dir %s: %w", sessionsDir, err)
	}
	if !st.IsDir() {
		return idx, nil
	}

	// hostOf records which subtree owns a given SessionID so we can
	// produce a useful collision error without re-walking everything.
	hostOf := map[string]string{}

	addEntry := func(host string, entry SessionEntry) error {
		if existing, ok := hostOf[entry.SessionID]; ok && existing != host {
			return fmt.Errorf("aggregator: session_id %s appears in both %s and %s",
				entry.SessionID, existing, host)
		}
		hostOf[entry.SessionID] = host
		entry.Host = host
		idx.Entries[entry.SessionID] = entry
		return nil
	}

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir %s: %w", sessionsDir, err)
	}

	// 1) Per-host subtrees: any directory under sessions/ whose name is
	//    NOT _pre-staging-archive. Each such directory is treated as one
	//    host; we try the fast path (index.json + mtime check) first and
	//    fall back to a full walk on miss.
	// 2) Flat-layout files at sessions/ top level (and the
	//    _pre-staging-archive/ subtree) are picked up by a single
	//    project-scoped fallback walk that reuses the same per-file
	//    parser as the per-host fallback.

	flatNeedsWalk := false
	for _, e := range entries {
		if e.IsDir() {
			name := e.Name()
			if name == archiveDirName {
				flatNeedsWalk = true
				continue
			}
			// Treat as a host subtree.
			hostDir := filepath.Join(sessionsDir, name)
			used, hostErr := aggregateHost(vaultPath, project, name, hostDir, addEntry)
			if hostErr != nil {
				return nil, hostErr
			}
			_ = used
			continue
		}
		// Top-level .md file directly under sessions/ — legacy flat layout.
		if filepath.Ext(e.Name()) == ".md" {
			flatNeedsWalk = true
		}
	}

	if flatNeedsWalk {
		if err := walkFlatLayout(vaultPath, project, sessionsDir, addEntry); err != nil {
			return nil, err
		}
	}

	return idx, nil
}

// aggregateHost emits one entry per session under hostDir. Returns true
// when the fast path was used (the per-host index.json existed and was
// fresher than every .md in the subtree). False indicates a fallback
// walk. The returned bool exists for the Phase 4 fast-path tests; it is
// not load-bearing for production callers, who only care about the
// emitted entries.
func aggregateHost(vaultPath, project, host, hostDir string,
	addEntry func(string, SessionEntry) error,
) (bool, error) {
	idxPath := filepath.Join(hostDir, perHostIndexFile)
	if entries, ok := tryFastPath(hostDir, idxPath); ok {
		return true, emitFastPath(vaultPath, project, host, hostDir, entries, addEntry)
	}
	return false, emitFallbackWalk(vaultPath, project, host, hostDir, addEntry)
}

// tryFastPath returns the per-host {session_id: relpath} map IFF
// `<hostDir>/index.json` exists AND its mtime is strictly newer than the
// newest `.md` mtime under hostDir. A missing file, parse error, or
// stale mtime all collapse to (nil, false) so the caller falls back to a
// full walk — never silently miss a note because the per-host index is
// out of date.
func tryFastPath(hostDir, idxPath string) (map[string]string, bool) {
	idxStat, err := os.Stat(idxPath)
	if err != nil {
		return nil, false
	}
	if !idxStat.Mode().IsRegular() {
		return nil, false
	}

	newestMD, walkErr := newestMarkdownMtime(hostDir, idxPath)
	if walkErr != nil {
		return nil, false
	}
	// Strict >: if the index was written in the same instant as the
	// newest .md (Phase 3's mirror does exactly this), the index reflects
	// that .md and the fast path is safe.
	if newestMD.After(idxStat.ModTime()) {
		return nil, false
	}

	body, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, false
	}
	var entries map[string]string
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, false
	}
	return entries, true
}

// newestMarkdownMtime returns the latest mtime of any `.md` file under
// hostDir, ignoring idxPath itself. Zero time when no .md files exist.
func newestMarkdownMtime(hostDir, idxPath string) (time.Time, error) {
	var newest time.Time
	err := filepath.WalkDir(hostDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != hostDir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if path == idxPath {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest, err
}

// emitFastPath converts each entry from the per-host index into a
// SessionEntry. Phase 3 writes a minimal `{session_id: relpath}` map
// (Phase 4 design choice (a) per the plan); we enrich each row by
// parsing the underlying .md so the unified index has the full
// SessionEntry shape callers expect. Notes that fail to parse or whose
// frontmatter session_id mismatches the index key are skipped with a
// log line — the per-host index is a hint, not authoritative.
func emitFastPath(vaultPath, project, host, hostDir string,
	entries map[string]string,
	addEntry func(string, SessionEntry) error,
) error {
	for sid, rel := range entries {
		// Reject path-escape attempts. Forward-slash is the on-disk
		// shape (Phase 3 writes filepath.ToSlash). Reject if the
		// filepath.Rel of the joined absolute path back to hostDir
		// would step outside hostDir.
		absPath := filepath.Join(hostDir, filepath.FromSlash(rel))
		relCheck, err := filepath.Rel(hostDir, absPath)
		if err != nil || strings.HasPrefix(relCheck, "..") {
			log.Printf("aggregator: skip per-host entry %s/%s: path escape", host, sid)
			continue
		}
		entry, err := buildEntryFromFile(vaultPath, project, host, absPath)
		if err != nil {
			log.Printf("aggregator: skip per-host entry %s/%s: %v", host, sid, err)
			continue
		}
		if entry == nil {
			continue
		}
		// Defensive: per-host index keys are session_ids; trust the
		// frontmatter when they disagree, but log so divergence is
		// visible.
		if entry.SessionID != sid {
			log.Printf("aggregator: per-host index key %s/%s mismatches frontmatter session_id %s; using frontmatter",
				host, sid, entry.SessionID)
		}
		if err := addEntry(host, *entry); err != nil {
			return err
		}
	}
	return nil
}

// emitFallbackWalk parses every `.md` under hostDir via noteparse and
// emits a SessionEntry per valid note. Mirrors Rebuild's parsing logic
// but is project-scoped — the project arg is passed through (and
// cross-checked against frontmatter) rather than re-derived from the
// path on every file.
func emitFallbackWalk(vaultPath, project, host, hostDir string,
	addEntry func(string, SessionEntry) error,
) error {
	return filepath.WalkDir(hostDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != hostDir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		// Skip the per-host index file if it's renamed to .md.
		if d.Name() == perHostIndexFile {
			return nil
		}
		// Filename-shape gate: skip README.md and other non-session
		// files that happen to live under sessions/.
		if _, _, _, ok := render.ParseSessionFilename(d.Name()); !ok {
			return nil
		}
		entry, err := buildEntryFromFile(vaultPath, project, host, path)
		if err != nil {
			log.Printf("aggregator: skip %s: %v", path, err)
			return nil
		}
		if entry == nil {
			return nil
		}
		return addEntry(host, *entry)
	})
}

// walkFlatLayout walks `Projects/<p>/sessions/` for top-level .md files
// AND the `_pre-staging-archive/` subtree, emitting entries with empty
// Host (these layouts pre-date per-host bucketing). It deliberately
// skips any subdirectory that is NOT `_pre-staging-archive` to avoid
// double-counting per-host subtrees, which the caller already walks.
func walkFlatLayout(vaultPath, project, sessionsDir string,
	addEntry func(string, SessionEntry) error,
) error {
	return filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == sessionsDir {
				return nil
			}
			// Only descend into _pre-staging-archive/. Per-host subtrees
			// are handled by aggregateHost; descending here would
			// double-count them.
			if d.Name() == archiveDirName {
				return nil
			}
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		if _, _, _, ok := render.ParseSessionFilename(d.Name()); !ok {
			return nil
		}
		entry, err := buildEntryFromFile(vaultPath, project, "", path)
		if err != nil {
			log.Printf("aggregator: skip %s: %v", path, err)
			return nil
		}
		if entry == nil {
			return nil
		}
		return addEntry("", *entry)
	})
}

// buildEntryFromFile parses one note via noteparse and constructs a
// SessionEntry whose NotePath is vault-relative. Returns (nil, nil) for
// notes that pass parse but fail validation (no session_id, no project
// frontmatter, project mismatch); those are logged + skipped, matching
// Rebuild's defensive-skip semantics.
//
// The host arg becomes the SessionEntry.Host field; pass "" for
// archive/flat entries that pre-date per-host bucketing.
func buildEntryFromFile(vaultPath, project, host, absPath string) (*SessionEntry, error) {
	note, err := noteparse.ParseFile(absPath)
	if err != nil {
		return nil, err
	}
	if note == nil || note.SessionID == "" {
		log.Printf("aggregator: skip %s: no session_id", absPath)
		return nil, nil
	}
	if note.Project == "" {
		log.Printf("aggregator: skip %s: no project in frontmatter", absPath)
		return nil, nil
	}
	if note.Project != project {
		log.Printf("aggregator: skip %s: frontmatter project %q != aggregator project %q",
			absPath, note.Project, project)
		return nil, nil
	}

	relPath, relErr := filepath.Rel(vaultPath, absPath)
	if relErr != nil {
		return nil, fmt.Errorf("rel path %s: %w", absPath, relErr)
	}
	relPath = filepath.ToSlash(relPath)

	iteration := 0
	if note.Iteration != "" {
		iteration, _ = strconv.Atoi(note.Iteration)
	}

	entry := SessionEntry{
		SessionID:    note.SessionID,
		NotePath:     relPath,
		Project:      note.Project,
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
		Host:         host,
	}

	if t, ok := note.Frontmatter["title"]; ok && t != "" {
		entry.Title = t
	} else {
		entry.Title = note.Summary
	}

	if d, ok := note.Frontmatter["duration_minutes"]; ok {
		entry.Duration, _ = strconv.Atoi(d)
	}
	if note.Date != "" {
		if parsed, err := time.Parse("2006-01-02", note.Date); err == nil {
			entry.CreatedAt = parsed
		}
	}
	if tu, ok := note.Frontmatter["tool_uses"]; ok {
		entry.ToolUses, _ = strconv.Atoi(tu)
	}
	if ti, ok := note.Frontmatter["tokens_in"]; ok {
		entry.TokensIn, _ = strconv.Atoi(ti)
	}
	if to, ok := note.Frontmatter["tokens_out"]; ok {
		entry.TokensOut, _ = strconv.Atoi(to)
	}
	if msgs, ok := note.Frontmatter["messages"]; ok {
		entry.Messages, _ = strconv.Atoi(msgs)
	}
	if fs, ok := note.Frontmatter["friction_score"]; ok {
		entry.FrictionScore, _ = strconv.Atoi(fs)
	}
	if corr, ok := note.Frontmatter["corrections"]; ok {
		entry.Corrections, _ = strconv.Atoi(corr)
	}
	if status, ok := note.Frontmatter["status"]; ok && status == "checkpoint" {
		entry.Checkpoint = true
	}

	return &entry, nil
}
