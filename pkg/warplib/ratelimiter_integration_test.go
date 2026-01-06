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

// newDelayedRangeServer creates an HTTP server that supports range requests
// and has configurable delay per chunk for testing rate limiting.
func newDelayedRangeServer(t *testing.T, content []byte, chunkDelay time.Duration) *httptest.Server {
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

        // Write in small chunks with optional delay
        const chunkSize = 1024
        for i := 0; i < len(chunk); i += chunkSize {
            end := i + chunkSize
            if end > len(chunk) {
                end = len(chunk)
            }
            _, _ = w.Write(chunk[i:end])
            if f, ok := w.(http.Flusher); ok {
                f.Flush()
            }
            if chunkDelay > 0 {
                time.Sleep(chunkDelay)
            }
        }
    }))
}

func TestDownloaderWithSpeedLimit(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping timing test in short mode")
    }

    base := t.TempDir()
    if err := SetConfigDir(base); err != nil {
        t.Fatalf("SetConfigDir: %v", err)
    }

    // 10KB file at 10KB/s should take ~1 second
    dataSize := 10 * int(KB)
    content := bytes.Repeat([]byte("x"), dataSize)
    srv := newDelayedRangeServer(t, content, 0)
    defer srv.Close()

    start := time.Now()
    d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
        DownloadDirectory: base,
        MaxConnections:    1, // single connection for predictable timing
        MaxSegments:       1,
        SpeedLimit:        10 * KB, // 10KB/s
    })
    if err != nil {
        t.Fatalf("NewDownloader: %v", err)
    }
    if err := d.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    elapsed := time.Since(start)

    // Verify file content
    got, err := os.ReadFile(d.GetSavePath())
    if err != nil {
        t.Fatalf("ReadFile: %v", err)
    }
    if !bytes.Equal(got, content) {
        t.Fatalf("downloaded content mismatch")
    }

    // Should take approximately 1 second (allow 30% tolerance)
    expectedDuration := time.Second
    minDuration := time.Duration(float64(expectedDuration) * 0.7)
    maxDuration := time.Duration(float64(expectedDuration) * 1.5)

    if elapsed < minDuration || elapsed > maxDuration {
        t.Errorf("expected duration ~%v, got %v (tolerance: %v-%v)",
            expectedDuration, elapsed, minDuration, maxDuration)
    }
}

func TestDownloaderWithSpeedLimit_MultiPart(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping timing test in short mode")
    }

    base := t.TempDir()
    if err := SetConfigDir(base); err != nil {
        t.Fatalf("SetConfigDir: %v", err)
    }

    // 20KB file at 20KB/s total with 2 parts should take ~1 second
    // Each part gets 10KB/s (20KB/s / 2 parts)
    dataSize := 20 * int(KB)
    content := bytes.Repeat([]byte("y"), dataSize)
    srv := newDelayedRangeServer(t, content, 0)
    defer srv.Close()

    start := time.Now()
    d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
        DownloadDirectory: base,
        MaxConnections:    2,
        MaxSegments:       2,
        SpeedLimit:        20 * KB, // 20KB/s total, 10KB/s per part
    })
    if err != nil {
        t.Fatalf("NewDownloader: %v", err)
    }
    if err := d.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    elapsed := time.Since(start)

    // Verify file content
    got, err := os.ReadFile(d.GetSavePath())
    if err != nil {
        t.Fatalf("ReadFile: %v", err)
    }
    if !bytes.Equal(got, content) {
        t.Fatalf("downloaded content mismatch")
    }

    // Should take approximately 1 second (allow 40% tolerance for multi-part)
    expectedDuration := time.Second
    minDuration := time.Duration(float64(expectedDuration) * 0.6)
    maxDuration := time.Duration(float64(expectedDuration) * 1.6)

    if elapsed < minDuration || elapsed > maxDuration {
        t.Errorf("expected duration ~%v, got %v (tolerance: %v-%v)",
            expectedDuration, elapsed, minDuration, maxDuration)
    }
}

func TestDownloaderWithSpeedLimit_ZeroIsUnlimited(t *testing.T) {
    base := t.TempDir()
    if err := SetConfigDir(base); err != nil {
        t.Fatalf("SetConfigDir: %v", err)
    }

    // Small file, zero speed limit means unlimited
    dataSize := 10 * int(KB)
    content := bytes.Repeat([]byte("z"), dataSize)
    srv := newDelayedRangeServer(t, content, 0)
    defer srv.Close()

    start := time.Now()
    d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
        DownloadDirectory: base,
        MaxConnections:    1,
        MaxSegments:       1,
        SpeedLimit:        0, // unlimited
    })
    if err != nil {
        t.Fatalf("NewDownloader: %v", err)
    }
    if err := d.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    elapsed := time.Since(start)

    // Verify file content
    got, err := os.ReadFile(d.GetSavePath())
    if err != nil {
        t.Fatalf("ReadFile: %v", err)
    }
    if !bytes.Equal(got, content) {
        t.Fatalf("downloaded content mismatch")
    }

    // Should be very fast (< 500ms for 10KB with no limit)
    if elapsed > 500*time.Millisecond {
        t.Errorf("zero limit should not throttle, took %v", elapsed)
    }
}

func TestDownloaderWithSpeedLimit_LargeFile(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping timing test in short mode")
    }

    base := t.TempDir()
    if err := SetConfigDir(base); err != nil {
        t.Fatalf("SetConfigDir: %v", err)
    }

    // 50KB file at 50KB/s with 2 parts should take ~1 second
    dataSize := 50 * int(KB)
    content := bytes.Repeat([]byte("w"), dataSize)
    srv := newDelayedRangeServer(t, content, 0)
    defer srv.Close()

    start := time.Now()
    d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
        DownloadDirectory: base,
        MaxConnections:    2,
        MaxSegments:       2,
        SpeedLimit:        50 * KB,
    })
    if err != nil {
        t.Fatalf("NewDownloader: %v", err)
    }
    if err := d.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    elapsed := time.Since(start)

    // Verify file content
    got, err := os.ReadFile(d.GetSavePath())
    if err != nil {
        t.Fatalf("ReadFile: %v", err)
    }
    if !bytes.Equal(got, content) {
        t.Fatalf("downloaded content mismatch")
    }

    // Should take approximately 1 second
    expectedDuration := time.Second
    minDuration := time.Duration(float64(expectedDuration) * 0.6)
    maxDuration := time.Duration(float64(expectedDuration) * 1.6)

    if elapsed < minDuration || elapsed > maxDuration {
        t.Errorf("expected duration ~%v, got %v (tolerance: %v-%v)",
            expectedDuration, elapsed, minDuration, maxDuration)
    }
}

func TestDownloaderWithSpeedLimit_Accuracy(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping timing test in short mode")
    }

    base := t.TempDir()
    if err := SetConfigDir(base); err != nil {
        t.Fatalf("SetConfigDir: %v", err)
    }

    // Test that speed limit is accurately enforced
    // 20KB at 10KB/s should take ~2 seconds
    dataSize := 20 * int(KB)
    content := bytes.Repeat([]byte("a"), dataSize)
    srv := newDelayedRangeServer(t, content, 0)
    defer srv.Close()

    start := time.Now()
    d, err := NewDownloader(&http.Client{}, srv.URL+"/file.bin", &DownloaderOpts{
        DownloadDirectory: base,
        MaxConnections:    1,
        MaxSegments:       1,
        SpeedLimit:        10 * KB, // 10KB/s
    })
    if err != nil {
        t.Fatalf("NewDownloader: %v", err)
    }
    if err := d.Start(); err != nil {
        t.Fatalf("Start: %v", err)
    }
    elapsed := time.Since(start)

    // Verify file content
    got, err := os.ReadFile(d.GetSavePath())
    if err != nil {
        t.Fatalf("ReadFile: %v", err)
    }
    if !bytes.Equal(got, content) {
        t.Fatalf("downloaded content mismatch")
    }

    // Should take approximately 2 seconds
    expectedDuration := 2 * time.Second
    minDuration := time.Duration(float64(expectedDuration) * 0.7)
    maxDuration := time.Duration(float64(expectedDuration) * 1.5)

    if elapsed < minDuration || elapsed > maxDuration {
        t.Errorf("expected duration ~%v, got %v (tolerance: %v-%v)",
            expectedDuration, elapsed, minDuration, maxDuration)
    }
}
