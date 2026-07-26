package warplib

import (
	"context"
	"sort"
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

// queueActivation is a unique token for one transition of a hash into the
// active set. Pointer identity deliberately forms part of the token: removing
// and re-adding the same hash must not let an older reserved callback pass an
// active-membership check (the active-only ABA problem).
type queueActivation struct {
	generation uint64
	claimed    bool
}

// QueueActivation is an opaque lease for one exact transition of a hash into
// the active queue set. Removing and re-adding the same hash produces a new
// lease, so an older slow callback cannot launch or release the replacement.
type QueueActivation struct {
	queue      *QueueManager
	hash       string
	activation *queueActivation
}

// Hash returns the download identifier associated with the activation.
func (a QueueActivation) Hash() string {
	return a.hash
}

// QueuedItemState is the exported version of queuedItem for GOB persistence.
type QueuedItemState struct {
	Hash     string
	Priority Priority
}

// QueueState holds the persistent state of the queue.
// Active items are persisted separately and converted back to waiting items
// during LoadState. A process restart must never silently drop queue slots
// that were active when the previous daemon exited.
type QueueState struct {
	MaxConcurrent int
	Active        []QueuedItemState
	Waiting       []QueuedItemState
	Paused        bool
}

// QueueManager manages concurrent download limits.
// Downloads beyond maxConcurrent are queued and started when slots free up.
type QueueManager struct {
	maxConcurrent int
	active        map[string]*queueActivation
	// activePriorities preserves the priority of active items so they can
	// be returned to the waiting queue after a daemon restart.
	activePriorities map[string]Priority
	// pendingStarts contains activations whose onStart callback was reserved
	// but has not yet crossed its start linearization point. Remove and Pause
	// invalidate these tokens before returning.
	pendingStarts map[string]*queueActivation
	waiting       []queuedItem
	onStart       func(QueueActivation)
	// onChange is called after every successful mutation, outside qm.mu.
	// Manager uses it to persist queue state without coupling QueueManager
	// to the persistence implementation.
	onChange func()
	paused   bool
	quiesced bool

	// runningStarts counts callbacks that validated their activation token.
	// Validation is the callback's start linearization point. Quiesce waits
	// for those callbacks to return; ordinary Remove/Pause operations must not
	// wait because onStart may synchronously call back into the queue.
	runningStarts  int
	nextGeneration uint64
	startCond      *sync.Cond
	mu             sync.Mutex
}

// NewQueueManager creates a new QueueManager with the given concurrency limit.
// onStart is called when a download is activated (can be nil).
func NewQueueManager(maxConcurrent int, onStart func(hash string)) *QueueManager {
	var activationStart func(QueueActivation)
	if onStart != nil {
		activationStart = func(activation QueueActivation) {
			onStart(activation.Hash())
		}
	}
	return newQueueManagerWithActivation(maxConcurrent, activationStart)
}

func newQueueManagerWithActivation(maxConcurrent int, onStart func(QueueActivation)) *QueueManager {
	qm := &QueueManager{
		maxConcurrent:    maxConcurrent,
		active:           make(map[string]*queueActivation),
		activePriorities: make(map[string]Priority),
		pendingStarts:    make(map[string]*queueActivation),
		waiting:          make([]queuedItem, 0),
		onStart:          onStart,
	}
	qm.startCond = sync.NewCond(&qm.mu)
	return qm
}

// SetOnChange installs a callback invoked after queue state changes. The
// callback is always invoked without holding the queue lock.
func (qm *QueueManager) SetOnChange(onChange func()) {
	qm.mu.Lock()
	qm.onChange = onChange
	qm.mu.Unlock()
}

func (qm *QueueManager) notifyChange() {
	qm.mu.Lock()
	onChange := qm.onChange
	qm.mu.Unlock()
	if onChange != nil {
		onChange()
	}
}

func (qm *QueueManager) insertWaitingLocked(item queuedItem) {
	insertIdx := len(qm.waiting)
	for i, existing := range qm.waiting {
		if existing.priority < item.priority {
			insertIdx = i
			break
		}
	}
	qm.waiting = append(qm.waiting, queuedItem{})
	copy(qm.waiting[insertIdx+1:], qm.waiting[insertIdx:])
	qm.waiting[insertIdx] = item
}

func (qm *QueueManager) hasCapacityLocked() bool {
	return !qm.quiesced && (qm.maxConcurrent == 0 || len(qm.active) < qm.maxConcurrent)
}

func (qm *QueueManager) activateLocked(hash string, priority Priority) {
	qm.nextGeneration++
	activation := &queueActivation{generation: qm.nextGeneration}
	qm.active[hash] = activation
	qm.activePriorities[hash] = priority
}

func (qm *QueueManager) promoteOneLocked() string {
	if qm.paused || len(qm.waiting) == 0 || !qm.hasCapacityLocked() {
		return ""
	}
	next := qm.waiting[0]
	qm.waiting = qm.waiting[1:]
	qm.activateLocked(next.hash, next.priority)
	return next.hash
}

// reserveStartLocked captures the callback associated with a promotion and
// records the activation token it must validate immediately before invoking
// onStart. Validation under qm.mu is the start linearization point: a Remove
// or Pause that invalidates the token first prevents the callback, while a
// callback that validates first is already considered started and may finish
// without making the queue mutation wait for it.
func (qm *QueueManager) reserveStartLocked(hash string) func() {
	if hash == "" || qm.quiesced || qm.onStart == nil {
		return nil
	}
	activation, active := qm.active[hash]
	if !active {
		return nil
	}
	onStart := qm.onStart
	qm.pendingStarts[hash] = activation
	return func() {
		qm.mu.Lock()
		current, stillActive := qm.active[hash]
		pending, stillPending := qm.pendingStarts[hash]
		if !stillActive ||
			current != activation ||
			!stillPending ||
			pending != activation ||
			qm.paused ||
			qm.quiesced ||
			qm.onStart == nil {
			if stillPending && pending == activation {
				delete(qm.pendingStarts, hash)
			}
			qm.mu.Unlock()
			return
		}
		delete(qm.pendingStarts, hash)
		qm.runningStarts++
		qm.mu.Unlock()

		defer func() {
			qm.mu.Lock()
			qm.runningStarts--
			if qm.runningStarts == 0 {
				qm.startCond.Broadcast()
			}
			qm.mu.Unlock()
		}()
		onStart(QueueActivation{
			queue:      qm,
			hash:       hash,
			activation: activation,
		})
	}
}

// requeuePendingStartsLocked invalidates every callback that has not crossed
// its start linearization point. Pending activations are reinserted in their
// original activation order so Pause does not randomly reorder equal-priority
// work through map iteration.
func (qm *QueueManager) requeuePendingStartsLocked() {
	type pendingItem struct {
		item       queuedItem
		activation *queueActivation
	}
	pending := make([]pendingItem, 0, len(qm.pendingStarts))
	for hash, activation := range qm.pendingStarts {
		if current, active := qm.active[hash]; active && current == activation {
			pending = append(pending, pendingItem{
				item: queuedItem{
					hash:     hash,
					priority: qm.activePriorities[hash],
				},
				activation: activation,
			})
		}
		delete(qm.pendingStarts, hash)
	}
	sort.SliceStable(pending, func(i, j int) bool {
		return pending[i].activation.generation < pending[j].activation.generation
	})
	requeued := make([]queuedItem, 0, len(pending))
	for _, entry := range pending {
		if current, active := qm.active[entry.item.hash]; !active || current != entry.activation {
			continue
		}
		delete(qm.active, entry.item.hash)
		delete(qm.activePriorities, entry.item.hash)
		requeued = append(requeued, entry.item)
	}

	// Pending items had already been selected ahead of the remaining waiting
	// queue. Rebuild from that combined order so equal-priority items retain
	// their original FIFO position across Pause.
	remaining := append([]queuedItem(nil), qm.waiting...)
	qm.waiting = qm.waiting[:0]
	for _, item := range requeued {
		qm.insertWaitingLocked(item)
	}
	for _, item := range remaining {
		qm.insertWaitingLocked(item)
	}
}

// Quiesce prevents new queue activations for process shutdown without
// changing the user's persisted Paused preference. Existing active entries
// can still complete/remove themselves; waiting entries remain waiting for
// the next daemon.
func (qm *QueueManager) Quiesce() {
	_ = qm.QuiesceContext(context.Background())
}

// BeginQuiesce immediately closes queue start admission without waiting for
// callbacks that already crossed their start linearization point. Shutdown
// orchestration uses this before cancelling transfers so their stopped
// finalizers preserve active entries for restart.
func (qm *QueueManager) BeginQuiesce() {
	qm.mu.Lock()
	qm.beginQuiesceLocked()
	qm.mu.Unlock()
}

func (qm *QueueManager) beginQuiesceLocked() {
	qm.quiesced = true
	qm.onStart = nil
	// Reserved-but-not-started callbacks will fail token validation. Keep
	// their active entries in the persisted state so LoadState can return
	// interrupted work to the waiting queue after restart.
	clear(qm.pendingStarts)
}

// QuiesceContext is the bounded form of Quiesce. The queue is made quiescent
// before waiting, so a context timeout never re-enables promotion or loses
// queued state. Cancellation only bounds the wait for start callbacks that
// already crossed their linearization point.
func (qm *QueueManager) QuiesceContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	qm.mu.Lock()
	qm.beginQuiesceLocked()
	stopWakeup := context.AfterFunc(ctx, func() {
		qm.mu.Lock()
		qm.startCond.Broadcast()
		qm.mu.Unlock()
	})
	for qm.runningStarts > 0 && ctx.Err() == nil {
		qm.startCond.Wait()
	}
	var err error
	if qm.runningStarts > 0 {
		err = ctx.Err()
	}
	qm.mu.Unlock()
	stopWakeup()
	return err
}

// Add adds a download to the queue. If under capacity, it becomes active immediately.
// Otherwise, it's queued based on priority.
func (qm *QueueManager) Add(hash string, priority Priority) {
	var startCallback func()

	qm.mu.Lock()

	// Check if already active or queued
	if _, exists := qm.active[hash]; exists {
		qm.mu.Unlock()
		return
	}
	for _, item := range qm.waiting {
		if item.hash == hash {
			qm.mu.Unlock()
			return
		}
	}

	// A paused queue accepts new work but never starts it, even if capacity
	// is available. Previously Add bypassed Pause and started immediately.
	if !qm.paused && qm.hasCapacityLocked() {
		qm.activateLocked(hash, priority)
		startCallback = qm.reserveStartLocked(hash)
		qm.mu.Unlock()
		qm.notifyChange()
		if startCallback != nil {
			startCallback()
		}
		return
	}

	qm.insertWaitingLocked(queuedItem{hash: hash, priority: priority})
	qm.mu.Unlock()
	qm.notifyChange()
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
	qm.finish(hash, false)
}

// OnStopped releases a queue slot during normal operation, but preserves the
// active entry after Quiesce. Shutdown stops live downloaders before Manager
// persists its final snapshot; retaining the entry makes the incomplete work
// runnable after restart instead of silently dropping it from the queue.
func (qm *QueueManager) OnStopped(hash string) {
	qm.finish(hash, true)
}

func (qm *QueueManager) finish(hash string, preserveWhenQuiesced bool) {
	var startCallback func()
	qm.mu.Lock()

	// Ignore duplicate or unknown completions. Without this guard, a second
	// completion notification could consume another waiting item despite not
	// releasing an active slot.
	activation, exists := qm.active[hash]
	if !exists || activation.claimed {
		qm.mu.Unlock()
		return
	}
	if preserveWhenQuiesced && qm.quiesced {
		qm.mu.Unlock()
		return
	}
	delete(qm.active, hash)
	delete(qm.activePriorities, hash)
	delete(qm.pendingStarts, hash)
	startCallback = qm.reserveStartLocked(qm.promoteOneLocked())

	qm.mu.Unlock()
	qm.notifyChange()

	// Call onStart callback outside the lock to avoid deadlock
	if startCallback != nil {
		startCallback()
	}
}

// ClaimActivation transfers lifecycle ownership for an exact activation to
// the outer Start/Resume goroutine. Once claimed, hash-only OnComplete and
// OnStopped callbacks are ignored; FinishActivation must release the lease
// after the worker call returns.
func (qm *QueueManager) ClaimActivation(activation QueueActivation) bool {
	if activation.queue != qm || activation.activation == nil {
		return false
	}
	qm.mu.Lock()
	defer qm.mu.Unlock()
	current := qm.active[activation.hash]
	if current != activation.activation || current.claimed {
		return false
	}
	current.claimed = true
	return true
}

// IsActivationCurrent reports whether activation is still the exact active
// lease for its hash.
func (qm *QueueManager) IsActivationCurrent(activation QueueActivation) bool {
	if activation.queue != qm || activation.activation == nil {
		return false
	}
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.active[activation.hash] == activation.activation
}

// FinishActivation releases an exact claimed activation and promotes the next
// waiting item. It returns false when cancellation/re-add already replaced it.
func (qm *QueueManager) FinishActivation(activation QueueActivation) bool {
	return qm.finishActivation(activation, false)
}

// StopActivation releases an exact claimed activation during normal
// operation, but preserves it after Quiesce so shutdown persistence can
// restore the interrupted transfer as waiting work.
func (qm *QueueManager) StopActivation(activation QueueActivation) bool {
	return qm.finishActivation(activation, true)
}

func (qm *QueueManager) finishActivation(
	activation QueueActivation,
	preserveWhenQuiesced bool,
) bool {
	if activation.queue != qm || activation.activation == nil {
		return false
	}
	var startCallback func()
	qm.mu.Lock()
	if qm.active[activation.hash] != activation.activation {
		qm.mu.Unlock()
		return false
	}
	if preserveWhenQuiesced && qm.quiesced {
		qm.mu.Unlock()
		return true
	}
	delete(qm.active, activation.hash)
	delete(qm.activePriorities, activation.hash)
	delete(qm.pendingStarts, activation.hash)
	startCallback = qm.reserveStartLocked(qm.promoteOneLocked())
	qm.mu.Unlock()
	qm.notifyChange()
	if startCallback != nil {
		startCallback()
	}
	return true
}

// Remove cancels an active or waiting queue item. Removing an active item
// releases its slot and starts the next waiting item when the queue is not
// paused. It returns false when hash was not present.
func (qm *QueueManager) Remove(hash string) bool {
	var startCallback func()

	qm.mu.Lock()
	if _, exists := qm.active[hash]; exists {
		delete(qm.active, hash)
		delete(qm.activePriorities, hash)
		delete(qm.pendingStarts, hash)
		startCallback = qm.reserveStartLocked(qm.promoteOneLocked())
		qm.mu.Unlock()
		qm.notifyChange()
		if startCallback != nil {
			startCallback()
		}
		return true
	}

	for i, item := range qm.waiting {
		if item.hash != hash {
			continue
		}
		qm.waiting = append(qm.waiting[:i], qm.waiting[i+1:]...)
		qm.mu.Unlock()
		qm.notifyChange()
		return true
	}
	qm.mu.Unlock()
	return false
}

// IsActive reports whether hash currently occupies a queue slot.
func (qm *QueueManager) IsActive(hash string) bool {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	_, ok := qm.active[hash]
	return ok
}

// IsWaiting reports whether hash is waiting for a queue slot.
func (qm *QueueManager) IsWaiting(hash string) bool {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	for _, item := range qm.waiting {
		if item.hash == hash {
			return true
		}
	}
	return false
}

// runIfWaiting invokes fn while the queue lock proves hash is still waiting.
// This is used to release a probed downloader before a backlog promotion: a
// concurrent completion cannot promote the item between the membership check
// and resource teardown.
func (qm *QueueManager) runIfWaiting(hash string, fn func() error) (bool, error) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	for _, item := range qm.waiting {
		if item.hash == hash {
			return true, fn()
		}
	}
	return false, nil
}

// removeIfWaiting removes hash and invokes fn while the queue lock proves the
// item has not been promoted. The callback is used to detach its allocation
// before another completion can start it.
func (qm *QueueManager) removeIfWaiting(hash string, fn func() error) (bool, error) {
	qm.mu.Lock()
	for index, item := range qm.waiting {
		if item.hash != hash {
			continue
		}
		qm.waiting = append(qm.waiting[:index], qm.waiting[index+1:]...)
		var err error
		if fn != nil {
			err = fn()
		}
		qm.mu.Unlock()
		qm.notifyChange()
		return true, err
	}
	qm.mu.Unlock()
	return false, nil
}

// MaxConcurrent returns the maximum number of concurrent downloads.
func (qm *QueueManager) MaxConcurrent() int {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.maxConcurrent
}

// GetState returns the current queue state for persistence.
func (qm *QueueManager) GetState() QueueState {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.getStateLocked()
}

func (qm *QueueManager) getStateLocked() QueueState {
	activeHashes := make([]string, 0, len(qm.active))
	for hash := range qm.active {
		activeHashes = append(activeHashes, hash)
	}
	sort.Strings(activeHashes)
	active := make([]QueuedItemState, 0, len(activeHashes))
	for _, hash := range activeHashes {
		active = append(active, QueuedItemState{
			Hash:     hash,
			Priority: qm.activePriorities[hash],
		})
	}

	waiting := make([]QueuedItemState, len(qm.waiting))
	for i, item := range qm.waiting {
		waiting[i] = QueuedItemState{
			Hash:     item.hash,
			Priority: item.priority,
		}
	}

	return QueueState{
		MaxConcurrent: qm.maxConcurrent,
		Active:        active,
		Waiting:       waiting,
		Paused:        qm.paused,
	}
}

// removeForManagerPersistence removes candidate hashes while queue.mu is held,
// then asks persist to commit the corresponding manager+queue snapshot.
// rejectQueued excludes both active and waiting queue members (used by
// Flush/FlushOne, which are history cleanup operations and must not discard
// runnable work).
//
// The returned finalize function must run after the caller releases manager
// locks. It persists/promotes any slot made available by a committed active
// removal without invoking an onStart callback inside the manager transaction.
func (qm *QueueManager) removeForManagerPersistence(
	candidates map[string]struct{},
	rejectQueued bool,
	persist func(state *QueueState, removable map[string]struct{}) (committed bool, err error),
) (finalize func(), err error) {
	qm.mu.Lock()

	queued := make(map[string]struct{}, len(qm.active)+len(qm.waiting))
	for hash := range qm.active {
		queued[hash] = struct{}{}
	}
	for _, item := range qm.waiting {
		queued[item.hash] = struct{}{}
	}
	removable := make(map[string]struct{}, len(candidates))
	for hash := range candidates {
		if rejectQueued {
			if _, pending := queued[hash]; pending {
				continue
			}
		}
		removable[hash] = struct{}{}
	}

	activeBefore := make(map[string]*queueActivation, len(qm.active))
	for hash, activation := range qm.active {
		activeBefore[hash] = activation
	}
	prioritiesBefore := make(map[string]Priority, len(qm.activePriorities))
	for hash, priority := range qm.activePriorities {
		prioritiesBefore[hash] = priority
	}
	pendingBefore := make(map[string]*queueActivation, len(qm.pendingStarts))
	for hash, activation := range qm.pendingStarts {
		pendingBefore[hash] = activation
	}
	waitingBefore := append([]queuedItem(nil), qm.waiting...)

	removedActive := false
	for hash := range removable {
		if _, active := qm.active[hash]; active {
			removedActive = true
			delete(qm.active, hash)
			delete(qm.activePriorities, hash)
			delete(qm.pendingStarts, hash)
		}
	}
	if len(removable) > 0 && len(qm.waiting) > 0 {
		filtered := qm.waiting[:0]
		for _, item := range qm.waiting {
			if _, remove := removable[item.hash]; !remove {
				filtered = append(filtered, item)
			}
		}
		qm.waiting = filtered
	}

	state := qm.getStateLocked()
	committed, persistErr := persist(&state, removable)
	if !committed {
		qm.active = activeBefore
		qm.activePriorities = prioritiesBefore
		qm.pendingStarts = pendingBefore
		qm.waiting = waitingBefore
		qm.mu.Unlock()
		return nil, persistErr
	}

	var startCallbacks []func()
	if removedActive {
		for {
			hash := qm.promoteOneLocked()
			if hash == "" {
				break
			}
			if callback := qm.reserveStartLocked(hash); callback != nil {
				startCallbacks = append(startCallbacks, callback)
			}
		}
	}
	qm.mu.Unlock()

	return func() {
		if removedActive {
			// The transaction persisted a safe pre-promotion state. Persist
			// promoted slots now; a crash before this write merely restores
			// them as waiting and cannot strand a missing hash.
			qm.notifyChange()
		}
		for _, callback := range startCallbacks {
			callback()
		}
	}, persistErr
}

// LoadState restores queue state from persistence.
// Previously active items are returned to the waiting queue so they can be
// reconstructed and safely restarted by the new daemon process.
func (qm *QueueManager) LoadState(state QueueState) {
	qm.mu.Lock()

	qm.maxConcurrent = state.MaxConcurrent
	qm.active = make(map[string]*queueActivation)
	qm.activePriorities = make(map[string]Priority)
	qm.pendingStarts = make(map[string]*queueActivation)
	qm.waiting = make([]queuedItem, 0, len(state.Active)+len(state.Waiting))
	qm.paused = state.Paused

	for _, item := range state.Active {
		qm.insertWaitingLocked(queuedItem{
			hash:     item.Hash,
			priority: item.Priority,
		})
	}
	for _, item := range state.Waiting {
		qm.insertWaitingLocked(queuedItem{
			hash:     item.Hash,
			priority: item.Priority,
		})
	}
	qm.mu.Unlock()
	qm.notifyChange()
}

// Pause pauses the queue, preventing auto-start of waiting items.
func (qm *QueueManager) Pause() {
	qm.mu.Lock()
	if qm.paused {
		qm.mu.Unlock()
		return
	}
	qm.paused = true
	qm.requeuePendingStartsLocked()
	qm.mu.Unlock()
	qm.notifyChange()
}

// Resume resumes the queue, enabling auto-start and starting waiting items up to capacity.
func (qm *QueueManager) Resume() {
	var startCallbacks []func()

	qm.mu.Lock()

	qm.paused = false

	// Start waiting items up to capacity
	for {
		hash := qm.promoteOneLocked()
		if hash == "" {
			break
		}
		if startCallback := qm.reserveStartLocked(hash); startCallback != nil {
			startCallbacks = append(startCallbacks, startCallback)
		}
	}

	qm.mu.Unlock()
	qm.notifyChange()

	// Call onStart callbacks outside the lock to avoid deadlock
	for _, startCallback := range startCallbacks {
		startCallback()
	}
}

// IsPaused returns whether the queue is paused.
func (qm *QueueManager) IsPaused() bool {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.paused
}

// Move reorders a waiting item to a new position in the queue.
// Active downloads cannot be moved. Position is clamped to valid range [0, len-1].
func (qm *QueueManager) Move(hash string, position int) error {
	qm.mu.Lock()
	changed := false
	defer func() {
		onChange := qm.onChange
		qm.mu.Unlock()
		if changed && onChange != nil {
			onChange()
		}
	}()

	// Check if hash is in active (error - can't move active downloads)
	if _, exists := qm.active[hash]; exists {
		return ErrCannotMoveActive
	}

	// Find hash in waiting queue
	idx := -1
	for i, item := range qm.waiting {
		if item.hash == hash {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ErrQueueHashNotFound
	}

	// Clamp position to valid range
	if position < 0 {
		position = 0
	}
	if position >= len(qm.waiting) {
		position = len(qm.waiting) - 1
	}

	// No-op if moving to same position
	if idx == position {
		return nil
	}

	// Extract the item to move
	item := qm.waiting[idx]

	// Remove from current position
	qm.waiting = append(qm.waiting[:idx], qm.waiting[idx+1:]...)

	// Insert at new position
	qm.waiting = append(qm.waiting[:position], append([]queuedItem{item}, qm.waiting[position:]...)...)

	changed = true
	return nil
}

// GetActiveHashes returns a copy of the active download hashes.
func (qm *QueueManager) GetActiveHashes() []string {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	hashes := make([]string, 0, len(qm.active))
	for hash := range qm.active {
		hashes = append(hashes, hash)
	}
	return hashes
}

// GetWaitingItems returns a copy of the waiting queue items with their positions.
func (qm *QueueManager) GetWaitingItems() []QueuedItemState {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	items := make([]QueuedItemState, len(qm.waiting))
	for i, item := range qm.waiting {
		items[i] = QueuedItemState{
			Hash:     item.hash,
			Priority: item.priority,
		}
	}
	return items
}
