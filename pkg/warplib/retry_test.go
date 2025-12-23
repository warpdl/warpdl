package warplib

import (
    "context"
    "errors"
    "fmt"
    "io"
    "math"
    "net"
    "net/url"
    "syscall"
    "testing"
    "time"
)

func TestClassifyError(t *testing.T) {
    tests := []struct {
        name     string
        err      error
        expected ErrorCategory
    }{
        // Fatal errors
        {
            name:     "nil error",
            err:      nil,
            expected: ErrCategoryFatal,
        },
        {
            name:     "context.Canceled",
            err:      context.Canceled,
            expected: ErrCategoryFatal,
        },
        {
            name:     "context.DeadlineExceeded",
            err:      context.DeadlineExceeded,
            expected: ErrCategoryFatal,
        },
        {
            name:     "unknown error",
            err:      errors.New("some random error"),
            expected: ErrCategoryFatal,
        },

        // Retryable errors - io errors
        {
            name:     "io.EOF",
            err:      io.EOF,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "io.ErrUnexpectedEOF",
            err:      io.ErrUnexpectedEOF,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "wrapped EOF",
            err:      fmt.Errorf("wrap: %w", io.EOF),
            expected: ErrCategoryRetryable,
        },
        {
            name:     "wrapped ErrUnexpectedEOF",
            err:      fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", io.ErrUnexpectedEOF)),
            expected: ErrCategoryRetryable,
        },

        // Retryable errors - syscall errors
        {
            name:     "syscall.ECONNRESET",
            err:      syscall.ECONNRESET,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "syscall.ECONNREFUSED",
            err:      syscall.ECONNREFUSED,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "syscall.ECONNABORTED",
            err:      syscall.ECONNABORTED,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "syscall.ETIMEDOUT",
            err:      syscall.ETIMEDOUT,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "syscall.ENETUNREACH",
            err:      syscall.ENETUNREACH,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "syscall.EHOSTUNREACH",
            err:      syscall.EHOSTUNREACH,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "syscall.EPIPE",
            err:      syscall.EPIPE,
            expected: ErrCategoryRetryable,
        },
        {
            name:     "wrapped ECONNRESET",
            err:      fmt.Errorf("network error: %w", syscall.ECONNRESET),
            expected: ErrCategoryRetryable,
        },

        // Retryable errors - string pattern matching
        {
            name:     "timeout in message",
            err:      errors.New("request timeout exceeded"),
            expected: ErrCategoryRetryable,
        },
        {
            name:     "connection reset in message",
            err:      errors.New("connection reset by peer"),
            expected: ErrCategoryRetryable,
        },
        {
            name:     "broken pipe in message",
            err:      errors.New("broken pipe"),
            expected: ErrCategoryRetryable,
        },
        {
            name:     "temporary failure in message",
            err:      errors.New("temporary failure in name resolution"),
            expected: ErrCategoryRetryable,
        },
        {
            name:     "network unreachable in message",
            err:      errors.New("network is unreachable"),
            expected: ErrCategoryRetryable,
        },
        {
            name:     "case insensitive timeout",
            err:      errors.New("TIMEOUT occurred"),
            expected: ErrCategoryRetryable,
        },

        // Throttled errors - string pattern matching
        {
            name:     "429 in message",
            err:      errors.New("HTTP 429 Too Many Requests"),
            expected: ErrCategoryThrottled,
        },
        {
            name:     "503 in message",
            err:      errors.New("HTTP 503 Service Unavailable"),
            expected: ErrCategoryThrottled,
        },
        {
            name:     "too many requests",
            err:      errors.New("too many requests"),
            expected: ErrCategoryThrottled,
        },
        {
            name:     "service unavailable",
            err:      errors.New("service unavailable"),
            expected: ErrCategoryThrottled,
        },
        {
            name:     "rate limit",
            err:      errors.New("rate limit exceeded"),
            expected: ErrCategoryThrottled,
        },
        {
            name:     "throttled",
            err:      errors.New("request throttled"),
            expected: ErrCategoryThrottled,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := ClassifyError(tt.err)
            if got != tt.expected {
                t.Errorf("ClassifyError(%v) = %v, want %v", tt.err, got, tt.expected)
            }
        })
    }
}

func TestCalculateBackoff(t *testing.T) {
    t.Run("exponential backoff calculation", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      10 * time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0, // No jitter for deterministic tests
        }

        tests := []struct {
            attempt  int
            expected time.Duration
        }{
            {attempt: 1, expected: 100 * time.Millisecond},  // 100ms * 2^0 = 100ms
            {attempt: 2, expected: 200 * time.Millisecond},  // 100ms * 2^1 = 200ms
            {attempt: 3, expected: 400 * time.Millisecond},  // 100ms * 2^2 = 400ms
            {attempt: 4, expected: 800 * time.Millisecond},  // 100ms * 2^3 = 800ms
            {attempt: 5, expected: 1600 * time.Millisecond}, // 100ms * 2^4 = 1600ms
        }

        for _, tt := range tests {
            got := cfg.CalculateBackoff(tt.attempt)
            if got != tt.expected {
                t.Errorf("CalculateBackoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
            }
        }
    })

    t.Run("respects MaxDelay cap", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     time.Second,
            MaxDelay:      5 * time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        // Attempt 5: 1s * 2^4 = 16s, should be capped at 5s
        got := cfg.CalculateBackoff(5)
        if got != 5*time.Second {
            t.Errorf("CalculateBackoff(5) = %v, want %v (MaxDelay cap)", got, 5*time.Second)
        }

        // Attempt 10: should also be capped
        got = cfg.CalculateBackoff(10)
        if got != 5*time.Second {
            t.Errorf("CalculateBackoff(10) = %v, want %v (MaxDelay cap)", got, 5*time.Second)
        }
    })

    t.Run("attempt less than 1 treated as 1", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        got := cfg.CalculateBackoff(0)
        expected := 100 * time.Millisecond
        if got != expected {
            t.Errorf("CalculateBackoff(0) = %v, want %v", got, expected)
        }

        got = cfg.CalculateBackoff(-5)
        if got != expected {
            t.Errorf("CalculateBackoff(-5) = %v, want %v", got, expected)
        }
    })

    t.Run("jitter adds randomness", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     time.Second,
            MaxDelay:      time.Minute,
            BackoffFactor: 2.0,
            JitterFactor:  0.5,
        }

        // Run multiple times and verify values vary
        results := make(map[time.Duration]int)
        iterations := 100
        for i := 0; i < iterations; i++ {
            got := cfg.CalculateBackoff(1)
            results[got]++
        }

        // With jitter, we should have multiple unique values
        if len(results) < 5 {
            t.Errorf("jitter should produce varied results, got only %d unique values", len(results))
        }

        // All values should be within jitter bounds
        // With JitterFactor=0.5 and BaseDelay=1s, range is 0.5s to 1.5s
        minExpected := time.Duration(float64(time.Second) * 0.5)
        maxExpected := time.Duration(float64(time.Second) * 1.5)

        for delay := range results {
            if delay < minExpected || delay > maxExpected {
                t.Errorf("delay %v outside jitter bounds [%v, %v]", delay, minExpected, maxExpected)
            }
        }
    })

    t.Run("zero jitter factor means no jitter", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        // Run multiple times - should always get same value
        expected := 100 * time.Millisecond
        for i := 0; i < 10; i++ {
            got := cfg.CalculateBackoff(1)
            if got != expected {
                t.Errorf("iteration %d: CalculateBackoff(1) = %v, want %v (no jitter)", i, got, expected)
            }
        }
    })

    t.Run("backoff factor of 1 means constant delay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     time.Second,
            MaxDelay:      time.Minute,
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }

        for attempt := 1; attempt <= 5; attempt++ {
            got := cfg.CalculateBackoff(attempt)
            if got != time.Second {
                t.Errorf("CalculateBackoff(%d) = %v, want %v with factor 1.0", attempt, got, time.Second)
            }
        }
    })
}

func TestShouldRetry(t *testing.T) {
    t.Run("fatal error returns false", func(t *testing.T) {
        cfg := &RetryConfig{MaxRetries: 10}
        state := &RetryState{Attempts: 0}

        // Fatal errors
        fatalErrors := []error{
            nil,
            context.Canceled,
            context.DeadlineExceeded,
            errors.New("unknown error"),
        }

        for _, err := range fatalErrors {
            if cfg.ShouldRetry(state, err) {
                t.Errorf("ShouldRetry(%v) = true, want false for fatal error", err)
            }
        }
    })

    t.Run("retryable error with attempts less than max returns true", func(t *testing.T) {
        cfg := &RetryConfig{MaxRetries: 5}
        state := &RetryState{Attempts: 3}

        retryableErrors := []error{
            io.EOF,
            io.ErrUnexpectedEOF,
            syscall.ECONNRESET,
            errors.New("connection reset"),
            errors.New("timeout occurred"),
        }

        for _, err := range retryableErrors {
            if !cfg.ShouldRetry(state, err) {
                t.Errorf("ShouldRetry(%v) with %d attempts (max %d) = false, want true",
                    err, state.Attempts, cfg.MaxRetries)
            }
        }
    })

    t.Run("retryable error with attempts at max returns false", func(t *testing.T) {
        cfg := &RetryConfig{MaxRetries: 5}
        state := &RetryState{Attempts: 5}

        if cfg.ShouldRetry(state, io.EOF) {
            t.Errorf("ShouldRetry(io.EOF) with %d attempts (max %d) = true, want false",
                state.Attempts, cfg.MaxRetries)
        }
    })

    t.Run("retryable error with attempts exceeding max returns false", func(t *testing.T) {
        cfg := &RetryConfig{MaxRetries: 5}
        state := &RetryState{Attempts: 10}

        if cfg.ShouldRetry(state, io.EOF) {
            t.Errorf("ShouldRetry(io.EOF) with %d attempts (max %d) = true, want false",
                state.Attempts, cfg.MaxRetries)
        }
    })

    t.Run("MaxRetries 0 means unlimited retries for retryable", func(t *testing.T) {
        cfg := &RetryConfig{MaxRetries: 0}

        testAttempts := []int{0, 1, 10, 100, 1000, math.MaxInt32}
        for _, attempts := range testAttempts {
            state := &RetryState{Attempts: attempts}
            if !cfg.ShouldRetry(state, io.EOF) {
                t.Errorf("ShouldRetry(io.EOF) with MaxRetries=0, attempts=%d = false, want true",
                    attempts)
            }
        }
    })

    t.Run("MaxRetries 0 still rejects fatal errors", func(t *testing.T) {
        cfg := &RetryConfig{MaxRetries: 0}
        state := &RetryState{Attempts: 0}

        if cfg.ShouldRetry(state, context.Canceled) {
            t.Error("ShouldRetry(context.Canceled) = true, want false even with unlimited retries")
        }
    })

    t.Run("throttled errors are retryable", func(t *testing.T) {
        cfg := &RetryConfig{MaxRetries: 5}
        state := &RetryState{Attempts: 2}

        throttledErrors := []error{
            errors.New("HTTP 429 Too Many Requests"),
            errors.New("rate limit exceeded"),
            errors.New("service unavailable 503"),
        }

        for _, err := range throttledErrors {
            if !cfg.ShouldRetry(state, err) {
                t.Errorf("ShouldRetry(%v) = false, want true for throttled error", err)
            }
        }
    })
}

func TestDefaultRetryConfig(t *testing.T) {
    cfg := DefaultRetryConfig()

    t.Run("returns non-zero values", func(t *testing.T) {
        if cfg.MaxRetries == 0 {
            t.Error("MaxRetries should not be 0 for default config")
        }
        if cfg.BaseDelay == 0 {
            t.Error("BaseDelay should not be 0")
        }
        if cfg.MaxDelay == 0 {
            t.Error("MaxDelay should not be 0")
        }
        if cfg.BackoffFactor == 0 {
            t.Error("BackoffFactor should not be 0")
        }
    })

    t.Run("matches expected defaults", func(t *testing.T) {
        if cfg.MaxRetries != DEF_MAX_RETRIES {
            t.Errorf("MaxRetries = %d, want %d", cfg.MaxRetries, DEF_MAX_RETRIES)
        }
        if cfg.BaseDelay != DEF_BASE_DELAY {
            t.Errorf("BaseDelay = %v, want %v", cfg.BaseDelay, DEF_BASE_DELAY)
        }
        if cfg.MaxDelay != DEF_MAX_DELAY {
            t.Errorf("MaxDelay = %v, want %v", cfg.MaxDelay, DEF_MAX_DELAY)
        }
        if cfg.JitterFactor != DEF_JITTER_FACTOR {
            t.Errorf("JitterFactor = %v, want %v", cfg.JitterFactor, DEF_JITTER_FACTOR)
        }
        if cfg.BackoffFactor != DEF_BACKOFF_FACTOR {
            t.Errorf("BackoffFactor = %v, want %v", cfg.BackoffFactor, DEF_BACKOFF_FACTOR)
        }
    })

    t.Run("MaxDelay greater than BaseDelay", func(t *testing.T) {
        if cfg.MaxDelay <= cfg.BaseDelay {
            t.Errorf("MaxDelay (%v) should be greater than BaseDelay (%v)",
                cfg.MaxDelay, cfg.BaseDelay)
        }
    })

    t.Run("JitterFactor in valid range", func(t *testing.T) {
        if cfg.JitterFactor < 0 || cfg.JitterFactor > 1 {
            t.Errorf("JitterFactor = %v, should be in range [0, 1]", cfg.JitterFactor)
        }
    })

    t.Run("BackoffFactor at least 1", func(t *testing.T) {
        if cfg.BackoffFactor < 1 {
            t.Errorf("BackoffFactor = %v, should be at least 1", cfg.BackoffFactor)
        }
    })
}

func TestRetryState(t *testing.T) {
    t.Run("initial state is zero", func(t *testing.T) {
        state := RetryState{}
        if state.Attempts != 0 {
            t.Errorf("initial Attempts = %d, want 0", state.Attempts)
        }
        if state.TotalDelayed != 0 {
            t.Errorf("initial TotalDelayed = %v, want 0", state.TotalDelayed)
        }
    })

    t.Run("tracks attempts correctly", func(t *testing.T) {
        state := &RetryState{}

        for i := 1; i <= 5; i++ {
            state.Attempts++
            if state.Attempts != i {
                t.Errorf("after increment %d, Attempts = %d, want %d", i, state.Attempts, i)
            }
        }
    })

    t.Run("accumulates TotalDelayed", func(t *testing.T) {
        state := &RetryState{}

        delays := []time.Duration{
            100 * time.Millisecond,
            200 * time.Millisecond,
            400 * time.Millisecond,
        }

        var expectedTotal time.Duration
        for _, d := range delays {
            state.TotalDelayed += d
            expectedTotal += d
            if state.TotalDelayed != expectedTotal {
                t.Errorf("after adding %v, TotalDelayed = %v, want %v",
                    d, state.TotalDelayed, expectedTotal)
            }
        }

        if state.TotalDelayed != 700*time.Millisecond {
            t.Errorf("final TotalDelayed = %v, want %v", state.TotalDelayed, 700*time.Millisecond)
        }
    })

    t.Run("stores LastError", func(t *testing.T) {
        state := &RetryState{}
        testErr := errors.New("test error")

        state.LastError = testErr
        if state.LastError != testErr {
            t.Errorf("LastError = %v, want %v", state.LastError, testErr)
        }

        // Update to new error
        newErr := io.EOF
        state.LastError = newErr
        if state.LastError != newErr {
            t.Errorf("LastError = %v, want %v", state.LastError, newErr)
        }
    })

    t.Run("stores LastAttempt time", func(t *testing.T) {
        state := &RetryState{}
        now := time.Now()

        state.LastAttempt = now
        if state.LastAttempt != now {
            t.Errorf("LastAttempt = %v, want %v", state.LastAttempt, now)
        }
    })
}

func TestErrorCategoryConstants(t *testing.T) {
    t.Run("categories have distinct values", func(t *testing.T) {
        categories := map[ErrorCategory]string{
            ErrCategoryFatal:     "Fatal",
            ErrCategoryRetryable: "Retryable",
            ErrCategoryThrottled: "Throttled",
        }

        if len(categories) != 3 {
            t.Errorf("expected 3 distinct error categories")
        }
    })

    t.Run("Fatal is zero value", func(t *testing.T) {
        if ErrCategoryFatal != 0 {
            t.Errorf("ErrCategoryFatal = %d, want 0 (zero value)", ErrCategoryFatal)
        }
    })
}

func TestWaitForRetry(t *testing.T) {
    t.Run("respects context cancellation", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     time.Hour, // Very long delay
            MaxDelay:      time.Hour,
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }
        state := &RetryState{Attempts: 1}

        ctx, cancel := context.WithCancel(context.Background())

        done := make(chan error, 1)
        go func() {
            done <- cfg.WaitForRetry(ctx, state, ErrCategoryRetryable)
        }()

        // Cancel almost immediately
        time.Sleep(10 * time.Millisecond)
        cancel()

        select {
        case err := <-done:
            if !errors.Is(err, context.Canceled) {
                t.Errorf("WaitForRetry returned %v, want context.Canceled", err)
            }
        case <-time.After(time.Second):
            t.Error("WaitForRetry did not return after context cancellation")
        }
    })

    t.Run("waits for calculated delay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     50 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 1.0,
            JitterFactor:  0, // No jitter for predictable timing
        }
        state := &RetryState{Attempts: 1}

        start := time.Now()
        err := cfg.WaitForRetry(context.Background(), state, ErrCategoryRetryable)
        elapsed := time.Since(start)

        if err != nil {
            t.Errorf("WaitForRetry returned error: %v", err)
        }

        // Allow some tolerance for timing
        minExpected := 40 * time.Millisecond
        maxExpected := 100 * time.Millisecond
        if elapsed < minExpected || elapsed > maxExpected {
            t.Errorf("elapsed time %v not in expected range [%v, %v]",
                elapsed, minExpected, maxExpected)
        }
    })

    t.Run("updates TotalDelayed", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     10 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }
        state := &RetryState{Attempts: 1, TotalDelayed: 0}

        err := cfg.WaitForRetry(context.Background(), state, ErrCategoryRetryable)
        if err != nil {
            t.Errorf("WaitForRetry returned error: %v", err)
        }

        if state.TotalDelayed != 10*time.Millisecond {
            t.Errorf("TotalDelayed = %v, want %v", state.TotalDelayed, 10*time.Millisecond)
        }
    })

    t.Run("throttled errors double the delay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     50 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }
        state := &RetryState{Attempts: 1, TotalDelayed: 0}

        start := time.Now()
        err := cfg.WaitForRetry(context.Background(), state, ErrCategoryThrottled)
        elapsed := time.Since(start)

        if err != nil {
            t.Errorf("WaitForRetry returned error: %v", err)
        }

        // Expect ~100ms (double the 50ms base delay)
        minExpected := 80 * time.Millisecond
        maxExpected := 150 * time.Millisecond
        if elapsed < minExpected || elapsed > maxExpected {
            t.Errorf("throttled elapsed time %v not in expected range [%v, %v]",
                elapsed, minExpected, maxExpected)
        }
    })

    t.Run("throttled delay respects MaxDelay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      150 * time.Millisecond, // Less than 2x BaseDelay
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }
        state := &RetryState{Attempts: 1, TotalDelayed: 0}

        start := time.Now()
        err := cfg.WaitForRetry(context.Background(), state, ErrCategoryThrottled)
        elapsed := time.Since(start)

        if err != nil {
            t.Errorf("WaitForRetry returned error: %v", err)
        }

        // Doubled delay (200ms) should be capped at MaxDelay (150ms)
        if elapsed > 200*time.Millisecond {
            t.Errorf("throttled delay %v exceeded MaxDelay cap", elapsed)
        }
    })
}

func TestClassifyErrorWrappedErrors(t *testing.T) {
    t.Run("deeply wrapped EOF", func(t *testing.T) {
        err := fmt.Errorf("layer1: %w",
            fmt.Errorf("layer2: %w",
                fmt.Errorf("layer3: %w", io.EOF)))

        got := ClassifyError(err)
        if got != ErrCategoryRetryable {
            t.Errorf("ClassifyError(deeply wrapped EOF) = %v, want ErrCategoryRetryable", got)
        }
    })

    t.Run("wrapped context.Canceled", func(t *testing.T) {
        err := fmt.Errorf("operation failed: %w", context.Canceled)

        got := ClassifyError(err)
        if got != ErrCategoryFatal {
            t.Errorf("ClassifyError(wrapped Canceled) = %v, want ErrCategoryFatal", got)
        }
    })

    t.Run("wrapped syscall error", func(t *testing.T) {
        err := fmt.Errorf("network failure: %w", syscall.ECONNRESET)

        got := ClassifyError(err)
        if got != ErrCategoryRetryable {
            t.Errorf("ClassifyError(wrapped ECONNRESET) = %v, want ErrCategoryRetryable", got)
        }
    })
}

func BenchmarkClassifyError(b *testing.B) {
    errors := []error{
        nil,
        context.Canceled,
        io.EOF,
        io.ErrUnexpectedEOF,
        syscall.ECONNRESET,
        fmt.Errorf("wrapped: %w", io.EOF),
        errors.New("connection reset by peer"),
        errors.New("random error message"),
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        for _, err := range errors {
            ClassifyError(err)
        }
    }
}

func BenchmarkCalculateBackoff(b *testing.B) {
    cfg := &RetryConfig{
        BaseDelay:     100 * time.Millisecond,
        MaxDelay:      10 * time.Second,
        BackoffFactor: 2.0,
        JitterFactor:  0.2,
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        cfg.CalculateBackoff((i % 10) + 1)
    }
}

// ============================================================================
// NEW EDGE CASE TESTS
// ============================================================================

// mockNetError implements net.Error for testing purposes
type mockNetError struct {
    msg       string
    timeout   bool
    temporary bool
}

func (e *mockNetError) Error() string   { return e.msg }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return e.temporary }

// Ensure mockNetError implements net.Error
var _ net.Error = (*mockNetError)(nil)

func TestCalculateBackoffEdgeCases(t *testing.T) {
    t.Run("BackoffFactor=1 no exponential growth", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      10 * time.Second,
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }

        // All attempts should return the same delay
        for attempt := 1; attempt <= 20; attempt++ {
            got := cfg.CalculateBackoff(attempt)
            if got != 100*time.Millisecond {
                t.Errorf("CalculateBackoff(%d) with factor=1 = %v, want %v",
                    attempt, got, 100*time.Millisecond)
            }
        }
    })

    t.Run("very large attempt numbers cap at MaxDelay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        // Test extremely large attempt numbers
        largeAttempts := []int{50, 100, 1000, math.MaxInt32 - 1}
        for _, attempt := range largeAttempts {
            got := cfg.CalculateBackoff(attempt)
            if got != time.Second {
                t.Errorf("CalculateBackoff(%d) = %v, want %v (MaxDelay cap)",
                    attempt, got, time.Second)
            }
        }
    })

    t.Run("MaxDelay=0 returns zero duration", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      0,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        // With MaxDelay=0, delay should be capped at 0
        got := cfg.CalculateBackoff(1)
        if got != 0 {
            t.Errorf("CalculateBackoff(1) with MaxDelay=0 = %v, want 0", got)
        }
    })

    t.Run("negative attempt treated as 1", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        negativeAttempts := []int{-1, -5, -100, math.MinInt32 + 1}
        expected := 100 * time.Millisecond // Same as attempt=1

        for _, attempt := range negativeAttempts {
            got := cfg.CalculateBackoff(attempt)
            if got != expected {
                t.Errorf("CalculateBackoff(%d) = %v, want %v (treated as 1)",
                    attempt, got, expected)
            }
        }
    })

    t.Run("overflow protection with extreme values", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     time.Hour,
            MaxDelay:      time.Hour,
            BackoffFactor: 10.0,
            JitterFactor:  0,
        }

        // Should not panic or return invalid values
        got := cfg.CalculateBackoff(100)
        if got < 0 || got > time.Hour {
            t.Errorf("CalculateBackoff(100) with extreme config = %v, expected <= MaxDelay", got)
        }
    })
}

func TestClassifyErrorMoreCases(t *testing.T) {
    tests := []struct {
        name     string
        err      error
        expected ErrorCategory
    }{
        // net.OpError with Timeout() returning true
        {
            name: "net.Error with Timeout=true",
            err: &mockNetError{
                msg:     "i/o timeout",
                timeout: true,
            },
            expected: ErrCategoryRetryable,
        },
        // net.OpError with Timeout() returning false (should fall through to string matching)
        {
            name: "net.Error with Timeout=false no pattern",
            err: &mockNetError{
                msg:     "some network error",
                timeout: false,
            },
            expected: ErrCategoryFatal,
        },
        {
            name: "net.Error with Timeout=false but timeout in message",
            err: &mockNetError{
                msg:     "connection timeout occurred",
                timeout: false,
            },
            expected: ErrCategoryRetryable,
        },
        // URL parse errors are fatal
        {
            name:     "URL parse error",
            err:      &url.Error{Op: "parse", URL: "://invalid", Err: errors.New("missing protocol scheme")},
            expected: ErrCategoryFatal,
        },
        // Custom error wrapping syscall error
        {
            name:     "custom error wrapping syscall.ECONNRESET",
            err:      fmt.Errorf("custom wrapper: %w", fmt.Errorf("inner: %w", syscall.ECONNRESET)),
            expected: ErrCategoryRetryable,
        },
        {
            name:     "custom error wrapping syscall.EPIPE",
            err:      fmt.Errorf("write failed: %w", syscall.EPIPE),
            expected: ErrCategoryRetryable,
        },
        // Rate limit in message
        {
            name:     "rate limit in message lowercase",
            err:      errors.New("rate limit exceeded, please retry later"),
            expected: ErrCategoryThrottled,
        },
        {
            name:     "rate limit in message mixed case",
            err:      errors.New("Rate Limit Exceeded"),
            expected: ErrCategoryThrottled,
        },
        // Too many open files (EMFILE) - treated as fatal by default (not in retry list)
        {
            name:     "too many open files syscall",
            err:      syscall.EMFILE,
            expected: ErrCategoryFatal,
        },
        // But string pattern "too many" doesn't exist, so this stays fatal
        {
            name:     "too many open files message",
            err:      errors.New("too many open files"),
            expected: ErrCategoryFatal,
        },
        // No such host (retryable via string pattern)
        {
            name:     "no such host",
            err:      errors.New("dial tcp: lookup example.com: no such host"),
            expected: ErrCategoryRetryable,
        },
        // Wrapped net.Error
        {
            name: "wrapped net.Error with timeout",
            err: fmt.Errorf("request failed: %w", &mockNetError{
                msg:     "dial timeout",
                timeout: true,
            }),
            expected: ErrCategoryRetryable,
        },
        // EOF in error message string
        {
            name:     "eof in lowercase message",
            err:      errors.New("unexpected eof while reading"),
            expected: ErrCategoryRetryable,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := ClassifyError(tt.err)
            if got != tt.expected {
                t.Errorf("ClassifyError(%v) = %v, want %v", tt.err, got, tt.expected)
            }
        })
    }
}

func TestWaitForRetryTransient(t *testing.T) {
    t.Run("transient category uses normal delay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     20 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }
        state := &RetryState{Attempts: 1, TotalDelayed: 0}

        start := time.Now()
        err := cfg.WaitForRetry(context.Background(), state, ErrCategoryRetryable)
        elapsed := time.Since(start)

        if err != nil {
            t.Errorf("WaitForRetry returned error: %v", err)
        }

        // Should be close to BaseDelay (20ms)
        minExpected := 15 * time.Millisecond
        maxExpected := 50 * time.Millisecond
        if elapsed < minExpected || elapsed > maxExpected {
            t.Errorf("transient elapsed time %v not in expected range [%v, %v]",
                elapsed, minExpected, maxExpected)
        }

        // Verify TotalDelayed was updated correctly
        if state.TotalDelayed != 20*time.Millisecond {
            t.Errorf("TotalDelayed = %v, want %v", state.TotalDelayed, 20*time.Millisecond)
        }
    })

    t.Run("fatal category should still work with minimal delay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     10 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 1.0,
            JitterFactor:  0,
        }
        state := &RetryState{Attempts: 1, TotalDelayed: 0}

        // Even for fatal category, WaitForRetry should complete
        start := time.Now()
        err := cfg.WaitForRetry(context.Background(), state, ErrCategoryFatal)
        elapsed := time.Since(start)

        if err != nil {
            t.Errorf("WaitForRetry returned error: %v", err)
        }

        // Fatal doesn't get doubled, should be ~10ms
        if elapsed < 5*time.Millisecond || elapsed > 50*time.Millisecond {
            t.Errorf("fatal elapsed time %v not in expected range", elapsed)
        }
    })
}

func TestShouldRetryEdgeCases(t *testing.T) {
    tests := []struct {
        name       string
        maxRetries int
        attempts   int
        err        error
        expected   bool
    }{
        {
            name:       "MaxRetries=1, Attempts=0, retryable error",
            maxRetries: 1,
            attempts:   0,
            err:        io.EOF,
            expected:   true,
        },
        {
            name:       "MaxRetries=1, Attempts=1, retryable error",
            maxRetries: 1,
            attempts:   1,
            err:        io.EOF,
            expected:   false,
        },
        {
            name:       "nil error returns false",
            maxRetries: 10,
            attempts:   0,
            err:        nil,
            expected:   false,
        },
        {
            name:       "MaxRetries=1, Attempts=0, nil error",
            maxRetries: 1,
            attempts:   0,
            err:        nil,
            expected:   false,
        },
        {
            name:       "MaxRetries=2, Attempts=1, retryable error",
            maxRetries: 2,
            attempts:   1,
            err:        syscall.ECONNRESET,
            expected:   true,
        },
        {
            name:       "MaxRetries=2, Attempts=2, retryable error",
            maxRetries: 2,
            attempts:   2,
            err:        syscall.ECONNRESET,
            expected:   false,
        },
        {
            name:       "MaxRetries=0 (unlimited), high attempts, retryable",
            maxRetries: 0,
            attempts:   999999,
            err:        io.EOF,
            expected:   true,
        },
        {
            name:       "MaxRetries=0 (unlimited), nil error still false",
            maxRetries: 0,
            attempts:   0,
            err:        nil,
            expected:   false,
        },
        {
            name:       "throttled error with attempts available",
            maxRetries: 5,
            attempts:   3,
            err:        errors.New("HTTP 429"),
            expected:   true,
        },
        {
            name:       "throttled error with no attempts left",
            maxRetries: 5,
            attempts:   5,
            err:        errors.New("HTTP 429"),
            expected:   false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cfg := &RetryConfig{MaxRetries: tt.maxRetries}
            state := &RetryState{Attempts: tt.attempts}

            got := cfg.ShouldRetry(state, tt.err)
            if got != tt.expected {
                t.Errorf("ShouldRetry(attempts=%d, maxRetries=%d, err=%v) = %v, want %v",
                    tt.attempts, tt.maxRetries, tt.err, got, tt.expected)
            }
        })
    }
}

func TestRetryConfigZeroValues(t *testing.T) {
    t.Run("BaseDelay=0 results in zero delay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     0,
            MaxDelay:      time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        // 0 * 2^n = 0 for any n
        for attempt := 1; attempt <= 5; attempt++ {
            got := cfg.CalculateBackoff(attempt)
            if got != 0 {
                t.Errorf("CalculateBackoff(%d) with BaseDelay=0 = %v, want 0", attempt, got)
            }
        }
    })

    t.Run("JitterFactor=0 produces deterministic results", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }

        // Should always produce the same result
        expected := 100 * time.Millisecond
        for i := 0; i < 100; i++ {
            got := cfg.CalculateBackoff(1)
            if got != expected {
                t.Errorf("iteration %d: CalculateBackoff(1) = %v, want %v", i, got, expected)
            }
        }
    })

    t.Run("BackoffFactor=0 results in zero delay", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     100 * time.Millisecond,
            MaxDelay:      time.Second,
            BackoffFactor: 0,
            JitterFactor:  0,
        }

        // BaseDelay * 0^n = 0 for n >= 1 (0^0 = 1 for attempt=1 case)
        // Actually math.Pow(0, 0) = 1, so attempt=1 gives BaseDelay * 1 = BaseDelay
        got := cfg.CalculateBackoff(1)
        // 0^0 is 1 in Go's math.Pow
        if got != 100*time.Millisecond {
            t.Errorf("CalculateBackoff(1) with BackoffFactor=0 = %v, want %v",
                got, 100*time.Millisecond)
        }

        // For attempt > 1, 0^n = 0
        for attempt := 2; attempt <= 5; attempt++ {
            got := cfg.CalculateBackoff(attempt)
            if got != 0 {
                t.Errorf("CalculateBackoff(%d) with BackoffFactor=0 = %v, want 0",
                    attempt, got)
            }
        }
    })

    t.Run("all zero config", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     0,
            MaxDelay:      0,
            BackoffFactor: 0,
            JitterFactor:  0,
            MaxRetries:    0,
        }

        // Should not panic
        got := cfg.CalculateBackoff(1)
        if got != 0 {
            t.Errorf("CalculateBackoff(1) with all zeros = %v, want 0", got)
        }

        // ShouldRetry with MaxRetries=0 means unlimited
        state := &RetryState{Attempts: 100}
        if !cfg.ShouldRetry(state, io.EOF) {
            t.Error("ShouldRetry with MaxRetries=0 should allow unlimited retries")
        }
    })

    t.Run("WaitForRetry with zero BaseDelay completes immediately", func(t *testing.T) {
        cfg := &RetryConfig{
            BaseDelay:     0,
            MaxDelay:      time.Second,
            BackoffFactor: 2.0,
            JitterFactor:  0,
        }
        state := &RetryState{Attempts: 1}

        start := time.Now()
        err := cfg.WaitForRetry(context.Background(), state, ErrCategoryRetryable)
        elapsed := time.Since(start)

        if err != nil {
            t.Errorf("WaitForRetry returned error: %v", err)
        }

        // Should complete almost instantly
        if elapsed > 50*time.Millisecond {
            t.Errorf("WaitForRetry with BaseDelay=0 took %v, expected near-instant", elapsed)
        }
    })
}
