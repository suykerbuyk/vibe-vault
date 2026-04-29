// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"fmt"
	"os"
)

// atomicWriteFile writes data to path atomically via a temp file.
// Creates parent directories as needed. The temp file is created in
// the same directory as the destination so the final os.Rename is a
// same-filesystem rename (atomic on POSIX).
func atomicWriteFile(path string, data []byte) error {
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
	return nil
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
