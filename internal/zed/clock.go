// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import "time"

// Clock abstracts the timing primitives Watch uses. Production passes
// realClock{}; tests pass a fake clock that advances on demand.
type Clock interface {
	AfterFunc(d time.Duration, f func()) Stoppable
}

// Stoppable matches the subset of *time.Timer that Watch consumes.
// Stop() returns bool to match *time.Timer.Stop() exactly so realClock
// can return a *time.Timer directly without an adapter. Watch ignores
// the return value, but keeping the signature aligned with stdlib means
// no wrapper allocation in the production path.
type Stoppable interface {
	Stop() bool
}

// realClock wraps stdlib time. Default for nil WatcherConfig.Clock.
type realClock struct{}

func (realClock) AfterFunc(d time.Duration, f func()) Stoppable {
	return time.AfterFunc(d, f)
}

// Compile-time interface satisfaction asserts. If either of these
// breaks, the surrounding interface or stdlib has changed and the
// production fast-path needs revisiting.
var (
	_ Clock     = realClock{}
	_ Stoppable = (*time.Timer)(nil)
)
