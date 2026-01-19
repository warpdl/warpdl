package warplib

import (
	"sync"
)

// Priority represents the priority level for queued downloads.
type Priority int

const (
	// PriorityLow is the lowest priority for downloads.
	PriorityLow Priority = iota
	// PriorityNormal is the default priority for downloads.
	PriorityNormal
	// PriorityHigh is the highest priority for downloads.
	PriorityHigh
)

// queuedItem represents a download waiting in the queue.
type queuedItem struct {
	hash     string
	priority Priority
}

// QueuedItemState is the exported version of queuedItem for GOB persistence.
type QueuedItemState struct {
	Hash     string
	Priority Priority
}

// QueueState holds the persistent state of the queue.
// Active items are not persisted (they'll be re-queued on restart).
type QueueState struct {
	MaxConcurrent int
	Waiting       []QueuedItemState
	Paused        bool
}

// QueueManager manages concurrent download limits.
// Downloads beyond maxConcurrent are queued and started when slots free up.
type QueueManager struct {
	maxConcurrent int
	active        map[string]struct{}
	waiting       []queuedItem
	onStart       func(hash string)
	paused        bool
	mu            sync.Mutex
}

// NewQueueManager creates a new QueueManager with the given concurrency limit.
// onStart is called when a download is activated (can be nil).
func NewQueueManager(maxConcurrent int, onStart func(hash string)) *QueueManager {
	return &QueueManager{
		maxConcurrent: maxConcurrent,
		active:        make(map[string]struct{}),
		waiting:       make([]queuedItem, 0),
		onStart:       onStart,
	}
}

// Add adds a download to the queue. If under capacity, it becomes active immediately.
// Otherwise, it's queued based on priority.
func (qm *QueueManager) Add(hash string, priority Priority) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Check if already active or queued
	if _, exists := qm.active[hash]; exists {
		return
	}
	for _, item := range qm.waiting {
		if item.hash == hash {
			return
		}
	}

	// If under capacity, activate immediately
	if len(qm.active) < qm.maxConcurrent {
		qm.active[hash] = struct{}{}
		if qm.onStart != nil {
			qm.onStart(hash)
		}
		return
	}

	// Queue the download based on priority (higher priority inserted before lower)
	// Find insertion point: insert before first item with lower priority
	insertIdx := len(qm.waiting) // Default: append at end
	for i, item := range qm.waiting {
		if item.priority < priority {
			insertIdx = i
			break
		}
	}

	// Insert at the correct position
	newItem := queuedItem{hash: hash, priority: priority}
	qm.waiting = append(qm.waiting, queuedItem{}) // Grow slice
	copy(qm.waiting[insertIdx+1:], qm.waiting[insertIdx:])
	qm.waiting[insertIdx] = newItem
}

// ActiveCount returns the number of currently active downloads.
func (qm *QueueManager) ActiveCount() int {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return len(qm.active)
}

// WaitingCount returns the number of downloads waiting in the queue.
func (qm *QueueManager) WaitingCount() int {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return len(qm.waiting)
}

// OnComplete marks a download as complete and starts the next waiting download if available.
func (qm *QueueManager) OnComplete(hash string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Remove from active
	delete(qm.active, hash)

	// If paused, don't auto-start
	if qm.paused {
		return
	}

	// If there are waiting items and we have capacity, start the next one
	if len(qm.waiting) > 0 && len(qm.active) < qm.maxConcurrent {
		// Pop first item from waiting queue
		next := qm.waiting[0]
		qm.waiting = qm.waiting[1:]

		// Add to active
		qm.active[next.hash] = struct{}{}

		// Call onStart callback
		if qm.onStart != nil {
			qm.onStart(next.hash)
		}
	}
}

// MaxConcurrent returns the maximum number of concurrent downloads.
func (qm *QueueManager) MaxConcurrent() int {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.maxConcurrent
}

// GetState returns the current queue state for persistence.
// Active items are not included (they'll be re-queued on restart).
func (qm *QueueManager) GetState() QueueState {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	waiting := make([]QueuedItemState, len(qm.waiting))
	for i, item := range qm.waiting {
		waiting[i] = QueuedItemState{
			Hash:     item.hash,
			Priority: item.priority,
		}
	}

	return QueueState{
		MaxConcurrent: qm.maxConcurrent,
		Waiting:       waiting,
		Paused:        qm.paused,
	}
}

// LoadState restores queue state from persistence.
// Active items are reset to empty (previously active items should be re-queued).
func (qm *QueueManager) LoadState(state QueueState) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.maxConcurrent = state.MaxConcurrent
	qm.active = make(map[string]struct{})
	qm.waiting = make([]queuedItem, len(state.Waiting))
	qm.paused = state.Paused

	for i, item := range state.Waiting {
		qm.waiting[i] = queuedItem{
			hash:     item.Hash,
			priority: item.Priority,
		}
	}
}

// Pause pauses the queue, preventing auto-start of waiting items.
func (qm *QueueManager) Pause() {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.paused = true
}

// Resume resumes the queue, enabling auto-start and starting waiting items up to capacity.
func (qm *QueueManager) Resume() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	qm.paused = false

	// Start waiting items up to capacity
	for len(qm.waiting) > 0 && len(qm.active) < qm.maxConcurrent {
		// Pop first item from waiting queue
		next := qm.waiting[0]
		qm.waiting = qm.waiting[1:]

		// Add to active
		qm.active[next.hash] = struct{}{}

		// Call onStart callback
		if qm.onStart != nil {
			qm.onStart(next.hash)
		}
	}
}

// IsPaused returns whether the queue is paused.
func (qm *QueueManager) IsPaused() bool {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.paused
}
