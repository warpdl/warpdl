package warplib

import (
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// TestVMapStress_MixedOps drives high-concurrency random operations
// (Set, Get, Delete, Range, Len, Make, Reset) against a shared VMap.
// Invariants:
//   - no panic
//   - Len always returns a non-negative int
//   - no goroutine leak
//
// We don't assert specific contents because the interleaving makes them
// non-deterministic; we assert the operations terminate and the
// container stays internally consistent.
func TestVMapStress_MixedOps(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var vm VMap[int, int]
		const (
			workers = 64
			iters   = 5000
		)
		var (
			panics    atomic.Int32
			negLens   atomic.Int32
			operation atomic.Int64
		)
		stressRun(t, workers, iters, func(w, i int) {
			defer func() {
				if r := recover(); r != nil {
					panics.Add(1)
				}
			}()
			operation.Add(1)

			switch rand.IntN(8) {
			case 0:
				vm.Set(rand.IntN(500), i)
			case 1:
				_ = vm.Get(rand.IntN(500))
			case 2:
				vm.Delete(rand.IntN(500))
			case 3:
				vm.Range(func(_, _ int) bool { return true })
			case 4:
				if vm.Len() < 0 {
					negLens.Add(1)
				}
			case 5:
				vm.Make()
			case 6:
				// Reset rarely - it's destructive.
				if rand.IntN(100) == 0 {
					vm.Reset()
				}
			case 7:
				vm.RangeLocked(func(_, _ int) bool { return true })
			}
		})
		if panics.Load() != 0 {
			t.Fatalf("unexpected panics: %d", panics.Load())
		}
		if negLens.Load() != 0 {
			t.Fatalf("Len returned negative: %d times", negLens.Load())
		}
	})
}

// TestVMapStress_RangeCallbackMutates verifies that mutating the map
// from inside Range (which was impossible with the old RLock
// implementation) is now safe and terminates.
func TestVMapStress_RangeCallbackMutates(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var vm VMap[int, int]
		for i := 0; i < 100; i++ {
			vm.Set(i, i)
		}

		// Callback mutates the same map while iterating.
		var callbackInvocations atomic.Int32
		vm.Range(func(k, v int) bool {
			callbackInvocations.Add(1)
			vm.Set(k+1000, v)
			vm.Delete(k + 500) // delete something that may not exist
			return true
		})
		if callbackInvocations.Load() != 100 {
			t.Fatalf("Range visited %d, want 100 (snapshot size)",
				callbackInvocations.Load())
		}
		// Mutations must be visible afterwards.
		if vm.Len() < 100 {
			t.Fatalf("expected mutations to be persisted, Len=%d", vm.Len())
		}
	})
}

// TestVMapStress_MakeResetRace races concurrent Make / Reset / Set / Get
// calls. None of these are allowed to panic or leave the map in an
// inconsistent state.
func TestVMapStress_MakeResetRace(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var vm VMap[int, string]
		const workers = 48
		const iters = 3000
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < iters; i++ {
					switch i % 5 {
					case 0:
						vm.Make()
					case 1:
						vm.Reset()
					case 2:
						vm.Set(i+w*iters, "v")
					case 3:
						_ = vm.Get(i)
					case 4:
						_ = vm.Len()
					}
				}
			}(w)
		}
		wg.Wait()
	})
}

// TestVMapAllocBudget_GetSet verifies the Get and Set fast paths do not
// allocate memory in steady state.
func TestVMapAllocBudget_GetSet(t *testing.T) {
	var vm VMap[int, int]
	// Seed with enough entries that map growth doesn't happen mid-test.
	for i := 0; i < 256; i++ {
		vm.Set(i, i)
	}
	runtime.GC()

	allocsGet := testing.AllocsPerRun(500, func() {
		_ = vm.Get(42)
	})
	if allocsGet > 0 {
		t.Fatalf("VMap.Get allocs = %.1f, want 0", allocsGet)
	}

	allocsSet := testing.AllocsPerRun(500, func() {
		vm.Set(42, 99) // overwrite existing key
	})
	if allocsSet > 0 {
		t.Fatalf("VMap.Set(existing) allocs = %.1f, want 0", allocsSet)
	}
}

// TestVMapStress_DumpConsistency verifies Dump returns equal-length
// key/value slices even under very heavy concurrent mutation.
func TestVMapStress_DumpConsistency(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var vm VMap[int, int]
		for i := 0; i < 200; i++ {
			vm.Set(i, i)
		}

		var wg sync.WaitGroup
		stop := make(chan struct{})

		for w := 0; w < 16; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					default:
					}
					vm.Set(w*10000+i, i)
					if i%3 == 0 {
						vm.Delete(w*10000 + i - 1)
					}
				}
			}(w)
		}

		// Reader asserts Dump consistency for ~200 ms.
		var mismatches atomic.Int32
		for i := 0; i < 5000; i++ {
			keys, vals := vm.Dump()
			if len(keys) != len(vals) {
				mismatches.Add(1)
			}
		}
		close(stop)
		wg.Wait()

		if mismatches.Load() != 0 {
			t.Fatalf("Dump returned unequal-length slices %d times",
				mismatches.Load())
		}
	})
}

// TestVMapStress_RangeEarlyTerminationNoLeak verifies that Range
// returning early does not leave goroutines or locks in a weird state.
func TestVMapStress_RangeEarlyTerminationNoLeak(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		var vm VMap[int, int]
		for i := 0; i < 1000; i++ {
			vm.Set(i, i)
		}

		for i := 0; i < 200; i++ {
			limit := rand.IntN(100)
			count := 0
			vm.Range(func(_, _ int) bool {
				count++
				return count < limit
			})
		}
	})
}
