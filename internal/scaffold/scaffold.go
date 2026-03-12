// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed all:templates
var templates embed.FS

// EmbeddedFS returns the embedded template filesystem.
func EmbeddedFS() embed.FS { return templates }

// Options controls scaffold behavior.
type Options struct {
	GitInit bool // run git init after scaffolding
}

// vaultState describes what already exists at a target path.
type vaultState int

const (
	vaultNone        vaultState = iota // nothing exists
	vaultVibeVault                     // .obsidian/ + Projects/ — an existing vibe-vault
	vaultObsidian                      // .obsidian/ only — not a vibe-vault
	vaultStateDir                      // .vibe-vault/ only — state dir exists
)

// detectVault inspects targetPath and returns its vault state.
func detectVault(targetPath string) vaultState {
	hasObsidian := dirExists(filepath.Join(targetPath, ".obsidian"))
	hasProjects := dirExists(filepath.Join(targetPath, "Projects"))
	hasState := dirExists(filepath.Join(targetPath, ".vibe-vault"))

	switch {
	case hasObsidian && hasProjects:
		return vaultVibeVault
	case hasObsidian:
		return vaultObsidian
	case hasState:
		return vaultStateDir
	default:
		return vaultNone
	}
}

// Init creates or adopts a vibe-vault Obsidian vault at targetPath.
// It returns an action string ("created" or "adopted") and any error.
func Init(targetPath string, opts Options) (string, error) {
	targetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}

	switch detectVault(targetPath) {
	case vaultVibeVault:
		// Existing vibe-vault — adopt it (just write config, skip scaffolding).
		return "adopted", nil
	case vaultObsidian:
		return "", fmt.Errorf("%s contains .obsidian/ but no Projects/ — looks like an Obsidian vault, not a vibe-vault", targetPath)
	case vaultStateDir:
		return "", fmt.Errorf("%s already contains .vibe-vault/ — refusing to overwrite", targetPath)
	}

	// vaultNone — scaffold a new vault.
	vaultName := filepath.Base(targetPath)

	// Walk embedded templates and copy to target.
	err = fs.WalkDir(templates, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Strip the "templates/" prefix to get the relative path within the vault.
		rel, err := filepath.Rel("templates", path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		dest := filepath.Join(targetPath, rel)

		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}

		data, err := templates.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}

		// Template substitution for README.md
		if rel == "README.md" {
			data = []byte(strings.ReplaceAll(string(data), "{{VAULT_NAME}}", vaultName))
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}

		perm := filePermission(rel)
		return os.WriteFile(dest, data, perm)
	})
	if err != nil {
		return "", fmt.Errorf("scaffold vault: %w", err)
	}

	if opts.GitInit {
		cmd := exec.Command("git", "init", targetPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git init: %w", err)
		}
	}

	return "created", nil
}

// filePermission returns 0o755 for shell scripts and git hooks, 0o644 for everything else.
func filePermission(rel string) os.FileMode {
	if strings.HasSuffix(rel, ".sh") {
		return 0o755
	}
	if strings.HasPrefix(rel, ".githooks/") || strings.HasPrefix(rel, filepath.Join(".githooks")+string(filepath.Separator)) {
		return 0o755
	}
	return 0o644
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
