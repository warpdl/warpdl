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

var syncTokenParentDirectory = syncParentDirectory

// tokenStoreCommittedError reports a durability or reopen failure that
// happened after the replacement file became the live token store. Callers
// must not roll their in-memory mutation back in this case: disk already
// contains the new snapshot.
type tokenStoreCommittedError struct {
	err error
}

func (e *tokenStoreCommittedError) Error() string {
	return e.err.Error()
}

func (e *tokenStoreCommittedError) Unwrap() error {
	return e.err
}

func tokenStoreCommitSucceeded(err error) bool {
	var committedErr *tokenStoreCommittedError
	return errors.As(err, &committedErr)
}

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
// at the newly-renamed file on success. Failures before replacement leave
// the old store intact; post-replacement durability or reopen failures are
// marked as committed so callers keep memory consistent with the new
// on-disk snapshot.
func (tm *TokenManager) save() error {
	if tm.f == nil {
		return fmt.Errorf("token manager is closed")
	}
	if _, err := tm.f.Stat(); err != nil {
		return fmt.Errorf("token store is unavailable: %w", err)
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(tm.tokens); err != nil {
		return err
	}
	dir := filepath.Dir(tm.filePath)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(tm.filePath)+".tmp-*")
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
	if err := tmp.Chmod(0o600); err != nil {
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
	if err := tm.f.Close(); err != nil {
		return err
	}
	tm.f = nil
	if err := replaceFile(tmpPath, tm.filePath); err != nil {
		tm.f, _ = os.OpenFile(tm.filePath, os.O_RDWR, 0o600)
		return err
	}
	cleanup = false
	dirSyncErr := syncTokenParentDirectory(dir)
	f, reopenErr := os.OpenFile(tm.filePath, os.O_RDWR, 0o600)
	if reopenErr == nil {
		tm.f = f
	}
	if dirSyncErr != nil || reopenErr != nil {
		var committedErr error
		if dirSyncErr != nil {
			committedErr = errors.Join(
				committedErr,
				fmt.Errorf("sync token store directory: %w", dirSyncErr),
			)
		}
		if reopenErr != nil {
			committedErr = errors.Join(
				committedErr,
				fmt.Errorf("reopen token store: %w", reopenErr),
			)
		}
		return &tokenStoreCommittedError{err: committedErr}
	}
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
// If save() fails before the replacement commits, the in-memory map is
// rolled back so it stays in sync with the on-disk state. A committed
// error keeps the new entry: disk already contains the new snapshot.
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
		if !tokenStoreCommitSucceeded(err) {
			if existed {
				tm.tokens[key] = prev
			} else {
				delete(tm.tokens, key)
			}
		}
		return err
	}
	return nil
}

// Delete removes a token entry. If save() fails before the replacement
// commits, the in-memory entry is restored. A committed error keeps the
// deletion: disk already contains the new snapshot.
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
		if !tokenStoreCommitSucceeded(err) {
			tm.tokens[key] = prev
		}
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
	var closeErr error
	if tm.f != nil {
		closeErr = tm.f.Close()
		tm.f = nil
	}
	return errors.Join(saveErr, closeErr)
}
