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

// =============================================================================
// TDD Cycle 4: VMap Range and Delete Tests (RED)
// =============================================================================

// TestVMapRange tests the Range method for thread-safe iteration.
func TestVMapRange(t *testing.T) {
	t.Run("iterate all entries", func(t *testing.T) {
		vm := NewVMap[int, string]()
		vm.Set(1, "one")
		vm.Set(2, "two")
		vm.Set(3, "three")

		visited := make(map[int]string)
		vm.Range(func(key int, val string) bool {
			visited[key] = val
			return true
		})

		if len(visited) != 3 {
			t.Errorf("Range visited %d entries, want 3", len(visited))
		}
		if visited[1] != "one" || visited[2] != "two" || visited[3] != "three" {
			t.Errorf("Range returned incorrect values: %v", visited)
		}
	})

	t.Run("early termination", func(t *testing.T) {
		vm := NewVMap[int, string]()
		for i := 0; i < 10; i++ {
			vm.Set(i, "value")
		}

		count := 0
		vm.Range(func(key int, val string) bool {
			count++
			return count < 3 // stop after 3 iterations
		})

		if count != 3 {
			t.Errorf("Range should stop after 3, got %d", count)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		vm := NewVMap[int, string]()
		count := 0
		vm.Range(func(key int, val string) bool {
			count++
			return true
		})

		if count != 0 {
			t.Errorf("Range on empty map should visit 0, got %d", count)
		}
	})
}

// TestVMapRangeConcurrent tests Range under concurrent modifications.
func TestVMapRangeConcurrent(t *testing.T) {
	vm := NewVMap[int, string]()
	var wg sync.WaitGroup

	// Add initial data
	for i := 0; i < 50; i++ {
		vm.Set(i, "initial")
	}

	// Concurrent writers
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				vm.Set(1000+id*100+i, "concurrent")
			}
		}(w)
	}

	// Concurrent Range calls
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				count := 0
				vm.Range(func(key int, val string) bool {
					count++
					return true
				})
				// Should not panic or have race
			}
		}()
	}

	wg.Wait()
}

// TestVMapDelete tests the Delete method for thread-safe removal.
func TestVMapDelete(t *testing.T) {
	t.Run("delete existing key", func(t *testing.T) {
		vm := NewVMap[int, string]()
		vm.Set(1, "one")
		vm.Set(2, "two")
		vm.Set(3, "three")

		vm.Delete(2)

		if vm.Get(2) != "" {
			t.Errorf("Delete failed, key 2 still has value: %q", vm.Get(2))
		}
		if vm.Get(1) != "one" || vm.Get(3) != "three" {
			t.Error("Delete affected wrong keys")
		}
	})

	t.Run("delete non-existing key", func(t *testing.T) {
		vm := NewVMap[int, string]()
		vm.Set(1, "one")

		// Should not panic
		vm.Delete(999)

		if vm.Get(1) != "one" {
			t.Error("Delete of non-existing key affected other keys")
		}
	})

	t.Run("delete from empty map", func(t *testing.T) {
		vm := NewVMap[int, string]()
		// Should not panic
		vm.Delete(1)
	})
}

// TestVMapDeleteConcurrent tests Delete under concurrent access.
func TestVMapDeleteConcurrent(t *testing.T) {
	vm := NewVMap[int, string]()
	var wg sync.WaitGroup

	// Add initial data
	for i := 0; i < 1000; i++ {
		vm.Set(i, "value")
	}

	// Concurrent deleters
	for d := 0; d < 5; d++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				vm.Delete(id*200 + i)
			}
		}(d)
	}

	// Concurrent readers
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				_ = vm.Get(i)
			}
		}()
	}

	// Concurrent writers
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				vm.Set(2000+id*100+i, "new")
			}
		}(w)
	}

	wg.Wait()
}

// =============================================================================
// VMap Make / Reset / Len tests (concurrent safety)
// =============================================================================

// TestVMapMakeIsIdempotentAndThreadSafe verifies that Make can be invoked
// concurrently with other operations without triggering a data race or
// destroying data that was already written.
func TestVMapMakeIsIdempotentAndThreadSafe(t *testing.T) {
	vm := NewVMap[int, string]()
	const sentinelKey = -1
	vm.Set(sentinelKey, "alpha")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vm.Make()
		}()
	}

	// Concurrent writers/readers while Make is being spammed. These keys
	// are disjoint from the sentinel so they cannot clobber it.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := id*1000 + j + 1 // strictly positive keys only
				vm.Set(key, "x")
				_ = vm.Get(key)
			}
		}(i)
	}

	wg.Wait()

	// Because Make is idempotent, the sentinel entry must survive.
	if got := vm.Get(sentinelKey); got != "alpha" {
		t.Fatalf("Make dropped existing entry: want %q got %q", "alpha", got)
	}
}

// TestVMapResetReplacesMap verifies Reset actually clears the map and is safe
// under concurrent use.
func TestVMapResetReplacesMap(t *testing.T) {
	vm := NewVMap[int, string]()
	for i := 0; i < 50; i++ {
		vm.Set(i, "v")
	}
	if vm.Len() != 50 {
		t.Fatalf("unexpected pre-reset len: %d", vm.Len())
	}

	vm.Reset()
	if vm.Len() != 0 {
		t.Fatalf("Reset did not clear map, len=%d", vm.Len())
	}

	vm.Set(1, "new")
	if vm.Get(1) != "new" {
		t.Fatalf("Reset broke subsequent Set/Get")
	}
}

// TestVMapZeroValueUsable verifies that a zero-value VMap (no Make) is still
// safely usable - Set must auto-init the internal map.
func TestVMapZeroValueUsable(t *testing.T) {
	var vm VMap[int, string]
	// No Make() called.
	vm.Set(1, "one")
	if vm.Get(1) != "one" {
		t.Fatalf("zero-value VMap not usable after Set")
	}
	if vm.Len() != 1 {
		t.Fatalf("unexpected len %d", vm.Len())
	}
}

// TestVMapRangeSnapshotSemantics verifies that Range now iterates over a
// snapshot - callbacks can mutate the VMap without deadlock.
func TestVMapRangeSnapshotSemantics(t *testing.T) {
	vm := NewVMap[int, int]()
	for i := 0; i < 10; i++ {
		vm.Set(i, i)
	}

	count := 0
	vm.Range(func(key, val int) bool {
		count++
		// Mutate the underlying map from inside the callback - this used
		// to deadlock the RLock variant.
		vm.Set(key+100, val)
		return true
	})

	if count != 10 {
		t.Fatalf("expected to see 10 initial entries, got %d", count)
	}
	// Mutations must have been applied.
	if vm.Len() != 20 {
		t.Fatalf("expected 20 entries after Range mutations, got %d", vm.Len())
	}
}

// TestVMapRangeLockedSemantics verifies the zero-copy variant still works
// for the original callers that don't mutate.
func TestVMapRangeLockedSemantics(t *testing.T) {
	vm := NewVMap[int, int]()
	for i := 0; i < 5; i++ {
		vm.Set(i, i*2)
	}

	seen := map[int]int{}
	vm.RangeLocked(func(k, v int) bool {
		seen[k] = v
		return true
	})
	if len(seen) != 5 {
		t.Fatalf("RangeLocked visited %d entries, want 5", len(seen))
	}
	for k, v := range seen {
		if v != k*2 {
			t.Fatalf("RangeLocked corrupted value for key %d: %d", k, v)
		}
	}
}

// TestVMapDumpUsesReadLock is a smoke test ensuring Dump does not block
// concurrent Dump callers - both must be able to hold the read lock.
func TestVMapDumpUsesReadLock(t *testing.T) {
	vm := NewVMap[int, int]()
	for i := 0; i < 100; i++ {
		vm.Set(i, i)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys, vals := vm.Dump()
			if len(keys) != len(vals) {
				t.Errorf("dump length mismatch")
			}
		}()
	}
	wg.Wait()
}
