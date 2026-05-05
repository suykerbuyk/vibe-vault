// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionsource

import (
	"context"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/session"
)

// Sink is the convergence layer every SessionSource calls to persist
// a captured session. The shape — (ctx, CaptureOpts, cfg) — was
// reconciled in v5 of the plan after earlier drafts proposed a Sink
// taking *transcript.Transcript directly. That signature compiled
// against neither existing path: hook calls session.Capture (path-
// based, internal parse); zed calls session.CaptureFromParsed (with
// pre-rendered narrative + dialogue not in any single signature).
//
// The CaptureOpts shape now carries four optional pre-parsed fields
// (Transcript, Info, Narrative, Dialogue). Sources that have already
// rendered their own transcript set all four; sources that pass a
// raw transcript path leave them nil. session.Capture inspects the
// four-field set and routes accordingly — pre-parsed fast-path or
// JSONL parse-and-build.
type Sink interface {
	Capture(ctx context.Context, opts session.CaptureOpts, cfg config.Config) (*session.CaptureResult, error)
}

// CaptureSink is the production Sink. It thin-wraps session.Capture
// and is shared across all SessionSource implementations. The ctx
// parameter is currently unused by session.Capture (which runs
// synchronously to completion) but is part of the signature so
// future cancellation support is additive rather than breaking.
type CaptureSink struct{}

// Capture implements Sink by calling session.Capture, which itself
// routes to CaptureFromParsed when CaptureOpts has the four
// pre-parsed fields populated.
func (CaptureSink) Capture(_ context.Context, opts session.CaptureOpts, cfg config.Config) (*session.CaptureResult, error) {
	return session.Capture(opts, cfg)
}

// Compile-time interface satisfaction.
var _ Sink = CaptureSink{}
