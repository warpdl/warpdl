package warplib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// stallTimeoutError is returned when a download stalls (no data received for
// the configured timeout duration). It implements net.Error with Timeout()=true
// so that ClassifyError treats it as retryable, unlike context.Canceled which
// is treated as fatal (user-initiated stop).
type stallTimeoutError struct {
	timeout time.Duration
}

func (e *stallTimeoutError) Error() string {
	return fmt.Sprintf("stall timeout: no data received for %v", e.timeout)
}

// Timeout returns true, marking this as a timeout error for net.Error
// and ClassifyError compatibility.
func (e *stallTimeoutError) Timeout() bool { return true }

// Temporary returns true for net.Error interface compatibility.
// Deprecated in Go 1.18+ but still required by the interface.
func (e *stallTimeoutError) Temporary() bool { return true }

// stallReader wraps an io.ReadCloser with stall detection. A watchdog timer
// fires if no data is received within the timeout duration, cancelling the
// request context. Each successful Read resets the timer.
//
// When the stall timer fires, subsequent Read errors are replaced with
// stallTimeoutError (retryable) instead of context.Canceled (fatal),
// but only if the parent context is still alive (distinguishing stall
// from user-initiated cancellation).
type stallReader struct {
	src       io.ReadCloser
	cancel    context.CancelFunc
	timer     *time.Timer
	timeout   time.Duration
	stalled   atomic.Bool
	parentCtx context.Context
	once      sync.Once
}

// newStallReader creates a stallReader that wraps src with stall detection.
// The cancel function is called if no data is received for timeout duration.
// parentCtx is used to distinguish stall timeout from user cancellation.
func newStallReader(src io.ReadCloser, cancel context.CancelFunc, timeout time.Duration, parentCtx context.Context) *stallReader {
	r := &stallReader{
		src:       src,
		cancel:    cancel,
		timeout:   timeout,
		parentCtx: parentCtx,
	}
	r.timer = time.AfterFunc(timeout, func() {
		r.stalled.Store(true)
		cancel()
	})
	return r
}

// Read reads from the underlying reader and resets the stall timer on each
// successful read. If the read fails due to a stall timeout (our timer fired),
// the error is replaced with stallTimeoutError so the retry logic treats it
// as retryable. If the parent context was cancelled (user stopped download),
// the original context.Canceled error is preserved (treated as fatal).
func (r *stallReader) Read(p []byte) (n int, err error) {
	n, err = r.src.Read(p)
	if n > 0 {
		r.timer.Reset(r.timeout)
	}
	// Replace context.Canceled with retryable stall error, but only if:
	// 1. Our stall timer actually fired (not user cancellation)
	// 2. The parent context is still alive (user didn't stop)
	if err != nil && r.stalled.Load() && r.parentCtx.Err() == nil && errors.Is(err, context.Canceled) {
		err = &stallTimeoutError{timeout: r.timeout}
	}
	return
}

// Close stops the stall timer, cancels the context, and closes the
// underlying reader. Safe to call multiple times.
func (r *stallReader) Close() error {
	r.once.Do(func() {
		r.timer.Stop()
		if r.cancel != nil {
			r.cancel()
		}
	})
	return r.src.Close()
}

// resetTimer resets the stall timer to the full timeout duration.
// Used when the reader is returned for reuse between copyBuffer calls.
func (r *stallReader) resetTimer() {
	r.stalled.Store(false)
	r.timer.Reset(r.timeout)
}
