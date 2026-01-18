//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	expectedFileSize = 100 * 1024 * 1024 // 100MB
	downloadTimeout  = 5 * time.Minute
	daemonStartWait  = 2 * time.Second
)

var binaryPath string

func TestMain(m *testing.M) {
	// Build binary once for all tests
	tmpDir, err := os.MkdirTemp("", "warpdl-e2e-bin-*")
	if err != nil {
		panic(fmt.Sprintf("failed to create temp dir: %v", err))
	}
	defer os.RemoveAll(tmpDir)

	binaryPath = filepath.Join(tmpDir, "warpdl")
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = getProjectRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("failed to build binary: %s: %v", string(out), err))
	}

	os.Exit(m.Run())
}

func testCLIDownload(t *testing.T, url string) {
	t.Helper()

	// Create isolated temp directories
	configDir := t.TempDir()
	downloadDir := t.TempDir()
	socketPath := filepath.Join(configDir, "warpdl.sock")

	// Set environment for isolation
	env := append(os.Environ(),
		"WARPDL_CONFIG_DIR="+configDir,
		"WARPDL_SOCKET_PATH="+socketPath,
	)

	// Create context for daemon with cancel for cleanup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start daemon in background
	daemonCmd := exec.CommandContext(ctx, binaryPath, "daemon")
	daemonCmd.Env = env
	daemonCmd.Stdout = os.Stdout
	daemonCmd.Stderr = os.Stderr
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}

	// Cleanup: stop daemon gracefully, then force kill
	defer func() {
		// Try graceful stop first
		stopCmd := exec.Command(binaryPath, "stop-daemon")
		stopCmd.Env = env
		_ = stopCmd.Run()

		// Cancel context to trigger kill
		cancel()

		// Wait for daemon to exit (with timeout)
		done := make(chan error, 1)
		go func() { done <- daemonCmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = daemonCmd.Process.Kill()
		}
	}()

	// Wait for daemon to be ready
	time.Sleep(daemonStartWait)

	// Run download command with debug logging
	dlCmd := exec.Command(binaryPath, "download", url,
		"-d", // debug logging for CI visibility
		"-l", downloadDir,
		"-x", "4", // max connections
		"-s", "4", // max segments
	)
	dlCmd.Env = env

	output, err := runWithTimeout(dlCmd, downloadTimeout)
	if err != nil {
		if isNetworkError(err, output) {
			t.Skipf("Network unavailable: %v\nOutput: %s", err, output)
		}
		t.Fatalf("Download failed: %v\nOutput: %s", err, output)
	}

	// Find downloaded file
	filePath := filepath.Join(downloadDir, "100MB.bin")
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("File not found at %s: %v\nDownload output: %s", filePath, err, output)
	}

	// Verify size
	if info.Size() != expectedFileSize {
		t.Fatalf("Size mismatch: want %d, got %d", expectedFileSize, info.Size())
	}

	t.Logf("Downloaded %s successfully (%d bytes)", filePath, info.Size())
}

func runWithTimeout(cmd *exec.Cmd, timeout time.Duration) (string, error) {
	done := make(chan error, 1)
	var output []byte
	var err error

	go func() {
		output, err = cmd.CombinedOutput()
		done <- err
	}()

	select {
	case <-done:
		return string(output), err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("timeout after %v", timeout)
	}
}

func isNetworkError(err error, output string) bool {
	combined := strings.ToLower(err.Error() + output)
	keywords := []string{
		"connection refused",
		"no such host",
		"timeout",
		"deadline exceeded",
		"connection reset",
		"network is unreachable",
		"i/o timeout",
		"dial tcp",
		"tls handshake",
		"no route to host",
		"name resolution",
		"dns",
	}
	for _, kw := range keywords {
		if strings.Contains(combined, kw) {
			return true
		}
	}
	return false
}

func getProjectRoot() string {
	// Walk up from test file to find go.mod
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("failed to get working directory: %v", err))
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find project root (go.mod)")
		}
		dir = parent
	}
}

func TestCLIDownload_ASH(t *testing.T) {
	testCLIDownload(t, "https://ash-speed.hetzner.com/100MB.bin")
}

func TestCLIDownload_FSN1(t *testing.T) {
	testCLIDownload(t, "https://fsn1-speed.hetzner.com/100MB.bin")
}

func TestCLIDownload_HEL1(t *testing.T) {
	testCLIDownload(t, "https://hel1-speed.hetzner.com/100MB.bin")
}

func TestCLIDownload_HIL(t *testing.T) {
	testCLIDownload(t, "https://hil-speed.hetzner.com/100MB.bin")
}
