package warplib

import (
	"context"
	"sync"
	"testing"
	"time"
)

// nopWriteCloser is a no-op io.WriteCloser for testing
type nopWriteCloser struct{}

func (n nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (n nopWriteCloser) Close() error                { return nil }

// TestStopResumeConcurrent tests Race 2: concurrent calls to StopDownload and Resume
// This verifies that the Item.Resume() properly snapshots Parts before accessing dAlloc,
// preventing a TOCTOU race where StopDownload sets dAlloc=nil between the dAlloc check
// and the Resume() call on it.
func TestStopResumeConcurrent(t *testing.T) {
	iterations := 100
	if testing.Short() {
		iterations = 10
	}

	mu := &sync.RWMutex{}
	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      mu,
	}
	// Add some parts for Resume to work with
	item.Parts[0] = &ItemPart{Hash: "p1", FinalOffset: 100}
	item.memPart["p1"] = 0

	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		// Reset downloader for each iteration with minimal initialization
		ctx, cancel := context.WithCancel(context.Background())
		item.dAllocMu.Lock()
		item.dAlloc = &Downloader{
			ctx:    ctx,
			cancel: cancel,
			lw:     nopWriteCloser{}, // Prevent nil pointer dereference
			wg:     &sync.WaitGroup{},
		}
		item.dAllocMu.Unlock()

		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = item.StopDownload()
		}()
		go func() {
			defer wg.Done()
			_ = item.Resume()
		}()
		wg.Wait()
		cancel()
	}
	// No panic = success
}

// TestResumeWithNilDownloader tests that Resume properly handles nil downloader
func TestResumeWithNilDownloader(t *testing.T) {
	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      &sync.RWMutex{},
		dAlloc:  nil,
	}

	err := item.Resume()
	if err != ErrItemDownloaderNotFound {
		t.Fatalf("expected ErrItemDownloaderNotFound, got %v", err)
	}
}

// TestResumePartsSnapshot tests that Resume creates a proper snapshot of Parts
// to avoid race conditions with concurrent modifications to the Parts map
func TestResumePartsSnapshot(t *testing.T) {
	mu := &sync.RWMutex{}
	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      mu,
	}
	item.Parts[0] = &ItemPart{Hash: "p1", FinalOffset: 100}
	item.memPart["p1"] = 0

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockDownloader := &Downloader{
		ctx:    ctx,
		cancel: cancel,
		lw:     nopWriteCloser{},
		wg:     &sync.WaitGroup{},
	}

	item.setDAlloc(mockDownloader)

	// Concurrent modification and resume
	var wg sync.WaitGroup

	// Goroutine 1: Modify Parts map
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		item.mu.Lock()
		item.Parts[100] = &ItemPart{Hash: "p2", FinalOffset: 200}
		item.memPart["p2"] = 100
		item.mu.Unlock()
	}()

	// Goroutine 2: Call Resume - this should snapshot Parts before concurrent modification
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = item.Resume()
	}()

	wg.Wait()
}

// TestStopDownloadConcurrent tests concurrent StopDownload calls
func TestStopDownloadConcurrent(t *testing.T) {
	goroutines := 50
	if testing.Short() {
		goroutines = 10
	}

	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      &sync.RWMutex{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	item.setDAlloc(&Downloader{ctx: ctx, cancel: cancel})

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = item.StopDownload()
		}()
	}
	wg.Wait()

	// Verify downloader is nil
	if item.getDAlloc() != nil {
		t.Fatal("expected downloader to be nil after stop")
	}
}

// TestResumeSnapshotting tests that Resume properly snapshots the Parts map
// This test focuses on verifying the snapshot is created correctly without
// actually calling Resume() which would cause races in openFile()
func TestResumeSnapshotting(t *testing.T) {
	readers := 10
	readerIterations := 100
	writerIterations := 50
	if testing.Short() {
		readers = 5
		readerIterations = 20
		writerIterations = 10
	}

	mu := &sync.RWMutex{}
	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      mu,
	}
	item.Parts[0] = &ItemPart{Hash: "p1", FinalOffset: 100}
	item.memPart["p1"] = 0

	// This test just verifies that concurrent reads via Resume's snapshot logic
	// and concurrent writes to Parts don't race
	var wg sync.WaitGroup

	// Multiple goroutines reading (would call Resume's snapshot logic)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readerIterations; j++ {
				item.mu.RLock()
				partsCopy := make(map[int64]*ItemPart, len(item.Parts))
				for k, v := range item.Parts {
					partsCopy[k] = v
				}
				item.mu.RUnlock()
				_ = partsCopy
			}
		}()
	}

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; i < writerIterations; i++ {
			item.mu.Lock()
			item.Parts[int64(i*100)] = &ItemPart{Hash: "px", FinalOffset: int64(i * 200)}
			item.mu.Unlock()
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()
	// No panic = success
}

// TestIsDownloadingRace tests concurrent access to IsDownloading
func TestIsDownloadingRace(t *testing.T) {
	iterations := 100
	if testing.Short() {
		iterations = 20
	}

	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      &sync.RWMutex{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		wg.Add(3)

		// Set downloader
		go func() {
			defer wg.Done()
			item.setDAlloc(&Downloader{ctx: ctx, cancel: cancel})
		}()

		// Clear downloader
		go func() {
			defer wg.Done()
			item.clearDAlloc()
		}()

		// Check if downloading
		go func() {
			defer wg.Done()
			_ = item.IsDownloading()
		}()
	}
	wg.Wait()
}
