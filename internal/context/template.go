// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"os"
	"path/filepath"
	"strings"
	"time"
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

// resolveTemplate checks vault Templates/agentctx/{relPath} first.
// If found, applies variable substitution and returns it.
// Otherwise, calls fallback() for the embedded default.
func resolveTemplate(vaultPath, relPath string, vars TemplateVars, fallback func() string) string {
	tmplPath := filepath.Join(vaultPath, "Templates", "agentctx", relPath)
	data, err := os.ReadFile(tmplPath)
	if err != nil {
		return fallback()
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

	templates := []struct {
		rel     string
		content string
	}{
		{"README.md", vaultTemplateReadme},
		{"workflow.md", vaultTemplateWorkflow},
		{"resume.md", vaultTemplateResume},
		{"iterations.md", vaultTemplateIterations},
		{"commands/restart.md", vaultTemplateRestart},
		{"commands/wrap.md", vaultTemplateWrap},
		{"commands/license.md", vaultTemplateLicense},
		{"commands/makefile.md", vaultTemplateMakefile},
	}

	var actions []FileAction
	for _, t := range templates {
		path := filepath.Join(tmplDir, t.rel)
		action := safeWrite(path, t.content, false)
		if action != "SKIP" {
			actions = append(actions, FileAction{
				Path:   filepath.Join("Templates", "agentctx", t.rel),
				Action: action,
			})
		}
	}
	return actions
}

// BuiltinTemplates returns the canonical agentctx template contents keyed by
// relative path (e.g. "README.md", "commands/restart.md").
func BuiltinTemplates() map[string]string {
	return map[string]string{
		"README.md":            vaultTemplateReadme,
		"workflow.md":          vaultTemplateWorkflow,
		"resume.md":            vaultTemplateResume,
		"iterations.md":        vaultTemplateIterations,
		"commands/restart.md":  vaultTemplateRestart,
		"commands/wrap.md":     vaultTemplateWrap,
		"commands/license.md":  vaultTemplateLicense,
		"commands/makefile.md": vaultTemplateMakefile,
	}
}

// --- Vault template defaults ---
// These are seeded into Templates/agentctx/ for user customization.
// They use {{PROJECT}} and {{DATE}} variables.

var vaultTemplateReadme = `# Agentctx Templates

These templates are used by ` + "`vv context init`" + ` to scaffold vault-resident
AI context files for new projects.

## Variables

- ` + "`{{PROJECT}}`" + ` — project name (auto-detected or --project flag)
- ` + "`{{DATE}}`" + ` — current date (YYYY-MM-DD)

## How it works

When you run ` + "`vv context init`" + `, each file here is checked before
falling back to the embedded defaults. To customize a template, edit
the file here. Your changes will be used for all future project inits.

## Shared commands

Files in commands/ are propagated to all projects by ` + "`vv context sync`" + `.
Project-specific commands always take precedence (never overwritten).
`

var vaultTemplateWorkflow = generateWorkflowMD("{{PROJECT}}")

var vaultTemplateResume = generateResume("{{PROJECT}}")

var vaultTemplateIterations = generateIterations("{{PROJECT}}")

var vaultTemplateRestart = generateRestartMD()

var vaultTemplateWrap = generateWrapMD()

var vaultTemplateLicense = generateLicenseMD()

var vaultTemplateMakefile = generateMakefileMD()
