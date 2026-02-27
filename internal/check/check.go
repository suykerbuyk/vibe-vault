package check

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/johns/vibe-vault/internal/config"
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

// CheckSessions checks whether the Sessions directory exists and reports note count.
func CheckSessions(sessDir string) Result {
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return Result{Name: "sessions", Status: Warn, Detail: "Sessions/ not found (fresh vault)"}
	}
	// Count .md files recursively.
	count := countMD(sessDir)
	_ = entries
	return Result{Name: "sessions", Status: Pass, Detail: fmt.Sprintf("Sessions/ (%d notes)", count)}
}

func countMD(dir string) int {
	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
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

// CheckEnrichment checks enrichment configuration.
func CheckEnrichment(ecfg config.EnrichmentConfig) Result {
	if !ecfg.Enabled {
		return Result{Name: "enrichment", Status: Pass, Detail: "disabled"}
	}
	keyEnv := ecfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = "XAI_API_KEY"
	}
	if os.Getenv(keyEnv) != "" {
		return Result{Name: "enrichment", Status: Pass, Detail: keyEnv + " set"}
	}
	return Result{Name: "enrichment", Status: Warn, Detail: keyEnv + " not set"}
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

// Run executes all checks against the given config and returns a report.
func Run(cfg config.Config) Report {
	var results []Result

	results = append(results, CheckConfig())
	results = append(results, CheckVaultPath(cfg.VaultPath))
	results = append(results, CheckObsidian(cfg.VaultPath))
	results = append(results, CheckSessions(cfg.SessionsDir()))
	results = append(results, CheckStateDir(cfg.StateDir()))
	results = append(results, CheckIndex(cfg.StateDir()))
	results = append(results, CheckDomains(cfg.Domains)...)
	results = append(results, CheckEnrichment(cfg.Enrichment))
	results = append(results, CheckHook())

	return Report{Results: results}
}
