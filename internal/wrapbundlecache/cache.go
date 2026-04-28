// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package wrapbundlecache stores wrap-skeleton JSON files in a host-local
// cache directory and provides log-rotated retention.
//
// The cache is intentionally NOT vault-side: skeletons are an ephemeral
// orchestrator-facts payload reused across escalation tiers within a single
// wrap dispatch, not a multi-machine artifact. The on-disk shape is one JSON
// file per iteration per project:
//
//	<cache_dir>/<project>/iter-<N>-skeleton.json
//
// Files are written atomically (temp + rename) with 0600 perms so a crashed
// writer never leaves a half-written skeleton. Read() refuses paths outside
// the cache dir to make path-traversal abuse impossible from input data.
//
// On the first CacheDir() call after upgrade, any pre-migration root-level
// iter-*-skeleton.json files are relocated to <cache_dir>/_legacy/ so an
// operator inspecting the cache sees what happened.
package wrapbundlecache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DefaultRotationN is the default per-project retention count used by both
// the prepare-skeleton MCP tool (post-write rotation) and the read-side
// IsNotExist diagnostic (so the operator-facing message stays in sync with
// the actual rotation policy). Single source of truth — do not duplicate
// this literal elsewhere in the package.
const DefaultRotationN = 3

// cacheDirOverride is a test seam; when non-empty CacheDir() returns
// "<cacheDirOverride>/<project>" verbatim (after creating it). Production
// code leaves it empty.
var cacheDirOverride string

// SetCacheDirForTesting overrides the cache directory base for the duration
// of a test. Pass "" to restore default behaviour. Intended for use only
// from _test.go files in packages that import this one.
func SetCacheDirForTesting(dir string) {
	cacheDirOverride = dir
	// Reset migration state so tests that seed a fresh root-level layout
	// re-enter migrateLegacyFiles. Production code never calls this.
	legacyMigrationOnce = sync.Once{}
}

// skeletonFilePattern matches the iter-<N>-skeleton.json filename layout.
var skeletonFilePattern = regexp.MustCompile(`^iter-(\d+)-skeleton\.json$`)

// validateProject mirrors internal/mcp.validateProjectName but lives here
// to avoid an import cycle (internal/mcp already imports
// internal/wrapbundlecache, so a reverse import would not compile). Keep
// the rejection set in sync with internal/mcp/tools.go validateProjectName:
// reject empty, "/", "\", "..". If you change one, update the other in the
// same patch — see DESIGN #91 for the drift-risk note.
func validateProject(name string) error {
	if name == "" {
		return errors.New("project name is required")
	}
	if strings.Contains(name, "/") ||
		strings.Contains(name, "\\") ||
		strings.Contains(name, "..") {
		return fmt.Errorf("invalid project name: %q", name)
	}
	return nil
}

// legacyMigrationOnce gates the one-time relocation of root-level
// iter-*-skeleton.json files into <base>/_legacy/. Reset by
// SetCacheDirForTesting so each test starts with a fresh migration window.
var legacyMigrationOnce sync.Once

// migrateLegacyFiles relocates root-level skeleton files (pre-per-project
// layout) into <base>/_legacy/ on the first call after process startup.
// Multi-process safe: each os.Rename is checked individually, and
// os.IsNotExist is treated as "another process beat us to this file".
// The stderr line counts only successful relocations.
func migrateLegacyFiles(base string) {
	legacyMigrationOnce.Do(func() {
		entries, err := os.ReadDir(base)
		if err != nil {
			return
		}
		var legacy []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if skeletonFilePattern.MatchString(e.Name()) {
				legacy = append(legacy, e.Name())
			}
		}
		if len(legacy) == 0 {
			return
		}
		legacyDir := filepath.Join(base, "_legacy")
		if mkErr := os.MkdirAll(legacyDir, 0o700); mkErr != nil {
			return
		}
		relocated := 0
		for _, name := range legacy {
			src := filepath.Join(base, name)
			dst := filepath.Join(legacyDir, name)
			err := os.Rename(src, dst)
			switch {
			case err == nil:
				relocated++
			case os.IsNotExist(err):
				// Concurrent process beat us. Acceptable.
			default:
				fmt.Fprintf(os.Stderr,
					"wrapbundlecache: legacy migration failed for %s: %v\n", name, err)
			}
		}
		if relocated > 0 {
			fmt.Fprintf(os.Stderr,
				"wrapbundlecache: relocated %d legacy iter-*-skeleton.json files to %s/_legacy/ (per-project layout migration)\n",
				relocated, base)
		}
	})
}

// baseCacheDir resolves the on-disk root for the wrap-bundle cache,
// creating it with 0700 perms if missing. Honours cacheDirOverride for
// tests; otherwise uses os.UserCacheDir() + "/vibe-vault/wrap-bundles".
// This is the parent directory under which per-project subdirectories live.
func baseCacheDir() (string, error) {
	if cacheDirOverride != "" {
		if err := os.MkdirAll(cacheDirOverride, 0o700); err != nil {
			return "", fmt.Errorf("create cache dir %q: %w", cacheDirOverride, err)
		}
		return cacheDirOverride, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	dir := filepath.Join(base, "vibe-vault", "wrap-bundles")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create cache dir %q: %w", dir, err)
	}
	return dir, nil
}

// CacheDir returns the per-project cache directory
// <base>/<project>/, creating it with 0700 perms if missing. On the first
// call after process startup, any pre-migration root-level
// iter-*-skeleton.json files in <base> are relocated to <base>/_legacy/
// (one-time, idempotent across subsequent calls).
func CacheDir(project string) (string, error) {
	if err := validateProject(project); err != nil {
		return "", err
	}
	base, err := baseCacheDir()
	if err != nil {
		return "", err
	}
	migrateLegacyFiles(base)
	dir := filepath.Join(base, project)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create project cache dir %q: %w", dir, err)
	}
	return dir, nil
}

// SkeletonPath returns the canonical filename for the given project + iter.
func SkeletonPath(project string, iter int) (string, error) {
	if err := validateProject(project); err != nil {
		return "", err
	}
	dir, err := CacheDir(project)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("iter-%d-skeleton.json", iter)), nil
}

// Write atomically persists data to
// <CacheDir(project)>/iter-<iter>-skeleton.json with 0600 perms and returns
// the final path plus the hex-encoded SHA-256 of data. The on-disk file
// never exists in a partial state: bytes are staged to a temp file in the
// same per-project subdirectory and renamed into place.
func Write(project string, iter int, data []byte) (string, string, error) {
	if err := validateProject(project); err != nil {
		return "", "", err
	}
	if iter <= 0 {
		return "", "", fmt.Errorf("iter must be > 0, got %d", iter)
	}
	dir, err := CacheDir(project)
	if err != nil {
		return "", "", err
	}
	finalPath := filepath.Join(dir, fmt.Sprintf("iter-%d-skeleton.json", iter))

	tmp, err := os.CreateTemp(dir, fmt.Sprintf("iter-%d-skeleton-*.tmp", iter))
	if err != nil {
		return "", "", fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}
	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		cleanup()
		return "", "", fmt.Errorf("write temp: %w", writeErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		cleanup()
		return "", "", fmt.Errorf("fsync temp: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return "", "", fmt.Errorf("close temp: %w", closeErr)
	}
	if chmodErr := os.Chmod(tmpPath, 0o600); chmodErr != nil {
		cleanup()
		return "", "", fmt.Errorf("chmod temp: %w", chmodErr)
	}
	if renameErr := os.Rename(tmpPath, finalPath); renameErr != nil {
		cleanup()
		return "", "", fmt.Errorf("rename temp into place: %w", renameErr)
	}
	sum := sha256.Sum256(data)
	return finalPath, hex.EncodeToString(sum[:]), nil
}

// Read returns the bytes at path. The path MUST resolve to a location under
// the wrap-bundle cache base directory; otherwise an error is returned
// without touching disk. The base-dir traversal check correctly admits
// per-project subdirectory paths (the project segment is just a non-".."
// path component).
func Read(path string) ([]byte, error) {
	dir, err := baseCacheDir()
	if err != nil {
		return nil, err
	}
	absPath, absErr := filepath.Abs(path)
	if absErr != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", absErr)
	}
	absDir, absDirErr := filepath.Abs(dir)
	if absDirErr != nil {
		return nil, fmt.Errorf("resolve cache dir: %w", absDirErr)
	}
	rel, relErr := filepath.Rel(absDir, absPath)
	if relErr != nil {
		return nil, fmt.Errorf("path outside cache dir")
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || hasParentTraversal(rel) {
		return nil, fmt.Errorf("path %q escapes cache dir", path)
	}
	data, err := os.ReadFile(absPath)
	if err != nil && os.IsNotExist(err) {
		return nil, fmt.Errorf(
			"skeleton cache miss at %s — was the skeleton evicted by RotateKeepN, or did the orchestrator pass a stale handle? (rotation policy: keep %d most recent per project)",
			absPath, DefaultRotationN)
	}
	return data, err
}

// hasParentTraversal returns true when rel contains a ".." segment.
func hasParentTraversal(rel string) bool {
	for _, part := range filepath.SplitList(rel) {
		if part == ".." {
			return true
		}
	}
	parts := splitPath(rel)
	for _, p := range parts {
		if p == ".." {
			return true
		}
	}
	return false
}

func splitPath(p string) []string {
	var out []string
	for {
		dir, file := filepath.Split(p)
		if file != "" {
			out = append([]string{file}, out...)
		}
		if dir == "" || dir == string(filepath.Separator) {
			if dir == string(filepath.Separator) {
				out = append([]string{string(filepath.Separator)}, out...)
			}
			return out
		}
		p = filepath.Clean(dir)
		if p == "." {
			return out
		}
	}
}

// RotateKeepN keeps the n most recent (highest iter number) skeleton files
// in <CacheDir(project)>/ and deletes the rest. Returns the absolute paths
// that were removed. n must be >= 1. Walks ONLY the project's subdirectory
// — sibling projects and the _legacy/ directory are untouched.
//
// os.IsNotExist on remove is tolerated: a file already gone is success
// (e.g. concurrent rotation from another process).
func RotateKeepN(project string, n int) ([]string, error) {
	if err := validateProject(project); err != nil {
		return nil, err
	}
	if n < 1 {
		return nil, errors.New("n must be >= 1")
	}
	dir, err := CacheDir(project)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read cache dir: %w", err)
	}
	type pair struct {
		iter int
		name string
	}
	var pairs []pair
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := skeletonFilePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		num, parseErr := strconv.Atoi(m[1])
		if parseErr != nil {
			continue
		}
		pairs = append(pairs, pair{iter: num, name: e.Name()})
	}
	if len(pairs) <= n {
		return nil, nil
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].iter > pairs[j].iter })
	var deleted []string
	for _, p := range pairs[n:] {
		full := filepath.Join(dir, p.name)
		if rmErr := os.Remove(full); rmErr != nil && !os.IsNotExist(rmErr) {
			return deleted, fmt.Errorf("remove %q: %w", full, rmErr)
		}
		deleted = append(deleted, full)
	}
	if len(deleted) > 0 {
		names := make([]string, len(deleted))
		for i, p := range deleted {
			names[i] = filepath.Base(p)
		}
		fmt.Fprintf(os.Stderr,
			"wrapbundlecache: rotated project=%s deleted=%v kept=%d\n",
			project, names, n)
	}
	return deleted, nil
}
