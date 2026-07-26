package cookies

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SafeCopy copies a SQLite cookie file (and its -wal and -shm companions if
// they exist) to a temporary directory. This prevents locking conflicts with
// the browser that owns the database.
//
// Returns the temporary directory path, a cleanup function that removes the
// temp directory, and an error. The caller MUST call cleanup when done.
func SafeCopy(srcPath string) (tempDir string, cleanup func(), err error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", nil, fmt.Errorf("cookie file not found: %s", srcPath)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("%s is a directory, expected a cookie file path or 'auto'", srcPath)
	}
	if info.Size() == 0 {
		return "", nil, fmt.Errorf("cookie file at %s is empty or corrupted", srcPath)
	}

	tempDir, err = os.MkdirTemp("", "warpdl-cookies-*")
	if err != nil {
		return "", nil, fmt.Errorf("cannot create temp directory: %w", err)
	}

	cleanup = func() {
		_ = os.RemoveAll(tempDir)
	}

	baseName := filepath.Base(srcPath)

	// Copy main file
	if err := copyFile(srcPath, filepath.Join(tempDir, baseName)); err != nil {
		cleanup()
		return "", nil, err
	}

	// Copy WAL and SHM if they exist (best-effort)
	for _, suffix := range []string{"-wal", "-shm"} {
		companion := srcPath + suffix
		if _, err := os.Stat(companion); err == nil {
			_ = copyFile(companion, filepath.Join(tempDir, baseName+suffix))
		}
	}

	return tempDir, cleanup, nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open source file %s: %w", src, err)
	}
	defer func() {
		if closeErr := in.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("cannot close source file %s: %w", src, closeErr)
		}
	}()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("cannot create destination file %s: %w", dst, err)
	}
	defer func() {
		if closeErr := out.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("cannot close destination file %s: %w", dst, closeErr)
		}
	}()

	if _, err = io.Copy(out, in); err != nil {
		return fmt.Errorf("cannot copy file: %w", err)
	}
	return nil
}
