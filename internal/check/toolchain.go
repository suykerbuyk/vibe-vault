// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// toolchainSpec describes a single external binary the toolchain probe
// inspects: which executable to look up on PATH, which arguments to pass
// to elicit a version banner, and what hint to surface to the operator
// when the binary is missing or its --version invocation fails.
type toolchainSpec struct {
	Bin         string   // "gh"
	VersionArgs []string // ["--version"]
	InstallHint string   // "install via OS package manager or https://cli.github.com"
}

// toolchainSpecs enumerates the binaries CheckToolchain probes, in the
// order their results are reported. The set covers the build, lint, GitHub
// integration, build-driver, and version-control surfaces that vv depends
// on at development and operational time.
var toolchainSpecs = []toolchainSpec{
	{Bin: "go", VersionArgs: []string{"version"}, InstallHint: "install Go from https://go.dev/dl/"},
	{Bin: "golangci-lint", VersionArgs: []string{"--version"}, InstallHint: "install via `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` or a tagged release"},
	{Bin: "gh", VersionArgs: []string{"--version"}, InstallHint: "install via OS package manager or https://cli.github.com"},
	{Bin: "make", VersionArgs: []string{"--version"}, InstallHint: "install via OS package manager (build-essential / xcode-select)"},
	{Bin: "git", VersionArgs: []string{"--version"}, InstallHint: "install via OS package manager"},
}

// CheckToolchain probes each spec sequentially and returns one Result per
// binary. Results carry a "tool:<bin>" Name so JSON projection can route
// them unambiguously alongside the existing aggregator output. The probe
// is invisible in production until Phases 2 and 3 wire it into the CLI
// and MCP surfaces.
func CheckToolchain() []Result {
	results := make([]Result, 0, len(toolchainSpecs))
	for _, spec := range toolchainSpecs {
		results = append(results, checkToolchainSpec(spec))
	}
	return results
}

// checkToolchainSpec probes a single spec. Package-private; exists as a
// refactoring seam so individual specs can be tested without invoking the
// whole toolchainSpecs slice. Mirrors the pattern of checkHookFile(path
// string) for CheckHook().
func checkToolchainSpec(spec toolchainSpec) Result {
	name := "tool:" + spec.Bin

	path, err := exec.LookPath(spec.Bin)
	if err != nil {
		// Any LookPath failure — exec.ErrNotFound or otherwise (e.g. PATH
		// entries unreadable) — funnels into the same operator-facing
		// "not installed" warning.
		return Result{
			Name:   name,
			Status: Warn,
			Detail: spec.Bin + ": not installed — " + spec.InstallHint,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, spec.VersionArgs...)
	out, err := cmd.Output()
	if err != nil {
		// Non-zero exit, context-timeout cancellation, and any other
		// invocation failure all funnel here. The operator-facing
		// message is intentionally uniform: --version did not work.
		return Result{
			Name:   name,
			Status: Warn,
			Detail: spec.Bin + ": --version failed — " + spec.InstallHint,
		}
	}

	detail := firstNonEmptyLine(out)
	if detail == "" {
		return Result{
			Name:   name,
			Status: Pass,
			Detail: spec.Bin + " --version: no output",
		}
	}
	return Result{
		Name:   name,
		Status: Pass,
		Detail: detail,
	}
}

// firstNonEmptyLine splits out on '\n', trims whitespace from each line,
// and returns the first line that is not empty. Returns "" when out is
// empty or all lines are blank.
func firstNonEmptyLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
