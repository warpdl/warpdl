package warplib

import (
	"fmt"
	"sync"
	"testing"
)

// =============================================================================
// Race Condition Tests for QueueManager
// Run with: go test -race -run TestQueueManager ./pkg/warplib/
// =============================================================================

// TestQueueManager_Race_ConcurrentAdd tests that concurrent Add calls are race-free.
func TestQueueManager_Race_ConcurrentAdd(t *testing.T) {
	qm := NewQueueManager(5, nil)

	var wg sync.WaitGroup
	// Spawn 100 goroutines that all call Add
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("hash%d", n), PriorityNormal)
		}(i)
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}

// TestQueueManager_Race_ConcurrentOnComplete tests that concurrent OnComplete calls are race-free.
func TestQueueManager_Race_ConcurrentOnComplete(t *testing.T) {
	qm := NewQueueManager(100, nil)

	// Pre-populate with active items
	for i := 0; i < 100; i++ {
		qm.Add(fmt.Sprintf("hash%d", i), PriorityNormal)
	}

	var wg sync.WaitGroup
	// Spawn goroutines that all call OnComplete
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.OnComplete(fmt.Sprintf("hash%d", n))
		}(i)
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}

// TestQueueManager_Race_AddOnComplete tests mixed Add/OnComplete concurrent access.
func TestQueueManager_Race_AddOnComplete(t *testing.T) {
	qm := NewQueueManager(5, nil)

	var wg sync.WaitGroup
	// Spawn goroutines that Add
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("hash%d", n), PriorityNormal)
		}(i)
	}

	// Spawn goroutines that OnComplete
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.OnComplete(fmt.Sprintf("hash%d", n))
		}(i)
	}

	wg.Wait()
	// No assertion needed - just verify no race
}

// TestQueueManager_Race_ConcurrentMove tests that concurrent Move calls are race-free.
func TestQueueManager_Race_ConcurrentMove(t *testing.T) {
	qm := NewQueueManager(1, nil)

	// Create active item to block queue
	qm.Add("active", PriorityNormal)

	// Add items to waiting queue
	for i := 0; i < 20; i++ {
		qm.Add(fmt.Sprintf("waiting%d", i), PriorityNormal)
	}

	var wg sync.WaitGroup
	// Spawn goroutines that all try to Move
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = qm.Move(fmt.Sprintf("waiting%d", n%20), n%20)
		}(i)
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}

// TestQueueManager_Race_PauseResumeDuringAddOnComplete tests Pause/Resume during Add/OnComplete.
func TestQueueManager_Race_PauseResumeDuringAddOnComplete(t *testing.T) {
	qm := NewQueueManager(5, nil)

	var wg sync.WaitGroup

	// Spawn goroutines that Add
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("hash%d", n), Priority(n%3))
		}(i)
	}

	// Spawn goroutines that OnComplete
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.OnComplete(fmt.Sprintf("hash%d", n))
		}(i)
	}

	// Spawn goroutines that Pause and Resume
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				qm.Pause()
			} else {
				qm.Resume()
			}
		}(i)
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}

// TestQueueManager_Race_AllOperations hammers all operations concurrently.
func TestQueueManager_Race_AllOperations(t *testing.T) {
	qm := NewQueueManager(10, nil)

	var wg sync.WaitGroup

	// Add operations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("item%d", n), Priority(n%3))
		}(i)
	}

	// OnComplete operations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.OnComplete(fmt.Sprintf("item%d", n))
		}(i)
	}

	// Move operations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = qm.Move(fmt.Sprintf("item%d", n), n%20)
		}(i)
	}

	// Pause/Resume operations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				qm.Pause()
			} else {
				qm.Resume()
			}
		}(i)
	}

	// Read operations (GetState, ActiveCount, etc.)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = qm.GetState()
			_ = qm.ActiveCount()
			_ = qm.WaitingCount()
			_ = qm.MaxConcurrent()
			_ = qm.IsPaused()
			_ = qm.GetActiveHashes()
			_ = qm.GetWaitingItems()
		}()
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}

// TestQueueManager_Race_LoadStateConcurrent tests LoadState during other operations.
func TestQueueManager_Race_LoadStateConcurrent(t *testing.T) {
	qm := NewQueueManager(5, nil)

	// Create a state to load
	state := QueueState{
		MaxConcurrent: 10,
		Waiting: []QueuedItemState{
			{Hash: "waiting1", Priority: PriorityHigh},
			{Hash: "waiting2", Priority: PriorityNormal},
		},
		Paused: false,
	}

	var wg sync.WaitGroup

	// Concurrent LoadState calls
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			qm.LoadState(state)
		}()
	}

	// Concurrent Add calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("concurrent%d", n), PriorityNormal)
		}(i)
	}

	// Concurrent GetState calls
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = qm.GetState()
		}()
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}

// TestQueueManager_Race_HighContentionOnStart tests callback execution under contention.
func TestQueueManager_Race_HighContentionOnStart(t *testing.T) {
	var mu sync.Mutex
	startedCount := 0

	onStart := func(hash string) {
		mu.Lock()
		defer mu.Unlock()
		startedCount++
	}

	qm := NewQueueManager(5, onStart)

	var wg sync.WaitGroup

	// Add many items concurrently
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("item%d", n), Priority(n%3))
		}(i)
	}

	// Complete items concurrently (triggers more onStart calls)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.OnComplete(fmt.Sprintf("item%d", n))
		}(i)
	}

	wg.Wait()

	// Verify callback was called (number depends on timing)
	mu.Lock()
	defer mu.Unlock()
	t.Logf("onStart called %d times", startedCount)
	// No assertion on exact count - timing dependent
}

// TestQueueManager_Race_AddDuplicates tests concurrent adds of same hash.
func TestQueueManager_Race_AddDuplicates(t *testing.T) {
	qm := NewQueueManager(5, nil)

	var wg sync.WaitGroup

	// Many goroutines trying to add same hash
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			qm.Add("same-hash", PriorityNormal)
		}()
	}

	wg.Wait()

	// Should only have 1 active item
	if qm.ActiveCount() != 1 {
		t.Errorf("expected 1 active (duplicate detection), got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 0 {
		t.Errorf("expected 0 waiting (duplicate detection), got %d", qm.WaitingCount())
	}
}

// TestQueueManager_Race_MoveWhileAdd tests Move operations while Adds are happening.
func TestQueueManager_Race_MoveWhileAdd(t *testing.T) {
	qm := NewQueueManager(1, nil)

	// Block the queue with one active item
	qm.Add("blocker", PriorityNormal)

	var wg sync.WaitGroup

	// Continuously add items
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("item%d", n), PriorityNormal)
		}(i)
	}

	// Continuously move items
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Move may fail if item doesn't exist yet - that's fine
			_ = qm.Move(fmt.Sprintf("item%d", n%50), n%20)
		}(i)
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}

// TestQueueManager_Race_ReadWhileWrite tests read operations during write operations.
func TestQueueManager_Race_ReadWhileWrite(t *testing.T) {
	qm := NewQueueManager(10, nil)

	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			qm.Add(fmt.Sprintf("write%d", n), Priority(n%3))
			qm.OnComplete(fmt.Sprintf("write%d", n))
		}(i)
	}

	// Reader goroutines
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = qm.ActiveCount()
			_ = qm.WaitingCount()
			_ = qm.MaxConcurrent()
			_ = qm.IsPaused()
			_ = qm.GetState()
			_ = qm.GetActiveHashes()
			_ = qm.GetWaitingItems()
		}()
	}

	wg.Wait()
	// No assertion needed - test passes if no race detected
}
