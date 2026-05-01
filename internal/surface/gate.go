// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package surface

import (
	"fmt"
	"os"
)

// gateStderr is the destination for gate diagnostic output. Tests override it
// to capture stderr; production uses os.Stderr.
var gateStderr = os.Stderr

// EnforceFailStop checks compatibility and returns the error directly,
// unless VV_SURFACE_GATE=warn is set (in which case it logs a single line
// to stderr and returns nil). Used by CLI write entry points and the MCP
// server startup gate.
//
// On vault-unreachable (CheckCompatible returns nil for empty/missing
// paths), this is a no-op.
func EnforceFailStop(vaultPath string) error {
	err := CheckCompatible(vaultPath)
	if err == nil {
		return nil
	}
	if os.Getenv("VV_SURFACE_GATE") == "warn" {
		fmt.Fprintln(gateStderr, err.Error())
		return nil
	}
	return err
}

// EnforceWarnOnly checks compatibility and emits a single stderr warning
// on mismatch (regardless of VV_SURFACE_GATE). Always returns nil. Used by
// the `vv hook` subprocess and read-only CLI commands so an out-of-date
// binary can never block a capture or a read.
//
// VV_SURFACE_QUIET=1 suppresses the stderr line for non-interactive callers
// that just want the work to proceed.
func EnforceWarnOnly(vaultPath string) {
	err := CheckCompatible(vaultPath)
	if err == nil {
		return
	}
	if os.Getenv("VV_SURFACE_QUIET") == "1" {
		return
	}
	fmt.Fprintln(gateStderr, err.Error())
}
