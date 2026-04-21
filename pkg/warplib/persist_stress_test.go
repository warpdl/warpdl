package warplib

import (
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPersisterStress_ProducerFlusherShutdown drives many concurrent
// producers (markDirty), many concurrent flushers, and eventually
// shuts the persister down - with a mix of writeFn successes and
// errors. Invariants:
//   - no panic anywhere
//   - shutdown returns and is idempotent
//   - no goroutine leak
func TestPersisterStress_ProducerFlusherShutdown(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var (
			writes  atomic.Int64
			errsCnt atomic.Int64
		)
		p := newPersister(func() error {
			writes.Add(1)
			// Fail 10% of writes to exercise error plumbing.
			if rand.IntN(10) == 0 {
				errsCnt.Add(1)
				return errors.New("flaky disk")
			}
			return nil
		}, 5*time.Millisecond, nil)

		var wg sync.WaitGroup

		// Producers.
		const producers = 64
		for w := 0; w < producers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 2000; i++ {
					p.markDirty()
				}
			}()
		}

		// Flushers - racing with producers.
		const flushers = 8
		for w := 0; w < flushers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					_ = p.flush()
				}
			}()
		}

		wg.Wait()

		// Shut down and verify it's idempotent.
		_ = p.shutdown()
		mustNoHang(t, 500*time.Millisecond, func() {
			_ = p.shutdown()
			_ = p.shutdown()
		})

		if writes.Load() < 1 {
			t.Fatalf("expected at least 1 write, got %d", writes.Load())
		}
	})
}

// TestPersisterStress_PanickingWriteFn repeatedly crashes writeFn. The
// persister must:
//   - not crash the test process
//   - not leak goroutines
//   - surface the panic as an error on flush
//   - keep the writer goroutine alive for subsequent flushes (recovery)
func TestPersisterStress_PanickingWriteFn(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var calls atomic.Int64
		var shouldPanic atomic.Bool

		p := newPersister(func() error {
			n := calls.Add(1)
			if shouldPanic.Load() && n%3 == 0 {
				panic("boom")
			}
			return nil
		}, 3*time.Millisecond, nil)
		defer p.shutdown()

		// Trigger panicking phase.
		shouldPanic.Store(true)

		var sawPanicErr atomic.Int32
		var wg sync.WaitGroup
		for w := 0; w < 16; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 100; i++ {
					p.markDirty()
					if err := p.flush(); err != nil {
						sawPanicErr.Add(1)
					}
				}
			}()
		}
		wg.Wait()

		// At least one flush must have surfaced the panic as an error.
		if sawPanicErr.Load() == 0 {
			t.Fatalf("expected some flush calls to surface panic, got 0")
		}

		// Recovery phase: writer must still work after panics.
		shouldPanic.Store(false)
		p.markDirty()
		if err := p.flush(); err != nil {
			t.Fatalf("writer did not recover after panics: %v", err)
		}
	})
}

// TestPersisterStress_RapidCreateShutdown creates and shuts down many
// persisters back-to-back. Catches leaks where the writer goroutine
// isn't tied to shutdown.
func TestPersisterStress_RapidCreateShutdown(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		for i := 0; i < 200; i++ {
			p := newPersister(func() error { return nil }, time.Millisecond, nil)
			if i%3 == 0 {
				p.markDirty()
			}
			if err := p.shutdown(); err != nil {
				t.Fatalf("shutdown: %v", err)
			}
		}
	})
}

// TestPersisterStress_UseAfterShutdown documents that all public methods
// remain safe (no panic, bounded latency) after shutdown.
func TestPersisterStress_UseAfterShutdown(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		p := newPersister(func() error { return nil }, 10*time.Millisecond, nil)
		_ = p.shutdown()

		// markDirty after shutdown must be a fast no-op (writer is
		// gone, flag gets set but nobody reads it).
		mustNoHang(t, 500*time.Millisecond, func() {
			for i := 0; i < 1000; i++ {
				p.markDirty()
			}
		})

		// flush after shutdown must return promptly.
		mustNoHang(t, 500*time.Millisecond, func() {
			if err := p.flush(); err != nil {
				t.Fatalf("flush after shutdown returned error: %v", err)
			}
		})
	})
}

// TestPersisterStress_WriteFnReturnsError exercises the error-propagation
// path under load. flush must surface errors but must not wedge the
// writer goroutine.
func TestPersisterStress_WriteFnReturnsError(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		sentinel := errors.New("disk full")
		var shouldErr atomic.Bool
		p := newPersister(func() error {
			if shouldErr.Load() {
				return sentinel
			}
			return nil
		}, 5*time.Millisecond, nil)
		defer p.shutdown()

		// Working phase.
		p.markDirty()
		if err := p.flush(); err != nil {
			t.Fatalf("unexpected flush error: %v", err)
		}

		// Error phase.
		shouldErr.Store(true)
		p.markDirty()
		if err := p.flush(); !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got: %v", err)
		}

		// Recovery phase.
		shouldErr.Store(false)
		p.markDirty()
		if err := p.flush(); err != nil {
			t.Fatalf("writer did not recover: %v", err)
		}
	})
}

// TestPersisterStress_ZeroIntervalIsSafe ensures calling newPersister
// with interval=0 falls back to the default and doesn't hot-loop.
func TestPersisterStress_ZeroIntervalIsSafe(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		p := newPersister(func() error { return nil }, 0, nil)
		defer p.shutdown()

		p.markDirty()
		if err := p.flush(); err != nil {
			t.Fatalf("flush: %v", err)
		}
	})
}

// TestPersisterStress_FlushHangDeadline catches the case where shutdown
// could theoretically deadlock with an in-flight flush request. We
// issue many flushes concurrently with a single shutdown.
func TestPersisterStress_FlushHangDeadline(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		p := newPersister(func() error {
			time.Sleep(2 * time.Millisecond)
			return nil
		}, 2*time.Millisecond, nil)

		var wg sync.WaitGroup
		const flushers = 16
		for w := 0; w < flushers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 50; i++ {
					p.markDirty()
					_ = p.flush()
				}
			}()
		}

		// Let flushers run briefly, then shutdown concurrently.
		time.Sleep(20 * time.Millisecond)
		mustNoHang(t, 3*time.Second, func() {
			_ = p.shutdown()
		})
		wg.Wait()
	})
}
