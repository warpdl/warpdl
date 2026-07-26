// Package credman provides encrypted credential management for HTTP cookies.
// It handles secure storage, retrieval, and persistence of cookies using
// AES-GCM encryption backed by the operating system's keyring.
package credman

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/warpdl/warpdl/pkg/credman/encryption"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

var syncCookieParentDirectory = syncParentDirectory

// cookieStoreCommittedError reports a durability or reopen failure that
// happened after the replacement file became the live cookie store. Callers
// must not roll their in-memory mutation back in this case: disk already
// contains the new snapshot.
type cookieStoreCommittedError struct {
	err error
}

func (e *cookieStoreCommittedError) Error() string {
	return e.err.Error()
}

func (e *cookieStoreCommittedError) Unwrap() error {
	return e.err
}

func cookieStoreCommitSucceeded(err error) bool {
	var committedErr *cookieStoreCommittedError
	return errors.As(err, &committedErr)
}

// CookieManager handles encrypted storage and retrieval of HTTP cookies.
// It persists cookies to a file using GOB encoding, with values encrypted
// using AES-GCM before storage. The manager maintains an in-memory cache
// of cookies for efficient access.
type CookieManager struct {
	f        *os.File
	filePath string
	key      []byte
	cookies  map[string]*types.Cookie
	mu       sync.RWMutex
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
	cm.f, err = os.OpenFile(cm.filePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	if err := cm.f.Chmod(0600); err != nil {
		_ = cm.f.Close()
		cm.f = nil
		return err
	}

	cookiesData, err := io.ReadAll(cm.f)
	if err != nil {
		_ = cm.f.Close()
		cm.f = nil
		return err
	}
	if len(cookiesData) == 0 { // don't decode empty data
		return nil
	}
	buf := bytes.NewBuffer(cookiesData)
	dec := gob.NewDecoder(buf)
	err = dec.Decode(&cm.cookies)

	if err != nil {
		_ = cm.f.Close()
		cm.f = nil
		return err
	}
	return nil
}

func (cm *CookieManager) saveCookies() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.saveCookiesLocked()
}

// saveCookiesLocked atomically replaces the cookie store. The caller must hold
// cm.mu for writing. Failures before replacement leave the old store intact;
// post-replacement durability or reopen failures are marked as committed so
// callers keep memory consistent with the new on-disk snapshot.
func (cm *CookieManager) saveCookiesLocked() error {
	if cm.f == nil {
		return fmt.Errorf("cookie manager is closed")
	}
	if _, err := cm.f.Stat(); err != nil {
		return fmt.Errorf("cookie store is unavailable: %w", err)
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	err := enc.Encode(cm.cookies)
	if err != nil {
		return err
	}

	dir := filepath.Dir(cm.filePath)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(cm.filePath)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Windows cannot replace a file while our old handle is open.
	if err := cm.f.Close(); err != nil {
		return err
	}
	cm.f = nil
	if err := replaceFile(tmpPath, cm.filePath); err != nil {
		cm.f, _ = os.OpenFile(cm.filePath, os.O_RDWR, 0600)
		return err
	}
	cleanup = false

	dirSyncErr := syncCookieParentDirectory(dir)
	f, reopenErr := os.OpenFile(cm.filePath, os.O_RDWR, 0600)
	if reopenErr == nil {
		cm.f = f
	}
	if dirSyncErr != nil || reopenErr != nil {
		var committedErr error
		if dirSyncErr != nil {
			committedErr = errors.Join(
				committedErr,
				fmt.Errorf("sync cookie store directory: %w", dirSyncErr),
			)
		}
		if reopenErr != nil {
			committedErr = errors.Join(
				committedErr,
				fmt.Errorf("reopen cookie store: %w", reopenErr),
			)
		}
		return &cookieStoreCommittedError{err: committedErr}
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
	cm.mu.Lock()
	defer cm.mu.Unlock()
	old, existed := cm.cookies[cookie.Name]
	cm.cookies[cookie.Name] = &cookie
	if err := cm.saveCookiesLocked(); err != nil {
		if !cookieStoreCommitSucceeded(err) {
			if existed {
				cm.cookies[cookie.Name] = old
			} else {
				delete(cm.cookies, cookie.Name)
			}
		}
		return err
	}
	return nil
}

// GetCookie retrieves a cookie by name and returns it with its value
// decrypted. Returns a copy of the cookie to prevent modification of
// the internal state. Returns an error if the cookie does not exist
// or if decryption fails.
func (cm *CookieManager) GetCookie(name string) (*types.Cookie, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
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
	cm.mu.Lock()
	defer cm.mu.Unlock()
	old, ok := cm.cookies[name]
	if !ok {
		return fmt.Errorf("cookie not found: %s", name)
	}
	delete(cm.cookies, name)
	if err := cm.saveCookiesLocked(); err != nil {
		if !cookieStoreCommitSucceeded(err) {
			cm.cookies[name] = old
		}
		return err
	}
	return nil
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
	cm.mu.Lock()
	defer cm.mu.Unlock()
	old, existed := cm.cookies[copyCookie.Name]
	cm.cookies[copyCookie.Name] = &copyCookie
	if err := cm.saveCookiesLocked(); err != nil {
		if !cookieStoreCommitSucceeded(err) {
			if existed {
				cm.cookies[copyCookie.Name] = old
			} else {
				delete(cm.cookies, copyCookie.Name)
			}
		}
		return err
	}
	return nil
}

// Close persists all cookies to disk and closes the underlying file handle.
// This method should be called when the CookieManager is no longer needed
// to ensure all data is saved and resources are released.
func (cm *CookieManager) Close() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.f == nil {
		return nil
	}
	saveErr := cm.saveCookiesLocked()
	var closeErr error
	if cm.f != nil {
		closeErr = cm.f.Close()
		cm.f = nil
	}
	return errors.Join(saveErr, closeErr)
}
