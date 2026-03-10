// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package identity

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const fileName = ".vibe-vault.toml"

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
