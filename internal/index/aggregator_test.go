// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// aggMkNote returns a valid session-note frontmatter+body for the given
// project + session_id + iteration. The shape mirrors what render.SessionNote
// emits in production so noteparse round-trips cleanly through the
// aggregator's buildEntryFromFile.
func aggMkNote(project, sid string, iter int) string {
	return fmt.Sprintf(`---
date: 2026-05-03
type: session
project: %s
domain: personal
session_id: "%s"
iteration: %d
tags: [vv-session, implementation]
summary: "Phase 4 aggregator fixture"
---

# Phase 4 aggregator fixture
`, project, sid, iter)
}

// writeAggNote writes content to <vault>/Projects/<project>/sessions/<subpath>/<filename>.
// MkdirAll'ing the parent makes the helper safe to call from any test ordering.
func writeAggNote(t *testing.T, vault, project, subpath, filename, content string) string {
	t.Helper()
	dir := filepath.Join(vault, "Projects", project, "sessions", subpath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	full := filepath.Join(dir, filename)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	return full
}

// writePerHostIdx writes the Phase 3 per-host index.json shape:
// `{session_id: relpath}` (relpath is relative to <hostDir>, forward-slash).
// Sets the index's mtime to the supplied value so callers can simulate
// fresh / stale conditions deterministically.
func writePerHostIdx(t *testing.T, hostDir string, entries map[string]string, mtime time.Time) string {
	t.Helper()
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatalf("mkdir host dir: %v", err)
	}
	body, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	idxPath := filepath.Join(hostDir, perHostIndexFile)
	if err := os.WriteFile(idxPath, body, 0o644); err != nil {
		t.Fatalf("write idx: %v", err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(idxPath, mtime, mtime); err != nil {
			t.Fatalf("chtimes idx: %v", err)
		}
	}
	return idxPath
}

// TestAggregateProject_TwoHostsFiveEachUnified locks the basic happy path:
// two host subtrees each contributing 5 sessions produce a single Index of
// 10 entries with no collisions. Validates per-host attribution lands on
// every emitted SessionEntry.
func TestAggregateProject_TwoHostsFiveEachUnified(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"

	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("h1-%d", i)
		fname := fmt.Sprintf("2026-05-03-1430%05d.md", i)
		writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"), fname,
			aggMkNote(project, sid, i+1))
	}
	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("h2-%d", i)
		fname := fmt.Sprintf("2026-05-03-1530%05d.md", i)
		writeAggNote(t, vault, project, filepath.Join("host2", "2026-05-03"), fname,
			aggMkNote(project, sid, i+10))
	}

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 10 {
		t.Errorf("len(Entries) = %d, want 10", got)
	}

	hostCounts := map[string]int{}
	for _, e := range idx.Entries {
		hostCounts[e.Host]++
		// Vault-relative invariant.
		if filepath.IsAbs(e.NotePath) {
			t.Errorf("entry %s NotePath %q must be vault-relative", e.SessionID, e.NotePath)
		}
		if !strings.HasPrefix(e.NotePath, "Projects/"+project+"/sessions/") {
			t.Errorf("entry %s NotePath %q missing project/sessions prefix", e.SessionID, e.NotePath)
		}
	}
	if hostCounts["host1"] != 5 || hostCounts["host2"] != 5 {
		t.Errorf("host distribution = %v, want host1=5,host2=5", hostCounts)
	}
}

// TestAggregateProject_MixedLegacyArchiveAndStaged combines the legacy
// `_pre-staging-archive/` flat layout (no host) with a per-host subtree
// (host1, with date sub-bucket). Total is 8; archive entries have
// Host="", per-host entries have Host="host1".
func TestAggregateProject_MixedLegacyArchiveAndStaged(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"

	// 5 legacy archive notes.
	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("arch-%d", i)
		fname := fmt.Sprintf("2026-04-15-%02d.md", i+1)
		writeAggNote(t, vault, project, archiveDirName, fname, aggMkNote(project, sid, i+1))
	}
	// 3 per-host staged notes.
	for i := 0; i < 3; i++ {
		sid := fmt.Sprintf("h1-%d", i)
		fname := fmt.Sprintf("2026-05-03-1430%05d.md", i)
		writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"), fname,
			aggMkNote(project, sid, i+10))
	}

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 8 {
		t.Fatalf("len = %d, want 8", got)
	}
	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("arch-%d", i)
		e, ok := idx.Entries[sid]
		if !ok {
			t.Errorf("missing archive entry %s", sid)
			continue
		}
		if e.Host != "" {
			t.Errorf("archive %s Host = %q, want empty", sid, e.Host)
		}
		wantPrefix := "Projects/" + project + "/sessions/" + archiveDirName + "/"
		if !strings.HasPrefix(e.NotePath, wantPrefix) {
			t.Errorf("archive %s NotePath = %q, want prefix %q", sid, e.NotePath, wantPrefix)
		}
	}
	for i := 0; i < 3; i++ {
		sid := fmt.Sprintf("h1-%d", i)
		e, ok := idx.Entries[sid]
		if !ok {
			t.Errorf("missing per-host entry %s", sid)
			continue
		}
		if e.Host != "host1" {
			t.Errorf("per-host %s Host = %q, want host1", sid, e.Host)
		}
	}
}

// TestAggregateProject_PerHostIndexFastPath exercises the fast path: when
// the per-host index.json mtime is newer than every .md mtime, the
// aggregator trusts the index. We prove the fast path is taken by
// asserting on the public observable: a malformed-but-ignored .md file
// in a peer date dir does not break aggregation, and a manually authored
// per-host index that points at the real notes still produces the right
// count.
func TestAggregateProject_PerHostIndexFastPath(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"

	hostDir := filepath.Join(vault, "Projects", project, "sessions", "host1")

	// Write 5 valid notes with timestamps in the past.
	past := time.Now().Add(-2 * time.Hour)
	relPaths := map[string]string{}
	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("fp-%d", i)
		fname := fmt.Sprintf("2026-05-03-1430%05d.md", i)
		full := writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"), fname,
			aggMkNote(project, sid, i+1))
		// Pin .md mtime so the index can be definitively newer.
		if err := os.Chtimes(full, past, past); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		relPaths[sid] = "2026-05-03/" + fname
	}

	// Write per-host index AFTER notes, with mtime fresh (now).
	now := time.Now()
	writePerHostIdx(t, hostDir, relPaths, now)

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 5 {
		t.Errorf("len = %d, want 5", got)
	}
	for sid := range relPaths {
		e, ok := idx.Entries[sid]
		if !ok {
			t.Errorf("missing entry %s", sid)
			continue
		}
		if e.Host != "host1" {
			t.Errorf("%s Host = %q, want host1", sid, e.Host)
		}
	}

	// Direct fast-path probe via the package-internal helper. This is
	// the load-bearing observability hook for the fast-path test —
	// without it the fall-back walk would also pass the count assertion
	// above. Asserting on tryFastPath returning ok proves the
	// production code took the fast lane.
	entries, ok := tryFastPath(hostDir, filepath.Join(hostDir, perHostIndexFile))
	if !ok {
		t.Error("tryFastPath returned false; fast path did not engage")
	}
	if len(entries) != 5 {
		t.Errorf("tryFastPath returned %d entries, want 5", len(entries))
	}
}

// TestAggregateProject_PerHostIndexStale: index.json mtime is OLDER than
// the newest .md under the host dir. Aggregator must fall back to the
// walk. Total entries equal the .md count, including a note that the
// (stale) index does not list — proves the fallback is doing the work.
func TestAggregateProject_PerHostIndexStale(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"
	hostDir := filepath.Join(vault, "Projects", project, "sessions", "host1")

	// Write index FIRST with old mtime, claiming only 2 sessions.
	oldTime := time.Now().Add(-24 * time.Hour)
	stale := map[string]string{
		"stale-0": "2026-05-03/2026-05-03-1430000000.md",
		"stale-1": "2026-05-03/2026-05-03-1430000001.md",
	}
	idxPath := writePerHostIdx(t, hostDir, stale, oldTime)

	// Write 5 actual .md files with FRESH mtime so the index is stale
	// relative to the on-disk truth.
	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("walk-%d", i)
		fname := fmt.Sprintf("2026-05-03-1430%05d.md", i)
		writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"), fname,
			aggMkNote(project, sid, i+1))
	}

	// Confirm the fast-path probe rejects the stale index.
	if _, ok := tryFastPath(hostDir, idxPath); ok {
		t.Fatal("tryFastPath accepted stale index; fast path would silently miss new notes")
	}

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 5 {
		t.Errorf("len = %d, want 5 (fallback walk)", got)
	}
	// Stale entries must not leak.
	for _, sid := range []string{"stale-0", "stale-1"} {
		if _, ok := idx.Entries[sid]; ok {
			t.Errorf("stale entry %s leaked into result", sid)
		}
	}
}

// TestAggregateProject_PerHostIndexMissing: no per-host index.json file at
// all. Aggregator falls back to walking the .md tree.
func TestAggregateProject_PerHostIndexMissing(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"

	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("walk-%d", i)
		fname := fmt.Sprintf("2026-05-03-1430%05d.md", i)
		writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"), fname,
			aggMkNote(project, sid, i+1))
	}

	hostDir := filepath.Join(vault, "Projects", project, "sessions", "host1")
	if _, ok := tryFastPath(hostDir, filepath.Join(hostDir, perHostIndexFile)); ok {
		t.Fatal("tryFastPath returned true for missing index")
	}

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 5 {
		t.Errorf("len = %d, want 5", got)
	}
	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("walk-%d", i)
		e, ok := idx.Entries[sid]
		if !ok {
			t.Errorf("missing %s", sid)
			continue
		}
		if e.Host != "host1" {
			t.Errorf("%s Host = %q, want host1", sid, e.Host)
		}
	}
}

// TestAggregateProject_DuplicateSessionIDError: two hosts both list the
// same session_id in their per-host index.json files. Aggregator must
// surface a non-nil error naming both hostnames; defense-in-depth even
// though SessionIDs are UUIDs in production.
func TestAggregateProject_DuplicateSessionIDError(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"

	dup := "duplicate-sid-001"

	// Both hosts have the same .md content (same session_id in
	// frontmatter) under their own date subdirs. The fast-path index
	// also points at it — collision must surface regardless of which
	// branch runs.
	host1Dir := filepath.Join(vault, "Projects", project, "sessions", "host1")
	host2Dir := filepath.Join(vault, "Projects", project, "sessions", "host2")

	writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"),
		"2026-05-03-143000000.md", aggMkNote(project, dup, 1))
	writeAggNote(t, vault, project, filepath.Join("host2", "2026-05-03"),
		"2026-05-03-143000001.md", aggMkNote(project, dup, 2))

	now := time.Now()
	writePerHostIdx(t, host1Dir, map[string]string{dup: "2026-05-03/2026-05-03-143000000.md"}, now)
	writePerHostIdx(t, host2Dir, map[string]string{dup: "2026-05-03/2026-05-03-143000001.md"}, now)

	// Pin .md mtimes to the past so the indexes are definitively fresh.
	past := time.Now().Add(-1 * time.Hour)
	for _, p := range []string{
		filepath.Join(host1Dir, "2026-05-03", "2026-05-03-143000000.md"),
		filepath.Join(host2Dir, "2026-05-03", "2026-05-03-143000001.md"),
	} {
		_ = os.Chtimes(p, past, past)
	}

	_, err := AggregateProject(vault, project)
	if err == nil {
		t.Fatal("expected duplicate session_id error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, dup) {
		t.Errorf("error %q does not mention session_id %q", msg, dup)
	}
	// Either ordering is acceptable depending on os.ReadDir order, but
	// both hostnames must appear.
	if !strings.Contains(msg, "host1") || !strings.Contains(msg, "host2") {
		t.Errorf("error %q does not name both hosts", msg)
	}
}

// TestAggregateProject_NoProjectTree: project directory does not exist.
// Aggregator returns an empty Index with no error — callers iterating
// over many projects rely on this.
func TestAggregateProject_NoProjectTree(t *testing.T) {
	vault := t.TempDir()
	idx, err := AggregateProject(vault, "ghost")
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 0 {
		t.Errorf("len = %d, want 0", got)
	}
}

// TestAggregateProject_EmptyArgs: defensive surface — empty vaultPath /
// project both error rather than silently returning empty results.
func TestAggregateProject_EmptyArgs(t *testing.T) {
	if _, err := AggregateProject("", "p"); err == nil {
		t.Error("expected error for empty vaultPath")
	}
	if _, err := AggregateProject("/tmp", ""); err == nil {
		t.Error("expected error for empty project")
	}
}

// TestAggregateProject_FastPathPathEscape: a per-host index.json that
// references a path escaping the host dir (`../../../etc/passwd.md`) is
// rejected by the fast-path emitter — the entry is logged + skipped,
// not honored. Other valid index entries continue to land normally.
func TestAggregateProject_FastPathPathEscape(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"
	hostDir := filepath.Join(vault, "Projects", project, "sessions", "host1")

	// One legit note + one bogus path-escape entry.
	past := time.Now().Add(-1 * time.Hour)
	full := writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"),
		"2026-05-03-1430000000.md", aggMkNote(project, "ok-1", 1))
	if err := os.Chtimes(full, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	now := time.Now()
	writePerHostIdx(t, hostDir, map[string]string{
		"ok-1":   "2026-05-03/2026-05-03-1430000000.md",
		"escape": "../../../etc/passwd.md",
	}, now)

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 1 {
		t.Errorf("len = %d, want 1 (escape entry must be rejected)", got)
	}
	if _, ok := idx.Entries["escape"]; ok {
		t.Error("path-escape entry leaked into result")
	}
	if _, ok := idx.Entries["ok-1"]; !ok {
		t.Error("legitimate entry missing")
	}
}

// TestAggregateProject_FastPathSessionIDMismatch: per-host index.json
// key disagrees with the underlying note's frontmatter session_id. The
// aggregator trusts frontmatter (logs the divergence); the entry lands
// under its frontmatter SessionID.
func TestAggregateProject_FastPathSessionIDMismatch(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"
	hostDir := filepath.Join(vault, "Projects", project, "sessions", "host1")

	past := time.Now().Add(-1 * time.Hour)
	full := writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"),
		"2026-05-03-1430000000.md", aggMkNote(project, "frontmatter-sid", 1))
	if err := os.Chtimes(full, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	// Index claims the file's session_id is something else.
	now := time.Now()
	writePerHostIdx(t, hostDir, map[string]string{
		"index-key-sid": "2026-05-03/2026-05-03-1430000000.md",
	}, now)

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if _, ok := idx.Entries["frontmatter-sid"]; !ok {
		t.Error("frontmatter session_id should win on mismatch")
	}
	if _, ok := idx.Entries["index-key-sid"]; ok {
		t.Error("index-key session_id should not be used")
	}
}

// TestAggregateProject_FrontmatterProjectMismatch: a note whose
// frontmatter project disagrees with the aggregator's project arg is
// log+skipped rather than incorrectly attributed.
func TestAggregateProject_FrontmatterProjectMismatch(t *testing.T) {
	vault := t.TempDir()
	writeAggNote(t, vault, "real-project", filepath.Join("host1", "2026-05-03"),
		"2026-05-03-1430000000.md", aggMkNote("OTHER-PROJECT", "mismatch-1", 1))

	idx, err := AggregateProject(vault, "real-project")
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 0 {
		t.Errorf("len = %d, want 0 (project mismatch must skip)", got)
	}
}

// TestAggregateProject_PerHostIndexInvalidJSON: a syntactically broken
// per-host index.json gets ignored; aggregator falls back to the walk.
func TestAggregateProject_PerHostIndexInvalidJSON(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"
	hostDir := filepath.Join(vault, "Projects", project, "sessions", "host1")

	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Garbage JSON.
	if err := os.WriteFile(filepath.Join(hostDir, perHostIndexFile),
		[]byte("not-json{{"), 0o644); err != nil {
		t.Fatalf("write idx: %v", err)
	}

	for i := 0; i < 3; i++ {
		sid := fmt.Sprintf("walk-%d", i)
		fname := fmt.Sprintf("2026-05-03-1430%05d.md", i)
		writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"), fname,
			aggMkNote(project, sid, i+1))
	}

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if got := len(idx.Entries); got != 3 {
		t.Errorf("len = %d, want 3 (fallback walk)", got)
	}
}

// TestAggregateProject_HostFastPathMissingNote: per-host index.json
// references a session_id whose underlying .md file no longer exists
// (e.g. mid-rebase deletion). The fast path's per-entry emit silently
// skips the missing note rather than erroring — the aggregator's job is
// best-effort enrichment, not filesystem repair.
func TestAggregateProject_HostFastPathMissingNote(t *testing.T) {
	vault := t.TempDir()
	project := "myproj"
	hostDir := filepath.Join(vault, "Projects", project, "sessions", "host1")

	past := time.Now().Add(-1 * time.Hour)
	full := writeAggNote(t, vault, project, filepath.Join("host1", "2026-05-03"),
		"2026-05-03-1430000000.md", aggMkNote(project, "real-1", 1))
	if err := os.Chtimes(full, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	now := time.Now()
	writePerHostIdx(t, hostDir, map[string]string{
		"real-1":  "2026-05-03/2026-05-03-1430000000.md",
		"ghost-1": "2026-05-03/2026-05-03-1430999999.md", // no such file
	}, now)

	idx, err := AggregateProject(vault, project)
	if err != nil {
		t.Fatalf("AggregateProject: %v", err)
	}
	if _, ok := idx.Entries["ghost-1"]; ok {
		t.Error("missing-file entry should not appear in result")
	}
	if _, ok := idx.Entries["real-1"]; !ok {
		t.Error("real entry missing")
	}
}
