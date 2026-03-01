package cookies

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeCopy_CopiesSQLiteFile(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "cookies.sqlite")
	content := []byte("SQLite format 3\x00 some data here for testing")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	tempDir, cleanup, err := SafeCopy(srcPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	// Verify file was copied
	copiedPath := filepath.Join(tempDir, "cookies.sqlite")
	copiedContent, err := os.ReadFile(copiedPath)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(copiedContent) != string(content) {
		t.Error("copied file content does not match source")
	}
}

func TestSafeCopy_CopiesWALAndSHM(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "cookies.sqlite")
	if err := os.WriteFile(srcPath, []byte("main db"), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}
	// Create WAL and SHM files
	walPath := srcPath + "-wal"
	shmPath := srcPath + "-shm"
	if err := os.WriteFile(walPath, []byte("wal data"), 0644); err != nil {
		t.Fatalf("failed to write WAL: %v", err)
	}
	if err := os.WriteFile(shmPath, []byte("shm data"), 0644); err != nil {
		t.Fatalf("failed to write SHM: %v", err)
	}

	tempDir, cleanup, err := SafeCopy(srcPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	// Verify WAL was copied
	copiedWAL, err := os.ReadFile(filepath.Join(tempDir, "cookies.sqlite-wal"))
	if err != nil {
		t.Fatalf("failed to read copied WAL: %v", err)
	}
	if string(copiedWAL) != "wal data" {
		t.Error("copied WAL content does not match source")
	}

	// Verify SHM was copied
	copiedSHM, err := os.ReadFile(filepath.Join(tempDir, "cookies.sqlite-shm"))
	if err != nil {
		t.Fatalf("failed to read copied SHM: %v", err)
	}
	if string(copiedSHM) != "shm data" {
		t.Error("copied SHM content does not match source")
	}
}

func TestSafeCopy_NoWALOrSHM(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "cookies.sqlite")
	if err := os.WriteFile(srcPath, []byte("main db"), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	tempDir, cleanup, err := SafeCopy(srcPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	// WAL and SHM should not exist
	if _, err := os.Stat(filepath.Join(tempDir, "cookies.sqlite-wal")); !os.IsNotExist(err) {
		t.Error("expected WAL not to exist when source has no WAL")
	}
	if _, err := os.Stat(filepath.Join(tempDir, "cookies.sqlite-shm")); !os.IsNotExist(err) {
		t.Error("expected SHM not to exist when source has no SHM")
	}
}

func TestSafeCopy_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()

	_, _, err := SafeCopy(dir)
	if err == nil {
		t.Fatal("expected error when source is a directory, got nil")
	}
}

func TestSafeCopy_RejectsZeroByteFile(t *testing.T) {
	dir := t.TempDir()
	fpath := filepath.Join(dir, "empty.sqlite")
	if err := os.WriteFile(fpath, []byte{}, 0644); err != nil {
		t.Fatalf("failed to write empty file: %v", err)
	}

	_, _, err := SafeCopy(fpath)
	if err == nil {
		t.Fatal("expected error for zero-byte file, got nil")
	}
}

func TestSafeCopy_FileNotFound(t *testing.T) {
	_, _, err := SafeCopy("/nonexistent/path/cookies.sqlite")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestSafeCopy_CleanupRemovesDir(t *testing.T) {
	srcDir := t.TempDir()
	srcPath := filepath.Join(srcDir, "cookies.sqlite")
	if err := os.WriteFile(srcPath, []byte("data"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	tempDir, cleanup, err := SafeCopy(srcPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify temp dir exists before cleanup
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Fatalf("temp dir does not exist before cleanup")
	}

	cleanup()

	// Verify temp dir is gone after cleanup
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Error("temp dir still exists after cleanup")
	}
}
