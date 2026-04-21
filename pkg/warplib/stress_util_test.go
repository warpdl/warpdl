package warplib

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// goroutineLeakTolerance is the max additional goroutines allowed after
// a test. We allow a small budget to accommodate Go's runtime (GC, net
// pollers) and the testing framework itself.
const goroutineLeakTolerance = 2

// testLogger captures the minimal subset of *testing.T we need. Using an
// interface lets us exercise the assertions themselves (see mockT below).
type testLogger interface {
	Helper()
	Fatalf(format string, args ...any)
}

// assertNoGoroutineLeak snapshots the goroutine count before running fn
// and asserts the count has returned within tolerance after fn completes.
// It uses a settle loop: goroutines created by fn may take a few
// scheduler ticks to finish exiting even after their logical shutdown.
func assertNoGoroutineLeak(t testLogger, fn func()) {
	t.Helper()

	// Let any prior work settle before taking the baseline.
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	start := runtime.NumGoroutine()

	fn()

	// Settle loop: poll up to 2s for goroutine count to return to baseline
	// before declaring a leak.
	deadline := time.Now().Add(2 * time.Second)
	var end int
	for time.Now().Before(deadline) {
		runtime.GC()
		end = runtime.NumGoroutine()
		if end <= start+goroutineLeakTolerance {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("goroutine leak: start=%d end=%d (tolerance=%d)",
		start, end, goroutineLeakTolerance)
}

// stressRun fans out `workers` goroutines, each running `fn(worker, iter)`
// for `iters` iterations. It waits for all to complete. Suitable for
// driving high concurrency in a single test.
func stressRun(_ *testing.T, workers, iters int, fn func(worker, iter int)) {
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				fn(w, i)
			}
		}(w)
	}
	wg.Wait()
}

// mustNoHang runs fn in a goroutine and fails the test if it does not
// return within d. Intended to catch blocking bugs in "fire-and-forget"
// API paths (e.g. markDirty after shutdown).
func mustNoHang(t testLogger, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("fn did not return within %v (likely deadlock)", d)
	}
}

// TestStressHarness_LeakDetectionFiresOnRealLeak is a meta-test: it makes
// sure assertNoGoroutineLeak actually catches a leak. Without this we'd
// have no confidence that the other stress tests' leak checks do anything.
// We leak well above the tolerance so the assertion is guaranteed to
// observe the delta.
func TestStressHarness_LeakDetectionFiresOnRealLeak(t *testing.T) {
	mt := &mockT{}
	leaked := make(chan struct{})
	const leakCount = goroutineLeakTolerance + 5
	var started sync.WaitGroup
	started.Add(leakCount)
	assertNoGoroutineLeak(mt, func() {
		for i := 0; i < leakCount; i++ {
			go func() {
				started.Done()
				<-leaked // never returns during fn
			}()
		}
		// Ensure the goroutines are all in the blocked state before we
		// exit fn — otherwise the leak window might miss them.
		started.Wait()
	})
	close(leaked) // release the leak so it cleans up
	if !mt.failed {
		t.Fatalf("assertNoGoroutineLeak did not detect %d leaked goroutines", leakCount)
	}
}

// TestStressHarness_NoHangDetectsBlocking verifies mustNoHang fails when
// fn blocks indefinitely.
func TestStressHarness_NoHangDetectsBlocking(t *testing.T) {
	mt := &mockT{}
	release := make(chan struct{})
	mustNoHang(mt, 50*time.Millisecond, func() {
		<-release
	})
	close(release)
	if !mt.failed {
		t.Fatal("mustNoHang did not flag an obvious hang")
	}
}

// mockT is a minimal testLogger stand-in that records whether Fatalf was
// called, letting us exercise the assertions themselves.
type mockT struct {
	failed bool
	msg    string
}

func (m *mockT) Helper()                           {}
func (m *mockT) Fatalf(format string, args ...any) { m.failed = true; m.msg = format }
