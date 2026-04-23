package warplib

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: when a queued download's Start() fails (e.g. target file
// already exists on disk), the user had no per-download log entry to
// explain why. Before this fix logs.txt contained only the init lines
// (GET:, CONTENT-LENGTH:, FILE-NAME:) and the caller had to chase the
// daemon console to find the error. Now Start() records the failure
// via d.Log, so `cat dldata/<hash>/logs.txt` always tells the story.
func TestStartLogsOpenFileFailureToLogsTxt(t *testing.T) {
	body := []byte("some file content that would normally download")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "46")
		w.Header().Set("Content-Disposition", `attachment; filename="already.bin"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	// Redirect DlDataDir so this test's segment/log directories live in
	// a temp path that t.TempDir will clean up.
	orig := DlDataDir
	DlDataDir = t.TempDir()
	defer func() { DlDataDir = orig }()

	downloadDir := t.TempDir()
	// Pre-create the destination so openFile returns ErrFileExists.
	collidePath := filepath.Join(downloadDir, "already.bin")
	if err := os.WriteFile(collidePath, []byte("existing"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: downloadDir,
		// LockFileName=true forces strict-collision behaviour (user
		// explicitly chose this name; do NOT silently auto-rename).
		// Without this flag the downloader would land on
		// "already (1).bin" and Start() would succeed — a separate
		// regression covered by uniquify_test.go.
		FileName:     "already.bin",
		LockFileName: true,
		Overwrite:    false,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	startErr := d.Start()
	if startErr == nil {
		t.Fatal("expected Start to fail when destination exists and Overwrite=false")
	}
	if !strings.Contains(startErr.Error(), "already exists") {
		t.Fatalf("unexpected Start error: %v", startErr)
	}

	// Critical assertion: logs.txt must contain the failure.
	logsPath := filepath.Join(DlDataDir, d.GetHash(), "logs.txt")
	data, err := os.ReadFile(logsPath)
	if err != nil {
		t.Fatalf("read logs.txt: %v", err)
	}
	if !strings.Contains(string(data), "Start failed") {
		t.Errorf("logs.txt missing Start failed entry:\n%s", data)
	}
	if !strings.Contains(string(data), "already exists") {
		t.Errorf("logs.txt missing underlying error:\n%s", data)
	}
}

// A successful download must NOT leave a "Start failed" entry in
// logs.txt — regression guard against accidentally logging errors on
// the happy path.
func TestStartLogsNoFailureOnSuccess(t *testing.T) {
	body := []byte("hello world")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Range"), "bytes=") {
			// Serve the range — warpdl will issue ranged requests for
			// the segmented download. Keep it simple: return the whole body.
			w.Header().Set("Content-Range", "bytes 0-10/11")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body)
			return
		}
		w.Header().Set("Content-Length", "11")
		w.Header().Set("Content-Disposition", `attachment; filename="ok.txt"`)
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	orig := DlDataDir
	DlDataDir = t.TempDir()
	defer func() { DlDataDir = orig }()

	d, err := NewDownloader(srv.Client(), srv.URL, &DownloaderOpts{
		DownloadDirectory: t.TempDir(),
		MaxConnections:    1,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(DlDataDir, d.GetHash(), "logs.txt"))
	if strings.Contains(string(data), "Start failed") {
		t.Errorf("happy-path logs.txt contains Start failed:\n%s", data)
	}
}
