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
// 3. Main file exists if any part was already compiled into it ({item.AbsolutePath})
//
// Raw download progress can live entirely inside part files, so Downloaded > 0
// does not by itself require a non-empty destination file yet.
//
// Returns ErrDownloadDataMissing if any check fails.
func validateDownloadIntegrity(item *Item) error {
	return validateDownloadIntegritySnapshot(item.Snapshot())
}

// validateDownloadIntegritySnapshot operates only on an immutable deep
// snapshot. Manager resume already owns such a snapshot, avoiding unlocked
// reads of Item fields while API and progress callbacks mutate them.
func validateDownloadIntegritySnapshot(item ItemSnapshot) error {
	// Check 1: Download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if !dirExists(dlPath) {
		return fmt.Errorf("%w: download data directory missing: %s", ErrDownloadDataMissing, dlPath)
	}

	hasCompiledPart := false
	allPartsCompiled := len(item.Parts) > 0
	maxCompiledOffset := int64(-1)

	// Check 2: Part files for non-compiled parts, while calculating the
	// minimum physical destination extent required by compiled ranges.
	for start, part := range item.Parts {
		if part == nil {
			return fmt.Errorf("%w: nil part at offset %d", ErrDownloadDataMissing, start)
		}
		if part.Compiled {
			hasCompiledPart = true
			if start < 0 || part.FinalOffset < start {
				return fmt.Errorf("%w: invalid compiled range %d-%d",
					ErrDownloadDataMissing, start, part.FinalOffset)
			}
			if part.FinalOffset > maxCompiledOffset {
				maxCompiledOffset = part.FinalOffset
			}
			continue
		}
		allPartsCompiled = false
		partFile := getFileName(dlPath, part.Hash)
		stat, err := WarpLstat(partFile)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: part file missing: %s", ErrDownloadDataMissing, partFile)
			}
			return fmt.Errorf("%w: cannot access part file %s: %v",
				ErrDownloadDataMissing, partFile, err)
		}
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("%w: part file is not regular: %s", ErrDownloadDataMissing, partFile)
		}
		if start < 0 || part.FinalOffset < start {
			return fmt.Errorf("%w: invalid part range %d-%d",
				ErrDownloadDataMissing, start, part.FinalOffset)
		}
		expectedSize := part.FinalOffset - start + 1
		if stat.Size() > expectedSize {
			return fmt.Errorf(
				"%w: part file %s contains %d bytes, declared range permits at most %d",
				ErrDownloadDataMissing,
				partFile,
				stat.Size(),
				expectedSize,
			)
		}
	}

	// A resumed HTTP download must still have some segment state unless it has
	// already compiled data into the destination file.
	if item.Downloaded > 0 && len(item.Parts) == 0 && !hasCompiledPart {
		return fmt.Errorf("%w: download has progress but no part state: %s", ErrDownloadDataMissing, item.Hash)
	}

	if hasCompiledPart {
		mainFile := GetPath(item.AbsoluteLocation, item.Name)
		stat, err := os.Stat(mainFile)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("%w: main file missing: %s", ErrDownloadDataMissing, mainFile)
			}
			return fmt.Errorf("%w: cannot access main file: %s: %v", ErrDownloadDataMissing, mainFile, err)
		}
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("%w: main destination is not a regular file: %s", ErrDownloadDataMissing, mainFile)
		}
		requiredSize := maxCompiledOffset + 1
		if stat.Size() < requiredSize {
			return fmt.Errorf(
				"%w: main file is truncated: %s has %d bytes, compiled ranges require at least %d",
				ErrDownloadDataMissing,
				mainFile,
				stat.Size(),
				requiredSize,
			)
		}
		totalSize := item.TotalSize.v()
		if totalSize > 0 && stat.Size() > totalSize {
			return fmt.Errorf(
				"%w: main file has %d bytes, expected at most %d while resuming: %s",
				ErrDownloadDataMissing,
				stat.Size(),
				totalSize,
				mainFile,
			)
		}
		if allPartsCompiled && (totalSize <= 0 || stat.Size() != totalSize) {
			return fmt.Errorf(
				"%w: fully compiled main file has %d bytes, expected exactly %d: %s",
				ErrDownloadDataMissing,
				stat.Size(),
				totalSize,
				mainFile,
			)
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
