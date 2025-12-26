package warplib

import (
	"sync"
	"testing"
)

// TestVMapDumpConcurrentModification tests Race 1: VMap.Dump() reading len(vm.kv)
// without lock protection before acquiring the lock for iteration.
// This test will fail with -race if the bug exists.
func TestVMapDumpConcurrentModification(t *testing.T) {
	vm := NewVMap[int, string]()
	var wg sync.WaitGroup

	// 10 writers adding entries concurrently
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				vm.Set(id*100+i, "value")
			}
		}(w)
	}

	// 5 concurrent Dump() callers
	for d := 0; d < 5; d++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				keys, vals := vm.Dump()
				if len(keys) != len(vals) {
					t.Errorf("mismatch: keys=%d vals=%d", len(keys), len(vals))
				}
			}
		}()
	}

	wg.Wait()
}

// TestVMapDumpConsistency verifies that Dump() returns consistent snapshots
// even under concurrent modifications.
func TestVMapDumpConsistency(t *testing.T) {
	vm := NewVMap[string, int]()
	var wg sync.WaitGroup

	// Add initial data
	for i := 0; i < 50; i++ {
		vm.Set("initial", i)
	}

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			vm.Set("concurrent", i)
		}
	}()

	// Multiple readers calling Dump()
	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				keys, vals := vm.Dump()
				// Verify internal consistency
				if len(keys) != len(vals) {
					t.Errorf("inconsistent dump: keys=%d vals=%d", len(keys), len(vals))
				}
			}
		}()
	}

	wg.Wait()
}
