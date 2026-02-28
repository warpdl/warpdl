// Package scheduler provides download scheduling functionality for WarpDL.
// It implements a single-goroutine scheduler using a min-heap of ScheduleEvents
// sorted by trigger time, with a 60-second max-sleep-cap to handle NTP steps,
// DST transitions, and system sleep (macOS monotonic clock pause).
//
// The scheduler is a daemon-level component that fires events and calls a
// registered OnTrigger callback to enqueue downloads through the existing
// download flow. It does not persist state — the scheduler heap is rebuilt
// from Item fields on daemon restart.
package scheduler
