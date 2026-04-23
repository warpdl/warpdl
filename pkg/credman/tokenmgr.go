package credman

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/warpdl/warpdl/pkg/credman/encryption"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// TokenManager handles encrypted storage and retrieval of OAuth 2.0 tokens.
// Sibling of CookieManager: identical persistence shape, different payload type.
// Tokens are GOB-encoded on disk; AccessToken / RefreshToken / IDToken fields
// are AES-GCM encrypted per-entry with a random nonce each save.
type TokenManager struct {
	f        *os.File
	filePath string
	key      []byte
	tokens   map[types.TokenKey]*types.OAuth2Token
	mu       sync.RWMutex
}

// NewTokenManager opens (or creates) `filePath` and decodes any existing
// tokens into memory.
func NewTokenManager(filePath string, key []byte) (*TokenManager, error) {
	tm := &TokenManager{
		filePath: filePath,
		key:      key,
		tokens:   make(map[types.TokenKey]*types.OAuth2Token),
	}
	if err := tm.load(); err != nil {
		return nil, err
	}
	return tm, nil
}

func (tm *TokenManager) load() error {
	var err error
	tm.f, err = os.OpenFile(tm.filePath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(tm.f)
	if err != nil {
		tm.f.Close()
		tm.f = nil
		return err
	}
	if len(data) == 0 {
		return nil
	}
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&tm.tokens); err != nil {
		tm.f.Close()
		tm.f = nil
		return err
	}
	return nil
}

// save writes the map to a sibling temp file, then atomically renames it
// over filePath. The receiver's file handle (tm.f) is updated to point
// at the newly-renamed file on success. On any failure the on-disk state
// and the original handle are untouched — so the caller can roll back
// the in-memory map safely.
func (tm *TokenManager) save() error {
	if tm.f == nil {
		return fmt.Errorf("token manager is closed")
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(tm.tokens); err != nil {
		return err
	}
	tmpPath := tm.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, tm.filePath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	// Close the old handle and reopen at the renamed path so subsequent
	// saves/closes see the new inode on Linux.
	_ = tm.f.Close()
	f, err := os.OpenFile(tm.filePath, os.O_RDWR, 0600)
	if err != nil {
		tm.f = nil
		return err
	}
	tm.f = f
	return nil
}

func (tm *TokenManager) encryptSecrets(t *types.OAuth2Token) (*types.OAuth2Token, error) {
	cp := *t
	if cp.AccessToken != "" {
		b, err := encryption.EncryptValue(cp.AccessToken, tm.key)
		if err != nil {
			return nil, err
		}
		cp.AccessToken = string(b)
	}
	if cp.RefreshToken != "" {
		b, err := encryption.EncryptValue(cp.RefreshToken, tm.key)
		if err != nil {
			return nil, err
		}
		cp.RefreshToken = string(b)
	}
	if cp.IDToken != "" {
		b, err := encryption.EncryptValue(cp.IDToken, tm.key)
		if err != nil {
			return nil, err
		}
		cp.IDToken = string(b)
	}
	return &cp, nil
}

func (tm *TokenManager) decryptSecrets(t *types.OAuth2Token) (*types.OAuth2Token, error) {
	cp := *t
	if cp.AccessToken != "" {
		b, err := encryption.DecryptValue([]byte(cp.AccessToken), tm.key)
		if err != nil {
			return nil, err
		}
		cp.AccessToken = string(b)
	}
	if cp.RefreshToken != "" {
		b, err := encryption.DecryptValue([]byte(cp.RefreshToken), tm.key)
		if err != nil {
			return nil, err
		}
		cp.RefreshToken = string(b)
	}
	if cp.IDToken != "" {
		b, err := encryption.DecryptValue([]byte(cp.IDToken), tm.key)
		if err != nil {
			return nil, err
		}
		cp.IDToken = string(b)
	}
	return &cp, nil
}

// Get returns a decrypted copy of the token for key.
func (tm *TokenManager) Get(key types.TokenKey) (*types.OAuth2Token, error) {
	key = key.WithDefaultAccount()
	tm.mu.RLock()
	raw, ok := tm.tokens[key]
	tm.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("token not found: %s/%s", key.PluginID, key.Account)
	}
	return tm.decryptSecrets(raw)
}

// Set encrypts and stores the token, replacing any existing entry.
// If save() fails, the in-memory map is rolled back so it stays in
// sync with the on-disk state.
func (tm *TokenManager) Set(key types.TokenKey, t *types.OAuth2Token) error {
	if t == nil {
		return fmt.Errorf("token is nil")
	}
	enc, err := tm.encryptSecrets(t)
	if err != nil {
		return err
	}
	key = key.WithDefaultAccount()
	tm.mu.Lock()
	defer tm.mu.Unlock()
	prev, existed := tm.tokens[key]
	tm.tokens[key] = enc
	if err := tm.save(); err != nil {
		if existed {
			tm.tokens[key] = prev
		} else {
			delete(tm.tokens, key)
		}
		return err
	}
	return nil
}

// Delete removes a token entry. If save() fails, the in-memory map is
// rolled back so it stays in sync with the on-disk state.
func (tm *TokenManager) Delete(key types.TokenKey) error {
	key = key.WithDefaultAccount()
	tm.mu.Lock()
	defer tm.mu.Unlock()
	prev, ok := tm.tokens[key]
	if !ok {
		return fmt.Errorf("token not found: %s/%s", key.PluginID, key.Account)
	}
	delete(tm.tokens, key)
	if err := tm.save(); err != nil {
		tm.tokens[key] = prev
		return err
	}
	return nil
}

// List returns all token keys currently stored.
func (tm *TokenManager) List() []types.TokenKey {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make([]types.TokenKey, 0, len(tm.tokens))
	for k := range tm.tokens {
		out = append(out, k)
	}
	return out
}

// Close flushes and closes the underlying file.
func (tm *TokenManager) Close() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if tm.f == nil {
		return nil
	}
	saveErr := tm.save()
	closeErr := tm.f.Close()
	tm.f = nil
	if saveErr != nil {
		return saveErr
	}
	return closeErr
}
