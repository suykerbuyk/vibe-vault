// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import "strings"

// Detector recognizes a harness-specific lock-reason marker, extracts
// the holder PID, and reports the expected branch-name layout for a
// worktree that the harness owns.
type Detector interface {
	// Name returns the harness identifier (e.g. "claude").
	Name() string
	// Detect attempts to parse a lock-reason string. It returns the
	// holder PID and ok=true if the reason matches this detector's
	// format; otherwise ok=false. Implementations MUST NOT trim
	// whitespace — the caller (ParseMarker) does that exactly once.
	Detect(reason string) (pid int, ok bool)
	// ExpectedBranch returns the branch name this detector expects a
	// worktree directory of the given basename to be checked out on.
	ExpectedBranch(worktreeName string) string
}

// defaultDetectors is the package-level fallback when Options.Detectors
// is nil. Order matters: ParseMarker tries each in sequence and the
// first match wins.
var defaultDetectors = []Detector{claudeDetector{}}

// ParseMarker tries each detector in order; the first match wins.
// Caller need not pre-trim — ParseMarker calls strings.TrimSpace
// internally exactly once before dispatching to detectors. If
// detectors is nil, falls back to the package defaultDetectors.
func ParseMarker(reasonText string, detectors []Detector) (harness string, pid int, detector Detector) {
	if detectors == nil {
		detectors = defaultDetectors
	}
	s := strings.TrimSpace(reasonText)
	for _, d := range detectors {
		if pid, ok := d.Detect(s); ok {
			return d.Name(), pid, d
		}
	}
	return "", 0, nil
}
