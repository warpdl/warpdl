package warplib

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand"
	"net"
	"strings"
	"syscall"
	"time"
)

// Default retry configuration values
const (
	DEF_MAX_RETRIES    = 5
	DEF_BASE_DELAY     = 500 * time.Millisecond
	DEF_MAX_DELAY      = 30 * time.Second
	DEF_JITTER_FACTOR  = 0.5
	DEF_BACKOFF_FACTOR = 2.0
)

// RetryConfig holds configuration for retry behavior
type RetryConfig struct {
	MaxRetries    int           // Maximum number of retry attempts (0 = unlimited)
	BaseDelay     time.Duration // Initial delay before first retry
	MaxDelay      time.Duration // Maximum delay between retries
	JitterFactor  float64       // Random jitter factor (0-1)
	BackoffFactor float64       // Exponential backoff multiplier
}

// DefaultRetryConfig returns a RetryConfig with sensible defaults
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:    DEF_MAX_RETRIES,
		BaseDelay:     DEF_BASE_DELAY,
		MaxDelay:      DEF_MAX_DELAY,
		JitterFactor:  DEF_JITTER_FACTOR,
		BackoffFactor: DEF_BACKOFF_FACTOR,
	}
}

// RetryState tracks the state of retry attempts
type RetryState struct {
	Attempts     int           // Number of attempts made
	LastError    error         // Most recent error encountered
	LastAttempt  time.Time     // Time of last attempt
	TotalDelayed time.Duration // Cumulative time spent waiting between retries
}

// ErrorCategory classifies errors for retry decisions
type ErrorCategory int

const (
	ErrCategoryFatal     ErrorCategory = iota // Non-retryable errors (404, canceled)
	ErrCategoryRetryable                      // Transient errors (EOF, timeout, reset)
	ErrCategoryThrottled                      // Rate limiting errors (429, 503)
)

// ClassifyError determines how an error should be handled for retry purposes
func ClassifyError(err error) ErrorCategory {
	if err == nil {
		return ErrCategoryFatal
	}

	// Context cancellation is not retryable (user stopped download)
	// Deadline exceeded (timeout) is retryable (per-request timeout)
	if errors.Is(err, context.Canceled) {
		return ErrCategoryFatal
	}

	// EOF errors are retryable (connection dropped mid-transfer)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrCategoryRetryable
	}

	// Check for network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return ErrCategoryRetryable
		}
	}

	// Check for syscall errors indicating connection issues
	var sysErr syscall.Errno
	if errors.As(err, &sysErr) {
		if isRetryableErrno(sysErr) {
			return ErrCategoryRetryable
		}
	}

	// String-based pattern matching for wrapped errors
	errStr := strings.ToLower(err.Error())
	retryablePatterns := []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"timeout",
		"eof",
		"temporary failure",
		"no such host",
		"network is unreachable",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(errStr, pattern) {
			return ErrCategoryRetryable
		}
	}

	// Check for throttling indicators
	throttlePatterns := []string{
		"429",
		"503",
		"too many requests",
		"service unavailable",
		"rate limit",
		"throttl",
	}
	for _, pattern := range throttlePatterns {
		if strings.Contains(errStr, pattern) {
			return ErrCategoryThrottled
		}
	}

	// Unknown errors are treated as fatal to avoid infinite retry loops
	return ErrCategoryFatal
}

// CalculateBackoff computes the delay before the next retry attempt
func (c *RetryConfig) CalculateBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	// Exponential backoff: baseDelay * (backoffFactor ^ (attempt-1))
	delay := float64(c.BaseDelay) * math.Pow(c.BackoffFactor, float64(attempt-1))

	// Apply jitter: delay * (1 + jitterFactor * random(-1, 1))
	if c.JitterFactor > 0 {
		jitter := c.JitterFactor * (2*rand.Float64() - 1) // random in [-1, 1]
		delay *= (1 + jitter)
	}

	// Cap at maximum delay
	if delay > float64(c.MaxDelay) {
		delay = float64(c.MaxDelay)
	}

	// Ensure non-negative
	if delay < 0 {
		delay = float64(c.BaseDelay)
	}

	return time.Duration(delay)
}

// ShouldRetry determines if another retry attempt should be made
func (c *RetryConfig) ShouldRetry(state *RetryState, err error) bool {
	category := ClassifyError(err)

	// Fatal errors are never retried
	if category == ErrCategoryFatal {
		return false
	}

	// Check retry limit (0 = unlimited)
	if c.MaxRetries > 0 && state.Attempts >= c.MaxRetries {
		return false
	}

	return true
}

// WaitForRetry blocks until the retry delay has elapsed or context is canceled
func (c *RetryConfig) WaitForRetry(ctx context.Context, state *RetryState, category ErrorCategory) error {
	delay := c.CalculateBackoff(state.Attempts)

	// Throttled errors get double the normal delay
	if category == ErrCategoryThrottled {
		delay *= 2
		if delay > c.MaxDelay {
			delay = c.MaxDelay
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		state.TotalDelayed += delay
		return nil
	}
}
