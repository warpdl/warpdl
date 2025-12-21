// Package credman provides encrypted credential management for HTTP cookies.
// It handles secure storage, retrieval, and persistence of cookies using
// AES-GCM encryption backed by the operating system's keyring.
package credman

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"os"

	"github.com/warpdl/warpdl/pkg/credman/encryption"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// CookieManager handles encrypted storage and retrieval of HTTP cookies.
// It persists cookies to a file using GOB encoding, with values encrypted
// using AES-GCM before storage. The manager maintains an in-memory cache
// of cookies for efficient access.
type CookieManager struct {
	f        *os.File
	filePath string
	key      []byte
	cookies  map[string]*types.Cookie
}

// NewCookieManager creates a new CookieManager that stores cookies at the
// specified file path, encrypted with the provided key. The key must be
// 32 bytes for AES-256 encryption. If the file exists, existing cookies
// are loaded into memory. Returns an error if the file cannot be opened
// or if existing cookie data is corrupted.
func NewCookieManager(filePath string, key []byte) (*CookieManager, error) {
	cm := &CookieManager{
		filePath: filePath,
		key:      key,
		cookies:  make(map[string]*types.Cookie),
	}

	err := cm.loadCookies()
	if err != nil {
		return nil, err
	}

	return cm, nil
}

func (cm *CookieManager) loadCookies() error {
	var err error
	cm.f, err = os.OpenFile(cm.filePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}

	cookiesData, err := io.ReadAll(cm.f)
	if err != nil {
		return err
	}
	if len(cookiesData) == 0 { // don't decode empty data
		return nil
	}
	buf := bytes.NewBuffer(cookiesData)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&cm.cookies)

	if err != nil {
		return err
	}
	return nil
}

func (cm *CookieManager) saveCookies() error {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(cm.cookies)
	if err != nil {
		return err
	}

	if err := cm.f.Truncate(0); err != nil {
		return err
	}
	if _, err := cm.f.Seek(0, 0); err != nil {
		return err
	}
	_, err = cm.f.Write(buf.Bytes())
	if err != nil {
		return err
	}

	return nil
}

// SetCookie stores a new cookie with its value encrypted. The cookie is
// identified by its Name field. If a cookie with the same name already
// exists, it is overwritten. The encrypted cookie is immediately persisted
// to disk. Returns an error if encryption or persistence fails.
func (cm *CookieManager) SetCookie(cookie types.Cookie) error {
	encryptedValue, err := encryption.EncryptValue(cookie.Value, cm.key)
	if err != nil {
		return err
	}
	cookie.Value = string(encryptedValue)
	cm.cookies[cookie.Name] = &cookie
	return cm.saveCookies()
}

// GetCookie retrieves a cookie by name and returns it with its value
// decrypted. Returns a copy of the cookie to prevent modification of
// the internal state. Returns an error if the cookie does not exist
// or if decryption fails.
func (cm *CookieManager) GetCookie(name string) (*types.Cookie, error) {
	cookie, ok := cm.cookies[name]
	if !ok {
		return nil, fmt.Errorf("cookie not found: %s", name)
	}

	decrpytedValue, err := encryption.DecryptValue([]byte(cookie.Value), cm.key)
	if err != nil {
		return nil, err
	}
	copyCookie := *cookie
	copyCookie.Value = string(decrpytedValue)
	return &copyCookie, nil
}

// DeleteCookie removes a cookie by name from storage. The change is
// immediately persisted to disk. Returns an error if the cookie does
// not exist or if persistence fails.
func (cm *CookieManager) DeleteCookie(name string) error {
	_, ok := cm.cookies[name]
	if !ok {
		return fmt.Errorf("cookie not found: %s", name)
	}
	delete(cm.cookies, name)
	return cm.saveCookies()
}

// UpdateCookie updates an existing cookie with new values. The cookie's
// value is encrypted before storage. Unlike SetCookie, this method accepts
// a pointer and creates an internal copy. Returns an error if the cookie
// pointer is nil, encryption fails, or persistence fails.
func (cm *CookieManager) UpdateCookie(cookie *types.Cookie) error {
	if cookie == nil {
		return fmt.Errorf("cookie is nil")
	}
	copyCookie := *cookie
	encryptedValue, err := encryption.EncryptValue(copyCookie.Value, cm.key)
	if err != nil {
		return err
	}
	copyCookie.Value = string(encryptedValue)
	cm.cookies[copyCookie.Name] = &copyCookie
	return cm.saveCookies()
}

// Close persists all cookies to disk and closes the underlying file handle.
// This method should be called when the CookieManager is no longer needed
// to ensure all data is saved and resources are released.
func (cm *CookieManager) Close() error {
	defer cm.f.Close()
	return cm.saveCookies()
}
