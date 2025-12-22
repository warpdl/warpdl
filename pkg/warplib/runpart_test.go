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
	"sync"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type slowReadCloser struct {
	data  []byte
	delay time.Duration
	pos   int
}

func (s *slowReadCloser) Read(p []byte) (int, error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	n := copy(p, s.data[s.pos:])
	s.pos += n
	return n, nil
}

func (s *slowReadCloser) Close() error {
	return nil
}

func newRunPartDownloader(t *testing.T, client *http.Client, preName string, f *os.File) *Downloader {
	t.Helper()
	handlers := &Handlers{}
	handlers.setDefault(log.New(io.Discard, "", 0))
	return &Downloader{
		ctx:      context.Background(),
		client:   client,
		url:      "http://example.com/file.bin",
		chunk:    1,
		handlers: handlers,
		l:        log.New(io.Discard, "", 0),
		wg:       &sync.WaitGroup{},
		maxConn:  2,
		dlPath:   preName,
		f:        f,
	}
}

func newRunPart(t *testing.T, d *Downloader, preName string, f *os.File) *Part {
	t.Helper()
	part, err := newPart(d.ctx, d.client, d.url, partArgs{
		copyChunk: 1,
		preName:   preName,
		rpHandler: func(string, int) {},
		pHandler:  func(string, int) {},
		oHandler:  func(string, int64) {},
		cpHandler: func(string, int) {},
		logger:    d.l,
		offset:    0,
		f:         f,
	})
	if err != nil {
		t.Fatalf("newPart: %v", err)
	}
	return part
}

func TestRunPartDownloadError(t *testing.T) {
	base := t.TempDir()
	mainFile, err := os.Create(filepath.Join(base, "main.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer mainFile.Close()

	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		}),
	}
	preName := filepath.Join(base, "part-")
	d := newRunPartDownloader(t, client, preName, mainFile)
	called := false
	d.handlers.ErrorHandler = func(string, error) { called = true }

	part := newRunPart(t, d, preName, mainFile)
	defer part.close()

	if err := d.runPart(part, 0, 2, MB, false, nil); err == nil {
		t.Fatalf("expected runPart to return error")
	}
	if !called {
		t.Fatalf("expected error handler to be called")
	}
}

func TestRunPartSlowMinPartSize(t *testing.T) {
	base := t.TempDir()
	mainFile, err := os.Create(filepath.Join(base, "main.bin"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer mainFile.Close()

	reader := &slowReadCloser{
		data:  bytes.Repeat([]byte("a"), 32),
		delay: 2 * time.Millisecond,
	}
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       reader,
				Header:     make(http.Header),
			}, nil
		}),
	}
	preName := filepath.Join(base, "part-")
	d := newRunPartDownloader(t, client, preName, mainFile)

	part := newRunPart(t, d, preName, mainFile)
	defer part.close()

	if err := d.runPart(part, 0, 15, 10*MB, false, nil); err != nil {
		t.Fatalf("runPart: %v", err)
	}
}
