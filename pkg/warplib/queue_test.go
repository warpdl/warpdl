package warplib

import (
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
