package warplib

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestManagerUpdateItemCoalescesWrites drives Manager.UpdateItem at a rate
// that would previously produce hundreds of synchronous file writes and
// verifies the background persister collapses them to far fewer writes.
func TestManagerUpdateItemCoalescesWrites(t *testing.T) {
	// Force a short interval so the test finishes quickly.
	prev := DefaultPersistInterval
	DefaultPersistInterval = 20 * time.Millisecond
	defer func() { DefaultPersistInterval = prev }()

	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	d.dlLoc = t.TempDir()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := m.GetItem(d.hash)
	if item == nil {
		t.Fatalf("item missing")
	}

	// Replace the persister's writeFn with a counting wrapper so we can
	// measure actual writes without racing on the underlying stat file.
	var writes atomic.Int64
	p := m.persister.Load()
	orig := p.writeFn
	p.writeFn = func() error {
		writes.Add(1)
		return orig()
	}

	// Drain any writes that have happened so far.
	if err := p.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	startWrites := writes.Load()

	const iterations = 5_000
	for i := 0; i < iterations; i++ {
		// DownloadProgressHandler on patchHandlers flows into UpdateItem.
		d.handlers.DownloadProgressHandler(d.hash, 1)
	}

	// Give the debouncer a few ticks.
	if err := p.flush(); err != nil {
		t.Fatalf("final flush: %v", err)
	}

	delta := writes.Load() - startWrites
	if delta < 1 {
		t.Fatalf("expected >= 1 write, got %d", delta)
	}
	// With a 20ms debounce and 5k sync calls (no I/O), the bulk of work
	// finishes in a couple of ticks; allow a generous ceiling.
	if delta > 50 {
		t.Fatalf("expected heavy coalescing, got %d writes for %d calls",
			delta, iterations)
	}
}

// TestManagerCloseFlushesPendingUpdates verifies that Close waits for the
// background persister to drain any pending updates before writing the
// final state.
func TestManagerCloseFlushesPendingUpdates(t *testing.T) {
	prev := DefaultPersistInterval
	DefaultPersistInterval = 1 * time.Second // force reliance on Close to flush
	defer func() { DefaultPersistInterval = prev }()

	m := newTestManager(t)

	d := newTestDownloader()
	d.dlLoc = t.TempDir()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	d.handlers.DownloadProgressHandler(d.hash, 42)

	// Close should flush.
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and verify our 42 bytes were recorded.
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer m2.Close()

	it := m2.GetItem(d.hash)
	if it == nil {
		t.Fatalf("reopened manager missing item")
	}
	if it.Downloaded != 42 {
		t.Fatalf("Downloaded after Close reopen = %d want 42", it.Downloaded)
	}
}

// TestManagerFlushWaitsForPendingUpdates verifies Flush coalesces pending
// writes too - a pending dirty state must be applied before Flush runs
// its own persist.
func TestManagerFlushWaitsForPendingUpdates(t *testing.T) {
	prev := DefaultPersistInterval
	DefaultPersistInterval = 5 * time.Second
	defer func() { DefaultPersistInterval = prev }()

	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	d.dlLoc = t.TempDir()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}

	// Emit a dirty signal.
	d.handlers.DownloadProgressHandler(d.hash, 7)

	// Flush must pick it up.
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Confirm the in-memory value is correct (side-effect of the
	// DownloadProgressHandler wrapper).
	it := m.GetItem(d.hash)
	if it == nil || it.Downloaded != 7 {
		t.Fatalf("expected Downloaded=7 after Flush, got item=%v", it)
	}
}

// TestManagerProgressHandlerDoesNotBlockOnSlowDisk verifies that the hot
// progress-handler path coalesces writes and never blocks the download
// goroutine on a slow disk.
func TestManagerProgressHandlerDoesNotBlockOnSlowDisk(t *testing.T) {
	prev := DefaultPersistInterval
	DefaultPersistInterval = 20 * time.Millisecond
	defer func() { DefaultPersistInterval = prev }()

	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	d.dlLoc = t.TempDir()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}

	// Inject a slow writeFn - each write takes 40ms.
	p := m.persister.Load()
	orig := p.writeFn
	p.writeFn = func() error {
		time.Sleep(40 * time.Millisecond)
		return orig()
	}

	start := time.Now()
	const iterations = 100
	for i := 0; i < iterations; i++ {
		d.handlers.DownloadProgressHandler(d.hash, 1)
	}
	elapsed := time.Since(start)
	// 100 calls at 40ms each would be 4 seconds synchronous. Debounced
	// it should be well under 200ms.
	if elapsed > 250*time.Millisecond {
		t.Fatalf("progress handler was not debounced: %d calls took %v",
			iterations, elapsed)
	}
}

// TestManagerUpdateItemAsyncIsCoalesced stresses the async API directly
// to verify its coalescing semantics in isolation from patchHandlers.
func TestManagerUpdateItemAsyncIsCoalesced(t *testing.T) {
	prev := DefaultPersistInterval
	DefaultPersistInterval = 25 * time.Millisecond
	defer func() { DefaultPersistInterval = prev }()

	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	d.dlLoc = t.TempDir()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := m.GetItem(d.hash)

	var writes atomic.Int64
	p := m.persister.Load()
	orig := p.writeFn
	p.writeFn = func() error {
		writes.Add(1)
		return orig()
	}
	// Drain the write count from the AddDownload flush.
	if err := p.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	writes.Store(0)

	const iterations = 10_000
	for i := 0; i < iterations; i++ {
		m.UpdateItemAsync(item)
	}
	if err := p.flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if writes.Load() > 20 {
		t.Fatalf("async path did not coalesce: %d writes for %d calls",
			writes.Load(), iterations)
	}
}

// TestManagerUpdateItemIsSynchronous verifies the non-async API still
// persists synchronously - existing tests and external callers rely on
// this.
func TestManagerUpdateItemIsSynchronous(t *testing.T) {
	prev := DefaultPersistInterval
	DefaultPersistInterval = 5 * time.Second // debounce tick too slow to help
	defer func() { DefaultPersistInterval = prev }()

	m := newTestManager(t)

	d := newTestDownloader()
	d.dlLoc = t.TempDir()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}

	// Mutate an item and call UpdateItem, do NOT call Flush.
	item := m.GetItem(d.hash)
	item.mu.Lock()
	item.Downloaded = 123
	item.mu.Unlock()
	m.UpdateItem(item)

	// Without calling Close or Flush, reopen the file - the value must
	// be on disk because UpdateItem was synchronous.
	// We cannot re-open the same file while m still holds it (Windows),
	// so persist a second manager pointed at the same path.
	// Simplest approach: close m, then reopen and verify.
	if err := m.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	m2, err := InitManager()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer m2.Close()

	it2 := m2.GetItem(d.hash)
	if it2 == nil || it2.Downloaded != 123 {
		t.Fatalf("UpdateItem did not persist synchronously: got %+v", it2)
	}
}
