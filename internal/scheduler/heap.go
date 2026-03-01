package scheduler

import "container/heap"

// scheduleHeap implements container/heap.Interface for ScheduleEvent,
// sorted by TriggerAt (earliest first — min-heap).
type scheduleHeap []ScheduleEvent

func (h scheduleHeap) Len() int           { return len(h) }
func (h scheduleHeap) Less(i, j int) bool { return h[i].TriggerAt.Before(h[j].TriggerAt) }
func (h scheduleHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *scheduleHeap) Push(x any) {
	*h = append(*h, x.(ScheduleEvent))
}

func (h *scheduleHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// heapPush adds a ScheduleEvent to the heap, maintaining heap invariant.
func heapPush(h *scheduleHeap, e ScheduleEvent) {
	heap.Push(h, e)
}

// heapPop removes and returns the ScheduleEvent with the earliest TriggerAt.
// Panics if the heap is empty.
func heapPop(h *scheduleHeap) ScheduleEvent {
	return heap.Pop(h).(ScheduleEvent)
}

// heapRemoveByHash removes the first ScheduleEvent with the given ItemHash.
// Returns true if the event was found and removed, false otherwise.
func heapRemoveByHash(h *scheduleHeap, itemHash string) bool {
	for i, e := range *h {
		if e.ItemHash == itemHash {
			heap.Remove(h, i)
			return true
		}
	}
	return false
}
