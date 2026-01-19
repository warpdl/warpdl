package keyring

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileKeyStore_SetGetDelete(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	key, err := store.SetKey()
	if err != nil {
		t.Fatalf("SetKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}

	keyPath := filepath.Join(tmpDir, keyFileName)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file not created: %v", err)
	}
	if info.Mode().Perm() != keyFileMode {
		t.Fatalf("expected permissions %o, got %o", keyFileMode, info.Mode().Perm())
	}

	gotKey, err := store.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if !bytes.Equal(key, gotKey) {
		t.Fatalf("roundtrip failed: set %x, got %x", key, gotKey)
	}

	if err := store.DeleteKey(); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("key file should be deleted")
	}
}

func TestFileKeyStore_GetKey_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	_, err := store.GetKey()
	if err == nil {
		t.Fatal("expected error for missing key file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.IsNotExist error, got: %v", err)
	}
}

func TestFileKeyStore_GetKey_InvalidHex(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	keyPath := filepath.Join(tmpDir, keyFileName)
	if err := os.WriteFile(keyPath, []byte("not-valid-hex!"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := store.GetKey()
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestFileKeyStore_GetKey_WrongLength(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	keyPath := filepath.Join(tmpDir, keyFileName)
	if err := os.WriteFile(keyPath, []byte("aabbccdd"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := store.GetKey()
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}

func TestFileKeyStore_SetKey_RandError(t *testing.T) {
	origRandRead := fileRandRead
	defer func() { fileRandRead = origRandRead }()

	fileRandRead = func(b []byte) (int, error) {
		return 0, errors.New("rand fail")
	}

	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	_, err := store.SetKey()
	if err == nil {
		t.Fatal("expected error for rand failure")
	}
}

func TestFileKeyStore_SetKey_MkdirError(t *testing.T) {
	origMkdirAll := fileMkdirAll
	defer func() { fileMkdirAll = origMkdirAll }()

	fileMkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("mkdir fail")
	}

	store := NewFileKeyStore("/nonexistent/path")

	_, err := store.SetKey()
	if err == nil {
		t.Fatal("expected error for mkdir failure")
	}
}

func TestFileKeyStore_SetKey_RenameError(t *testing.T) {
	origRename := fileRename
	defer func() { fileRename = origRename }()

	fileRename = func(oldpath, newpath string) error {
		os.Remove(oldpath)
		return errors.New("rename fail")
	}

	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	_, err := store.SetKey()
	if err == nil {
		t.Fatal("expected error for rename failure")
	}
}

func TestFileKeyStore_DeleteKey_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	err := store.DeleteKey()
	if err == nil {
		t.Fatal("expected error for deleting non-existent key")
	}
}

type mockTempFile struct {
	name        string
	writeErr    error
	closeErr    error
	closeCalled bool
	writeCalled bool
}

func (m *mockTempFile) Name() string { return m.name }
func (m *mockTempFile) WriteString(s string) (int, error) {
	m.writeCalled = true
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(s), nil
}
func (m *mockTempFile) Close() error {
	m.closeCalled = true
	return m.closeErr
}

func TestFileKeyStore_SetKey_TempFileError(t *testing.T) {
	origTempFile := fileTempFile
	defer func() { fileTempFile = origTempFile }()

	fileTempFile = func(dir, pattern string) (TempFile, error) {
		return nil, errors.New("tempfile fail")
	}

	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	_, err := store.SetKey()
	if err == nil {
		t.Fatal("expected error for temp file creation failure")
	}
}

func TestFileKeyStore_SetKey_WriteError(t *testing.T) {
	origTempFile := fileTempFile
	origRemove := fileRemove
	defer func() {
		fileTempFile = origTempFile
		fileRemove = origRemove
	}()

	tmpPath := "/tmp/mock-temp-file"
	var removeCalledWith string
	fileRemove = func(path string) error {
		removeCalledWith = path
		return nil
	}

	fileTempFile = func(dir, pattern string) (TempFile, error) {
		return &mockTempFile{name: tmpPath, writeErr: errors.New("write fail")}, nil
	}

	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	_, err := store.SetKey()
	if err == nil {
		t.Fatal("expected error for write failure")
	}
	if removeCalledWith != tmpPath {
		t.Fatalf("expected cleanup of %s, got %s", tmpPath, removeCalledWith)
	}
}

func TestFileKeyStore_SetKey_CloseError(t *testing.T) {
	origTempFile := fileTempFile
	origRemove := fileRemove
	defer func() {
		fileTempFile = origTempFile
		fileRemove = origRemove
	}()

	tmpPath := "/tmp/mock-temp-file"
	var removeCalledWith string
	fileRemove = func(path string) error {
		removeCalledWith = path
		return nil
	}

	fileTempFile = func(dir, pattern string) (TempFile, error) {
		return &mockTempFile{name: tmpPath, closeErr: errors.New("close fail")}, nil
	}

	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	_, err := store.SetKey()
	if err == nil {
		t.Fatal("expected error for close failure")
	}
	if removeCalledWith != tmpPath {
		t.Fatalf("expected cleanup of %s, got %s", tmpPath, removeCalledWith)
	}
}

func TestFileKeyStore_SetKey_ChmodError(t *testing.T) {
	origChmod := fileChmod
	defer func() { fileChmod = origChmod }()

	fileChmod = func(name string, mode os.FileMode) error {
		return errors.New("chmod fail")
	}

	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	_, err := store.SetKey()
	if err == nil {
		t.Fatal("expected error for chmod failure")
	}
}

func TestFileKeyStore_SetKey_OverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewFileKeyStore(tmpDir)

	key1, err := store.SetKey()
	if err != nil {
		t.Fatalf("first SetKey: %v", err)
	}

	key2, err := store.SetKey()
	if err != nil {
		t.Fatalf("second SetKey: %v", err)
	}

	if bytes.Equal(key1, key2) {
		t.Fatal("second SetKey should generate different key")
	}

	gotKey, err := store.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if !bytes.Equal(key2, gotKey) {
		t.Fatal("GetKey should return second key")
	}
}
