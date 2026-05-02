// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package lockfile provides a generalized advisory file-based locking
// primitive built on syscall.Flock. It is intended for any caller that
// needs cross-process exclusive access to a resource keyed by a path
// (session index, worktree GC, etc.).
package lockfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked is returned by AcquireNonBlocking when the lock is already
// held by another process.
var ErrLocked = errors.New("lockfile: already locked")

// Lockfile holds an acquired advisory file lock. Use Acquire or
// AcquireNonBlocking to obtain one; pair every successful acquire with
// Release (typically via defer).
type Lockfile struct {
	path string
	file *os.File
}

// Acquire opens (creating if necessary) the file at path and takes an
// exclusive advisory lock on it, blocking until the lock is available.
// The parent directory is created if it does not yet exist.
func Acquire(path string) (*Lockfile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	return &Lockfile{path: path, file: f}, nil
}

// AcquireNonBlocking attempts to take an exclusive advisory lock on the
// file at path without blocking. If the lock is already held by another
// process, it returns ErrLocked. The parent directory is created if it
// does not yet exist.
func AcquireNonBlocking(path string) (*Lockfile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Close the fd before returning so we do not leak it.
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	return &Lockfile{path: path, file: f}, nil
}

// Release releases the advisory lock and closes the underlying file.
// It is safe to call multiple times; subsequent calls are no-ops.
func (fl *Lockfile) Release() error {
	if fl == nil || fl.file == nil {
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
