package warpcli

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/warpdl/warpdl/common"
)

// tcpPort returns the TCP port from environment or default 3849
func tcpPort() int {
	if port := os.Getenv(common.TCPPortEnv); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			// Validate port range (1-65535)
			if p >= 1 && p <= 65535 {
				return p
			}
			debugLog("invalid TCP port %d, using default %d", p, common.DefaultTCPPort)
		}
	}
	return common.DefaultTCPPort
}

// forceTCP returns true if WARPDL_FORCE_TCP=1
func forceTCP() bool {
	return os.Getenv(common.ForceTCPEnv) == "1"
}

// debugMode returns true if WARPDL_DEBUG=1
func debugMode() bool {
	return os.Getenv(common.DebugEnv) == "1"
}

// tcpAddress returns "localhost:{port}"
func tcpAddress() string {
	return fmt.Sprintf("%s:%d", common.TCPHost, tcpPort())
}

// debugLog logs only if debugMode() is true
func debugLog(format string, args ...any) {
	if debugMode() {
		log.Printf(format, args...)
	}
}
