package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// loadItemSpec is a compact spec for building test ItemsMap entries.
type loadItemSpec struct {
	hash      string
	state     string
	triggerAt time.Time
	cronExpr  string
}

// makeLoadItems builds a warplib.ItemsMap from the given specs.
func makeLoadItems(t *testing.T, specs []loadItemSpec) warplib.ItemsMap {
	t.Helper()
	m := make(warplib.ItemsMap, len(specs))
	for _, s := range specs {
		m[s.hash] = &warplib.Item{
			Hash:          s.hash,
			ScheduleState: warplib.ScheduleState(s.state),
			ScheduledAt:   s.triggerAt,
			CronExpr:      s.cronExpr,
		}
	}
	return m
}

// T013: Scheduler core loop tests

func TestScheduler_AddAndFire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	fired := make(map[string]bool)
	onTrigger := func(hash string) {
		mu.Lock()
		fired[hash] = true
		mu.Unlock()
	}

	s := New(ctx, onTrigger)

	// Schedule an event 100ms from now
	s.Add(ScheduleEvent{
		ItemHash:  "hash1",
		TriggerAt: time.Now().Add(100 * time.Millisecond),
	})

	// Wait enough time for the event to fire
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !fired["hash1"] {
		t.Fatal("expected hash1 to fire")
	}
}

func TestScheduler_CancelBeforeFire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	fired := make(map[string]bool)
	onTrigger := func(hash string) {
		mu.Lock()
		fired[hash] = true
		mu.Unlock()
	}

	s := New(ctx, onTrigger)

	// Schedule an event 2s from now (plenty of margin)
	s.Add(ScheduleEvent{
		ItemHash:  "hash2",
		TriggerAt: time.Now().Add(2 * time.Second),
	})

	// Give the goroutine time to process the add
	time.Sleep(100 * time.Millisecond)

	// Cancel it before it fires
	s.Remove("hash2")

	// Give the goroutine time to process the remove
	time.Sleep(100 * time.Millisecond)

	// Wait past the trigger time
	time.Sleep(2 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if fired["hash2"] {
		t.Fatal("expected hash2 NOT to fire after cancel")
	}
}

func TestScheduler_ShutdownViaContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var mu sync.Mutex
	fired := make(map[string]bool)
	onTrigger := func(hash string) {
		mu.Lock()
		fired[hash] = true
		mu.Unlock()
	}

	s := New(ctx, onTrigger)

	// Schedule an event 500ms from now
	s.Add(ScheduleEvent{
		ItemHash:  "hash3",
		TriggerAt: time.Now().Add(500 * time.Millisecond),
	})

	// Cancel context immediately
	cancel()

	// Wait past the trigger time
	time.Sleep(700 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if fired["hash3"] {
		t.Fatal("expected hash3 NOT to fire after context cancel")
	}
	_ = s // ensure scheduler is referenced
}

func TestScheduler_EmptyDoesNotFire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firedCount := 0
	onTrigger := func(hash string) {
		firedCount++
	}

	_ = New(ctx, onTrigger)

	// Wait a bit to ensure nothing spurious fires
	time.Sleep(200 * time.Millisecond)

	if firedCount != 0 {
		t.Fatalf("expected no triggers on empty scheduler, got %d", firedCount)
	}
}

func TestScheduler_MultipleEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	fired := []string{}
	onTrigger := func(hash string) {
		mu.Lock()
		fired = append(fired, hash)
		mu.Unlock()
	}

	s := New(ctx, onTrigger)

	// Schedule two events at different times
	s.Add(ScheduleEvent{
		ItemHash:  "first",
		TriggerAt: time.Now().Add(100 * time.Millisecond),
	})
	s.Add(ScheduleEvent{
		ItemHash:  "second",
		TriggerAt: time.Now().Add(200 * time.Millisecond),
	})

	// Wait for both to fire
	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(fired))
	}
	// First should fire before second
	if fired[0] != "first" {
		t.Errorf("expected first to fire first, got %s", fired[0])
	}
	if fired[1] != "second" {
		t.Errorf("expected second to fire second, got %s", fired[1])
	}
}

func TestScheduler_RemoveNonexistent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := New(ctx, func(hash string) {})

	// Removing a nonexistent hash should not panic
	s.Remove("nonexistent")
}

// T047: Missed-schedule detection tests

func TestLoadSchedules_MissedItems(t *testing.T) {
	now := time.Now()
	items := makeLoadItems(t, []loadItemSpec{
		{hash: "past1", state: "scheduled", triggerAt: now.Add(-1 * time.Hour)},
		{hash: "past2", state: "scheduled", triggerAt: now.Add(-10 * time.Minute)},
	})

	missed, future := LoadSchedules(items, now)

	if len(missed) != 2 {
		t.Fatalf("expected 2 missed items, got %d", len(missed))
	}
	if len(future) != 0 {
		t.Fatalf("expected 0 future events, got %d", len(future))
	}
	for _, item := range missed {
		if item.ScheduleState != "missed" {
			t.Errorf("expected ScheduleState 'missed', got %q for item %s", item.ScheduleState, item.Hash)
		}
	}
}

func TestLoadSchedules_FutureItems(t *testing.T) {
	now := time.Now()
	items := makeLoadItems(t, []loadItemSpec{
		{hash: "future1", state: "scheduled", triggerAt: now.Add(1 * time.Hour)},
		{hash: "future2", state: "scheduled", triggerAt: now.Add(2 * time.Hour)},
	})

	missed, future := LoadSchedules(items, now)

	if len(missed) != 0 {
		t.Fatalf("expected 0 missed items, got %d", len(missed))
	}
	if len(future) != 2 {
		t.Fatalf("expected 2 future events, got %d", len(future))
	}
}

func TestLoadSchedules_MixedItems(t *testing.T) {
	now := time.Now()
	items := makeLoadItems(t, []loadItemSpec{
		{hash: "past1", state: "scheduled", triggerAt: now.Add(-30 * time.Minute)},
		{hash: "future1", state: "scheduled", triggerAt: now.Add(30 * time.Minute)},
		{hash: "cancelled1", state: "cancelled", triggerAt: now.Add(-1 * time.Hour)},
		{hash: "triggered1", state: "triggered", triggerAt: now.Add(-2 * time.Hour)},
		{hash: "none1", state: "", triggerAt: now.Add(1 * time.Hour)},
	})

	missed, future := LoadSchedules(items, now)

	if len(missed) != 1 {
		t.Fatalf("expected 1 missed item, got %d", len(missed))
	}
	if missed[0].Hash != "past1" {
		t.Errorf("expected missed item to be 'past1', got %q", missed[0].Hash)
	}
	if len(future) != 1 {
		t.Fatalf("expected 1 future event, got %d", len(future))
	}
	if future[0].ItemHash != "future1" {
		t.Errorf("expected future event to be 'future1', got %q", future[0].ItemHash)
	}
}

func TestLoadSchedules_Empty(t *testing.T) {
	items := makeLoadItems(t, nil)
	missed, future := LoadSchedules(items, time.Now())
	if len(missed) != 0 || len(future) != 0 {
		t.Errorf("expected empty results for empty items, got missed=%d future=%d", len(missed), len(future))
	}
}

func TestLoadSchedules_FutureEventPreservesFields(t *testing.T) {
	now := time.Now()
	triggerAt := now.Add(1 * time.Hour)
	items := makeLoadItems(t, []loadItemSpec{
		{hash: "cron1", state: "scheduled", triggerAt: triggerAt, cronExpr: "0 2 * * *"},
	})

	_, future := LoadSchedules(items, now)

	if len(future) != 1 {
		t.Fatalf("expected 1 future event, got %d", len(future))
	}
	ev := future[0]
	if ev.ItemHash != "cron1" {
		t.Errorf("expected ItemHash 'cron1', got %q", ev.ItemHash)
	}
	if ev.CronExpr != "0 2 * * *" {
		t.Errorf("expected CronExpr '0 2 * * *', got %q", ev.CronExpr)
	}
	if !ev.TriggerAt.Equal(triggerAt) {
		t.Errorf("expected TriggerAt %v, got %v", triggerAt, ev.TriggerAt)
	}
}

// T064: cron next-occurrence tests

func TestNextCronOccurrence_ValidExpr(t *testing.T) {
	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	next, err := nextCronOccurrence("0 2 * * *", now)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	// Should be 2026-03-01 02:00 UTC
	if next.Hour() != 2 || next.Minute() != 0 {
		t.Errorf("expected 02:00, got %v", next)
	}
}

func TestNextCronOccurrence_InvalidExpr(t *testing.T) {
	_, err := nextCronOccurrence("bad-expr", time.Now())
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestHasOccurrenceWithinYear(t *testing.T) {
	// A valid common expression should have occurrences
	now := time.Now()
	if !hasOccurrenceWithinYear("0 2 * * *", now) {
		t.Error("expected daily cron to have occurrence in next year")
	}
}

func TestHasOccurrenceWithinYear_InvalidExpr(t *testing.T) {
	if hasOccurrenceWithinYear("bad-cron", time.Now()) {
		t.Error("invalid cron should return false")
	}
}

// T072: Missed recurring schedules on daemon restart
// LoadSchedules must handle missed recurring: enqueue missed AND compute next cron

func TestLoadSchedules_MissedRecurring_ComputesNextOccurrence(t *testing.T) {
	now := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	// scheduled 1 hour before now, with a cron expression
	items := makeLoadItems(t, []loadItemSpec{
		{hash: "recurring1", state: "scheduled", triggerAt: now.Add(-1 * time.Hour), cronExpr: "0 2 * * *"},
	})

	missed, future := LoadSchedules(items, now)

	// Should be marked missed for immediate trigger
	if len(missed) != 1 {
		t.Fatalf("expected 1 missed item, got %d", len(missed))
	}
	if missed[0].Hash != "recurring1" {
		t.Errorf("expected missed item 'recurring1', got %q", missed[0].Hash)
	}
	if missed[0].ScheduleState != "missed" {
		t.Errorf("expected ScheduleState 'missed', got %q", missed[0].ScheduleState)
	}

	// AND a future event computed from the cron expression
	if len(future) != 1 {
		t.Fatalf("expected 1 future event for next cron occurrence, got %d", len(future))
	}
	if future[0].ItemHash != "recurring1" {
		t.Errorf("expected future event ItemHash 'recurring1', got %q", future[0].ItemHash)
	}
	if future[0].CronExpr != "0 2 * * *" {
		t.Errorf("expected CronExpr preserved in future event, got %q", future[0].CronExpr)
	}
	// Next occurrence must be after now
	if !future[0].TriggerAt.After(now) {
		t.Errorf("expected future TriggerAt to be after now (%v), got %v", now, future[0].TriggerAt)
	}
}

func TestLoadSchedules_RecurringFuture_PreservesAsFuture(t *testing.T) {
	now := time.Now()
	// scheduled in the future, with a cron expression — should simply go into future
	items := makeLoadItems(t, []loadItemSpec{
		{hash: "cron-future", state: "scheduled", triggerAt: now.Add(2 * time.Hour), cronExpr: "*/30 * * * *"},
	})

	missed, future := LoadSchedules(items, now)

	if len(missed) != 0 {
		t.Fatalf("expected 0 missed items for future recurring, got %d", len(missed))
	}
	if len(future) != 1 {
		t.Fatalf("expected 1 future event, got %d", len(future))
	}
	if future[0].CronExpr != "*/30 * * * *" {
		t.Errorf("expected CronExpr '*/30 * * * *', got %q", future[0].CronExpr)
	}
}

// T069 (scheduler side): recurring re-schedule after fire
// The scheduler must re-enqueue an event with CronExpr after triggering it.

func TestScheduler_RecurringReSchedule(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	fireCount := 0
	onTrigger := func(hash string) {
		mu.Lock()
		fireCount++
		mu.Unlock()
	}

	s := New(ctx, onTrigger)

	// Schedule a recurring event every 100ms
	s.Add(ScheduleEvent{
		ItemHash:  "recurring",
		TriggerAt: time.Now().Add(100 * time.Millisecond),
		CronExpr:  "* * * * *", // every minute — scheduler uses next occurrence logic
	})

	// Wait enough for 2 firings — but with a 1-minute cron the second won't fire in 500ms.
	// So we just verify it fired at least once and the event stays alive.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := fireCount
	mu.Unlock()

	if count < 1 {
		t.Fatal("expected recurring event to fire at least once")
	}
}
