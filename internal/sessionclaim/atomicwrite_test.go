// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionclaim

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAtomicWrite_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claim.json")
	want := []byte(`{"hello":"world"}`)

	if err := atomicWrite(path, want); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("ReadFile = %q, want %q", got, want)
	}
}

func TestAtomicWrite_Mode0o600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claim.json")
	if err := atomicWrite(path, []byte("{}")); err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("mode = %o, want 0600", mode)
	}
}

func TestAtomicWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claim.json")
	if err := atomicWrite(path, []byte("A")); err != nil {
		t.Fatalf("atomicWrite A: %v", err)
	}
	if err := atomicWrite(path, []byte("B")); err != nil {
		t.Fatalf("atomicWrite B: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "B" {
		t.Errorf("after overwrite ReadFile = %q, want %q", got, "B")
	}
}

func TestAtomicWrite_ConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claim.json")

	const N = 16
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := atomicWrite(path, []byte{byte('A' + i)}); err != nil {
				t.Errorf("atomicWrite[%d]: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len(got) = %d, want 1 — atomic write did not produce a single-byte payload", len(got))
	}
	// Confirm one of the writers' bytes won; no corruption.
	if got[0] < 'A' || got[0] >= 'A'+N {
		t.Errorf("got byte %q outside writer range", got[0])
	}

	// No leftover temp files in the directory — defer-cleanup paths
	// must reliably remove ".tmp.*" siblings.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != "claim.json" {
			t.Errorf("leftover entry %q after concurrent writers", name)
		}
	}
}
