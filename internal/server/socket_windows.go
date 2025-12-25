//go:build windows

package server

import (
    "os"

    "github.com/warpdl/warpdl/common"
)

// pipePath returns the Windows named pipe path.
// This is a convenience wrapper around common.PipePath().
func pipePath() string {
    return common.PipePath()
}

// forceTCP checks if the WARPDL_FORCE_TCP environment variable is set to "1".
// When enabled, the server will skip named pipe creation and use TCP only.
func forceTCP() bool {
    return os.Getenv(common.ForceTCPEnv) == "1"
}
