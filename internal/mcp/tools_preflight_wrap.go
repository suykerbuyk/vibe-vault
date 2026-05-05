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
func runPreflight(cfg config.Config, cwd string) (PreflightWrapResult, error) {
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
	// already degrades to false on missing/non-git vault paths.
	vaultDirty, err := vaultHasUncommittedWrites(cfg.VaultPath)
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
func NewPreflightWrapTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_preflight_wrap",
			Description: "Run /wrap's readiness probe: " +
				"surface compatibility (error if binary < vault stamp), " +
				"vault dirty (warning), project dirty (warning). " +
				"Returns {ok, warnings[], errors[]}. ok=false halts the wrap " +
				"so the operator can resolve the incompat.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {}
			}`),
		},
		Handler: func(_ json.RawMessage) (string, error) {
			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}
			res, err := runPreflight(cfg, cwd)
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
