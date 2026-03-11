// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"os"
	"path/filepath"
	"strings"
)

// CommandDiff holds diff data for a single outdated command.
type CommandDiff struct {
	Name    string // e.g. "wrap.md"
	Current string // current file content
	Pending string // pending update content
}

// DiffProjectCommands scans agentctxPath/commands/ for .pending files
// and returns content pairs for the current command and the pending update.
func DiffProjectCommands(agentctxPath string) []CommandDiff {
	cmdsDir := filepath.Join(agentctxPath, "commands")
	entries, err := os.ReadDir(cmdsDir)
	if err != nil {
		return nil
	}

	var diffs []CommandDiff
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md.pending") {
			continue
		}
		baseName := strings.TrimSuffix(e.Name(), ".pending")

		pendingPath := filepath.Join(cmdsDir, e.Name())
		currentPath := filepath.Join(cmdsDir, baseName)

		pendingData, err := os.ReadFile(pendingPath)
		if err != nil {
			continue
		}
		currentData, err := os.ReadFile(currentPath)
		if err != nil {
			continue
		}

		diffs = append(diffs, CommandDiff{
			Name:    baseName,
			Current: string(currentData),
			Pending: string(pendingData),
		})
	}
	return diffs
}
