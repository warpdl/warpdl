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
}

// New creates and starts a new Scheduler.
// The onTrigger callback is invoked when a scheduled event fires.
// The scheduler goroutine exits when ctx is cancelled.
func New(ctx context.Context, onTrigger func(string)) *Scheduler {
	s := &Scheduler{
		addChan:    make(chan ScheduleEvent, 64),
		removeChan: make(chan string, 64),
		ctx:        ctx,
	}
	go s.run(onTrigger)
	return s
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

// nextCronOccurrence returns the next time the cron expression fires strictly
// after start. Uses gronx.NextTickAfter with inclRefTime=false.
func nextCronOccurrence(expr string, start time.Time) (time.Time, error) {
	return gronx.NextTickAfter(expr, start, false)
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
// and identify future scheduled events to add to the scheduler heap.
//
// Items with ScheduleState="scheduled" and ScheduledAt before now are marked
// ScheduleStateMissed and returned in missed for immediate enqueueing.
// Items with ScheduleState="scheduled" and ScheduledAt after now are returned
// in future as ScheduleEvents ready to push into the heap.
// Items without ScheduledAt set or with other ScheduleStates are skipped.
//
// T072: For missed recurring items (CronExpr != ""), the next cron occurrence
// is computed and added to future so the recurring schedule continues.
func LoadSchedules(items warplib.ItemsMap, now time.Time) (missed []*warplib.Item, future []ScheduleEvent) {
	for _, item := range items {
		if item.ScheduleState != warplib.ScheduleStateScheduled {
			continue
		}
		if item.ScheduledAt.IsZero() {
			continue
		}
		if item.ScheduledAt.Before(now) {
			item.ScheduleState = warplib.ScheduleStateMissed
			missed = append(missed, item)
			// T072: For recurring items, also compute the next occurrence and add to future.
			if item.CronExpr != "" {
				next, err := nextCronOccurrence(item.CronExpr, now)
				if err == nil {
					future = append(future, ScheduleEvent{
						ItemHash:  item.Hash,
						TriggerAt: next,
						CronExpr:  item.CronExpr,
					})
				}
			}
		} else {
			future = append(future, ScheduleEvent{
				ItemHash:  item.Hash,
				TriggerAt: item.ScheduledAt,
				CronExpr:  item.CronExpr,
			})
		}
	}
	return missed, future
}
