// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

//go:build linux

package pidlive

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/v3/process"
)

// isAliveImpl is the Linux PID-liveness probe. Mirrors the kill(pid, 0)
// pattern at internal/worktreegc/worktreegc.go:102 — nil and EPERM mean
// alive, ESRCH means dead, anything else is treated as alive
// (fail-closed) to avoid sweeping a still-running session due to a
// transient kernel error.
func isAliveImpl(pid int) bool {
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil:
		return true
	case errors.Is(err, syscall.EPERM):
		return true
	case errors.Is(err, syscall.ESRCH):
		return false
	default:
		return true
	}
}

// starttimeImpl wraps gopsutil's CreateTime. Returns milliseconds since
// the Unix epoch.
func starttimeImpl(pid int) (int64, error) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return 0, fmt.Errorf("pidlive: NewProcess(%d): %w", pid, err)
	}
	ms, err := p.CreateTime()
	if err != nil {
		return 0, fmt.Errorf("pidlive: CreateTime(%d): %w", pid, err)
	}
	return ms, nil
}

// parentNameImpl reads /proc/<ppid>/comm. The kernel writes a basename
// (truncated to 15 chars) here, which is the desired form. Trailing
// newline is stripped.
func parentNameImpl(ppid int) (string, error) {
	path := "/proc/" + strconv.Itoa(ppid) + "/comm"
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("pidlive: read %s: %w", path, err)
	}
	return strings.TrimRight(string(data), "\n"), nil
}
