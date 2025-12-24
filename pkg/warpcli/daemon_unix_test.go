//go:build !windows

package warpcli

import (
    "net"
    "os"
    "path/filepath"
    "testing"
    "time"
)

// TestWaitForSocket_BecomesAvailable tests that waitForSocket waits for a Unix socket
// to become available. This test is Unix-specific because it creates a Unix socket
// in a goroutine after a delay.
func TestWaitForSocket_BecomesAvailable(t *testing.T) {
    // Create listener in background after delay
    tmpDir, err := os.MkdirTemp("/tmp", "wdl")
    if err != nil {
        t.Fatalf("failed to create temp dir: %v", err)
    }
    defer os.RemoveAll(tmpDir)
    socketPath := filepath.Join(tmpDir, "delayed.sock")

    listenerReady := make(chan struct{})

    // Start socket creation in background
    go func() {
        time.Sleep(100 * time.Millisecond)
        listener, err := net.Listen("unix", socketPath)
        if err != nil {
            t.Logf("listener creation failed: %v", err)
            return
        }
        close(listenerReady)
        // Keep listener alive for duration of test
        time.Sleep(2 * time.Second)
        listener.Close()
    }()

    start := time.Now()
    err = waitForSocket(socketPath, 2*time.Second)
    elapsed := time.Since(start)

    if err != nil {
        t.Fatalf("waitForSocket failed: %v", err)
    }
    // Should have waited at least 100ms but not much more
    if elapsed < 100*time.Millisecond {
        t.Fatal("waitForSocket returned too early")
    }
    if elapsed > 1*time.Second {
        t.Fatalf("waitForSocket took too long: %v", elapsed)
    }
}
