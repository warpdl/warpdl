package warplib

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// ParseSpeedLimit Tests
// =============================================================================

func TestParseSpeedLimit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		// Basic units
		{name: "bytes only", input: "100", expected: 100},
		{name: "zero", input: "0", expected: 0},
		{name: "kilobytes lowercase", input: "512kb", expected: 512 * KB},
		{name: "kilobytes uppercase", input: "512KB", expected: 512 * KB},
		{name: "megabytes lowercase", input: "1mb", expected: 1 * MB},
		{name: "megabytes uppercase", input: "1MB", expected: 1 * MB},
		{name: "gigabytes lowercase", input: "1gb", expected: 1 * GB},
		{name: "gigabytes uppercase", input: "1GB", expected: 1 * GB},

		// Decimal values
		{name: "decimal megabytes", input: "1.5MB", expected: int64(1.5 * float64(MB))},
		{name: "decimal kilobytes", input: "2.5KB", expected: int64(2.5 * float64(KB))},

		// Edge cases
		{name: "large value", input: "10GB", expected: 10 * GB},
		{name: "small value", input: "1B", expected: 1},
		{name: "with spaces trimmed", input: " 1MB ", expected: 1 * MB},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSpeedLimit(tt.input)
			if err != nil {
				t.Fatalf("ParseSpeedLimit(%q) returned error: %v", tt.input, err)
			}
			if got != tt.expected {
				t.Errorf("ParseSpeedLimit(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseSpeedLimit_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty string", input: ""},
		{name: "letters only", input: "abc"},
		{name: "unit only", input: "MB"},
		{name: "negative with unit", input: "-100MB"},
		{name: "invalid unit", input: "100XB"},
		{name: "multiple units", input: "100MBKB"},
		{name: "special characters", input: "100@MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSpeedLimit(tt.input)
			if err == nil {
				t.Errorf("ParseSpeedLimit(%q) expected error, got nil", tt.input)
			}
		})
	}
}

// =============================================================================
// RateLimitedReader Tests
// =============================================================================

func TestRateLimitedReader_ZeroLimitIsUnlimited(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1024)
	reader := NewRateLimitedReader(bytes.NewReader(data), 0)

	start := time.Now()
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	elapsed := time.Since(start)

	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1024 {
		t.Errorf("expected 1024 bytes, got %d", n)
	}
	// Should be nearly instant (< 100ms) since no throttling
	if elapsed > 100*time.Millisecond {
		t.Errorf("zero limit should not throttle, took %v", elapsed)
	}
}

func TestRateLimitedReader_NegativeLimitIsUnlimited(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1024)
	reader := NewRateLimitedReader(bytes.NewReader(data), -1)

	start := time.Now()
	buf := make([]byte, 1024)
	n, err := reader.Read(buf)
	elapsed := time.Since(start)

	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1024 {
		t.Errorf("expected 1024 bytes, got %d", n)
	}
	// Should be nearly instant (< 100ms) since no throttling
	if elapsed > 100*time.Millisecond {
		t.Errorf("negative limit should not throttle, took %v", elapsed)
	}
}

func TestRateLimitedReader_BasicThrottling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	// 10KB at 10KB/s should take ~1 second
	dataSize := 10 * int(KB)
	limit := 10 * KB
	data := bytes.Repeat([]byte("x"), dataSize)
	reader := NewRateLimitedReader(bytes.NewReader(data), limit)

	start := time.Now()
	buf := make([]byte, 1024)
	totalRead := 0
	for {
		n, err := reader.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	elapsed := time.Since(start)

	if totalRead != dataSize {
		t.Errorf("expected %d bytes, got %d", dataSize, totalRead)
	}

	// Should take approximately 1 second (allow 20% tolerance)
	expectedDuration := time.Second
	minDuration := time.Duration(float64(expectedDuration) * 0.8)
	maxDuration := time.Duration(float64(expectedDuration) * 1.5)

	if elapsed < minDuration || elapsed > maxDuration {
		t.Errorf("expected duration ~%v, got %v (tolerance: %v-%v)",
			expectedDuration, elapsed, minDuration, maxDuration)
	}
}

func TestRateLimitedReader_VeryLowLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timing test in short mode")
	}

	// 100 bytes at 100 bytes/s should take ~1 second
	dataSize := 100
	limit := int64(100)
	data := bytes.Repeat([]byte("x"), dataSize)
	reader := NewRateLimitedReader(bytes.NewReader(data), limit)

	start := time.Now()
	buf := make([]byte, 50)
	totalRead := 0
	for {
		n, err := reader.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	elapsed := time.Since(start)

	if totalRead != dataSize {
		t.Errorf("expected %d bytes, got %d", dataSize, totalRead)
	}

	// Should take approximately 1 second (allow 30% tolerance for low limits)
	expectedDuration := time.Second
	minDuration := time.Duration(float64(expectedDuration) * 0.7)
	maxDuration := time.Duration(float64(expectedDuration) * 1.6)

	if elapsed < minDuration || elapsed > maxDuration {
		t.Errorf("expected duration ~%v, got %v (tolerance: %v-%v)",
			expectedDuration, elapsed, minDuration, maxDuration)
	}
}

func TestRateLimitedReader_ReaderError(t *testing.T) {
	expectedErr := errors.New("test error")
	errReader := &errorReader{err: expectedErr}
	reader := NewRateLimitedReader(errReader, 1*MB)

	buf := make([]byte, 100)
	_, err := reader.Read(buf)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestRateLimitedReader_EOF(t *testing.T) {
	data := []byte("hello")
	reader := NewRateLimitedReader(bytes.NewReader(data), 0)

	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	if n != 5 {
		t.Errorf("expected 5 bytes, got %d", n)
	}

	// Second read should return EOF
	n, err = reader.Read(buf)
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes on EOF, got %d", n)
	}
}

func TestRateLimitedReader_ConcurrentReads(t *testing.T) {
	var wg sync.WaitGroup
	const numGoroutines = 10

	// This test verifies no race conditions with concurrent SetLimit calls
	// Each goroutine gets its own reader, but they share access to SetLimit
	// Run with -race flag to detect races
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine gets its own underlying reader and rate limiter
			data := bytes.Repeat([]byte("x"), 1000)
			reader := NewRateLimitedReader(bytes.NewReader(data), 0) // unlimited for speed
			buf := make([]byte, 100)
			for j := 0; j < 10; j++ {
				reader.Read(buf)
				// Also test concurrent limit updates
				if j%2 == 0 {
					reader.SetLimit(int64(j * 1000))
				}
			}
		}()
	}

	wg.Wait()
}

func TestRateLimitedReader_UpdateLimit(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1024)
	reader := NewRateLimitedReader(bytes.NewReader(data), 0)

	// Start with no limit
	buf := make([]byte, 100)
	_, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}

	// Update to a limit
	reader.SetLimit(1 * MB)

	// Should still work
	_, err = reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error after SetLimit: %v", err)
	}

	// Update back to unlimited
	reader.SetLimit(0)

	_, err = reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error after SetLimit(0): %v", err)
	}
}

func TestRateLimitedReader_PartialRead(t *testing.T) {
	// Reader that returns fewer bytes than requested
	partialReader := &partialReader{data: bytes.Repeat([]byte("x"), 1000), chunkSize: 10}
	reader := NewRateLimitedReader(partialReader, 0)

	buf := make([]byte, 100)
	n, err := reader.Read(buf)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 10 {
		t.Errorf("expected 10 bytes (partial), got %d", n)
	}
}

func TestRateLimitedReader_LargeBuffer(t *testing.T) {
	dataSize := 10 * int(MB)
	data := bytes.Repeat([]byte("x"), dataSize)
	reader := NewRateLimitedReader(bytes.NewReader(data), 0) // unlimited

	buf := make([]byte, dataSize)
	n, err := reader.Read(buf)

	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != dataSize {
		t.Errorf("expected %d bytes, got %d", dataSize, n)
	}
}

func TestRateLimitedReader_SmallBuffer(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	reader := NewRateLimitedReader(bytes.NewReader(data), 0)

	buf := make([]byte, 1) // tiny buffer
	totalRead := 0
	for {
		n, err := reader.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if totalRead != 100 {
		t.Errorf("expected 100 bytes total, got %d", totalRead)
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

// errorReader always returns an error
type errorReader struct {
	err error
}

func (r *errorReader) Read(b []byte) (int, error) {
	return 0, r.err
}

// partialReader returns only chunkSize bytes per read
type partialReader struct {
	data      []byte
	pos       int
	chunkSize int
}

func (r *partialReader) Read(b []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	end := r.pos + r.chunkSize
	if end > len(r.data) {
		end = len(r.data)
	}
	if end > r.pos+len(b) {
		end = r.pos + len(b)
	}

	n := copy(b, r.data[r.pos:end])
	r.pos += n
	return n, nil
}

// =============================================================================
// RateLimitedReadCloser Tests (wrapper for io.ReadCloser)
// =============================================================================

func TestRateLimitedReadCloser_Close(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 100)
	underlying := io.NopCloser(bytes.NewReader(data))
	reader := NewRateLimitedReadCloser(underlying, 0)

	// Read some data
	buf := make([]byte, 50)
	_, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}

	// Close should work
	err = reader.Close()
	if err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestRateLimitedReadCloser_CloseError(t *testing.T) {
	expectedErr := errors.New("close error")
	underlying := &errorCloser{err: expectedErr}
	reader := NewRateLimitedReadCloser(underlying, 0)

	err := reader.Close()
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

// errorCloser returns an error on Close
type errorCloser struct {
	io.Reader
	err error
}

func (c *errorCloser) Read(b []byte) (int, error) {
	return 0, io.EOF
}

func (c *errorCloser) Close() error {
	return c.err
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkRateLimitedReader_Unlimited(b *testing.B) {
	data := bytes.Repeat([]byte("x"), 1*int(MB))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewRateLimitedReader(bytes.NewReader(data), 0)
		io.Copy(io.Discard, reader)
	}
}

func BenchmarkRateLimitedReader_Limited(b *testing.B) {
	// Use a high limit so benchmark completes quickly
	data := bytes.Repeat([]byte("x"), 1*int(KB))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewRateLimitedReader(bytes.NewReader(data), 100*MB)
		io.Copy(io.Discard, reader)
	}
}

func BenchmarkParseSpeedLimit(b *testing.B) {
	inputs := []string{"1MB", "512KB", "1.5GB", "100", "10GB"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			ParseSpeedLimit(input)
		}
	}
}

// Ensure strings package is used (for linter)
var _ = strings.TrimSpace
