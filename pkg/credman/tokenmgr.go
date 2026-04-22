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
	return gob.NewDecoder(bytes.NewReader(data)).Decode(&tm.tokens)
}

func (tm *TokenManager) save() error {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(tm.tokens); err != nil {
		return err
	}
	if err := tm.f.Truncate(0); err != nil {
		return err
	}
	if _, err := tm.f.Seek(0, 0); err != nil {
		return err
	}
	_, err := tm.f.Write(buf.Bytes())
	return err
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
	tm.tokens[key] = enc
	return tm.save()
}

// Delete removes a token entry.
func (tm *TokenManager) Delete(key types.TokenKey) error {
	key = key.WithDefaultAccount()
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if _, ok := tm.tokens[key]; !ok {
		return fmt.Errorf("token not found: %s/%s", key.PluginID, key.Account)
	}
	delete(tm.tokens, key)
	return tm.save()
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
	if tm.f == nil {
		return nil
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	saveErr := tm.save()
	closeErr := tm.f.Close()
	tm.f = nil
	if saveErr != nil {
		return saveErr
	}
	return closeErr
}
