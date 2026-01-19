// Package keyring provides secure key storage using the operating system's
// native keyring service with automatic fallback to file-based storage.
package keyring

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const (
	keyFileName = "cookie.key"
	keyFileMode = 0600
)

// FileKeyStore provides file-based key storage as a fallback when the system
// keyring is unavailable. Keys are stored as hex-encoded strings with 0600 permissions.
type FileKeyStore struct {
	configDir string
}

var (
	fileRandRead    = rand.Read
	fileWriteFile   = os.WriteFile
	fileReadFile    = os.ReadFile
	fileRemove      = os.Remove
	fileRename      = os.Rename
	fileMkdirAll    = os.MkdirAll
	fileTempFile    = os.CreateTemp
	fileTempFileDir = ""
)

// NewFileKeyStore creates a new FileKeyStore that stores keys in the specified
// configuration directory. The directory must exist or be creatable.
func NewFileKeyStore(configDir string) *FileKeyStore {
	return &FileKeyStore{
		configDir: configDir,
	}
}

func (f *FileKeyStore) keyPath() string {
	return filepath.Join(f.configDir, keyFileName)
}

// SetKey generates a new 32-byte cryptographic key using a cryptographically
// secure random number generator, stores it in a file as a hex-encoded string,
// and returns the raw key bytes. The file is written atomically using a
// temporary file and rename to prevent corruption. Returns an error if random
// generation fails or file operations fail.
func (f *FileKeyStore) SetKey() ([]byte, error) {
	if err := fileMkdirAll(f.configDir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	key := make([]byte, 32)
	if _, err := fileRandRead(key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	keyHex := hex.EncodeToString(key)

	// Write atomically: create temp file, write, rename
	// This prevents corruption if the process is interrupted
	dir := f.configDir
	if fileTempFileDir != "" {
		dir = fileTempFileDir
	}
	tmpFile, err := fileTempFile(dir, ".cookie.key.tmp.*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(keyHex); err != nil {
		tmpFile.Close()
		fileRemove(tmpPath)
		return nil, fmt.Errorf("write key: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		fileRemove(tmpPath)
		return nil, fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Chmod(tmpPath, keyFileMode); err != nil {
		fileRemove(tmpPath)
		return nil, fmt.Errorf("set permissions: %w", err)
	}

	if err := fileRename(tmpPath, f.keyPath()); err != nil {
		fileRemove(tmpPath)
		return nil, fmt.Errorf("rename key file: %w", err)
	}

	return key, nil
}

// GetKey retrieves the stored key from the file. The stored hex-encoded string
// is decoded back to the original 32-byte key. Returns an error if the file
// does not exist, cannot be read, or if the stored value is not valid hex.
func (f *FileKeyStore) GetKey() ([]byte, error) {
	data, err := fileReadFile(f.keyPath())
	if err != nil {
		return nil, err
	}

	key, err := hex.DecodeString(string(data))
	if err != nil {
		return nil, fmt.Errorf("invalid key format: %w", err)
	}

	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length: expected 32, got %d", len(key))
	}

	return key, nil
}

// DeleteKey removes the key file from the filesystem. Returns an error if the
// file does not exist or cannot be deleted.
func (f *FileKeyStore) DeleteKey() error {
	return fileRemove(f.keyPath())
}
