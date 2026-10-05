package cmd

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// TestSpeedCounter_SetBar_Concurrent tests for race conditions when SetBar and IncrBy
// are called concurrently. Run with: go test -race -run TestSpeedCounter_SetBar_Concurrent
func TestSpeedCounter_SetBar_Concurrent(t *testing.T) {
	sc := NewSpeedCounter(time.Millisecond)
	p := mpb.New()
	bar1 := p.AddBar(100)
	bar2 := p.AddBar(100)

	sc.Start()
	defer sc.Stop()

	var wg sync.WaitGroup
	// Spawn goroutines that call SetBar concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				sc.SetBar(bar1)
			} else {
				sc.SetBar(bar2)
			}
		}(i)
	}

	// Spawn goroutines that call IncrBy concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc.IncrBy(100)
		}()
	}

	wg.Wait()
	// Test passes if no race detected (run with -race flag)
}

// ewmaRecorder is a minimal mpb decorator that records the updates the speed
// counter's worker pushes into its bar via EwmaIncrInt64. It makes the
// worker's delivered byte counts and its zero-byte skips observable.
type ewmaRecorder struct {
	decor.WC
	mu      sync.Mutex
	updates []ewmaUpdate
}

type ewmaUpdate struct {
	bytes int64
	iter  time.Duration
}

func (r *ewmaRecorder) Decor(decor.Statistics) (text string, width int) { return "", 0 }

func (r *ewmaRecorder) EwmaUpdate(n int64, iterDur time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updates = append(r.updates, ewmaUpdate{bytes: n, iter: iterDur})
}

func (r *ewmaRecorder) recorded() []ewmaUpdate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ewmaUpdate(nil), r.updates...)
}

// newSpeedCounterTestBar builds a bar whose rendering goes to io.Discard, so
// tests never write progress frames, and records the EWMA updates the speed
// counter pushes into it.
func newSpeedCounterTestBar() (*mpb.Bar, *ewmaRecorder) {
	rec := &ewmaRecorder{}
	p := mpb.New(mpb.WithOutput(io.Discard))
	return p.AddBar(1000, mpb.PrependDecorators(rec)), rec
}

// waitForSpeedCounterBar polls the bar until it reports want; the poll interval
// only waits for the worker's next tick, the reported value is the assertion.
func waitForSpeedCounterBar(t *testing.T, bar *mpb.Bar, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if bar.Current() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("bar current = %d, want %d", bar.Current(), want)
}

// waitForSpeedCounterTicks waits for n ticks on the counter's ticker. The
// worker receives from the same channel, so once this returns the worker has
// been handed ticks to process too.
func waitForSpeedCounterTicks(t *testing.T, ticker *time.Ticker, n int) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for i := range n {
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("ticker fired %d times, want %d", i, n)
		}
	}
}

// TestSpeedCounter_NilBarTicksAreIgnoredUntilBarIsSet covers the worker's
// nil-bar skip: ticks before SetBar must not panic, must leave queued bytes
// untouched, and those bytes must still be delivered once a bar is attached.
func TestSpeedCounter_NilBarTicksAreIgnoredUntilBarIsSet(t *testing.T) {
	sc := NewSpeedCounter(time.Millisecond)
	sc.Start()
	defer sc.Stop()

	sc.IncrBy(100)
	// The nil-bar arm never consumes, so the queued total survives any number
	// of ticks; waiting for ticks first gives the worker some to process.
	waitForSpeedCounterTicks(t, sc.ticker, 2)
	if got := atomic.LoadInt64(&sc.bpc); got != 100 {
		t.Fatalf("bpc = %d with no bar set, want 100", got)
	}

	bar, _ := newSpeedCounterTestBar()
	sc.SetBar(bar)
	waitForSpeedCounterBar(t, bar, 100)
	if got := atomic.LoadInt64(&sc.bpc); got != 0 {
		t.Fatalf("bpc = %d after delivery, want 0", got)
	}
}

// TestSpeedCounter_IncrByAccumulatesExactTotals pins IncrBy's observable total
// before and after Stop: every queued byte is counted exactly once, and Stop
// (which only stops the ticker) must not disturb further accumulation. The
// one-hour refresh rate keeps the worker from consuming anything mid-test.
func TestSpeedCounter_IncrByAccumulatesExactTotals(t *testing.T) {
	sc := NewSpeedCounter(time.Hour)
	sc.Start()

	sc.IncrBy(100)
	sc.IncrBy(200)
	sc.IncrBy(300)
	if got := atomic.LoadInt64(&sc.bpc); got != 600 {
		t.Fatalf("bpc = %d after queuing 100+200+300, want 600", got)
	}

	sc.Stop()
	sc.IncrBy(50)
	if got := atomic.LoadInt64(&sc.bpc); got != 650 {
		t.Fatalf("bpc = %d after Stop + IncrBy(50), want 650", got)
	}
}

// TestSpeedCounter_TickDeliversQueuedBytesToBar covers the worker's tick path:
// everything queued reaches the bar exactly once, the queue is drained, and the
// elapsed time handed to the bar is a real duration rather than a zero (or
// wildly stale) value.
func TestSpeedCounter_TickDeliversQueuedBytesToBar(t *testing.T) {
	start := time.Now()
	sc := NewSpeedCounter(time.Millisecond)
	bar, rec := newSpeedCounterTestBar()
	sc.SetBar(bar)
	sc.Start()
	defer sc.Stop()

	sc.IncrBy(100)
	sc.IncrBy(200)
	sc.IncrBy(300)

	waitForSpeedCounterBar(t, bar, 600)
	if got := atomic.LoadInt64(&sc.bpc); got != 0 {
		t.Fatalf("bpc = %d after delivery, want 0", got)
	}

	var delivered int64
	for _, u := range rec.recorded() {
		if u.bytes <= 0 {
			t.Fatalf("bar update with %d bytes, want a positive batch", u.bytes)
		}
		if u.iter <= 0 || u.iter > time.Since(start) {
			t.Fatalf("bar update elapsed = %s, want a positive duration no longer than the test itself", u.iter)
		}
		delivered += u.bytes
	}
	if delivered != 600 {
		t.Fatalf("delivered %d bytes to the bar, want 600", delivered)
	}
}

// TestSpeedCounter_ZeroByteTicksDoNotUpdateBar covers the worker's zero-byte
// skip: cycles with nothing queued must not push zero-byte updates into the
// bar, which would skew the EWMA speed it drives. The first delivery proves the
// worker is ticking; the recorded updates then pin the exact behaviour.
func TestSpeedCounter_ZeroByteTicksDoNotUpdateBar(t *testing.T) {
	sc := NewSpeedCounter(time.Millisecond)
	bar, rec := newSpeedCounterTestBar()
	sc.SetBar(bar)
	sc.Start()
	defer sc.Stop()

	sc.IncrBy(100)
	waitForSpeedCounterBar(t, bar, 100)
	// Idle cycles for the worker to skip. A single IncrBy is never split, so
	// the delivery above must stay the only recorded update.
	waitForSpeedCounterTicks(t, sc.ticker, 2)

	updates := rec.recorded()
	if len(updates) != 1 || updates[0].bytes != 100 {
		t.Fatalf("bar updates = %+v, want exactly one delivery of 100 bytes", updates)
	}
}

// TestSpeedCounter_IncrByAtomic verifies IncrBy is atomic under concurrent access
func TestSpeedCounter_IncrByAtomic(t *testing.T) {
	sc := NewSpeedCounter(time.Hour) // Long tick to prevent consumption
	p := mpb.New()
	bar := p.AddBar(10000)
	sc.SetBar(bar)

	sc.Start()
	defer sc.Stop()

	var wg sync.WaitGroup
	numGoroutines := 100
	incPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < incPerGoroutine; j++ {
				sc.IncrBy(1)
			}
		}()
	}

	wg.Wait()

	expected := int64(numGoroutines * incPerGoroutine)
	actual := atomic.LoadInt64(&sc.bpc)
	if actual != expected {
		t.Errorf("expected bpc=%d, got %d", expected, actual)
	}
}
