package scheduler

import (
	"testing"
	"time"
)

func TestHeapPushPopOrdering(t *testing.T) {
	h := &scheduleHeap{}

	t1 := time.Now().Add(3 * time.Hour)
	t2 := time.Now().Add(1 * time.Hour)
	t3 := time.Now().Add(2 * time.Hour)

	heapPush(h, ScheduleEvent{ItemHash: "hash3", TriggerAt: t1})
	heapPush(h, ScheduleEvent{ItemHash: "hash1", TriggerAt: t2})
	heapPush(h, ScheduleEvent{ItemHash: "hash2", TriggerAt: t3})

	// Pop should return in ascending TriggerAt order (min-heap)
	first := heapPop(h)
	if first.ItemHash != "hash1" {
		t.Errorf("expected hash1 (earliest), got %s", first.ItemHash)
	}
	second := heapPop(h)
	if second.ItemHash != "hash2" {
		t.Errorf("expected hash2 (middle), got %s", second.ItemHash)
	}
	third := heapPop(h)
	if third.ItemHash != "hash3" {
		t.Errorf("expected hash3 (latest), got %s", third.ItemHash)
	}
}

func TestHeapEmpty(t *testing.T) {
	h := &scheduleHeap{}
	if h.Len() != 0 {
		t.Errorf("expected empty heap, got len %d", h.Len())
	}
}

func TestHeapDuplicateTriggerTimes(t *testing.T) {
	h := &scheduleHeap{}
	sameTime := time.Now().Add(1 * time.Hour)

	heapPush(h, ScheduleEvent{ItemHash: "hashA", TriggerAt: sameTime})
	heapPush(h, ScheduleEvent{ItemHash: "hashB", TriggerAt: sameTime})
	heapPush(h, ScheduleEvent{ItemHash: "hashC", TriggerAt: sameTime})

	if h.Len() != 3 {
		t.Fatalf("expected 3 items, got %d", h.Len())
	}

	// All three should be popped without error (any order is valid for equal times)
	seen := map[string]bool{}
	for h.Len() > 0 {
		e := heapPop(h)
		if seen[e.ItemHash] {
			t.Errorf("duplicate pop for %s", e.ItemHash)
		}
		seen[e.ItemHash] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct items, got %d", len(seen))
	}
}

func TestHeapRemoveByHash(t *testing.T) {
	h := &scheduleHeap{}

	t1 := time.Now().Add(1 * time.Hour)
	t2 := time.Now().Add(2 * time.Hour)
	t3 := time.Now().Add(3 * time.Hour)

	heapPush(h, ScheduleEvent{ItemHash: "hashA", TriggerAt: t1})
	heapPush(h, ScheduleEvent{ItemHash: "hashB", TriggerAt: t2})
	heapPush(h, ScheduleEvent{ItemHash: "hashC", TriggerAt: t3})

	// Remove the middle element
	removed := heapRemoveByHash(h, "hashB")
	if !removed {
		t.Error("expected removal to succeed")
	}
	if h.Len() != 2 {
		t.Errorf("expected 2 items after removal, got %d", h.Len())
	}

	// Pop should return hashA then hashC
	first := heapPop(h)
	if first.ItemHash != "hashA" {
		t.Errorf("expected hashA, got %s", first.ItemHash)
	}
	second := heapPop(h)
	if second.ItemHash != "hashC" {
		t.Errorf("expected hashC, got %s", second.ItemHash)
	}
}

func TestHeapRemoveByHashNotFound(t *testing.T) {
	h := &scheduleHeap{}
	heapPush(h, ScheduleEvent{ItemHash: "hashA", TriggerAt: time.Now()})

	removed := heapRemoveByHash(h, "nonexistent")
	if removed {
		t.Error("expected removal to fail for nonexistent hash")
	}
	if h.Len() != 1 {
		t.Errorf("expected 1 item to remain, got %d", h.Len())
	}
}

func TestHeapRemoveFirst(t *testing.T) {
	h := &scheduleHeap{}
	heapPush(h, ScheduleEvent{ItemHash: "only", TriggerAt: time.Now()})

	removed := heapRemoveByHash(h, "only")
	if !removed {
		t.Error("expected removal to succeed")
	}
	if h.Len() != 0 {
		t.Errorf("expected empty heap after removal, got %d", h.Len())
	}
}
