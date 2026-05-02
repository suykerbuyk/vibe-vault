// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

//go:build windows

package pidlive

import (
	"github.com/shirou/gopsutil/v3/process"
)

// isAliveImpl on Windows uses gopsutil's PidExists, which wraps
// OpenProcess + GetExitCodeProcess to determine whether the PID is
// currently mapped to a running process.
func isAliveImpl(pid int) bool {
	exists, err := process.PidExists(int32(pid))
	if err != nil {
		// Fail-closed: treat unknown errors as alive to avoid sweeping
		// a still-running session.
		return true
	}
	return exists
}

// starttimeImpl on Windows returns (0, nil): the platform offers a
// reasonable signal via gopsutil, but treating a Windows host as
// "no liveness signal available" keeps the contract simple. Validate
// will see the constant zero and treat any stored value other than 0
// as a mismatch — meaning no claim file is ever ratified after the
// minting process exits, which is the safe default on Windows.
func starttimeImpl(pid int) (int64, error) {
	return 0, nil
}

// parentNameImpl on Windows returns "" with nil error. Harness
// detection falls through to "unknown" by design (decision 6).
func parentNameImpl(ppid int) (string, error) {
	return "", nil
}
