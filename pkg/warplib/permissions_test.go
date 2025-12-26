package warplib

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// TestFilePermissionConstants verifies that permission constants exist
// and have the correct values.
func TestFilePermissionConstants(t *testing.T) {
	if DefaultFileMode != 0644 {
		t.Errorf("DefaultFileMode = %o, want 0644", DefaultFileMode)
	}

	if DefaultDirMode != 0755 {
		t.Errorf("DefaultDirMode = %o, want 0755", DefaultDirMode)
	}
}

// TestOpenFilePermissions verifies that openFile() creates files with 0644 permissions.
// This test will FAIL because current implementation uses 0666.
func TestOpenFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File permission tests are not applicable on Windows")
	}

	// Save and restore original umask
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	tmpDir := t.TempDir()
	if err := SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	hash := "test_hash_1"
	savePath := filepath.Join(tmpDir, "test_download.bin")

	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create downloader using initDownloader with overwrite enabled
	d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", 1024, &DownloaderOpts{
		DownloadDirectory: tmpDir,
		FileName:          "test_download.bin",
		Overwrite:         true,
		Handlers: &Handlers{
			DownloadProgressHandler: func(hash string, read int) {},
			DownloadCompleteHandler: func(hash string, size int64) {},
		},
	})
	if err != nil {
		t.Fatalf("initDownloader() failed: %v", err)
	}
	defer d.Close()

	// Call openFile directly
	err = d.openFile()
	if err != nil {
		t.Fatalf("openFile() failed: %v", err)
	}
	d.f.Close()

	// Verify file permissions (with umask 0, we see the actual mode used)
	info, err := os.Stat(savePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	gotPerm := info.Mode().Perm()
	wantPerm := os.FileMode(0644)

	if gotPerm != wantPerm {
		t.Errorf("file permissions = %o, want %o (EXPECTED FAILURE: current code uses 0666)", gotPerm, wantPerm)
	}
}

// TestSetupLoggerPermissions verifies that setupLogger() creates logs.txt with 0644 permissions.
// This test will FAIL because current implementation uses 0666.
func TestSetupLoggerPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File permission tests are not applicable on Windows")
	}

	// Save and restore original umask
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	tmpDir := t.TempDir()
	dlPath := filepath.Join(tmpDir, "test_hash")

	// Create the download directory
	err := os.MkdirAll(dlPath, 0755)
	if err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	// Create a minimal downloader with dlPath set
	d := &Downloader{
		dlPath: dlPath,
	}

	// Call setupLogger
	err = d.setupLogger()
	if err != nil {
		t.Fatalf("setupLogger() failed: %v", err)
	}
	d.lw.Close()

	// Verify logs.txt permissions (with umask 0, we see the actual mode used)
	logPath := filepath.Join(dlPath, "logs.txt")
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	gotPerm := info.Mode().Perm()
	wantPerm := os.FileMode(0644)

	if gotPerm != wantPerm {
		t.Errorf("logs.txt permissions = %o, want %o (EXPECTED FAILURE: current code uses 0666)", gotPerm, wantPerm)
	}
}

// TestWarpCreatePermissions verifies that WarpCreate() creates files with 0644 permissions.
// This test will FAIL because os.Create() defaults to 0666.
func TestWarpCreatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File permission tests are not applicable on Windows")
	}

	// Save and restore original umask
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "warp_create_test.bin")

	// Call WarpCreate (with umask 0, we see the actual mode used)
	f, err := WarpCreate(testFile)
	if err != nil {
		t.Fatalf("WarpCreate() failed: %v", err)
	}
	f.Close()

	// Verify file permissions
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	gotPerm := info.Mode().Perm()
	wantPerm := os.FileMode(0644)

	if gotPerm != wantPerm {
		t.Errorf("file permissions = %o, want %o (EXPECTED FAILURE: os.Create defaults to 0666)", gotPerm, wantPerm)
	}
}

// TestPartFilePermissions verifies that part files are created with 0644 permissions.
// This test will FAIL because current implementation uses 0666 in openPartFile.
func TestPartFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File permission tests are not applicable on Windows")
	}

	// Save and restore original umask
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	tmpDir := t.TempDir()
	partFileName := filepath.Join(tmpDir, "test_part.warpPart")

	// Create a part file using WarpCreate (simulating createPartFile behavior)
	f, err := WarpCreate(partFileName)
	if err != nil {
		t.Fatalf("WarpCreate() failed: %v", err)
	}
	f.Close()

	// Now open it with openPartFile permissions (this is what Part.openPartFile does)
	// The mode parameter in OpenFile is only used if the file doesn't exist,
	// but let's verify the file has correct permissions
	f2, err := WarpOpenFile(partFileName, os.O_RDWR, 0666)
	if err != nil {
		t.Fatalf("WarpOpenFile() failed: %v", err)
	}
	f2.Close()

	// Verify file permissions after both operations (with umask 0, we see the actual mode used)
	info, err := os.Stat(partFileName)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	gotPerm := info.Mode().Perm()
	wantPerm := os.FileMode(0644)

	if gotPerm != wantPerm {
		t.Errorf("part file permissions = %o, want %o (EXPECTED FAILURE: current code uses 0666)", gotPerm, wantPerm)
	}
}

// TestNewFileCreationPermissions verifies that when openFile creates a new file
// (not overwriting), it uses 0644 permissions.
// This test will FAIL because current implementation uses 0666.
func TestNewFileCreationPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File permission tests are not applicable on Windows")
	}

	// Save and restore original umask
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	tmpDir := t.TempDir()
	if err := SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	hash := "test_hash_2"
	savePath := filepath.Join(tmpDir, "new_file.bin")

	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create downloader without overwrite (new file creation)
	d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", 512, &DownloaderOpts{
		DownloadDirectory: tmpDir,
		FileName:          "new_file.bin",
		Handlers: &Handlers{
			DownloadProgressHandler: func(hash string, read int) {},
			DownloadCompleteHandler: func(hash string, size int64) {},
		},
	})
	if err != nil {
		t.Fatalf("initDownloader() failed: %v", err)
	}
	defer d.Close()

	// Call openFile to create new file
	err = d.openFile()
	if err != nil {
		t.Fatalf("openFile() failed: %v", err)
	}
	d.f.Close()

	// Verify file permissions (with umask 0, we see the actual mode used)
	info, err := os.Stat(savePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	gotPerm := info.Mode().Perm()
	wantPerm := os.FileMode(0644)

	if gotPerm != wantPerm {
		t.Errorf("new file permissions = %o, want %o (EXPECTED FAILURE: current code uses 0666)", gotPerm, wantPerm)
	}
}

// TestOverwriteFilePermissions verifies that when openFile overwrites an existing file,
// it uses 0644 permissions.
// This test will FAIL because current implementation uses 0666.
func TestOverwriteFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("File permission tests are not applicable on Windows")
	}

	// Save and restore original umask
	oldMask := syscall.Umask(0)
	defer syscall.Umask(oldMask)

	tmpDir := t.TempDir()
	if err := SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	hash := "test_hash_3"
	savePath := filepath.Join(tmpDir, "existing_file.bin")

	if err := os.MkdirAll(filepath.Join(DlDataDir, hash), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create existing file with different permissions
	err := os.WriteFile(savePath, []byte("old content"), 0600)
	if err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	// Create downloader with overwrite enabled
	d, err := initDownloader(&http.Client{}, hash, "http://example.com/file.bin", 256, &DownloaderOpts{
		DownloadDirectory: tmpDir,
		FileName:          "existing_file.bin",
		Overwrite:         true,
		Handlers: &Handlers{
			DownloadProgressHandler: func(hash string, read int) {},
			DownloadCompleteHandler: func(hash string, size int64) {},
		},
	})
	if err != nil {
		t.Fatalf("initDownloader() failed: %v", err)
	}
	defer d.Close()

	// Call openFile to overwrite
	err = d.openFile()
	if err != nil {
		t.Fatalf("openFile() failed: %v", err)
	}
	d.f.Close()

	// Verify file permissions were set correctly during overwrite (with umask 0, we see the actual mode used)
	info, err := os.Stat(savePath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	gotPerm := info.Mode().Perm()
	wantPerm := os.FileMode(0644)

	if gotPerm != wantPerm {
		t.Errorf("overwritten file permissions = %o, want %o (EXPECTED FAILURE: current code uses 0666)", gotPerm, wantPerm)
	}
}
