package warplib

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNewDownloaderNoAcceptRanges(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("a"), 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer srv.Close()

	d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	defer d.Close()
	if d.resumable {
		t.Fatalf("expected resumable to be false without Accept-Ranges")
	}
	if d.numBaseParts != 1 {
		t.Fatalf("expected numBaseParts=1, got %d", d.numBaseParts)
	}
}

func TestNewDownloaderMissingFileName(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	if _, err := NewDownloader(&http.Client{}, srv.URL+"/", &DownloaderOpts{
		DownloadDirectory: base,
	}); err == nil {
		t.Fatalf("expected error for missing file name")
	}
}
