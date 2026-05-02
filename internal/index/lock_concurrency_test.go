// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package index

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
)

// TestIndexConcurrentSave is an integration-style test verifying that
// the lockfile primitive (promoted out of this package in Phase 1a)
// still serializes Index Load/Add/Save sequences correctly. Each of
// N goroutines acquires the lock, loads the index, adds a unique entry,
// and saves; the final entry count must equal N.
func TestIndexConcurrentSave(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "session-index.json")
	lockPath := indexPath + ".lock"

	// Write initial index.
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

			fl, err := lockfile.Acquire(lockPath)
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = fl.Release() }()

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

	final, err := Load(dir)
	if err != nil {
		t.Fatalf("final load: %v", err)
	}

	if len(final.Entries) != goroutines {
		t.Errorf("expected %d entries, got %d", goroutines, len(final.Entries))
	}
}
