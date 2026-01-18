//go:build e2e

package warplib

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	expectedFileSize = 100 * 1024 * 1024 // 100MB
	downloadTimeout  = 3 * time.Minute
)

func testE2EDownloadFromURL(t *testing.T, url string) {
	t.Helper()

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	client := &http.Client{Timeout: downloadTimeout}

	d, err := NewDownloader(client, url, &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    4,
		MaxSegments:       4,
	})
	if err != nil {
		if isNetworkError(err) {
			t.Skipf("Network unavailable: %v", err)
		}
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		if isNetworkError(err) {
			t.Skipf("Download failed (network): %v", err)
		}
		t.Fatalf("Start: %v", err)
	}

	info, err := os.Stat(d.GetSavePath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Size() != expectedFileSize {
		t.Fatalf("Size mismatch: want %d, got %d", expectedFileSize, info.Size())
	}
}

func isNetworkError(err error) bool {
	keywords := []string{
		"connection refused",
		"no such host",
		"timeout",
		"deadline exceeded",
		"connection reset",
		"network is unreachable",
		"i/o timeout",
		"dial tcp",
		"tls handshake timeout",
	}
	errLower := strings.ToLower(err.Error())
	for _, kw := range keywords {
		if strings.Contains(errLower, kw) {
			return true
		}
	}
	return false
}

func TestE2EDownload_ASH(t *testing.T) {
	testE2EDownloadFromURL(t, "https://ash-speed.hetzner.com/100MB.bin")
}

func TestE2EDownload_FSN1(t *testing.T) {
	testE2EDownloadFromURL(t, "https://fsn1-speed.hetzner.com/100MB.bin")
}

func TestE2EDownload_HEL1(t *testing.T) {
	testE2EDownloadFromURL(t, "https://hel1-speed.hetzner.com/100MB.bin")
}

func TestE2EDownload_HIL(t *testing.T) {
	testE2EDownloadFromURL(t, "https://hil-speed.hetzner.com/100MB.bin")
}
