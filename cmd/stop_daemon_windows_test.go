//go:build windows

package cmd

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"
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

func TestKillDaemon_FallsBackToKillOnSignalFailure(t *testing.T) {
	fake := &fakeWindowsProcess{
		signalErr: errors.New("interrupt failed"),
	}
	overrideFindProcess(t, fake)

	if err := killDaemon(1234); err != nil {
		t.Fatalf("killDaemon: %v", err)
	}

	if fake.getSignalCalls() != 1 {
		t.Fatalf("expected 1 signal call, got %d", fake.getSignalCalls())
	}
	if fake.getKillCalls() != 1 {
		t.Fatalf("expected 1 kill call, got %d", fake.getKillCalls())
	}
}

func TestKillDaemon_ProcessExits(t *testing.T) {
	// Use 'ping' command which runs for 10 seconds, giving us time to kill it
	cmd := exec.Command("cmd", "/c", "ping -n 10 127.0.0.1 > nul")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid

	// Give the process time to start
	time.Sleep(100 * time.Millisecond)

	// Verify process is running before we kill it
	if !isProcessRunning(pid) {
		t.Fatal("process should be running before kill")
	}

	// Kill it with our function
	err := killDaemon(pid)
	if err != nil {
		t.Fatalf("killDaemon: %v", err)
	}

	// Wait for process to finish - this is the authoritative signal
	// that the process has terminated
	_ = cmd.Wait()

	// Note: We don't check isProcessRunning() after Wait() because on Windows,
	// process handles can remain briefly valid after termination due to handle
	// caching. Wait() returning is the definitive proof the process is dead.
}

func TestKillDaemon_ForceKillAfterTimeout(t *testing.T) {
	waitBlock := make(chan struct{})
	fake := &fakeWindowsProcess{
		waitBlock: waitBlock,
	}
	overrideFindProcess(t, fake)

	timerChan := make(chan time.Time, 1)
	overrideTimeAfter(t, func(d time.Duration) <-chan time.Time {
		return timerChan
	})

	go func() {
		time.Sleep(10 * time.Millisecond)
		timerChan <- time.Time{}
	}()

	if err := killDaemon(5678); err != nil {
		t.Fatalf("killDaemon: %v", err)
	}

	if fake.getSignalCalls() != 1 {
		t.Fatalf("expected 1 signal call, got %d", fake.getSignalCalls())
	}
	if fake.getKillCalls() != 1 {
		t.Fatalf("expected 1 kill call, got %d", fake.getKillCalls())
	}
	if fake.getWaitCalls() != 1 {
		t.Fatalf("expected Wait to be called once, got %d", fake.getWaitCalls())
	}
}

func TestStopDaemon_RunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Use 'ping' command which runs for 10 seconds
	cmd := exec.Command("cmd", "/c", "ping -n 10 127.0.0.1 > nul")
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start test process: %v", err)
	}
	pid := cmd.Process.Pid

	// Give the process time to start
	time.Sleep(100 * time.Millisecond)

	// Verify process is running before we stop it
	if !isProcessRunning(pid) {
		t.Fatal("process should be running before stopDaemon")
	}

	// Write its PID
	if err := os.WriteFile(getPidFilePath(), []byte(strconv.Itoa(pid)), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx := newContext(cli.NewApp(), nil, "stop-daemon")
	if err := stopDaemon(ctx); err != nil {
		t.Fatalf("stopDaemon: %v", err)
	}

	// Wait for process to finish - this is the authoritative signal
	_ = cmd.Wait()

	// Note: We don't check isProcessRunning() after Wait() because on Windows,
	// process handles can remain briefly valid after termination.
}

type fakeWindowsProcess struct {
	mu sync.Mutex

	signalErr error
	killErr   error
	waitErr   error

	waitBlock chan struct{}

	signalCalls int
	killCalls   int
	waitCalls   int
}

func (f *fakeWindowsProcess) Signal(os.Signal) error {
	f.mu.Lock()
	f.signalCalls++
	err := f.signalErr
	f.mu.Unlock()
	return err
}

func (f *fakeWindowsProcess) Kill() error {
	f.mu.Lock()
	f.killCalls++
	ch := f.waitBlock
	if ch != nil {
		f.waitBlock = nil
	}
	err := f.killErr
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
	return err
}

func (f *fakeWindowsProcess) Wait() (*os.ProcessState, error) {
	f.mu.Lock()
	f.waitCalls++
	ch := f.waitBlock
	err := f.waitErr
	f.mu.Unlock()
	if ch != nil {
		<-ch
	}
	return nil, err
}

func (f *fakeWindowsProcess) getSignalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.signalCalls
}

func (f *fakeWindowsProcess) getKillCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.killCalls
}

func (f *fakeWindowsProcess) getWaitCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitCalls
}

func overrideFindProcess(t *testing.T, proc windowsProcess) {
	t.Helper()
	original := findProcess
	findProcess = func(pid int) (windowsProcess, error) {
		return proc, nil
	}
	t.Cleanup(func() {
		findProcess = original
	})
}

func overrideTimeAfter(t *testing.T, fn func(time.Duration) <-chan time.Time) {
	t.Helper()
	original := timeAfter
	timeAfter = fn
	t.Cleanup(func() {
		timeAfter = original
	})
}
