// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/vaultsync"
)

// MigrateArchiveDir is the destination subtree for pre-staging session
// notes inside the shared vault. Frozen, source-controlled history; once
// populated, OPERATIONS.md documents that this tree must never be deleted.
const MigrateArchiveDir = "_pre-staging-archive"

// MigrateResult summarizes what Migrate did for a single project.
// CommitSHA is empty when the project had no flat session notes
// (no-op) or when the underlying git commands failed before commit.
type MigrateResult struct {
	Project    string   // project slug as it appears under <vault>/Projects/<p>/
	Moved      []string // vault-relative source paths that were `git mv`'d
	CommitSHA  string   // commit recording the moves (empty on no-op)
	IndexFixed int      // number of session-index.json entries rewritten
}

// MigrateOptions configures the migration helper. Exactly one of Project
// or AllProjects must be set; Migrate enforces this.
type MigrateOptions struct {
	VaultPath   string // absolute path to the shared vault root
	Project     string // single project to migrate; empty when AllProjects is set
	AllProjects bool   // walk every project under <vault>/Projects/
}

// Migrate moves committed session notes from the flat layout
// (<vault>/Projects/<p>/sessions/*.md, top-level only) into
// <vault>/Projects/<p>/sessions/_pre-staging-archive/. Per the v3-H3 plan
// fix, it then rewrites session-index.json `note_path` entries in-place
// so MCP and `vv inject` keep working before Phase 1.5 ships.
//
// One commit is produced per project (not one giant commit) for
// diffability and per-project revert. Projects with no eligible files
// are recorded in the result with empty Moved/CommitSHA — a no-op,
// not an error.
//
// Recursive `.md` files under sessions/<host>/... or sessions/<archive>/...
// are not touched: the migration only catches the legacy flat layout.
func Migrate(opts MigrateOptions) ([]MigrateResult, error) {
	if opts.VaultPath == "" {
		return nil, errors.New("staging: vault path is empty")
	}
	if opts.Project == "" && !opts.AllProjects {
		return nil, errors.New("staging: must set Project or AllProjects")
	}
	if opts.Project != "" && opts.AllProjects {
		return nil, errors.New("staging: Project and AllProjects are mutually exclusive")
	}

	var projects []string
	if opts.AllProjects {
		discovered, err := listVaultProjects(opts.VaultPath)
		if err != nil {
			return nil, err
		}
		projects = discovered
	} else {
		projects = []string{opts.Project}
	}

	var results []MigrateResult
	for _, p := range projects {
		res, err := migrateProject(opts.VaultPath, p)
		if err != nil {
			return results, fmt.Errorf("migrate project %s: %w", p, err)
		}
		results = append(results, res)
	}
	return results, nil
}

// listVaultProjects returns the set of project slugs found under
// <vault>/Projects/. Sorted for deterministic per-project commit order.
func listVaultProjects(vaultPath string) ([]string, error) {
	projectsDir := filepath.Join(vaultPath, "Projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", projectsDir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// migrateProject does the per-project work. Returns a populated
// MigrateResult even on the no-op path so the caller can report
// "0 files migrated" rather than swallow the project silently.
func migrateProject(vaultPath, project string) (MigrateResult, error) {
	res := MigrateResult{Project: project}

	sessionsDir := filepath.Join(vaultPath, "Projects", project, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil // no sessions/ at all — nothing to migrate
		}
		return res, fmt.Errorf("read %s: %w", sessionsDir, err)
	}

	// Collect top-level *.md only. Files under sessions/<host>/ or
	// sessions/_pre-staging-archive/ are recursive children — they
	// already live in the new layout (or the archive itself) and must
	// not be moved.
	var flatNotes []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		flatNotes = append(flatNotes, name)
	}
	if len(flatNotes) == 0 {
		return res, nil // no flat-layout notes to move — fully migrated already
	}
	sort.Strings(flatNotes)

	archiveAbs := filepath.Join(sessionsDir, MigrateArchiveDir)
	if mkErr := os.MkdirAll(archiveAbs, 0o755); mkErr != nil {
		return res, fmt.Errorf("create archive dir: %w", mkErr)
	}

	for _, name := range flatNotes {
		src := filepath.Join("Projects", project, "sessions", name)
		dst := filepath.Join("Projects", project, "sessions", MigrateArchiveDir, name)
		if _, mvErr := vaultsync.GitCommand(vaultPath, gitTimeout, "mv", "--", src, dst); mvErr != nil {
			return res, fmt.Errorf("git mv %s -> %s: %w", src, dst, mvErr)
		}
		res.Moved = append(res.Moved, src)
	}

	msg := fmt.Sprintf("staging migrate: archive %d flat-layout sessions for project %s", len(flatNotes), project)
	if _, commitErr := vaultsync.GitCommand(vaultPath, gitTimeout, "commit", "-m", msg); commitErr != nil {
		return res, fmt.Errorf("git commit migration for %s: %w", project, commitErr)
	}
	sha, shaErr := vaultsync.GitCommand(vaultPath, gitTimeout, "rev-parse", "HEAD")
	if shaErr != nil {
		return res, fmt.Errorf("rev-parse migration HEAD: %w", shaErr)
	}
	res.CommitSHA = sha

	fixed, fixErr := rewriteIndexPaths(vaultPath, project, flatNotes)
	if fixErr != nil {
		// The git mv already happened; surface the index error but keep
		// the result so the caller sees what landed.
		return res, fmt.Errorf("rewrite session-index paths: %w", fixErr)
	}
	res.IndexFixed = fixed
	return res, nil
}

// rewriteIndexPaths edits .vibe-vault/session-index.json in place,
// retargeting any entry whose NotePath points at one of the moved
// flat files for `project` to its new _pre-staging-archive location.
// SessionIDs are preserved; the in-memory map is JSON-marshaled back
// out via the same pretty-print format used by index.Save.
//
// Returns the number of entries rewritten. Missing index file is a
// no-op (returns 0, nil) — fresh vaults that never built an index have
// nothing to fix.
func rewriteIndexPaths(vaultPath, project string, movedNames []string) (int, error) {
	indexPath := filepath.Join(vaultPath, ".vibe-vault", "session-index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read %s: %w", indexPath, err)
	}

	// Decode into map[string]json.RawMessage so unknown keys are preserved
	// verbatim — the migration writer is intentionally schema-agnostic and
	// must not drop fields added by future versions of vv.
	var entries map[string]map[string]json.RawMessage
	if uErr := json.Unmarshal(data, &entries); uErr != nil {
		return 0, fmt.Errorf("parse session-index: %w", uErr)
	}

	moved := make(map[string]struct{}, len(movedNames))
	for _, n := range movedNames {
		moved[path.Join("Projects", project, "sessions", n)] = struct{}{}
	}

	rewritten := 0
	for sid, e := range entries {
		raw, ok := e["note_path"]
		if !ok {
			continue
		}
		var notePath string
		if uErr := json.Unmarshal(raw, &notePath); uErr != nil {
			continue // non-string value — skip without aborting the migration
		}
		if _, hit := moved[notePath]; !hit {
			continue
		}
		newPath := path.Join("Projects", project, "sessions", MigrateArchiveDir, filepath.Base(notePath))
		newRaw, mErr := json.Marshal(newPath)
		if mErr != nil {
			return rewritten, fmt.Errorf("marshal new note_path for %s: %w", sid, mErr)
		}
		e["note_path"] = newRaw
		entries[sid] = e
		rewritten++
	}

	if rewritten == 0 {
		return 0, nil
	}

	out, mErr := json.MarshalIndent(entries, "", "  ")
	if mErr != nil {
		return rewritten, fmt.Errorf("marshal session-index: %w", mErr)
	}
	if wErr := os.WriteFile(indexPath, out, 0o644); wErr != nil {
		return rewritten, fmt.Errorf("write session-index: %w", wErr)
	}
	return rewritten, nil
}
