//go:build !windows

package cmd

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestStopDaemon_NoPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// No PID file should succeed with message
	ctx := newContext(cli.NewApp(), nil, "stop-daemon")
	if err := stopDaemon(ctx); err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}
}

func TestStopDaemon_InvalidPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write invalid PID
	if err := os.WriteFile(getPidFilePath(), []byte("invalid"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := newContext(cli.NewApp(), nil, "stop-daemon")
	if err := stopDaemon(ctx); err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}
}

func TestStopDaemon_ProcessNotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write PID of non-existent process
	if err := os.WriteFile(getPidFilePath(), []byte("999999999"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := newContext(cli.NewApp(), nil, "stop-daemon")
	if err := stopDaemon(ctx); err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}
}

func TestKillDaemon_ProcessNotFound(t *testing.T) {
	// Very high PID that doesn't exist
	err := killDaemon(999999999)
	if err == nil {
		t.Fatal("expected error for non-existent process")
	}
}

func TestKillDaemon_ProcessExits(t *testing.T) {
	// Use 'cat' which responds to SIGTERM (unlike 'sleep' on some systems)
	cmd := exec.Command("cat")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to get stdin pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	defer stdin.Close()
	pid := cmd.Process.Pid

	// Kill it with our function
	err = killDaemon(pid)
	if err != nil {
		t.Fatalf("killDaemon: %v", err)
	}

	// Wait for process to finish
	_ = cmd.Wait()

	// Verify it's dead
	time.Sleep(100 * time.Millisecond)
	if isProcessRunning(pid) {
		t.Fatal("expected process to be dead")
	}
}

func TestStopDaemon_RunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Use 'cat' which responds to SIGTERM
	cmd := exec.Command("cat")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to get stdin pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	defer stdin.Close()
	pid := cmd.Process.Pid

	// Write its PID
	if err := os.WriteFile(getPidFilePath(), []byte(strconv.Itoa(pid)), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := newContext(cli.NewApp(), nil, "stop-daemon")
	if err := stopDaemon(ctx); err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}

	// Wait for process to finish
	_ = cmd.Wait()

	// Process should be dead
	time.Sleep(100 * time.Millisecond)
	if isProcessRunning(pid) {
		t.Fatal("expected process to be dead after stopDaemon")
	}
}
