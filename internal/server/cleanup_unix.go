//go:build !windows

package server

import "os"

// cleanupSocket removes the Unix socket file.
// Returns an error if removal fails, unless the file doesn't exist.
func cleanupSocket() error {
	socketPath := socketPath()
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
