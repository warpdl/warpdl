package credman

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

func newTestManager(t *testing.T) (*CookieManager, []byte, func()) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	filePath := filepath.Join(t.TempDir(), "cookies.warp")
	cm, err := NewCookieManager(filePath, key)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}
	return cm, key, func() {
		_ = cm.Close()
	}
}

func testCookie() types.Cookie {
	return types.Cookie{
		Name:     "test_cookie",
		Value:    "test_value",
		Domain:   "example.com",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
	}
}

func TestCookieManagerCRUD(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	c := testCookie()
	if err := cm.SetCookie(c); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	got, err := cm.GetCookie(c.Name)
	if err != nil {
		t.Fatalf("GetCookie: %v", err)
	}
	compareCookies(t, &c, got)

	updated := c
	updated.Value = "updated_value"
	if err := cm.UpdateCookie(&updated); err != nil {
		t.Fatalf("UpdateCookie: %v", err)
	}
	got, err = cm.GetCookie(c.Name)
	if err != nil {
		t.Fatalf("GetCookie after update: %v", err)
	}
	compareCookies(t, &updated, got)

	if err := cm.DeleteCookie(c.Name); err != nil {
		t.Fatalf("DeleteCookie: %v", err)
	}
	if _, err := cm.GetCookie(c.Name); err == nil {
		t.Fatalf("expected error for deleted cookie")
	}
}

func TestCookieManagerPersistence(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	path := filepath.Join(dir, "cookies.warp")
	cm, err := NewCookieManager(path, key)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}
	c := testCookie()
	if err := cm.SetCookie(c); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cm, err = NewCookieManager(path, key)
	if err != nil {
		t.Fatalf("NewCookieManager reload: %v", err)
	}
	defer cm.Close()
	got, err := cm.GetCookie(c.Name)
	if err != nil {
		t.Fatalf("GetCookie after reload: %v", err)
	}
	compareCookies(t, &c, got)
}

func TestCookieManagerDirectorySyncFailureKeepsCommittedState(t *testing.T) {
	dir := t.TempDir()
	key := bytes.Repeat([]byte{0x37}, 32)
	path := filepath.Join(dir, "cookies.warp")
	cm, err := NewCookieManager(path, key)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}

	originalSync := syncCookieParentDirectory
	t.Cleanup(func() {
		syncCookieParentDirectory = originalSync
	})
	syncFailure := errors.New("simulated directory sync failure")
	syncCookieParentDirectory = func(string) error {
		return syncFailure
	}

	cookie := types.Cookie{Name: "session", Value: "committed-secret"}
	err = cm.SetCookie(cookie)
	if !errors.Is(err, syncFailure) {
		t.Fatalf("SetCookie error = %v, want directory sync failure", err)
	}
	if !cookieStoreCommitSucceeded(err) {
		t.Fatalf("SetCookie error does not report a committed replacement: %v", err)
	}

	got, err := cm.GetCookie(cookie.Name)
	if err != nil {
		t.Fatalf("GetCookie after committed error: %v", err)
	}
	compareCookies(t, &cookie, got)

	// Restore normal syncing before Close writes another snapshot. A fresh
	// manager must observe the state that was already renamed into place.
	syncCookieParentDirectory = originalSync
	if err := cm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := NewCookieManager(path, key)
	if err != nil {
		t.Fatalf("NewCookieManager after committed error: %v", err)
	}
	defer reopened.Close()
	got, err = reopened.GetCookie(cookie.Name)
	if err != nil {
		t.Fatalf("GetCookie after reload: %v", err)
	}
	compareCookies(t, &cookie, got)
}

func TestCookieManagerMutationsRollBackBeforeCommit(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	original := types.Cookie{Name: "session", Value: "original-secret"}
	if err := cm.SetCookie(original); err != nil {
		t.Fatalf("seed cookie: %v", err)
	}

	// A missing live handle makes saveCookiesLocked fail before the atomic
	// rename. Every mutation must therefore restore the prior in-memory
	// state so it remains consistent with the unchanged on-disk snapshot.
	if err := cm.f.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	cm.f = nil

	overwrite := original
	overwrite.Value = "uncommitted-overwrite"
	if err := cm.SetCookie(overwrite); err == nil {
		t.Fatal("SetCookie overwrite unexpectedly succeeded")
	}
	got, err := cm.GetCookie(original.Name)
	if err != nil {
		t.Fatalf("GetCookie after failed overwrite: %v", err)
	}
	compareCookies(t, &original, got)

	added := types.Cookie{Name: "new-session", Value: "uncommitted-add"}
	if err := cm.SetCookie(added); err == nil {
		t.Fatal("SetCookie add unexpectedly succeeded")
	}
	if _, err := cm.GetCookie(added.Name); err == nil {
		t.Fatal("failed SetCookie left a new in-memory cookie")
	}

	if err := cm.DeleteCookie(original.Name); err == nil {
		t.Fatal("DeleteCookie unexpectedly succeeded")
	}
	got, err = cm.GetCookie(original.Name)
	if err != nil {
		t.Fatalf("GetCookie after failed delete: %v", err)
	}
	compareCookies(t, &original, got)

	update := original
	update.Value = "uncommitted-update"
	if err := cm.UpdateCookie(&update); err == nil {
		t.Fatal("UpdateCookie existing entry unexpectedly succeeded")
	}
	got, err = cm.GetCookie(original.Name)
	if err != nil {
		t.Fatalf("GetCookie after failed update: %v", err)
	}
	compareCookies(t, &original, got)

	newUpdate := types.Cookie{Name: "new-update", Value: "uncommitted-update"}
	if err := cm.UpdateCookie(&newUpdate); err == nil {
		t.Fatal("UpdateCookie new entry unexpectedly succeeded")
	}
	if _, err := cm.GetCookie(newUpdate.Name); err == nil {
		t.Fatal("failed UpdateCookie left a new in-memory cookie")
	}
}

func TestCookieManagerUsesPrivateFileMode(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	path := filepath.Join(t.TempDir(), "cookies.warp")

	cm, err := NewCookieManager(path, key)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}
	if err := cm.SetCookie(types.Cookie{Name: "session", Value: "secret"}); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("cookie store mode = %04o, want 0600", got)
	}

	// Existing stores created by older versions are tightened on open.
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	cm, err = NewCookieManager(path, key)
	if err != nil {
		t.Fatalf("reopen CookieManager: %v", err)
	}
	defer cm.Close()
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("Stat reopened store: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("reopened cookie store mode = %04o, want 0600", got)
	}
}

func TestCookieManagerGetDoesNotMutate(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	c := testCookie()
	if err := cm.SetCookie(c); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	if _, err := cm.GetCookie(c.Name); err != nil {
		t.Fatalf("GetCookie: %v", err)
	}
	if _, err := cm.GetCookie(c.Name); err != nil {
		t.Fatalf("GetCookie second time: %v", err)
	}
}

func TestCookieManagerWrongKey(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	path := filepath.Join(dir, "cookies.warp")
	cm, err := NewCookieManager(path, key)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}
	c := testCookie()
	if err := cm.SetCookie(c); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}
	if err := cm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	badKey := make([]byte, 32)
	if _, err := rand.Read(badKey); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	cm, err = NewCookieManager(path, badKey)
	if err != nil {
		t.Fatalf("NewCookieManager with bad key: %v", err)
	}
	defer cm.Close()
	if _, err := cm.GetCookie(c.Name); err == nil {
		t.Fatalf("expected decrypt error with wrong key")
	}
}

func TestCookieManagerUpdateNil(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	if err := cm.UpdateCookie(nil); err == nil {
		t.Fatalf("expected error for nil cookie")
	}
}

func TestCookieManagerDeleteNonExistent(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	err := cm.DeleteCookie("nonexistent_cookie")
	if err == nil {
		t.Fatalf("expected error for deleting non-existent cookie")
	}
}

func TestCookieManagerInvalidFilePath(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	// Try to open a file in a non-existent directory
	invalidPath := filepath.Join(t.TempDir(), "nonexistent", "subdir", "cookies.warp")
	_, err := NewCookieManager(invalidPath, key)
	if err == nil {
		t.Fatalf("expected error for invalid file path")
	}
}

func TestCookieManagerCorruptData(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	path := filepath.Join(dir, "cookies.warp")

	// Write corrupt/invalid GOB data to the file
	if err := os.WriteFile(path, []byte("not valid gob data"), 0666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Attempt to load should fail due to invalid GOB decoding
	_, err := NewCookieManager(path, key)
	if err == nil {
		t.Fatalf("expected error for corrupt data")
	}
}

func TestCookieManagerSetCookieInvalidKey(t *testing.T) {
	dir := t.TempDir()
	// Invalid key length (should be 16, 24, or 32 bytes for AES)
	invalidKey := make([]byte, 10)
	if _, err := rand.Read(invalidKey); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	path := filepath.Join(dir, "cookies.warp")

	cm, err := NewCookieManager(path, invalidKey)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}
	defer cm.Close()

	c := testCookie()
	err = cm.SetCookie(c)
	if err == nil {
		t.Fatalf("expected encryption error for invalid key")
	}
}

func TestCookieManagerUpdateCookieInvalidKey(t *testing.T) {
	dir := t.TempDir()
	// Invalid key length (should be 16, 24, or 32 bytes for AES)
	invalidKey := make([]byte, 10)
	if _, err := rand.Read(invalidKey); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	path := filepath.Join(dir, "cookies.warp")

	cm, err := NewCookieManager(path, invalidKey)
	if err != nil {
		t.Fatalf("NewCookieManager: %v", err)
	}
	defer cm.Close()

	c := testCookie()
	err = cm.UpdateCookie(&c)
	if err == nil {
		t.Fatalf("expected encryption error for invalid key")
	}
}

func TestCookieManagerMultipleCookies(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	cookies := []types.Cookie{
		{Name: "cookie1", Value: "value1", Domain: "example.com"},
		{Name: "cookie2", Value: "value2", Domain: "example.org"},
		{Name: "cookie3", Value: "value3", Domain: "example.net"},
	}

	for _, c := range cookies {
		if err := cm.SetCookie(c); err != nil {
			t.Fatalf("SetCookie %s: %v", c.Name, err)
		}
	}

	for _, c := range cookies {
		got, err := cm.GetCookie(c.Name)
		if err != nil {
			t.Fatalf("GetCookie %s: %v", c.Name, err)
		}
		if got.Value != c.Value {
			t.Fatalf("expected value %s, got %s", c.Value, got.Value)
		}
	}

	// Delete middle cookie and verify others still exist
	if err := cm.DeleteCookie("cookie2"); err != nil {
		t.Fatalf("DeleteCookie: %v", err)
	}

	if _, err := cm.GetCookie("cookie2"); err == nil {
		t.Fatalf("expected error for deleted cookie2")
	}

	// Verify cookie1 and cookie3 still exist
	if _, err := cm.GetCookie("cookie1"); err != nil {
		t.Fatalf("GetCookie cookie1 after delete: %v", err)
	}
	if _, err := cm.GetCookie("cookie3"); err != nil {
		t.Fatalf("GetCookie cookie3 after delete: %v", err)
	}
}

func TestCookieManagerOverwriteCookie(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	c := testCookie()
	if err := cm.SetCookie(c); err != nil {
		t.Fatalf("SetCookie: %v", err)
	}

	// Overwrite with same name but different value
	c.Value = "new_value"
	if err := cm.SetCookie(c); err != nil {
		t.Fatalf("SetCookie overwrite: %v", err)
	}

	got, err := cm.GetCookie(c.Name)
	if err != nil {
		t.Fatalf("GetCookie: %v", err)
	}
	if got.Value != "new_value" {
		t.Fatalf("expected value new_value, got %s", got.Value)
	}
}

func TestCookieManagerSaveCookiesClosedFile(t *testing.T) {
	cm, _, cleanup := newTestManager(t)
	defer cleanup()

	if err := cm.f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := cm.saveCookies(); err == nil {
		t.Fatalf("expected error for closed file")
	}
}

// TestCookieManagerLoadErrorClosesFile verifies that file handles are properly
// closed when loadCookies encounters an error. This test will FAIL initially
// because the current implementation leaks file handles on error paths.
func TestCookieManagerLoadErrorClosesFile(t *testing.T) {
	dir := t.TempDir()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	path := filepath.Join(dir, "cookies.warp")

	// Write corrupt GOB data that will trigger decode error at line 64
	corruptData := []byte("this is not valid GOB encoded data")
	if err := os.WriteFile(path, corruptData, 0666); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Attempt to create CookieManager - should fail during loadCookies()
	_, err := NewCookieManager(path, key)
	if err == nil {
		t.Fatalf("expected error for corrupt GOB data")
	}

	// Verify file handle was released by attempting to remove the file.
	// On Windows, this fails if the file handle is still open.
	if err := os.Remove(path); err != nil {
		t.Errorf("failed to remove file after error - file handle leaked: %v", err)
	}
}

// TestCookieManagerCloseNilFile verifies that calling Close() on a
// CookieManager with a nil file pointer does not panic. This can occur
// if the struct is created manually or if initialization fails partway.
// This test will FAIL initially because Close() unconditionally dereferences cm.f.
func TestCookieManagerCloseNilFile(t *testing.T) {
	// Create CookieManager with nil file pointer to simulate partial initialization
	cm := &CookieManager{
		filePath: "/tmp/test.warp",
		key:      make([]byte, 32),
		cookies:  make(map[string]*types.Cookie),
		f:        nil, // Explicitly nil
	}

	// This should not panic - Close() should handle nil gracefully
	// Using defer/recover to catch panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Close() panicked with nil file: %v", r)
		}
	}()

	err := cm.Close()
	// We expect an error (can't close nil file), but NOT a panic
	if err == nil {
		t.Log("Close returned nil error for nil file - acceptable if defensive")
	}
}

func compareCookies(t *testing.T, expected *types.Cookie, actual *types.Cookie) {
	t.Helper()
	expectedValue := reflect.ValueOf(expected).Elem()
	actualValue := reflect.ValueOf(actual).Elem()
	timeType := reflect.TypeOf(time.Time{})

	for i := 0; i < expectedValue.NumField(); i++ {
		expectedField := expectedValue.Field(i)
		actualField := actualValue.Field(i)

		if expectedField.Type() == timeType {
			exp := expectedField.Interface().(time.Time)
			act := actualField.Interface().(time.Time)
			if !exp.Equal(act) {
				t.Errorf("Expected %s %v, got %v", expectedValue.Type().Field(i).Name, exp, act)
			}
			continue
		}
		if !reflect.DeepEqual(expectedField.Interface(), actualField.Interface()) {
			t.Errorf("Expected %s %v, got %v", expectedValue.Type().Field(i).Name, expectedField.Interface(), actualField.Interface())
		}
	}
}
