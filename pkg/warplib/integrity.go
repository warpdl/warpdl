package warplib

import (
	"fmt"
	"os"
	"path/filepath"
)

// validateDownloadIntegrity checks that all required data files exist for resuming a download.
// It verifies:
// 1. Download data directory exists ({DlDataDir}/{hash}/)
// 2. Part files exist for all non-compiled parts ({dlPath}/{part.Hash}.warp)
// 3. Main file exists if any part is compiled OR if bytes were downloaded ({item.AbsolutePath})
//
// Returns ErrDownloadDataMissing if any check fails.
func validateDownloadIntegrity(item *Item) error {
	// Check 1: Download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if !dirExists(dlPath) {
		return fmt.Errorf("%w: download data directory missing: %s", ErrDownloadDataMissing, dlPath)
	}

	// Check 2: Part files for non-compiled parts
	for _, part := range item.Parts {
		if part.Compiled {
			continue
		}
		partFile := getFileName(dlPath, part.Hash)
		if !fileExists(partFile) {
			return fmt.Errorf("%w: part file missing: %s", ErrDownloadDataMissing, partFile)
		}
	}

	// Check 3: Main file if download has progress
	needsMainFile := item.Downloaded > 0
	if !needsMainFile {
		for _, part := range item.Parts {
			if part.Compiled {
				needsMainFile = true
				break
			}
		}
	}

	if needsMainFile {
		mainFile := item.GetAbsolutePath()
		stat, err := os.Stat(mainFile)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: main file missing: %s", ErrDownloadDataMissing, mainFile)
			}
			return fmt.Errorf("%w: cannot access main file: %s: %v", ErrDownloadDataMissing, mainFile, err)
		}
		if stat.Size() == 0 {
			return fmt.Errorf("%w: main file exists but is empty: %s", ErrDownloadDataMissing, mainFile)
		}
	}

	return nil
}

// fileExists checks if a regular file exists at the given path.
func fileExists(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !stat.IsDir()
}
