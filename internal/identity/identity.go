// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package identity

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const fileName = ".vibe-vault.toml"

// ErrNoProjectMarker is returned by FindMarker when no .vibe-vault.toml is
// found in startDir or any ancestor directory up to the filesystem root.
var ErrNoProjectMarker = errors.New("no .vibe-vault.toml found in current directory or any ancestor")

// ProjectIdentity holds the contents of a .vibe-vault.toml file.
type ProjectIdentity struct {
	Project ProjectConfig `toml:"project"`
	Meta    MetaConfig    `toml:"meta"`
}

// ProjectConfig identifies the project.
type ProjectConfig struct {
	Name   string   `toml:"name"`
	Domain string   `toml:"domain"`
	Tags   []string `toml:"tags"`
}

// MetaConfig holds optional project metadata.
type MetaConfig struct {
	Author  string `toml:"author"`
	Company string `toml:"company"`
}

// Template returns a commented-out .vibe-vault.toml template.
// When all values are commented out, detection falls back to heuristics.
// Any uncommented value takes precedence over heuristic detection.
func Template(project string) string {
	return `# vibe-vault project identity
# Uncomment and set values to override automatic detection.
# When commented out, vv uses heuristics (git remote name, directory basename).

[project]
# name = "` + project + `"
# domain = ""          # e.g. "work", "personal", "opensource", or custom
# tags = []            # e.g. ["go", "cli", "obsidian"]

# [meta]
# author = ""
# company = ""
`
}

// FileName returns the identity file name.
func FileName() string {
	return fileName
}

// FindMarker walks up from startDir looking for a .vibe-vault.toml file.
// Returns the directory containing the marker, or ErrNoProjectMarker if none
// is found up to the filesystem root. Existence alone counts — the marker
// may be the commented-out template (all values commented) and still signals
// that this is an initialized vibe-vault project.
func FindMarker(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, fileName)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoProjectMarker
		}
		dir = parent
	}
}

// Load reads a .vibe-vault.toml from the given directory.
// Returns (nil, nil) if the file doesn't exist or if Project.Name is empty.
func Load(dir string) (*ProjectIdentity, error) {
	path := filepath.Join(dir, fileName)
	if _, err := os.Stat(path); err != nil {
		return nil, nil
	}

	var id ProjectIdentity
	if _, err := toml.DecodeFile(path, &id); err != nil {
		return nil, err
	}

	if id.Project.Name == "" {
		return nil, nil
	}

	return &id, nil
}
