// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/johns/vibe-vault/templates"
)

// TemplateVars holds substitution variables for vault templates.
type TemplateVars struct {
	Project string
	Date    string
}

// DefaultVars returns TemplateVars with the current date.
func DefaultVars(project string) TemplateVars {
	return TemplateVars{
		Project: project,
		Date:    time.Now().Format("2006-01-02"),
	}
}

// readEmbedded reads a file from the embedded agentctx FS.
// relPath is relative to agentctx/, e.g. "workflow.md" or "commands/restart.md".
func readEmbedded(relPath string) string {
	data, err := fs.ReadFile(templates.AgentctxFS(), filepath.Join("agentctx", relPath))
	if err != nil {
		return ""
	}
	return string(data)
}

// resolveTemplate checks vault Templates/agentctx/{relPath} first.
// If found, applies variable substitution and returns it.
// Otherwise, reads from the embedded template files.
func resolveTemplate(vaultPath, relPath string, vars TemplateVars) string {
	tmplPath := filepath.Join(vaultPath, "Templates", "agentctx", relPath)
	data, err := os.ReadFile(tmplPath)
	if err != nil {
		return applyVars(readEmbedded(relPath), vars)
	}
	return applyVars(string(data), vars)
}

// applyVars performs {{PROJECT}} and {{DATE}} substitution on content.
func applyVars(content string, vars TemplateVars) string {
	content = strings.ReplaceAll(content, "{{PROJECT}}", vars.Project)
	content = strings.ReplaceAll(content, "{{DATE}}", vars.Date)
	return content
}

// EnsureVaultTemplates seeds Templates/agentctx/ with embedded defaults.
// Uses safeWrite so existing user files are never overwritten.
func EnsureVaultTemplates(vaultPath string) []FileAction {
	tmplDir := filepath.Join(vaultPath, "Templates", "agentctx")

	var actions []FileAction
	for relPath, content := range BuiltinTemplates() {
		path := filepath.Join(tmplDir, relPath)
		action := safeWrite(path, content, false)
		if action != "SKIP" {
			actions = append(actions, FileAction{
				Path:   filepath.Join("Templates", "agentctx", relPath),
				Action: action,
			})
		}
	}
	return actions
}

// BuiltinTemplates returns the canonical agentctx template contents keyed by
// relative path (e.g. "README.md", "commands/restart.md", "CLAUDE.md").
func BuiltinTemplates() map[string]string {
	result := make(map[string]string)
	fsys := templates.AgentctxFS()
	_ = fs.WalkDir(fsys, "agentctx", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("agentctx", path)
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil
		}
		result[rel] = string(data)
		return nil
	})
	return result
}
