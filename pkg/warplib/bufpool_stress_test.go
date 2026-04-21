package warplib

import (
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// TestBufPoolStress_RandomSizes runs many workers concurrently requesting
// buffers of random sizes. Invariants:
//   - every returned buffer has len == requested size
//   - every returned buffer has cap >= requested size
//   - writes by one worker are never observed by another concurrent worker
//     (same isolation property as TestBufPoolExclusivity, but across a
//     wider size distribution and under higher load)
//   - no goroutines leak
func TestBufPoolStress_RandomSizes(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		const (
			workers = 64
			iters   = 2000
		)
		var mismatches atomic.Int32
		stressRun(t, workers, iters, func(w, _ int) {
			// Wide size distribution including boundary values.
			var size int
			switch rand.IntN(5) {
			case 0:
				size = 1
			case 1:
				size = int(DEF_CHUNK_SIZE) - 1
			case 2:
				size = int(DEF_CHUNK_SIZE)
			case 3:
				size = int(DEF_CHUNK_SIZE) + 1
			default:
				size = 1 + rand.IntN(4*int(DEF_CHUNK_SIZE))
			}

			bp := getBuf(size)
			if len(*bp) != size {
				mismatches.Add(1)
				putBuf(bp)
				return
			}
			if cap(*bp) < size {
				mismatches.Add(1)
				putBuf(bp)
				return
			}
			// Brand the buffer with our worker id and verify no other
			// worker wrote here under us.
			brand := byte(w)
			for i := range *bp {
				(*bp)[i] = brand
			}
			// Sample the first and last byte after writing. Because the
			// pool is exclusive the whole buffer must still read `brand`.
			if (*bp)[0] != brand || (*bp)[len(*bp)-1] != brand {
				mismatches.Add(1)
			}
			putBuf(bp)
		})
		if m := mismatches.Load(); m != 0 {
			t.Fatalf("buffer isolation violated %d times", m)
		}
	})
}

// TestBufPoolStress_DoublePutSafety models a careless user returning the
// same buffer pointer twice. sync.Pool itself tolerates this (it just
// stores the pointer twice), but we verify:
//  1. No panic.
//  2. Whichever subsequent getter gets the duplicated pointer still
//     receives a usable buffer of the requested length.
//
// Note: we do not promise "double-put protection" - this test documents
// that the behaviour is at-worst benign.
func TestBufPoolStress_DoublePutSafety(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("double putBuf panicked: %v", r)
			}
		}()

		for i := 0; i < 1000; i++ {
			bp := getBuf(int(DEF_CHUNK_SIZE))
			putBuf(bp)
			putBuf(bp) // intentional double-put
		}

		// Drain pool and confirm every buffer we fetch is usable.
		for i := 0; i < 1000; i++ {
			bp := getBuf(int(DEF_CHUNK_SIZE))
			if len(*bp) != int(DEF_CHUNK_SIZE) {
				t.Fatalf("post double-put buffer has wrong length: %d", len(*bp))
			}
			if cap(*bp) < int(DEF_CHUNK_SIZE) {
				t.Fatalf("post double-put buffer has shrunk cap: %d", cap(*bp))
			}
			putBuf(bp)
		}
	})
}

// TestBufPoolAllocBudget_SteadyState verifies the steady-state
// round-trip cost of getBuf/putBuf is zero allocations. Uses
// testing.AllocsPerRun which is deterministic for this purpose.
func TestBufPoolAllocBudget_SteadyState(t *testing.T) {
	// Prime the pool so first-use allocation doesn't skew the average.
	for i := 0; i < 10; i++ {
		putBuf(getBuf(int(DEF_CHUNK_SIZE)))
	}
	runtime.GC()

	allocs := testing.AllocsPerRun(100, func() {
		bp := getBuf(int(DEF_CHUNK_SIZE))
		putBuf(bp)
	})
	if allocs > 0 {
		t.Fatalf("getBuf/putBuf steady-state allocs = %.1f, want 0", allocs)
	}
}

// TestBufPoolStress_HighChurnBytesAllocated creates a pathological pattern:
// many workers race to drain and repopulate the pool. We assert that the
// total bytes allocated by the process during this workload is a tiny
// fraction of what `make([]byte, DEF_CHUNK_SIZE)` would have allocated
// for each op - i.e. the pool genuinely prevents 32 KB allocations.
//
// Note: `Mallocs` counts all allocations process-wide (including GC
// bookkeeping, test framework, and sync.Pool's internal structures that
// may grow on first-use). We measure *bytes* because that's where the
// pool wins, and it is dominated by the chunk buffers.
func TestBufPoolStress_HighChurnBytesAllocated(t *testing.T) {
	const workers = 32
	const iters = 1000
	const ops = workers * iters
	const hypotheticalRawBytes = uint64(ops) * uint64(DEF_CHUNK_SIZE)

	// Prime pool.
	for i := 0; i < workers; i++ {
		putBuf(getBuf(int(DEF_CHUNK_SIZE)))
	}
	runtime.GC()

	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				bp := getBuf(int(DEF_CHUNK_SIZE))
				(*bp)[0] = 1
				putBuf(bp)
			}
		}()
	}
	wg.Wait()

	runtime.ReadMemStats(&memAfter)
	bytesAllocated := memAfter.TotalAlloc - memBefore.TotalAlloc
	// Budget: sync.Pool uses per-P local caches, so under high
	// GOMAXPROCS and heavy concurrency the pool occasionally misses
	// (bucket in CPU A, request from CPU B). We still demand the pool
	// saves at least 50% of what a naive make() would allocate -
	// anything materially worse would mean the pool is broken.
	budget := hypotheticalRawBytes / 2 // 50%
	if bytesAllocated >= budget {
		t.Fatalf("high-churn alloc bytes=%d budget=%d (raw would be %d)",
			bytesAllocated, budget, hypotheticalRawBytes)
	}
	t.Logf("bytes allocated=%d (vs %d raw), saved %.1f%%",
		bytesAllocated, hypotheticalRawBytes,
		100.0*(1.0-float64(bytesAllocated)/float64(hypotheticalRawBytes)))
}

// TestBufPoolStress_LargeRequestDoesNotShrinkPool verifies that asking
// for a buffer much larger than DEF_CHUNK_SIZE doesn't poison the pool -
// subsequent small requests still get a pooled buffer, not fresh
// allocations.
func TestBufPoolStress_LargeRequestDoesNotShrinkPool(t *testing.T) {
	// Warm the pool.
	for i := 0; i < 10; i++ {
		putBuf(getBuf(int(DEF_CHUNK_SIZE)))
	}
	runtime.GC()

	// Issue a huge request; discard buffer.
	huge := getBuf(int(DEF_CHUNK_SIZE) * 1000)
	putBuf(huge)

	// Subsequent default-size round-trips must still be zero-alloc.
	allocs := testing.AllocsPerRun(50, func() {
		bp := getBuf(int(DEF_CHUNK_SIZE))
		putBuf(bp)
	})
	if allocs > 0 {
		t.Fatalf("post-huge alloc budget violated: allocs=%.1f want 0", allocs)
	}
}
