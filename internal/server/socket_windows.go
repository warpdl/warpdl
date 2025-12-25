//go:build windows

package server

import (
	"github.com/warpdl/warpdl/common"
)

// pipePath returns the Windows named pipe path.
// This is a convenience wrapper around common.PipePath().
func pipePath() string {
	return common.PipePath()
}
