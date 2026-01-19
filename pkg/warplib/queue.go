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

// QueueManager manages concurrent download limits.
// Downloads beyond maxConcurrent are queued and started when slots free up.
type QueueManager struct {
    maxConcurrent int
    active        map[string]struct{}
    waiting       []queuedItem
    onStart       func(hash string)
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

    // Queue the download
    qm.waiting = append(qm.waiting, queuedItem{
        hash:     hash,
        priority: priority,
    })
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
