package warplib

import (
	"bytes"
	"net/http"
	"testing"
	"time"
)

func TestPrepareDownloaderSlowSpeed(t *testing.T) {
	reader := &slowReadCloser{
		data:  bytes.Repeat([]byte("a"), 8),
		delay: time.Millisecond,
	}
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("Accept-Ranges", "bytes")
			h.Set("Content-Length", "8")
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       reader,
				Header:     h,
			}, nil
		}),
	}
	d := &Downloader{
		client:        client,
		url:           "http://example.com/file.bin",
		chunk:         8,
		force:         false,
		numBaseParts:  0,
		contentLength: 1000,
		headers:       Headers{},
	}
	if err := d.prepareDownloader(); err != nil {
		t.Fatalf("prepareDownloader: %v", err)
	}
	if d.numBaseParts != 14 {
		t.Fatalf("expected numBaseParts=14, got %d", d.numBaseParts)
	}
}

func TestNewDownloaderSkipSetup(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	srv := newRangeServer(t, bytes.Repeat([]byte("a"), 1024))
	defer srv.Close()

	d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		SkipSetup:         true,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}
	if d.dlPath != "" {
		t.Fatalf("expected dlPath to be empty when SkipSetup is true")
	}
	if d.hash == "" {
		return
	}
	t.Fatalf("expected hash to be empty when SkipSetup is true")
}
