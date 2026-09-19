package warplib

import (
	"testing"
	"time"
)

func mustParseSchedule(t *testing.T, s string) *SpeedSchedule {
	t.Helper()
	schedule, err := ParseSpeedSchedule(s)
	if err != nil {
		t.Fatalf("ParseSpeedSchedule(%q): %v", s, err)
	}
	return schedule
}

func atTime(hour, minute int) time.Time {
	return time.Date(2026, time.September, 18, hour, minute, 0, 0, time.Local)
}

func TestParseSpeedSchedule(t *testing.T) {
	schedule := mustParseSchedule(t, "09:00-17:00:512KB")
	if got := schedule.LimitAt(atTime(10, 0)); got != 512*KB {
		t.Fatalf("LimitAt(10:00) = %d, want %d", got, 512*KB)
	}
	if got := schedule.LimitAt(atTime(8, 59)); got != 0 {
		t.Fatalf("LimitAt(08:59) = %d, want unlimited", got)
	}
	if got := schedule.LimitAt(atTime(17, 0)); got != 0 {
		t.Fatalf("LimitAt(17:00) = %d, want unlimited (end-exclusive)", got)
	}
}

func TestParseSpeedSchedule_Overnight(t *testing.T) {
	schedule := mustParseSchedule(t, "22:00-06:00:1MB")
	if got := schedule.LimitAt(atTime(23, 0)); got != MB {
		t.Fatalf("LimitAt(23:00) = %d, want %d", got, MB)
	}
	if got := schedule.LimitAt(atTime(5, 59)); got != MB {
		t.Fatalf("LimitAt(05:59) = %d, want %d", got, MB)
	}
	if got := schedule.LimitAt(atTime(12, 0)); got != 0 {
		t.Fatalf("LimitAt(12:00) = %d, want unlimited", got)
	}
}

func TestParseSpeedSchedule_Invalid(t *testing.T) {
	for _, input := range []string{"", "09:00", "09:00-09:00:1MB", "09:00-17:00:0", "09:00-17:00:bogus", "25:00-17:00:1MB"} {
		if _, err := ParseSpeedSchedule(input); err == nil {
			t.Errorf("ParseSpeedSchedule(%q) expected error, got nil", input)
		}
	}
}

func TestCombineSpeedLimits(t *testing.T) {
	if got := CombineSpeedLimits(0, 0); got != 0 {
		t.Fatalf("CombineSpeedLimits(0,0) = %d, want 0", got)
	}
	if got := CombineSpeedLimits(100, 0); got != 100 {
		t.Fatalf("CombineSpeedLimits(100,0) = %d, want 100", got)
	}
	if got := CombineSpeedLimits(0, 200); got != 200 {
		t.Fatalf("CombineSpeedLimits(0,200) = %d, want 200", got)
	}
	if got := CombineSpeedLimits(100, 200); got != 100 {
		t.Fatalf("CombineSpeedLimits(100,200) = %d, want 100", got)
	}
}

func TestApplySpeedSchedule_PreservesBase(t *testing.T) {
	d := &Downloader{numBaseParts: 1, handlers: &Handlers{}}
	d.setSpeedLimits(2*MB, 2*MB)
	schedule := mustParseSchedule(t, "09:00-17:00:512KB")

	d.ApplySpeedSchedule(schedule, atTime(10, 0))
	if got := d.GetSpeedLimit(); got != 512*KB {
		t.Fatalf("in-window effective = %d, want %d", got, 512*KB)
	}
	// Applying twice must not ratchet: base still combines to the same value.
	d.ApplySpeedSchedule(schedule, atTime(10, 0))
	if got := d.GetSpeedLimit(); got != 512*KB {
		t.Fatalf("second in-window effective = %d, want %d", got, 512*KB)
	}
	d.ApplySpeedSchedule(schedule, atTime(18, 0))
	if got := d.GetSpeedLimit(); got != 2*MB {
		t.Fatalf("out-of-window effective = %d, want base %d", got, 2*MB)
	}
	if got := d.GetBaseSpeedLimit(); got != 2*MB {
		t.Fatalf("base = %d, want %d", got, 2*MB)
	}
}

func TestManagerApplySpeedSchedule_ThrottleAndLift(t *testing.T) {
	m := newTestManager(t)
	defer func() { _ = m.Close() }()
	schedule := mustParseSchedule(t, "09:00-17:00:512KB")
	m.SetSpeedSchedule(schedule)

	active := &Downloader{numBaseParts: 1, handlers: &Handlers{}}
	active.setSpeedLimits(2*MB, 2*MB)
	idle := &Downloader{numBaseParts: 1, handlers: &Handlers{}}
	idle.setSpeedLimits(2*MB, 2*MB)
	idle.Stop()

	activeItem, err := newItem(m.mu, "active.bin", "http://example.com/active", t.TempDir(), "active", 100, true, &itemOpts{
		TransferConfig: TransferConfig{SpeedLimit: 2 * MB},
	})
	if err != nil {
		t.Fatalf("newItem active: %v", err)
	}
	activeItem.setDAlloc(&httpProtocolDownloader{inner: active, rawURL: "http://example.com/active", probed: true})
	idleItem, err := newItem(m.mu, "idle.bin", "http://example.com/idle", t.TempDir(), "idle", 100, true, &itemOpts{
		TransferConfig: TransferConfig{SpeedLimit: 2 * MB},
	})
	if err != nil {
		t.Fatalf("newItem idle: %v", err)
	}
	idleItem.setDAlloc(&httpProtocolDownloader{inner: idle, rawURL: "http://example.com/idle", probed: true})
	m.items[activeItem.Hash] = activeItem
	m.items[idleItem.Hash] = idleItem

	// Acceptance 1: crossing into the window throttles the active download.
	m.ApplySpeedSchedule(atTime(9, 0))
	if got := active.GetSpeedLimit(); got != 512*KB {
		t.Fatalf("in-window active = %d, want %d", got, 512*KB)
	}
	// Acceptance 2: crossing out lifts back to the per-download base.
	m.ApplySpeedSchedule(atTime(17, 0))
	if got := active.GetSpeedLimit(); got != 2*MB {
		t.Fatalf("out-of-window active = %d, want base %d", got, 2*MB)
	}
	if got := activeItem.Snapshot().TransferConfig.SpeedLimit; got != 2*MB {
		t.Fatalf("persisted base = %d, want %d", got, 2*MB)
	}
}
