// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// iterNarrativeRe matches the H3 narrative header used in iterations.md
// (e.g., "### Iteration 168 — title (date)"). The capture group is the
// project-wide iteration number.
var iterNarrativeRe = regexp.MustCompile(`(?m)^### Iteration (\d+)\b`)

// nextIterFromIterationsMD parses iterations.md for a project and returns
// max(### Iteration N) + 1. Returns 1 when the file is missing, unreadable,
// or contains no matching headers — the canonical "fresh project" signal.
func nextIterFromIterationsMD(vaultPath, project string) (int, error) {
	if vaultPath == "" || project == "" {
		return 1, nil
	}
	path := filepath.Join(vaultPath, "Projects", project, "agentctx", "iterations.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	max := 0
	for _, m := range iterNarrativeRe.FindAllStringSubmatch(string(data), -1) {
		if len(m) < 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err == nil && n > max {
			max = n
		}
	}
	return max + 1, nil
}
