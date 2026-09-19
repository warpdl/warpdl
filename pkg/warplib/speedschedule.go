package warplib

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SpeedSchedule throttles all downloads during a daily local-time window.
// Outside the window the limit is lifted (0 = unlimited).
type SpeedSchedule struct {
	startMin int   // minutes since midnight, [0,1440)
	endMin   int   // minutes since midnight, [0,1440)
	limit    int64 // bytes per second applied inside the window
}

// String renders the window back in flag form for daemon startup logs.
func (s *SpeedSchedule) String() string {
	if s == nil {
		return "none"
	}
	return fmt.Sprintf("%02d:%02d-%02d:%02d:%d", s.startMin/60, s.startMin%60, s.endMin/60, s.endMin%60, s.limit)
}

// ParseSpeedSchedule parses a daily throttle rule of the form
// "HH:MM-HH:MM:SPEED" (e.g. "09:00-17:00:512KB").
//
// The window recurs every day in local time. When the end is earlier than
// the start the window wraps past midnight (e.g. "22:00-06:00:1MB").
// The speed reuses ParseSpeedLimit and must be positive.
func ParseSpeedSchedule(s string) (*SpeedSchedule, error) {
	s = strings.TrimSpace(s)
	dash := strings.Index(s, "-")
	if dash < 0 {
		return nil, fmt.Errorf("invalid speed schedule %q: want HH:MM-HH:MM:SPEED", s)
	}
	startStr := strings.TrimSpace(s[:dash])
	rest := strings.TrimSpace(s[dash+1:])
	colon := strings.LastIndex(rest, ":")
	if colon < 0 {
		return nil, fmt.Errorf("invalid speed schedule %q: want HH:MM-HH:MM:SPEED", s)
	}
	endStr := strings.TrimSpace(rest[:colon])
	speedStr := strings.TrimSpace(rest[colon+1:])
	start, err := parseScheduleHM(startStr)
	if err != nil {
		return nil, fmt.Errorf("invalid speed schedule %q: %w", s, err)
	}
	end, err := parseScheduleHM(endStr)
	if err != nil {
		return nil, fmt.Errorf("invalid speed schedule %q: %w", s, err)
	}
	if start == end {
		return nil, fmt.Errorf("invalid speed schedule %q: start and end are equal", s)
	}
	limit, err := ParseSpeedLimit(speedStr)
	if err != nil {
		return nil, fmt.Errorf("invalid speed schedule %q: %w", s, err)
	}
	if limit <= 0 {
		return nil, fmt.Errorf("invalid speed schedule %q: speed must be positive", s)
	}
	return &SpeedSchedule{startMin: start, endMin: end, limit: limit}, nil
}
func parseScheduleHM(s string) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok {
		return 0, fmt.Errorf("invalid time %q: want HH:MM", s)
	}
	hi, err := strconv.Atoi(h)
	if err != nil || hi < 0 || hi > 23 {
		return 0, fmt.Errorf("invalid hour %q: want 00-23", s)
	}
	mi, err := strconv.Atoi(m)
	if err != nil || mi < 0 || mi > 59 {
		return 0, fmt.Errorf("invalid minute %q: want 00-59", s)
	}
	return hi*60 + mi, nil
}

// LimitAt returns the schedule's limit at t, or 0 when t falls outside the
// window. The window covers [start, end); a nil schedule never throttles.
func (s *SpeedSchedule) LimitAt(t time.Time) int64 {
	if s == nil {
		return 0
	}
	mins := t.Hour()*60 + t.Minute()
	if s.startMin <= s.endMin {
		if mins >= s.startMin && mins < s.endMin {
			return s.limit
		}
		return 0
	}
	if mins >= s.startMin || mins < s.endMin {
		return s.limit
	}
	return 0
}

// CombineSpeedLimits returns the most restrictive of two limits where 0 (or
// negative) means unlimited: the smaller positive value wins, a lone
// positive value wins over unlimited, and 0 means no cap at all.
func CombineSpeedLimits(a, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 {
		return a
	}
	if a < b {
		return a
	}
	return b
}
