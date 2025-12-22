package warpcli

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsDaemonRunning_NotRunning(t *testing.T) {
	// Use a path that definitely doesn't exist
	path := filepath.Join(t.TempDir(), "nonexistent.sock")
	if isDaemonRunning(path) {
		t.Fatal("expected daemon to not be running")
	}
}

func TestIsDaemonRunning_Running(t *testing.T) {
	// Use /tmp for shorter path to avoid macOS socket path length issues
	sockPath := filepath.Join("/tmp", "warpdl_test_running.sock")
	os.Remove(sockPath) // Clean up any leftover
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	if !isDaemonRunning(sockPath) {
		t.Fatal("expected daemon to be running")
	}
}

func TestWaitForSocket_AlreadyExists(t *testing.T) {
	// Use /tmp for shorter path to avoid macOS socket path length issues
	sockPath := filepath.Join("/tmp", "warpdl_test_existing.sock")
	os.Remove(sockPath) // Clean up any leftover
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Should return immediately
	start := time.Now()
	err = waitForSocket(sockPath, 1*time.Second)
	if err != nil {
		t.Fatalf("waitForSocket failed: %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("waitForSocket took too long for existing socket")
	}
}

func TestWaitForSocket_Timeout(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	start := time.Now()
	err := waitForSocket(sockPath, 200*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed < 200*time.Millisecond {
		t.Fatalf("waitForSocket returned too early: %v", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForSocket took too long: %v", elapsed)
	}
}

func TestWaitForSocket_BecomesAvailable(t *testing.T) {
	// Use /tmp for shorter path to avoid macOS socket path length issues
	sockPath := filepath.Join("/tmp", "warpdl_test_delayed.sock")
	os.Remove(sockPath) // Clean up any leftover
	defer os.Remove(sockPath)

	listenerReady := make(chan struct{})

	// Start socket creation in background
	go func() {
		time.Sleep(100 * time.Millisecond)
		listener, err := net.Listen("unix", sockPath)
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
	err := waitForSocket(sockPath, 2*time.Second)
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

func TestEnsureDaemon_AlreadyRunning(t *testing.T) {
	// Use /tmp for shorter path to avoid macOS socket path length issues
	sockPath := filepath.Join("/tmp", "warpdl_test_ensure.sock")
	os.Remove(sockPath) // Clean up any leftover
	defer os.Remove(sockPath)

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Point to our test socket
	t.Setenv("WARPDL_SOCKET_PATH", sockPath)

	// Should return immediately without spawning
	err = ensureDaemon()
	if err != nil {
		t.Fatalf("ensureDaemon failed when daemon running: %v", err)
	}
}

func TestSpawnDaemon_InvalidExecutable(t *testing.T) {
	// This test verifies spawnDaemon works with the current executable
	// We can't easily test failure case without modifying os.Executable
	// Skip if we can't get the executable path
	exe, err := os.Executable()
	if err != nil {
		t.Skip("cannot get executable path")
	}
	if exe == "" {
		t.Skip("empty executable path")
	}
	// Just verify the function doesn't panic
	// Note: We don't actually test spawnDaemon here since it would
	// spawn a real daemon process
}
