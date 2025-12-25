package warplib

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestDownloadSurvivesPanicInProgressCallback tests that a panic in the
// DownloadProgressHandler callback does not crash the download process.
// This verifies that the goroutine spawned at copyBufferChunk (line 228 in parts.go)
// has proper panic recovery.
func TestDownloadSurvivesPanicInProgressCallback(t *testing.T) {
	// Setup config dir for warplib
	if err := SetConfigDir(t.TempDir()); err != nil {
		t.Fatalf("SetConfigDir failed: %v", err)
	}

	// Create test data (64 bytes to ensure progress callback is invoked)
	content := bytes.Repeat([]byte("x"), 64)

	// Setup HTTP test server that returns content with range support
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	var panicCount int32

	// Handler that panics on every progress callback
	handlers := &Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			atomic.AddInt32(&panicCount, 1)
			panic("intentional panic in progress callback")
		},
	}

	tmpDir := t.TempDir()
	opts := &DownloaderOpts{
		DownloadDirectory: tmpDir,
		MaxConnections:    1,
		Handlers:          handlers,
		FileName:          "test.bin",
	}

	d, err := NewDownloader(&http.Client{}, srv.URL, opts)
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}
	defer d.Close()

	// Create channel to detect if Start() completes or hangs
	done := make(chan error, 1)
	go func() {
		done <- d.Start()
	}()

	// Wait for download to complete or timeout
	select {
	case err := <-done:
		// Download completed - this is what we want
		// The error might be non-nil due to panic, but the important thing
		// is that it didn't hang
		if atomic.LoadInt32(&panicCount) == 0 {
			t.Fatal("expected panic to be triggered at least once")
		}
		t.Logf("Download completed with error: %v (panic count: %d)", err, atomic.LoadInt32(&panicCount))

	case <-time.After(5 * time.Second):
		// This indicates WaitGroup is hanging - TEST SHOULD FAIL
		t.Fatal("download hung - WaitGroup did not complete (goroutine panic not recovered)")
	}
}

// TestAsyncCallbackProxyReaderSurvivesPanic tests that AsyncCallbackProxyReader
// properly recovers from panics in the async callback goroutine.
// This tests the goroutine spawned at line 49 in reader.go.
func TestAsyncCallbackProxyReaderSurvivesPanic(t *testing.T) {
	// Create test data
	data := bytes.Repeat([]byte("test data"), 10)
	reader := bytes.NewReader(data)

	var panicCount int32
	var callbackCompleted int32

	// Callback that panics
	callback := func(n int) {
		count := atomic.AddInt32(&panicCount, 1)
		if count <= 3 {
			// Panic on first 3 invocations
			panic("intentional panic in async callback")
		}
		atomic.AddInt32(&callbackCompleted, 1)
	}

	proxyReader := NewAsyncCallbackProxyReader(reader, callback, nil)

	// Read all data
	buf := make([]byte, 16)
	totalRead := 0
	for {
		n, err := proxyReader.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error during read: %v", err)
		}
	}

	// Wait for async callbacks to complete
	// NOTE: This will fail to compile because Wait() doesn't exist yet on AsyncCallbackProxyReader
	// This is EXPECTED for TDD - we're defining the API we want
	proxyReader.Wait()

	if totalRead != len(data) {
		t.Fatalf("expected to read %d bytes, got %d", len(data), totalRead)
	}

	if atomic.LoadInt32(&panicCount) == 0 {
		t.Fatal("expected panic to be triggered")
	}

	// Give some time for async callbacks to complete
	time.Sleep(100 * time.Millisecond)

	t.Logf("Successfully recovered from %d panics, %d callbacks completed normally",
		atomic.LoadInt32(&panicCount), atomic.LoadInt32(&callbackCompleted))
}

// TestPartProgressCallbackPanicDoesNotHang tests that panics in the part progress
// callback goroutine don't cause pwg.Wait() to hang indefinitely.
// This tests the goroutine at copyBufferChunk (line 228-231 in parts.go).
func TestPartProgressCallbackPanicDoesNotHang(t *testing.T) {
	content := bytes.Repeat([]byte("y"), 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content)
	}))
	defer srv.Close()

	var panicCount int32

	// Progress handler that panics
	progressHandler := func(hash string, nread int) {
		atomic.AddInt32(&panicCount, 1)
		panic("intentional panic in part progress handler")
	}

	tmpDir := t.TempDir()
	mainFile, err := os.Create(filepath.Join(tmpDir, "main.bin"))
	if err != nil {
		t.Fatalf("failed to create main file: %v", err)
	}
	defer mainFile.Close()

	partsDir := filepath.Join(tmpDir, "parts")
	if err := os.MkdirAll(partsDir, 0755); err != nil {
		t.Fatalf("failed to create parts dir: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a Part with panicking progress handler
	part, err := newPart(
		ctx,
		&http.Client{},
		srv.URL,
		partArgs{
			copyChunk: 16,
			preName:   partsDir,
			rpHandler: func(string, int) {},
			pHandler:  progressHandler, // This will panic
			oHandler:  func(string, int64) {},
			cpHandler: func(string, int) {},
			logger:    log.New(io.Discard, "", 0),
			offset:    0,
			f:         mainFile,
		},
	)
	if err != nil {
		t.Fatalf("newPart failed: %v", err)
	}
	defer part.close()

	// Download the content - this will trigger progress callbacks
	_, slow, err := part.download(nil, 0, 63, true, 0)
	if err != nil {
		t.Logf("download returned error (expected due to panic): %v", err)
	}

	// The critical test: does pwg.Wait() complete without hanging?
	done := make(chan struct{})
	go func() {
		part.pwg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - Wait() completed
		if atomic.LoadInt32(&panicCount) == 0 {
			t.Fatal("expected panic to be triggered at least once")
		}
		t.Logf("pwg.Wait() completed successfully despite %d panics (slow=%v)", atomic.LoadInt32(&panicCount), slow)

	case <-time.After(5 * time.Second):
		t.Fatal("part.pwg.Wait() hung - goroutine panic not recovered")
	}
}

// TestNewPartDownloadSurvivesPanic tests that the goroutine spawned in
// newPartDownload (line 705 in dloader.go) survives panics in progress callbacks.
func TestNewPartDownloadSurvivesPanic(t *testing.T) {
	// Setup config dir for warplib
	if err := SetConfigDir(t.TempDir()); err != nil {
		t.Fatalf("SetConfigDir failed: %v", err)
	}

	content := bytes.Repeat([]byte("z"), 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "128")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content)
	}))
	defer srv.Close()

	var progressPanics int32
	var compilePanics int32

	handlers := &Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			if atomic.AddInt32(&progressPanics, 1) <= 2 {
				panic("panic in progress handler")
			}
		},
		CompileProgressHandler: func(hash string, nread int) {
			if atomic.AddInt32(&compilePanics, 1) == 1 {
				panic("panic in compile handler")
			}
		},
	}

	tmpDir := t.TempDir()
	opts := &DownloaderOpts{
		DownloadDirectory: tmpDir,
		MaxConnections:    2,
		Handlers:          handlers,
		FileName:          "test2.bin",
	}

	d, err := NewDownloader(&http.Client{}, srv.URL, opts)
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}
	defer d.Close()

	done := make(chan error, 1)
	go func() {
		done <- d.Start()
	}()

	select {
	case err := <-done:
		t.Logf("Download completed with error: %v (progress panics: %d, compile panics: %d)",
			err, atomic.LoadInt32(&progressPanics), atomic.LoadInt32(&compilePanics))

		if atomic.LoadInt32(&progressPanics) == 0 {
			t.Error("expected progress panic to be triggered")
		}

	case <-time.After(10 * time.Second):
		t.Fatal("download hung - goroutine spawned by newPartDownload did not complete")
	}
}

// TestResumePartDownloadSurvivesPanic tests that the goroutine spawned in
// resumePartDownload (line 385 in dloader.go) survives panics.
func TestResumePartDownloadSurvivesPanic(t *testing.T) {
	content := bytes.Repeat([]byte("a"), 128)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "128")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content)
	}))
	defer srv.Close()

	var panicCount int32

	handlers := &Handlers{
		ResumeProgressHandler: func(hash string, nread int) {
			if atomic.AddInt32(&panicCount, 1) <= 3 {
				panic("panic in resume handler")
			}
		},
	}
	handlers.setDefault(log.New(io.Discard, "", 0))

	tmpDir := t.TempDir()
	mainFile, err := os.Create(filepath.Join(tmpDir, "main.bin"))
	if err != nil {
		t.Fatalf("create main file: %v", err)
	}
	defer mainFile.Close()

	partsDir := filepath.Join(tmpDir, "parts")
	if err := os.MkdirAll(partsDir, 0755); err != nil {
		t.Fatalf("mkdir parts: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	retryConfig := DefaultRetryConfig()
	d := &Downloader{
		ctx:         ctx,
		cancel:      cancel,
		client:      &http.Client{},
		url:         srv.URL,
		chunk:       16,
		handlers:    handlers,
		l:           log.New(io.Discard, "", 0),
		wg:          &sync.WaitGroup{},
		maxConn:     2,
		dlPath:      partsDir,
		f:           mainFile,
		retryConfig: &retryConfig,
	}
	d.ohmap.Make()

	// Create an existing part file with some content so seek() is called
	partFile, err := os.Create(filepath.Join(partsDir, "test-hash-001.warp"))
	if err != nil {
		t.Fatalf("create part file: %v", err)
	}
	partFile.Write(bytes.Repeat([]byte("b"), 64)) // Write 64 bytes
	partFile.Close()


	// Simulate resuming a download by calling resumePartDownload
	d.wg.Add(1)

	done := make(chan struct{})
	go func() {
		d.resumePartDownload("test-hash-001", 0, 127, 4*MB)
		close(done)
	}()

	select {
	case <-done:
		d.wg.Wait() // Ensure WaitGroup completes
		if atomic.LoadInt32(&panicCount) == 0 {
			t.Error("expected panic to be triggered")
		}
		t.Logf("resumePartDownload completed despite %d panics", atomic.LoadInt32(&panicCount))

	case <-time.After(10 * time.Second):
		t.Fatal("resumePartDownload hung")
	}
}

// TestDownloadUnknownSizeFileSurvivesPanic tests that the goroutine spawned in
// downloadUnknownSizeFile (line 329 in dloader.go) survives panics in progress callback.
func TestDownloadUnknownSizeFileSurvivesPanic(t *testing.T) {
	// Setup config dir for warplib
	if err := SetConfigDir(t.TempDir()); err != nil {
		t.Fatalf("SetConfigDir failed: %v", err)
	}

	content := bytes.Repeat([]byte("b"), 64)

	// Server that doesn't report content length
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Length header
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	var panicCount int32

	handlers := &Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			if atomic.AddInt32(&panicCount, 1) <= 2 {
				panic("panic in unknown size download handler")
			}
		},
	}

	tmpDir := t.TempDir()
	opts := &DownloaderOpts{
		DownloadDirectory: tmpDir,
		Handlers:          handlers,
		FileName:          "unknown-size.bin",
	}

	d, err := NewDownloader(&http.Client{}, srv.URL, opts)
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}
	defer d.Close()

	done := make(chan error, 1)
	go func() {
		done <- d.Start()
	}()

	select {
	case err := <-done:
		t.Logf("Unknown size download completed with error: %v (panics: %d)",
			err, atomic.LoadInt32(&panicCount))

		if atomic.LoadInt32(&panicCount) == 0 {
			t.Error("expected panic to be triggered")
		}

	case <-time.After(5 * time.Second):
		t.Fatal("downloadUnknownSizeFile hung")
	}
}

// TestCompileProgressCallbackPanicDoesNotHang tests that panics in compile
// progress callbacks don't cause the compilation process to hang.
// This tests the goroutine at compile() in parts.go around line 260.
func TestCompileProgressCallbackPanicDoesNotHang(t *testing.T) {
	// Setup config dir for warplib
	if err := SetConfigDir(t.TempDir()); err != nil {
		t.Fatalf("SetConfigDir failed: %v", err)
	}

	content := bytes.Repeat([]byte("c"), 64)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(content)
	}))
	defer srv.Close()

	var compilePanics int32

	handlers := &Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			// Don't panic during download
		},
		CompileProgressHandler: func(hash string, nread int) {
			if atomic.AddInt32(&compilePanics, 1) <= 2 {
				panic("panic in compile progress handler")
			}
		},
	}

	tmpDir := t.TempDir()
	opts := &DownloaderOpts{
		DownloadDirectory: tmpDir,
		MaxConnections:    1,
		Handlers:          handlers,
		FileName:          "compile-test.bin",
	}

	d, err := NewDownloader(&http.Client{}, srv.URL, opts)
	if err != nil {
		t.Fatalf("NewDownloader failed: %v", err)
	}
	defer d.Close()

	done := make(chan error, 1)
	go func() {
		done <- d.Start()
	}()

	select {
	case err := <-done:
		t.Logf("Download completed with error: %v (compile panics: %d)",
			err, atomic.LoadInt32(&compilePanics))

		if atomic.LoadInt32(&compilePanics) == 0 {
			t.Error("expected compile panic to be triggered")
		}

	case <-time.After(10 * time.Second):
		t.Fatal("compile process hung due to panic in progress callback")
	}
}

// TestCallbackProxyReaderSurvivesPanic tests that CallbackProxyReader (synchronous)
// properly handles panics in the callback. Since it's synchronous, the panic
// will propagate to the caller, but we want to ensure cleanup still happens.
func TestCallbackProxyReaderSurvivesPanic(t *testing.T) {
	data := []byte("test data")
	reader := bytes.NewReader(data)

	panicCallback := func(n int) {
		panic("intentional panic in sync callback")
	}

	proxyReader := NewCallbackProxyReader(reader, panicCallback)

	// Attempt to read - this should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic to propagate from synchronous callback")
		} else {
			t.Logf("Successfully caught panic from synchronous callback: %v", r)
		}
	}()

	buf := make([]byte, 16)
	proxyReader.Read(buf)

	t.Fatal("should not reach here - panic should have occurred")
}
