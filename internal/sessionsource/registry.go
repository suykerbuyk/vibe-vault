// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionsource

import (
	"context"
	"fmt"
	"sync"
)

// Registry is the in-process registrar for SessionSource
// implementations. It owns the lifecycle of any goroutines a source
// launches in Start() and enforces the double-Start guard.
//
// The hook source's Start is a no-op so its registry tracking is
// trivial. The zed source's Start launches an fsnotify-watcher
// goroutine; the registry retains the cancellation handle so a
// subsequent Stop tears it down cleanly.
//
// Registry is safe for concurrent use, although in practice the
// orchestrator (cmd/vv/main.go runZedWatch and the hook handler)
// drives it from a single goroutine. The mutex is conservative.
type Registry struct {
	mu      sync.Mutex
	sources map[string]*entry
}

type entry struct {
	source  SessionSource
	started bool
	cancel  context.CancelFunc
}

// NewRegistry constructs an empty registry.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]*entry)}
}

// Register adds a source to the registry under its Name(). If a
// source with the same Name is already registered, Register returns
// an error. Sources may be registered in any order; Start may then
// be called on individual names.
func (r *Registry) Register(s SessionSource) error {
	if s == nil {
		return fmt.Errorf("sessionsource: nil source")
	}
	name := s.Name()
	if name == "" {
		return fmt.Errorf("sessionsource: source has empty Name()")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sources[name]; ok {
		return fmt.Errorf("sessionsource: %q already registered", name)
	}
	r.sources[name] = &entry{source: s}
	return nil
}

// Get returns the registered source by name (or nil + false if
// missing). Useful for tests and ad-hoc orchestration code that
// wants to call Enabled() before Start.
func (r *Registry) Get(name string) (SessionSource, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.sources[name]
	if !ok {
		return nil, false
	}
	return e.source, true
}

// Names returns the registered source names in insertion order
// (caller-relevant: deterministic Start ordering when iterating).
// Allocates a fresh slice on each call.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.sources))
	for name := range r.sources {
		names = append(names, name)
	}
	return names
}

// Start launches the named source's capture loop. Returns an error
// if the source is not registered, is already started, or its
// Start() returns an error. The provided ctx is wrapped in a
// cancellable child so a later Stop on this source can cancel
// independently of the parent.
func (r *Registry) Start(ctx context.Context, name string, sink Sink) error {
	r.mu.Lock()
	e, ok := r.sources[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("sessionsource: %q not registered", name)
	}
	if e.started {
		r.mu.Unlock()
		return fmt.Errorf("sessionsource: %q already started", name)
	}
	childCtx, cancel := context.WithCancel(ctx)
	e.started = true
	e.cancel = cancel
	r.mu.Unlock()

	// Call Start outside the lock — it may block (zed-acp's Start
	// blocks for the lifetime of the watcher). The lock would
	// serialize all sources to one-at-a-time which defeats the
	// registry's purpose.
	if err := e.source.Start(childCtx, sink); err != nil {
		// Roll back the started flag so a later Start can retry.
		r.mu.Lock()
		e.started = false
		e.cancel = nil
		r.mu.Unlock()
		cancel()
		return err
	}
	return nil
}

// Stop terminates the named source's capture loop. Stop on a
// never-Started source returns nil — this matches the convention
// that Stop is idempotent and safe to call defensively. Stop on an
// unregistered name returns an error (the caller asked about
// something that doesn't exist).
func (r *Registry) Stop(name string) error {
	r.mu.Lock()
	e, ok := r.sources[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("sessionsource: %q not registered", name)
	}
	if !e.started {
		r.mu.Unlock()
		return nil
	}
	cancel := e.cancel
	e.started = false
	e.cancel = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return e.source.Stop()
}

// StopAll calls Stop on every registered source. Errors are
// collected into a single aggregated error (joined with semicolons).
// Useful at process shutdown.
func (r *Registry) StopAll() error {
	r.mu.Lock()
	names := make([]string, 0, len(r.sources))
	for name, e := range r.sources {
		if e.started {
			names = append(names, name)
		}
	}
	r.mu.Unlock()

	var firstErr error
	for _, name := range names {
		if err := r.Stop(name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
