// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package pidlive

import (
	"os"
	"runtime"
	"testing"
)

func TestIsAlive_Self(t *testing.T) {
	if !IsAlive(os.Getpid()) {
		t.Fatalf("IsAlive(%d) = false; want true (own pid)", os.Getpid())
	}
}

func TestIsAlive_Dead(t *testing.T) {
	// PID 9999999 is well above default kernel.pid_max defaults on
	// Linux (32768/4194304); even on hosts with custom limits this is
	// vanishingly unlikely to map to a real process during a test run.
	const deadPID = 9999999
	if IsAlive(deadPID) {
		t.Fatalf("IsAlive(%d) = true; want false (synthetic high pid)", deadPID)
	}
}

func TestStarttime_Stable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows starttimeImpl returns constant 0")
	}
	a, err := Starttime(os.Getpid())
	if err != nil {
		t.Fatalf("Starttime: %v", err)
	}
	b, err := Starttime(os.Getpid())
	if err != nil {
		t.Fatalf("Starttime: %v", err)
	}
	if a != b {
		t.Errorf("Starttime not stable: %d vs %d", a, b)
	}
}

func TestValidate_Match(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows starttimeImpl returns constant 0; Validate semantics differ")
	}
	st, err := Starttime(os.Getpid())
	if err != nil {
		t.Fatalf("Starttime: %v", err)
	}
	if !Validate(os.Getpid(), st) {
		t.Errorf("Validate(self, currentStarttime) = false; want true")
	}
	if Validate(os.Getpid(), st+1) {
		t.Errorf("Validate(self, currentStarttime+1) = true; want false")
	}
}

func TestParentName_NonEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows parentNameImpl returns empty by design")
	}
	name, err := ParentName(os.Getppid())
	if err != nil {
		t.Fatalf("ParentName(%d): %v", os.Getppid(), err)
	}
	if name == "" {
		// Don't assert the specific value — could be `go`, `bash`,
		// `claude`, etc. depending on the test runner.
		t.Errorf("ParentName(%d) = empty; want non-empty on %s", os.Getppid(), runtime.GOOS)
	}
}

// TestFunctionVarOverride exercises the L9 testability pattern: tests
// must be able to swap any of the four package-level vars and observe
// the override from a wrapper that calls through the var (rather than
// the unexported impl directly).
func TestFunctionVarOverride(t *testing.T) {
	origAlive := IsAlive
	origStart := Starttime
	origParent := ParentName
	origValidate := Validate
	t.Cleanup(func() {
		IsAlive = origAlive
		Starttime = origStart
		ParentName = origParent
		Validate = origValidate
	})

	IsAlive = func(int) bool { return true }
	Starttime = func(int) (int64, error) { return 42, nil }
	ParentName = func(int) (string, error) { return "test-parent", nil }
	Validate = func(int, int64) bool { return true }

	if !IsAlive(0) {
		t.Errorf("override IsAlive not visible")
	}
	if v, _ := Starttime(0); v != 42 {
		t.Errorf("override Starttime not visible: got %d", v)
	}
	if n, _ := ParentName(0); n != "test-parent" {
		t.Errorf("override ParentName not visible: got %q", n)
	}
	if !Validate(0, 0) {
		t.Errorf("override Validate not visible")
	}
}
