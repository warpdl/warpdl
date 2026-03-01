package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/adhocore/gronx"
)

const startAtLayout = "2006-01-02 15:04"

// parseStartAt validates and parses a --start-at value.
// Returns the parsed time or an error with the expected format.
func parseStartAt(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("error: invalid --start-at format, expected YYYY-MM-DD HH:MM")
	}
	t, err := time.ParseInLocation(startAtLayout, value, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("error: invalid --start-at format, expected YYYY-MM-DD HH:MM")
	}
	return t, nil
}

// validateStartAt checks whether the --start-at value is in the past.
// Returns:
//   - result: the original value if future, empty string if past or empty
//   - warning: non-empty warning message if the time is in the past
func validateStartAt(value string) (result, warning string) {
	if value == "" {
		return "", ""
	}
	t, err := parseStartAt(value)
	if err != nil {
		// Invalid format — caller should have validated first
		return value, ""
	}
	if t.Before(time.Now()) {
		return "", "warning: scheduled time is in the past, starting download immediately"
	}
	return value, ""
}

// parseStartIn validates a --start-in duration string and returns the resolved absolute time.
// Valid formats: Go duration syntax (e.g., "2h", "30m", "1h30m", "45s").
// Zero durations (0s, 0m, 0h) are valid and resolve to now (immediate start).
// Returns error for empty string or invalid duration formats.
func parseStartIn(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("error: invalid --start-in duration, expected format like 2h, 30m, or 1h30m (days not supported — use 24h)")
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("error: invalid --start-in duration, expected format like 2h, 30m, or 1h30m (days not supported — use 24h)")
	}
	return time.Now().Add(d), nil
}

// validateStartAtStartInExclusion checks that --start-at and --start-in are not both set.
// Returns an error if both are non-empty.
func validateStartAtStartInExclusion(startAt, startIn string) error {
	if startAt != "" && startIn != "" {
		return fmt.Errorf("error: flags --start-at and --start-in are mutually exclusive")
	}
	return nil
}

// validateSchedule checks if the --schedule cron expression is valid.
// Enforces exactly 5 fields (minute hour day-of-month month day-of-week).
// Returns error for invalid expressions (empty, wrong field count, invalid values).
func validateSchedule(expr string) error {
	if expr == "" {
		return fmt.Errorf("error: invalid cron expression %q, expected 5-field format (minute hour day-of-month month day-of-week)", expr)
	}
	// Enforce exactly 5 fields — gronx.IsValid also accepts 6-field (with seconds).
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("error: invalid cron expression %q, expected 5-field format (minute hour day-of-month month day-of-week)", expr)
	}
	if !gronx.IsValid(expr) {
		return fmt.Errorf("error: invalid cron expression %q, expected 5-field format (minute hour day-of-month month day-of-week)", expr)
	}
	return nil
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
