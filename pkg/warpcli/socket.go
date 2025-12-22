package warpcli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const socketPathEnv = "WARPDL_SOCKET_PATH"

func socketPath() string {
	if path := os.Getenv(socketPathEnv); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "warpdl.sock")
}

// ParseDaemonURI parses a daemon URI and returns the network type and address.
// Supported formats:
//   - tcp://host:port (e.g., tcp://localhost:9090)
//   - unix:///path/to/socket (e.g., unix:///tmp/warpdl.sock)
//   - /path/to/socket (defaults to unix socket)
//
// Returns ("unix", path) or ("tcp", host:port)
func ParseDaemonURI(uri string) (network, address string, err error) {
	if uri == "" {
		return "", "", fmt.Errorf("daemon URI cannot be empty")
	}

	// Handle tcp:// prefix
	if strings.HasPrefix(uri, "tcp://") {
		address = strings.TrimPrefix(uri, "tcp://")
		if address == "" {
			return "", "", fmt.Errorf("invalid TCP URI: missing address")
		}
		return "tcp", address, nil
	}

	// Handle unix:// prefix
	if strings.HasPrefix(uri, "unix://") {
		address = strings.TrimPrefix(uri, "unix://")
		if address == "" {
			return "", "", fmt.Errorf("invalid Unix socket URI: missing path")
		}
		return "unix", address, nil
	}

	// Default: treat as Unix socket path
	return "unix", uri, nil
}
