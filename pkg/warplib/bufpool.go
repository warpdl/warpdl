package warplib

import (
	"sync"
)

// bufPool recycles chunk-sized byte slices used by the download copy loops
// and the checksum validator. Keeping a pool dramatically reduces GC pressure
// on large downloads where the same 32 KB buffers are otherwise allocated
// hundreds of thousands of times per gigabyte.
//
// The pool stores *[]byte rather than []byte because sync.Pool cannot
// distinguish slice header updates from distinct allocations; storing the
// pointer avoids implicit re-boxing when the backing array is resliced.
var bufPool = sync.Pool{
	New: func() any {
		b := make([]byte, DEF_CHUNK_SIZE)
		return &b
	},
}

// getBuf returns a buffer of at least the requested size. If size == 0,
// a buffer of DEF_CHUNK_SIZE is returned. The caller must release the
// buffer with putBuf when done.
//
// When size exceeds the pool's default capacity, a fresh buffer is
// allocated and returned; the caller should still putBuf it — this keeps
// the pool healthy for subsequent callers that might need the larger cap.
func getBuf(size int) *[]byte {
	if size <= 0 {
		size = int(DEF_CHUNK_SIZE)
	}
	bp := bufPool.Get().(*[]byte)
	if cap(*bp) < size {
		// Underlying array is too small. Discard and allocate to avoid
		// handing a short buffer back to a caller that asked for more.
		nb := make([]byte, size)
		*bp = nb
		return bp
	}
	*bp = (*bp)[:size]
	return bp
}

// putBuf returns a buffer to the pool. Safe to call with a nil pointer.
func putBuf(bp *[]byte) {
	if bp == nil {
		return
	}
	// Reset slice length; keep capacity so the backing array is reusable.
	*bp = (*bp)[:cap(*bp)]
	bufPool.Put(bp)
}
