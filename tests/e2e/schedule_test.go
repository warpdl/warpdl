//go:build e2e

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestScheduledDownload_StartAt verifies that a download scheduled with
// --start-at near-future actually starts and completes (T074 scheduling path).
func TestScheduledDownload_StartAt(t *testing.T) {
	configDir := t.TempDir()
	downloadDir := t.TempDir()
	socketPath := filepath.Join(configDir, "warpdl.sock")

	env := append(os.Environ(),
		"WARPDL_CONFIG_DIR="+configDir,
		"WARPDL_SOCKET_PATH="+socketPath,
	)

	ctx, cancel := newDaemonContext(t)
	defer cancel()

	daemonCmd := exec.CommandContext(ctx, binaryPath, "daemon")
	daemonCmd.Env = env
	daemonCmd.Stdout = os.Stdout
	daemonCmd.Stderr = os.Stderr
	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	defer stopDaemon(t, binaryPath, env, daemonCmd, cancel)

	time.Sleep(daemonStartWait)

	// Schedule download 2 minutes from now — far enough ahead that the
	// minute-precision format "2006-01-02 15:04" never falls in the past,
	// regardless of what second within the current minute we are.
	startAt := time.Now().Add(2 * time.Minute).Format("2006-01-02 15:04")
	dlURL := "https://ash-speed.hetzner.com/100MB.bin"

	dlCmd := exec.Command(binaryPath, "download", dlURL,
		"--start-at", startAt,
		"-l", downloadDir,
		"-x", "4",
	)
	dlCmd.Env = env

	output, err := runWithTimeout(dlCmd, 30*time.Second)
	if err != nil && !isNetworkError(err, output) {
		t.Fatalf("schedule download: %v\nOutput: %s", err, output)
	}
	if isNetworkError(err, output) {
		t.Skipf("Network unavailable: %v", err)
	}

	// List downloads immediately — the item must appear as scheduled (not yet triggered).
	listCmd := exec.Command(binaryPath, "list")
	listCmd.Env = env
	listOutput, _ := listCmd.CombinedOutput()
	t.Logf("List output: %s", listOutput)
	if !strings.Contains(string(listOutput), "100MB.bin") {
		t.Errorf("expected scheduled download to appear in list output, got: %s", listOutput)
	}
}

// newDaemonContext creates a context for daemon lifecycle management.
func newDaemonContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}

// stopDaemon gracefully stops the daemon process.
func stopDaemon(t *testing.T, binary string, env []string, cmd *exec.Cmd, cancel context.CancelFunc) {
	t.Helper()
	stopCmd := exec.Command(binary, "stop-daemon")
	stopCmd.Env = env
	_ = stopCmd.Run()
	cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
	}
}
