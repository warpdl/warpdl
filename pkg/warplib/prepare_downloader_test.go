package warplib

import (
	"bytes"
	"io"
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
			h.Set("ETag", `"speed-test"`)
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       reader,
				Header:     h,
			}, nil
		}),
	}
	d := &Downloader{
		client:       client,
		url:          "http://example.com/file.bin",
		chunk:        8,
		force:        false,
		numBaseParts: 0,
		headers:      Headers{},
		resourceETag: `"speed-test"`,
	}
	d.contentLength.Store(1000)
	if err := d.prepareDownloader(); err != nil {
		t.Fatalf("prepareDownloader: %v", err)
	}
	if d.numBaseParts != 4 {
		t.Fatalf("expected numBaseParts=4 for very slow download, got %d", d.numBaseParts)
	}
}

func TestPrepareDownloaderSpeedAllocation(t *testing.T) {
	const size = 64 * KB
	tests := []struct {
		name    string
		elapsed time.Duration
		parts   int32
	}{
		{"below 100KB/s", getDownloadTime(100*KB, size) + 1, 4},
		{"at 100KB/s", getDownloadTime(100*KB, size), 6},
		{"at 1MB/s", getDownloadTime(MB, size), 8},
		{"at 5MB/s", getDownloadTime(5*MB, size), 8},
		{"above 5MB/s", getDownloadTime(5*MB, size) - 1, 10},
		{"at 10MB/s", getDownloadTime(10*MB, size), 10},
		{"above 10MB/s", getDownloadTime(10*MB, size) - 1, 12},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := partsForProbe(tt.elapsed, size); got != tt.parts {
				t.Fatalf("partsForProbe(%s, %d) = %d, want %d", tt.elapsed, size, got, tt.parts)
			}
		})
	}
	// The same elapsed time must select fewer parts for half as many bytes.
	if got := partsForProbe(getDownloadTime(10*MB, size)-1, size/2); got != 10 {
		t.Fatalf("half-size probe selected %d parts, want 10", got)
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

// TestPrepareDownloaderNoAcceptRanges tests when server doesn't support range requests.
func TestPrepareDownloaderNoAcceptRanges(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			h := make(http.Header)
			// No Accept-Ranges header
			h.Set("Content-Length", "1048576")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 1024))),
				Header:     h,
			}, nil
		}),
	}
	d := &Downloader{
		client:       client,
		url:          "http://example.com/file.bin",
		chunk:        1024,
		force:        false, // Not forcing range requests
		numBaseParts: 0,
		headers:      Headers{},
		resourceETag: `"no-ranges-test"`,
	}
	d.contentLength.Store(1048576)
	if err := d.prepareDownloader(); err != nil {
		t.Fatalf("prepareDownloader: %v", err)
	}
	if d.numBaseParts != 1 {
		t.Errorf("expected numBaseParts=1 without Accept-Ranges, got %d", d.numBaseParts)
	}
	if d.resumable {
		t.Error("expected resumable=false without Accept-Ranges")
	}
}

// TestPrepareDownloaderSmallContent tests when content is smaller than chunk size.
func TestPrepareDownloaderSmallContent(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("Accept-Ranges", "bytes")
			h.Set("Content-Length", "100") // Very small file
			h.Set("ETag", `"small-test"`)
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 100))),
				Header:     h,
			}, nil
		}),
	}
	d := &Downloader{
		client:       client,
		url:          "http://example.com/file.bin",
		chunk:        1024, // Larger than content
		force:        false,
		numBaseParts: 0,
		headers:      Headers{},
		resourceETag: `"small-test"`,
	}
	d.contentLength.Store(100) // Content smaller than chunk
	if err := d.prepareDownloader(); err != nil {
		t.Fatalf("prepareDownloader: %v", err)
	}
	if d.numBaseParts != 1 {
		t.Errorf("expected numBaseParts=1 for small content, got %d", d.numBaseParts)
	}
}

// TestPrepareDownloaderNumBasePartsPreset tests when numBaseParts is already set.
func TestPrepareDownloaderNumBasePartsPreset(t *testing.T) {
	client := &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("Accept-Ranges", "bytes")
			h.Set("Content-Length", "1048576")
			h.Set("ETag", `"preset-test"`)
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 1024))),
				Header:     h,
			}, nil
		}),
	}
	d := &Downloader{
		client:       client,
		url:          "http://example.com/file.bin",
		chunk:        1024,
		force:        false,
		numBaseParts: 7, // Already set
		headers:      Headers{},
		resourceETag: `"preset-test"`,
	}
	d.contentLength.Store(1048576)
	if err := d.prepareDownloader(); err != nil {
		t.Fatalf("prepareDownloader: %v", err)
	}
	// Should remain unchanged
	if d.numBaseParts != 7 {
		t.Errorf("expected numBaseParts=7 (preset), got %d", d.numBaseParts)
	}
}
