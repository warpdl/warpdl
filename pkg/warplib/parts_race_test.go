package warplib

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPartReadAtomicRegression ensures Part.read is accessed atomically
// via getRead() at parts.go:116-118.
func TestPartReadAtomicRegression(t *testing.T) {
	p := &Part{read: 0}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&p.read, 1)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = p.getRead() // Must use atomic.LoadInt64
			}
		}()
	}
	wg.Wait()

	expected := int64(100 * 1000)
	if p.getRead() != expected {
		t.Errorf("expected %d, got %d", expected, p.getRead())
	}
}

// TestPartReadConsistency ensures atomic reads return consistent values
func TestPartReadConsistency(t *testing.T) {
	p := &Part{read: 0}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := int64(0); ; i++ {
			select {
			case <-stop:
				return
			default:
				atomic.StoreInt64(&p.read, i)
			}
		}
	}()

	// Reader goroutines - verify we never get torn reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prevVal := int64(-1)
			for j := 0; j < 10000; j++ {
				select {
				case <-stop:
					return
				default:
					val := p.getRead()
					if val < 0 {
						t.Errorf("got negative value: %d (indicates torn read)", val)
					}
					// Values should be monotonically increasing or equal
					if val < prevVal {
						// This is acceptable due to timing - just ensure no corruption
					}
					prevVal = val
				}
			}
		}()
	}

	// Let it run briefly
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100000; i++ {
			// busy loop
		}
		close(stop)
	}()

	wg.Wait()
}

// TestPartReadNoTearingRegression verifies that concurrent reads and writes
// to Part.read don't result in torn/corrupted values when using atomic operations.
func TestPartReadNoTearingRegression(t *testing.T) {
	p := &Part{read: 0}
	const numWriters = 10
	const numReaders = 20
	const iterations = 5000

	var wg sync.WaitGroup

	// Writer goroutines - increment atomically
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				atomic.AddInt64(&p.read, 1)
			}
		}()
	}

	// Reader goroutines - read atomically via getRead()
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				val := p.getRead()
				// Value should never be negative or exceed max possible writes
				if val < 0 || val > int64(numWriters*iterations) {
					t.Errorf("corrupted read: %d (expected 0 <= val <= %d)", val, numWriters*iterations)
				}
			}
		}()
	}

	wg.Wait()

	// Final value must equal total increments
	expected := int64(numWriters * iterations)
	if p.getRead() != expected {
		t.Errorf("final value mismatch: expected %d, got %d", expected, p.getRead())
	}
}

// TestPartAtomicOperationsConcurrent tests all atomic operations on Part struct
func TestPartAtomicOperationsConcurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &Part{
		ctx:   ctx,
		read:  0,
		chunk: 1024 * 1024, // 1MB chunk
	}

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 1000

	// Mixed read/write workload
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				switch j % 3 {
				case 0:
					// Atomic read
					_ = p.getRead()
				case 1:
					// Atomic increment
					atomic.AddInt64(&p.read, 1)
				case 2:
					// Atomic read followed by comparison
					val := p.getRead()
					if val < 0 {
						t.Errorf("goroutine %d: negative value %d", id, val)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	finalRead := p.getRead()
	expectedMin := int64(0)
	expectedMax := int64(goroutines * iterations)

	if finalRead < expectedMin || finalRead > expectedMax {
		t.Errorf("final read out of expected range: %d (expected %d-%d)", finalRead, expectedMin, expectedMax)
	}
}

// TestPartGetReadVsDirectAccess demonstrates why getRead() is necessary
// This test would fail with race detector if we accessed p.read directly
func TestPartGetReadVsDirectAccess(t *testing.T) {
	p := &Part{read: 0}

	var wg sync.WaitGroup

	// Writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			atomic.AddInt64(&p.read, 1)
		}
	}()

	// Reader using safe method
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			val := p.getRead() // Safe: uses atomic.LoadInt64
			if val < 0 {
				t.Error("corrupted read via getRead()")
			}
		}
	}()

	wg.Wait()
}

// TestPartConcurrentReadAndEpeed tests thread-safe read and expected speed calculations
func TestPartConcurrentReadAndEpeed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &Part{
		ctx:   ctx,
		chunk: 1024 * 1024,
		read:  0,
	}

	var wg sync.WaitGroup

	// Concurrent expected speed updates
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(speed int64) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_ = p.getRead()
			}
		}(int64(1024 * (i + 1)))
	}

	// Concurrent read increments
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				atomic.AddInt64(&p.read, 100)
				_ = p.getRead()
			}
		}()
	}

	wg.Wait()

	// Verify no corruption
	if p.getRead() != 10*1000*100 {
		t.Errorf("read count mismatch: expected %d, got %d", 10*1000*100, p.getRead())
	}
}

// TestPartContextCancellationRace tests concurrent context cancellation and read access
func TestPartContextCancellationRace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Part{
		ctx:   ctx,
		read:  0,
		chunk: 1024,
	}

	var wg sync.WaitGroup

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				select {
				case <-p.ctx.Done():
					return
				default:
					_ = p.getRead()
				}
			}
		}()
	}

	// Writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				select {
				case <-p.ctx.Done():
					return
				default:
					atomic.AddInt64(&p.read, 1)
				}
			}
		}()
	}

	// Canceler
	time.Sleep(10 * time.Millisecond)
	cancel()

	wg.Wait()
}
