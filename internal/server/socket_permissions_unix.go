//go:build !windows

package server

import "os"

// setSocketPermissions sets Unix socket permissions to owner-only (0700).
// This restricts access to the daemon socket to only the user who started it,
// following the principle of least privilege.
func setSocketPermissions(path string) error {
	return os.Chmod(path, 0700)
}
