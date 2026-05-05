// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package sessionsource defines the SessionSource interface — the
// pluggable capture-side abstraction for session telemetry.
//
// γ Phase 1 of doc/SESSION-CAPTURE-ARCHITECTURE.md (Decision Phase 2)
// extracts the abstraction from the two existing implementations
// (claude-code-jsonl via SessionEnd / Stop / PreCompact hooks;
// zed-acp via fsnotify watcher on threads.db). The asymmetry between
// the two — event-driven-out-of-process vs watch-driven-in-process —
// is the load-bearing reason for the interface.
//
// The shape is intentionally minimal: Name() identifies the source in
// the registry; Enabled() detects whether the source can run on this
// host; Start() launches any in-process loop (no-op for hook-driven
// sources); Stop() gracefully shuts down. Concrete implementations
// live alongside their existing capture code (internal/hook,
// internal/zed) and register themselves with the in-process registry.
package sessionsource

import "context"

// SessionSource is the capability boundary for one capture
// environment. Each implementation translates a harness-specific
// transcript layout into the canonical Sink call.
type SessionSource interface {
	// Name returns a stable identifier for the source, e.g.
	// "claude-code-jsonl" or "zed-acp". Used as the registry key
	// and as the value plumbed through to CaptureOpts.Source for
	// downstream classification.
	Name() string

	// Enabled reports whether this source can run on this host. For
	// hook-driven sources this is typically true unconditionally
	// (the source is always "available" because Claude Code invokes
	// `vv hook` externally). For watch-driven sources this checks
	// for the harness's transcript artifact, e.g. zed-acp checks
	// for `threads.db`.
	Enabled() bool

	// Start kicks off any in-process capture loop. For hook-driven
	// sources this is a no-op — capture runs out-of-process when
	// the harness invokes `vv hook` directly. For watch-driven
	// sources this launches the fsnotify watcher goroutine; the
	// goroutine's lifecycle is owned by the registry that called
	// Start. Sources that have already been Started must return an
	// error from a subsequent Start call (Registry enforces this
	// contract).
	Start(ctx context.Context, sink Sink) error

	// Stop terminates any in-process capture loop launched by
	// Start. Hook-driven sources implement Stop as a no-op. Stop
	// must be safe to call on a never-Started source (returns nil).
	Stop() error
}
