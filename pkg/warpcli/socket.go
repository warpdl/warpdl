package warpcli

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

const (
	socketPathEnv  = "WARPDL_SOCKET_PATH"
	tcpPortEnv     = "WARPDL_TCP_PORT"
	forceTCPEnv    = "WARPDL_FORCE_TCP"
	debugEnv       = "WARPDL_DEBUG"
	defaultTCPPort = 3849
)

func socketPath() string {
	if path := os.Getenv(socketPathEnv); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "warpdl.sock")
}

// tcpPort returns the TCP port from environment or default 3849
func tcpPort() int {
	if port := os.Getenv(tcpPortEnv); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			return p
		}
	}
	return defaultTCPPort
}

// forceTCP returns true if WARPDL_FORCE_TCP=1
func forceTCP() bool {
	return os.Getenv(forceTCPEnv) == "1"
}

// debugMode returns true if WARPDL_DEBUG=1
func debugMode() bool {
	return os.Getenv(debugEnv) == "1"
}

// tcpAddress returns "localhost:{port}"
func tcpAddress() string {
	return fmt.Sprintf("localhost:%d", tcpPort())
}

// debugLog logs only if debugMode() is true
func debugLog(format string, args ...any) {
	if debugMode() {
		log.Printf(format, args...)
	}
}
