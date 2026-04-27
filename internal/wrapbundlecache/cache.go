// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package wrapbundlecache stores wrap-skeleton JSON files in a host-local
// cache directory and provides log-rotated retention.
//
// The cache is intentionally NOT vault-side: skeletons are an ephemeral
// orchestrator-facts payload reused across escalation tiers within a single
// wrap dispatch, not a multi-machine artifact. The on-disk shape is one JSON
// file per iteration: <cache_dir>/iter-<N>-skeleton.json.
//
// Files are written atomically (temp + rename) with 0600 perms so a crashed
// writer never leaves a half-written skeleton. Read() refuses paths outside
// the cache dir to make path-traversal abuse impossible from input data.
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
)

// cacheDirOverride is a test seam; when non-empty CacheDir() returns it
// verbatim. Production code leaves it empty.
var cacheDirOverride string

// SetCacheDirForTesting overrides the cache directory for the duration of a
// test. Pass "" to restore default behaviour. Intended for use only from
// _test.go files in packages that import this one.
func SetCacheDirForTesting(dir string) {
	cacheDirOverride = dir
}

// skeletonFilePattern matches the iter-<N>-skeleton.json filename layout.
var skeletonFilePattern = regexp.MustCompile(`^iter-(\d+)-skeleton\.json$`)

// CacheDir returns the host-local skeleton cache directory, creating it with
// 0700 perms if missing. Honours cacheDirOverride for tests; otherwise uses
// os.UserCacheDir() + "/vibe-vault/wrap-bundles".
func CacheDir() (string, error) {
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

// SkeletonPath returns the canonical filename for the given iteration.
func SkeletonPath(iter int) (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("iter-%d-skeleton.json", iter)), nil
}

// Write atomically persists data to <CacheDir()>/iter-<iter>-skeleton.json
// with 0600 perms and returns the final path plus the hex-encoded SHA-256 of
// data. The on-disk file never exists in a partial state: the bytes are
// staged to a temp file and renamed into place.
func Write(iter int, data []byte) (string, string, error) {
	if iter <= 0 {
		return "", "", fmt.Errorf("iter must be > 0, got %d", iter)
	}
	dir, err := CacheDir()
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
// CacheDir(); otherwise an error is returned without touching disk.
func Read(path string) ([]byte, error) {
	dir, err := CacheDir()
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
	return os.ReadFile(absPath)
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
// in CacheDir() and deletes the rest. Returns the absolute paths that were
// removed. n must be >= 1.
func RotateKeepN(n int) ([]string, error) {
	if n < 1 {
		return nil, errors.New("n must be >= 1")
	}
	dir, err := CacheDir()
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
		if rmErr := os.Remove(full); rmErr != nil {
			return deleted, fmt.Errorf("remove %q: %w", full, rmErr)
		}
		deleted = append(deleted, full)
	}
	return deleted, nil
}
