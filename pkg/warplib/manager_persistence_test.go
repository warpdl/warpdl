package warplib

import (
	"encoding/gob"
	"fmt"
	"os"
	"testing"
)

// TestEncodeTruncatesFile verifies that encode truncates the file before writing.
// This prevents leftover bytes from corrupting the state when new data is smaller.
func TestEncodeTruncatesFile(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// Add a large item
	largeItem := &Item{
		Hash:       "large",
		Name:       "large.bin",
		Url:        "http://example.com/large.bin",
		TotalSize:  1000,
		Downloaded: 0,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m.mu,
		memPart:    make(map[string]int64),
	}
	m.UpdateItem(largeItem)

	// Check file size after encoding large item
	stat1, err := m.f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	size1 := stat1.Size()

	// Now add a small item and flush the large one
	m.deleteItem("large")
	smallItem := &Item{
		Hash:       "small",
		Name:       "s.bin",
		Url:        "http://example.com/s.bin",
		TotalSize:  10,
		Downloaded: 0,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m.mu,
		memPart:    make(map[string]int64),
	}
	m.UpdateItem(smallItem)

	// Check file size after encoding small item
	stat2, err := m.f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	size2 := stat2.Size()

	// The file should be smaller now (truncated)
	if size2 >= size1 {
		t.Fatalf("expected file to be truncated, size before: %d, size after: %d", size1, size2)
	}
}

// TestEncodeSyncsFile verifies that encode syncs the file to disk.
func TestEncodeSyncsFile(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	item := &Item{
		Hash:       "sync-test",
		Name:       "sync.bin",
		Url:        "http://example.com/sync.bin",
		TotalSize:  100,
		Downloaded: 0,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m.mu,
		memPart:    make(map[string]int64),
	}

	// UpdateItem should call encode which should sync
	m.UpdateItem(item)

	// Close and reopen to verify data was persisted
	filePath := m.f.Name()
	m.Close()

	// Open the file and decode
	f, err := os.Open(filePath)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()

	var items ItemsMap
	if err := gob.NewDecoder(f).Decode(&items); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if _, found := items["sync-test"]; !found {
		t.Fatalf("item not persisted after sync")
	}
}

// TestDecodeCorruptedFile verifies graceful handling of corrupted files.
func TestDecodeCorruptedFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write corrupted data to the userdata file
	corruptedData := []byte("this is not valid GOB data")
	if err := os.WriteFile(__USERDATA_FILE_NAME, corruptedData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// InitManager should handle the corrupted file gracefully
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	defer m.Close()

	// Should start with empty state
	if len(m.items) != 0 {
		t.Fatalf("expected empty items after loading corrupted file, got %d items", len(m.items))
	}

	// Should be able to add new items
	item := &Item{
		Hash:       "new",
		Name:       "new.bin",
		Url:        "http://example.com/new.bin",
		TotalSize:  100,
		Downloaded: 0,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m.mu,
		memPart:    make(map[string]int64),
	}
	m.UpdateItem(item)

	if m.GetItem("new") == nil {
		t.Fatalf("failed to add item after recovering from corrupted state")
	}
}

// TestDecodeEmptyFile verifies handling of empty files.
func TestDecodeEmptyFile(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create an empty userdata file
	if err := os.WriteFile(__USERDATA_FILE_NAME, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// InitManager should handle the empty file (EOF error is expected and ignored)
	m, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	defer m.Close()

	// Should start with empty state
	if len(m.items) != 0 {
		t.Fatalf("expected empty items after loading empty file, got %d items", len(m.items))
	}
}

// TestConcurrentUpdateItem verifies thread safety of UpdateItem.
func TestConcurrentUpdateItem(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// Create multiple items concurrently
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			item := &Item{
				Hash:       fmt.Sprintf("hash-%d", id),
				Name:       "file.bin",
				Url:        "http://example.com/file.bin",
				TotalSize:  100,
				Downloaded: 0,
				Resumable:  true,
				Parts:      make(map[int64]*ItemPart),
				mu:         m.mu,
				memPart:    make(map[string]int64),
			}
			m.UpdateItem(item)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify all items were added
	if len(m.items) != numGoroutines {
		t.Fatalf("expected %d items, got %d", numGoroutines, len(m.items))
	}
}

// TestFlushTruncatesFile verifies that Flush also truncates the file properly.
func TestFlushTruncatesFile(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	// Add multiple completed items
	for i := 0; i < 10; i++ {
		item := &Item{
			Hash:       fmt.Sprintf("hash-%d", i),
			Name:       "file.bin",
			Url:        "http://example.com/file.bin",
			TotalSize:  100,
			Downloaded: 100, // Completed
			Resumable:  true,
			Parts:      make(map[int64]*ItemPart),
			mu:         m.mu,
			memPart:    make(map[string]int64),
		}
		m.UpdateItem(item)
	}

	// Check file size before flush
	stat1, err := m.f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	size1 := stat1.Size()

	// Flush all items
	m.Flush()

	// Check file size after flush
	stat2, err := m.f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	size2 := stat2.Size()

	// File should be much smaller (nearly empty) after flushing all items
	if size2 >= size1 {
		t.Fatalf("expected file to be truncated after flush, size before: %d, size after: %d", size1, size2)
	}

	// Should have no items
	if len(m.items) != 0 {
		t.Fatalf("expected no items after flush, got %d", len(m.items))
	}
}

// TestPersistenceAcrossRestarts verifies state is preserved across manager restarts.
func TestPersistenceAcrossRestarts(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create first manager and add items
	m1, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}

	item := &Item{
		Hash:       "persistent",
		Name:       "persist.bin",
		Url:        "http://example.com/persist.bin",
		TotalSize:  100,
		Downloaded: 50,
		Resumable:  true,
		Parts:      make(map[int64]*ItemPart),
		mu:         m1.mu,
		memPart:    make(map[string]int64),
	}
	item.Parts[0] = &ItemPart{
		Hash:        "part1",
		FinalOffset: 50,
		Compiled:    false,
	}
	m1.UpdateItem(item)
	m1.Close()

	// Create second manager - should load the persisted state
	m2, err := InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	defer m2.Close()

	// Verify the item was persisted
	loadedItem := m2.GetItem("persistent")
	if loadedItem == nil {
		t.Fatalf("item not found after restart")
	}

	if loadedItem.Name != "persist.bin" {
		t.Fatalf("expected name 'persist.bin', got '%s'", loadedItem.Name)
	}
	if loadedItem.Downloaded != 50 {
		t.Fatalf("expected downloaded 50, got %d", loadedItem.Downloaded)
	}
	if len(loadedItem.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(loadedItem.Parts))
	}
}

// TestEncodeReturnsError verifies that encode properly returns errors.
func TestEncodeReturnsError(t *testing.T) {
	m := newTestManager(t)

	// Close the file to cause errors
	m.f.Close()

	// encode should return an error now
	err := m.encode(m.items)
	if err == nil {
		t.Fatalf("expected error when encoding to closed file")
	}
}
