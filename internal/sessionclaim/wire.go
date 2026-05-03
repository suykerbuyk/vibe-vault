// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionclaim

import "github.com/suykerbuyk/vibe-vault/internal/session"

// init wires sessionclaim.UpdateHarnessSessionID into the
// internal/session package. Phase 4 of
// session-slot-multihost-disambiguation: session.Capture needs to call
// UpdateHarnessSessionID after CWD/projectRoot/sessionID are resolved
// (M8 architectural cleanup), but session can't import sessionclaim
// because sessionclaim imports session for DetectProjectRoot.
//
// The indirection is registered in init() so any binary that imports
// sessionclaim (cmd/vv, the MCP server, the hook handler) automatically
// gets the wiring. Pure-session tests that don't link sessionclaim see
// the no-op default and still pass.
func init() {
	session.SetUpdateHarnessSessionID(UpdateHarnessSessionID)
}
