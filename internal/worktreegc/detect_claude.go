// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package worktreegc

import (
	"regexp"
	"strconv"
)

// claudeDetector recognizes lock-reason markers written by the
// Claude-agent harness.
type claudeDetector struct{}

// Name reports the harness identifier.
func (claudeDetector) Name() string { return "claude" }

// claudeRegexes lists the formats this detector accepts. Newest format
// variants should be PREPENDED here; older formats remain at the tail
// for back-compat with markers written by prior binary versions.
var claudeRegexes = []*regexp.Regexp{
	regexp.MustCompile(`^claude agent agent-[a-f0-9]{16,} \(pid (\d+)\)$`),
	// Future format variations prepended here. Older formats remain at
	// the tail for back-compat with markers from prior binary versions.
}

// Detect tries each known regex in order and returns the holder PID on
// first match. ParseMarker already TrimSpace'd the reason; we don't
// re-trim.
func (claudeDetector) Detect(reason string) (int, bool) {
	// ParseMarker already TrimSpace'd; we don't re-trim.
	for _, re := range claudeRegexes {
		if m := re.FindStringSubmatch(reason); m != nil {
			pid, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			return pid, true
		}
	}
	return 0, false
}

// ExpectedBranch returns the branch name this harness uses for a given
// worktree directory basename: "worktree-<name>".
func (claudeDetector) ExpectedBranch(worktreeName string) string {
	return "worktree-" + worktreeName
}
