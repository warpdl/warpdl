//go:build !darwin && !freebsd && !linux && !windows

package warplib

// checkDiskSpace is a stub for platforms where disk space checking is not implemented.
// Returns nil to allow downloads to proceed without disk space validation.
func checkDiskSpace(path string, requiredBytes int64) error {
	return nil
}
