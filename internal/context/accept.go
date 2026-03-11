// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"os"
	"path/filepath"
	"strings"
)

// AcceptCommands processes pending command updates in agentctxPath/commands/.
// If keepMine is true, it writes a .pinned marker and removes .pending (keeping
// the current version). Otherwise it copies .pending over the original and
// removes .pending.
// If file is non-empty, only that file is processed; otherwise all .pending files.
func AcceptCommands(agentctxPath, file string, keepMine bool) ([]FileAction, error) {
	cmdsDir := filepath.Join(agentctxPath, "commands")

	if file != "" {
		action, err := acceptOne(cmdsDir, file, keepMine)
		if err != nil {
			return nil, err
		}
		return []FileAction{action}, nil
	}

	// Process all .pending files
	entries, err := os.ReadDir(cmdsDir)
	if err != nil {
		return nil, err
	}

	var actions []FileAction
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md.pending") {
			continue
		}
		baseName := strings.TrimSuffix(e.Name(), ".pending")
		action, err := acceptOne(cmdsDir, baseName, keepMine)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func acceptOne(cmdsDir, name string, keepMine bool) (FileAction, error) {
	pendingPath := filepath.Join(cmdsDir, name+".pending")
	originalPath := filepath.Join(cmdsDir, name)

	if keepMine {
		// Write .pinned marker and remove .pending
		pinnedPath := filepath.Join(cmdsDir, name+".pinned")
		if err := os.WriteFile(pinnedPath, []byte("pinned\n"), 0o644); err != nil {
			return FileAction{}, err
		}
		os.Remove(pendingPath)
		return FileAction{
			Path:     "commands/" + name,
			Action:   "SKIP",
			Location: "vault",
		}, nil
	}

	// Accept: copy .pending over original, remove .pending
	data, err := os.ReadFile(pendingPath)
	if err != nil {
		return FileAction{}, err
	}
	if err := os.WriteFile(originalPath, data, 0o644); err != nil {
		return FileAction{}, err
	}
	os.Remove(pendingPath)
	return FileAction{
		Path:     "commands/" + name,
		Action:   "UPDATE",
		Location: "vault",
	}, nil
}
