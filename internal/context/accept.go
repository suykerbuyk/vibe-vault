// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"os"
	"path/filepath"
	"strings"
)

// AcceptPending processes pending updates across all propagated subdirectories
// (commands, skills). If keepMine is true, it writes a .pinned marker and
// removes .pending (keeping the current version). Otherwise it copies .pending
// over the original and removes .pending.
// If file is non-empty (e.g. "commands/wrap.md"), only that file is processed;
// otherwise all .pending files across all subdirs.
func AcceptPending(agentctxPath, file string, keepMine bool) ([]FileAction, error) {
	if file != "" {
		// Parse subdir from qualified path (e.g. "commands/wrap.md").
		// Fall back to commands/ for bare filenames (backward compat).
		subdir := "commands"
		name := file
		if i := strings.IndexByte(file, '/'); i >= 0 {
			subdir = file[:i]
			name = file[i+1:]
		}
		dir := filepath.Join(agentctxPath, subdir)
		action, err := acceptOne(dir, subdir, name, keepMine)
		if err != nil {
			return nil, err
		}
		return []FileAction{action}, nil
	}

	var actions []FileAction
	for _, sub := range propagateDirs {
		subActions, err := acceptSubdir(agentctxPath, sub, keepMine)
		if err != nil {
			return nil, err
		}
		actions = append(actions, subActions...)
	}
	return actions, nil
}

// AcceptCommands processes pending command updates in agentctxPath/commands/.
// Deprecated: use AcceptPending for multi-subdir processing.
func AcceptCommands(agentctxPath, file string, keepMine bool) ([]FileAction, error) {
	cmdsDir := filepath.Join(agentctxPath, "commands")

	if file != "" {
		action, err := acceptOne(cmdsDir, "commands", file, keepMine)
		if err != nil {
			return nil, err
		}
		return []FileAction{action}, nil
	}

	return acceptSubdir(agentctxPath, "commands", keepMine)
}

func acceptSubdir(agentctxPath, subdir string, keepMine bool) ([]FileAction, error) {
	dir := filepath.Join(agentctxPath, subdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var actions []FileAction
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pending") {
			continue
		}
		baseName := strings.TrimSuffix(e.Name(), ".pending")
		action, err := acceptOne(dir, subdir, baseName, keepMine)
		if err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func acceptOne(dir, subdir, name string, keepMine bool) (FileAction, error) {
	pendingPath := filepath.Join(dir, name+".pending")
	originalPath := filepath.Join(dir, name)

	if keepMine {
		pinnedPath := filepath.Join(dir, name+".pinned")
		if err := os.WriteFile(pinnedPath, []byte("pinned\n"), 0o644); err != nil {
			return FileAction{}, err
		}
		os.Remove(pendingPath)
		return FileAction{
			Path:     subdir + "/" + name,
			Action:   "SKIP",
			Location: "vault",
		}, nil
	}

	data, err := os.ReadFile(pendingPath)
	if err != nil {
		return FileAction{}, err
	}
	if err := os.WriteFile(originalPath, data, 0o644); err != nil {
		return FileAction{}, err
	}
	os.Remove(pendingPath)
	return FileAction{
		Path:     subdir + "/" + name,
		Action:   "UPDATE",
		Location: "vault",
	}, nil
}
