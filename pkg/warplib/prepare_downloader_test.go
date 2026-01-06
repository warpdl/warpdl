package warplib

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"
)

// assertPartsInRange checks if numBaseParts is within expected ± tolerance.
// Used for timing-sensitive tests where CI variance may cause slight variations.
func assertPartsInRange(t *testing.T, got, expected int32, tolerance int32, name, desc string) {
	t.Helper()
	if got < expected-tolerance || got > expected+tolerance {
		t.Errorf("%s: %s - expected numBaseParts=%d (±%d), got %d",
			name, desc, expected, tolerance, got)
	}
}

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
	if d.numBaseParts != 4 {
		t.Fatalf("expected numBaseParts=4 for very slow download, got %d", d.numBaseParts)
	}
}

func TestPrepareDownloaderSpeedAllocation(t *testing.T) {
	tests := []struct {
		name          string
		chunkSize     int
		readDelay     time.Duration
		expectedParts int32
		description   string
	}{
		{
			name:          "Very Slow Speed < 100KB/s",
			chunkSize:     32 * 1024, // 32KB
			readDelay:     400 * time.Millisecond,
			expectedParts: 4,
			description:   "Very slow downloads should use fewer parts to avoid server overload",
		},
		{
			name:          "Slow Speed < 1MB/s",
			chunkSize:     32 * 1024,
			readDelay:     40 * time.Millisecond,
			expectedParts: 6,
			description:   "Slow downloads should use moderate parts to maintain stability",
		},
		{
			name:          "Moderate Speed 1-5MB/s",
			chunkSize:     32 * 1024,
			readDelay:     16 * time.Millisecond, // ~2MB/s
			expectedParts: 8,
			description:   "Moderate speed downloads should use balanced part count",
		},
		{
			name:          "Fast Speed > 5MB/s",
			chunkSize:     32 * 1024,
			readDelay:     4 * time.Millisecond, // ~8MB/s, with margin for CI timing jitter
			expectedParts: 10,
			description:   "Fast downloads should use 10 parts for good performance",
		},
		{
			name:          "Super Fast Speed > 10MB/s",
			chunkSize:     64 * 1024,            // 64KB - doubled for CI timing margin
			readDelay:     1 * time.Millisecond, // ~64MB/s with 64KB chunks
			expectedParts: 12,
			description:   "Super fast downloads should use more parts to maximize throughput",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &slowReadCloser{
				data:  bytes.Repeat([]byte("a"), tt.chunkSize),
				delay: tt.readDelay,
			}
			client := &http.Client{
				Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
					h := make(http.Header)
					h.Set("Accept-Ranges", "bytes")
					h.Set("Content-Length", "1048576") // 1MB
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
				chunk:         tt.chunkSize,
				force:         false,
				numBaseParts:  0,
				contentLength: 1048576, // 1MB
				headers:       Headers{},
			}
			if err := d.prepareDownloader(); err != nil {
				t.Fatalf("prepareDownloader: %v", err)
			}
			// Use tolerance for timing-sensitive test cases
			// CI runners (especially macOS) have variable timing
			if tt.name == "Super Fast Speed > 10MB/s" || tt.name == "Fast Speed > 5MB/s" {
				assertPartsInRange(t, d.numBaseParts, tt.expectedParts, 2, tt.name, tt.description)
			} else if d.numBaseParts != tt.expectedParts {
				t.Errorf("%s: %s - expected numBaseParts=%d, got %d",
					tt.name, tt.description, tt.expectedParts, d.numBaseParts)
			}
		})
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
		client:        client,
		url:           "http://example.com/file.bin",
		chunk:         1024,
		force:         false, // Not forcing range requests
		numBaseParts:  0,
		contentLength: 1048576,
		headers:       Headers{},
	}
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
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 100))),
				Header:     h,
			}, nil
		}),
	}
	d := &Downloader{
		client:        client,
		url:           "http://example.com/file.bin",
		chunk:         1024, // Larger than content
		force:         false,
		numBaseParts:  0,
		contentLength: 100, // Content smaller than chunk
		headers:       Headers{},
	}
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
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 1024))),
				Header:     h,
			}, nil
		}),
	}
	d := &Downloader{
		client:        client,
		url:           "http://example.com/file.bin",
		chunk:         1024,
		force:         false,
		numBaseParts:  7, // Already set
		contentLength: 1048576,
		headers:       Headers{},
	}
	if err := d.prepareDownloader(); err != nil {
		t.Fatalf("prepareDownloader: %v", err)
	}
	// Should remain unchanged
	if d.numBaseParts != 7 {
		t.Errorf("expected numBaseParts=7 (preset), got %d", d.numBaseParts)
	}
}
