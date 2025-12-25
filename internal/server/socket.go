//go:build !windows

package server

import (
	"os"
	"path/filepath"

	"github.com/warpdl/warpdl/common"
)

func socketPath() string {
	if path := os.Getenv(common.SocketPathEnv); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "warpdl.sock")
}
