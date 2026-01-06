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
