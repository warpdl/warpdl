//go:build !windows

package server

import "os"

func setSocketPermissions(path string) {
	_ = os.Chmod(path, 0700)
}
