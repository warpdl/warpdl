package keyring

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestKeyringSetGetDelete(t *testing.T) {
	origSet := keyringSet
	origGet := keyringGet
	origDelete := keyringDelete
	origRandRead := randRead
	defer func() {
		keyringSet = origSet
		keyringGet = origGet
		keyringDelete = origDelete
		randRead = origRandRead
	}()

	var setApp, setKey, setValue string
	keyringSet = func(app, key, value string) error {
		setApp = app
		setKey = key
		setValue = value
		return nil
	}
	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0x01
		}
		return len(b), nil
	}

	kr := NewKeyring()
	key, err := kr.SetKey()
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
	expectedHex := hex.EncodeToString(key)
	if setApp != kr.AppName || setKey != kr.KeyField || setValue != expectedHex {
		t.Fatalf("unexpected set call: %q %q %q", setApp, setKey, setValue)
	}

	// GetKey now expects hex-encoded string, so return valid hex
	testBytes := []byte{0xaa, 0xbb, 0xcc, 0xdd}
	keyringGet = func(app, key string) (string, error) {
		if app != kr.AppName || key != kr.KeyField {
			return "", errors.New("unexpected key")
		}
		return hex.EncodeToString(testBytes), nil
	}
	got, err := kr.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if !bytes.Equal(got, testBytes) {
		t.Fatalf("unexpected key value: got %x, want %x", got, testBytes)
	}

	var deleteApp, deleteKey string
	keyringDelete = func(app, key string) error {
		deleteApp = app
		deleteKey = key
		return nil
	}
	if err := kr.DeleteKey(); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if deleteApp != kr.AppName || deleteKey != kr.KeyField {
		t.Fatalf("unexpected delete call: %q %q", deleteApp, deleteKey)
	}
}

func TestKeyringSetError(t *testing.T) {
	origSet := keyringSet
	origRandRead := randRead
	defer func() {
		keyringSet = origSet
		randRead = origRandRead
	}()

	randRead = func(b []byte) (int, error) { return 0, errors.New("rand fail") }
	kr := NewKeyring()
	if _, err := kr.SetKey(); err == nil {
		t.Fatalf("expected rand error")
	}

	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0x02
		}
		return len(b), nil
	}
	keyringSet = func(string, string, string) error { return errors.New("set fail") }
	if _, err := kr.SetKey(); err == nil {
		t.Fatalf("expected set error")
	}
}

func TestKeyringGetError(t *testing.T) {
	origGet := keyringGet
	defer func() { keyringGet = origGet }()

	keyringGet = func(string, string) (string, error) {
		return "", errors.New("get fail")
	}
	kr := NewKeyring()
	if _, err := kr.GetKey(); err == nil {
		t.Fatalf("expected get error")
	}
}

func TestSetKeyGetKeyRoundtrip(t *testing.T) {
	// Save and restore original functions
	origSet := keyringSet
	origGet := keyringGet
	origRandRead := randRead
	defer func() {
		keyringSet = origSet
		keyringGet = origGet
		randRead = origRandRead
	}()

	// Connected mock storage (SetKey stores, GetKey retrieves same value)
	var storedValue string
	keyringSet = func(app, key, value string) error {
		storedValue = value
		return nil
	}
	keyringGet = func(app, key string) (string, error) {
		return storedValue, nil
	}
	// Deterministic random for reproducibility
	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = byte(i)
		}
		return len(b), nil
	}

	kr := NewKeyring()

	// SetKey returns 32 bytes
	setBytes, err := kr.SetKey()
	if err != nil {
		t.Fatalf("SetKey failed: %v", err)
	}
	if len(setBytes) != 32 {
		t.Fatalf("SetKey should return 32 bytes, got %d", len(setBytes))
	}

	// GetKey should return the SAME 32 bytes
	getBytes, err := kr.GetKey()
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}

	// Core assertions
	if len(getBytes) != 32 {
		t.Fatalf("GetKey should return 32 bytes, got %d", len(getBytes))
	}
	if !bytes.Equal(setBytes, getBytes) {
		t.Fatalf("roundtrip failed: SetKey returned %x, GetKey returned %x", setBytes, getBytes)
	}
}

func TestGetKeyInvalidHex(t *testing.T) {
	origGet := keyringGet
	defer func() { keyringGet = origGet }()

	keyringGet = func(app, key string) (string, error) {
		return "not-valid-hex!", nil
	}

	kr := NewKeyring()
	_, err := kr.GetKey()
	if err == nil {
		t.Fatal("expected error for invalid hex string")
	}
}

type mockLogger struct {
	warnings []string
}

func (m *mockLogger) Warning(format string, args ...interface{}) {
	m.warnings = append(m.warnings, format)
}

func TestNewKeyringWithFallback(t *testing.T) {
	tmpDir := t.TempDir()
	logger := &mockLogger{}

	ks := NewKeyringWithFallback(tmpDir, logger)
	if ks == nil {
		t.Fatal("expected non-nil KeyStore")
	}
}

func TestFallbackKeyStore_GetKey_KeyringSuccess(t *testing.T) {
	origGet := keyringGet
	defer func() { keyringGet = origGet }()

	expectedKey := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
		0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20}
	keyringGet = func(app, key string) (string, error) {
		return hex.EncodeToString(expectedKey), nil
	}

	tmpDir := t.TempDir()
	ks := NewKeyringWithFallback(tmpDir, nil)

	key, err := ks.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if !bytes.Equal(key, expectedKey) {
		t.Fatalf("got %x, want %x", key, expectedKey)
	}
}

func TestFallbackKeyStore_GetKey_FallsBackToFile(t *testing.T) {
	origGet := keyringGet
	defer func() { keyringGet = origGet }()

	keyringGet = func(app, key string) (string, error) {
		return "", errors.New("keyring unavailable")
	}

	tmpDir := t.TempDir()
	fileStore := NewFileKeyStore(tmpDir)
	expectedKey, err := fileStore.SetKey()
	if err != nil {
		t.Fatalf("setup file key: %v", err)
	}

	ks := NewKeyringWithFallback(tmpDir, nil)

	key, err := ks.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if !bytes.Equal(key, expectedKey) {
		t.Fatalf("got %x, want %x", key, expectedKey)
	}
}

func TestFallbackKeyStore_SetKey_KeyringSuccess(t *testing.T) {
	origSet := keyringSet
	origRandRead := randRead
	defer func() {
		keyringSet = origSet
		randRead = origRandRead
	}()

	var storedValue string
	keyringSet = func(app, keyField, value string) error {
		storedValue = value
		return nil
	}
	randRead = func(b []byte) (int, error) {
		for i := range b {
			b[i] = byte(i)
		}
		return len(b), nil
	}

	tmpDir := t.TempDir()
	logger := &mockLogger{}
	ks := NewKeyringWithFallback(tmpDir, logger)

	key, err := ks.SetKey()
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
	if storedValue != hex.EncodeToString(key) {
		t.Fatal("key not stored in keyring")
	}
	if len(logger.warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", logger.warnings)
	}
}

func TestFallbackKeyStore_SetKey_FallsBackToFile(t *testing.T) {
	origSet := keyringSet
	defer func() { keyringSet = origSet }()

	keyringSet = func(app, keyField, value string) error {
		return errors.New("keyring unavailable")
	}

	tmpDir := t.TempDir()
	logger := &mockLogger{}
	ks := NewKeyringWithFallback(tmpDir, logger)

	key, err := ks.SetKey()
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
	if len(logger.warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(logger.warnings))
	}

	fileStore := NewFileKeyStore(tmpDir)
	fileKey, err := fileStore.GetKey()
	if err != nil {
		t.Fatalf("file key not set: %v", err)
	}
	if !bytes.Equal(key, fileKey) {
		t.Fatal("returned key doesn't match file key")
	}
}

func TestFallbackKeyStore_SetKey_NilLogger(t *testing.T) {
	origSet := keyringSet
	defer func() { keyringSet = origSet }()

	keyringSet = func(app, keyField, value string) error {
		return errors.New("keyring unavailable")
	}

	tmpDir := t.TempDir()
	ks := NewKeyringWithFallback(tmpDir, nil)

	key, err := ks.SetKey()
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
}
