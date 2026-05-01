// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"errors"
	"fmt"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/surface"
)

// CheckSurface verifies the binary's MCPSurfaceVersion is >= the maximum
// .surface stamp recorded in the vault. Returns Pass when the binary is at
// or above the vault max, Fail when behind, Warn when no vault is configured.
//
// Vault-unreachable cases (missing vaultPath dir) are treated by
// surface.CheckCompatible as best-effort and return nil error — that path
// surfaces as Pass here, mirroring the runtime gate's behavior.
func CheckSurface(cfg config.Config) Result {
	if cfg.VaultPath == "" {
		return Result{Name: "surface", Status: Warn, Detail: "no vault configured"}
	}
	err := surface.CheckCompatible(cfg.VaultPath)
	if err == nil {
		return Result{
			Name:   "surface",
			Status: Pass,
			Detail: fmt.Sprintf("binary v%d >= vault max", surface.MCPSurfaceVersion),
		}
	}
	var ie *surface.IncompatibleError
	if errors.As(err, &ie) {
		return Result{
			Name:   "surface",
			Status: Fail,
			Detail: fmt.Sprintf("binary v%d < vault v%d at %s",
				ie.BinarySurface, ie.VaultSurface, ie.StampDir),
		}
	}
	return Result{Name: "surface", Status: Warn, Detail: err.Error()}
}
