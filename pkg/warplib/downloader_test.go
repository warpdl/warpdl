package warplib

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newRangeServer(t *testing.T, content []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
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
}

func newChunkedServer(t *testing.T, content []byte, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		for i := 0; i < len(content); i += 8 {
			end := i + 8
			if end > len(content) {
				end = len(content)
			}
			_, _ = w.Write(content[i:end])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if delay > 0 {
				time.Sleep(delay)
			}
		}
	}))
}

func TestDownloaderStartRange(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("a"), 64*1024)
	srv := newRangeServer(t, content)
	defer srv.Close()

	d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    2,
		MaxSegments:       2,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content mismatch")
	}
}

func TestDownloaderUnknownSize(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := []byte("hello-world")
	srv := newChunkedServer(t, content, 0)
	defer srv.Close()

	d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, err := os.ReadFile(d.GetSavePath())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("downloaded content mismatch")
	}
}

func TestDownloaderStop(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	content := bytes.Repeat([]byte("b"), 1024)
	srv := newChunkedServer(t, content, time.Millisecond*5)
	defer srv.Close()

	stopped := make(chan struct{}, 1)
	var d *Downloader
	handlers := &Handlers{
		DownloadProgressHandler: func(hash string, nread int) {
			if d != nil {
				d.Stop()
			}
		},
		DownloadStoppedHandler: func() {
			stopped <- struct{}{}
		},
	}
	d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		Handlers:          handlers,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatalf("expected DownloadStoppedHandler to fire")
	}
}
