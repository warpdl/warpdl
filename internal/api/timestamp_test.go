package api

import (
	"testing"
	"time"
)

// T065: timestamp suffix tests

func TestApplyTimestampSuffix_WithExtension(t *testing.T) {
	ts := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	result := applyTimestampSuffix("backup.tar.gz", ts)
	expected := "backup.tar-2026-03-01T020000.gz"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestApplyTimestampSuffix_NoExtension(t *testing.T) {
	ts := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	result := applyTimestampSuffix("backup", ts)
	expected := "backup-2026-03-01T020000"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestApplyTimestampSuffix_MultipleDots(t *testing.T) {
	ts := time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC)
	result := applyTimestampSuffix("my.file.name.zip", ts)
	// Last extension only: .zip
	expected := "my.file.name-2026-03-01T020000.zip"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestApplyTimestampSuffix_SingleExtension(t *testing.T) {
	ts := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	result := applyTimestampSuffix("file.bin", ts)
	expected := "file-2026-01-15T093000.bin"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestApplyTimestampSuffix_TimestampFormat(t *testing.T) {
	// Verify that midnight is formatted as T000000, not with leading zeros missing
	ts := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	result := applyTimestampSuffix("data.csv", ts)
	expected := "data-2026-12-31T000000.csv"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}
