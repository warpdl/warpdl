package warplib

import (
    "fmt"
    "io"
    "strconv"
    "strings"
    "sync"
    "time"
)

// RateLimitedReader wraps an io.Reader and limits the read rate.
// A limit of 0 or negative means unlimited (no throttling).
type RateLimitedReader struct {
    r        io.Reader
    limit    int64 // bytes per second, 0 or negative = unlimited
    mu       sync.Mutex
    lastRead time.Time
    tokens   int64 // available tokens (bytes)
}

// NewRateLimitedReader creates a rate-limited reader.
// limit is in bytes per second. 0 or negative means unlimited.
func NewRateLimitedReader(r io.Reader, limit int64) *RateLimitedReader {
    return &RateLimitedReader{
        r:        r,
        limit:    limit,
        lastRead: time.Now(),
        tokens:   0, // start with empty bucket - no initial burst
    }
}

// Read implements io.Reader with rate limiting using a token bucket algorithm.
func (r *RateLimitedReader) Read(b []byte) (n int, err error) {
    // No limit - pass through directly
    if r.limit <= 0 {
        return r.r.Read(b)
    }

    r.mu.Lock()

    // Refill tokens based on elapsed time
    now := time.Now()
    elapsed := now.Sub(r.lastRead)
    r.lastRead = now

    // Add tokens for elapsed time (bytes per second * seconds elapsed)
    tokensToAdd := int64(float64(r.limit) * elapsed.Seconds())
    r.tokens += tokensToAdd

    // Cap tokens at limit (1 second worth of data max burst)
    if r.tokens > r.limit {
        r.tokens = r.limit
    }

    // Determine how many bytes we want to read
    wantToRead := int64(len(b))
    if wantToRead > r.limit {
        wantToRead = r.limit // never read more than 1 second worth
    }

    // If we don't have enough tokens, calculate wait time
    if r.tokens < wantToRead {
        // How many more tokens do we need?
        needed := wantToRead - r.tokens
        // How long to wait for those tokens?
        waitTime := time.Duration(float64(time.Second) * float64(needed) / float64(r.limit))

        if waitTime > 0 {
            r.mu.Unlock()
            time.Sleep(waitTime)
            r.mu.Lock()

            // After sleeping, recalculate tokens
            now = time.Now()
            elapsed = now.Sub(r.lastRead)
            r.lastRead = now
            tokensToAdd = int64(float64(r.limit) * elapsed.Seconds())
            r.tokens += tokensToAdd
            if r.tokens > r.limit {
                r.tokens = r.limit
            }
        }
    }

    // Limit read size to available tokens (but at least 1 byte)
    readSize := int(wantToRead)
    if r.tokens > 0 && int64(readSize) > r.tokens {
        readSize = int(r.tokens)
    }
    if readSize <= 0 {
        readSize = 1
    }

    r.mu.Unlock()

    // Perform the actual read (outside the lock)
    n, err = r.r.Read(b[:readSize])

    // Consume tokens
    r.mu.Lock()
    r.tokens -= int64(n)
    r.mu.Unlock()

    return n, err
}

// SetLimit updates the rate limit dynamically.
// 0 or negative means unlimited.
func (r *RateLimitedReader) SetLimit(limit int64) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.limit = limit
    if limit > 0 && r.tokens > limit {
        r.tokens = limit
    }
}

// GetLimit returns the current rate limit in bytes per second.
func (r *RateLimitedReader) GetLimit() int64 {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.limit
}

// RateLimitedReadCloser wraps an io.ReadCloser with rate limiting.
type RateLimitedReadCloser struct {
    *RateLimitedReader
    closer io.Closer
}

// NewRateLimitedReadCloser creates a rate-limited ReadCloser.
// limit is in bytes per second. 0 or negative means unlimited.
func NewRateLimitedReadCloser(rc io.ReadCloser, limit int64) *RateLimitedReadCloser {
    return &RateLimitedReadCloser{
        RateLimitedReader: NewRateLimitedReader(rc, limit),
        closer:            rc,
    }
}

// Close closes the underlying ReadCloser.
func (r *RateLimitedReadCloser) Close() error {
    return r.closer.Close()
}

// ParseSpeedLimit parses a human-readable speed limit string.
// Returns bytes per second. 0 means unlimited.
//
// Supported formats:
//   - Plain bytes: "100", "1024"
//   - With B suffix: "100B", "1024B"
//   - Kilobytes: "512KB", "512kb"
//   - Megabytes: "1MB", "1.5mb"
//   - Gigabytes: "1GB", "2.5gb"
//
// Returns an error for invalid formats.
func ParseSpeedLimit(s string) (int64, error) {
    s = strings.TrimSpace(s)
    if s == "" {
        return 0, fmt.Errorf("empty speed limit")
    }

    // Handle "0" special case
    if s == "0" {
        return 0, nil
    }

    // Try to find the numeric part and unit part
    s = strings.ToUpper(s)

    var numStr string
    var unit string

    // Find where the number ends and unit begins
    for i, c := range s {
        if (c < '0' || c > '9') && c != '.' && c != '-' {
            numStr = s[:i]
            unit = s[i:]
            break
        }
    }

    // If no unit found, it's just a number
    if numStr == "" {
        numStr = s
        unit = ""
    }

    // Parse the numeric part
    if numStr == "" {
        return 0, fmt.Errorf("invalid speed limit: no numeric value in %q", s)
    }

    // Check for negative values
    if strings.HasPrefix(numStr, "-") {
        return 0, fmt.Errorf("invalid speed limit: negative value not allowed in %q", s)
    }

    // Parse as float to support decimals
    num, err := strconv.ParseFloat(numStr, 64)
    if err != nil {
        return 0, fmt.Errorf("invalid speed limit: %q is not a valid number", numStr)
    }

    // Apply multiplier based on unit
    var multiplier int64
    switch unit {
    case "", "B":
        multiplier = B
    case "KB", "K":
        multiplier = KB
    case "MB", "M":
        multiplier = MB
    case "GB", "G":
        multiplier = GB
    default:
        return 0, fmt.Errorf("invalid speed limit unit: %q (use B, KB, MB, or GB)", unit)
    }

    result := int64(num * float64(multiplier))

    // Sanity check
    if result < 0 {
        return 0, fmt.Errorf("invalid speed limit: result is negative")
    }

    return result, nil
}
