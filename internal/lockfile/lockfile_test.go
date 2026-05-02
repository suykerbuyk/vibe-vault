// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package lockfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
)

func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	fl, err := lockfile.Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = fl.Release() })

	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file should exist on disk: %v", err)
	}

	if err := fl.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestReleaseIdempotent(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	fl, err := lockfile.Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	t.Cleanup(func() { _ = fl.Release() })

	if err := fl.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := fl.Release(); err != nil {
		t.Fatalf("second Release should be no-op: %v", err)
	}
}

func TestAcquireConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	const goroutines = 10
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex // protects test bookkeeping; the lockfile itself enforces ordering across processes
		counter int
		errs    = make(chan error, goroutines)
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			fl, err := lockfile.Acquire(lockPath)
			if err != nil {
				errs <- err
				return
			}

			// Critical section: increment shared counter while holding the lock.
			mu.Lock()
			counter++
			mu.Unlock()

			if err := fl.Release(); err != nil {
				errs <- err
				return
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("goroutine error: %v", err)
	}

	if counter != goroutines {
		t.Errorf("expected counter == %d, got %d", goroutines, counter)
	}
}

func TestAcquireNonBlocking_Free(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	fl, err := lockfile.AcquireNonBlocking(lockPath)
	if err != nil {
		t.Fatalf("AcquireNonBlocking on free path: %v", err)
	}
	t.Cleanup(func() { _ = fl.Release() })

	if fl == nil {
		t.Fatal("expected non-nil lockfile")
	}
}

func TestAcquireNonBlocking_Contended(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "test.lock")

	acquired := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		fl, err := lockfile.Acquire(lockPath)
		if err != nil {
			t.Errorf("G1 Acquire: %v", err)
			close(acquired)
			return
		}
		close(acquired)
		<-release
		_ = fl.Release()
	}()

	<-acquired

	fl, err := lockfile.AcquireNonBlocking(lockPath)
	if err == nil {
		_ = fl.Release()
		close(release)
		<-done
		t.Fatal("expected ErrLocked, got nil error")
	}
	if !errors.Is(err, lockfile.ErrLocked) {
		close(release)
		<-done
		t.Fatalf("expected ErrLocked, got: %v", err)
	}

	close(release)
	<-done
}

func TestAcquire_AutoCreatesParent(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "nonexistent-parent", "foo.lock")

	fl, err := lockfile.Acquire(lockPath)
	if err != nil {
		t.Fatalf("Acquire on nonexistent parent: %v", err)
	}
	t.Cleanup(func() { _ = fl.Release() })

	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("lock file should exist on disk: %v", err)
	}
}

// TestAcquire_OpenFileError exercises the error path where MkdirAll
// succeeds but OpenFile fails because the lock path itself is a directory.
func TestAcquire_OpenFileError(t *testing.T) {
	dir := t.TempDir()
	// Make the "lock path" a directory so OpenFile(O_RDWR) on it fails.
	lockPath := filepath.Join(dir, "is-a-dir")
	if err := os.Mkdir(lockPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := lockfile.Acquire(lockPath); err == nil {
		t.Fatal("expected error opening directory as lock file, got nil")
	}
	if _, err := lockfile.AcquireNonBlocking(lockPath); err == nil {
		t.Fatal("expected error opening directory as lock file, got nil")
	}
}

// TestAcquire_MkdirError exercises the parent-create failure path: the
// parent is an existing regular file, so MkdirAll fails.
func TestAcquire_MkdirError(t *testing.T) {
	dir := t.TempDir()
	parentAsFile := filepath.Join(dir, "parent")
	if err := os.WriteFile(parentAsFile, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	lockPath := filepath.Join(parentAsFile, "child.lock")

	if _, err := lockfile.Acquire(lockPath); err == nil {
		t.Fatal("expected MkdirAll failure, got nil")
	}
	if _, err := lockfile.AcquireNonBlocking(lockPath); err == nil {
		t.Fatal("expected MkdirAll failure, got nil")
	}
}
