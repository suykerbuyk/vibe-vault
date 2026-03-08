// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package templates

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResetAction describes a single reset operation.
type ResetAction struct {
	RelPath string
	Action  string // "reset" or "created"
}

// Reset overwrites a single vault template with its built-in default.
func (r *Registry) Reset(vaultTemplatesDir, relPath string) (ResetAction, error) {
	content, ok := r.DefaultContent(relPath)
	if !ok {
		return ResetAction{}, fmt.Errorf("unknown template: %s", relPath)
	}

	dest := filepath.Join(vaultTemplatesDir, relPath)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return ResetAction{}, err
	}

	action := "reset"
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		action = "created"
	}

	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return ResetAction{}, err
	}

	return ResetAction{RelPath: relPath, Action: action}, nil
}

// ResetAll overwrites all vault templates with their built-in defaults.
func (r *Registry) ResetAll(vaultTemplatesDir string) ([]ResetAction, error) {
	var actions []ResetAction
	for _, e := range r.entries {
		a, err := r.Reset(vaultTemplatesDir, e.RelPath)
		if err != nil {
			return actions, fmt.Errorf("reset %s: %w", e.RelPath, err)
		}
		actions = append(actions, a)
	}
	return actions, nil
}
