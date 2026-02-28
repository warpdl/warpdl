package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// T029/T052/T071: formatCountdown and formatScheduleColumn tests

func TestFormatCountdown_HoursAndMinutes(t *testing.T) {
	got := formatCountdown(2*time.Hour + 30*time.Minute)
	if got != "in 2h30m" {
		t.Errorf("expected 'in 2h30m', got %q", got)
	}
}

func TestFormatCountdown_HoursOnly(t *testing.T) {
	got := formatCountdown(3 * time.Hour)
	if got != "in 3h" {
		t.Errorf("expected 'in 3h', got %q", got)
	}
}

func TestFormatCountdown_MinutesOnly(t *testing.T) {
	got := formatCountdown(45 * time.Minute)
	if got != "in 45m" {
		t.Errorf("expected 'in 45m', got %q", got)
	}
}

func TestFormatCountdown_SecondsOnly(t *testing.T) {
	got := formatCountdown(30 * time.Second)
	if got != "in 30s" {
		t.Errorf("expected 'in 30s', got %q", got)
	}
}

func TestFormatCountdown_Zero(t *testing.T) {
	got := formatCountdown(0)
	if got != "now" {
		t.Errorf("expected 'now', got %q", got)
	}
}

func TestFormatCountdown_Negative(t *testing.T) {
	got := formatCountdown(-time.Second)
	if got != "now" {
		t.Errorf("expected 'now' for negative duration, got %q", got)
	}
}

func TestFormatScheduleColumn_Missed_WithTime(t *testing.T) {
	scheduledAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateMissed,
		ScheduledAt:   scheduledAt,
	}
	got := formatScheduleColumn(item)
	if !strings.HasPrefix(got, "was ") {
		t.Errorf("expected 'was ...' prefix, got %q", got)
	}
	if !strings.HasSuffix(got, " (starting now)") {
		t.Errorf("expected '(starting now)' suffix, got %q", got)
	}
	if !strings.Contains(got, "2026-01-01 12:00") {
		t.Errorf("expected formatted date in output, got %q", got)
	}
}

func TestFormatScheduleColumn_Missed_NoTime(t *testing.T) {
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateMissed,
	}
	got := formatScheduleColumn(item)
	if got != "missed" {
		t.Errorf("expected 'missed', got %q", got)
	}
}

func TestFormatScheduleColumn_Recurring_WithNextTime(t *testing.T) {
	scheduledAt := time.Date(2026, 3, 15, 2, 0, 0, 0, time.Local)
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateScheduled,
		CronExpr:      "0 2 * * *",
		ScheduledAt:   scheduledAt,
	}
	got := formatScheduleColumn(item)
	if !strings.Contains(got, "recurring: 0 2 * * *") {
		t.Errorf("expected cron expr in output, got %q", got)
	}
	if !strings.Contains(got, "next:") {
		t.Errorf("expected 'next:' in output, got %q", got)
	}
}

func TestFormatScheduleColumn_Recurring_NoNextTime(t *testing.T) {
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateScheduled,
		CronExpr:      "*/30 * * * *",
	}
	got := formatScheduleColumn(item)
	expected := "(recurring: */30 * * * *)"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestFormatScheduleColumn_Scheduled_Within24h(t *testing.T) {
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateScheduled,
		ScheduledAt:   time.Now().Add(2 * time.Hour),
	}
	got := formatScheduleColumn(item)
	if !strings.HasPrefix(got, "in ") {
		t.Errorf("expected countdown prefix 'in ', got %q", got)
	}
}

func TestFormatScheduleColumn_Scheduled_Beyond24h(t *testing.T) {
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateScheduled,
		ScheduledAt:   time.Now().Add(48 * time.Hour),
	}
	got := formatScheduleColumn(item)
	// Should return date format "01-02 15:04", not a countdown
	if strings.HasPrefix(got, "in ") {
		t.Errorf("expected date format for > 24h, got countdown: %q", got)
	}
	if got == "\u2014" {
		t.Errorf("expected date string for future scheduled time, got em dash")
	}
}

func TestFormatScheduleColumn_Scheduled_PastTime(t *testing.T) {
	// remaining <= 0 falls through to Format("01-02 15:04")
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateScheduled,
		ScheduledAt:   time.Now().Add(-5 * time.Minute),
	}
	got := formatScheduleColumn(item)
	if strings.HasPrefix(got, "in ") {
		t.Errorf("expected date format for past time, got countdown: %q", got)
	}
}

func TestFormatScheduleColumn_Scheduled_ZeroTime(t *testing.T) {
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateScheduled,
	}
	got := formatScheduleColumn(item)
	if got != "\u2014" {
		t.Errorf("expected em dash for scheduled with zero time, got %q", got)
	}
}

func TestFormatScheduleColumn_Default(t *testing.T) {
	item := &warplib.Item{}
	got := formatScheduleColumn(item)
	if got != "\u2014" {
		t.Errorf("expected em dash for default state, got %q", got)
	}
}

func TestFormatScheduleColumn_Cancelled(t *testing.T) {
	item := &warplib.Item{
		ScheduleState: warplib.ScheduleStateCancelled,
		ScheduledAt:   time.Now().Add(time.Hour),
	}
	got := formatScheduleColumn(item)
	if got != "\u2014" {
		t.Errorf("expected em dash for cancelled state, got %q", got)
	}
}
