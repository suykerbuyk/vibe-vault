// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeStubScript writes an executable shell script at <dir>/<name> with
// the given body. The caller is expected to have already pointed PATH at
// dir via t.Setenv. Returns the absolute path for diagnostic use.
func writeStubScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", path, err)
	}
	return path
}

func TestCheckToolchain_StubPresent_Pass(t *testing.T) {
	dir := t.TempDir()
	writeStubScript(t, dir, "gh", "#!/bin/sh\necho \"gh version 2.50.0 (test stub)\"\nexit 0\n")
	t.Setenv("PATH", dir)

	spec := toolchainSpec{
		Bin:         "gh",
		VersionArgs: []string{"--version"},
		InstallHint: "install via OS package manager or https://cli.github.com",
	}
	got := checkToolchainSpec(spec)

	if got.Status != Pass {
		t.Fatalf("status: got %v, want Pass; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "gh version 2.50.0") {
		t.Errorf("detail %q missing version string", got.Detail)
	}
	if got.Name != "tool:gh" {
		t.Errorf("name: got %q, want %q", got.Name, "tool:gh")
	}
}

func TestCheckToolchain_StubExit1_Warn(t *testing.T) {
	dir := t.TempDir()
	writeStubScript(t, dir, "gh", "#!/bin/sh\necho \"oops\"\nexit 1\n")
	t.Setenv("PATH", dir)

	hint := "install via OS package manager or https://cli.github.com"
	spec := toolchainSpec{
		Bin:         "gh",
		VersionArgs: []string{"--version"},
		InstallHint: hint,
	}
	got := checkToolchainSpec(spec)

	if got.Status != Warn {
		t.Fatalf("status: got %v, want Warn; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "--version failed") {
		t.Errorf("detail %q missing %q", got.Detail, "--version failed")
	}
	if !strings.Contains(got.Detail, hint) {
		t.Errorf("detail %q missing install hint %q", got.Detail, hint)
	}
	if got.Name != "tool:gh" {
		t.Errorf("name: got %q, want %q", got.Name, "tool:gh")
	}
}

func TestCheckToolchain_Absent_Warn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	hint := "install via OS package manager"
	spec := toolchainSpec{
		Bin:         "vv-toolchain-test-noop-binary-2026",
		VersionArgs: []string{"--version"},
		InstallHint: hint,
	}
	got := checkToolchainSpec(spec)

	if got.Status != Warn {
		t.Fatalf("status: got %v, want Warn; detail=%q", got.Status, got.Detail)
	}
	if !strings.Contains(got.Detail, "not installed") {
		t.Errorf("detail %q missing %q", got.Detail, "not installed")
	}
	if !strings.Contains(got.Detail, hint) {
		t.Errorf("detail %q missing install hint %q", got.Detail, hint)
	}
	if got.Name != "tool:vv-toolchain-test-noop-binary-2026" {
		t.Errorf("name: got %q, want %q", got.Name, "tool:vv-toolchain-test-noop-binary-2026")
	}
}

func TestCheckToolchain_EmptyOutput_PassWithMessage(t *testing.T) {
	dir := t.TempDir()
	writeStubScript(t, dir, "quietbin", "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", dir)

	spec := toolchainSpec{
		Bin:         "quietbin",
		VersionArgs: []string{"--version"},
		InstallHint: "n/a",
	}
	got := checkToolchainSpec(spec)

	if got.Status != Pass {
		t.Fatalf("status: got %v, want Pass; detail=%q", got.Status, got.Detail)
	}
	want := "quietbin --version: no output"
	if got.Detail != want {
		t.Errorf("detail: got %q, want %q", got.Detail, want)
	}
	if got.Name != "tool:quietbin" {
		t.Errorf("name: got %q, want %q", got.Name, "tool:quietbin")
	}
}

func TestCheckToolchain_AllProbes_RealHostSmoke(t *testing.T) {
	results := CheckToolchain()
	if got, want := len(results), len(toolchainSpecs); got != want {
		t.Fatalf("len(results): got %d, want %d", got, want)
	}
	for i, r := range results {
		if !strings.HasPrefix(r.Name, "tool:") {
			t.Errorf("results[%d].Name = %q, want prefix %q", i, r.Name, "tool:")
		}
		// Ordering sanity: Name should match the spec at the same index.
		wantName := "tool:" + toolchainSpecs[i].Bin
		if r.Name != wantName {
			t.Errorf("results[%d].Name = %q, want %q", i, r.Name, wantName)
		}
	}
}

func TestCheckToolchain_GitVersionRealBinary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not present on host; skipping real-binary smoke")
	}
	spec := toolchainSpec{
		Bin:         "git",
		VersionArgs: []string{"--version"},
		InstallHint: "install via OS package manager",
	}
	got := checkToolchainSpec(spec)
	if got.Status != Pass {
		t.Fatalf("status: got %v, want Pass; detail=%q", got.Status, got.Detail)
	}
	if got.Name != "tool:git" {
		t.Errorf("name: got %q, want %q", got.Name, "tool:git")
	}
}
