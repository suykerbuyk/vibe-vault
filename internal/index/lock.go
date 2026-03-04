// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"fmt"
	"os"
	"syscall"
)

// FileLock provides advisory file-based locking using syscall.Flock.
// Use around Load->Add->Save sequences to prevent concurrent corruption.
type FileLock struct {
	path string
	file *os.File
}

// Lock acquires an exclusive advisory lock on the index file.
// The lock file is created at indexPath + ".lock".
func Lock(indexPath string) (*FileLock, error) {
	lockPath := indexPath + ".lock"

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	return &FileLock{path: lockPath, file: f}, nil
}

// Unlock releases the advisory lock and closes the lock file.
func (fl *FileLock) Unlock() error {
	if fl.file == nil {
		return nil
	}

	err := syscall.Flock(int(fl.file.Fd()), syscall.LOCK_UN)
	closeErr := fl.file.Close()
	fl.file = nil

	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close lock file: %w", closeErr)
	}
	return nil
}
