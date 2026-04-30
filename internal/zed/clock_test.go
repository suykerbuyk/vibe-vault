// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a deterministic Clock for tests. Time only advances on
// explicit Advance calls; AfterFunc registrations fire synchronously
// when their deadline is reached. Safe for concurrent use from one
// watcher goroutine plus one test goroutine. Two Advance calls must
// not run concurrently with each other.
type fakeClock struct {
	mu         sync.Mutex
	now        time.Time
	pending    []*fakeTimer
	registered int // monotonic count of AfterFunc calls (test sync point)
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0)}
}

// fakeTimer holds a back-pointer to its parent clock so Stop() can
// acquire the same mutex Advance() uses. v2 of this plan omitted the
// back-pointer and Stop() mutated cancelled/fired without the lock,
// which races with Advance() writing fired=true under the lock. The
// race was reachable in practice because Watch's goroutine calls
// Stop() on every fsnotify event while the test goroutine calls
// Advance(); shared mutable state with no synchronization is a data
// race even when the project's CI doesn't run -race.
type fakeTimer struct {
	fireAt    time.Time
	fn        func()
	clock     *fakeClock
	cancelled bool // protected by clock.mu
	fired     bool // protected by clock.mu
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Stoppable {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{fireAt: c.now.Add(d), fn: f, clock: c}
	c.pending = append(c.pending, t)
	c.registered++
	return t
}

// Stop matches *time.Timer.Stop semantics: returns true if the timer
// was active (not yet fired and not yet stopped). Acquires the parent
// clock's mutex to serialize against Advance().
func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.fired || t.cancelled {
		return false
	}
	t.cancelled = true
	return true
}

// Advance moves the clock forward by d, firing any pending AfterFunc
// whose deadline is now reached. Fires synchronously in registration
// order. The clock mutex is released before each callback runs, so
// callbacks may freely call AfterFunc, Stop, Pending, Registered, or
// Advance on this clock without deadlock — each acquires clock.mu
// independently. Callbacks that register additional timers see those
// evaluated against the post-Advance time, so they may fire in the
// same Advance call.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		var next *fakeTimer
		for _, t := range c.pending {
			if t.cancelled || t.fired {
				continue
			}
			if !t.fireAt.After(c.now) {
				next = t
				break
			}
		}
		if next == nil {
			c.mu.Unlock()
			return
		}
		next.fired = true
		c.mu.Unlock()
		next.fn()
	}
}

// Registered returns the monotonic count of AfterFunc calls made since
// clock creation. Tests use this as a synchronization barrier: wait
// until Registered() reaches an expected value before asserting.
func (c *fakeClock) Registered() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.registered
}

// Pending returns the count of timers that have not been fired or stopped.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.pending {
		if !t.fired && !t.cancelled {
			n++
		}
	}
	return n
}

// --- Self-tests for the fake clock ---

func TestFakeClock_AdvanceFiresExpiredTimers(t *testing.T) {
	c := newFakeClock()
	var fired atomic.Int32
	c.AfterFunc(100*time.Millisecond, func() { fired.Add(1) })
	if fired.Load() != 0 {
		t.Fatalf("timer fired before Advance: got %d", fired.Load())
	}
	c.Advance(100 * time.Millisecond)
	if got := fired.Load(); got != 1 {
		t.Fatalf("expected 1 fire after Advance(100ms), got %d", got)
	}
}

func TestFakeClock_AdvanceShortOfDeadlineDoesNotFire(t *testing.T) {
	c := newFakeClock()
	var fired atomic.Int32
	c.AfterFunc(100*time.Millisecond, func() { fired.Add(1) })
	c.Advance(99 * time.Millisecond)
	if got := fired.Load(); got != 0 {
		t.Fatalf("expected 0 fires before deadline, got %d", got)
	}
	c.Advance(1 * time.Millisecond)
	if got := fired.Load(); got != 1 {
		t.Fatalf("expected 1 fire at deadline, got %d", got)
	}
}

func TestFakeClock_StopBeforeFireReturnsTrue(t *testing.T) {
	c := newFakeClock()
	timer := c.AfterFunc(100*time.Millisecond, func() {})
	if !timer.Stop() {
		t.Fatalf("Stop() before fire should return true")
	}
	if timer.Stop() {
		t.Fatalf("second Stop() should return false (already cancelled)")
	}
}

func TestFakeClock_StopAfterFireReturnsFalse(t *testing.T) {
	c := newFakeClock()
	timer := c.AfterFunc(100*time.Millisecond, func() {})
	c.Advance(100 * time.Millisecond)
	if timer.Stop() {
		t.Fatalf("Stop() after fire should return false")
	}
}

func TestFakeClock_RegisteredCountsAllAfterFunc(t *testing.T) {
	c := newFakeClock()
	if got := c.Registered(); got != 0 {
		t.Fatalf("Registered() initial: want 0, got %d", got)
	}
	for i := 0; i < 5; i++ {
		c.AfterFunc(time.Second, func() {})
	}
	if got := c.Registered(); got != 5 {
		t.Fatalf("Registered() after 5 calls: want 5, got %d", got)
	}
	// Stopping a timer does not decrement Registered.
	t6 := c.AfterFunc(time.Second, func() {})
	t6.Stop()
	if got := c.Registered(); got != 6 {
		t.Fatalf("Registered() after stop: want 6 (monotonic), got %d", got)
	}
}

func TestFakeClock_PendingExcludesStoppedAndFired(t *testing.T) {
	c := newFakeClock()
	t1 := c.AfterFunc(100*time.Millisecond, func() {})
	c.AfterFunc(100*time.Millisecond, func() {}) // will fire
	t3 := c.AfterFunc(time.Second, func() {})    // will remain pending
	if got := c.Pending(); got != 3 {
		t.Fatalf("Pending() after 3 registrations: want 3, got %d", got)
	}
	t1.Stop()
	if got := c.Pending(); got != 2 {
		t.Fatalf("Pending() after Stop(t1): want 2, got %d", got)
	}
	c.Advance(100 * time.Millisecond) // fires t2
	if got := c.Pending(); got != 1 {
		t.Fatalf("Pending() after Advance fires t2: want 1, got %d", got)
	}
	_ = t3
}

func TestFakeClock_CallbackCanRegisterMoreTimers(t *testing.T) {
	c := newFakeClock()
	var outer, inner atomic.Int32
	c.AfterFunc(100*time.Millisecond, func() {
		outer.Add(1)
		// Register an inner timer with deadline equal to current (post-Advance) now.
		c.AfterFunc(0, func() { inner.Add(1) })
	})
	c.Advance(100 * time.Millisecond)
	if got := outer.Load(); got != 1 {
		t.Fatalf("outer fired: want 1, got %d", got)
	}
	// The inner timer has deadline now+0 == now, so !fireAt.After(now) is true; it should fire in the same Advance.
	if got := inner.Load(); got != 1 {
		t.Fatalf("inner timer registered in callback should fire in same Advance: got %d", got)
	}
}

func TestFakeClock_StopRaceWithAdvance(t *testing.T) {
	// Spawn a goroutine that registers and Stops many timers in a tight
	// loop while the main test calls Advance repeatedly. With the
	// back-pointer + mutex in fakeTimer.Stop, no data race exists.
	// The state-consistency invariant must hold: every registered
	// timer ends in exactly one of two terminal states —
	// fired XOR cancelled. Never both true (Stop won AND Advance fired
	// the same timer); never both false (test ended before steady state).
	c := newFakeClock()
	const N = 1000

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < N; i++ {
			tm := c.AfterFunc(1*time.Millisecond, func() {})
			tm.Stop()
		}
	}()

	// Drive Advance concurrently with the registration/Stop goroutine.
	for {
		c.Advance(1 * time.Millisecond)
		select {
		case <-done:
			// Final Advance after registrations complete to flush any
			// timers that the goroutine registered but didn't get to
			// Stop before Advance fired them.
			c.Advance(1 * time.Second)
			goto check
		default:
		}
	}

check:
	c.mu.Lock()
	defer c.mu.Unlock()
	if got := len(c.pending); got != N {
		t.Fatalf("expected %d registered timers, got %d", N, got)
	}
	for i, tm := range c.pending {
		switch {
		case tm.fired && tm.cancelled:
			t.Fatalf("timer %d in inconsistent state: fired AND cancelled", i)
		case !tm.fired && !tm.cancelled:
			t.Fatalf("timer %d in inconsistent state: neither fired nor cancelled (steady state not reached)", i)
		}
	}
}
