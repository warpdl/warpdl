// Package warplib provides the core download engine for WarpDL.
// This file contains utilities for file operations, particularly for
// cross-device file moves which are common when the download temp directory
// and final destination are on different filesystems/drives.
package warplib

import (
	"fmt"
	"io"
	"os"
)

// moveFile moves a file from src to dst atomically when possible.
//
// It first attempts os.Rename which is atomic on the same filesystem.
// If the rename fails with a cross-device error (EXDEV on Unix,
// ERROR_NOT_SAME_DEVICE on Windows), it falls back to copy+delete.
//
// This function is particularly useful on Windows where download temp
// directories may be on a different drive than the final destination.
//
// Parameters:
//   - src: absolute path to the source file (must exist)
//   - dst: absolute path to the destination file (parent directory must exist)
//
// Returns an error if:
//   - The source file does not exist
//   - The destination directory does not exist
//   - There are permission issues
//   - The copy operation fails (disk full, I/O error, etc.)
func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Check if it's a cross-device error
	if !isCrossDeviceError(err) {
		return fmt.Errorf("moveFile %s -> %s: %w", src, dst, err)
	}

	// Fall back to copy+delete for cross-device moves
	if err := copyAndDelete(src, dst); err != nil {
		return fmt.Errorf("moveFile %s -> %s (cross-device): %w", src, dst, err)
	}
	return nil
}

// copyAndDelete copies a file from src to dst, then deletes the source.
//
// This function is used as a fallback when os.Rename fails due to
// cross-device errors. It preserves the file permissions from the source.
//
// The function uses a 32KB buffer (DEF_CHUNK_SIZE) for efficient copying
// of large files, matching the chunk size used elsewhere in warplib.
//
// On error during copy, any partially written destination file is cleaned up.
// The source file is only deleted after successful copy and sync.
//
// Parameters:
//   - src: absolute path to the source file (must exist)
//   - dst: absolute path to the destination file (parent directory must exist)
//
// Returns an error if:
//   - The source file cannot be read
//   - The destination file cannot be created
//   - The copy operation fails
//   - The source file cannot be deleted after successful copy
func copyAndDelete(src, dst string) error {
	// Get source file info for permissions
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	// Open source file
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	// Create destination file with same permissions
	dstFile, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, srcInfo.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}

	// Track whether we need to clean up on error
	copySucceeded := false
	defer func() {
		dstFile.Close()
		if !copySucceeded {
			// Clean up partial destination file on error
			os.Remove(dst)
		}
	}()

	// Copy content using a buffer for better performance with large files
	buf := make([]byte, DEF_CHUNK_SIZE)
	_, err = io.CopyBuffer(dstFile, srcFile, buf)
	if err != nil {
		return fmt.Errorf("copy content: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("sync destination: %w", err)
	}

	// Explicitly check close error before marking success
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}

	// Mark copy as successful so defer doesn't clean up
	copySucceeded = true

	// Close source file before delete
	srcFile.Close()

	// Delete source only after successful copy
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove source: %w", err)
	}

	return nil
}
