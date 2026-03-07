package warplib

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestStallTimeoutError_ImplementsNetError(t *testing.T) {
	var err error = &stallTimeoutError{timeout: 5 * time.Second}

	var netErr net.Error
	if !errors.As(err, &netErr) {
		t.Fatal("stallTimeoutError should satisfy net.Error interface")
	}
	if !netErr.Timeout() {
		t.Error("stallTimeoutError.Timeout() should return true")
	}
}

func TestStallTimeoutError_ClassifiedAsRetryable(t *testing.T) {
	err := &stallTimeoutError{timeout: 30 * time.Second}
	category := ClassifyError(err)
	if category != ErrCategoryRetryable {
		t.Errorf("ClassifyError(stallTimeoutError) = %v, want ErrCategoryRetryable", category)
	}
}

func TestStallTimeoutError_ErrorMessage(t *testing.T) {
	err := &stallTimeoutError{timeout: 30 * time.Second}
	msg := err.Error()
	if !strings.Contains(msg, "stall timeout") {
		t.Errorf("error message should contain 'stall timeout', got: %s", msg)
	}
	if !strings.Contains(msg, "30s") {
		t.Errorf("error message should contain timeout duration, got: %s", msg)
	}
}

// slowReader blocks for a specified duration before returning data.
type slowReader struct {
	data    []byte
	pos     int
	delay   time.Duration
	ctx     context.Context
	closeFn func() error
}

func (r *slowReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	select {
	case <-time.After(r.delay):
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *slowReader) Close() error {
	if r.closeFn != nil {
		return r.closeFn()
	}
	return nil
}

// stallingReader returns data for a while, then blocks indefinitely.
type stallingReader struct {
	data     []byte
	pos      int
	stallAt  int
	ctx      context.Context
	stallCh  chan struct{}
	closedCh chan struct{}
}

func (r *stallingReader) Read(p []byte) (n int, err error) {
	if r.pos >= r.stallAt {
		// Stall: block until context is cancelled
		if r.stallCh != nil {
			close(r.stallCh) // signal that we've stalled
		}
		<-r.ctx.Done()
		return 0, r.ctx.Err()
	}
	end := r.pos + len(p)
	if end > r.stallAt {
		end = r.stallAt
	}
	n = copy(p, r.data[r.pos:end])
	r.pos += n
	return n, nil
}

func (r *stallingReader) Close() error {
	if r.closedCh != nil {
		select {
		case <-r.closedCh:
		default:
			close(r.closedCh)
		}
	}
	return nil
}

func TestStallReader_ActiveDataFlow(t *testing.T) {
	// Data flows quickly — stall timer should never fire
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	data := strings.Repeat("x", 1024)
	src := &slowReader{
		data:  []byte(data),
		delay: 1 * time.Millisecond, // fast reads
		ctx:   ctx,
	}

	sr := newStallReader(src, cancel, 500*time.Millisecond, context.Background())
	defer sr.Close()

	buf := make([]byte, 128)
	totalRead := 0
	for {
		n, err := sr.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error during active read: %v", err)
		}
	}

	if totalRead != len(data) {
		t.Errorf("read %d bytes, want %d", totalRead, len(data))
	}
}

func TestStallReader_StallTriggersTimeout(t *testing.T) {
	// Reader stalls after some data — stall timer should fire
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	data := make([]byte, 1024)
	for i := range data {
		data[i] = byte(i % 256)
	}

	stallCh := make(chan struct{})
	src := &stallingReader{
		data:    data,
		stallAt: 512, // stall after 512 bytes
		ctx:     ctx,
		stallCh: stallCh,
	}

	stallTimeout := 100 * time.Millisecond
	sr := newStallReader(src, cancel, stallTimeout, context.Background())
	defer sr.Close()

	buf := make([]byte, 128)
	var lastErr error
	for {
		_, err := sr.Read(buf)
		if err != nil {
			lastErr = err
			break
		}
	}

	// Should get a stallTimeoutError, not context.Canceled
	var stallErr *stallTimeoutError
	if !errors.As(lastErr, &stallErr) {
		t.Fatalf("expected stallTimeoutError, got: %v (type: %T)", lastErr, lastErr)
	}
	if stallErr.timeout != stallTimeout {
		t.Errorf("stallTimeoutError.timeout = %v, want %v", stallErr.timeout, stallTimeout)
	}

	// Verify it's classified as retryable
	category := ClassifyError(lastErr)
	if category != ErrCategoryRetryable {
		t.Errorf("ClassifyError(stallTimeoutError) = %v, want ErrCategoryRetryable", category)
	}
}

func TestStallReader_ParentCancellationPreserved(t *testing.T) {
	// When parent context is cancelled (user stopped download),
	// the error should remain context.Canceled (fatal), not be
	// replaced with stallTimeoutError (retryable).
	parentCtx, parentCancel := context.WithCancel(context.Background())
	childCtx, childCancel := context.WithCancel(parentCtx)
	defer childCancel()

	data := make([]byte, 1024)
	stallCh := make(chan struct{})
	src := &stallingReader{
		data:    data,
		stallAt: 256,
		ctx:     childCtx,
		stallCh: stallCh,
	}

	// Use a long stall timeout so it doesn't fire before parent cancellation
	sr := newStallReader(src, childCancel, 5*time.Second, parentCtx)
	defer sr.Close()

	// Read some data first (consumes 256 bytes, reaching stallAt)
	buf := make([]byte, 128)
	for i := 0; i < 2; i++ {
		_, err := sr.Read(buf)
		if err != nil {
			t.Fatalf("unexpected error on read %d: %v", i, err)
		}
	}

	// Start a blocking read in a goroutine — this will stall inside the reader
	errCh := make(chan error, 1)
	go func() {
		_, err := sr.Read(buf)
		errCh <- err
	}()

	// Wait for reader to actually stall, then cancel parent (simulating user stop)
	<-stallCh
	parentCancel()

	// The goroutine's read should return context.Canceled (NOT stallTimeoutError)
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after parent cancellation")
		}

		// The error should be context.Canceled because parent was cancelled
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got: %v", err)
		}

		// It should NOT be a stallTimeoutError
		var stallErr *stallTimeoutError
		if errors.As(err, &stallErr) {
			t.Error("error should NOT be stallTimeoutError when parent context is cancelled")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for read to return after parent cancellation")
	}
}

func TestStallReader_ResetTimer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	data := strings.Repeat("x", 256)
	src := &slowReader{
		data:  []byte(data),
		delay: 1 * time.Millisecond,
		ctx:   ctx,
	}

	stallTimeout := 200 * time.Millisecond
	sr := newStallReader(src, cancel, stallTimeout, context.Background())

	// Read all data
	buf := make([]byte, 256)
	_, err := sr.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reset timer (simulates returning body for reuse)
	sr.resetTimer()

	// Stalled flag should be cleared
	if sr.stalled.Load() {
		t.Error("stalled flag should be false after resetTimer()")
	}

	sr.Close()
}
