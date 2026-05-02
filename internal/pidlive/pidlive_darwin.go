// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

//go:build darwin

package pidlive

import (
	"errors"
	"fmt"
	"syscall"

	"github.com/shirou/gopsutil/v3/process"
)

// isAliveImpl on macOS mirrors the Linux kill(pid, 0) probe. The
// semantics are identical (POSIX) — nil and EPERM mean alive, ESRCH
// means dead.
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

// parentNameImpl on macOS uses gopsutil's Name(), which returns a
// basename derived from the process's executable path.
func parentNameImpl(ppid int) (string, error) {
	p, err := process.NewProcess(int32(ppid))
	if err != nil {
		return "", fmt.Errorf("pidlive: NewProcess(%d): %w", ppid, err)
	}
	name, err := p.Name()
	if err != nil {
		return "", fmt.Errorf("pidlive: Name(%d): %w", ppid, err)
	}
	return name, nil
}
