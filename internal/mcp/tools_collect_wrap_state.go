// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

// vv_collect_wrap_state is the server-side state collector that backs
// /wrap. The slash command calls this once before composing the iter
// narrative; the tool returns every mechanically-computable fact the
// orchestrator needs (iter_n, branch, anchor SHA, commits/files since
// anchor, task deltas via snapshot diff, test counts, dirty-state flags)
// plus a wrap-shape classification. Registered in Commit 2 of the
// wrap-mcp-offload PR (atomic surface 15→16); helpers were relocated in
// Commit 1.

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
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
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

// iterationsMDHasIter reports whether the project's iterations.md file
// already contains a `### Iteration N` header. The wrap pipeline uses
// this to detect the "writes already landed" shape — when the iter-N-1
// narrative made it into iterations.md but the surrounding commit did
// not (vault-dirty).
//
// Returns false (no error) on missing/unreadable iterations.md.
func iterationsMDHasIter(vaultPath, project string, iterN int) bool {
	if vaultPath == "" || project == "" || iterN < 1 {
		return false
	}
	path := filepath.Join(vaultPath, "Projects", project, "agentctx", "iterations.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, m := range iterNarrativeRe.FindAllStringSubmatch(string(data), -1) {
		if len(m) < 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err == nil && n == iterN {
			return true
		}
	}
	return false
}

// collectWrapState assembles the full wrap-state record for a project.
// Pure orchestration: every input is computed by a helper above, and
// the resulting struct is the JSON shape returned by
// vv_collect_wrap_state.
func collectWrapState(ctx context.Context, cfg config.Config, project, cwd string) (CollectWrapStateResult, error) {
	res := CollectWrapStateResult{}

	n, err := nextIterFromIterationsMD(cfg.VaultPath, project)
	if err != nil {
		return res, fmt.Errorf("next iter from iterations.md: %w", err)
	}
	res.IterN = n

	res.Branch = detectBranch(cwd)

	anchorSHA, err := lastIterAnchorSha(cwd)
	if err != nil {
		return res, fmt.Errorf("last iter anchor sha: %w", err)
	}
	res.LastIterAnchorSha = anchorSHA

	// iter-N-1 already in iterations.md? (Used by the writes-already-landed
	// classifier.) Only meaningful when iter_n >= 2.
	if n >= 2 {
		res.IterNMinusOneAlreadyInIterationsMD = iterationsMDHasIter(cfg.VaultPath, project, n-1)
	}

	// commits/files since anchor — fall back to oldest root commit when
	// no anchor exists (first wrap, project never stamped).
	scanFrom := anchorSHA
	if scanFrom == "" {
		root, rerr := oldestRootCommit(ctx, cwd)
		if rerr != nil {
			return res, fmt.Errorf("oldest root commit: %w", rerr)
		}
		scanFrom = root
	}
	commits, err := commitsSinceAnchor(ctx, cwd, scanFrom)
	if err != nil {
		return res, fmt.Errorf("commits since anchor: %w", err)
	}
	if commits == nil {
		commits = []CommitInfo{}
	}
	res.CommitsSinceLastIter = commits

	files, err := filesChangedSinceAnchor(ctx, cwd, scanFrom)
	if err != nil {
		return res, fmt.Errorf("files changed since anchor: %w", err)
	}
	if files == nil {
		files = []string{}
	}
	res.FilesChanged = files

	// task_deltas — set-difference of last-tasks-snapshot.json against
	// the live tasks/ tree. Bootstraps gracefully when the snapshot is
	// absent (empty snapshot ⇒ all live active reads as added).
	snapshot, err := readLastTasksSnapshot(cwd)
	if err != nil {
		return res, fmt.Errorf("read last tasks snapshot: %w", err)
	}
	tasksDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "tasks")
	active, done, cancelled, err := enumerateLiveTasksFS(tasksDir)
	if err != nil {
		return res, fmt.Errorf("enumerate live tasks: %w", err)
	}
	res.TaskDeltas = computeTaskDeltas(snapshot, active, done, cancelled)
	if res.TaskDeltas.Added == nil {
		res.TaskDeltas.Added = []string{}
	}
	if res.TaskDeltas.Retired == nil {
		res.TaskDeltas.Retired = []string{}
	}
	if res.TaskDeltas.Cancelled == nil {
		res.TaskDeltas.Cancelled = []string{}
	}

	// test counts — best-effort parse of doc/TESTING.md headline.
	u, i, l, warn := testCountsFromTestingMD(filepath.Join(cwd, "doc", "TESTING.md"))
	res.TestCounts = TestCounts{
		Unit:        u,
		Integration: i,
		Lint:        l,
		Warning:     warn,
	}

	// vault and project dirty flags.
	vaultDirty, err := vaultHasUncommittedWrites(cfg.VaultPath)
	if err != nil {
		return res, fmt.Errorf("vault git status: %w", err)
	}
	res.VaultHasUncommittedWrites = vaultDirty

	projectDirty, err := projectHasUncommittedWrites(cwd)
	if err != nil {
		return res, fmt.Errorf("project git status: %w", err)
	}
	res.ProjectHasUncommittedWrites = projectDirty

	// shape: classifier runs on the fully-populated state.
	res.Shape = ClassifyWrapShape(res)

	return res, nil
}

// NewCollectWrapStateTool creates the vv_collect_wrap_state MCP tool.
//
// The tool is the C3-v6 server-side counterpart to the wrap pipeline:
// every mechanically-computable fact /wrap needs lives here, so the
// slash command becomes a thin orchestrator that composes prose around
// the returned record. Replaces the four-field vv_describe_iter_state
// tool retired in surface v15→v16.
func NewCollectWrapStateTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_collect_wrap_state",
			Description: "Return the full wrap-state record for the current project: " +
				"iter_n, branch, last_iter_anchor_sha, iter_n_minus_one_already_in_iterations_md, " +
				"commits_since_last_iter, files_changed, task_deltas (added/retired/cancelled via " +
				"last-tasks-snapshot.json diff), test_counts (parsed from doc/TESTING.md headline), " +
				"vault_has_uncommitted_writes, project_has_uncommitted_writes, and shape " +
				"(fresh-feature | planning | bookkeeping | writes-already-landed). " +
				"Used by /wrap to compose the iter narrative inline.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			res, err := collectWrapState(ctx, cfg, project, cwd)
			if err != nil {
				return "", err
			}

			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}

