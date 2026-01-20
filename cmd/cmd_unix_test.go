//go:build !windows

package cmd

import (
	"net"
	"os"
	"testing"
)

// createTestListener creates a Unix socket listener for testing on Unix platforms.
func createTestListener(t *testing.T, socketPath string) (net.Listener, error) {
	t.Helper()
	_ = os.Remove(socketPath)
	return net.Listen("unix", socketPath)
}

func TestGetPlatformCommands_NonWindows(t *testing.T) {
	if cmds := getPlatformCommands(); len(cmds) != 0 {
		t.Fatalf("expected no platform-specific commands, got %d", len(cmds))
	}
}
