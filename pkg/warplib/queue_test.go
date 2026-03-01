package warplib

import (
	"fmt"
	"sync"
	"testing"
)

// TestQueueManager_AddWithCapacity tests that QueueManager respects maxConcurrent limit.
// When adding 4 downloads with maxConcurrent=3, expect 3 active and 1 waiting.
func TestQueueManager_AddWithCapacity(t *testing.T) {
	qm := NewQueueManager(3, nil)

	// Add 4 downloads
	for i := 0; i < 4; i++ {
		hash := "hash" + string(rune('0'+i))
		qm.Add(hash, PriorityNormal)
	}

	activeCount := qm.ActiveCount()
	waitingCount := qm.WaitingCount()

	if activeCount != 3 {
		t.Fatalf("expected 3 active downloads, got %d", activeCount)
	}
	if waitingCount != 1 {
		t.Fatalf("expected 1 waiting download, got %d", waitingCount)
	}
}

// TestQueueManager_OnComplete tests that OnComplete triggers auto-start of waiting items.
// When an active download completes, the next waiting item should become active via onStart callback.
func TestQueueManager_OnComplete(t *testing.T) {
	var mu sync.Mutex
	startedHashes := make([]string, 0)

	onStart := func(hash string) {
		mu.Lock()
		defer mu.Unlock()
		startedHashes = append(startedHashes, hash)
	}

	qm := NewQueueManager(2, onStart)

	// Add 3 downloads: first 2 become active, third waits
	qm.Add("hash0", PriorityNormal)
	qm.Add("hash1", PriorityNormal)
	qm.Add("hash2", PriorityNormal)

	// Verify initial state
	if qm.ActiveCount() != 2 {
		t.Fatalf("expected 2 active downloads, got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 1 {
		t.Fatalf("expected 1 waiting download, got %d", qm.WaitingCount())
	}

	// Clear started hashes to only track new starts
	mu.Lock()
	startedHashes = startedHashes[:0]
	mu.Unlock()

	// Complete one active download
	qm.OnComplete("hash0")

	// Verify waiting item was auto-started
	mu.Lock()
	defer mu.Unlock()

	if len(startedHashes) != 1 {
		t.Fatalf("expected onStart called once, got %d calls", len(startedHashes))
	}
	if startedHashes[0] != "hash2" {
		t.Fatalf("expected hash2 to be started, got %s", startedHashes[0])
	}

	// Verify final state: still 2 active (hash1, hash2), 0 waiting
	if qm.ActiveCount() != 2 {
		t.Fatalf("expected 2 active downloads after completion, got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 0 {
		t.Fatalf("expected 0 waiting downloads after completion, got %d", qm.WaitingCount())
	}
}

// TestQueueManager_Priority tests that waiting queue is ordered by priority, not FIFO.
// High priority items should be started before lower priority items, regardless of add order.
func TestQueueManager_Priority(t *testing.T) {
	var mu sync.Mutex
	startedHashes := make([]string, 0)

	onStart := func(hash string) {
		mu.Lock()
		defer mu.Unlock()
		startedHashes = append(startedHashes, hash)
	}

	// maxConcurrent=1 so only first item is active, rest go to waiting
	qm := NewQueueManager(1, onStart)

	// Add first item - becomes active immediately
	qm.Add("first", PriorityNormal)

	// Add items to waiting queue in order: low, normal, high
	qm.Add("low", PriorityLow)
	qm.Add("normal", PriorityNormal)
	qm.Add("high", PriorityHigh)

	// Verify initial state: 1 active (first), 3 waiting
	if qm.ActiveCount() != 1 {
		t.Fatalf("expected 1 active, got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 3 {
		t.Fatalf("expected 3 waiting, got %d", qm.WaitingCount())
	}

	// Clear started hashes to only track new starts
	mu.Lock()
	startedHashes = startedHashes[:0]
	mu.Unlock()

	// Complete the active download to free a slot
	qm.OnComplete("first")

	// Verify HIGH priority item was started (not low which was added first)
	mu.Lock()
	defer mu.Unlock()

	if len(startedHashes) != 1 {
		t.Fatalf("expected onStart called once, got %d calls", len(startedHashes))
	}
	if startedHashes[0] != "high" {
		t.Fatalf("expected high priority item to start first, got %s", startedHashes[0])
	}
}

// TestQueueManager_Pause tests that pause prevents auto-start and resume re-enables it.
// When paused, completing an active download should NOT auto-start waiting items.
func TestQueueManager_Pause(t *testing.T) {
	var mu sync.Mutex
	startedHashes := make([]string, 0)

	onStart := func(hash string) {
		mu.Lock()
		defer mu.Unlock()
		startedHashes = append(startedHashes, hash)
	}

	qm := NewQueueManager(2, onStart)

	// Add 3 items: 2 active, 1 waiting
	qm.Add("hash0", PriorityNormal)
	qm.Add("hash1", PriorityNormal)
	qm.Add("hash2", PriorityNormal)

	// Verify initial state
	if qm.ActiveCount() != 2 {
		t.Fatalf("expected 2 active, got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 1 {
		t.Fatalf("expected 1 waiting, got %d", qm.WaitingCount())
	}

	// Clear started hashes to track only new starts
	mu.Lock()
	startedHashes = startedHashes[:0]
	mu.Unlock()

	// Pause the queue
	qm.Pause()
	if !qm.IsPaused() {
		t.Fatal("expected queue to be paused")
	}

	// Complete one active item
	qm.OnComplete("hash0")

	// Verify NO auto-start happened (paused)
	mu.Lock()
	startCount := len(startedHashes)
	mu.Unlock()

	if startCount != 0 {
		t.Fatalf("expected no auto-start when paused, got %d starts", startCount)
	}

	// State: 1 active (hash1), 1 waiting (hash2)
	if qm.ActiveCount() != 1 {
		t.Fatalf("expected 1 active after completion while paused, got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 1 {
		t.Fatalf("expected 1 waiting (not auto-started), got %d", qm.WaitingCount())
	}

	// Resume the queue
	qm.Resume()
	if qm.IsPaused() {
		t.Fatal("expected queue to be unpaused after Resume")
	}

	// Verify waiting item now started
	mu.Lock()
	startCount = len(startedHashes)
	mu.Unlock()

	if startCount != 1 {
		t.Fatalf("expected 1 auto-start after resume, got %d", startCount)
	}

	// State: 2 active (hash1, hash2), 0 waiting
	if qm.ActiveCount() != 2 {
		t.Fatalf("expected 2 active after resume, got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 0 {
		t.Fatalf("expected 0 waiting after resume, got %d", qm.WaitingCount())
	}
}

// TestQueueManager_PausePersistence tests that pause state is persisted.
func TestQueueManager_PausePersistence(t *testing.T) {
	qm := NewQueueManager(2, nil)

	// Pause and get state
	qm.Pause()
	state := qm.GetState()

	if !state.Paused {
		t.Fatal("expected Paused=true in state")
	}

	// Restore to new queue
	qm2 := NewQueueManager(2, nil)
	qm2.LoadState(state)

	if !qm2.IsPaused() {
		t.Fatal("expected queue to be paused after LoadState")
	}
}

// TestQueueManager_Move tests the Move method for reordering waiting queue.
func TestQueueManager_Move(t *testing.T) {
	t.Run("MoveToFront", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		// Add items: first becomes active, rest go to waiting
		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		err := qm.Move("c", 0)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// waiting should be: [c, a, b]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		if len(qm.waiting) != 3 {
			t.Fatalf("expected 3 waiting, got %d", len(qm.waiting))
		}
		if qm.waiting[0].hash != "c" {
			t.Errorf("expected c at position 0, got %s", qm.waiting[0].hash)
		}
		if qm.waiting[1].hash != "a" {
			t.Errorf("expected a at position 1, got %s", qm.waiting[1].hash)
		}
		if qm.waiting[2].hash != "b" {
			t.Errorf("expected b at position 2, got %s", qm.waiting[2].hash)
		}
	})

	t.Run("MoveToEnd", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		err := qm.Move("a", 2)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// waiting should be: [b, c, a]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		if qm.waiting[0].hash != "b" {
			t.Errorf("expected b at position 0, got %s", qm.waiting[0].hash)
		}
		if qm.waiting[1].hash != "c" {
			t.Errorf("expected c at position 1, got %s", qm.waiting[1].hash)
		}
		if qm.waiting[2].hash != "a" {
			t.Errorf("expected a at position 2, got %s", qm.waiting[2].hash)
		}
	})

	t.Run("MoveToMiddle", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		qm.Add("d", PriorityNormal)
		// waiting: [a, b, c, d]

		err := qm.Move("d", 1)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// waiting should be: [a, d, b, c]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		expected := []string{"a", "d", "b", "c"}
		for i, exp := range expected {
			if qm.waiting[i].hash != exp {
				t.Errorf("expected %s at position %d, got %s", exp, i, qm.waiting[i].hash)
			}
		}
	})

	t.Run("MoveInvalidHash", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)

		err := qm.Move("nonexistent", 0)
		if err != ErrQueueHashNotFound {
			t.Fatalf("expected ErrQueueHashNotFound, got %v", err)
		}
	})

	t.Run("MoveActiveHash", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)

		err := qm.Move("active", 0)
		if err != ErrCannotMoveActive {
			t.Fatalf("expected ErrCannotMoveActive, got %v", err)
		}
	})

	t.Run("PositionClampNegative", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		err := qm.Move("c", -5) // negative should clamp to 0
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// waiting should be: [c, a, b]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		if qm.waiting[0].hash != "c" {
			t.Errorf("expected c at position 0 after negative clamp, got %s", qm.waiting[0].hash)
		}
	})

	t.Run("PositionClampTooLarge", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		err := qm.Move("a", 100) // too large should clamp to end
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// waiting should be: [b, c, a]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		if qm.waiting[2].hash != "a" {
			t.Errorf("expected a at position 2 after large clamp, got %s", qm.waiting[2].hash)
		}
	})

	t.Run("MoveSamePosition", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		err := qm.Move("b", 1) // Move to same position should be no-op
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// waiting should remain: [a, b, c]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		expected := []string{"a", "b", "c"}
		for i, exp := range expected {
			if qm.waiting[i].hash != exp {
				t.Errorf("expected %s at position %d, got %s", exp, i, qm.waiting[i].hash)
			}
		}
	})
}

// TestQueueManager_StatePersistence tests that queue state can be saved and restored.
// Waiting items should survive GetState/LoadState cycle. Active items are not persisted.
func TestQueueManager_StatePersistence(t *testing.T) {
	// Create queue with items: 2 active, 3 waiting
	qm := NewQueueManager(2, nil)

	qm.Add("hash0", PriorityNormal) // active
	qm.Add("hash1", PriorityNormal) // active
	qm.Add("hash2", PriorityLow)    // waiting
	qm.Add("hash3", PriorityHigh)   // waiting (should be first in queue due to priority)
	qm.Add("hash4", PriorityNormal) // waiting

	// Verify initial state
	if qm.ActiveCount() != 2 {
		t.Fatalf("expected 2 active, got %d", qm.ActiveCount())
	}
	if qm.WaitingCount() != 3 {
		t.Fatalf("expected 3 waiting, got %d", qm.WaitingCount())
	}

	// Save state
	state := qm.GetState()

	// Verify state has correct values
	if state.MaxConcurrent != 2 {
		t.Fatalf("expected MaxConcurrent=2, got %d", state.MaxConcurrent)
	}
	if len(state.Waiting) != 3 {
		t.Fatalf("expected 3 waiting in state, got %d", len(state.Waiting))
	}

	// Create new queue and restore state
	var startedHashes []string
	onStart := func(hash string) {
		startedHashes = append(startedHashes, hash)
	}
	qm2 := NewQueueManager(0, onStart) // start with 0, will be overwritten

	qm2.LoadState(state)

	// Verify restored state
	if qm2.MaxConcurrent() != 2 {
		t.Fatalf("expected restored MaxConcurrent=2, got %d", qm2.MaxConcurrent())
	}
	if qm2.WaitingCount() != 3 {
		t.Fatalf("expected restored waiting=3, got %d", qm2.WaitingCount())
	}
	// Active is 0 after restore (active items not persisted)
	if qm2.ActiveCount() != 0 {
		t.Fatalf("expected restored active=0, got %d", qm2.ActiveCount())
	}

	// Verify priority order maintained: high (hash3), normal (hash4), low (hash2)
	// Simulate one slot becoming available by adding and completing a dummy active
	qm2.mu.Lock()
	qm2.active["dummy"] = struct{}{}
	qm2.mu.Unlock()
	qm2.OnComplete("dummy")

	// hash3 (high) should have started
	if len(startedHashes) != 1 || startedHashes[0] != "hash3" {
		t.Fatalf("expected hash3 (high priority) started first, got %v", startedHashes)
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestQueueManager_EmptyQueue tests operations on an empty queue.
func TestQueueManager_EmptyQueue(t *testing.T) {
	t.Run("OnCompleteEmptyQueue", func(t *testing.T) {
		qm := NewQueueManager(3, nil)

		// Should not panic when calling OnComplete on empty queue
		qm.OnComplete("nonexistent")

		if qm.ActiveCount() != 0 {
			t.Fatalf("expected 0 active, got %d", qm.ActiveCount())
		}
		if qm.WaitingCount() != 0 {
			t.Fatalf("expected 0 waiting, got %d", qm.WaitingCount())
		}
	})

	t.Run("ResumeEmptyQueue", func(t *testing.T) {
		startCalled := false
		onStart := func(hash string) {
			startCalled = true
		}

		qm := NewQueueManager(3, onStart)
		qm.Pause()
		qm.Resume()

		if startCalled {
			t.Fatal("expected onStart not called on empty queue resume")
		}
		if qm.ActiveCount() != 0 {
			t.Fatalf("expected 0 active, got %d", qm.ActiveCount())
		}
	})

	t.Run("MoveEmptyQueue", func(t *testing.T) {
		qm := NewQueueManager(3, nil)

		err := qm.Move("nonexistent", 0)
		if err != ErrQueueHashNotFound {
			t.Fatalf("expected ErrQueueHashNotFound, got %v", err)
		}
	})

	t.Run("GetStateEmptyQueue", func(t *testing.T) {
		qm := NewQueueManager(3, nil)

		state := qm.GetState()
		if state.MaxConcurrent != 3 {
			t.Fatalf("expected MaxConcurrent=3, got %d", state.MaxConcurrent)
		}
		if len(state.Waiting) != 0 {
			t.Fatalf("expected empty waiting, got %d items", len(state.Waiting))
		}
		if state.Paused {
			t.Fatal("expected not paused")
		}
	})

	t.Run("GetActiveHashesEmptyQueue", func(t *testing.T) {
		qm := NewQueueManager(3, nil)

		hashes := qm.GetActiveHashes()
		if len(hashes) != 0 {
			t.Fatalf("expected empty hashes, got %v", hashes)
		}
	})

	t.Run("GetWaitingItemsEmptyQueue", func(t *testing.T) {
		qm := NewQueueManager(3, nil)

		items := qm.GetWaitingItems()
		if len(items) != 0 {
			t.Fatalf("expected empty items, got %v", items)
		}
	})
}

// TestQueueManager_SingleItem tests single item edge cases.
func TestQueueManager_SingleItem(t *testing.T) {
	t.Run("SingleItemMaxConcurrent1", func(t *testing.T) {
		startCalled := false
		startedHash := ""
		onStart := func(hash string) {
			startCalled = true
			startedHash = hash
		}

		qm := NewQueueManager(1, onStart)
		qm.Add("single", PriorityNormal)

		// Single item with maxConcurrent=1 should become active immediately
		if !startCalled {
			t.Fatal("expected onStart called for single item")
		}
		if startedHash != "single" {
			t.Fatalf("expected 'single' started, got %s", startedHash)
		}
		if qm.ActiveCount() != 1 {
			t.Fatalf("expected 1 active, got %d", qm.ActiveCount())
		}
		if qm.WaitingCount() != 0 {
			t.Fatalf("expected 0 waiting, got %d", qm.WaitingCount())
		}
	})

	t.Run("TwoItemsMaxConcurrent1", func(t *testing.T) {
		var startedHashes []string
		onStart := func(hash string) {
			startedHashes = append(startedHashes, hash)
		}

		qm := NewQueueManager(1, onStart)
		qm.Add("first", PriorityNormal)
		qm.Add("second", PriorityNormal)

		// First becomes active, second waits
		if qm.ActiveCount() != 1 {
			t.Fatalf("expected 1 active, got %d", qm.ActiveCount())
		}
		if qm.WaitingCount() != 1 {
			t.Fatalf("expected 1 waiting, got %d", qm.WaitingCount())
		}
		if len(startedHashes) != 1 || startedHashes[0] != "first" {
			t.Fatalf("expected only 'first' started, got %v", startedHashes)
		}
	})

	t.Run("OnCompleteWithSingleWaiting", func(t *testing.T) {
		var startedHashes []string
		onStart := func(hash string) {
			startedHashes = append(startedHashes, hash)
		}

		qm := NewQueueManager(1, onStart)
		qm.Add("first", PriorityNormal)
		qm.Add("second", PriorityNormal)

		// Clear to track only new starts
		startedHashes = startedHashes[:0]

		// Complete first, second should auto-start
		qm.OnComplete("first")

		if len(startedHashes) != 1 || startedHashes[0] != "second" {
			t.Fatalf("expected 'second' auto-started, got %v", startedHashes)
		}
		if qm.ActiveCount() != 1 {
			t.Fatalf("expected 1 active, got %d", qm.ActiveCount())
		}
		if qm.WaitingCount() != 0 {
			t.Fatalf("expected 0 waiting, got %d", qm.WaitingCount())
		}
	})
}

// TestQueueManager_MaxConcurrentOne tests strict serialization with maxConcurrent=1.
func TestQueueManager_MaxConcurrentOne(t *testing.T) {
	t.Run("StrictSerialization", func(t *testing.T) {
		var startOrder []string
		onStart := func(hash string) {
			startOrder = append(startOrder, hash)
		}

		qm := NewQueueManager(1, onStart)

		// Add 5 items, all same priority (FIFO within priority)
		for i := 0; i < 5; i++ {
			qm.Add(string(rune('a'+i)), PriorityNormal)
		}

		// Only first should be active
		if qm.ActiveCount() != 1 {
			t.Fatalf("expected 1 active, got %d", qm.ActiveCount())
		}
		if qm.WaitingCount() != 4 {
			t.Fatalf("expected 4 waiting, got %d", qm.WaitingCount())
		}

		// Complete each one, verify serial execution
		for i := 0; i < 5; i++ {
			if qm.ActiveCount() != 1 && i < 4 {
				t.Fatalf("at step %d: expected 1 active, got %d", i, qm.ActiveCount())
			}
			qm.OnComplete(string(rune('a' + i)))
		}

		// All should have started in order a, b, c, d, e
		expectedOrder := []string{"a", "b", "c", "d", "e"}
		if len(startOrder) != 5 {
			t.Fatalf("expected 5 starts, got %d", len(startOrder))
		}
		for i, exp := range expectedOrder {
			if startOrder[i] != exp {
				t.Errorf("expected start[%d]=%s, got %s", i, exp, startOrder[i])
			}
		}
	})

	t.Run("PriorityOrderingSerialExecution", func(t *testing.T) {
		var startOrder []string
		onStart := func(hash string) {
			startOrder = append(startOrder, hash)
		}

		qm := NewQueueManager(1, onStart)

		// First item becomes active
		qm.Add("blocker", PriorityNormal)

		// Add items with different priorities
		qm.Add("low1", PriorityLow)
		qm.Add("normal1", PriorityNormal)
		qm.Add("high1", PriorityHigh)
		qm.Add("low2", PriorityLow)
		qm.Add("high2", PriorityHigh)

		// Clear start order to track dequeue order
		startOrder = startOrder[:0]

		// Complete blocker and all waiting items
		qm.OnComplete("blocker")
		qm.OnComplete("high1")
		qm.OnComplete("high2")
		qm.OnComplete("normal1")
		qm.OnComplete("low1")
		qm.OnComplete("low2")

		// Expected order: high1, high2, normal1, low1, low2 (priority then FIFO)
		expectedOrder := []string{"high1", "high2", "normal1", "low1", "low2"}
		if len(startOrder) != 5 {
			t.Fatalf("expected 5 starts, got %d: %v", len(startOrder), startOrder)
		}
		for i, exp := range expectedOrder {
			if startOrder[i] != exp {
				t.Errorf("expected start[%d]=%s, got %s", i, exp, startOrder[i])
			}
		}
	})
}

// TestQueueManager_BoundaryPositions tests extreme position values.
func TestQueueManager_BoundaryPositions(t *testing.T) {
	t.Run("MoveToNegative100", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		err := qm.Move("c", -100)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// Should clamp to 0, waiting: [c, a, b]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		expected := []string{"c", "a", "b"}
		for i, exp := range expected {
			if qm.waiting[i].hash != exp {
				t.Errorf("expected %s at position %d, got %s", exp, i, qm.waiting[i].hash)
			}
		}
	})

	t.Run("MoveToPosition1000000", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		err := qm.Move("a", 1000000)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// Should clamp to end (2), waiting: [b, c, a]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		expected := []string{"b", "c", "a"}
		for i, exp := range expected {
			if qm.waiting[i].hash != exp {
				t.Errorf("expected %s at position %d, got %s", exp, i, qm.waiting[i].hash)
			}
		}
	})

	t.Run("MoveToSamePositionNoOp", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		qm.Add("c", PriorityNormal)
		// waiting: [a, b, c]

		// Move b to position 1 (same position)
		err := qm.Move("b", 1)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		// Should remain: [a, b, c]
		qm.mu.Lock()
		defer qm.mu.Unlock()
		expected := []string{"a", "b", "c"}
		for i, exp := range expected {
			if qm.waiting[i].hash != exp {
				t.Errorf("expected %s at position %d, got %s", exp, i, qm.waiting[i].hash)
			}
		}
	})

	t.Run("MoveFirstItemToZero", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("a", PriorityNormal)
		qm.Add("b", PriorityNormal)
		// waiting: [a, b]

		// Move first item to position 0 (no-op)
		err := qm.Move("a", 0)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}

		qm.mu.Lock()
		defer qm.mu.Unlock()
		expected := []string{"a", "b"}
		for i, exp := range expected {
			if qm.waiting[i].hash != exp {
				t.Errorf("expected %s at position %d, got %s", exp, i, qm.waiting[i].hash)
			}
		}
	})
}

// TestQueueManager_IdempotentOperations tests that repeated operations are safe.
func TestQueueManager_IdempotentOperations(t *testing.T) {
	t.Run("PauseTwice", func(t *testing.T) {
		qm := NewQueueManager(3, nil)

		qm.Pause()
		if !qm.IsPaused() {
			t.Fatal("expected paused after first Pause()")
		}

		qm.Pause()
		if !qm.IsPaused() {
			t.Fatal("expected still paused after second Pause()")
		}
	})

	t.Run("ResumeTwice", func(t *testing.T) {
		var startCount int
		onStart := func(hash string) {
			startCount++
		}

		qm := NewQueueManager(2, onStart)
		qm.Add("hash0", PriorityNormal)
		qm.Add("hash1", PriorityNormal)
		qm.Add("hash2", PriorityNormal) // waiting

		qm.Pause()

		// Complete one to free a slot while paused
		qm.OnComplete("hash0")

		// Reset start count
		startCount = 0

		// First resume should start the waiting item (now there's capacity)
		qm.Resume()
		if qm.IsPaused() {
			t.Fatal("expected not paused after first Resume()")
		}
		if startCount != 1 {
			t.Fatalf("expected 1 start after first resume, got %d", startCount)
		}

		// Second resume should be no-op (nothing waiting)
		qm.Resume()
		if qm.IsPaused() {
			t.Fatal("expected still not paused after second Resume()")
		}
		if startCount != 1 {
			t.Fatalf("expected still 1 start after second resume, got %d", startCount)
		}
	})

	t.Run("AddSameHashTwice", func(t *testing.T) {
		var startCount int
		onStart := func(hash string) {
			startCount++
		}

		qm := NewQueueManager(3, onStart)

		qm.Add("same", PriorityNormal)
		if startCount != 1 {
			t.Fatalf("expected 1 start after first Add, got %d", startCount)
		}

		// Second Add with same hash should be ignored
		qm.Add("same", PriorityNormal)
		if startCount != 1 {
			t.Fatalf("expected still 1 start after duplicate Add, got %d", startCount)
		}
		if qm.ActiveCount() != 1 {
			t.Fatalf("expected 1 active, got %d", qm.ActiveCount())
		}
	})

	t.Run("AddSameHashTwiceInWaiting", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		qm.Add("waiting1", PriorityNormal)
		qm.Add("waiting1", PriorityNormal) // duplicate - should be ignored

		if qm.WaitingCount() != 1 {
			t.Fatalf("expected 1 waiting, got %d", qm.WaitingCount())
		}
	})

	t.Run("OnCompleteNonexistent", func(t *testing.T) {
		qm := NewQueueManager(3, nil)
		qm.Add("hash0", PriorityNormal)

		// Should not panic or cause issues
		qm.OnComplete("nonexistent")

		if qm.ActiveCount() != 1 {
			t.Fatalf("expected 1 active, got %d", qm.ActiveCount())
		}
	})

	t.Run("OnCompleteTwice", func(t *testing.T) {
		var startCount int
		onStart := func(hash string) {
			startCount++
		}

		qm := NewQueueManager(1, onStart)
		qm.Add("first", PriorityNormal)
		qm.Add("second", PriorityNormal)

		startCount = 0

		// First complete should start waiting item
		qm.OnComplete("first")
		if startCount != 1 {
			t.Fatalf("expected 1 start after first complete, got %d", startCount)
		}

		// Second complete of same hash should be no-op
		qm.OnComplete("first")
		if startCount != 1 {
			t.Fatalf("expected still 1 start after duplicate complete, got %d", startCount)
		}
	})
}

// TestQueueManager_ConcurrentModifications tests concurrent access safety.
func TestQueueManager_ConcurrentModifications(t *testing.T) {
	t.Run("ConcurrentAddAndComplete", func(t *testing.T) {
		qm := NewQueueManager(5, nil)
		var wg sync.WaitGroup

		// Concurrent adds
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				hash := "hash" + string(rune('A'+id%26)) + string(rune('0'+id%10))
				qm.Add(hash, Priority(id%3))
			}(i)
		}

		// Concurrent completes
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				hash := "hash" + string(rune('A'+id%26)) + string(rune('0'+id%10))
				qm.OnComplete(hash)
			}(i)
		}

		wg.Wait()

		// Should not panic, state should be consistent
		active := qm.ActiveCount()
		waiting := qm.WaitingCount()
		t.Logf("After concurrent ops: active=%d, waiting=%d", active, waiting)
	})

	t.Run("ConcurrentPauseResume", func(t *testing.T) {
		qm := NewQueueManager(3, nil)
		var wg sync.WaitGroup

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				if id%2 == 0 {
					qm.Pause()
				} else {
					qm.Resume()
				}
			}(i)
		}

		wg.Wait()

		// Should not panic, final state is either paused or not
		_ = qm.IsPaused()
	})

	t.Run("ConcurrentMoves", func(t *testing.T) {
		qm := NewQueueManager(1, nil)

		qm.Add("active", PriorityNormal)
		for i := 0; i < 10; i++ {
			qm.Add("item"+string(rune('0'+i)), PriorityNormal)
		}

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				hash := "item" + string(rune('0'+id%10))
				_ = qm.Move(hash, id%10)
			}(i)
		}

		wg.Wait()

		// Should not panic, waiting count should be 10
		if qm.WaitingCount() != 10 {
			t.Fatalf("expected 10 waiting, got %d", qm.WaitingCount())
		}
	})

	t.Run("ConcurrentGetState", func(t *testing.T) {
		qm := NewQueueManager(3, nil)

		// Add some items
		for i := 0; i < 10; i++ {
			qm.Add("hash"+string(rune('0'+i)), PriorityNormal)
		}

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = qm.GetState()
				_ = qm.GetActiveHashes()
				_ = qm.GetWaitingItems()
			}()
		}

		// Concurrent modifications
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				qm.Add("new"+string(rune('A'+id%26)), Priority(id%3))
			}(i)
		}

		wg.Wait()
	})
}

// TestQueueManager_SimultaneousScheduleTriggers verifies T075b (FR-010):
// When N > maxConcurrent schedule triggers fire simultaneously, at most
// maxConcurrent downloads are active and the rest are queued.
func TestQueueManager_SimultaneousScheduleTriggers(t *testing.T) {
	const maxConcurrent = 3
	const totalTriggers = 7

	qm := NewQueueManager(maxConcurrent, nil)

	// Simulate N simultaneous schedule triggers
	var wg sync.WaitGroup
	for i := 0; i < totalTriggers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			hash := fmt.Sprintf("scheduled-%d", id)
			qm.Add(hash, PriorityNormal)
		}(i)
	}
	wg.Wait()

	active := qm.ActiveCount()
	waiting := qm.WaitingCount()

	if active > maxConcurrent {
		t.Errorf("active count %d exceeds maxConcurrent %d (FR-010)", active, maxConcurrent)
	}
	if active+waiting != totalTriggers {
		t.Errorf("active(%d) + waiting(%d) != totalTriggers(%d)", active, waiting, totalTriggers)
	}
	if waiting != totalTriggers-maxConcurrent {
		t.Errorf("expected %d waiting, got %d", totalTriggers-maxConcurrent, waiting)
	}
}
