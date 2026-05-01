// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package atomicfile provides an atomic file write primitive shared across
// vibe-vault internals. On a successful write with a non-empty vaultPath the
// resolved <stampDir>/.surface file is touched (best-effort) so the vault
// records the MCP tool surface that produced the change.
package atomicfile

import (
	"fmt"
	"os"

	"github.com/suykerbuyk/vibe-vault/internal/surface"
)

// Write atomically writes data to path via a same-directory temp file and
// os.Rename. Parent directories are created as needed.
//
// When vaultPath != "" and the write target resolves under a recognized
// vault layout (Projects/<p>/, Knowledge/, Templates/), a successful write
// refreshes the corresponding .surface file as a best-effort side channel.
// Stamping failures are logged to stderr but never propagate — the primary
// write has already succeeded.
func Write(vaultPath, path string, data []byte) error {
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return fmt.Errorf("create parent directories: %w", err)
	}
	tmp, err := os.CreateTemp(dirOf(path), ".vv-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	removeTemp = false

	if vaultPath != "" {
		stampOnSuccess(vaultPath, path)
	}
	return nil
}

// stampOnSuccess refreshes the .surface stamp for a successful vault write.
// Any error is logged to stderr but never fails the primary write.
func stampOnSuccess(vaultPath, writePath string) {
	stampDir, err := surface.ResolveStampDir(vaultPath, writePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vv: warning — surface resolve failed for %q: %v\n", writePath, err)
		return
	}
	if stampDir == "" {
		return
	}
	if err := surface.WriteStamp(stampDir, surface.MCPSurfaceVersion, surface.WriterFingerprint(vaultPath)); err != nil {
		fmt.Fprintf(os.Stderr, "vv: warning — surface stamp failed for %q: %v\n", stampDir, err)
	}
}

// dirOf returns the directory component of path.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
