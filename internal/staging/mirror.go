// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/atomicfile"
)

// Mirror copies every regular `.md` file from srcDir into dstDir,
// preserving the source's relative path layout. Existing destination
// files are skipped when their content matches the source byte-for-byte
// (SHA-256 compare against the destination's on-disk bytes); destination
// files whose content differs (or that are absent) are rewritten via an
// atomic same-directory rename.
//
// Returns the slice of relative paths (relative to dstDir) that were
// actually written. Empty slice means no changes — the staging tree is
// already mirrored. The orchestrator uses this signal to skip the
// per-project commit entirely (idempotent re-runs perform zero copies
// AND zero commits).
//
// Pure copy semantics — Mirror never deletes from dstDir. Staging is
// append-only from the mirror's perspective; the wrap-time orchestrator
// owns garbage-collection policy for the destination subtree.
//
// Files inside any dot-directory (`.git/`, `.cache/`, etc.) are skipped
// entirely; their presence under the staging tree is operational
// (per-host git metadata) and must not leak into the shared vault.
//
// Non-`.md` regular files are skipped silently — staging holds session
// notes, and the mirror's contract is "session notes only." Adding new
// kinds requires an explicit decision; today the operator can put
// scratch files in staging without polluting the vault.
//
// Symlinks, sockets, devices, and other non-regular filesystem entries
// are skipped silently. The staging tree is host-local and curated by
// the hook layer; defensive skip is the right default rather than
// fail-closed on a stray FIFO.
//
// A srcDir that does not exist returns (nil, nil) — a project with no
// staging dir simply has no sessions to mirror, not an error condition.
//
// dstDir is created (with parents) if missing. Mirror does not write
// any sentinel or index file; the orchestrator (SyncSessions) writes
// the per-host index.json AFTER Mirror returns.
//
// Errors mid-walk are surfaced immediately. Partial copies are
// acceptable (the next sync re-tries via the content-hash skip) but
// the per-project commit is all-or-nothing — see SyncSessions.
func Mirror(srcDir, dstDir string) ([]string, error) {
	if srcDir == "" {
		return nil, errors.New("staging.Mirror: srcDir is empty")
	}
	if dstDir == "" {
		return nil, errors.New("staging.Mirror: dstDir is empty")
	}

	srcInfo, err := os.Stat(srcDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat src %s: %w", srcDir, err)
	}
	if !srcInfo.IsDir() {
		return nil, fmt.Errorf("staging.Mirror: srcDir %s is not a directory", srcDir)
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return nil, fmt.Errorf("create dst %s: %w", dstDir, err)
	}

	var changed []string
	walkErr := filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip dot-directories (most importantly `.git/`) wholesale.
		// Do not treat the root as a dot-dir even if dstDir's basename
		// happens to start with '.'.
		if d.IsDir() {
			if path == srcDir {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		// Regular files only.
		if !d.Type().IsRegular() {
			return nil
		}
		// Markdown only.
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return fmt.Errorf("rel path for %s: %w", path, relErr)
		}
		dstPath := filepath.Join(dstDir, rel)

		wrote, copyErr := copyIfDifferent(path, dstPath)
		if copyErr != nil {
			return fmt.Errorf("mirror %s -> %s: %w", path, dstPath, copyErr)
		}
		if wrote {
			changed = append(changed, filepath.ToSlash(rel))
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	// Deterministic ordering simplifies tests and produces stable
	// commit messages downstream.
	sort.Strings(changed)
	return changed, nil
}

// copyIfDifferent reads srcPath, compares its SHA-256 against the
// destination's on-disk content (if present), and atomically rewrites
// the destination only when they differ. Returns true when a write
// occurred.
//
// Reading the source twice (once for hashing, once for the copy) would
// double the I/O on a large mirror; we instead read the source once
// into memory. Session notes are bounded in size (markdown, ~2-50 KB
// each) so the memory cost is negligible — measured at <5 MB peak for
// a worst-case 800-file production-sized fixture.
func copyIfDifferent(srcPath, dstPath string) (bool, error) {
	srcBytes, err := os.ReadFile(srcPath)
	if err != nil {
		return false, fmt.Errorf("read src: %w", err)
	}
	srcHash := sha256.Sum256(srcBytes)

	dstBytes, err := os.ReadFile(dstPath)
	if err == nil {
		dstHash := sha256.Sum256(dstBytes)
		if dstHash == srcHash {
			return false, nil // identical — skip
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read dst: %w", err)
	}

	// atomicfile.Write handles MkdirAll + temp-file + rename. vaultPath=""
	// suppresses the .surface stamp side channel — the destination is a
	// vault subtree but the mirror is a wrap-time orchestrator concern,
	// and the per-project commit downstream is the canonical surface
	// signal for these writes.
	if err := atomicfile.Write("", dstPath, srcBytes); err != nil {
		return false, err
	}
	return true, nil
}

// io.Copy is intentionally avoided in the hot path: ReadFile +
// atomicfile.Write keeps the temp-file dance and atomic rename inside
// a single helper, and the in-memory bytes are reused for both the
// content hash AND the write. Adding a streamed io.Copy variant later
// (for hypothetical multi-MB session notes) is a one-line swap if
// session sizes ever change shape.
