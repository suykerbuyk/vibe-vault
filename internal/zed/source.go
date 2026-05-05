// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/sessionsource"
)

// SourceName is the stable identifier for the Zed agent-panel
// SessionSource as registered in the SessionSource registry. The
// "-acp" suffix distinguishes it from a hypothetical future
// "zed-jsonl" or "zed-direct" source — Zed's Agent Client Protocol
// is the specific surface this source consumes.
const SourceName = "zed-acp"

// SourceCaptureSourceTag is the value plumbed through to
// session.CaptureOpts.Source for entries this source captures. It
// matches the registry name exactly — γ Phase 1 makes the source
// identity uniform top-to-bottom (registry key, CaptureOpts.Source,
// session-note frontmatter `source` field).
const SourceCaptureSourceTag = SourceName

// SourceConfig parameterizes a zed-acp Source. The fields mirror
// runZedWatch's command-line / config inputs so cmd/vv/main.go can
// drive registration with values resolved from flags + config.
type SourceConfig struct {
	// DBPath is the absolute path to Zed's threads.db. If empty,
	// Enabled() returns false (no point running a watcher when no
	// DB exists). Production callers resolve via cfg.Zed.DBPath
	// then DefaultDBPath().
	DBPath string

	// Debounce is the quiet-period window before a fsnotify event
	// burst flushes through to BatchCapture. Zero falls back to
	// the watcher's default (5 minutes).
	Debounce time.Duration

	// ProjectFilter narrows BatchCapture to a single project name.
	// Empty captures all projects. Mirrors runZedWatch's --project
	// flag.
	ProjectFilter string

	// Cfg is the resolved vibe-vault configuration; used by
	// BatchCapture for staging-root resolution and domain mapping.
	Cfg config.Config

	// Logger receives watcher-loop diagnostics. If nil, the
	// process default logger is used. Tests inject a buffered
	// logger to assert on output.
	Logger *log.Logger
}

// Source is the SessionSource implementation backing the Zed
// agent-panel watcher. Start() launches the fsnotify-watch
// goroutine; Stop() cancels its context. BatchCapture remains the
// shared pipeline and is invoked from inside the on-change callback
// — this keeps the source implementation purely about lifecycle
// and delegates the rendering pipeline to existing code.
//
// Asymmetric Start semantics (versus the hook source's no-op
// Start) is the load-bearing reason the SessionSource interface
// exists — see internal/sessionsource/source.go.
type Source struct {
	cfg SourceConfig

	mu     sync.Mutex
	cancel context.CancelFunc
	doneCh chan struct{}
}

// NewSource constructs a Zed agent-panel SessionSource with the
// given config. The constructor performs no I/O and never fails;
// Enabled() and Start() report runtime conditions.
func NewSource(cfg SourceConfig) *Source {
	return &Source{cfg: cfg}
}

// Name returns "zed-acp".
func (*Source) Name() string { return SourceName }

// Enabled reports whether Zed's threads.db exists and the source
// can actually capture. An empty DBPath returns false (caller did
// not configure a target). A non-empty DBPath that does not exist
// also returns false — there is no Zed install on this host or
// it has not been used yet.
func (s *Source) Enabled() bool {
	if s.cfg.DBPath == "" {
		return false
	}
	_, err := os.Stat(s.cfg.DBPath)
	return err == nil
}

// Start launches the fsnotify watcher and blocks for the lifetime
// of ctx (or until Stop is called). On every debounce-firing the
// watcher re-parses recent threads from threads.db and calls
// BatchCapture, which routes through the supplied Sink (today's
// production Sink wraps session.Capture, which itself routes to
// CaptureFromParsed via the pre-parsed fast-path on CaptureOpts).
//
// Concurrent BatchCapture invocations are serialized by an
// internal mutex — fsnotify can deliver overlapping events on
// busy WAL writes, and the index lockfile would serialize them
// downstream anyway, but acquiring the mutex up-front avoids
// duplicate parsing of the same threads.
func (s *Source) Start(ctx context.Context, sink sessionsource.Sink) error {
	logger := s.cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return errAlreadyStarted
	}
	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	doneCh := make(chan struct{})
	s.doneCh = doneCh
	s.mu.Unlock()

	defer close(doneCh)

	// onChange callback serialized by mu so a fast WAL-write storm
	// cannot fan out into overlapping BatchCapture calls.
	var cbMu sync.Mutex
	onChange := func() {
		cbMu.Lock()
		defer cbMu.Unlock()

		window := s.cfg.Debounce + time.Minute
		if s.cfg.Debounce == 0 {
			window = 6 * time.Minute
		}
		since := time.Now().Add(-window)

		threads, err := ParseDB(s.cfg.DBPath, ParseOpts{Since: since})
		if err != nil {
			logger.Printf("zed-acp: error reading threads: %v", err)
			return
		}
		if len(threads) == 0 {
			return
		}

		result := batchCaptureViaSink(childCtx, sink, BatchCaptureOpts{
			Threads:       threads,
			DBPath:        s.cfg.DBPath,
			ProjectFilter: s.cfg.ProjectFilter,
			Cfg:           s.cfg.Cfg,
			Logger:        logger,
		})
		if result.Processed > 0 || result.Errors > 0 {
			logger.Printf("zed-acp [%s] captured: %d, skipped: %d, errors: %d",
				time.Now().Format("15:04:05"), result.Processed, result.Skipped, result.Errors)
		}
	}

	err := Watch(childCtx, WatcherConfig{
		DBPath:   s.cfg.DBPath,
		Debounce: s.cfg.Debounce,
		Logger:   logger,
	}, onChange)
	if err != nil && err != context.Canceled {
		return err
	}
	return nil
}

// Stop cancels the watcher context and waits for the watcher
// goroutine to exit. Stop on a never-Started Source returns nil.
// Stop is safe to call multiple times — the second call observes
// the closed doneCh and returns immediately.
func (s *Source) Stop() error {
	s.mu.Lock()
	cancel := s.cancel
	doneCh := s.doneCh
	s.cancel = nil
	s.doneCh = nil
	s.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	if doneCh != nil {
		<-doneCh
	}
	return nil
}

// errAlreadyStarted is returned by Start when called on an
// already-Started Source. The registry's own double-Start guard
// catches this earlier in production paths; this is a defense for
// direct callers.
var errAlreadyStarted = sourceError("zed-acp: already started")

type sourceError string

func (e sourceError) Error() string { return string(e) }

// Compile-time interface satisfaction.
var _ sessionsource.SessionSource = (*Source)(nil)
