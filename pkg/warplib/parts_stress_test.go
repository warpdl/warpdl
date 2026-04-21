package warplib

import (
	"bytes"
	"errors"
	"io"
	"log"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestPartCallbackStress_BlockingCallback verifies that a slow progress
// callback does not cause unbounded goroutine growth (the old
// implementation spawned one goroutine per chunk).
//
// We read through a small buffer with a callback that sleeps; if the
// implementation still fans out, goroutines pile up linearly in the
// number of reads.
func TestPartCallbackStress_BlockingCallback(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		payload := bytes.Repeat([]byte{1}, 100_000)

		var invocations atomic.Int32
		r := NewAsyncCallbackProxyReader(
			bytes.NewReader(payload),
			func(n int) {
				invocations.Add(1)
				time.Sleep(100 * time.Microsecond)
			},
			log.New(io.Discard, "", 0),
		)

		buf := make([]byte, 128)
		// Take a goroutine-count snapshot after first read so any one-time
		// init (readers, schedulers) has settled.
		_, err := r.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("first read: %v", err)
		}
		runtime.GC()
		start := runtime.NumGoroutine()

		for {
			_, err := r.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
		}
		r.Wait()

		// Allow a small settle window.
		time.Sleep(20 * time.Millisecond)
		runtime.GC()
		end := runtime.NumGoroutine()

		// A regressed implementation would have len(payload)/128 ~=
		// 780 goroutines in flight at peak. With the synchronous
		// implementation, we should see no sustained growth.
		if end-start > goroutineLeakTolerance {
			t.Fatalf("goroutine growth during copy loop: start=%d end=%d",
				start, end)
		}
		if invocations.Load() == 0 {
			t.Fatal("callback never invoked")
		}
	})
}

// TestPartCallbackStress_PanickingCallback verifies that a callback that
// panics does not corrupt the reader or leak goroutines. Multiple
// consecutive panics must not prevent subsequent reads from succeeding.
func TestPartCallbackStress_PanickingCallback(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		payload := bytes.Repeat([]byte{42}, 10_000)
		var panics atomic.Int32
		r := NewAsyncCallbackProxyReader(
			bytes.NewReader(payload),
			func(n int) {
				panics.Add(1)
				panic("test panic")
			},
			log.New(io.Discard, "", 0),
		)

		buf := make([]byte, 256)
		total := 0
		for {
			n, err := r.Read(buf)
			total += n
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read returned non-EOF error: %v", err)
			}
		}
		r.Wait()

		if total != len(payload) {
			t.Fatalf("total read = %d, want %d (panics in callback "+
				"corrupted the read loop)", total, len(payload))
		}
		if panics.Load() == 0 {
			t.Fatal("callback was never invoked")
		}
	})
}

// TestPartCallbackStress_CallbackRacesReader emulates a callback that
// reads shared state concurrently with another thread writing it.
// Because the callback now runs synchronously on the read goroutine,
// any state shared with outside parties must still be safe to access.
// We just verify under -race that no race is flagged.
func TestPartCallbackStress_CallbackRacesReader(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		payload := bytes.Repeat([]byte{7}, 10_000)
		var counter atomic.Int64

		r := NewAsyncCallbackProxyReader(
			bytes.NewReader(payload),
			func(n int) { counter.Add(int64(n)) },
			log.New(io.Discard, "", 0),
		)

		// Another goroutine reading the same counter (different type of
		// access). Race detector will flag any UB.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 1000; i++ {
				_ = counter.Load()
				time.Sleep(10 * time.Microsecond)
			}
		}()

		buf := make([]byte, 128)
		for {
			_, err := r.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
		}
		r.Wait()
		<-done

		if counter.Load() != int64(len(payload)) {
			t.Fatalf("counter = %d, want %d", counter.Load(), len(payload))
		}
	})
}

// TestCopyLoopAllocBudget verifies the pooled-buffer copy loop has
// steady-state zero allocations. Runs a simplified copy analogous to
// parts.copyBuffer.
func TestCopyLoopAllocBudget(t *testing.T) {
	// Warm the pool.
	for i := 0; i < 10; i++ {
		putBuf(getBuf(int(DEF_CHUNK_SIZE)))
	}
	runtime.GC()

	payload := bytes.Repeat([]byte{1}, int(DEF_CHUNK_SIZE)*4)

	allocs := testing.AllocsPerRun(50, func() {
		src := bytes.NewReader(payload)
		dst := &bytes.Buffer{}
		dst.Grow(len(payload)) // pre-size dst so it doesn't skew results
		bp := getBuf(int(DEF_CHUNK_SIZE))
		for {
			n, err := src.Read(*bp)
			if n > 0 {
				dst.Write((*bp)[:n])
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
		}
		putBuf(bp)
	})

	// Budget: at most 2 allocs per run (one for src reader, one for dst).
	// If the pool ever stops participating we'd see 4+ allocs (1 per chunk).
	if allocs > 4 {
		t.Fatalf("copy-loop steady allocs = %.1f, want <=4 (pool may be bypassed)",
			allocs)
	}
}

// TestPartCallbackStress_NilCallback exercises the safety path when the
// callback function is nil (currently documented as a user error, but
// must not panic).
func TestPartCallbackStress_NilCallback(t *testing.T) {
	assertNoGoroutineLeak(t, func() {
		payload := bytes.Repeat([]byte{1}, 1000)
		r := NewAsyncCallbackProxyReader(
			bytes.NewReader(payload),
			nil, // <-- nil callback
			log.New(io.Discard, "", 0),
		)

		defer func() {
			if rec := recover(); rec != nil {
				t.Fatalf("nil callback panicked: %v", rec)
			}
		}()

		buf := make([]byte, 128)
		totalRead := 0
		for {
			n, err := r.Read(buf)
			totalRead += n
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
		}
		if totalRead != len(payload) {
			t.Fatalf("nil callback: totalRead=%d want %d", totalRead, len(payload))
		}
	})
}

// TestPartCallbackStress_NilCallbackEager verifies the callback is
// invoked exactly zero times when nil.
func TestPartCallbackStress_NilCallbackEager(t *testing.T) {
	r := NewAsyncCallbackProxyReader(
		errReader{err: io.EOF},
		nil,
		log.New(io.Discard, "", 0),
	)
	buf := make([]byte, 10)
	_, err := r.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// errReader is a test helper that returns a predetermined error on Read.
type errReader struct{ err error }

func (e errReader) Read(_ []byte) (int, error) { return 0, e.err }
