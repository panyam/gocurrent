package gocurrent

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestIdleTimer_ExpiresAfterTimeout verifies that the IdleTimer fires its
// onExpire callback after the configured timeout elapses with no activity.
// This is the basic idle timeout behavior — if nobody calls Acquire(),
// the timer fires and the resource can be cleaned up.
func TestIdleTimer_ExpiresAfterTimeout(t *testing.T) {
	var expired atomic.Bool
	timer := NewIdleTimer(100*time.Millisecond, func() {
		expired.Store(true)
	})
	defer timer.Stop()

	time.Sleep(250 * time.Millisecond)

	if !expired.Load() {
		t.Error("onExpire should have been called after timeout")
	}
}

// TestIdleTimer_AcquirePreventsExpiry verifies that Acquire() pauses the
// idle timer so it does not fire while activity is in progress. Even if
// the timeout duration elapses, the callback must not be called until all
// activity is released.
func TestIdleTimer_AcquirePreventsExpiry(t *testing.T) {
	var expired atomic.Bool
	timer := NewIdleTimer(50*time.Millisecond, func() {
		expired.Store(true)
	})
	defer timer.Stop()

	timer.Acquire()

	// Wait well past the timeout
	time.Sleep(150 * time.Millisecond)

	if expired.Load() {
		t.Error("onExpire should NOT have been called while acquired")
	}

	timer.Release()
}

// TestIdleTimer_ReleaseRestartsTimer verifies that Release() restarts the
// idle timer when the active count drops to zero. The callback fires after
// the full timeout duration from the Release() point (not from creation).
func TestIdleTimer_ReleaseRestartsTimer(t *testing.T) {
	var expired atomic.Bool
	timer := NewIdleTimer(100*time.Millisecond, func() {
		expired.Store(true)
	})
	defer timer.Stop()

	timer.Acquire()
	time.Sleep(150 * time.Millisecond) // well past timeout, but acquired
	timer.Release()

	// Should not have expired yet (just released, timer restarted)
	if expired.Load() {
		t.Error("onExpire should not have fired immediately after Release")
	}

	// Wait for the new timeout
	time.Sleep(150 * time.Millisecond)

	if !expired.Load() {
		t.Error("onExpire should have fired after timeout from Release point")
	}
}

// TestIdleTimer_MultipleAcquireRelease verifies ref counting: if Acquire()
// is called N times, the timer only restarts after N corresponding Release()
// calls. This ensures overlapping activity (concurrent requests) keeps the
// timer paused until ALL activity completes.
func TestIdleTimer_MultipleAcquireRelease(t *testing.T) {
	var expired atomic.Bool
	timer := NewIdleTimer(50*time.Millisecond, func() {
		expired.Store(true)
	})
	defer timer.Stop()

	timer.Acquire()
	timer.Acquire()

	// Release once — still one active
	timer.Release()
	time.Sleep(100 * time.Millisecond)

	if expired.Load() {
		t.Error("onExpire should not fire with one acquire still outstanding")
	}

	// Release second — now zero active, timer restarts
	timer.Release()
	time.Sleep(100 * time.Millisecond)

	if !expired.Load() {
		t.Error("onExpire should have fired after all releases")
	}
}

// TestIdleTimer_StopPreventsExpiry verifies that Stop() permanently cancels
// the idle timer. After Stop(), the onExpire callback must never fire,
// even after the timeout duration elapses.
func TestIdleTimer_StopPreventsExpiry(t *testing.T) {
	var expired atomic.Bool
	timer := NewIdleTimer(50*time.Millisecond, func() {
		expired.Store(true)
	})

	timer.Stop()
	time.Sleep(150 * time.Millisecond)

	if expired.Load() {
		t.Error("onExpire should not fire after Stop()")
	}
}

// TestIdleTimer_StopIdempotent verifies that Stop() can be called multiple
// times without panicking. This is important for cleanup paths where Stop()
// may be called from both explicit session deletion and server shutdown.
func TestIdleTimer_StopIdempotent(t *testing.T) {
	timer := NewIdleTimer(1*time.Second, func() {})
	timer.Stop()
	timer.Stop() // should not panic
	timer.Stop() // should not panic
}

// TestIdleTimer_ZeroTimeoutNoTimer verifies that a zero timeout produces
// a no-op IdleTimer: Acquire/Release/Stop do nothing and no callback fires.
// This is the default behavior when no timeout is configured.
func TestIdleTimer_ZeroTimeoutNoTimer(t *testing.T) {
	timer := NewIdleTimer(0, func() {
		t.Error("onExpire should never be called with zero timeout")
	})

	// All operations should be safe no-ops
	timer.Acquire()
	timer.Release()
	timer.Stop()

	time.Sleep(50 * time.Millisecond)
}

// TestIdleTimer_NilSafe verifies that all methods are safe to call on a nil
// *IdleTimer. This enables callers to store *IdleTimer as a field and call
// methods without nil checks (matching servicekit's nil-safe middleware pattern).
func TestIdleTimer_NilSafe(t *testing.T) {
	var timer *IdleTimer
	timer.Acquire() // should not panic
	timer.Release() // should not panic
	timer.Stop()    // should not panic
}

// TestIdleTimer_ConcurrentAcquireRelease verifies that IdleTimer is safe
// under concurrent access. 100 goroutines each do Acquire+Release, and the
// timer must not corrupt its internal state or panic.
func TestIdleTimer_ConcurrentAcquireRelease(t *testing.T) {
	var expired atomic.Bool
	timer := NewIdleTimer(200*time.Millisecond, func() {
		expired.Store(true)
	})
	defer timer.Stop()

	// Hold one acquire to prevent expiry during the test
	timer.Acquire()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timer.Acquire()
			time.Sleep(time.Millisecond)
			timer.Release()
		}()
	}

	wg.Wait()

	if expired.Load() {
		t.Error("onExpire should not fire while base acquire is held")
	}

	timer.Release() // release base acquire
}
