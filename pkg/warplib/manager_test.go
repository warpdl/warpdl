package warplib

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
		fileName:       "file.bin",
		url:            "http://example.com/file.bin",
		dlLoc:          ".",
		hash:           "hash1",
		contentLength:  100,
		resumable:      true,
		maxConn:        2,
		maxParts:       2,
		headers:        Headers{{Key: "X-Test", Value: "one"}},
		handlers:       &Handlers{},
		wg:             &sync.WaitGroup{},
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
	m.Flush()
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
		Hash:        "h1",
		Name:        "a",
		Url:         "u",
		TotalSize:   10,
		Downloaded:  10,
		Resumable:   true,
		Parts:       make(map[int64]*ItemPart),
		mu:          m.mu,
		memPart:     make(map[string]int64),
		Children:    false,
	}
	item2 := &Item{
		Hash:        "h2",
		Name:        "b",
		Url:         "u",
		TotalSize:   10,
		Downloaded:  0,
		Resumable:   true,
		Parts:       make(map[int64]*ItemPart),
		mu:          m.mu,
		memPart:     make(map[string]int64),
		Children:    true,
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
		Hash:    "h1",
		Name:    "a",
		Url:     "u",
		Parts:   map[int64]*ItemPart{0: {Hash: "p1", FinalOffset: 10}},
		Resumable: true,
		mu:      m.mu,
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
	if _, err := m.ResumeDownload(&http.Client{}, item.Hash, &ResumeDownloadOpts{}); err != nil {
		t.Fatalf("ResumeDownload: %v", err)
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
