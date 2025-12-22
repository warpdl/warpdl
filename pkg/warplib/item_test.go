package warplib

import (
	"context"
	"sync"
	"testing"
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
