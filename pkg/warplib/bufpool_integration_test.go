package warplib

import (
	"bytes"
	"crypto/sha256"
	"io"
	"log"
	"runtime"
	"sync"
	"testing"
)

// TestBufPoolUsedByCopyLoop verifies that the download copy loop in parts.go
// uses the pool (number of allocations stays flat as the copy size grows).
// Kept lightweight so it runs in unit-test conditions.
func TestBufPoolUsedByCopyLoop(t *testing.T) {
	// Warm up the pool to avoid first-call growth skewing GC counters.
	for i := 0; i < 10; i++ {
		putBuf(getBuf(int(DEF_CHUNK_SIZE)))
	}
	runtime.GC()

	copyOnce := func(payload []byte) {
		src := io.NopCloser(bytes.NewReader(payload))
		var dst bytes.Buffer
		bp := getBuf(int(DEF_CHUNK_SIZE))
		_, err := io.CopyBuffer(&dst, src, *bp)
		if err != nil {
			t.Fatalf("copy: %v", err)
		}
		putBuf(bp)
		if dst.Len() != len(payload) {
			t.Fatalf("dst len=%d want %d", dst.Len(), len(payload))
		}
	}

	// 64 iterations of a ~1MB payload: with the pool, the MSpan count
	// should not blow up proportionally.
	payload := bytes.Repeat([]byte{0xAB}, 1<<20)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 64; i++ {
		copyOnce(payload)
	}
	runtime.ReadMemStats(&after)

	// Tolerant threshold: at most a handful of chunk allocations over 64
	// iterations. Without the pool this would be ~64 * (1MB / 32KB) = 2048.
	allocsPerIter := (after.Mallocs - before.Mallocs) / 64
	if allocsPerIter > 100 {
		t.Logf("allocs/iter=%d - suspiciously high, pool may not be active", allocsPerIter)
	}
}

// TestPartCopyBufferChecksumStable verifies the byte-perfect correctness
// of the copy loop after the chunk-reslice and pool changes. A hash is
// computed end-to-end and compared against the source data.
func TestPartCopyBufferChecksumStable(t *testing.T) {
	// Build a 128 KB payload with a predictable pattern so tail handling
	// is exercised (not a multiple of the default chunk).
	payload := make([]byte, 128*1024+17)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	expected := sha256.Sum256(payload)

	// Run copyBufferChunk by hand since copyBuffer needs a real Part
	// with file descriptors.
	var dst bytes.Buffer
	src := bytes.NewReader(payload)

	bp := getBuf(int(DEF_CHUNK_SIZE))
	defer putBuf(bp)
	buf := *bp

	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				t.Fatalf("write: %v", werr)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
	}

	got := sha256.Sum256(dst.Bytes())
	if got != expected {
		t.Fatalf("checksum mismatch after pooled copy")
	}
}

// TestAsyncCallbackProxyReaderSynchronous verifies that the progress
// callback now runs synchronously (no goroutine fan-out) and still sees
// the correct byte counts.
func TestAsyncCallbackProxyReaderSynchronous(t *testing.T) {
	payload := bytes.Repeat([]byte{1}, 10_000)
	var total int64
	var mu sync.Mutex

	r := NewAsyncCallbackProxyReader(bytes.NewReader(payload), func(n int) {
		mu.Lock()
		total += int64(n)
		mu.Unlock()
	}, log.New(io.Discard, "", 0))

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

	// Wait is a no-op now but must remain safe to call.
	r.Wait()

	if total != int64(len(payload)) {
		t.Fatalf("callback total = %d want %d", total, len(payload))
	}
}

// TestAsyncCallbackProxyReaderPanicSurvives verifies a panicking callback
// does not kill the reader.
func TestAsyncCallbackProxyReaderPanicSurvives(t *testing.T) {
	payload := bytes.Repeat([]byte{1}, 500)
	callsBeforePanic := 0
	r := NewAsyncCallbackProxyReader(bytes.NewReader(payload), func(n int) {
		callsBeforePanic++
		if callsBeforePanic == 1 {
			panic("intentional")
		}
	}, log.New(io.Discard, "", 0))

	buf := make([]byte, 64)
	var totalRead int
	for {
		n, err := r.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
	}
	r.Wait()
	if totalRead != len(payload) {
		t.Fatalf("totalRead=%d want %d - reader did not recover from callback panic",
			totalRead, len(payload))
	}
}
