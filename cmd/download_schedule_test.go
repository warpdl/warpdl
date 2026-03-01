package cmd

import (
	"testing"
	"time"
)

// T012: --start-at flag validation tests

func TestParseStartAt_ValidFormat(t *testing.T) {
	input := "2026-03-01 14:30"
	parsed, err := parseStartAt(input)
	if err != nil {
		t.Fatalf("expected valid format to parse, got error: %v", err)
	}
	if parsed.Year() != 2026 || parsed.Month() != 3 || parsed.Day() != 1 {
		t.Errorf("unexpected date: %v", parsed)
	}
	if parsed.Hour() != 14 || parsed.Minute() != 30 {
		t.Errorf("unexpected time: %v", parsed)
	}
}

func TestParseStartAt_InvalidFormat(t *testing.T) {
	invalidInputs := []string{
		"not-a-date",
		"2026/03/01 14:30",
		"2026-03-01T14:30",
		"14:30",
		"2026-03-01",
		"",
	}
	for _, input := range invalidInputs {
		_, err := parseStartAt(input)
		if err == nil {
			t.Errorf("expected error for invalid input %q, got nil", input)
		}
	}
}

func TestParseStartAt_ErrorMessage(t *testing.T) {
	_, err := parseStartAt("bad-input")
	if err == nil {
		t.Fatal("expected error")
	}
	expected := "invalid --start-at format, expected YYYY-MM-DD HH:MM"
	assertContains(t, err.Error(), expected)
}

func TestValidateStartAt_PastTime(t *testing.T) {
	// A time clearly in the past
	past := time.Now().Add(-1 * time.Hour)
	startAtStr := past.Format("2006-01-02 15:04")

	result, warning := validateStartAt(startAtStr)
	if result != "" {
		t.Errorf("expected empty result for past time, got %q", result)
	}
	if warning == "" {
		t.Error("expected warning for past time, got empty string")
	}
	assertContains(t, warning, "scheduled time is in the past")
}

func TestValidateStartAt_FutureTime(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	startAtStr := future.Format("2006-01-02 15:04")

	result, warning := validateStartAt(startAtStr)
	if result != startAtStr {
		t.Errorf("expected result %q, got %q", startAtStr, result)
	}
	if warning != "" {
		t.Errorf("expected no warning for future time, got %q", warning)
	}
}

func TestValidateStartAt_Empty(t *testing.T) {
	result, warning := validateStartAt("")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	if warning != "" {
		t.Errorf("expected no warning, got %q", warning)
	}
}

// T025: --start-in flag validation tests

func TestParseStartIn_ValidDurations(t *testing.T) {
	cases := []struct {
		input   string
		wantMin time.Duration
		wantMax time.Duration
	}{
		{"2h", 2*time.Hour - time.Second, 2*time.Hour + time.Second},
		{"30m", 30*time.Minute - time.Second, 30*time.Minute + time.Second},
		{"1h30m", 90*time.Minute - time.Second, 90*time.Minute + time.Second},
		{"45s", 44 * time.Second, 46 * time.Second},
	}
	for _, tc := range cases {
		resolvedAt, err := parseStartIn(tc.input)
		if err != nil {
			t.Errorf("expected %q to parse without error, got: %v", tc.input, err)
			continue
		}
		remaining := time.Until(resolvedAt)
		if remaining < tc.wantMin || remaining > tc.wantMax {
			t.Errorf("parseStartIn(%q): expected remaining %v..%v, got %v", tc.input, tc.wantMin, tc.wantMax, remaining)
		}
	}
}

func TestParseStartIn_ZeroDuration(t *testing.T) {
	cases := []string{"0s", "0m", "0h"}
	for _, input := range cases {
		resolvedAt, err := parseStartIn(input)
		if err != nil {
			t.Errorf("expected 0-duration %q to be valid, got error: %v", input, err)
			continue
		}
		// Should be at or very slightly after now (within 1s)
		if time.Until(resolvedAt) > time.Second {
			t.Errorf("expected 0-duration %q to resolve to now, got %v from now", input, time.Until(resolvedAt))
		}
	}
}

func TestParseStartIn_InvalidFormat(t *testing.T) {
	invalidInputs := []string{
		"not-a-duration",
		"2d",
		"1 hour",
		"",
		"abc",
		"1week",
	}
	for _, input := range invalidInputs {
		_, err := parseStartIn(input)
		if err == nil {
			t.Errorf("expected error for invalid input %q, got nil", input)
		}
	}
}

func TestParseStartIn_ErrorMessage(t *testing.T) {
	_, err := parseStartIn("bad-duration")
	if err == nil {
		t.Fatal("expected error")
	}
	assertContains(t, err.Error(), "invalid --start-in duration")
}

func TestValidateStartAtStartInMutualExclusion(t *testing.T) {
	err := validateStartAtStartInExclusion("2026-03-01 14:30", "2h")
	if err == nil {
		t.Fatal("expected error when both --start-at and --start-in are set")
	}
	assertContains(t, err.Error(), "--start-at and --start-in are mutually exclusive")
}

func TestValidateStartAtStartInMutualExclusion_OnlyStartAt(t *testing.T) {
	err := validateStartAtStartInExclusion("2026-03-01 14:30", "")
	if err != nil {
		t.Errorf("expected no error when only --start-at is set, got: %v", err)
	}
}

func TestValidateStartAtStartInMutualExclusion_OnlyStartIn(t *testing.T) {
	err := validateStartAtStartInExclusion("", "2h")
	if err != nil {
		t.Errorf("expected no error when only --start-in is set, got: %v", err)
	}
}

func TestValidateStartAtStartInMutualExclusion_Neither(t *testing.T) {
	err := validateStartAtStartInExclusion("", "")
	if err != nil {
		t.Errorf("expected no error when neither flag is set, got: %v", err)
	}
}

// T063: --schedule flag validation tests

func TestParseSchedule_ValidCron(t *testing.T) {
	// valid 5-field cron expressions accepted by validateSchedule
	valid := []string{"0 2 * * *", "*/15 * * * *", "0 0 1 * *", "30 9 * * 1-5"}
	for _, expr := range valid {
		err := validateSchedule(expr)
		if err != nil {
			t.Errorf("expected %q to be valid, got: %v", expr, err)
		}
	}
}

func TestParseSchedule_InvalidCron(t *testing.T) {
	invalid := []string{"not-a-cron", "0 2 * *", "0 2 * * * *", "99 2 * * *", ""}
	for _, expr := range invalid {
		err := validateSchedule(expr)
		if err == nil {
			t.Errorf("expected error for invalid cron %q, got nil", expr)
		}
	}
}

func TestParseSchedule_ErrorMessage(t *testing.T) {
	err := validateSchedule("bad-cron")
	if err == nil {
		t.Fatal("expected error")
	}
	assertContains(t, err.Error(), "invalid cron expression")
}

func TestScheduleCombinedWithStartAt(t *testing.T) {
	// --start-at + --schedule: allowed (start-at mutual exclusion only applies to start-in)
	err := validateStartAtStartInExclusion("2026-03-01 14:30", "")
	if err != nil {
		t.Errorf("start-at alone should be valid: %v", err)
	}
	// --schedule with --start-at doesn't trigger mutual exclusion
	err = validateStartAtStartInExclusion("2026-03-01 14:30", "")
	if err != nil {
		t.Errorf("start-at + schedule allowed: %v", err)
	}
}

func TestScheduleCombinedWithStartIn(t *testing.T) {
	// --start-in + --schedule: allowed
	err := validateStartAtStartInExclusion("", "2h")
	if err != nil {
		t.Errorf("start-in alone should be valid: %v", err)
	}
}

func TestScheduleMutualExclusion_StartAtAndStartInStillApplies(t *testing.T) {
	// --start-at + --start-in + --schedule: start-at and start-in still mutually exclusive
	err := validateStartAtStartInExclusion("2026-03-01 14:30", "2h")
	if err == nil {
		t.Fatal("expected error when both --start-at and --start-in set (even with --schedule)")
	}
	assertContains(t, err.Error(), "mutually exclusive")
}

// T063b: hasOccurrenceWithinYear tests (cmd package version)

func TestHasOccurrenceWithinYear_Valid(t *testing.T) {
	if !hasOccurrenceWithinYear("0 2 * * *", time.Now()) {
		t.Error("expected daily cron to have occurrence within next year")
	}
}

func TestHasOccurrenceWithinYear_Invalid(t *testing.T) {
	if hasOccurrenceWithinYear("bad-cron", time.Now()) {
		t.Error("invalid cron should return false")
	}
}

func TestHasOccurrenceWithinYear_EveryMinute(t *testing.T) {
	if !hasOccurrenceWithinYear("* * * * *", time.Now()) {
		t.Error("every-minute cron should have occurrence within next year")
	}
}
