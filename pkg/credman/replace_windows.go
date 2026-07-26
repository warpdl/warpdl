//go:build windows

package credman

import "golang.org/x/sys/windows"

func replaceFile(src, dst string) error {
	srcPtr, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstPtr, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		srcPtr,
		dstPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}

// MoveFileEx with MOVEFILE_WRITE_THROUGH flushes the replacement before it
// returns. Windows does not expose a portable directory-fsync equivalent.
func syncParentDirectory(string) error {
	return nil
}
