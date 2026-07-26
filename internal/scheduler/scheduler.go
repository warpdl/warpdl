package scheduler

import (
	"container/heap"
	"context"
	"time"

	"github.com/adhocore/gronx"
	"github.com/warpdl/warpdl/pkg/warplib"
)

const maxSleepCap = 60 * time.Second

// Scheduler manages scheduled download events using a min-heap.
// It runs a background goroutine that sleeps until the next event's
// trigger time, then calls the onTrigger callback with the item hash.
type Scheduler struct {
	addChan    chan ScheduleEvent
	removeChan chan string
	ctx        context.Context
	done       chan struct{}
}

// New creates and starts a new Scheduler.
// The onTrigger callback is invoked when a scheduled event fires.
// The scheduler goroutine exits when ctx is cancelled.
func New(ctx context.Context, onTrigger func(string)) *Scheduler {
	s := &Scheduler{
		addChan:    make(chan ScheduleEvent, 64),
		removeChan: make(chan string, 64),
		ctx:        ctx,
		done:       make(chan struct{}),
	}
	go s.run(onTrigger)
	return s
}

// Done is closed after the scheduler loop and any in-flight trigger callback
// have returned.
func (s *Scheduler) Done() <-chan struct{} {
	return s.done
}

// Add enqueues a new schedule event.
func (s *Scheduler) Add(event ScheduleEvent) {
	select {
	case s.addChan <- event:
	case <-s.ctx.Done():
	}
}

// Remove cancels a scheduled event by item hash.
func (s *Scheduler) Remove(itemHash string) {
	select {
	case s.removeChan <- itemHash:
	case <-s.ctx.Done():
	}
}

// run is the core scheduler goroutine implementing the active-object pattern.
// It maintains a min-heap of events and sleeps with a 60s max-sleep-cap.
// For recurring events (CronExpr != ""), after firing it computes the next
// occurrence and re-adds it to the heap automatically.
func (s *Scheduler) run(onTrigger func(string)) {
	defer close(s.done)
	h := &scheduleHeap{}
	heap.Init(h)

	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	resetTimer := func() <-chan time.Time {
		if timer != nil {
			timer.Stop()
		}
		if h.Len() == 0 {
			// No events — block indefinitely on channels
			return nil
		}
		next := (*h)[0].TriggerAt
		dur := time.Until(next)
		if dur > maxSleepCap {
			dur = maxSleepCap
		}
		if dur < 0 {
			dur = 0
		}
		timer = time.NewTimer(dur)
		return timer.C
	}

	timerCh := resetTimer()

	for {
		select {
		case <-s.ctx.Done():
			return

		case event := <-s.addChan:
			heapPush(h, event)
			timerCh = resetTimer()

		case hash := <-s.removeChan:
			heapRemoveByHash(h, hash)
			timerCh = resetTimer()

		case <-timerCh:
			// Check and fire all events whose time has arrived
			now := time.Now()
			for h.Len() > 0 && !(*h)[0].TriggerAt.After(now) {
				event := heapPop(h)
				onTrigger(event.ItemHash)
				// T069: For recurring events, compute next cron occurrence and re-add.
				if event.CronExpr != "" {
					next, err := nextCronOccurrence(event.CronExpr, time.Now())
					if err == nil {
						heapPush(h, ScheduleEvent{
							ItemHash:  event.ItemHash,
							TriggerAt: next,
							CronExpr:  event.CronExpr,
						})
					}
				}
			}
			timerCh = resetTimer()
		}
	}
}

// NextOccurrence returns the next time the cron expression fires strictly
// after start. Daemon orchestration uses this to persist the same occurrence
// that the in-memory scheduler will enqueue.
func NextOccurrence(expr string, start time.Time) (time.Time, error) {
	return gronx.NextTickAfter(expr, start, false)
}

func nextCronOccurrence(expr string, start time.Time) (time.Time, error) {
	return NextOccurrence(expr, start)
}

// hasOccurrenceWithinYear checks if a cron expression has any occurrence
// within 1 year from the given time. Returns false for invalid expressions
// or if no occurrence exists within the 1-year window.
func hasOccurrenceWithinYear(expr string, from time.Time) bool {
	next, err := gronx.NextTickAfter(expr, from, false)
	if err != nil {
		return false
	}
	return next.Before(from.Add(365 * 24 * time.Hour))
}

// LoadSchedules scans an ItemsMap at daemon startup to detect missed schedules
// and identify future scheduled events to add to the scheduler heap. It is
// read-only: manager-owned Items are snapshotted and all state transitions are
// left to Manager methods in daemon orchestration.
//
// Items with ScheduleState="scheduled" and ScheduledAt before now are returned
// in missed for immediate enqueueing.
// Items with ScheduleState="scheduled" and ScheduledAt after now are returned
// in future as ScheduleEvents ready to push into the heap.
// Items without ScheduledAt set or with other ScheduleStates are skipped.
//
// T072: For missed recurring items (CronExpr != ""), the next cron occurrence
// is computed and added to future so the recurring schedule continues.
func LoadSchedules(items warplib.ItemsMap, now time.Time) (missed []ScheduleEvent, future []ScheduleEvent) {
	for _, item := range items {
		snapshot := item.Snapshot()
		if snapshot.ScheduleState != warplib.ScheduleStateScheduled {
			continue
		}
		if snapshot.ScheduledAt.IsZero() {
			continue
		}
		event := ScheduleEvent{
			ItemHash:  snapshot.Hash,
			TriggerAt: snapshot.ScheduledAt,
			CronExpr:  snapshot.CronExpr,
		}
		if !snapshot.ScheduledAt.After(now) {
			missed = append(missed, event)
			// Return a future event for recurring schedules, but do not
			// mutate the Item. Daemon persistence owns that transition.
			if snapshot.CronExpr != "" {
				next, err := nextCronOccurrence(snapshot.CronExpr, now)
				if err == nil {
					future = append(future, ScheduleEvent{
						ItemHash:  snapshot.Hash,
						TriggerAt: next,
						CronExpr:  snapshot.CronExpr,
					})
				}
			}
		} else {
			future = append(future, event)
		}
	}
	return missed, future
}
