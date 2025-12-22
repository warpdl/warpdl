package warplib

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func newTestItem(t *testing.T, hash string) *Item {
	t.Helper()
	return &Item{
		Hash:             hash,
		Name:             "file.bin",
		Url:              "http://example.com/file.bin",
		TotalSize:        100,
		Downloaded:       0,
		DownloadLocation: t.TempDir(),
		AbsoluteLocation: t.TempDir(),
		Resumable:        true,
		Parts:            make(map[int64]*ItemPart),
		mu:               &sync.RWMutex{},
		memPart:          make(map[string]int64),
	}
}

func TestValidateDownloadIntegrity_MissingDlDataDir(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "missing-dir")
	// Do not create the download data directory

	err := validateDownloadIntegrity(item)
	if err == nil {
		t.Fatal("expected error for missing dldata directory")
	}
	if !errors.Is(err, ErrDownloadDataMissing) {
		t.Fatalf("expected ErrDownloadDataMissing, got %v", err)
	}
}

func TestValidateDownloadIntegrity_MissingPartFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "missing-part")
	// Create the download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Add a non-compiled part (file doesn't exist)
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 50,
		Compiled:    false,
	}

	err := validateDownloadIntegrity(item)
	if err == nil {
		t.Fatal("expected error for missing part file")
	}
	if !errors.Is(err, ErrDownloadDataMissing) {
		t.Fatalf("expected ErrDownloadDataMissing, got %v", err)
	}
}

func TestValidateDownloadIntegrity_MissingMainFile_CompiledPart(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "missing-main")
	// Create the download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Add a compiled part (main file should exist but doesn't)
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 50,
		Compiled:    true,
	}

	err := validateDownloadIntegrity(item)
	if err == nil {
		t.Fatal("expected error for missing main file with compiled part")
	}
	if !errors.Is(err, ErrDownloadDataMissing) {
		t.Fatalf("expected ErrDownloadDataMissing, got %v", err)
	}
}

func TestValidateDownloadIntegrity_MissingMainFile_Downloaded(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "missing-main-downloaded")
	item.Downloaded = 50 // Has progress

	// Create the download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err := validateDownloadIntegrity(item)
	if err == nil {
		t.Fatal("expected error for missing main file with Downloaded > 0")
	}
	if !errors.Is(err, ErrDownloadDataMissing) {
		t.Fatalf("expected ErrDownloadDataMissing, got %v", err)
	}
}

func TestValidateDownloadIntegrity_EmptyMainFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "empty-main")
	// Create the download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Add a compiled part
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 50,
		Compiled:    true,
	}

	// Create an empty main file
	mainFile := item.GetAbsolutePath()
	f, err := os.Create(mainFile)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()

	err = validateDownloadIntegrity(item)
	if err == nil {
		t.Fatal("expected error for empty main file")
	}
	if !errors.Is(err, ErrDownloadDataMissing) {
		t.Fatalf("expected ErrDownloadDataMissing, got %v", err)
	}
}

func TestValidateDownloadIntegrity_ValidState_NoDownloaded(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "valid-no-download")
	item.Downloaded = 0

	// Create the download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Add a non-compiled part with existing file
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 50,
		Compiled:    false,
	}
	partFile := getFileName(dlPath+"/", "part1")
	if err := os.WriteFile(partFile, []byte("test data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := validateDownloadIntegrity(item)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateDownloadIntegrity_ValidState_WithMainFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "valid-with-main")
	item.Downloaded = 50

	// Create the download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Add a non-compiled part with existing file
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 100,
		Compiled:    false,
	}
	partFile := getFileName(dlPath+"/", "part1")
	if err := os.WriteFile(partFile, []byte("test data"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Create main file with content
	mainFile := item.GetAbsolutePath()
	if err := os.WriteFile(mainFile, []byte("partial download content"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := validateDownloadIntegrity(item)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateDownloadIntegrity_ValidState_CompiledWithMainFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	item := newTestItem(t, "valid-compiled")
	item.Downloaded = 0 // No Download counter but part is compiled

	// Create the download data directory
	dlPath := filepath.Join(DlDataDir, item.Hash)
	if err := os.MkdirAll(dlPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Add a compiled part (no part file needed)
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 50,
		Compiled:    true,
	}

	// Create main file with content
	mainFile := item.GetAbsolutePath()
	if err := os.WriteFile(mainFile, []byte("compiled content"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := validateDownloadIntegrity(item)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestFileExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Test non-existent file
	if fileExists(filepath.Join(tmpDir, "nonexistent")) {
		t.Fatal("expected fileExists to return false for non-existent file")
	}

	// Test existing file
	existingFile := filepath.Join(tmpDir, "exists.txt")
	if err := os.WriteFile(existingFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if !fileExists(existingFile) {
		t.Fatal("expected fileExists to return true for existing file")
	}

	// Test directory (should return false)
	if fileExists(tmpDir) {
		t.Fatal("expected fileExists to return false for directory")
	}
}
