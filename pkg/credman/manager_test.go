package credman

import (
	"crypto/rand"
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
