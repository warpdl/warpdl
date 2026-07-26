package warplib

import (
	"errors"
	"fmt"
	"io"
	"os"
)

type syncWriteCloser interface {
	Sync() error
	Close() error
}

// validatePhysicalFileSize verifies the local representation at the final
// commit boundary. Logical copy counters alone cannot detect writes performed
// through another descriptor, or a stale tail left by an existing file.
func validatePhysicalFileSize(local *os.File, expected int64) error {
	if local == nil {
		return fmt.Errorf("%w: completed destination is not open", ErrDownloadDataMissing)
	}
	info, err := local.Stat()
	if err != nil {
		return fmt.Errorf("inspect completed destination: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: completed destination is not a regular file", ErrDownloadDataMissing)
	}
	if expected < 0 || info.Size() != expected {
		return fmt.Errorf(
			"%w: completed destination has %d bytes, expected exactly %d",
			ErrDownloadSizeMismatch,
			info.Size(),
			expected,
		)
	}
	return nil
}

// finalizeProtocolTransfer makes durability and remote transfer-finalization
// part of the success path. Both sides are attempted even when one fails so
// resources are not leaked and callers receive every relevant close error.
func finalizeProtocolTransfer(local syncWriteCloser, remote io.Closer) error {
	var errs []error
	if local != nil {
		if err := local.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("sync local destination: %w", err))
		}
		if err := local.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close local destination: %w", err))
		}
	}
	if remote != nil {
		if err := remote.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close remote transfer: %w", err))
		}
	}
	return errors.Join(errs...)
}
