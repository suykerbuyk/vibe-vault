// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	vvcontext "github.com/suykerbuyk/vibe-vault/internal/context"
	"github.com/suykerbuyk/vibe-vault/internal/plugin"
)

// Status represents the outcome of a single check.
type Status int

const (
	Pass Status = iota
	Warn
	Fail
)

func (s Status) String() string {
	switch s {
	case Pass:
		return "pass"
	case Warn:
		return "warn"
	case Fail:
		return "FAIL"
	default:
		return "unknown"
	}
}

// Result holds the outcome of a single check.
type Result struct {
	Name   string
	Status Status
	Detail string
}

// Report aggregates all check results.
type Report struct {
	Results []Result
}

// HasFailures returns true if any result has Fail status.
func (r Report) HasFailures() bool {
	for _, res := range r.Results {
		if res.Status == Fail {
			return true
		}
	}
	return false
}

// Format returns the human-readable report string.
func (r Report) Format() string {
	if len(r.Results) == 0 {
		return "vv check\n\n  no checks ran\n"
	}

	// Find max name length for alignment.
	maxName := 0
	for _, res := range r.Results {
		if len(res.Name) > maxName {
			maxName = len(res.Name)
		}
	}

	var b strings.Builder
	b.WriteString("vv check\n\n")

	var passed, warnings, failures int
	for _, res := range r.Results {
		switch res.Status {
		case Pass:
			passed++
		case Warn:
			warnings++
		case Fail:
			failures++
		}
		fmt.Fprintf(&b, "  %-4s  %-*s  %s\n", res.Status, maxName, res.Name, res.Detail)
	}

	fmt.Fprintf(&b, "\n%d passed, %d warning, %d failure\n", passed, warnings, failures)
	return b.String()
}

// CheckConfig reports the resolved config path. Always passes — broken TOML
// is caught by mustLoadConfig before we get here.
func CheckConfig() Result {
	cfgPath := filepath.Join(config.ConfigDir(), "config.toml")
	return Result{
		Name:   "config",
		Status: Pass,
		Detail: config.CompressHome(cfgPath),
	}
}

// CheckVaultPath checks whether the vault directory exists.
func CheckVaultPath(path string) Result {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return Result{Name: "vault", Status: Pass, Detail: config.CompressHome(path)}
	}
	return Result{Name: "vault", Status: Fail, Detail: path + " not found"}
}

// CheckObsidian checks whether .obsidian/ exists inside the vault.
func CheckObsidian(path string) Result {
	obsDir := filepath.Join(path, ".obsidian")
	if info, err := os.Stat(obsDir); err == nil && info.IsDir() {
		return Result{Name: "obsidian", Status: Pass, Detail: ".obsidian/ found"}
	}
	return Result{Name: "obsidian", Status: Warn, Detail: ".obsidian/ not found (not yet opened in Obsidian)"}
}

// CheckProjects checks whether the Projects directory exists and reports note count.
func CheckProjects(projDir string) Result {
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return Result{Name: "projects", Status: Warn, Detail: "Projects/ not found (fresh vault)"}
	}
	// Count .md files recursively.
	count := countMD(projDir)
	_ = entries
	return Result{Name: "projects", Status: Pass, Detail: fmt.Sprintf("Projects/ (%d notes)", count)}
}

func countMD(dir string) int {
	count := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".md") {
			count++
		}
		return nil
	})
	return count
}

// CheckStateDir checks whether the .vibe-vault state directory exists.
func CheckStateDir(stateDir string) Result {
	if info, err := os.Stat(stateDir); err == nil && info.IsDir() {
		return Result{Name: "state", Status: Pass, Detail: ".vibe-vault/ found"}
	}
	return Result{Name: "state", Status: Warn, Detail: ".vibe-vault/ not found (fresh vault)"}
}

// CheckIndex validates the session-index.json file.
func CheckIndex(stateDir string) Result {
	path := filepath.Join(stateDir, "session-index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Name: "index", Status: Warn, Detail: "session-index.json not found yet"}
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Result{Name: "index", Status: Fail, Detail: "session-index.json invalid JSON"}
	}

	return Result{Name: "index", Status: Pass, Detail: fmt.Sprintf("session-index.json (%d entries)", len(parsed))}
}

// CheckDomains checks that each configured domain path exists.
// Empty paths are skipped (no result emitted).
func CheckDomains(domains config.DomainsConfig) []Result {
	pairs := []struct {
		name string
		path string
	}{
		{"domain:work", domains.Work},
		{"domain:personal", domains.Personal},
		{"domain:opensource", domains.Opensource},
	}

	var results []Result
	for _, p := range pairs {
		if p.path == "" {
			continue
		}
		if info, err := os.Stat(p.path); err == nil && info.IsDir() {
			results = append(results, Result{Name: p.name, Status: Pass, Detail: config.CompressHome(p.path)})
		} else {
			results = append(results, Result{Name: p.name, Status: Warn, Detail: p.path + " not found"})
		}
	}
	return results
}

// CheckEnrichment checks enrichment configuration with provider-aware messaging.
func CheckEnrichment(ecfg config.EnrichmentConfig) Result {
	if !ecfg.Enabled {
		return Result{
			Name:   "enrichment",
			Status: Warn,
			Detail: "disabled (enable for AI summaries, decisions, knowledge capture)",
		}
	}

	provider := ecfg.Provider
	if provider == "" {
		provider = "openai"
	}

	keyEnv := ecfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = config.DefaultAPIKeyEnv(provider)
	}

	if os.Getenv(keyEnv) != "" {
		return Result{
			Name:   "enrichment",
			Status: Pass,
			Detail: fmt.Sprintf("%s/%s (API key set)", provider, ecfg.Model),
		}
	}
	return Result{
		Name:   "enrichment",
		Status: Fail,
		Detail: fmt.Sprintf("enabled but %s not set", keyEnv),
	}
}

// CheckSynthesis checks whether the synthesis agent is configured and has
// access to an LLM provider (which comes from the enrichment config).
func CheckSynthesis(scfg config.SynthesisConfig, ecfg config.EnrichmentConfig) Result {
	if !scfg.Enabled {
		return Result{
			Name:   "synthesis",
			Status: Warn,
			Detail: "disabled (enable for end-of-session knowledge propagation)",
		}
	}

	if !ecfg.Enabled {
		return Result{
			Name:   "synthesis",
			Status: Warn,
			Detail: "enabled but no LLM provider — configure [enrichment] with API key",
		}
	}

	keyEnv := ecfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = config.DefaultAPIKeyEnv(ecfg.Provider)
	}
	if keyEnv == "" || os.Getenv(keyEnv) == "" {
		return Result{
			Name:   "synthesis",
			Status: Warn,
			Detail: "enabled but no LLM provider — configure [enrichment] with API key",
		}
	}

	provider := ecfg.Provider
	if provider == "" {
		provider = "openai"
	}
	return Result{
		Name:   "synthesis",
		Status: Pass,
		Detail: fmt.Sprintf("enabled (%s/%s)", provider, ecfg.Model),
	}
}

// CheckHook checks whether "vv hook" is configured in ~/.claude/settings.json.
func CheckHook() Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{Name: "hook", Status: Warn, Detail: "cannot determine home directory"}
	}
	path := filepath.Join(home, ".claude", "settings.json")
	return checkHookFile(path)
}

func checkHookFile(path string) Result {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Name: "hook", Status: Warn, Detail: config.CompressHome(path) + " not found"}
	}
	if strings.Contains(string(data), "vv hook") {
		return Result{Name: "hook", Status: Pass, Detail: "vv hook found in " + config.CompressHome(path)}
	}
	return Result{Name: "hook", Status: Fail, Detail: "vv hook not found in " + config.CompressHome(path)}
}

// CheckMCP checks whether the vibe-vault MCP server is configured in ~/.claude/settings.json.
func CheckMCP() Result {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{Name: "mcp", Status: Warn, Detail: "cannot determine home directory"}
	}
	path := filepath.Join(home, ".claude", "settings.json")
	return checkMCPFile(path)
}

func checkMCPFile(path string) Result {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{Name: "mcp", Status: Warn, Detail: config.CompressHome(path) + " not found"}
	}

	content := string(data)
	hasMcpServers := strings.Contains(content, `"vibe-vault"`) && strings.Contains(content, "mcpServers")
	hasPlugin := strings.Contains(content, plugin.MarketplaceName) && strings.Contains(content, "extraKnownMarketplaces")

	switch {
	case hasPlugin:
		// Verify plugin files actually exist on disk.
		filesOK := plugin.IsInstalled()
		cacheOK := plugin.AnyCacheInstalled()

		switch {
		case filesOK && cacheOK && hasMcpServers:
			return Result{Name: "mcp", Status: Pass, Detail: "vibe-vault MCP via plugin (legacy mcpServers also present)"}
		case filesOK && cacheOK:
			return Result{Name: "mcp", Status: Pass, Detail: "vibe-vault MCP via plugin"}
		case filesOK:
			return Result{Name: "mcp", Status: Pass, Detail: "vibe-vault MCP via plugin (cache missing — re-run `vv mcp install --claude-plugin`)"}
		default:
			return Result{Name: "mcp", Status: Warn, Detail: "plugin configured but files missing — re-run `vv mcp install --claude-plugin`"}
		}
	case hasMcpServers:
		return Result{Name: "mcp", Status: Warn, Detail: "mcpServers configured but tools may not register — try `vv mcp install --claude-plugin`"}
	default:
		return Result{Name: "mcp", Status: Warn, Detail: "not configured — run `vv mcp install`"}
	}
}

// CheckAgentctxSchema checks the agentctx schema version for a project.
// Returns nil if no agentctx directory exists.
func CheckAgentctxSchema(vaultPath, project string, latestVersion int) *Result {
	agentctxDir := filepath.Join(vaultPath, "Projects", project, "agentctx")
	if _, err := os.Stat(agentctxDir); os.IsNotExist(err) {
		return nil
	}

	versionPath := filepath.Join(agentctxDir, ".version")
	data, err := os.ReadFile(versionPath)
	if err != nil {
		// No .version file means schema v0
		return &Result{
			Name:   "agentctx",
			Status: Warn,
			Detail: fmt.Sprintf("%s: schema v0 (latest: v%d) — run `vv context sync`", project, latestVersion),
		}
	}

	// Parse just the schema_version field
	version := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "schema_version") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				_, _ = fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &version)
			}
		}
	}

	if version >= latestVersion {
		return &Result{
			Name:   "agentctx",
			Status: Pass,
			Detail: fmt.Sprintf("%s: schema v%d (current)", project, version),
		}
	}

	return &Result{
		Name:   "agentctx",
		Status: Warn,
		Detail: fmt.Sprintf("%s: schema v%d (latest: v%d) — run `vv context sync`", project, version, latestVersion),
	}
}

// CheckMemoryLink reports whether a project's host-local Claude Code
// memory directory is correctly symlinked into the vault. Projects
// without host-local state at all are quietly skipped (pass: nothing
// to do). Returns nil when the project lacks either a detectable
// Claude slug directory or vault-side agentctx/memory target — this
// is an advisory check, not a scope gate.
func CheckMemoryLink(vaultPath, project, cwd string) *Result {
	if vaultPath == "" || project == "" || project == "_unknown" {
		return nil
	}
	agentctxDir := filepath.Join(vaultPath, "Projects", project, "agentctx")
	if _, err := os.Stat(agentctxDir); os.IsNotExist(err) {
		return nil
	}
	target := filepath.Join(agentctxDir, "memory")

	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil
	}
	if evald, symErr := filepath.EvalSymlinks(abs); symErr == nil {
		abs = evald
	}
	slug := strings.ReplaceAll(filepath.Clean(abs), "/", "-")

	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	source := filepath.Join(home, ".claude", "projects", slug, "memory")

	info, err := os.Lstat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return &Result{
				Name:   "memory-link",
				Status: Warn,
				Detail: fmt.Sprintf("%s: not linked (run `vv memory link`)", project),
			}
		}
		return nil
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return &Result{
			Name:   "memory-link",
			Status: Warn,
			Detail: fmt.Sprintf("%s: host-local memory is a real directory (run `vv memory link`)", project),
		}
	}
	resolved, err := os.Readlink(source)
	if err != nil {
		return &Result{
			Name:   "memory-link",
			Status: Warn,
			Detail: fmt.Sprintf("%s: cannot read symlink: %v", project, err),
		}
	}
	if filepath.Clean(resolved) != filepath.Clean(target) {
		return &Result{
			Name:   "memory-link",
			Status: Warn,
			Detail: fmt.Sprintf("%s: symlink points to %s, expected %s", project, resolved, target),
		}
	}
	return &Result{
		Name:   "memory-link",
		Status: Pass,
		Detail: fmt.Sprintf("%s: linked → %s", project, config.CompressHome(target)),
	}
}

// CheckCurrentStateInvariants validates a v10 project's resume.md Current
// State section against the v10 invariant-bullet contract. Returns nil for
// pre-v10 projects, missing agentctx dirs, or missing resume.md — the
// contract doesn't apply yet. On v10 projects: Pass if every bullet
// satisfies IsInvariantBullet; Warn if the Current State heading is missing
// or any bullet fails classification.
func CheckCurrentStateInvariants(vaultPath, project string) *Result {
	agentctxDir := filepath.Join(vaultPath, "Projects", project, "agentctx")
	if _, err := os.Stat(agentctxDir); os.IsNotExist(err) {
		return nil
	}
	vf, err := vvcontext.ReadVersion(agentctxDir)
	if err != nil || vf.SchemaVersion < 10 {
		return nil
	}
	resumePath := filepath.Join(agentctxDir, "resume.md")
	data, err := os.ReadFile(resumePath)
	if err != nil {
		return nil
	}
	body, found := extractCurrentStateBody(string(data))
	if !found {
		return &Result{
			Name:   "resume-invariants",
			Status: Warn,
			Detail: fmt.Sprintf("%s: `## %s` section missing", project, vvcontext.CurrentStateSection),
		}
	}
	if bad, ok := vvcontext.ValidateCurrentStateBody(body); !ok {
		return &Result{
			Name:   "resume-invariants",
			Status: Warn,
			Detail: fmt.Sprintf("%s: non-invariant bullet: %s", project, truncateLine(bad, 120)),
		}
	}
	return &Result{
		Name:   "resume-invariants",
		Status: Pass,
		Detail: fmt.Sprintf("%s: Current State invariants clean", project),
	}
}

// extractCurrentStateBody returns the body of the `## Current State` section
// — lines between that heading and the next `## ` heading (exclusive on both
// ends). Returns ("", false) if the heading is missing.
func extractCurrentStateBody(doc string) (string, bool) {
	heading := "## " + vvcontext.CurrentStateSection
	lines := strings.Split(doc, "\n")
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == heading {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), true
}

// truncateLine shortens a line to at most n runes, appending an ellipsis
// when the line is longer. Rune-safe so multi-byte characters are not split.
func truncateLine(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// CheckStaleSymlinks checks for leftover symlinks from pre-v5 schema.
// Returns nil if no issues found.
func CheckStaleSymlinks(repoPath string, schemaVersion int) []Result {
	if schemaVersion < 5 {
		return nil
	}

	var results []Result

	// Check for stale agentctx symlink
	agentctxPath := filepath.Join(repoPath, "agentctx")
	if info, err := os.Lstat(agentctxPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		results = append(results, Result{
			Name:   "stale-symlink",
			Status: Warn,
			Detail: "agentctx symlink exists but schema >= v5 — run `vv context sync` to remove",
		})
	}

	// Check for stale .claude/ subdirectory symlinks
	for _, sub := range []string{"commands", "rules", "skills", "agents"} {
		subPath := filepath.Join(repoPath, ".claude", sub)
		if info, err := os.Lstat(subPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			results = append(results, Result{
				Name:   "stale-symlink",
				Status: Warn,
				Detail: fmt.Sprintf(".claude/%s is a symlink but schema >= v5 — run `vv context sync`", sub),
			})
		}
	}

	// Verify CLAUDE.md is a regular file
	claudePath := filepath.Join(repoPath, "CLAUDE.md")
	if info, err := os.Lstat(claudePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		results = append(results, Result{
			Name:   "stale-symlink",
			Status: Warn,
			Detail: "CLAUDE.md is a symlink but schema >= v5 — run `vv context sync`",
		})
	}

	return results
}

// Run executes all checks against the given config and returns a report.
func Run(cfg config.Config) Report {
	var results []Result

	results = append(results, CheckConfig())
	results = append(results, CheckVaultPath(cfg.VaultPath))
	results = append(results, CheckObsidian(cfg.VaultPath))
	results = append(results, CheckProjects(cfg.ProjectsDir()))
	results = append(results, CheckStateDir(cfg.StateDir()))
	results = append(results, CheckIndex(cfg.StateDir()))
	results = append(results, CheckDomains(cfg.Domains)...)
	results = append(results, CheckEnrichment(cfg.Enrichment))
	results = append(results, CheckSynthesis(cfg.Synthesis, cfg.Enrichment))
	results = append(results, CheckHook())
	results = append(results, CheckMCP())

	return Report{Results: results}
}
