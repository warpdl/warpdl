// Package keyring provides secure key storage using the operating system's
// native keyring service with automatic fallback to file-based storage.
package keyring

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/zalando/go-keyring"
)

// KeyStore defines the interface for key storage backends.
type KeyStore interface {
	GetKey() ([]byte, error)
	SetKey() ([]byte, error)
}

// Logger defines the interface for logging warnings during fallback.
type Logger interface {
	Warning(format string, args ...interface{})
}

// Keyring provides secure storage using the operating system's keyring service.
// It stores and retrieves cryptographic keys identified by application name
// and key field. On macOS it uses Keychain, on Linux it uses Secret Service,
// and on Windows it uses Credential Manager.
type Keyring struct {
	AppName  string
	KeyField string
}

var (
	keyringSet    = keyring.Set
	keyringGet    = keyring.Get
	keyringDelete = keyring.Delete
	randRead      = rand.Read
)

// NewKeyring creates a new Keyring instance configured for the WarpDL
// application. It uses "warpdl" as the application name and "main" as
// the default key field identifier.
func NewKeyring() *Keyring {
	return &Keyring{
		AppName:  "warpdl",
		KeyField: "main",
	}
}

// SetKey generates a new 32-byte (256-bit) cryptographic key using a
// cryptographically secure random number generator, stores it in the
// system keyring as a hex-encoded string, and returns the raw key bytes.
// Returns an error if random generation fails or keyring storage fails.
func (k *Keyring) SetKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := randRead(key); err != nil {
		return nil, err
	}
	keyString := hex.EncodeToString(key)
	err := keyringSet(k.AppName, k.KeyField, keyString)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// GetKey retrieves the stored key from the system keyring. The stored
// hex-encoded string is decoded back to the original 32-byte key.
// Returns an error if the key does not exist, cannot be accessed, or
// if the stored value is not valid hex.
func (k *Keyring) GetKey() ([]byte, error) {
	key, err := keyringGet(k.AppName, k.KeyField)
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(key)
}

// DeleteKey removes the stored key from the system keyring. Returns an
// error if the key does not exist or cannot be deleted.
func (k *Keyring) DeleteKey() error {
	return keyringDelete(k.AppName, k.KeyField)
}

type fallbackKeyStore struct {
	keyring   *Keyring
	fileStore *FileKeyStore
	logger    Logger
}

// NewKeyringWithFallback creates a KeyStore that tries the system keyring first,
// falling back to file-based storage if the keyring is unavailable.
func NewKeyringWithFallback(configDir string, logger Logger) KeyStore {
	return &fallbackKeyStore{
		keyring:   NewKeyring(),
		fileStore: NewFileKeyStore(configDir),
		logger:    logger,
	}
}

func (f *fallbackKeyStore) GetKey() ([]byte, error) {
	key, err := f.keyring.GetKey()
	if err == nil {
		return key, nil
	}

	return f.fileStore.GetKey()
}

func (f *fallbackKeyStore) SetKey() ([]byte, error) {
	key, err := f.keyring.SetKey()
	if err == nil {
		return key, nil
	}

	if f.logger != nil {
		f.logger.Warning("System keyring unavailable, using file-based key storage: %v", err)
	}

	return f.fileStore.SetKey()
}
