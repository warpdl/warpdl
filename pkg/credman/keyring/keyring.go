// Package keyring provides secure key storage using the operating system's
// native keyring service. It wraps the go-keyring library to manage
// cryptographic keys for the WarpDL application.
package keyring

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/zalando/go-keyring"
)

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

// GetKey retrieves the stored key from the system keyring. Note that the
// returned bytes are the raw string representation from the keyring, not
// hex-decoded. Returns an error if the key does not exist or cannot be
// accessed.
func (k *Keyring) GetKey() ([]byte, error) {
	key, err := keyringGet(k.AppName, k.KeyField)
	if err != nil {
		return nil, err
	}
	return []byte(key), nil
}

// DeleteKey removes the stored key from the system keyring. Returns an
// error if the key does not exist or cannot be deleted.
func (k *Keyring) DeleteKey() error {
	return keyringDelete(k.AppName, k.KeyField)
}
