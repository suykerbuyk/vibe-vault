// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package templates

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/context"
	"github.com/suykerbuyk/vibe-vault/internal/scaffold"
)

// Status describes the state of a vault template relative to its default.
type Status string

const (
	StatusDefault    Status = "default"
	StatusCustomized Status = "customized"
	StatusMissing    Status = "missing"
)

// Entry describes a single template in the registry.
type Entry struct {
	RelPath string // relative to Templates/, e.g. "session-summary.md" or "agentctx/workflow.md"
	content []byte
}

// FileStatus pairs an entry with its vault status.
type FileStatus struct {
	Entry  Entry
	Status Status
}

// Registry holds the complete set of built-in templates.
type Registry struct {
	entries []Entry
	byPath  map[string]Entry
}

// New builds a Registry from both template sources.
func New() *Registry {
	r := &Registry{byPath: make(map[string]Entry)}

	// Scaffold templates: discover top-level Templates/ files (skip agentctx/ subtree)
	scaffoldFS := scaffold.EmbeddedFS()
	_ = fs.WalkDir(scaffoldFS, "templates/Templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("templates/Templates", path)
		if strings.HasPrefix(rel, "agentctx") {
			return nil
		}
		data, err := scaffoldFS.ReadFile(path)
		if err != nil {
			return nil
		}
		e := Entry{RelPath: rel, content: data}
		r.entries = append(r.entries, e)
		r.byPath[rel] = e
		return nil
	})

	// Context generator templates: agentctx/ files
	for rel, content := range context.BuiltinTemplates() {
		relPath := filepath.Join("agentctx", rel)
		e := Entry{RelPath: relPath, content: []byte(content)}
		r.entries = append(r.entries, e)
		r.byPath[relPath] = e
	}

	sort.Slice(r.entries, func(i, j int) bool {
		return r.entries[i].RelPath < r.entries[j].RelPath
	})

	return r
}

// Has reports whether a template with the given relative path exists.
func (r *Registry) Has(relPath string) bool {
	_, ok := r.byPath[relPath]
	return ok
}

// List returns all template entries in sorted order.
func (r *Registry) List() []Entry {
	return r.entries
}

// DefaultContent returns a copy of the built-in content for a template.
func (r *Registry) DefaultContent(relPath string) ([]byte, bool) {
	e, ok := r.byPath[relPath]
	if !ok {
		return nil, false
	}
	out := make([]byte, len(e.content))
	copy(out, e.content)
	return out, true
}

// Compare checks vault templates against defaults and returns their status.
func (r *Registry) Compare(vaultTemplatesDir string) []FileStatus {
	result := make([]FileStatus, len(r.entries))
	for i, e := range r.entries {
		vaultPath := filepath.Join(vaultTemplatesDir, e.RelPath)
		data, err := os.ReadFile(vaultPath)
		if err != nil {
			result[i] = FileStatus{Entry: e, Status: StatusMissing}
			continue
		}
		if bytes.Equal(bytes.TrimSpace(data), bytes.TrimSpace(e.content)) {
			result[i] = FileStatus{Entry: e, Status: StatusDefault}
		} else {
			result[i] = FileStatus{Entry: e, Status: StatusCustomized}
		}
	}
	return result
}
