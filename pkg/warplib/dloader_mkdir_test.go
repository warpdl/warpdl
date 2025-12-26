package warplib

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSetupDlPathConcurrent tests concurrent directory creation by multiple downloaders.
// This test SHOULD FAIL with the current implementation using WarpMkdir (os.Mkdir).
// When multiple goroutines call setupDlPath() with the same hash concurrently,
// os.Mkdir will return "file exists" error for all but the first caller.
func TestSetupDlPathConcurrent(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	var errCount atomic.Int32
	errors := make([]error, numGoroutines)

	// Use a fixed hash to force collision
	testHash := "deadbeef"

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			d := &Downloader{
				hash: testHash,
			}

			err := d.setupDlPath()
			if err != nil {
				errCount.Add(1)
				errors[idx] = err
			}
		}(i)
	}

	wg.Wait()

	// All goroutines should succeed in setting up the path
	// With current implementation (WarpMkdir/os.Mkdir), this WILL FAIL
	// because only the first caller succeeds, others get "file exists" error
	if count := errCount.Load(); count > 0 {
		t.Errorf("Expected 0 errors from concurrent setupDlPath calls, got %d errors:", count)
		for i, err := range errors {
			if err != nil {
				t.Errorf("  goroutine %d: %v", i, err)
			}
		}
		t.Logf("This test is EXPECTED TO FAIL with current implementation using WarpMkdir (os.Mkdir)")
		t.Logf("Fix: Replace WarpMkdir with WarpMkdirAll in setupDlPath()")
	}

	// Verify directory was created
	dlPath := filepath.Join(DlDataDir, testHash)
	if _, err := os.Stat(dlPath); os.IsNotExist(err) {
		t.Errorf("Directory %s was not created", dlPath)
	}
}

// TestSetupDlPathIdempotent tests that calling setupDlPath multiple times
// on the same Downloader instance should be idempotent (not fail).
// This test SHOULD FAIL with current implementation.
func TestSetupDlPathIdempotent(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	d := &Downloader{
		hash: "cafebabe",
	}

	// First call should succeed
	if err := d.setupDlPath(); err != nil {
		t.Fatalf("First setupDlPath call failed: %v", err)
	}

	// Second call should also succeed (idempotent behavior)
	// With current implementation using WarpMkdir, this WILL FAIL
	// with "file exists" error
	if err := d.setupDlPath(); err != nil {
		t.Errorf("Second setupDlPath call failed: %v", err)
		t.Logf("This test is EXPECTED TO FAIL with current implementation")
		t.Logf("setupDlPath should be idempotent but os.Mkdir fails if directory exists")
		t.Logf("Fix: Replace WarpMkdir with WarpMkdirAll")
	}

	// Third call for good measure
	if err := d.setupDlPath(); err != nil {
		t.Errorf("Third setupDlPath call failed: %v", err)
	}

	// Verify directory exists
	dlPath := filepath.Join(DlDataDir, d.hash)
	stat, err := os.Stat(dlPath)
	if err != nil {
		t.Fatalf("Directory %s does not exist: %v", dlPath, err)
	}
	if !stat.IsDir() {
		t.Errorf("Path %s is not a directory", dlPath)
	}
}

// TestSetupDlPathPermissions verifies that the directory is created
// with correct permissions (0755 or os.ModePerm).
func TestSetupDlPathPermissions(t *testing.T) {
	// Skip on Windows as permission semantics differ
	if runtime.GOOS == "windows" {
		t.Skip("Skipping permission test on Windows")
	}

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	d := &Downloader{
		hash: "deadc0de",
	}

	if err := d.setupDlPath(); err != nil {
		t.Fatalf("setupDlPath failed: %v", err)
	}

	dlPath := filepath.Join(DlDataDir, d.hash)
	stat, err := os.Stat(dlPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	// os.ModePerm is 0777, but umask typically results in 0755
	// Check that it's a directory and has reasonable permissions
	mode := stat.Mode()
	if !mode.IsDir() {
		t.Errorf("Path is not a directory")
	}

	// On Unix systems, verify permissions include owner rwx
	perm := mode.Perm()
	if perm&0700 != 0700 {
		t.Errorf("Directory permissions %o don't include owner rwx (0700)", perm)
	}
}

// TestConcurrentDownloadsSameURL is a full integration test that simulates
// the real-world scenario: multiple concurrent downloads of the same URL.
// This test SHOULD FAIL because NewDownloader calls setupDlPath internally,
// and with a fixed hash (same URL), concurrent calls will race.
func TestConcurrentDownloadsSameURL(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create a test server
	content := bytes.Repeat([]byte("test content "), 1024) // ~12KB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))

		// Small delay to increase chance of race
		time.Sleep(time.Millisecond)

		if r.Header.Get("Range") != "" {
			// Handle range request
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write(content)
	}))
	defer srv.Close()

	const numDownloads = 10
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var failCount atomic.Int32
	errors := make([]error, numDownloads)

	// Launch concurrent downloads of the same URL
	// Since URL is the same, hash will be the same (deterministic)
	// This will cause setupDlPath race condition
	url := srv.URL + "/testfile.bin"

	for i := 0; i < numDownloads; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Note: We're intentionally NOT using unique filenames
			// to trigger the hash collision scenario
			d, err := NewDownloader(&http.Client{Timeout: 5 * time.Second}, url, &DownloaderOpts{
				DownloadDirectory: base,
				MaxConnections:    2,
				MaxSegments:       2,
			})

			if err != nil {
				failCount.Add(1)
				errors[idx] = err
				return
			}

			successCount.Add(1)

			// Clean up
			if d != nil {
				d.Close()
			}
		}(i)
	}

	wg.Wait()

	// All downloads should succeed in initialization
	// With current implementation, many will fail due to mkdir race
	if fails := failCount.Load(); fails > 0 {
		t.Errorf("Expected all %d concurrent NewDownloader calls to succeed, but %d failed:",
			numDownloads, fails)
		for i, err := range errors {
			if err != nil {
				t.Errorf("  download %d: %v", i, err)
			}
		}
		t.Logf("This test is EXPECTED TO FAIL with current implementation")
		t.Logf("Concurrent downloads with same hash cause directory creation race")
		t.Logf("Success: %d, Failed: %d", successCount.Load(), fails)
	}
}

// TestSetupDlPathRaceDetector is a stress test specifically designed
// to trigger the race detector. Run with: go test -race
func TestSetupDlPathRaceDetector(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	const numGoroutines = 50
	const iterations = 10

	for iter := 0; iter < iterations; iter++ {
		testHash := strconv.Itoa(iter)
		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				d := &Downloader{
					hash: testHash,
				}

				// Intentionally ignore errors - we're testing for races
				_ = d.setupDlPath()

				// Also test directory existence check
				dlPath := filepath.Join(DlDataDir, testHash)
				_, _ = os.Stat(dlPath)
			}()
		}

		wg.Wait()
	}
}

// TestSetupDlPathWithManualPreCreation verifies behavior when directory
// is manually pre-created (e.g., by another process or previous run).
// This test SHOULD FAIL because WarpMkdir fails on existing directories.
func TestSetupDlPathWithManualPreCreation(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	d := &Downloader{
		hash: "precreated",
	}

	// Manually pre-create the directory (simulating previous partial download
	// or external process)
	dlPath := filepath.Join(DlDataDir, d.hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("Pre-creating directory failed: %v", err)
	}

	// Now try setupDlPath - should handle existing directory gracefully
	// With current implementation using WarpMkdir, this WILL FAIL
	if err := d.setupDlPath(); err != nil {
		t.Errorf("setupDlPath failed on pre-existing directory: %v", err)
		t.Logf("This test is EXPECTED TO FAIL with current implementation")
		t.Logf("setupDlPath should handle pre-existing directories (e.g., from interrupted downloads)")
		t.Logf("Fix: Replace WarpMkdir with WarpMkdirAll which is idempotent")
	}

	// Verify d.dlPath was set correctly
	if d.dlPath != dlPath {
		t.Errorf("dlPath not set correctly: got %q, want %q", d.dlPath, dlPath)
	}
}

// TestSetupDlPathNestedConcurrency tests the worst-case scenario:
// Multiple layers of concurrent operations creating nested paths.
func TestSetupDlPathNestedConcurrency(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	const numWorkers = 5
	const downloadsPerWorker = 5

	var wg sync.WaitGroup
	var totalErrors atomic.Int32

	for worker := 0; worker < numWorkers; worker++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Each worker creates multiple downloaders with same hash
			for dl := 0; dl < downloadsPerWorker; dl++ {
				d := &Downloader{
					hash: "shared" + strconv.Itoa(workerID),
				}

				if err := d.setupDlPath(); err != nil {
					totalErrors.Add(1)
				}
			}
		}(worker)
	}

	wg.Wait()

	if errs := totalErrors.Load(); errs > 0 {
		t.Errorf("Got %d errors from nested concurrent operations", errs)
		t.Logf("This test is EXPECTED TO FAIL with current implementation")
		t.Logf("Expected 0 errors, but os.Mkdir is not safe for concurrent use")
	}
}
