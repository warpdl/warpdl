package warplib

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TryRemoveWithRetry attempts to remove a file or directory with retries.
// This is necessary because on Windows, file handles may not be released
// immediately after closing, causing os.Remove to fail temporarily.
//
// Parameters:
//   - path: absolute path to the file or directory to remove
//   - maxRetries: maximum number of retry attempts
//   - delay: duration to wait between retries
//
// Returns an error if all retry attempts are exhausted.
func TryRemoveWithRetry(path string, maxRetries int, delay time.Duration) error {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		err := os.RemoveAll(path)
		if err == nil {
			return nil
		}
		lastErr = err
		if i < maxRetries-1 {
			time.Sleep(delay)
		}
	}
	return lastErr
}

// TestDownloaderCloseWithoutStart verifies that a Downloader properly releases
// the log file handle when Close() is called without ever starting a download.
//
// This test currently FAILS because:
//   - Downloader.Close() method does not exist
//   - Log file (d.lw) opened in setupLogger() is never closed
//   - On Windows, this prevents cleanup of temp directories
//
// Expected behavior after fix:
//   - Close() should exist and close d.lw (log writer)
//   - Close() should be idempotent (safe to call multiple times)
//   - After Close(), temp directory should be removable
func TestDownloaderCloseWithoutStart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := []byte("test-content")
	srv := newRangeServer(t, content)
	defer srv.Close()

	// Create Downloader but never call Start()
	d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    2,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	// Get paths before closing
	dlPath := d.dlPath
	logPath := filepath.Join(dlPath, "logs.txt")

	// Verify log file was created
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatalf("log file was not created at %s", logPath)
	}

	// Close the downloader to release resources
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After Close(), verify log file can be removed
	// On Windows, this will fail if file handles aren't properly released
	if runtime.GOOS == "windows" {
		err = TryRemoveWithRetry(logPath, 5, 100*time.Millisecond)
	} else {
		err = os.Remove(logPath)
	}
	if err != nil {
		t.Errorf("failed to remove log file after Close(): %v", err)
	}

	// Verify entire download directory can be cleaned up
	if runtime.GOOS == "windows" {
		err = TryRemoveWithRetry(dlPath, 5, 100*time.Millisecond)
	} else {
		err = os.RemoveAll(dlPath)
	}
	if err != nil {
		t.Errorf("failed to remove download directory after Close(): %v", err)
	}
}

// TestDownloaderStopClosesAllHandles verifies that calling Close() after Stop()
// properly releases all file handles (log file and download file).
//
// Expected behavior:
//   - Stop() signals cancellation
//   - Close() releases all file handles after Start() returns
//   - All temp files should be removable after Close()
func TestDownloaderStopClosesAllHandles(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Use small content with delays to allow stopping mid-download
	content := bytes.Repeat([]byte("x"), 8192) // 8KB
	srv := newChunkedServer(t, content, 5*time.Millisecond)
	defer srv.Close()

	var d *Downloader
	stopOnce := sync.Once{}

	handlers := &Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			// Stop after first progress update
			stopOnce.Do(func() {
				if d != nil {
					d.Stop()
				}
			})
		},
	}

	var err error
	d, err = NewDownloader(&http.Client{Timeout: 5 * time.Second}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    2,
		Handlers:          handlers,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	// Capture paths before starting
	dlPath := d.dlPath

	// Start download - will be stopped by progress handler
	_ = d.Start() // Ignore error, we expect it to be stopped

	// Close to release all resources
	if err := d.Close(); err != nil {
		t.Logf("Close returned error (may be expected after stop): %v", err)
	}

	// Brief pause for Windows file handle release
	if runtime.GOOS == "windows" {
		time.Sleep(100 * time.Millisecond)
	}

	// Verify download directory can be cleaned up
	err = TryRemoveWithRetry(dlPath, 5, 100*time.Millisecond)
	if err != nil {
		t.Errorf("failed to remove download directory after Close(): %v", err)
	}
}

// TestDownloaderCompleteClosesAllHandles verifies that a completed download
// properly releases all file handles, allowing temp files to be cleaned up.
//
// Expected behavior:
//   - After successful download, all handles should be released
//   - Temp directory (with logs) should be removable
//   - Close() should be safe to call after successful download
func TestDownloaderCompleteClosesAllHandles(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("y"), 4096) // Small file for quick download
	srv := newRangeServer(t, content)
	defer srv.Close()

	var completedOnce sync.Once
	completed := make(chan struct{})
	var d *Downloader

	handlers := &Handlers{
		DownloadCompleteHandler: func(hash string, size int64) {
			// Use sync.Once to ensure we only close the channel once
			// even if handler is called multiple times
			completedOnce.Do(func() {
				close(completed)
			})
		},
	}

	d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    2,
		MaxSegments:       2,
		Handlers:          handlers,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	// Capture paths
	dlPath := d.dlPath
	logPath := filepath.Join(dlPath, "logs.txt")
	savePath := d.GetSavePath()

	// Start download
	err = d.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for completion
	select {
	case <-completed:
		// Expected
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for download to complete")
	}

	// Give brief time for cleanup
	time.Sleep(100 * time.Millisecond)

	// Verify downloaded file exists and matches
	got, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content mismatch")
	}

	// After successful download, Start() defers should have closed files
	// However, we should verify handles are actually released

	// Try to remove log file
	if runtime.GOOS == "windows" {
		err = TryRemoveWithRetry(logPath, 5, 100*time.Millisecond)
	} else {
		err = os.Remove(logPath)
	}
	if err != nil {
		t.Errorf("failed to remove log file after complete download: %v", err)
		t.Logf("Log file handle may not have been properly released")
	}

	// Try to remove temp directory (but keep downloaded file)
	// First remove downloaded file so we can test temp dir cleanup
	if err := os.Remove(savePath); err != nil {
		t.Fatalf("failed to remove downloaded file: %v", err)
	}

	if runtime.GOOS == "windows" {
		err = TryRemoveWithRetry(dlPath, 5, 100*time.Millisecond)
	} else {
		err = os.RemoveAll(dlPath)
	}
	if err != nil {
		t.Errorf("failed to remove download directory after complete download: %v", err)
		t.Logf("Temp directory cleanup failed, handles may not be fully released")
	}

	// Test that Close() is safe to call after successful download
	if err := d.Close(); err != nil {
		t.Logf("Close after complete returned: %v (may be expected)", err)
	}
}

// TestTryRemoveWithRetry verifies the retry helper function works correctly.
func TestTryRemoveWithRetry(t *testing.T) {
	base := t.TempDir()

	// Create a test file
	testFile := filepath.Join(base, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Should succeed on first try
	err := TryRemoveWithRetry(testFile, 3, 10*time.Millisecond)
	if err != nil {
		t.Errorf("TryRemoveWithRetry failed: %v", err)
	}

	// Verify file was removed
	if _, err := os.Stat(testFile); !os.IsNotExist(err) {
		t.Errorf("file still exists after TryRemoveWithRetry")
	}

	// Test with directory containing file
	testDir := filepath.Join(base, "testdir")
	if err := os.Mkdir(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	testFile2 := filepath.Join(testDir, "file.txt")
	if err := os.WriteFile(testFile2, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file in dir: %v", err)
	}

	err = TryRemoveWithRetry(testDir, 3, 10*time.Millisecond)
	if err != nil {
		t.Errorf("TryRemoveWithRetry failed on directory: %v", err)
	}

	// Verify directory was removed
	if _, err := os.Stat(testDir); !os.IsNotExist(err) {
		t.Errorf("directory still exists after TryRemoveWithRetry")
	}
}
