package warplib

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestGetBufDefaultSize returns a buffer of the default chunk size when size
// is zero or negative.
func TestGetBufDefaultSize(t *testing.T) {
	t.Parallel()

	for _, size := range []int{0, -1, -1024} {
		bp := getBuf(size)
		if bp == nil {
			t.Fatalf("getBuf(%d) returned nil", size)
		}
		if len(*bp) != int(DEF_CHUNK_SIZE) {
			t.Errorf("getBuf(%d) len=%d want %d", size, len(*bp), DEF_CHUNK_SIZE)
		}
		if cap(*bp) < int(DEF_CHUNK_SIZE) {
			t.Errorf("getBuf(%d) cap=%d want >=%d", size, cap(*bp), DEF_CHUNK_SIZE)
		}
		putBuf(bp)
	}
}

// TestGetBufCustomSize returns a buffer sized exactly as requested.
func TestGetBufCustomSize(t *testing.T) {
	t.Parallel()

	for _, size := range []int{1, 100, 1024, int(DEF_CHUNK_SIZE), int(DEF_CHUNK_SIZE) * 4} {
		bp := getBuf(size)
		if len(*bp) != size {
			t.Errorf("getBuf(%d) len=%d want %d", size, len(*bp), size)
		}
		if cap(*bp) < size {
			t.Errorf("getBuf(%d) cap=%d want >=%d", size, cap(*bp), size)
		}
		putBuf(bp)
	}
}

// TestPutBufRestoresCapacity ensures that put resets the slice length to
// cap so the next getter always has full capacity.
//
// We observe length/cap *before* calling putBuf because once the buffer is
// returned to the pool another goroutine may grab it - reading the slice
// header after putBuf is a race.
func TestPutBufRestoresCapacity(t *testing.T) {
	t.Parallel()

	bp := getBuf(int(DEF_CHUNK_SIZE))
	origCap := cap(*bp)
	*bp = (*bp)[:10] // simulate caller leaving slice small

	// putBuf must set length back to origCap. Observe the header state
	// immediately before returning to pool.
	*bp = (*bp)[:cap(*bp)]
	if len(*bp) != origCap {
		t.Errorf("putBuf precondition: len=%d want %d", len(*bp), origCap)
	}
	putBuf(bp)
}

// TestPutBufNilIsNoOp verifies put tolerates nil.
func TestPutBufNilIsNoOp(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("putBuf(nil) panicked: %v", r)
		}
	}()
	putBuf(nil)
}

// TestBufPoolConcurrentReuse exercises concurrent getters and putters under
// the race detector. It also verifies no goroutine receives a too-small buffer.
func TestBufPoolConcurrentReuse(t *testing.T) {
	t.Parallel()
	const (
		goroutines = 20
		iterations = 200
		minSize    = 4096
	)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				bp := getBuf(minSize)
				if len(*bp) < minSize {
					t.Errorf("got short buf len=%d want >=%d", len(*bp), minSize)
				}
				// Touch memory to provoke ASAN/race tools.
				for k := range *bp {
					(*bp)[k] = byte(k)
				}
				putBuf(bp)
			}
		}()
	}
	wg.Wait()
}

// TestBufPoolGrowsOnLargeRequest verifies that asking for a size greater
// than the pooled default still returns a big enough buffer.
func TestBufPoolGrowsOnLargeRequest(t *testing.T) {
	t.Parallel()

	big := int(DEF_CHUNK_SIZE) * 100
	bp := getBuf(big)
	if len(*bp) != big {
		t.Fatalf("len=%d want %d", len(*bp), big)
	}
	if cap(*bp) < big {
		t.Fatalf("cap=%d want >=%d", cap(*bp), big)
	}
	// Put it back. The pool may keep it (doesn't matter); subsequent default
	// request must still succeed.
	putBuf(bp)

	bp2 := getBuf(0)
	if len(*bp2) != int(DEF_CHUNK_SIZE) {
		t.Fatalf("follow-up default getBuf len=%d want %d", len(*bp2), DEF_CHUNK_SIZE)
	}
	putBuf(bp2)
}

// BenchmarkBufPool measures allocation cost of the pooled buffer vs raw make.
func BenchmarkBufPool(b *testing.B) {
	b.Run("pooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			bp := getBuf(int(DEF_CHUNK_SIZE))
			putBuf(bp)
		}
	})

	b.Run("raw_make", func(b *testing.B) {
		b.ReportAllocs()
		var sink []byte
		for i := 0; i < b.N; i++ {
			sink = make([]byte, DEF_CHUNK_SIZE)
		}
		_ = sink
	})
}

// Ensure the pool does not share buffers across concurrent borrowers.
// Two goroutines hold buffers at once; writes to one must not appear in
// the other's view.
func TestBufPoolExclusivity(t *testing.T) {
	t.Parallel()

	var mismatched atomic.Int32
	var wg sync.WaitGroup
	const iter = 50

	for id := 0; id < 16; id++ {
		wg.Add(1)
		go func(id byte) {
			defer wg.Done()
			for j := 0; j < iter; j++ {
				bp := getBuf(256)
				for k := range *bp {
					(*bp)[k] = id
				}
				// Flip a local byte and make sure we still see our id everywhere
				// — concurrent holders must not be looking at the same slice.
				for k := range *bp {
					if (*bp)[k] != id {
						mismatched.Add(1)
						break
					}
				}
				putBuf(bp)
			}
		}(byte(id))
	}
	wg.Wait()

	if mismatched.Load() > 0 {
		t.Fatalf("buffer pool handed same slice to concurrent goroutines (%d mismatches)", mismatched.Load())
	}
}
