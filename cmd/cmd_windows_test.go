//go:build windows

package cmd

import (
	"fmt"
	"net"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// createTestListener creates a TCP listener for testing on Windows.
// It uses a dynamic port (0) to avoid conflicts between parallel tests,
// then sets the environment variable so the client knows which port to use.
func createTestListener(t *testing.T, socketPath string) (net.Listener, error) {
	t.Helper()

	// Listen on port 0 to get a random available port
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", common.TCPHost))
	if err != nil {
		return nil, err
	}

	// Extract the actual port assigned by the OS
	port := listener.Addr().(*net.TCPAddr).Port

	// Force TCP mode so clients connect via TCP to this specific port
	t.Setenv(common.ForceTCPEnv, "1")
	t.Setenv(common.TCPPortEnv, fmt.Sprintf("%d", port))

	return listener, nil
}

func TestGetPlatformCommands_ReturnsServiceCommand(t *testing.T) {
	cmds := getPlatformCommands()

	if len(cmds) != 1 {
		t.Fatalf("getPlatformCommands() returned %d commands, want 1", len(cmds))
	}

	if cmds[0].Name != "service" {
		t.Errorf("getPlatformCommands()[0].Name = %q, want %q", cmds[0].Name, "service")
	}

	// Verify subcommands exist
	expectedSubcommands := []string{"install", "uninstall", "start", "stop", "status"}
	subcommandNames := make(map[string]bool)
	for _, subcmd := range cmds[0].Subcommands {
		subcommandNames[subcmd.Name] = true
	}

	for _, expected := range expectedSubcommands {
		if !subcommandNames[expected] {
			t.Errorf("service command missing subcommand %q", expected)
		}
	}
}
