// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"os"
	"path/filepath"
	"strings"
)

// ContentDiff holds diff data for a single outdated file.
type ContentDiff struct {
	Name    string // subdir-qualified, e.g. "commands/wrap.md"
	Current string // current file content
	Pending string // pending update content
}

// DiffProjectContent scans agentctxPath for .pending files across all
// propagated subdirectories (commands, skills) and returns content pairs
// for the current file and the pending update.
func DiffProjectContent(agentctxPath string) []ContentDiff {
	var diffs []ContentDiff
	for _, sub := range propagateDirs {
		diffs = append(diffs, diffSubdir(agentctxPath, sub)...)
	}
	return diffs
}

// DiffProjectCommands scans agentctxPath/commands/ for .pending files.
// Deprecated: use DiffProjectContent for multi-subdir scanning.
func DiffProjectCommands(agentctxPath string) []ContentDiff {
	return diffSubdir(agentctxPath, "commands")
}

func diffSubdir(agentctxPath, subdir string) []ContentDiff {
	dir := filepath.Join(agentctxPath, subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var diffs []ContentDiff
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pending") {
			continue
		}
		baseName := strings.TrimSuffix(e.Name(), ".pending")

		pendingPath := filepath.Join(dir, e.Name())
		currentPath := filepath.Join(dir, baseName)

		pendingData, err := os.ReadFile(pendingPath)
		if err != nil {
			continue
		}
		currentData, err := os.ReadFile(currentPath)
		if err != nil {
			continue
		}

		diffs = append(diffs, ContentDiff{
			Name:    subdir + "/" + baseName,
			Current: string(currentData),
			Pending: string(pendingData),
		})
	}
	return diffs
}
