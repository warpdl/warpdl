package scheduler

import "time"

// ScheduleEvent represents a pending scheduled download in the scheduler heap.
// It is an in-memory only type — the heap is rebuilt from Item fields on daemon restart.
type ScheduleEvent struct {
	// ItemHash is the unique identifier of the Item to download when TriggerAt is reached.
	ItemHash string
	// TriggerAt is the wall-clock time when this download should be enqueued.
	TriggerAt time.Time
	// CronExpr is the cron expression for recurring downloads.
	// Empty string means one-shot — no re-scheduling after firing.
	CronExpr string
}
