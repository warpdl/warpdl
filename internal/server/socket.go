package server

import (
	"os"
	"path/filepath"
)

const socketPathEnv = "WARPDL_SOCKET_PATH"

func socketPath() string {
	if path := os.Getenv(socketPathEnv); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "warpdl.sock")
}
