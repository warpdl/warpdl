package warplib

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPersisterCoalescesBurstWrites verifies that a high-frequency burst
// of markDirty calls collapses into a small number of actual writeFn
// invocations (i.e. the debounce works).
func TestPersisterCoalescesBurstWrites(t *testing.T) {
	var writes atomic.Int64
	p := newPersister(func() error {
		writes.Add(1)
		return nil
	}, 50*time.Millisecond, nil)
	defer p.shutdown()

	// Fire 10k dirty signals in quick succession.
	for i := 0; i < 10_000; i++ {
		p.markDirty()
	}

	// Flush to guarantee the writer observed our dirt.
	if err := p.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if writes.Load() > 10 {
		t.Fatalf("expected heavy coalescing, got %d writes for 10k signals",
			writes.Load())
	}
	if writes.Load() < 1 {
		t.Fatalf("expected at least 1 write, got %d", writes.Load())
	}
}

// TestPersisterFlushIsSynchronous verifies that flush blocks until the
// pending dirt has been written.
func TestPersisterFlushIsSynchronous(t *testing.T) {
	var writes atomic.Int64
	p := newPersister(func() error {
		time.Sleep(20 * time.Millisecond) // simulate slow disk
		writes.Add(1)
		return nil
	}, 10*time.Second /* effectively never */, nil)
	defer p.shutdown()

	p.markDirty()

	before := writes.Load()
	if err := p.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if writes.Load() == before {
		t.Fatal("flush returned without writeFn being called")
	}
}

// TestPersisterShutdownFlushes verifies that shutdown persists any
// remaining dirty state before returning.
func TestPersisterShutdownFlushes(t *testing.T) {
	var writes atomic.Int64
	p := newPersister(func() error {
		writes.Add(1)
		return nil
	}, 5*time.Second, nil)

	p.markDirty()

	if err := p.shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if writes.Load() != 1 {
		t.Fatalf("expected 1 write on shutdown, got %d", writes.Load())
	}
}

// TestPersisterShutdownIdempotent verifies that shutdown can be called
// multiple times without panicking or writing twice.
func TestPersisterShutdownIdempotent(t *testing.T) {
	var writes atomic.Int64
	p := newPersister(func() error {
		writes.Add(1)
		return nil
	}, 100*time.Millisecond, nil)

	p.markDirty()
	_ = p.shutdown()
	// Second call must be safe.
	_ = p.shutdown()
	// Third call must also be safe.
	_ = p.shutdown()

	if writes.Load() != 1 {
		t.Fatalf("expected exactly 1 write across multiple shutdowns, got %d",
			writes.Load())
	}
}

// TestPersisterFlushOnClean verifies that flushing a clean persister does
// not invoke writeFn (avoids pointless I/O).
func TestPersisterFlushOnClean(t *testing.T) {
	var writes atomic.Int64
	p := newPersister(func() error {
		writes.Add(1)
		return nil
	}, 50*time.Millisecond, nil)
	defer p.shutdown()

	if err := p.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if writes.Load() != 0 {
		t.Fatalf("flush on clean persister wrote %d times", writes.Load())
	}
}

// TestPersisterPropagatesWriteErrors verifies that flush returns the
// writeFn error.
func TestPersisterPropagatesWriteErrors(t *testing.T) {
	sentinel := errors.New("disk full")
	p := newPersister(func() error { return sentinel }, 50*time.Millisecond, nil)
	defer p.shutdown()

	p.markDirty()
	if err := p.flush(); !errors.Is(err, sentinel) {
		t.Fatalf("flush error = %v, want %v", err, sentinel)
	}
}

// TestPersisterFlushAfterShutdown verifies flush after shutdown does not
// block forever.
func TestPersisterFlushAfterShutdown(t *testing.T) {
	p := newPersister(func() error { return nil }, time.Second, nil)
	_ = p.shutdown()

	done := make(chan struct{})
	go func() {
		_ = p.flush()
		close(done)
	}()
	select {
	case <-done:
		// Good.
	case <-time.After(time.Second):
		t.Fatal("flush blocked indefinitely after shutdown")
	}
}

// TestPersisterConcurrentMarkDirtyAndFlush stresses the persister with
// many concurrent producers and flushers under the race detector.
func TestPersisterConcurrentMarkDirtyAndFlush(t *testing.T) {
	var writes atomic.Int64
	p := newPersister(func() error {
		writes.Add(1)
		return nil
	}, 10*time.Millisecond, nil)
	defer p.shutdown()

	var wg sync.WaitGroup

	// Producers.
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				p.markDirty()
			}
		}()
	}

	// Flushers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = p.flush()
			}
		}()
	}

	wg.Wait()

	// Give the background loop a moment to drain the final burst, then
	// request an explicit flush.
	if err := p.flush(); err != nil {
		t.Fatalf("final flush: %v", err)
	}

	// We don't assert an exact write count - race against the ticker makes
	// it non-deterministic - but there must be at least one write and far
	// fewer than the 32 * 1000 = 32k markDirty calls.
	w := writes.Load()
	if w < 1 {
		t.Fatalf("expected at least 1 write, got %d", w)
	}
	if w > 32_000 {
		t.Fatalf("no coalescing happened: %d writes for 32k signals", w)
	}
}

// TestPersisterZeroIntervalUsesDefault verifies that passing <=0 falls
// back to DefaultPersistInterval so the writer doesn't hot-loop.
func TestPersisterZeroIntervalUsesDefault(t *testing.T) {
	p := newPersister(func() error { return nil }, 0, nil)
	defer p.shutdown()

	if p.interval != DefaultPersistInterval {
		t.Fatalf("interval=%v, want %v", p.interval, DefaultPersistInterval)
	}
}
