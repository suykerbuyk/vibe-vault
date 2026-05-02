// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package pidlive provides cross-platform process-liveness checks and
// parent-process metadata. It is the canonical liveness predicate for
// session-slot-multihost-disambiguation Mechanism 2: a stored claim's
// (ppid, ppid_starttime) triple is validated by Validate to confirm
// that the parent process is still alive AND its start-time matches
// what the claim file recorded.
//
// All public entry points are exposed as package-level function vars
// (the L9 testability pattern from the plan): tests substitute these
// to inject mocked PID/starttime/parent-name values without spinning
// real processes. Production code calls the vars (e.g. pidlive.IsAlive)
// so test-time substitution is observable from callers.
package pidlive

// IsAlive reports whether pid maps to a live process on this host.
//
// Linux semantics (kill(pid, 0)):
//   - nil error: process exists and we own it.
//   - syscall.EPERM: process exists but is owned by another user (still
//     alive — we just lack permission to signal it).
//   - syscall.ESRCH: process is gone.
//   - any other error: treated as alive (fail-closed) to avoid sweeping
//     a still-running session due to a transient kernel error.
var IsAlive = isAliveImpl

// Starttime returns the process start-time for pid as reported by
// gopsutil (milliseconds since the Unix epoch). Returns (0, err) on
// any failure. On Windows the implementation may return (0, nil) when
// no signal is available; this is the "no-signal" case, not a failure.
var Starttime = starttimeImpl

// Validate reports whether (pid, expectedStarttime) refers to the same
// process the claim was minted against: pid must be alive AND its
// current start-time must equal expectedStarttime exactly.
//
// PID-reuse defense: a fresh process binding to the same PID will have
// a different start-time, and Validate returns false. This is the
// load-bearing predicate for sessionclaim's stale-claim sweep.
var Validate = validateImpl

// ParentName returns the basename of the parent process command:
//   - Linux: contents of /proc/<ppid>/comm (already a basename).
//   - macOS: gopsutil/v3 process.Process.Name().
//   - Windows: returns "" with no error ("no signal" case).
//
// On any failure (file missing, permission denied, gopsutil error),
// ParentName returns ("", err) and the caller falls back to harness =
// "unknown".
var ParentName = parentNameImpl

func validateImpl(pid int, expectedStarttime int64) bool {
	if !IsAlive(pid) {
		return false
	}
	got, err := Starttime(pid)
	if err != nil {
		return false
	}
	return got == expectedStarttime
}
