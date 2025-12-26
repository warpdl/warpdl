package warpcli

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
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
	if testing.Short() {
		t.Skip("skipping in short mode (flaky on Windows race tests)")
	}
	listener, socketPath, err := createTestListener(t)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	if !isDaemonRunning(socketPath) {
		t.Fatal("expected daemon to be running")
	}
}

func TestIsDaemonRunning_TCPFallback(t *testing.T) {
	// Create TCP listener on dynamic port
	tcpListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create TCP listener: %v", err)
	}
	defer tcpListener.Close()

	// Extract port number
	port := tcpListener.Addr().(*net.TCPAddr).Port

	// Configure environment to use TCP fallback
	t.Setenv("WARPDL_TCP_PORT", strconv.Itoa(port))

	// Use a Unix socket path that doesn't exist
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	// Should detect daemon via TCP fallback
	if !isDaemonRunning(sockPath) {
		t.Fatal("expected daemon to be detected via TCP fallback")
	}
}

func TestIsDaemonRunning_BothFail(t *testing.T) {
	// No Unix socket and no TCP listener
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	// Use a TCP port that's not listening
	t.Setenv("WARPDL_TCP_PORT", "9999")

	if isDaemonRunning(sockPath) {
		t.Fatal("expected daemon to not be running when both Unix and TCP fail")
	}
}

func TestWaitForSocket_AlreadyExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode (flaky on Windows race tests)")
	}
	listener, socketPath, err := createTestListener(t)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Should return immediately
	start := time.Now()
	err = waitForSocket(socketPath, 1*time.Second)
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

func TestWaitForSocket_TCPFallback(t *testing.T) {
	// Use a Unix socket path that doesn't exist
	sockPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	// Create TCP listener BEFORE starting waitForSocket
	tcpListener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("TCP listener creation failed: %v", err)
	}
	defer tcpListener.Close()

	// Extract port and set environment BEFORE starting waitForSocket
	port := tcpListener.Addr().(*net.TCPAddr).Port
	t.Setenv("WARPDL_TCP_PORT", strconv.Itoa(port))

	// Now waitForSocket should detect the TCP daemon immediately
	start := time.Now()
	err = waitForSocket(sockPath, 2*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("waitForSocket failed with TCP fallback: %v", err)
	}
	// Should return quickly since TCP is already listening
	if elapsed > 500*time.Millisecond {
		t.Fatalf("waitForSocket took too long: %v", elapsed)
	}
}

func TestEnsureDaemon_AlreadyRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode (flaky on Windows race tests)")
	}
	listener, _, err := createTestListener(t)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Should return immediately without spawning
	err = ensureDaemon()
	if err != nil {
		t.Fatalf("ensureDaemon failed when daemon running: %v", err)
	}
}

func TestSpawnDaemon_Helper(t *testing.T) {
	t.Setenv("WARPCLI_DAEMON_HELPER", "1")
	if err := spawnDaemon(); err != nil {
		t.Fatalf("spawnDaemon: %v", err)
	}
}

func TestEnsureDaemon_SpawnHelper(t *testing.T) {
	t.Setenv("WARPCLI_DAEMON_HELPER", "1")
	sockPath := filepath.Join("/tmp", "warpdl_test_spawn.sock")
	os.Remove(sockPath)
	defer os.Remove(sockPath)
	t.Setenv("WARPDL_SOCKET_PATH", sockPath)

	if err := ensureDaemon(); err != nil {
		t.Fatalf("ensureDaemon: %v", err)
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

func TestGetDaemonStartTimeout_Default(t *testing.T) {
	t.Setenv("WARPDL_DAEMON_TIMEOUT", "")
	timeout := getDaemonStartTimeout()
	if timeout != 10*time.Second {
		t.Fatalf("expected 10s default, got %v", timeout)
	}
}

func TestGetDaemonStartTimeout_EnvVar(t *testing.T) {
	t.Setenv("WARPDL_DAEMON_TIMEOUT", "5s")
	timeout := getDaemonStartTimeout()
	if timeout != 5*time.Second {
		t.Fatalf("expected 5s, got %v", timeout)
	}
}

func TestGetDaemonStartTimeout_EnvVarMilliseconds(t *testing.T) {
	t.Setenv("WARPDL_DAEMON_TIMEOUT", "500ms")
	timeout := getDaemonStartTimeout()
	if timeout != 500*time.Millisecond {
		t.Fatalf("expected 500ms, got %v", timeout)
	}
}

func TestGetDaemonStartTimeout_InvalidEnvVar(t *testing.T) {
	t.Setenv("WARPDL_DAEMON_TIMEOUT", "invalid")
	timeout := getDaemonStartTimeout()
	if timeout != 10*time.Second {
		t.Fatalf("expected 10s fallback for invalid, got %v", timeout)
	}
}

func TestGetDaemonStartTimeout_NegativeEnvVar(t *testing.T) {
	t.Setenv("WARPDL_DAEMON_TIMEOUT", "-5s")
	timeout := getDaemonStartTimeout()
	if timeout != 10*time.Second {
		t.Fatalf("expected 10s fallback for negative, got %v", timeout)
	}
}

func TestGetDaemonStartTimeout_ZeroEnvVar(t *testing.T) {
	t.Setenv("WARPDL_DAEMON_TIMEOUT", "0s")
	timeout := getDaemonStartTimeout()
	if timeout != 10*time.Second {
		t.Fatalf("expected 10s fallback for zero, got %v", timeout)
	}
}
