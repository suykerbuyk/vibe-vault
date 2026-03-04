// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLockUnlock(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "session-index.json")

	fl, err := Lock(indexPath)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// Lock file should exist
	lockPath := indexPath + ".lock"
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file should exist: %v", err)
	}

	if err := fl.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

func TestLockConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "session-index.json")

	// Write initial index
	idx := &Index{
		path:    indexPath,
		Entries: make(map[string]SessionEntry),
	}
	if err := idx.Save(); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			fl, err := Lock(indexPath)
			if err != nil {
				errs <- err
				return
			}
			defer fl.Unlock()

			// Load, modify, save while holding lock
			loaded, err := Load(dir)
			if err != nil {
				errs <- err
				return
			}
			loaded.Add(SessionEntry{
				SessionID: fmt.Sprintf("sess-%d", id),
				Project:   "test",
				Date:      "2026-03-04",
				Iteration: id,
			})
			if err := loaded.Save(); err != nil {
				errs <- err
				return
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("goroutine error: %v", err)
	}

	// Verify all entries were saved
	final, err := Load(dir)
	if err != nil {
		t.Fatalf("final load: %v", err)
	}

	if len(final.Entries) != goroutines {
		t.Errorf("expected %d entries, got %d", goroutines, len(final.Entries))
	}
}

func TestUnlockIdempotent(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "session-index.json")

	fl, err := Lock(indexPath)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// First unlock should succeed
	if err := fl.Unlock(); err != nil {
		t.Fatalf("first Unlock: %v", err)
	}

	// Second unlock should be a no-op (file is nil)
	if err := fl.Unlock(); err != nil {
		t.Fatalf("second Unlock should be no-op: %v", err)
	}
}
