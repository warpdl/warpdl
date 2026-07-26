//go:build !windows

package credman

import (
	"errors"
	"os"
)

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}

func syncParentDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(syncErr, closeErr)
}
