// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/atomicfile"
	"github.com/suykerbuyk/vibe-vault/internal/noteparse"
	"github.com/suykerbuyk/vibe-vault/internal/vaultsync"
)

// SyncSessionsOpts configures a single SyncSessions invocation.
//
// Projects (when non-empty) restricts the sync to the named projects;
// otherwise the orchestrator enumerates every directory under
// StagingRoot. Staging is the source-of-truth for "what has unsynced
// changes" — enumerating from the vault would silently miss a project
// that exists in staging but has never been mirrored.
//
// StagingRoot defaults to ResolveRoot("") when empty. Tests pass a temp
// dir to avoid touching the operator's XDG state.
//
// Hostname defaults to SanitizeHostname(currentHostname()) when empty.
// Tests pass a deterministic value; production callers pass "" so we
// resolve via the same VIBE_VAULT_HOSTNAME-aware path the hook layer
// uses.
type SyncSessionsOpts struct {
	Projects    []string
	StagingRoot string
	Hostname    string
}

// ProjectSyncResult records the outcome of one project's mirror+commit
// pass. FilesMirrored is the count of relative paths Mirror returned;
// CommitSHA is empty when no files changed (no-op pass).
type ProjectSyncResult struct {
	Project       string
	Hostname      string
	FilesMirrored int
	CommitSHA     string
}

// SyncResult bundles the per-project results plus a list of project
// names whose mirror returned zero changed files (useful for verbose
// CLI reporting and tests that assert idempotence).
type SyncResult struct {
	Projects []ProjectSyncResult
	NoOp     []string
}

// AnyChanged reports whether at least one project produced a commit.
func (r *SyncResult) AnyChanged() bool {
	for _, p := range r.Projects {
		if p.CommitSHA != "" {
			return true
		}
	}
	return false
}

// SyncSessions mirrors host-local staging content into the shared
// vault's per-host subtree (`Projects/<p>/sessions/<host>/`) and
// commits each project's changes locally (push=false). The terminal
// `vv vault push` performs the single network push for the whole wrap.
//
// **Package placement deviation from the plan.** The plan called for
// `internal/vaultsync/sync_sessions.go`, but the import graph forbids
// it: `internal/staging` already imports `internal/vaultsync` (for
// `GitCommand`), so a `vaultsync → staging` arrow would close a cycle.
// Resolving the plan's open question 1 in the simplest direction:
// the orchestrator lives in `internal/staging` (which already depends
// on `vaultsync`), keeping the dependency graph acyclic. CLI wiring in
// `cmd/vv/main.go` is unchanged in spirit — the new subcommand calls
// `staging.SyncSessions(...)` instead of `vaultsync.SyncSessions(...)`.
//
// For each in-scope project:
//
//  1. Compute the destination subtree
//     <vaultPath>/Projects/<project>/sessions/<host>/.
//  2. Mirror staging → destination (content-hash skip when unchanged).
//  3. If no files were written, skip the commit and record the project
//     in result.NoOp. Idempotent re-runs are zero-cost from this point.
//  4. Walk the destination subtree, building the per-host index.json
//     (session_id → relpath) and writing it to <destDir>/index.json.
//  5. Stage and locally commit ONLY the per-host subtree path via
//     vaultsync.CommitAndPushPaths(..., push=false). Per-project
//     commits keep the wrap-time history per-host-attributable.
//
// Per-project commits are all-or-nothing on the commit step; a mid-walk
// Mirror error surfaces immediately and aborts the entire sync to match
// the wrap-time atomic-failure expectation in the plan's resolved
// question 4 (deferred-push design means a failure leaves only local
// commits; nothing has been pushed yet).
func SyncSessions(vaultPath string, opts SyncSessionsOpts) (*SyncResult, error) {
	if vaultPath == "" {
		return nil, errors.New("staging.SyncSessions: vaultPath is empty")
	}

	stagingRoot := opts.StagingRoot
	if stagingRoot == "" {
		stagingRoot = ResolveRoot("")
	}
	if stagingRoot == "" {
		// Operator opt-out (VIBE_VAULT_DISABLE_STAGING=1) or unresolvable
		// XDG path: nothing to mirror. Return an empty result rather than
		// erroring — back-compat with hosts that haven't migrated yet.
		return &SyncResult{}, nil
	}

	host := opts.Hostname
	if host == "" {
		host = SanitizeHostname(currentHostname())
	}
	if host == "" {
		host = "_unknown"
	}

	projects, err := selectProjects(stagingRoot, opts.Projects)
	if err != nil {
		return nil, err
	}

	result := &SyncResult{}
	for _, project := range projects {
		stagingProjectDir := filepath.Join(stagingRoot, project)
		// Defensive existence probe — selectProjects already filters,
		// but explicit names from opts.Projects may not exist.
		if st, statErr := os.Stat(stagingProjectDir); statErr != nil || !st.IsDir() {
			continue
		}

		destDir := filepath.Join(vaultPath, "Projects", project, "sessions", host)

		changed, mirrorErr := Mirror(stagingProjectDir, destDir)
		if mirrorErr != nil {
			return result, fmt.Errorf("mirror %s: %w", project, mirrorErr)
		}

		pr := ProjectSyncResult{
			Project:       project,
			Hostname:      host,
			FilesMirrored: len(changed),
		}

		if len(changed) == 0 {
			result.NoOp = append(result.NoOp, project)
			result.Projects = append(result.Projects, pr)
			continue
		}

		// Build the per-host index BEFORE the commit so it lands in the
		// same commit as the mirrored notes. Walking the destination
		// subtree (not the source index) is intentional — the host's
		// view of its own subtree is authoritative; staging may have
		// notes the mirror skipped (non-.md, dot-dirs).
		idxPath := filepath.Join(destDir, "index.json")
		if err := writePerHostIndex(destDir, idxPath); err != nil {
			return result, fmt.Errorf("write per-host index for %s: %w", project, err)
		}

		// Commit ONLY the per-host subtree. Vault-relative path with
		// forward slashes (git accepts on every platform). push=false:
		// the terminal `vv vault push` pushes everything.
		relSubtree := strings.Join([]string{"Projects", project, "sessions", host}, "/")
		msg := commitMessage(project, host, changed)
		commitRes, commitErr := vaultsync.CommitAndPushPaths(vaultPath, msg, []string{relSubtree}, false)
		if commitErr != nil {
			return result, fmt.Errorf("commit %s: %w", project, commitErr)
		}
		if commitRes != nil {
			pr.CommitSHA = commitRes.CommitSHA
		}
		result.Projects = append(result.Projects, pr)
	}

	return result, nil
}

// selectProjects returns the projects the sync should iterate over.
// When explicit is non-empty, those names are returned verbatim — the
// caller has stated intent and we don't second-guess. When empty, we
// enumerate <stagingRoot>/*/ — staging is the source of truth for
// "what has unsynced changes."
func selectProjects(stagingRoot string, explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		out := make([]string, 0, len(explicit))
		out = append(out, explicit...)
		sort.Strings(out)
		return out, nil
	}
	entries, err := os.ReadDir(stagingRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read staging root %s: %w", stagingRoot, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// writePerHostIndex walks destDir for `.md` files, parses each note for
// its session_id, and writes a JSON map of session_id → relpath
// (relative to destDir, forward-slash separated). Notes without a
// session_id are skipped (defensive — Phase 4's aggregator owns full
// validation).
//
// The map shape is {session_id: relpath}; Phase 4's aggregator unions
// every host's index.json into the canonical .vibe-vault/session-index.json.
func writePerHostIndex(destDir, idxPath string) error {
	entries := map[string]string{}
	err := filepath.WalkDir(destDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == destDir {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
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
		// Skip the index file itself if it ever lands as .md in the future.
		if path == idxPath {
			return nil
		}
		note, parseErr := noteparse.ParseFile(path)
		if parseErr != nil || note == nil || note.SessionID == "" {
			return nil
		}
		rel, relErr := filepath.Rel(destDir, path)
		if relErr != nil {
			return nil
		}
		entries[note.SessionID] = filepath.ToSlash(rel)
		return nil
	})
	if err != nil {
		return err
	}
	body, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return atomicfile.Write("", idxPath, body)
}

// commitMessage builds a stable, host-attributable commit subject for
// the per-project sync-sessions commit. The body is a sorted list of
// the changed relpaths (truncated to keep the message bounded).
func commitMessage(project, host string, changed []string) string {
	dates := uniqueDates(changed)
	subject := fmt.Sprintf("vault: sync sessions %s/%s (%d file%s)",
		project, host, len(changed), pluralS(len(changed)))
	if len(dates) > 0 {
		subject = fmt.Sprintf("vault: sync sessions %s/%s (%d file%s, %s)",
			project, host, len(changed), pluralS(len(changed)), dateRange(dates))
	}
	// Keep the body bounded: 20 paths is enough breadcrumb for review;
	// the per-host index file in the same commit is the canonical
	// long-form record.
	const maxBodyPaths = 20
	body := changed
	truncated := false
	if len(body) > maxBodyPaths {
		body = body[:maxBodyPaths]
		truncated = true
	}
	var sb strings.Builder
	sb.WriteString(subject)
	sb.WriteString("\n\n")
	for _, p := range body {
		sb.WriteString("  ")
		sb.WriteString(p)
		sb.WriteByte('\n')
	}
	if truncated {
		fmt.Fprintf(&sb, "  ... (%d more)\n", len(changed)-maxBodyPaths)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// uniqueDates extracts the YYYY-MM-DD prefix of each path's basename.
// Paths in staging are flat (no date subdirs) and the BuildTimestampFilename
// shape is YYYY-MM-DD-HHMMSSmmm[.-N].md, so the first 10 bytes of the
// basename are the date prefix when present.
func uniqueDates(paths []string) []string {
	seen := map[string]struct{}{}
	for _, p := range paths {
		base := filepath.Base(p)
		if len(base) < 10 {
			continue
		}
		d := base[:10]
		// Cheap shape check: digits separated by hyphens at idx 4 and 7.
		if d[4] != '-' || d[7] != '-' {
			continue
		}
		seen[d] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// dateRange returns "YYYY-MM-DD" for one date or "first..last" for a
// range. Stable ordering comes from the caller (uniqueDates sorts).
func dateRange(dates []string) string {
	switch len(dates) {
	case 0:
		return ""
	case 1:
		return dates[0]
	default:
		return dates[0] + ".." + dates[len(dates)-1]
	}
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
