package warplib

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestItemBasics(t *testing.T) {
	mu := &sync.RWMutex{}
	item, err := newItem(mu, "file.bin", "http://example.com", "downloads", "hash", 100, true, &itemOpts{
		AbsoluteLocation: ".",
	})
	if err != nil {
		t.Fatalf("newItem: %v", err)
	}
	item.Downloaded = 50
	if item.GetPercentage() != 50 {
		t.Fatalf("expected 50%%, got %d", item.GetPercentage())
	}
	if item.GetSavePath() == "" || item.GetAbsolutePath() == "" {
		t.Fatalf("expected non-empty paths")
	}
	item.addPart("phash", 0, 10)
	off, part := item.getPart("phash")
	if part == nil || off != 0 {
		t.Fatalf("unexpected part lookup: %v %d", part, off)
	}
	if _, err := item.GetMaxConnections(); err == nil {
		t.Fatalf("expected error without downloader")
	}
	if _, err := item.GetMaxParts(); err == nil {
		t.Fatalf("expected error without downloader")
	}
	if err := item.Resume(); err == nil {
		t.Fatalf("expected error without downloader")
	}
	if err := item.StopDownload(); err == nil {
		t.Fatalf("expected error without downloader")
	}

	item.savePart(5, &ItemPart{Hash: "p2", FinalOffset: 9})
	if item.Parts[5] == nil {
		t.Fatalf("expected part to be saved")
	}

	_, cancel := context.WithCancel(context.Background())
	item.dAlloc = &Downloader{cancel: cancel, maxConn: 2, maxParts: 3}
	if _, err := item.GetMaxConnections(); err != nil {
		t.Fatalf("GetMaxConnections: %v", err)
	}
	if _, err := item.GetMaxParts(); err != nil {
		t.Fatalf("GetMaxParts: %v", err)
	}
	if err := item.StopDownload(); err != nil {
		t.Fatalf("StopDownload: %v", err)
	}
}

func TestItemIsDownloading(t *testing.T) {
	item := &Item{}
	if item.IsDownloading() {
		t.Fatalf("expected IsDownloading to be false")
	}
	item.dAlloc = &Downloader{}
	if !item.IsDownloading() {
		t.Fatalf("expected IsDownloading to be true")
	}
}

// TestItemDAllocConcurrentAccess tests for Race 3: Item.dAlloc TOCTOU
// This test verifies that concurrent access to dAlloc (check-then-use) is properly synchronized.
func TestItemDAllocConcurrentAccess(t *testing.T) {
	mu := &sync.RWMutex{}
	item := &Item{mu: mu, Parts: make(map[int64]*ItemPart), memPart: make(map[string]int64)}

	var wg sync.WaitGroup

	// Goroutine setting/clearing dAlloc
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			item.setDAlloc(&Downloader{ctx: ctx, cancel: cancel, wg: &sync.WaitGroup{}})
			time.Sleep(time.Microsecond)
			item.clearDAlloc()
			cancel()
		}
	}()

	// Goroutines checking dAlloc
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				_ = item.IsDownloading()
				_, _ = item.GetMaxConnections()
			}
		}()
	}
	wg.Wait()
}
