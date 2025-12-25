package warplib

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWarpOpen_Integration tests the WarpOpen wrapper function with real file operations.
func TestWarpOpen_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("open short path file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "short_file.txt")
		content := "test content for short path"

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		file, err := WarpOpen(filePath)
		if err != nil {
			t.Errorf("WarpOpen() unexpected error: %v", err)
			return
		}
		defer file.Close()

		readContent, err := io.ReadAll(file)
		if err != nil {
			t.Errorf("failed to read file: %v", err)
		}
		if string(readContent) != content {
			t.Errorf("file content = %q, want %q", string(readContent), content)
		}
	})

	t.Run("open non-existent file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "nonexistent.txt")
		_, err := WarpOpen(filePath)
		if err == nil {
			t.Errorf("WarpOpen() expected error for non-existent file, got nil")
		}
	})
}

// TestWarpCreate_Integration tests the WarpCreate wrapper function with real file operations.
func TestWarpCreate_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("create short path file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "created_short.txt")
		content := "test data for short path"

		file, err := WarpCreate(filePath)
		if err != nil {
			t.Errorf("WarpCreate() unexpected error: %v", err)
			return
		}

		n, err := file.WriteString(content)
		if err != nil {
			t.Errorf("failed to write to file: %v", err)
		}
		file.Close()

		if n != len(content) {
			t.Errorf("wrote %d bytes, want %d", n, len(content))
		}

		readContent, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("failed to read created file: %v", err)
		}
		if string(readContent) != content {
			t.Errorf("file content = %q, want %q", string(readContent), content)
		}
	})

	t.Run("create file in non-existent directory", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "nonexistent_dir", "file.txt")
		_, err := WarpCreate(filePath)
		if err == nil {
			t.Errorf("WarpCreate() expected error for non-existent directory, got nil")
		}
	})
}

// TestWarpMkdirAll_Integration tests the WarpMkdirAll wrapper function.
func TestWarpMkdirAll_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("create short path directory", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "short", "nested", "dir")

		err := WarpMkdirAll(dirPath, 0755)
		if err != nil {
			t.Errorf("WarpMkdirAll() unexpected error: %v", err)
			return
		}

		info, err := os.Stat(dirPath)
		if err != nil {
			t.Errorf("created directory does not exist: %v", err)
			return
		}
		if !info.IsDir() {
			t.Errorf("path exists but is not a directory")
		}
	})

	t.Run("create already existing directory", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "existing")
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			t.Fatalf("failed to create existing directory: %v", err)
		}

		err := WarpMkdirAll(dirPath, 0755)
		if err != nil {
			t.Errorf("WarpMkdirAll() should not error on existing directory: %v", err)
		}
	})
}

// TestWarpRemove_Integration tests the WarpRemove wrapper function.
func TestWarpRemove_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("remove short path file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "to_remove.txt")
		if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		err := WarpRemove(filePath)
		if err != nil {
			t.Errorf("WarpRemove() unexpected error: %v", err)
			return
		}

		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			t.Errorf("path should not exist after remove, got err: %v", err)
		}
	})

	t.Run("remove non-existent file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "nonexistent.txt")
		err := WarpRemove(filePath)
		if err == nil {
			t.Errorf("WarpRemove() expected error for non-existent file, got nil")
		}
	})

	t.Run("remove empty directory", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "dir_to_remove")
		if err := os.Mkdir(dirPath, 0755); err != nil {
			t.Fatalf("failed to create test directory: %v", err)
		}

		err := WarpRemove(dirPath)
		if err != nil {
			t.Errorf("WarpRemove() unexpected error: %v", err)
		}
	})
}

// TestWarpStat_Integration tests the WarpStat wrapper function.
func TestWarpStat_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("stat short path file", func(t *testing.T) {
		content := "test content"
		filePath := filepath.Join(tmpDir, "stat_file.txt")
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		info, err := WarpStat(filePath)
		if err != nil {
			t.Errorf("WarpStat() unexpected error: %v", err)
			return
		}

		if info.IsDir() {
			t.Errorf("expected file, got directory")
		}
		if info.Size() != int64(len(content)) {
			t.Errorf("file size = %d, want %d", info.Size(), len(content))
		}
	})

	t.Run("stat directory", func(t *testing.T) {
		dirPath := filepath.Join(tmpDir, "stat_dir")
		if err := os.Mkdir(dirPath, 0755); err != nil {
			t.Fatalf("failed to create test directory: %v", err)
		}

		info, err := WarpStat(dirPath)
		if err != nil {
			t.Errorf("WarpStat() unexpected error: %v", err)
			return
		}

		if !info.IsDir() {
			t.Errorf("expected directory, got file")
		}
	})

	t.Run("stat non-existent path", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "nonexistent.txt")
		_, err := WarpStat(filePath)
		if err == nil {
			t.Errorf("WarpStat() expected error for non-existent path, got nil")
		}
	})
}

// TestWarpRename_Integration tests the WarpRename wrapper function.
func TestWarpRename_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("rename short path file", func(t *testing.T) {
		content := "test content"
		oldPath := filepath.Join(tmpDir, "old_name.txt")
		newPath := filepath.Join(tmpDir, "new_name.txt")

		if err := os.WriteFile(oldPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		err := WarpRename(oldPath, newPath)
		if err != nil {
			t.Errorf("WarpRename() unexpected error: %v", err)
			return
		}

		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Errorf("old path should not exist after rename, got err: %v", err)
		}

		newContent, err := os.ReadFile(newPath)
		if err != nil {
			t.Errorf("new path should exist after rename: %v", err)
			return
		}
		if string(newContent) != content {
			t.Errorf("renamed file content = %q, want %q", string(newContent), content)
		}
	})

	t.Run("rename non-existent file", func(t *testing.T) {
		oldPath := filepath.Join(tmpDir, "nonexistent.txt")
		newPath := filepath.Join(tmpDir, "destination.txt")
		err := WarpRename(oldPath, newPath)
		if err == nil {
			t.Errorf("WarpRename() expected error for non-existent file, got nil")
		}
	})
}

// TestWarpFunctions_LongPathScenario tests all wrapper functions in a realistic long path scenario.
// This integration test verifies the complete workflow with long paths on Windows only.
func TestWarpFunctions_LongPathScenario(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("comprehensive long path test only runs on Windows")
	}

	tmpDir := t.TempDir()

	// Create a very long directory structure
	longDirPath := filepath.Join(tmpDir, strings.Repeat("i", 100), strings.Repeat("j", 100))
	t.Logf("Testing with long path (length=%d): %s", len(longDirPath), longDirPath)

	// Test 1: WarpMkdirAll - create long directory structure
	if err := WarpMkdirAll(longDirPath, 0755); err != nil {
		t.Fatalf("WarpMkdirAll() failed: %v", err)
	}

	// Test 2: WarpStat - verify directory exists
	dirInfo, err := WarpStat(longDirPath)
	if err != nil {
		t.Fatalf("WarpStat() on long directory failed: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Errorf("expected directory, got file")
	}

	// Test 3: WarpCreate - create file in long path
	filePath := filepath.Join(longDirPath, "test_file.txt")
	t.Logf("Creating file at long path (length=%d)", len(filePath))

	file, err := WarpCreate(filePath)
	if err != nil {
		t.Fatalf("WarpCreate() failed: %v", err)
	}

	testData := "test data for long path integration"
	if _, err := file.WriteString(testData); err != nil {
		file.Close()
		t.Fatalf("failed to write to file: %v", err)
	}
	file.Close()

	// Test 4: WarpStat - verify file exists
	fileInfo, err := WarpStat(filePath)
	if err != nil {
		t.Fatalf("WarpStat() on long file path failed: %v", err)
	}
	if fileInfo.Size() != int64(len(testData)) {
		t.Errorf("file size = %d, want %d", fileInfo.Size(), len(testData))
	}

	// Test 5: WarpOpen - read from long path
	readFile, err := WarpOpen(filePath)
	if err != nil {
		t.Fatalf("WarpOpen() failed: %v", err)
	}

	content, err := io.ReadAll(readFile)
	readFile.Close()
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != testData {
		t.Errorf("file content = %q, want %q", string(content), testData)
	}

	// Test 6: WarpRename - rename file in long path
	newFilePath := filepath.Join(longDirPath, "renamed_file.txt")
	if err := WarpRename(filePath, newFilePath); err != nil {
		t.Fatalf("WarpRename() failed: %v", err)
	}

	// Verify old path doesn't exist
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Errorf("old file path should not exist after rename")
	}

	// Verify new path exists
	if _, err := WarpStat(newFilePath); err != nil {
		t.Errorf("renamed file should exist: %v", err)
	}

	// Test 7: WarpRemove - remove file from long path
	if err := WarpRemove(newFilePath); err != nil {
		t.Fatalf("WarpRemove() failed: %v", err)
	}

	// Verify file is removed
	if _, err := os.Stat(newFilePath); !os.IsNotExist(err) {
		t.Errorf("file should not exist after remove")
	}

	t.Logf("All long path operations completed successfully")
}
