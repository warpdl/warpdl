package warplib

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestQueue_E2E verifies end-to-end queue behavior:
// 1. Manager with maxConcurrent=2 correctly queues downloads
// 2. When one completes, the next automatically starts
// 3. All downloads eventually complete
func TestQueue_E2E(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	// Track which downloads have been started via onStart callback
	var mu sync.Mutex
	startedHashes := make(map[string]bool)
	startOrder := make([]string, 0)

	// Configure queue with maxConcurrent=2
	m.SetMaxConcurrentDownloads(2, func(hash string) {
		mu.Lock()
		defer mu.Unlock()
		startedHashes[hash] = true
		startOrder = append(startOrder, hash)
	})

	queue := m.GetQueue()
	if queue == nil {
		t.Fatal("expected queue to be initialized")
	}

	// Create test server serving small content
	content := bytes.Repeat([]byte("x"), 1024)
	srv := newE2ETestServer(t, content)
	defer srv.Close()

	// Create 4 test downloaders
	hashes := []string{"dl-1", "dl-2", "dl-3", "dl-4"}
	for _, hash := range hashes {
		d, err := NewDownloader(&http.Client{}, srv.URL+"/"+hash+".bin", &DownloaderOpts{
			DownloadDirectory: base,
			MaxConnections:    1,
			MaxSegments:       1,
		})
		if err != nil {
			t.Fatalf("NewDownloader for %s: %v", hash, err)
		}
		// Override hash for test control
		d.hash = hash

		if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: base}); err != nil {
			t.Fatalf("AddDownload for %s: %v", hash, err)
		}
	}

	// Verify initial queue state: 2 active, 2 waiting
	if got := queue.ActiveCount(); got != 2 {
		t.Fatalf("expected 2 active downloads, got %d", got)
	}
	if got := queue.WaitingCount(); got != 2 {
		t.Fatalf("expected 2 waiting downloads, got %d", got)
	}

	// Verify onStart was called exactly twice (for the first 2 downloads)
	mu.Lock()
	initialStarts := len(startedHashes)
	mu.Unlock()
	if initialStarts != 2 {
		t.Fatalf("expected onStart called 2 times initially, got %d", initialStarts)
	}

	// Simulate completion of first download - should trigger next waiting download
	queue.OnComplete("dl-1")

	// Verify state after first completion: still 2 active (dl-2, dl-3), 1 waiting (dl-4)
	if got := queue.ActiveCount(); got != 2 {
		t.Fatalf("after first completion: expected 2 active, got %d", got)
	}
	if got := queue.WaitingCount(); got != 1 {
		t.Fatalf("after first completion: expected 1 waiting, got %d", got)
	}

	// Verify onStart was called for dl-3
	mu.Lock()
	if !startedHashes["dl-3"] {
		t.Fatal("expected dl-3 to be started after dl-1 completed")
	}
	afterFirstStarts := len(startedHashes)
	mu.Unlock()
	if afterFirstStarts != 3 {
		t.Fatalf("expected 3 total starts after first completion, got %d", afterFirstStarts)
	}

	// Simulate completion of second download - should trigger last waiting download
	queue.OnComplete("dl-2")

	// Verify state after second completion: still 2 active (dl-3, dl-4), 0 waiting
	if got := queue.ActiveCount(); got != 2 {
		t.Fatalf("after second completion: expected 2 active, got %d", got)
	}
	if got := queue.WaitingCount(); got != 0 {
		t.Fatalf("after second completion: expected 0 waiting, got %d", got)
	}

	// Verify all 4 downloads have been started
	mu.Lock()
	finalStarts := len(startedHashes)
	startOrderCopy := make([]string, len(startOrder))
	copy(startOrderCopy, startOrder)
	mu.Unlock()
	if finalStarts != 4 {
		t.Fatalf("expected all 4 downloads started, got %d", finalStarts)
	}

	// Verify start order: dl-1, dl-2 started immediately, then dl-3, dl-4 as slots freed
	expectedOrder := []string{"dl-1", "dl-2", "dl-3", "dl-4"}
	if len(startOrderCopy) != len(expectedOrder) {
		t.Fatalf("start order length mismatch: got %v, want %v", startOrderCopy, expectedOrder)
	}
	for i, want := range expectedOrder {
		if startOrderCopy[i] != want {
			t.Fatalf("start order[%d]: got %s, want %s", i, startOrderCopy[i], want)
		}
	}

	// Complete remaining downloads
	queue.OnComplete("dl-3")
	queue.OnComplete("dl-4")

	// Final state: all complete, queue empty
	if got := queue.ActiveCount(); got != 0 {
		t.Fatalf("final state: expected 0 active, got %d", got)
	}
	if got := queue.WaitingCount(); got != 0 {
		t.Fatalf("final state: expected 0 waiting, got %d", got)
	}
}

// TestQueue_E2E_Priority verifies priority-based ordering in E2E scenario.
func TestQueue_E2E_Priority(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	var mu sync.Mutex
	startOrder := make([]string, 0)

	// maxConcurrent=1 means only first download starts, rest queue
	m.SetMaxConcurrentDownloads(1, func(hash string) {
		mu.Lock()
		defer mu.Unlock()
		startOrder = append(startOrder, hash)
	})

	queue := m.GetQueue()
	content := bytes.Repeat([]byte("y"), 512)
	srv := newE2ETestServer(t, content)
	defer srv.Close()

	// First download starts immediately (blocks slot)
	d1, _ := NewDownloader(&http.Client{}, srv.URL+"/first.bin", &DownloaderOpts{
		DownloadDirectory: base, MaxConnections: 1, MaxSegments: 1,
	})
	d1.hash = "first"
	_ = m.AddDownload(d1, &AddDownloadOpts{AbsoluteLocation: base})

	// Add low priority (goes to waiting queue)
	queue.Add("low", PriorityLow)
	// Add normal priority (goes before low)
	queue.Add("normal", PriorityNormal)
	// Add high priority (goes before normal and low)
	queue.Add("high", PriorityHigh)

	// Verify initial state: 1 active (first), 3 waiting
	if got := queue.ActiveCount(); got != 1 {
		t.Fatalf("expected 1 active, got %d", got)
	}
	if got := queue.WaitingCount(); got != 3 {
		t.Fatalf("expected 3 waiting, got %d", got)
	}

	// Complete "first" - should start "high" (highest priority waiting)
	queue.OnComplete("first")
	mu.Lock()
	if len(startOrder) != 2 || startOrder[1] != "high" {
		t.Fatalf("expected 'high' to start second, got order %v", startOrder)
	}
	mu.Unlock()

	// Complete "high" - should start "normal"
	queue.OnComplete("high")
	mu.Lock()
	if len(startOrder) != 3 || startOrder[2] != "normal" {
		t.Fatalf("expected 'normal' to start third, got order %v", startOrder)
	}
	mu.Unlock()

	// Complete "normal" - should start "low"
	queue.OnComplete("normal")
	mu.Lock()
	if len(startOrder) != 4 || startOrder[3] != "low" {
		t.Fatalf("expected 'low' to start fourth, got order %v", startOrder)
	}
	mu.Unlock()

	// Verify final order: first (immediate), high, normal, low
	mu.Lock()
	defer mu.Unlock()
	expected := []string{"first", "high", "normal", "low"}
	if len(startOrder) != len(expected) {
		t.Fatalf("start order length mismatch: got %v, want %v", startOrder, expected)
	}
	for i, want := range expected {
		if startOrder[i] != want {
			t.Fatalf("priority order[%d]: got %s, want %s", i, startOrder[i], want)
		}
	}
}

// TestQueue_E2E_ConcurrentSlots tests that queue correctly limits concurrent slots.
func TestQueue_E2E_ConcurrentSlots(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	var activeCount atomic.Int32
	var maxConcurrent atomic.Int32
	var mu sync.Mutex
	completedHashes := make([]string, 0)

	// maxConcurrent=2: only 2 downloads can be active at once
	m.SetMaxConcurrentDownloads(2, func(hash string) {
		current := activeCount.Add(1)
		// Track max concurrent seen
		for {
			max := maxConcurrent.Load()
			if current <= max || maxConcurrent.CompareAndSwap(max, current) {
				break
			}
		}
	})

	queue := m.GetQueue()
	content := bytes.Repeat([]byte("z"), 512)
	srv := newE2ETestServer(t, content)
	defer srv.Close()

	// Add 6 downloads - should result in 2 active, 4 waiting
	for i := 0; i < 6; i++ {
		hash := "slot-" + strconv.Itoa(i)
		d, _ := NewDownloader(&http.Client{}, srv.URL+"/"+hash+".bin", &DownloaderOpts{
			DownloadDirectory: base, MaxConnections: 1, MaxSegments: 1,
		})
		d.hash = hash
		_ = m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: base})
	}

	// Verify initial state: 2 active, 4 waiting
	if got := queue.ActiveCount(); got != 2 {
		t.Fatalf("expected 2 active, got %d", got)
	}
	if got := queue.WaitingCount(); got != 4 {
		t.Fatalf("expected 4 waiting, got %d", got)
	}

	// Simulate completions one by one, verify queue maintains correct state
	for i := 0; i < 6; i++ {
		hash := "slot-" + strconv.Itoa(i)
		activeCount.Add(-1) // Simulate completion
		queue.OnComplete(hash)

		mu.Lock()
		completedHashes = append(completedHashes, hash)
		mu.Unlock()

		// After each completion, verify state
		expectedActive := min(2, 6-len(completedHashes))
		expectedWaiting := max(0, 6-len(completedHashes)-2)

		if got := queue.ActiveCount(); got != expectedActive {
			t.Fatalf("after completing %s: expected %d active, got %d", hash, expectedActive, got)
		}
		if got := queue.WaitingCount(); got != expectedWaiting {
			t.Fatalf("after completing %s: expected %d waiting, got %d", hash, expectedWaiting, got)
		}
	}

	// Verify max concurrent never exceeded 2
	if got := maxConcurrent.Load(); got > 2 {
		t.Fatalf("max concurrent exceeded limit: got %d, want <= 2", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// TestQueue_E2E_ManagerIntegration verifies Manager's DownloadCompleteHandler integration.
func TestQueue_E2E_ManagerIntegration(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	var mu sync.Mutex
	startedHashes := make([]string, 0)

	// maxConcurrent=1: verifies that DownloadCompleteHandler properly calls queue.OnComplete
	m.SetMaxConcurrentDownloads(1, func(hash string) {
		mu.Lock()
		defer mu.Unlock()
		startedHashes = append(startedHashes, hash)
	})

	queue := m.GetQueue()
	content := bytes.Repeat([]byte("a"), 512)
	srv := newE2ETestServer(t, content)
	defer srv.Close()

	// Create first download
	d1, err := NewDownloader(&http.Client{}, srv.URL+"/d1.bin", &DownloaderOpts{
		DownloadDirectory: base, MaxConnections: 1, MaxSegments: 1,
	})
	if err != nil {
		t.Fatalf("NewDownloader d1: %v", err)
	}
	d1.hash = "d1"
	if err := m.AddDownload(d1, &AddDownloadOpts{AbsoluteLocation: base}); err != nil {
		t.Fatalf("AddDownload d1: %v", err)
	}

	// Create second download (will be queued)
	d2, err := NewDownloader(&http.Client{}, srv.URL+"/d2.bin", &DownloaderOpts{
		DownloadDirectory: base, MaxConnections: 1, MaxSegments: 1,
	})
	if err != nil {
		t.Fatalf("NewDownloader d2: %v", err)
	}
	d2.hash = "d2"
	if err := m.AddDownload(d2, &AddDownloadOpts{AbsoluteLocation: base}); err != nil {
		t.Fatalf("AddDownload d2: %v", err)
	}

	// Verify: d1 active, d2 waiting
	if got := queue.ActiveCount(); got != 1 {
		t.Fatalf("expected 1 active, got %d", got)
	}
	if got := queue.WaitingCount(); got != 1 {
		t.Fatalf("expected 1 waiting, got %d", got)
	}

	mu.Lock()
	if len(startedHashes) != 1 || startedHashes[0] != "d1" {
		t.Fatalf("expected d1 to be started, got %v", startedHashes)
	}
	mu.Unlock()

	// Simulate download completion via handler (simulates real download complete)
	// The patched DownloadCompleteHandler in manager.go should call queue.OnComplete
	item := m.GetItem("d1")
	if item == nil {
		t.Fatal("d1 item not found")
	}

	// Trigger the patched DownloadCompleteHandler
	d1.handlers.DownloadCompleteHandler(MAIN_HASH, 512)

	// After completion, d2 should be auto-started
	if got := queue.ActiveCount(); got != 1 {
		t.Fatalf("after completion: expected 1 active (d2), got %d", got)
	}
	if got := queue.WaitingCount(); got != 0 {
		t.Fatalf("after completion: expected 0 waiting, got %d", got)
	}

	mu.Lock()
	if len(startedHashes) != 2 || startedHashes[1] != "d2" {
		t.Fatalf("expected d2 to be auto-started, got %v", startedHashes)
	}
	mu.Unlock()
}

// newE2ETestServer creates a test HTTP server for E2E queue tests.
func newE2ETestServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		rangeHeader := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(content) - 1
		if parts[1] != "" {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}
		if start > end || start < 0 || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
}

// TestQueue_Persistence verifies queue state survives Manager restart.
// Waiting items should be restored when Manager is reopened and queue is re-enabled.
func TestQueue_Persistence(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Phase 1: Create Manager with queue and add items
	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager (phase 1): %v", err)
	}

	var startedHashes1 []string
	m1.SetMaxConcurrentDownloads(2, func(hash string) {
		startedHashes1 = append(startedHashes1, hash)
	})

	queue1 := m1.GetQueue()
	if queue1 == nil {
		t.Fatal("queue1 should be initialized")
	}

	// Add items directly to queue (simulating downloads added)
	queue1.Add("active-1", PriorityNormal)       // becomes active (slot 1)
	queue1.Add("active-2", PriorityNormal)       // becomes active (slot 2)
	queue1.Add("waiting-low", PriorityLow)       // waiting (low priority)
	queue1.Add("waiting-high", PriorityHigh)     // waiting (high priority, should be first)
	queue1.Add("waiting-normal", PriorityNormal) // waiting (normal priority)

	// Verify initial state: 2 active, 3 waiting
	if got := queue1.ActiveCount(); got != 2 {
		t.Fatalf("phase 1: expected 2 active, got %d", got)
	}
	if got := queue1.WaitingCount(); got != 3 {
		t.Fatalf("phase 1: expected 3 waiting, got %d", got)
	}

	// Force persist by encoding
	if err := m1.encode(); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Close Manager
	if err := m1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Phase 2: Reopen Manager and verify queue state restored
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager (phase 2): %v", err)
	}
	defer m2.Close()

	// Queue is not set yet - queueState should be loaded
	if m2.queueState == nil {
		t.Fatal("phase 2: queueState should be loaded from file")
	}

	// Verify loaded queue state
	if m2.queueState.MaxConcurrent != 2 {
		t.Fatalf("phase 2: expected MaxConcurrent=2, got %d", m2.queueState.MaxConcurrent)
	}
	if len(m2.queueState.Waiting) != 3 {
		t.Fatalf("phase 2: expected 3 waiting in state, got %d", len(m2.queueState.Waiting))
	}

	// Verify priority order preserved: high, normal, low
	expectedOrder := []struct {
		hash     string
		priority Priority
	}{
		{"waiting-high", PriorityHigh},
		{"waiting-normal", PriorityNormal},
		{"waiting-low", PriorityLow},
	}
	for i, want := range expectedOrder {
		got := m2.queueState.Waiting[i]
		if got.Hash != want.hash || got.Priority != want.priority {
			t.Fatalf("phase 2: waiting[%d] got {%s, %d}, want {%s, %d}",
				i, got.Hash, got.Priority, want.hash, want.priority)
		}
	}

	// Enable queue - should restore state
	var startedHashes2 []string
	m2.SetMaxConcurrentDownloads(2, func(hash string) {
		startedHashes2 = append(startedHashes2, hash)
	})

	queue2 := m2.GetQueue()
	if queue2 == nil {
		t.Fatal("queue2 should be initialized")
	}

	// Verify restored queue state
	// Active should be 0 (active items not persisted, need to be re-queued)
	if got := queue2.ActiveCount(); got != 0 {
		t.Fatalf("phase 2: expected 0 active (restored), got %d", got)
	}
	// Waiting should be restored
	if got := queue2.WaitingCount(); got != 3 {
		t.Fatalf("phase 2: expected 3 waiting (restored), got %d", got)
	}

	// Verify priority order works after restore
	// Simulate completion to trigger auto-start
	queue2.mu.Lock()
	queue2.active["dummy"] = struct{}{}
	queue2.mu.Unlock()
	queue2.OnComplete("dummy")

	// High priority should start first
	if len(startedHashes2) != 1 || startedHashes2[0] != "waiting-high" {
		t.Fatalf("phase 2: expected waiting-high to start first, got %v", startedHashes2)
	}
}

// TestQueue_Persistence_BackwardCompat verifies old format (no queue) can still be loaded.
func TestQueue_Persistence_BackwardCompat(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Phase 1: Create Manager WITHOUT queue (old format)
	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager (phase 1): %v", err)
	}

	// Add an item without enabling queue
	item := &Item{
		Hash:       "test-item",
		Name:       "test.bin",
		Url:        "http://example.com/test.bin",
		TotalSize:  1000,
		Downloaded: 500,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m1.mu,
		memPart:    make(map[string]int64),
	}
	m1.UpdateItem(item)

	if err := m1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Phase 2: Reopen with queue enabled
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager (phase 2): %v", err)
	}
	defer m2.Close()

	// Item should be preserved
	loadedItem := m2.GetItem("test-item")
	if loadedItem == nil {
		t.Fatal("item not found after reload")
	}
	if loadedItem.Downloaded != 500 {
		t.Fatalf("expected Downloaded=500, got %d", loadedItem.Downloaded)
	}

	// Queue state should be nil (not saved in old format)
	// But enabling queue should work fine
	m2.SetMaxConcurrentDownloads(2, nil)
	queue := m2.GetQueue()
	if queue == nil {
		t.Fatal("queue should be enabled")
	}
	if queue.WaitingCount() != 0 {
		t.Fatal("new queue should have no waiting items")
	}
}
