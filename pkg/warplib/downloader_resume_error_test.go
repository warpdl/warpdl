package warplib

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestResumePartDownloadMissingPartFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	hash := "h-missing"
	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	handlers := &Handlers{}
	handlers.setDefault(log.New(io.Discard, "", 0))
	called := false
	handlers.ErrorHandler = func(string, error) { called = true }

	d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", 5, &DownloaderOpts{
		DownloadDirectory: base,
		FileName:          "file.bin",
		Handlers:          handlers,
	})
	if err != nil {
		t.Fatalf("initDownloader: %v", err)
	}
	defer d.Close()
	if err := d.openFile(); err != nil {
		t.Fatalf("openFile: %v", err)
	}
	defer d.f.Close()

	d.wg.Add(1)
	d.resumePartDownload("missing", 0, 4, MB)
	d.wg.Wait()
	if !called {
		t.Fatalf("expected error handler to be called")
	}
}
