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
