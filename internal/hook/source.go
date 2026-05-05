// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package hook

import (
	"context"

	"github.com/suykerbuyk/vibe-vault/internal/sessionsource"
)

// SourceName is the stable identifier for the Claude Code hook
// source as registered in the SessionSource registry.
const SourceName = "claude-code-jsonl"

// Source is the SessionSource implementation backing the Claude
// Code SessionEnd / Stop / PreCompact hooks. Capture happens
// out-of-process when Claude Code itself invokes `vv hook` on
// stdin; the in-process Source is therefore mostly a registry
// citizen — it surfaces a Name and Enabled predicate but Start /
// Stop are no-ops because there is no in-process loop to manage.
//
// The asymmetric Start semantics versus zed-acp's watch-driven
// Source is the load-bearing reason the SessionSource interface
// exists. Claude Code is event-driven-out-of-process; Zed is
// watch-driven-in-process. The interface accommodates both.
type Source struct{}

// NewSource constructs the Claude Code hook SessionSource. The
// constructor is parameter-free because the hook source's
// configuration lives in cfg.Config consumed by Handle() at hook-
// invocation time, not at source-registration time.
func NewSource() *Source { return &Source{} }

// Name returns "claude-code-jsonl".
func (Source) Name() string { return SourceName }

// Enabled returns true unconditionally. The hook source is invoked
// externally (Claude Code calls `vv hook`) so the source is always
// "available" in the registry sense — we cannot detect whether
// Claude Code is installed or whether the hook config is wired up,
// and absent that signal, treating the source as enabled is the
// right default. A future predicate could check for presence of
// `~/.config/claude/settings.json` hook entries, but that's
// orthogonal to the core capture path.
func (Source) Enabled() bool { return true }

// Start is a no-op. The hook source has no in-process capture
// loop; capture runs out-of-process on every `vv hook` invocation.
// Returning nil here keeps the registry's lifecycle bookkeeping
// uniform across hook-driven and watch-driven sources.
func (Source) Start(_ context.Context, _ sessionsource.Sink) error { return nil }

// Stop is a no-op for the same reason Start is. Nothing to tear
// down because nothing was started.
func (Source) Stop() error { return nil }

// Compile-time interface satisfaction.
var _ sessionsource.SessionSource = Source{}
