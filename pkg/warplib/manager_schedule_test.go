package warplib

import (
	"bytes"
	"encoding/gob"
	"testing"
	"time"
)

// T014: Schedule-aware item query tests

func TestGetScheduledItems_ReturnsOnlyScheduled(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// Scheduled item
	item1 := &Item{
		Hash:          "sched-1",
		Name:          "scheduled.bin",
		Url:           "http://example.com/sched.bin",
		TotalSize:     100,
		ScheduleState: ScheduleStateScheduled,
		ScheduledAt:   time.Now().Add(1 * time.Hour),
		Parts:         make(map[int64]*ItemPart),
		mu:            m.mu,
		memPart:       make(map[string]int64),
	}
	// Non-scheduled item
	item2 := &Item{
		Hash:          "normal-1",
		Name:          "normal.bin",
		Url:           "http://example.com/normal.bin",
		TotalSize:     100,
		ScheduleState: ScheduleStateNone,
		Parts:         make(map[int64]*ItemPart),
		mu:            m.mu,
		memPart:       make(map[string]int64),
	}
	// Triggered item (should NOT be returned)
	item3 := &Item{
		Hash:          "triggered-1",
		Name:          "triggered.bin",
		Url:           "http://example.com/triggered.bin",
		TotalSize:     100,
		ScheduleState: ScheduleStateTriggered,
		Parts:         make(map[int64]*ItemPart),
		mu:            m.mu,
		memPart:       make(map[string]int64),
	}
	// Cancelled item (should NOT be returned)
	item4 := &Item{
		Hash:          "cancelled-1",
		Name:          "cancelled.bin",
		Url:           "http://example.com/cancelled.bin",
		TotalSize:     100,
		ScheduleState: ScheduleStateCancelled,
		Parts:         make(map[int64]*ItemPart),
		mu:            m.mu,
		memPart:       make(map[string]int64),
	}
	// Missed item (should NOT be returned)
	item5 := &Item{
		Hash:          "missed-1",
		Name:          "missed.bin",
		Url:           "http://example.com/missed.bin",
		TotalSize:     100,
		ScheduleState: ScheduleStateMissed,
		Parts:         make(map[int64]*ItemPart),
		mu:            m.mu,
		memPart:       make(map[string]int64),
	}

	m.UpdateItem(item1)
	m.UpdateItem(item2)
	m.UpdateItem(item3)
	m.UpdateItem(item4)
	m.UpdateItem(item5)

	scheduled := m.GetScheduledItems()
	if len(scheduled) != 1 {
		t.Fatalf("expected 1 scheduled item, got %d", len(scheduled))
	}
	if scheduled[0].Hash != "sched-1" {
		t.Errorf("expected hash 'sched-1', got %q", scheduled[0].Hash)
	}
}

func TestGetScheduledItems_EmptyManager(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	scheduled := m.GetScheduledItems()
	if len(scheduled) != 0 {
		t.Fatalf("expected 0 scheduled items, got %d", len(scheduled))
	}
}

func TestGetScheduledItems_MultipleScheduled(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	for i := 0; i < 3; i++ {
		item := &Item{
			Hash:          "sched-multi-" + string(rune('a'+i)),
			Name:          "file.bin",
			Url:           "http://example.com/file.bin",
			TotalSize:     100,
			ScheduleState: ScheduleStateScheduled,
			ScheduledAt:   time.Now().Add(time.Duration(i+1) * time.Hour),
			Parts:         make(map[int64]*ItemPart),
			mu:            m.mu,
			memPart:       make(map[string]int64),
		}
		m.UpdateItem(item)
	}

	scheduled := m.GetScheduledItems()
	if len(scheduled) != 3 {
		t.Fatalf("expected 3 scheduled items, got %d", len(scheduled))
	}
}

// T048: Schedule persistence GOB round-trip tests

func TestScheduleFieldsGOBRoundTrip_AllStates(t *testing.T) {
	triggerAt := time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)
	cronExpr := "0 2 * * *"

	states := []ScheduleState{
		ScheduleStateNone,
		ScheduleStateScheduled,
		ScheduleStateTriggered,
		ScheduleStateMissed,
		ScheduleStateCancelled,
	}

	for _, state := range states {
		t.Run(string(state)+"_gob", func(t *testing.T) {
			original := ItemsMap{
				"test-hash": {
					Hash:          "test-hash",
					Name:          "file.bin",
					Url:           "http://example.com/file.bin",
					ScheduledAt:   triggerAt,
					CronExpr:      cronExpr,
					ScheduleState: state,
				},
			}
			data := ManagerData{Items: original}

			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(data); err != nil {
				t.Fatalf("encode: %v", err)
			}

			var decoded ManagerData
			if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}

			item, ok := decoded.Items["test-hash"]
			if !ok {
				t.Fatal("item not found in decoded map")
			}
			if item.ScheduleState != state {
				t.Errorf("ScheduleState: want %q, got %q", state, item.ScheduleState)
			}
			if !item.ScheduledAt.Equal(triggerAt) {
				t.Errorf("ScheduledAt: want %v, got %v", triggerAt, item.ScheduledAt)
			}
			if item.CronExpr != cronExpr {
				t.Errorf("CronExpr: want %q, got %q", cronExpr, item.CronExpr)
			}
		})
	}
}

func TestScheduleFieldsGOBRoundTrip_CookieSourcePath(t *testing.T) {
	original := ItemsMap{
		"h1": {
			Hash:             "h1",
			CookieSourcePath: "/tmp/cookies.sqlite",
		},
	}
	data := ManagerData{Items: original}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(data); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded ManagerData
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	item := decoded.Items["h1"]
	if item.CookieSourcePath != "/tmp/cookies.sqlite" {
		t.Errorf("CookieSourcePath: want '/tmp/cookies.sqlite', got %q", item.CookieSourcePath)
	}
}
