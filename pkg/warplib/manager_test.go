package warplib

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	return m
}

func newTestDownloader() *Downloader {
	d := &Downloader{
		fileName:      "file.bin",
		url:           "http://example.com/file.bin",
		dlLoc:         ".",
		hash:          "hash1",
		contentLength: 100,
		resumable:     true,
		maxConn:       2,
		maxParts:      2,
		headers:       Headers{{Key: "X-Test", Value: "one"}},
		handlers:      &Handlers{},
		wg:            &sync.WaitGroup{},
	}
	d.handlers.setDefault(log.New(io.Discard, "", 0))
	return d
}

func TestManagerAddAndGet(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	d.dlLoc = t.TempDir()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := m.GetItem(d.hash)
	if item == nil {
		t.Fatalf("expected item")
	}
	if item.Name != d.fileName || item.Url != d.url {
		t.Fatalf("unexpected item fields: %+v", item)
	}
	d.handlers.SpawnPartHandler("p1", 0, 10)
	if len(item.Parts) != 1 {
		t.Fatalf("expected parts to be tracked")
	}
	d.handlers.RespawnPartHandler("p1", 0, 0, 5)
	d.handlers.DownloadProgressHandler("p1", 2)
	d.handlers.CompileCompleteHandler("p1", 5)
	d.handlers.DownloadCompleteHandler(MAIN_HASH, 10)
	if item.Downloaded != item.TotalSize {
		t.Fatalf("expected item to be completed")
	}
}

func TestManagerLists(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := m.GetItem(d.hash)
	item.Downloaded = item.TotalSize
	m.UpdateItem(item)

	if len(m.GetCompletedItems()) != 1 {
		t.Fatalf("expected completed items")
	}
	if len(m.GetIncompleteItems()) != 0 {
		t.Fatalf("expected no incomplete items")
	}
}

func TestManagerFlushOne(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	if err := m.FlushOne("missing"); err != ErrFlushHashNotFound {
		t.Fatalf("expected ErrFlushHashNotFound, got %v", err)
	}
	d := newTestDownloader()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := m.GetItem(d.hash)
	if err := m.FlushOne(item.Hash); err != ErrFlushItemDownloading {
		t.Fatalf("expected ErrFlushItemDownloading, got %v", err)
	}
	item.Downloaded = item.TotalSize
	m.UpdateItem(item)
	if err := m.FlushOne(item.Hash); err != nil {
		t.Fatalf("FlushOne: %v", err)
	}
}

func TestManagerFlush(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}
	item := m.GetItem(d.hash)
	item.Downloaded = item.TotalSize
	m.UpdateItem(item)
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if m.GetItem(item.Hash) != nil {
		t.Fatalf("expected item to be flushed")
	}
}

func TestManagerResumeMissing(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	if _, err := m.ResumeDownload(nil, "missing", nil); err != ErrDownloadNotFound {
		t.Fatalf("expected ErrDownloadNotFound, got %v", err)
	}
}

func TestManagerGetPublicItems(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	item1 := &Item{
		Hash:       "h1",
		Name:       "a",
		Url:        "u",
		TotalSize:  10,
		Downloaded: 10,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m.mu,
		memPart:    make(map[string]int64),
		Children:   false,
	}
	item2 := &Item{
		Hash:       "h2",
		Name:       "b",
		Url:        "u",
		TotalSize:  10,
		Downloaded: 0,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m.mu,
		memPart:    make(map[string]int64),
		Children:   true,
	}
	m.UpdateItem(item1)
	m.UpdateItem(item2)
	pub := m.GetPublicItems()
	if len(pub) != 1 || pub[0].Hash != "h1" {
		t.Fatalf("expected only public items, got %d", len(pub))
	}
}

func TestManagerPopulateMemPart(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	item := &Item{
		Hash:      "h1",
		Name:      "a",
		Url:       "u",
		Parts:     map[int64]*ItemPart{0: {Hash: "p1", FinalOffset: 10}},
		Resumable: true,
		mu:        m.mu,
	}
	m.items[item.Hash] = item
	m.populateMemPart()
	if item.memPart == nil || item.memPart["p1"] != 0 {
		t.Fatalf("expected memPart to be populated")
	}
}

func TestManagerResumeSuccess(t *testing.T) {
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
		Hash:             "h1",
		Name:             "file.bin",
		Url:              "http://example.com/file.bin",
		TotalSize:        10,
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
	}
	m.UpdateItem(item)
	if err := os.MkdirAll(filepath.Join(DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	resumedItem, err := m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{})
	if err != nil {
		t.Fatalf("ResumeDownload: %v", err)
	}
	if resumedItem.dAlloc != nil {
		defer resumedItem.dAlloc.Close()
	}
}

func TestManagerResumeDownload_MissingData(t *testing.T) {
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
		Hash:             "h2",
		Name:             "file.bin",
		Url:              "http://example.com/file.bin",
		TotalSize:        100,
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		mu:               m.mu,
		memPart:          make(map[string]int64),
	}
	m.UpdateItem(item)

	// Do NOT create the dldata directory - should fail validation
	_, err = m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{})
	if err == nil {
		t.Fatal("expected error for missing dldata directory")
	}
	if err.Error() == "" || !containsString(err.Error(), "download data") {
		t.Fatalf("expected ErrDownloadDataMissing context, got %v", err)
	}
}

func TestManagerResumeDownload_MissingPartFile(t *testing.T) {
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
		Hash:             "h3",
		Name:             "file.bin",
		Url:              "http://example.com/file.bin",
		TotalSize:        100,
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		mu:               m.mu,
		memPart:          make(map[string]int64),
	}
	// Add a part that doesn't have a corresponding file
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 50,
		Compiled:    false,
	}
	m.UpdateItem(item)

	// Create the dldata directory but not the part file
	if err := os.MkdirAll(filepath.Join(DlDataDir, item.Hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, err = m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{})
	if err == nil {
		t.Fatal("expected error for missing part file")
	}
	if !containsString(err.Error(), "part file missing") {
		t.Fatalf("expected part file missing error, got %v", err)
	}
}

func TestManagerResumeDownload_NotResumable(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	item := &Item{
		Hash:      "h-notresumable",
		Name:      "file.bin",
		Url:       "http://example.com/file.bin",
		TotalSize: 10,
		Resumable: false,
		Parts:     make(map[int64]*ItemPart),
	}
	m.UpdateItem(item)

	if _, err := m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{}); err != ErrDownloadNotResumable {
		t.Fatalf("expected ErrDownloadNotResumable, got %v", err)
	}
}

func TestManagerCompileCompleteMissingPart(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	d := newTestDownloader()
	called := false
	d.handlers.ErrorHandler = func(string, error) { called = true }

	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: d.dlLoc}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}

	d.handlers.CompileCompleteHandler("missing", 1)
	if !called {
		t.Fatalf("expected error handler to be called for missing part")
	}
}

func TestInitManagerInvalidUserDataPath(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	oldPath := __USERDATA_FILE_NAME
	__USERDATA_FILE_NAME = filepath.Join(base, "missing", "userdata.warp")
	defer func() { __USERDATA_FILE_NAME = oldPath }()

	if _, err := InitManager(); err == nil {
		t.Fatalf("expected error for invalid userdata path")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestManagerResumeEarlyCompile tests that when a part is already complete
// during resume (poff >= foff), the Compiled state is properly updated
func TestManagerResumeEarlyCompile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	hash := "h-early-compile"
	partHash := "p-early-compile"

	// Create download directory
	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create a part file that's already complete
	dlPath := filepath.Join(DlDataDir, hash)
	partPath := getFileName(dlPath, partHash)
	testData := []byte("complete data")
	if err := os.WriteFile(partPath, testData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create an item with a part that's not marked as compiled yet
	item := &Item{
		Hash:             hash,
		Name:             "file.bin",
		Url:              "http://example.com/file.bin",
		TotalSize:        ContentLength(len(testData)),
		Downloaded:       0,
		DownloadLocation: base,
		AbsoluteLocation: base,
		Resumable:        true,
		Parts: map[int64]*ItemPart{
			0: {
				Hash:        partHash,
				FinalOffset: int64(len(testData)),
				Compiled:    false, // Not compiled yet
			},
		},
	}
	m.UpdateItem(item)

	// Create downloader and add it to manager
	d, err := initDownloader(&http.Client{}, hash, item.Url, ContentLength(len(testData)), &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          item.Name,
	})
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}

	// Track compile complete calls
	var compileCompleteCalled bool
	originalHandler := d.handlers.CompileCompleteHandler
	d.handlers.CompileCompleteHandler = func(h string, read int64) {
		compileCompleteCalled = true
		originalHandler(h, read)
	}

	// Add download to manager (this wraps handlers)
	if err := m.AddDownload(d, &AddDownloadOpts{AbsoluteLocation: base}); err != nil {
		t.Fatalf("AddDownload: %v", err)
	}

	// Resume the download - this should trigger early compile path
	d.ohmap.Make()
	if err := d.openFile(); err != nil {
		t.Fatalf("openFile: %v", err)
	}
	defer d.Close()

	d.wg.Add(1)
	d.resumePartDownload(partHash, 0, int64(len(testData)), MB)
	d.wg.Wait()

	// Verify CompileCompleteHandler was called
	if !compileCompleteCalled {
		t.Fatalf("expected CompileCompleteHandler to be called")
	}

	// Verify the Compiled state was updated in the item
	updatedItem := m.GetItem(hash)
	if updatedItem == nil {
		t.Fatalf("item not found after resume")
	}

	part := updatedItem.Parts[0]
	if part == nil {
		t.Fatalf("part not found in item")
	}

	if !part.Compiled {
		t.Fatalf("expected part.Compiled to be true after early compile, got false")
	}
}

// TDD Tests for GOB persistence fixes (these will FAIL initially as per TDD methodology)

func TestInitManager_CorruptGOBFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	// Write garbage bytes to userdata file
	userdataPath := filepath.Join(base, "userdata.warp")
	if err := os.WriteFile(userdataPath, []byte{0x00, 0xFF, 0xAB, 0xCD, 0xDE, 0xAD}, 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	// Should succeed despite corrupt data
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager should not fail on corrupt GOB: %v", err)
	}
	defer m.Close()
	// Should have empty items (fresh start)
	if len(m.GetItems()) != 0 {
		t.Fatalf("expected empty items after corrupt GOB recovery, got %d", len(m.GetItems()))
	}
}

func TestInitManager_EmptyFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	// Create empty file
	userdataPath := filepath.Join(base, "userdata.warp")
	f, err := os.Create(userdataPath)
	if err != nil {
		t.Fatalf("create empty file: %v", err)
	}
	f.Close()
	// Should succeed with empty file
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager should handle empty file: %v", err)
	}
	defer m.Close()
	if len(m.GetItems()) != 0 {
		t.Fatalf("expected empty items for empty file")
	}
}

func TestManager_TruncateBeforeEncode(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	// Phase 1: Create large state with many items
	for i := 0; i < 20; i++ {
		item := &Item{
			Hash:       fmt.Sprintf("hash-%d", i),
			Name:       strings.Repeat("x", 100),
			Url:        strings.Repeat("y", 200),
			TotalSize:  ContentLength(1000),
			Downloaded: ContentLength(1000), // Complete
			Resumable:  true,
			Parts:      make(map[int64]*ItemPart),
			mu:         m.mu,
			memPart:    make(map[string]int64),
		}
		m.UpdateItem(item)
	}
	m.Close()

	// Record large file size
	userdataPath := filepath.Join(base, "userdata.warp")
	largeInfo, _ := os.Stat(userdataPath)
	largeSize := largeInfo.Size()

	// Phase 2: Reopen and flush all (removes completed items)
	m, err = InitManager()
	if err != nil {
		t.Fatalf("InitManager after large: %v", err)
	}
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	m.Close()

	// File should be smaller
	smallInfo, _ := os.Stat(userdataPath)
	smallSize := smallInfo.Size()
	if smallSize >= largeSize {
		t.Fatalf("file not truncated: large=%d, small=%d", largeSize, smallSize)
	}

	// Phase 3: Reopen and verify no garbage
	m, err = InitManager()
	if err != nil {
		t.Fatalf("InitManager after flush: %v", err)
	}
	defer m.Close()
	if len(m.GetItems()) != 0 {
		t.Fatalf("expected 0 items after flush, got %d (garbage data)", len(m.GetItems()))
	}
}

func TestManager_DataIntegrityPersistence(t *testing.T) {
	m := newTestManager(t)

	// Create item with specific known values
	item := &Item{
		Hash:             "integrity-test",
		Name:             "test-file.bin",
		Url:              "http://example.com/test.bin",
		TotalSize:        ContentLength(12345),
		Downloaded:       ContentLength(6789),
		DownloadLocation: "/tmp/downloads",
		AbsoluteLocation: "/tmp/abs",
		Resumable:        true,
		Hidden:           true,
		Children:         false,
		Parts: map[int64]*ItemPart{
			0:    {Hash: "p1", FinalOffset: 1000, Compiled: true},
			1000: {Hash: "p2", FinalOffset: 2000, Compiled: false},
		},
		Headers: Headers{{Key: "X-Test", Value: "val1"}},
		mu:      m.mu,
		memPart: make(map[string]int64),
	}
	m.UpdateItem(item)
	m.Close()

	// Reopen and verify
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}
	defer m2.Close()

	loaded := m2.GetItem("integrity-test")
	if loaded == nil {
		t.Fatal("item not persisted")
	}
	if loaded.Name != "test-file.bin" {
		t.Errorf("Name: got %s, want test-file.bin", loaded.Name)
	}
	if loaded.TotalSize != 12345 {
		t.Errorf("TotalSize: got %d, want 12345", loaded.TotalSize)
	}
	if loaded.Downloaded != 6789 {
		t.Errorf("Downloaded: got %d, want 6789", loaded.Downloaded)
	}
	if len(loaded.Parts) != 2 {
		t.Errorf("Parts: got %d, want 2", len(loaded.Parts))
	}
	if !loaded.Resumable || !loaded.Hidden || loaded.Children {
		t.Errorf("flags mismatch")
	}
}

func TestManager_FlushProperlyTruncates(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}

	// Add many completed items to create large file
	for i := 0; i < 10; i++ {
		item := &Item{
			Hash:       fmt.Sprintf("complete-%d", i),
			Name:       strings.Repeat("x", 100),
			Url:        strings.Repeat("y", 200),
			TotalSize:  ContentLength(100),
			Downloaded: ContentLength(100), // Complete - will be flushed
			Resumable:  true,
			Parts:      make(map[int64]*ItemPart),
			mu:         m.mu,
			memPart:    make(map[string]int64),
		}
		m.UpdateItem(item)
	}
	m.Close()

	// Record file size before flush
	userdataPath := filepath.Join(base, "userdata.warp")
	beforeInfo, _ := os.Stat(userdataPath)
	beforeSize := beforeInfo.Size()

	// Reopen and flush
	m, err = InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	if err := m.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	m.Close()

	// File should be smaller after flush (all items removed)
	afterInfo, _ := os.Stat(userdataPath)
	afterSize := afterInfo.Size()
	if afterSize >= beforeSize {
		t.Errorf("file not truncated after flush: before=%d, after=%d", beforeSize, afterSize)
	}

	// Verify we can reload without corruption
	m, err = InitManager()
	if err != nil {
		t.Fatalf("InitManager after flush: %v", err)
	}
	defer m.Close()

	// All items should be flushed (complete items with no dAlloc)
	items := m.GetItems()
	if len(items) != 0 {
		t.Errorf("expected 0 items after flush, got %d", len(items))
	}
}

func TestManager_ConcurrentUpdateItem(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	var wg sync.WaitGroup
	numGoroutines := 50
	itemsPerGoroutine := 5

	// Concurrent writers
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < itemsPerGoroutine; i++ {
				item := &Item{
					Hash:       fmt.Sprintf("g%d-i%d", gid, i),
					Name:       fmt.Sprintf("file-%d-%d.bin", gid, i),
					Url:        "http://example.com/file.bin",
					TotalSize:  ContentLength(1000),
					Downloaded: ContentLength(gid*10 + i),
					Resumable:  true,
					Parts:      make(map[int64]*ItemPart),
					mu:         m.mu,
					memPart:    make(map[string]int64),
				}
				m.UpdateItem(item)
			}
		}(g)
	}

	wg.Wait()

	// All items should be present
	items := m.GetItems()
	expectedCount := numGoroutines * itemsPerGoroutine
	if len(items) != expectedCount {
		t.Errorf("expected %d items, got %d", expectedCount, len(items))
	}
}
