package warplib

import (
	"fmt"
	"os"
)

// ValidateDownloadDirectory checks if the given path is a valid, writable directory.
// Returns nil if valid, or a specific error:
// - ErrDirectoryNotFound if path doesn't exist
// - ErrNotADirectory if path is a file
// - ErrDirectoryNotWritable if directory is not writable
func ValidateDownloadDirectory(path string) error {
	if path == "" {
		return fmt.Errorf("%w: path is empty", ErrDirectoryNotFound)
	}

	// Check if path exists and get its info
	fileInfo, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrDirectoryNotFound, path)
		}
		return fmt.Errorf("%w: %v", ErrDirectoryNotFound, err)
	}

	// Check if path is a directory
	if !fileInfo.IsDir() {
		return fmt.Errorf("%w: %s", ErrNotADirectory, path)
	}

	// Check if directory is writable by attempting to create a temporary file
	testFile := fmt.Sprintf("%s/.warpdl_write_test_%d", path, os.Getpid())
	f, err := os.OpenFile(testFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrDirectoryNotWritable, path)
	}
	f.Close()
	os.Remove(testFile)

	return nil
}
