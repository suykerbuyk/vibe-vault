// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/suykerbuyk/vibe-vault/internal/help"
)

// LatestSchemaVersion is the current agentctx schema version.
const LatestSchemaVersion = 10

// VersionFile represents the .version TOML file in an agentctx directory.
type VersionFile struct {
	SchemaVersion int    `toml:"schema_version"`
	CreatedBy     string `toml:"created_by"`
	CreatedAt     string `toml:"created_at"`
	UpdatedBy     string `toml:"updated_by"`
	UpdatedAt     string `toml:"updated_at"`
}

// ReadVersion reads the .version file from an agentctx directory.
// Returns a zero-version VersionFile (schema_version=0) if the file is missing.
func ReadVersion(agentctxPath string) (VersionFile, error) {
	path := filepath.Join(agentctxPath, ".version")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return VersionFile{SchemaVersion: 0}, nil
		}
		return VersionFile{}, fmt.Errorf("read .version: %w", err)
	}

	var vf VersionFile
	if err := toml.Unmarshal(data, &vf); err != nil {
		return VersionFile{}, fmt.Errorf("parse .version: %w", err)
	}
	return vf, nil
}

// WriteVersion writes the .version TOML file to an agentctx directory.
func WriteVersion(agentctxPath string, vf VersionFile) error {
	path := filepath.Join(agentctxPath, ".version")
	if err := os.MkdirAll(agentctxPath, 0o755); err != nil {
		return fmt.Errorf("create agentctx dir: %w", err)
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(vf); err != nil {
		return fmt.Errorf("encode .version: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write .version: %w", err)
	}
	return nil
}

// Migration describes a schema migration from one version to the next.
type Migration struct {
	From, To int
	Apply    func(MigrationContext) ([]FileAction, error)
}

// MigrationContext provides context for a migration function.
type MigrationContext struct {
	AgentctxPath string // vault-side agentctx directory
	RepoPath     string // repo root (empty in --all mode)
	Project      string
	VaultPath    string
	Force        bool
	DryRun       bool // true when the migration should not modify disk state.
}

// migrations is the ordered list of all schema migrations.
var migrations = []Migration{
	{From: 0, To: 1, Apply: migrate0to1},
	{From: 1, To: 2, Apply: migrate1to2},
	{From: 2, To: 3, Apply: migrate2to3},
	{From: 3, To: 4, Apply: migrate3to4},
	{From: 4, To: 5, Apply: migrate4to5},
	{From: 5, To: 6, Apply: migrate5to6},
	{From: 6, To: 7, Apply: migrate6to7},
	{From: 7, To: 8, Apply: migrate7to8},
	{From: 9, To: 10, Apply: migrate9to10},
}

// migrationsFrom returns all migrations applicable from the given version.
func migrationsFrom(current int) []Migration {
	var result []Migration
	for _, m := range migrations {
		if m.From >= current {
			result = append(result, m)
		}
	}
	return result
}

// migrate0to1 writes the initial .version file (schema v1).
func migrate0to1(ctx MigrationContext) ([]FileAction, error) {
	vf := newVersionFile(1)
	if err := WriteVersion(ctx.AgentctxPath, vf); err != nil {
		return nil, err
	}
	return []FileAction{{Path: ".version", Action: "CREATE", Location: "vault"}}, nil
}

// migrate1to2 is implemented in sync.go (adds agentctx symlink, relative paths).
// Placeholder here for the migration registry — the real implementation is in sync.go.

// migrate9to10 is a no-op marker: its presence in the registry causes the
// migration framework to bump .version from 8 (or 9) to 10 for any vault
// not yet at LatestSchemaVersion. The retired migrate8to9 mechanism (see
// DESIGN.md #50) has no successor; v9 is no longer a meaningful resting
// state, so this single entry brings post-v7 vaults straight to v10.
func migrate9to10(_ MigrationContext) ([]FileAction, error) {
	return nil, nil
}

func vvVersion() string {
	return "vv " + help.Version
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// newVersionFile creates a VersionFile stamped with current time and version.
func newVersionFile(schema int) VersionFile {
	now := nowISO()
	ver := vvVersion()
	return VersionFile{
		SchemaVersion: schema,
		CreatedBy:     ver,
		CreatedAt:     now,
		UpdatedBy:     ver,
		UpdatedAt:     now,
	}
}
