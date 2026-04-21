package warplib

import (
	"testing"
)

// FuzzGetBuf throws random sizes at getBuf and verifies the invariants
// hold regardless of input. We bound the size to something reasonable
// so the fuzzer doesn't try to allocate 4 GB buffers on its own.
//
// Invariants:
//   - no panic for any int input
//   - if size > 0: len(*bp) == size and cap(*bp) >= size
//   - if size <= 0: len(*bp) == DEF_CHUNK_SIZE
//   - putBuf is always safe on the returned pointer
func FuzzGetBuf(f *testing.F) {
	// Seed with boundary values.
	seeds := []int{
		-1, 0, 1, 2, 31, 32,
		int(DEF_CHUNK_SIZE) - 1,
		int(DEF_CHUNK_SIZE),
		int(DEF_CHUNK_SIZE) + 1,
		int(DEF_CHUNK_SIZE) * 2,
		int(DEF_CHUNK_SIZE) * 10,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, size int) {
		// Avoid trying to allocate arbitrarily large buffers in fuzz mode.
		const maxFuzzSize = 10 * 1024 * 1024 // 10 MB
		if size > maxFuzzSize {
			t.Skip()
		}

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("getBuf(%d) panicked: %v", size, r)
			}
		}()

		bp := getBuf(size)
		if bp == nil {
			t.Fatalf("getBuf(%d) returned nil", size)
		}
		if size <= 0 {
			if len(*bp) != int(DEF_CHUNK_SIZE) {
				t.Fatalf("getBuf(%d): len=%d want %d (default)",
					size, len(*bp), DEF_CHUNK_SIZE)
			}
		} else {
			if len(*bp) != size {
				t.Fatalf("getBuf(%d): len=%d", size, len(*bp))
			}
			if cap(*bp) < size {
				t.Fatalf("getBuf(%d): cap=%d < size", size, cap(*bp))
			}
		}
		// Writing across the buffer must not panic.
		for i := range *bp {
			(*bp)[i] = byte(i)
		}
		putBuf(bp)
	})
}

// FuzzParseSpeedLimit throws random strings at the speed-limit parser
// and checks: never panics; return is either (>=0, nil) or (0, non-nil).
func FuzzParseSpeedLimit(f *testing.F) {
	seeds := []string{
		"", "0", "1", "-1", "1B", "1KB", "1.5MB", "2GB",
		"1kb", "1Mb", "1gB", "abc", "...", " ", "\t",
		"1.0.0", "999999999999999999999",
		"1PB", "1EB", "1TB", "1 MB", "1\nMB",
		"--1", "++1", "1e10", "NaN", "Inf",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ParseSpeedLimit(%q) panicked: %v", s, r)
			}
		}()

		v, err := ParseSpeedLimit(s)
		if err == nil {
			if v < 0 {
				t.Fatalf("ParseSpeedLimit(%q): negative value %d returned with nil error",
					s, v)
			}
		} else {
			if v != 0 {
				t.Fatalf("ParseSpeedLimit(%q): error returned but value=%d (should be 0)",
					s, v)
			}
		}
	})
}
