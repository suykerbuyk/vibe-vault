// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

// vv_preflight_wrap is the lightweight readiness probe /wrap calls
// before composing the iter narrative. Three checks per the M2-v6
// policy table: surface-compat (gating), vault-dirty (advisory),
// project-dirty (advisory). The render path retired in iter 216, so no
// `tier` parameter and no `[wrap.tiers]` lookup.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/surface"
)

// PreflightCheckItem is one entry in the warnings or errors list of a
// PreflightWrapResult. `check` is a stable identifier (surface,
// vault_dirty, project_dirty); `detail` is the human-readable message.
type PreflightCheckItem struct {
	Check  string `json:"check"`
	Detail string `json:"detail"`
}

// PreflightWrapResult is the JSON shape returned by vv_preflight_wrap.
// `ok` mirrors len(errors) == 0; warnings never flip ok off.
type PreflightWrapResult struct {
	OK       bool                 `json:"ok"`
	Warnings []PreflightCheckItem `json:"warnings"`
	Errors   []PreflightCheckItem `json:"errors"`
}

// surfaceCheckCompatible is the seam wrapping surface.CheckCompatible so
// tests can inject incompat conditions without staging an entire vault
// stamp tree.
var surfaceCheckCompatible = surface.CheckCompatible

// runPreflight assembles the result. Pure orchestration, no I/O beyond
// the three helper invocations — all I/O is delegated.
//
// project, when non-empty, scopes the vault-dirty probe to
// `Projects/<project>/` so a sibling project's uncommitted writes do
// not falsely trip the vault_dirty warning. Empty project degrades to
// whole-vault behavior (matches the historical preflight semantics).
func runPreflight(cfg config.Config, cwd, project string) (PreflightWrapResult, error) {
	res := PreflightWrapResult{
		Warnings: []PreflightCheckItem{},
		Errors:   []PreflightCheckItem{},
	}

	// 1. Surface check. Gate on surface.IncompatibleError; degrade
	// best-effort on other errors (vault-unreachable, etc.) — they
	// already returned nil from CheckCompatible per its contract.
	if err := surfaceCheckCompatible(cfg.VaultPath); err != nil {
		var ie *surface.IncompatibleError
		if errors.As(err, &ie) {
			res.Errors = append(res.Errors, PreflightCheckItem{
				Check: "surface",
				Detail: fmt.Sprintf("binary v%d < vault v%d at %s",
					ie.BinarySurface, ie.VaultSurface, ie.StampDir),
			})
		} else {
			res.Errors = append(res.Errors, PreflightCheckItem{
				Check:  "surface",
				Detail: err.Error(),
			})
		}
	}

	// 2. Vault dirty (warning, never error). vaultHasUncommittedWrites
	// already degrades to false on missing/non-git vault paths. The
	// probe is scoped to Projects/<project>/ when project is non-empty
	// so unrelated projects' dirt does not trigger the warning here.
	vaultDirty, err := vaultHasUncommittedWrites(cfg.VaultPath, project)
	if err != nil {
		res.Warnings = append(res.Warnings, PreflightCheckItem{
			Check:  "vault_dirty",
			Detail: fmt.Sprintf("vault git status probe failed: %v", err),
		})
	} else if vaultDirty {
		res.Warnings = append(res.Warnings, PreflightCheckItem{
			Check:  "vault_dirty",
			Detail: "vault has uncommitted writes — review before committing the wrap iter",
		})
	}

	// 3. Project dirty (warning, never error).
	projectDirty, err := projectHasUncommittedWrites(cwd)
	if err != nil {
		res.Warnings = append(res.Warnings, PreflightCheckItem{
			Check:  "project_dirty",
			Detail: fmt.Sprintf("project git status probe failed: %v", err),
		})
	} else if projectDirty {
		res.Warnings = append(res.Warnings, PreflightCheckItem{
			Check:  "project_dirty",
			Detail: "project has uncommitted writes — wrap will likely include them in the next commit",
		})
	}

	res.OK = len(res.Errors) == 0
	return res, nil
}

// NewPreflightWrapTool creates the vv_preflight_wrap MCP tool.
//
// Returns {ok, warnings[], errors[]}; the orchestrator uses ok as a
// gate (false = halt, surface needs upgrade), and prepends each
// warning to the iter narrative as advisory notes.
//
// The optional project argument scopes the vault_dirty check to
// Projects/<project>/ so a sibling project's uncommitted writes (e.g.
// collateral from `vv context sync` regenerating other projects'
// agentctx/.surface) do not falsely warn here. When omitted, the
// project is detected from the working directory; if detection fails
// the probe degrades to whole-vault behavior (legacy semantics).
func NewPreflightWrapTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_preflight_wrap",
			Description: "Run /wrap's readiness probe: " +
				"surface compatibility (error if binary < vault stamp), " +
				"vault dirty (warning, scoped to Projects/<project>/), project dirty (warning). " +
				"Returns {ok, warnings[], errors[]}. ok=false halts the wrap " +
				"so the operator can resolve the incompat.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory; if detection fails the vault_dirty probe degrades to whole-vault scope."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}

			// Best-effort project resolution. A failure here must not
			// fail the preflight — degrade to empty project (whole-vault
			// scope, legacy behavior). The wrap orchestrator will pass
			// the project explicitly in the common path.
			project, perr := resolveProject(args.Project)
			if perr != nil {
				project = ""
			}

			res, err := runPreflight(cfg, cwd, project)
			if err != nil {
				return "", err
			}
			out, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
