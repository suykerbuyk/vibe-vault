// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionclaim

import (
	"fmt"
	"os"
	"path/filepath"
)

// atomicWrite writes data to path atomically: write to a temp sibling
// (mode 0o600), fsync, close, rename. The 0o600 mode is load-bearing —
// claim files contain pid + cwd + harness session id and must be
// user-private (M6 in the v8 plan).
//
// This local helper exists because internal/atomicfile.Write requires
// a vault-path arg for surface-stamp validation; sessionclaim files
// live outside the vault so we ship our own ~15-line helper rather
// than weakening atomicfile's contract (L8 in the v8 plan).
//
// On error after CreateTemp, the temp file is best-effort removed so
// concurrent writers are not surprised by leftover *.tmp.* files.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("sessionclaim: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := os.Chmod(tmpPath, 0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("sessionclaim: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("sessionclaim: write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sessionclaim: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sessionclaim: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("sessionclaim: rename: %w", err)
	}
	cleanup = false
	return nil
}
