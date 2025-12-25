package warplib

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestInitDownloaderAndGetters(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	hash := "h1"
	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", 10, &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
		MaxConnections:    2,
		MaxSegments:       3,
	})
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()
	if d.GetMaxConnections() != 2 {
		t.Fatalf("unexpected max connections: %d", d.GetMaxConnections())
	}
	if d.GetMaxParts() != 3 {
		t.Fatalf("unexpected max parts: %d", d.GetMaxParts())
	}
	if d.GetFileName() != "file.bin" {
		t.Fatalf("unexpected file name: %s", d.GetFileName())
	}
	if d.GetDownloadDirectory() == "" {
		t.Fatalf("expected download directory")
	}
	if d.GetContentLength() != 10 {
		t.Fatalf("unexpected content length: %d", d.GetContentLength())
	}
	if d.GetContentLengthAsInt() != 10 {
		t.Fatalf("unexpected content length int: %d", d.GetContentLengthAsInt())
	}
	if d.GetContentLengthAsString() == "" {
		t.Fatalf("expected content length string")
	}
	if d.GetHash() != hash {
		t.Fatalf("unexpected hash: %s", d.GetHash())
	}
	if d.NumConnections() != 0 {
		t.Fatalf("expected zero connections")
	}
}

func TestInitDownloaderMissingDir(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	if _, err := initDownloader(&http.Client{}, "missing", "http://example.com", 10, &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
	}); err == nil {
		t.Fatalf("expected error for missing download dir")
	}
}

func TestDownloaderResumeCompiledPart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	hash := "h2"
	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", 5, &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
	})
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	parts := map[int64]*ItemPart{
		0: {Hash: "p1", FinalOffset: 4, Compiled: true},
	}
	if err := d.Resume(parts); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if err := d.Resume(map[int64]*ItemPart{}); err == nil {
		t.Fatalf("expected error for empty parts")
	}
	if err := d.Resume(nil); err == nil {
		t.Fatalf("expected error for nil parts")
	}
}

func TestResumePartDownloadCompilePath(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	hash := "h3"
	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Track handler calls
	var compileStartCalled, compileCompleteCalled bool
	var compileCompleteRead int64

	d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", 4, &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
		Handlers: &Handlers{
			CompileStartHandler: func(hash string) {
				compileStartCalled = true
			},
			CompileCompleteHandler: func(hash string, read int64) {
				compileCompleteCalled = true
				compileCompleteRead = read
			},
		},
	})
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()
	d.ohmap.Make()
	if err := d.openFile(); err != nil {
		t.Fatalf("openFile: %v", err)
	}
	defer d.f.Close()

	partHash := "p1"
	partPath := getFileName(d.dlPath, partHash)
	testData := []byte("data")
	if err := os.WriteFile(partPath, testData, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	d.wg.Add(1)
	d.resumePartDownload(partHash, 0, 2, MB)
	d.wg.Wait()

	info, err := d.f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected compiled data to be written")
	}

	// Verify handlers were called
	if !compileStartCalled {
		t.Fatalf("expected CompileStartHandler to be called")
	}
	if !compileCompleteCalled {
		t.Fatalf("expected CompileCompleteHandler to be called")
	}
	if compileCompleteRead != int64(len(testData)) {
		t.Fatalf("expected CompileCompleteHandler read=%d, got %d", len(testData), compileCompleteRead)
	}
}
