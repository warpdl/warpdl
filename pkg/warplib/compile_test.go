package warplib

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// newCompileTestPart returns a part whose part file holds content and whose
// main file is opened with mainFlag, ready to compile at offset.
func newCompileTestPart(t *testing.T, content []byte, offset int64, mainFlag int) (*Part, *int64) {
	t.Helper()
	dir := t.TempDir()
	pf, err := os.Create(filepath.Join(dir, "part.warp"))
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	t.Cleanup(func() { _ = pf.Close() })
	if _, err := pf.Write(content); err != nil {
		t.Fatalf("write part: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "main.bin"), os.O_CREATE|os.O_RDWR|mainFlag, 0o644)
	if err != nil {
		t.Fatalf("open main: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	progress := new(int64)
	return &Part{
		pf:     pf,
		f:      f,
		offset: offset,
		chunk:  DEF_CHUNK_SIZE,
		hash:   "compile",
		l:      log.New(io.Discard, "", 0),
		cfunc: func(_ string, n int) {
			*progress += int64(n)
		},
	}, progress
}

func TestPartCompileWritesAtOffset(t *testing.T) {
	content := make([]byte, 3*compileBufferSize+123)
	for i := range content {
		content[i] = byte(i % 251)
	}
	const offset = 4096
	p, progress := newCompileTestPart(t, content, offset, 0)

	read, written, err := p.compileExact(int64(len(content)))
	if err != nil {
		t.Fatalf("compileExact: %v", err)
	}
	if read != int64(len(content)) || written != int64(len(content)) {
		t.Fatalf("read=%d written=%d, want %d", read, written, len(content))
	}
	if *progress != int64(len(content)) {
		t.Fatalf("compile progress = %d, want %d", *progress, len(content))
	}
	got := make([]byte, len(content))
	if _, err := p.f.ReadAt(got, offset); err != nil {
		t.Fatalf("read main: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("compiled bytes differ from the part file")
	}
}
