//go:build windows

package warplib

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

var (
	// getFreeBytes is a function that calls Windows API.
	// Variable to allow mocking in tests.
	getFreeBytes = realGetFreeBytes
)

func realGetFreeBytes(path string) (freeBytesAvailable uint64, err error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("failed to convert path to UTF-16: %w", err)
	}

	err = windows.GetDiskFreeSpaceEx(pathPtr,
		&freeBytesAvailable,
		nil,
		nil,
	)

	if err != nil {
		return 0, fmt.Errorf("GetDiskFreeSpaceEx failed: %w", err)
	}

	return freeBytesAvailable, nil
}

// checkDiskSpace checks if there's enough disk space available at the given path
// to accommodate a file of the specified size.
// Returns an error if insufficient space is available.
//
// On Windows, this uses GetDiskFreeSpaceExW to query the volume's free space.
// If the check fails for any reason (invalid path, permission error, etc.),
// it returns nil to gracefully degrade and not block downloads.
func checkDiskSpace(path string, requiredBytes int64) error {
	if requiredBytes <= 0 {
		return nil
	}

	probePath, err := resolveProbePath(path)
	if err != nil {
		return nil
	}

	freeBytesAvailable, err := getFreeBytes(probePath)
	if err != nil {
		return nil
	}

	required := uint64(requiredBytes)
	if freeBytesAvailable < required {
		return fmt.Errorf("%w: required space %s, available space %s",
			ErrInsufficientDiskSpace,
			ContentLength(requiredBytes),
			ContentLength(int64(freeBytesAvailable)))
	}

	return nil
}

// resolveProbePath resolves a suitable path for GetDiskFreeSpaceExW from the given path.
// It handles relative paths, file paths, and various edge cases.
func resolveProbePath(path string) (string, error) {
	if path == "" {
		return filepath.Abs(".")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Abs(".")
	}

	info, err := getFileInfo(absPath)
	if err != nil {
		return filepath.Dir(absPath), nil
	}

	if !info.IsDir() {
		return filepath.Dir(absPath), nil
	}

	return absPath, nil
}

var getFileInfo = func(path string) (interface{ IsDir() bool }, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &dirInfo{info}, nil
}

type dirInfo struct {
	os.FileInfo
}

func (d *dirInfo) IsDir() bool {
	return d.FileInfo.IsDir()
}
