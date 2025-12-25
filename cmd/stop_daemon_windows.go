//go:build windows

package cmd

import (
	"fmt"
	"os"
	"time"
)

const (
	shutdownTimeout = 5 * time.Second
)

type windowsProcess interface {
	Signal(os.Signal) error
	Kill() error
	Wait() (*os.ProcessState, error)
}

var (
	findProcess = func(pid int) (windowsProcess, error) {
		return os.FindProcess(pid)
	}
	timeAfter = func(d time.Duration) <-chan time.Time {
		return time.After(d)
	}
)

var _ windowsProcess = (*os.Process)(nil)

// killDaemon sends an interrupt signal to the daemon and waits for it to exit.
// On Windows, we use os.Interrupt and then forcefully terminate if needed.
func killDaemon(pid int) error {
	process, err := findProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}

	// On Windows, os.Interrupt may not work for all processes.
	// We'll try it first, then fall back to Kill.
	if err := process.Signal(os.Interrupt); err != nil {
		// If interrupt fails, try to kill directly
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}
		return nil
	}

	// Wait for process to exit with timeout using a goroutine
	done := make(chan error, 1)
	go func() {
		_, err := process.Wait()
		done <- err
	}()

	select {
	case <-done:
		return nil // Process exited
	case <-timeAfter(shutdownTimeout):
		// Timeout exceeded, force kill
		fmt.Println("Graceful shutdown timeout, forcing kill...")
		if err := process.Kill(); err != nil {
			return fmt.Errorf("failed to kill daemon: %w", err)
		}
		return nil
	}
}
