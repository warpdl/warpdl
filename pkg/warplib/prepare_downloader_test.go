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
			if d.numBaseParts != tt.expectedParts {
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
