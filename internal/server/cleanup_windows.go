//go:build windows

package server

// cleanupSocket is a no-op on Windows since named pipes are automatically
// cleaned up by the OS when the last handle is closed.
func cleanupSocket() error {
	return nil
}
