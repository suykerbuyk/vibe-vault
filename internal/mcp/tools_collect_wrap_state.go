// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

// Helpers backing vv_collect_wrap_state. Tool registration lands in
// Commit 2; this file is helpers-only in Commit 1 to keep the MCP surface
// untouched while letting tests and downstream code reference the
// machinery.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// lastTasksSnapshot is the on-disk format of
// <projectRoot>/.vibe-vault/last-tasks-snapshot.json. Each successful
// wrap rewrites the snapshot via vv_stamp_iter (wired in Commit 3) so
// the next /wrap can compute task_deltas as a set-difference between
// the snapshot and the live filesystem.
type lastTasksSnapshot struct {
	IterN     int      `json:"iter_n"`
	AnchorSHA string   `json:"anchor_sha"`
	Active    []string `json:"active"`
	Done      []string `json:"done"`
	Cancelled []string `json:"cancelled"`
}

// readLastTasksSnapshot reads
// <projectRoot>/.vibe-vault/last-tasks-snapshot.json. An absent file
// is treated as the empty snapshot (first-wrap-since-PR-A condition,
// per the C3-v6 fix); only true I/O / parse errors propagate.
func readLastTasksSnapshot(projectRoot string) (lastTasksSnapshot, error) {
	if projectRoot == "" {
		return lastTasksSnapshot{}, nil
	}
	path := filepath.Join(projectRoot, ".vibe-vault", "last-tasks-snapshot.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return lastTasksSnapshot{}, nil
		}
		return lastTasksSnapshot{}, fmt.Errorf("read tasks snapshot: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return lastTasksSnapshot{}, nil
	}
	var snap lastTasksSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return lastTasksSnapshot{}, fmt.Errorf("parse tasks snapshot: %w", err)
	}
	return snap, nil
}

// enumerateLiveTasksFS walks tasksDir, tasksDir/done, and
// tasksDir/cancelled, returning the slug sets (filenames without `.md`)
// for each partition. Hidden files and non-`.md` entries are skipped.
// Missing partitions yield empty slices, not errors — first-bootstrap
// projects may not have created `done/` or `cancelled/` yet.
//
// Returns an error only for unexpected I/O failures inside an existing
// directory. The active partition's absence (tasksDir itself missing)
// also degrades to empty slices to match the bootstrap-friendly
// behavior of the rest of vv.
func enumerateLiveTasksFS(tasksDir string) (active, done, cancelled []string, err error) {
	active, err = listSlugsIn(tasksDir)
	if err != nil {
		return nil, nil, nil, err
	}
	done, err = listSlugsIn(filepath.Join(tasksDir, "done"))
	if err != nil {
		return nil, nil, nil, err
	}
	cancelled, err = listSlugsIn(filepath.Join(tasksDir, "cancelled"))
	if err != nil {
		return nil, nil, nil, err
	}
	return active, done, cancelled, nil
}

// listSlugsIn returns the .md slugs (basename minus extension) directly
// inside dir. Sub-directories are not descended. Missing dir → empty
// slice + nil error.
func listSlugsIn(dir string) ([]string, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".md"))
	}
	return out, nil
}

// computeTaskDeltas implements the C3-v6 set-difference rules:
//
//   - added: live `tasks/<slug>.md` entries that the snapshot does not
//     mention in any partition.
//   - retired: snapshot.Active slugs now sitting under live `tasks/done/`.
//   - cancelled: snapshot.Active slugs now sitting under live
//     `tasks/cancelled/`.
//
// The snapshot is the prior wrap's filesystem state; the live arrays
// are this wrap's filesystem state. Any slug that moves between
// partitions surfaces as exactly one of retired/cancelled (or as added
// if it never existed in the snapshot).
func computeTaskDeltas(snapshot lastTasksSnapshot, liveActive, liveDone, liveCancelled []string) TaskDeltas {
	known := make(map[string]struct{}, len(snapshot.Active)+len(snapshot.Done)+len(snapshot.Cancelled))
	for _, slug := range snapshot.Active {
		known[slug] = struct{}{}
	}
	for _, slug := range snapshot.Done {
		known[slug] = struct{}{}
	}
	for _, slug := range snapshot.Cancelled {
		known[slug] = struct{}{}
	}

	prevActive := make(map[string]struct{}, len(snapshot.Active))
	for _, slug := range snapshot.Active {
		prevActive[slug] = struct{}{}
	}

	liveDoneSet := make(map[string]struct{}, len(liveDone))
	for _, slug := range liveDone {
		liveDoneSet[slug] = struct{}{}
	}
	liveCancelledSet := make(map[string]struct{}, len(liveCancelled))
	for _, slug := range liveCancelled {
		liveCancelledSet[slug] = struct{}{}
	}

	added := []string{}
	for _, slug := range liveActive {
		if _, ok := known[slug]; !ok {
			added = append(added, slug)
		}
	}
	retired := []string{}
	cancelled := []string{}
	for _, slug := range snapshot.Active {
		if _, ok := liveDoneSet[slug]; ok {
			retired = append(retired, slug)
			continue
		}
		if _, ok := liveCancelledSet[slug]; ok {
			cancelled = append(cancelled, slug)
		}
	}

	return TaskDeltas{
		Added:     added,
		Retired:   retired,
		Cancelled: cancelled,
	}
}

// commitsSinceAnchor returns the commits between anchorSHA (exclusive)
// and HEAD (inclusive) in projectDir, as a list of {SHA, Subject}
// records. Empty anchorSHA degrades to an empty list — callers are
// expected to substitute oldestRootCommit() upstream.
func commitsSinceAnchor(ctx context.Context, projectDir, anchorSHA string) ([]CommitInfo, error) {
	if projectDir == "" || anchorSHA == "" {
		return nil, nil
	}
	out, err := gitCmdRunner(ctx, projectDir,
		"log", "--format=%H %s", anchorSHA+"..HEAD")
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	commits := []CommitInfo{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Format is "<sha><space><subject>". Subject may itself
		// contain spaces, so split on the first space only.
		idx := strings.IndexByte(line, ' ')
		if idx < 0 {
			commits = append(commits, CommitInfo{SHA: line})
			continue
		}
		commits = append(commits, CommitInfo{
			SHA:     line[:idx],
			Subject: line[idx+1:],
		})
	}
	return commits, nil
}

// filesChangedSinceAnchor returns the list of files that differ between
// anchorSHA and HEAD in projectDir. Empty anchorSHA degrades to empty
// list (callers should substitute oldestRootCommit() first).
func filesChangedSinceAnchor(ctx context.Context, projectDir, anchorSHA string) ([]string, error) {
	if projectDir == "" || anchorSHA == "" {
		return nil, nil
	}
	out, err := gitCmdRunner(ctx, projectDir,
		"diff", "--name-only", anchorSHA+"..HEAD")
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	files := []string{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		files = append(files, line)
	}
	return files, nil
}

// testCountsHeadlineRe matches the headline test-count line in
// doc/TESTING.md. Format (verified iter 216):
// "**Test counts: 2291 unit + 32 integration + 0 lint = 2323 tests**"
// Capture groups: unit, integration, lint.
var testCountsHeadlineRe = regexp.MustCompile(
	`(?i)test counts?:\s*(\d+)\s+unit\s*\+\s*(\d+)\s+integration\s*\+\s*(\d+)\s+lint`,
)

// testCountsFromTestingMD parses doc/TESTING.md's headline counts. On
// missing file or unparseable content, returns zeros and a non-empty
// warning string explaining why — never an error in the Go sense, since
// stale counts are advisory and shouldn't fail the wrap.
func testCountsFromTestingMD(testingMDPath string) (unit, integration, lint int, warning string) {
	if testingMDPath == "" {
		return 0, 0, 0, "doc/TESTING.md path empty"
	}
	data, err := os.ReadFile(testingMDPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, 0, "doc/TESTING.md missing"
		}
		return 0, 0, 0, fmt.Sprintf("read doc/TESTING.md: %v", err)
	}
	m := testCountsHeadlineRe.FindStringSubmatch(string(data))
	if len(m) < 4 {
		return 0, 0, 0, "doc/TESTING.md headline not found"
	}
	u, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, 0, fmt.Sprintf("parse unit count: %v", err)
	}
	i, err := strconv.Atoi(m[2])
	if err != nil {
		return 0, 0, 0, fmt.Sprintf("parse integration count: %v", err)
	}
	l, err := strconv.Atoi(m[3])
	if err != nil {
		return 0, 0, 0, fmt.Sprintf("parse lint count: %v", err)
	}
	return u, i, l, ""
}

// oldestRootCommit returns the SHA of the oldest root commit reachable
// from HEAD in projectDir. `git rev-list --max-parents=0 HEAD` lists
// every parent-less commit (one per disjoint history root); we pick the
// LAST line, which `git rev-list` emits in commit-date order (newest
// first), so the last entry is the oldest root.
//
// Per M3-v6: `gitCmdRunner` uses exec.CommandContext with no shell, so
// the slash-equivalent `git rev-list --max-parents=0 HEAD | tail -1`
// must be implemented in Go — capture full output, split on '\n', take
// the last non-empty line.
func oldestRootCommit(ctx context.Context, projectDir string) (string, error) {
	if projectDir == "" {
		return "", nil
	}
	out, err := gitCmdRunner(ctx, projectDir, "rev-list", "--max-parents=0", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-list root: %w", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l, nil
		}
	}
	return "", nil
}

