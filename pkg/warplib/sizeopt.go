package warplib

import (
	"strconv"
	"strings"
)

// SizeOption provides size unit conversion and formatting utilities.
// It holds a value representing the unit size and a format string for display.
type SizeOption struct {
	val int64
	fmt string
}

func (s *SizeOption) string(l int64) string {
	return strings.Join(
		[]string{
			strconv.FormatInt(l, 10),
			s.fmt,
		}, "",
	)
}

// Get divides the ContentLength by the unit size and returns the quotient and remainder.
func (s *SizeOption) Get(l ContentLength) (siz, rem int64) {
	siz = l.v() / s.val
	rem = l.v() % s.val
	return
}

// GetFrom divides the given int64 value by the unit size and returns the quotient and remainder.
func (s *SizeOption) GetFrom(l int64) (siz, rem int64) {
	siz = l / s.val
	rem = l % s.val
	return
}

// String returns the ContentLength formatted as a string with the unit suffix.
func (s *SizeOption) String(l ContentLength) string {
	siz, _ := s.Get(l)
	return s.string(siz)
}

// StringFrom returns the given int64 value formatted as a string with the unit suffix.
func (s *SizeOption) StringFrom(l int64) string {
	return s.string(l)
}

var (
	// SizeOptionBy is a SizeOption configured for bytes.
	SizeOptionBy = SizeOption{B, "Bytes"}
	// SizeOptionKB is a SizeOption configured for kilobytes.
	SizeOptionKB = SizeOption{KB, "KB"}
	// SizeOptionMB is a SizeOption configured for megabytes.
	SizeOptionMB = SizeOption{MB, "MB"}
	// SizeOptionGB is a SizeOption configured for gigabytes.
	SizeOptionGB = SizeOption{GB, "GB"}
	// SizeOptionTB is a SizeOption configured for terabytes.
	SizeOptionTB = SizeOption{TB, "TB"}
)
