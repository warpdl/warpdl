package warplib

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetRange(t *testing.T) {
	header := http.Header{}
	setRange(header, 5, 10)
	if header.Get("Range") != "bytes=5-10" {
		t.Fatalf("unexpected range: %s", header.Get("Range"))
	}
	setRange(header, 0, 0)
	if header.Get("Range") != "bytes=0-" {
		t.Fatalf("unexpected range for open end: %s", header.Get("Range"))
	}
}

// errorWriter is a writer that always returns an error
type errorWriter struct {
	err error
}

func (e errorWriter) Write(p []byte) (int, error) {
	return 0, e.err
}

// partialWriter writes fewer bytes than requested
type partialWriter struct {
	writeCount int
}

func (p *partialWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	// Return fewer bytes than provided (but not zero to avoid triggering invalid write)
	return p.writeCount, nil
}

// limitedReader reads only a limited number of bytes before returning EOF
type limitedReader struct {
	data []byte
	pos  int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.pos >= len(l.data) {
		return 0, io.EOF
	}
	n := copy(p, l.data[l.pos:])
	l.pos += n
	return n, nil
}

func (l *limitedReader) Close() error {
	return nil
}

// TestCopyBufferChunkWriteError tests copyBufferChunk when Write fails with an error.
func TestCopyBufferChunkWriteError(t *testing.T) {
	writeErr := errors.New("write failed")
	p := &Part{
		pfunc: func(string, int) {},
		hash:  "p1",
	}
	buf := make([]byte, 3)
	err := p.copyBufferChunk(bytes.NewReader([]byte("abc")), errorWriter{err: writeErr}, buf)
	if err != writeErr {
		t.Fatalf("expected write error %v, got %v", writeErr, err)
	}
	p.pwg.Wait()
}

// TestCopyBufferChunkShortWritePartial tests copyBufferChunk when Write returns fewer bytes than read.
func TestCopyBufferChunkShortWritePartial(t *testing.T) {
	p := &Part{
		pfunc: func(string, int) {},
		hash:  "p1",
	}
	// Writer returns 1 byte written when we try to write 3
	pw := &partialWriter{writeCount: 1}
	buf := make([]byte, 3)
	err := p.copyBufferChunk(bytes.NewReader([]byte("abc")), pw, buf)
	if err != io.ErrShortWrite {
		t.Fatalf("expected io.ErrShortWrite, got %v", err)
	}
	p.pwg.Wait()
}

// TestCopyBufferPrematureEOF tests that copyBuffer detects premature EOF.
// When the reader returns EOF before all expected bytes are read, it should
// return io.ErrUnexpectedEOF.
func TestCopyBufferPrematureEOF(t *testing.T) {
	dir := t.TempDir()
	partFile, err := os.Create(filepath.Join(dir, "part.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer partFile.Close()

	p := &Part{
		pf:     partFile,
		hash:   "p1",
		chunk:  4,
		offset: 0,
		read:   0,
		pfunc:  func(string, int) {},
		ofunc:  func(string, int64) { t.Fatalf("completion handler should not be called on premature EOF") },
	}

	// Reader only has 5 bytes but we expect 10 bytes (foff=9 means bytes 0-9, total 10 bytes)
	reader := &limitedReader{data: []byte("hello")}
	slow, err := p.copyBuffer(reader, 9, true)
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("expected io.ErrUnexpectedEOF, got %v", err)
	}
	if slow {
		t.Fatalf("expected slow=false")
	}
	p.pwg.Wait()
}

// TestCopyBufferCompleteEOF tests that copyBuffer treats complete EOF as success.
// When exactly the expected number of bytes is read, err should be nil and
// the completion handler should be called.
func TestCopyBufferCompleteEOF(t *testing.T) {
	dir := t.TempDir()
	partFile, err := os.Create(filepath.Join(dir, "part.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer partFile.Close()

	completionCalled := false
	p := &Part{
		pf:     partFile,
		hash:   "p1",
		chunk:  4,
		offset: 0,
		read:   0,
		l:      log.New(io.Discard, "", 0),
		pfunc:  func(string, int) {},
		ofunc: func(hash string, tread int64) {
			completionCalled = true
			if hash != "p1" {
				t.Errorf("expected hash p1, got %s", hash)
			}
			if tread != 5 {
				t.Errorf("expected 5 bytes read, got %d", tread)
			}
		},
	}

	// Reader has exactly 5 bytes and we expect 5 bytes (foff=4 means bytes 0-4, total 5 bytes)
	reader := &limitedReader{data: []byte("hello")}
	slow, err := p.copyBuffer(reader, 4, true)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if slow {
		t.Fatalf("expected slow=false")
	}
	p.pwg.Wait()
	if !completionCalled {
		t.Fatalf("expected completion handler to be called")
	}
}

// seekErrorFile is an os.File wrapper that returns an error on Seek
type seekErrorFile struct {
	*os.File
	seekErr error
}

func (s *seekErrorFile) Read(p []byte) (int, error) {
	return 0, s.seekErr
}

// TestPartCompileSeekError tests compile() when seek fails.
// The compile function calls Seek(0, 0) and then starts reading.
// If the underlying file has issues, the read will fail.
func TestPartCompileSeekError(t *testing.T) {
	dir := t.TempDir()

	// Create a part file with some data
	partPath := filepath.Join(dir, "part.bin")
	if err := os.WriteFile(partPath, []byte("test data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Open the part file then close it to cause read errors
	partFile, err := os.Open(partPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	partFile.Close() // Close to cause subsequent read/seek to fail

	mainFile, err := os.Create(filepath.Join(dir, "main.bin"))
	if err != nil {
		t.Fatalf("Create main: %v", err)
	}
	defer mainFile.Close()

	p := &Part{
		pf:    partFile, // closed file
		f:     mainFile,
		chunk: 4,
		hash:  "p1",
		pfunc: func(string, int) {},
		cfunc: func(string, int) {},
	}

	_, _, err = p.compile()
	if err == nil {
		t.Fatalf("expected compile to return error on closed file")
	}
	p.pwg.Wait()
}

// mockRoundTripper allows customizing HTTP responses for testing
type mockRoundTripper func(*http.Request) (*http.Response, error)

func (f mockRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// TestPartDownloadRequestError tests download() when the HTTP request fails.
func TestPartDownloadRequestError(t *testing.T) {
	dir := t.TempDir()
	mainFile, err := os.Create(filepath.Join(dir, "main.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer mainFile.Close()

	requestErr := errors.New("connection refused")
	client := &http.Client{
		Transport: mockRoundTripper(func(*http.Request) (*http.Response, error) {
			return nil, requestErr
		}),
	}

	p := &Part{
		ctx:     context.Background(),
		url:     "http://example.com/file.bin",
		chunk:   4,
		client:  client,
		preName: filepath.Join(dir, "part-"),
		pfunc:   func(string, int) {},
		ofunc:   func(string, int64) {},
		cfunc:   func(string, int) {},
		l:       log.New(io.Discard, "", 0),
		offset:  0,
		f:       mainFile,
		hash:    "p1",
	}

	// Create part file
	if err := p.createPartFile(); err != nil {
		t.Fatalf("createPartFile: %v", err)
	}
	defer p.close()

	_, _, err = p.download(nil, 0, 99, false, 0)
	if err == nil {
		t.Fatalf("expected download to return error")
	}
	// The HTTP client wraps the transport error, so check if it contains our error message
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected error to contain 'connection refused', got %v", err)
	}
}

// TestPartDownloadRequestCreationError tests download() when http.NewRequestWithContext fails.
// This happens when the context is cancelled before the request is made.
func TestPartDownloadRequestCreationError(t *testing.T) {
	dir := t.TempDir()
	mainFile, err := os.Create(filepath.Join(dir, "main.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer mainFile.Close()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := &Part{
		ctx:     ctx,
		url:     "http://example.com/file.bin",
		chunk:   4,
		client:  &http.Client{},
		preName: filepath.Join(dir, "part-"),
		pfunc:   func(string, int) {},
		ofunc:   func(string, int64) {},
		cfunc:   func(string, int) {},
		l:       log.New(io.Discard, "", 0),
		offset:  0,
		f:       mainFile,
		hash:    "p1",
	}

	// Create part file
	if err := p.createPartFile(); err != nil {
		t.Fatalf("createPartFile: %v", err)
	}
	defer p.close()

	_, _, err = p.download(nil, 0, 99, false, 0)
	if err == nil {
		t.Fatalf("expected error from cancelled context")
	}
}
