package warplib

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
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
	item.dAlloc = &httpProtocolDownloader{inner: &Downloader{cancel: cancel, maxConn: 2, maxParts: 3}, probed: true}
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
	item.dAlloc = &httpProtocolDownloader{inner: &Downloader{}}
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
			item.setDAlloc(&httpProtocolDownloader{inner: &Downloader{ctx: ctx, cancel: cancel, wg: &sync.WaitGroup{}}, probed: true})
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

func TestGetPartDesync(t *testing.T) {
	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      &sync.RWMutex{},
	}
	// Simulate desync: memPart has entry, Parts doesn't
	item.memPart["orphan_hash"] = 999

	_, part, err := item.getPartWithError("orphan_hash")
	if err == nil || part != nil {
		t.Fatal("expected error for desync")
	}
	if !errors.Is(err, ErrPartDesync) {
		t.Fatalf("expected ErrPartDesync, got %v", err)
	}
}

func TestGetPartWithError_NotFound(t *testing.T) {
	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      &sync.RWMutex{},
	}

	offset, part, err := item.getPartWithError("nonexistent")
	if err != nil || part != nil || offset != 0 {
		t.Fatal("expected nil values for nonexistent hash")
	}
}

func TestGetPartWithError_Found(t *testing.T) {
	item := &Item{
		Parts:   make(map[int64]*ItemPart),
		memPart: make(map[string]int64),
		mu:      &sync.RWMutex{},
	}
	item.Parts[100] = &ItemPart{Hash: "test_hash", FinalOffset: 200}
	item.memPart["test_hash"] = 100

	offset, part, err := item.getPartWithError("test_hash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if offset != 100 || part == nil || part.Hash != "test_hash" {
		t.Fatal("expected valid part")
	}
}

// TestItemSchedulingFieldsGOBRoundTrip verifies that the new scheduling and
// cookie fields survive a GOB encode/decode round-trip with all 5 ScheduleState
// values, and that pre-existing GOB data without these fields decodes safely
// (zero-value backward compatibility).
func TestItemSchedulingFieldsGOBRoundTrip(t *testing.T) {
	states := []ScheduleState{
		ScheduleStateNone,
		ScheduleStateScheduled,
		ScheduleStateTriggered,
		ScheduleStateMissed,
		ScheduleStateCancelled,
	}

	for _, state := range states {
		t.Run(string(state)+"_or_empty", func(t *testing.T) {
			original := ItemsMap{
				"hash1": {
					Hash:             "hash1",
					Name:             "file.bin",
					Url:              "http://example.com/file.bin",
					Headers:          nil,
					Parts:            make(map[int64]*ItemPart),
					ScheduledAt:      time.Date(2026, 3, 1, 2, 0, 0, 0, time.UTC),
					CronExpr:         "0 2 * * *",
					ScheduleState:    state,
					CookieSourcePath: "/home/user/.mozilla/firefox/profile/cookies.sqlite",
				},
			}

			var buf bytes.Buffer
			if err := gob.NewEncoder(&buf).Encode(original); err != nil {
				t.Fatalf("encode: %v", err)
			}

			var decoded ItemsMap
			if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}

			item := decoded["hash1"]
			if item == nil {
				t.Fatal("decoded item is nil")
			}
			if item.ScheduledAt != original["hash1"].ScheduledAt {
				t.Errorf("ScheduledAt: got %v, want %v", item.ScheduledAt, original["hash1"].ScheduledAt)
			}
			if item.CronExpr != "0 2 * * *" {
				t.Errorf("CronExpr: got %q, want %q", item.CronExpr, "0 2 * * *")
			}
			if item.ScheduleState != state {
				t.Errorf("ScheduleState: got %q, want %q", item.ScheduleState, state)
			}
			if item.CookieSourcePath != "/home/user/.mozilla/firefox/profile/cookies.sqlite" {
				t.Errorf("CookieSourcePath: got %q", item.CookieSourcePath)
			}
		})
	}
}

// TestItemSchedulingFieldsGOBBackwardCompat verifies that pre-existing GOB data
// (without scheduling/cookie fields) decodes to safe zero values.
func TestItemSchedulingFieldsGOBBackwardCompat(t *testing.T) {
	// Encode an item WITHOUT the new fields (simulate pre-existing GOB data)
	// by encoding a map with a struct that only has legacy fields.
	type legacyItem struct {
		Hash  string
		Name  string
		Url   string
		Parts map[int64]*ItemPart
	}
	type legacyItemsMap map[string]*legacyItem

	legacy := legacyItemsMap{
		"legacyhash": {
			Hash:  "legacyhash",
			Name:  "old.bin",
			Url:   "http://example.com/old.bin",
			Parts: make(map[int64]*ItemPart),
		},
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(legacy); err != nil {
		t.Fatalf("encode legacy: %v", err)
	}

	var decoded ItemsMap
	if err := gob.NewDecoder(&buf).Decode(&decoded); err != nil {
		t.Fatalf("decode into ItemsMap: %v", err)
	}

	item := decoded["legacyhash"]
	if item == nil {
		t.Fatal("decoded item is nil")
	}
	// New fields must be zero values
	if !item.ScheduledAt.IsZero() {
		t.Errorf("ScheduledAt should be zero, got %v", item.ScheduledAt)
	}
	if item.CronExpr != "" {
		t.Errorf("CronExpr should be empty, got %q", item.CronExpr)
	}
	if item.ScheduleState != ScheduleStateNone {
		t.Errorf("ScheduleState should be empty, got %q", item.ScheduleState)
	}
	if item.CookieSourcePath != "" {
		t.Errorf("CookieSourcePath should be empty, got %q", item.CookieSourcePath)
	}
}
