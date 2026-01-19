package warplib

import (
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
