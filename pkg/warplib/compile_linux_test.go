//go:build linux

package warplib

import (
	"bytes"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func stubCopyFileRange(t *testing.T, fn func(int, *int64, int, *int64, int, int) (int, error)) {
	t.Helper()
	orig := copyFileRange
	copyFileRange = fn
	t.Cleanup(func() { copyFileRange = orig })
}

func TestPartCompileFallsBackWhenKernelCopyUnsupported(t *testing.T) {
	stubCopyFileRange(t, func(int, *int64, int, *int64, int, int) (int, error) {
		return 0, unix.ENOSYS
	})
	content := bytes.Repeat([]byte("fallback"), int(compileBufferSize)/4)
	const offset = 512
	p, progress := newCompileTestPart(t, content, offset, 0)

	read, written, err := p.compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if read != int64(len(content)) || written != int64(len(content)) || *progress != int64(len(content)) {
		t.Fatalf("read=%d written=%d progress=%d, want %d", read, written, *progress, len(content))
	}
	got := make([]byte, len(content))
	if _, err := p.f.ReadAt(got, offset); err != nil {
		t.Fatalf("read main: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("fallback compile output differs")
	}
}

func TestPartCompileKernelCopyErrorAfterProgress(t *testing.T) {
	calls := 0
	stubCopyFileRange(t, func(rfd int, roff *int64, wfd int, woff *int64, n int, flags int) (int, error) {
		calls++
		switch calls {
		case 1:
			return 0, unix.EINTR
		case 2:
			return unix.CopyFileRange(rfd, roff, wfd, woff, 16, flags)
		default:
			return 0, unix.EIO
		}
	})
	p, _ := newCompileTestPart(t, bytes.Repeat([]byte("x"), 64), 0, 0)

	read, written, handled, err := p.compileInKernel()
	if !handled || !errors.Is(err, unix.EIO) {
		t.Fatalf("handled=%v err=%v, want handled EIO once bytes were copied", handled, err)
	}
	if read != 16 || written != 16 {
		t.Fatalf("read=%d written=%d, want 16", read, written)
	}
}
