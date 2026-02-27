package warplib

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// TestFlushOneConcurrentDownload tests Race 6: TOCTOU in FlushOne
// This verifies that FlushOne uses a write lock for the entire operation
// to prevent a race where a download becomes active between the check and delete.
func TestFlushOneConcurrentDownload(t *testing.T) {
	iterations := 50
	if testing.Short() {
		iterations = 10
	}

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	item := &Item{
		Hash:       "test-hash",
		TotalSize:  1000,
		Downloaded: 500,
		mu:         m.mu,
		Parts:      make(map[int64]*ItemPart),
		memPart:    make(map[string]int64),
	}
	m.mu.Lock()
	m.items[item.Hash] = item
	m.mu.Unlock()

	// Create the download directory
	dlPath := GetPath(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = m.FlushOne("test-hash")
		}()
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			item.setDAlloc(&httpProtocolDownloader{inner: &Downloader{ctx: ctx, cancel: cancel}, probed: true})
			time.Sleep(time.Microsecond)
			item.clearDAlloc()
			cancel()
		}()
	}
	wg.Wait()
	// No panic and no data corruption = success
}

// TestFlushOneNotFound tests that FlushOne returns proper error for missing hash
func TestFlushOneNotFound(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	err = m.FlushOne("nonexistent-hash")
	if err != ErrFlushHashNotFound {
		t.Fatalf("expected ErrFlushHashNotFound, got %v", err)
	}
}

// TestFlushOneActiveDownload tests that FlushOne rejects active downloads
func TestFlushOneActiveDownload(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	item := &Item{
		Hash:       "active-hash",
		TotalSize:  1000,
		Downloaded: 500,
		mu:         m.mu,
		Parts:      make(map[int64]*ItemPart),
		memPart:    make(map[string]int64),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	item.setDAlloc(&httpProtocolDownloader{inner: &Downloader{ctx: ctx, cancel: cancel}, probed: true})

	m.mu.Lock()
	m.items[item.Hash] = item
	m.mu.Unlock()

	err = m.FlushOne("active-hash")
	if err != ErrFlushItemDownloading {
		t.Fatalf("expected ErrFlushItemDownloading, got %v", err)
	}
}

// TestFlushOneCompletedDownload tests that FlushOne succeeds for completed downloads
func TestFlushOneCompletedDownload(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	item := &Item{
		Hash:       "completed-hash",
		TotalSize:  1000,
		Downloaded: 1000, // Completed
		mu:         m.mu,
		Parts:      make(map[int64]*ItemPart),
		memPart:    make(map[string]int64),
	}

	m.mu.Lock()
	m.items[item.Hash] = item
	m.mu.Unlock()

	// Create the download directory
	dlPath := GetPath(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = m.FlushOne("completed-hash")
	if err != nil {
		t.Fatalf("FlushOne should succeed for completed download: %v", err)
	}

	// Verify item was removed
	if m.GetItem("completed-hash") != nil {
		t.Fatal("item should be removed after flush")
	}
}

// TestFlushOneConcurrentMultipleItems tests FlushOne with multiple items being flushed concurrently
func TestFlushOneConcurrentMultipleItems(t *testing.T) {
	numItems := 10
	if testing.Short() {
		numItems = 5
	}

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	// Create multiple completed items
	for i := 0; i < numItems; i++ {
		hash := testHash(i)
		item := &Item{
			Hash:       hash,
			TotalSize:  1000,
			Downloaded: 1000, // Completed
			mu:         m.mu,
			Parts:      make(map[int64]*ItemPart),
			memPart:    make(map[string]int64),
		}
		m.mu.Lock()
		m.items[item.Hash] = item
		m.mu.Unlock()

		// Create the download directory
		dlPath := GetPath(DlDataDir, item.Hash)
		if err := os.MkdirAll(dlPath, 0755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	// Flush all items concurrently
	var wg sync.WaitGroup
	for i := 0; i < numItems; i++ {
		wg.Add(1)
		hash := testHash(i)
		go func(h string) {
			defer wg.Done()
			_ = m.FlushOne(h)
		}(hash)
	}
	wg.Wait()

	// All items should be flushed
	if len(m.GetItems()) > 0 {
		t.Fatalf("expected all items to be flushed, got %d remaining", len(m.GetItems()))
	}
}

// Helper function to generate test hashes
func testHash(i int) string {
	hashes := []string{
		"hash-0", "hash-1", "hash-2", "hash-3", "hash-4",
		"hash-5", "hash-6", "hash-7", "hash-8", "hash-9",
	}
	if i < len(hashes) {
		return hashes[i]
	}
	return "hash-unknown"
}
