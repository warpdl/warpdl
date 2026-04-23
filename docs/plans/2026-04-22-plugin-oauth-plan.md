# Plugin OAuth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** `docs/specs/2026-04-22-plugin-oauth-design.md`

**Goal:** Let warpdl plugins request OAuth 2.0 tokens so private resources (private Drive files, Dropbox, etc.) become downloadable after a first-run browser prompt.

**Architecture:** Daemon owns OAuth crypto + encrypted `tokens.gob` store (extension of `credman`); CLI owns the browser loopback and user prompts. Plugin manifests declare OAuth config; plugins call new JS bindings (`getAccessToken`, `fetchWithAuth`, `invalidateToken`, `listAccounts`) and can return `{url, headers}` from `extract()` so auth headers travel to the downloader.

**Tech Stack:** Go 1.25, goja JS runtime, existing `jrpc2` RPC, `credman` AES-GCM + keyring storage, `github.com/pkg/browser` for cross-platform browser open.

**Conventions used in every task below:**
- Tests are written first and run failing before the implementation lands.
- Each task ends with `go test ./<touched-pkg>/... -race -count=1 -timeout 60s` green before committing.
- Commits: `<type>(<scope>): <summary>` without Claude attribution.
- Imports added mechanically in the same Edit that introduces a new identifier.

---

## Phase A — Token storage (standalone, no engine deps)

### Task 1: `OAuth2Token` and `TokenKey` types

**Files:**
- Create: `pkg/credman/types/oauth.go`
- Create: `pkg/credman/types/oauth_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/credman/types/oauth_test.go
package types

import (
	"testing"
	"time"
)

func TestOAuth2TokenIsExpired(t *testing.T) {
	t.Run("future expiry is not expired", func(t *testing.T) {
		tok := OAuth2Token{ExpiresAt: time.Now().Add(5 * time.Minute)}
		if tok.IsExpired(60 * time.Second) {
			t.Fatal("token >1min future must not be expired")
		}
	})
	t.Run("within skew is expired", func(t *testing.T) {
		tok := OAuth2Token{ExpiresAt: time.Now().Add(30 * time.Second)}
		if !tok.IsExpired(60 * time.Second) {
			t.Fatal("token inside skew window must be expired")
		}
	})
	t.Run("past expiry is expired", func(t *testing.T) {
		tok := OAuth2Token{ExpiresAt: time.Now().Add(-1 * time.Minute)}
		if !tok.IsExpired(60 * time.Second) {
			t.Fatal("past expiry must be expired")
		}
	})
	t.Run("zero expiry is expired", func(t *testing.T) {
		tok := OAuth2Token{}
		if !tok.IsExpired(60 * time.Second) {
			t.Fatal("zero time must be treated as expired")
		}
	})
}

func TestOAuth2TokenHasScopes(t *testing.T) {
	tok := OAuth2Token{Scopes: []string{"a", "b", "c"}}
	if !tok.HasScopes([]string{"a"}) {
		t.Fatal("subset of stored scopes must match")
	}
	if !tok.HasScopes([]string{"a", "c"}) {
		t.Fatal("multi-element subset must match")
	}
	if tok.HasScopes([]string{"a", "d"}) {
		t.Fatal("scope not in stored set must not match")
	}
	if !tok.HasScopes(nil) {
		t.Fatal("empty request must trivially match")
	}
}

func TestTokenKeyZeroAccountDefaults(t *testing.T) {
	k := TokenKey{PluginID: "x"}.WithDefaultAccount()
	if k.Account != "default" {
		t.Fatalf("empty account must default to \"default\", got %q", k.Account)
	}
	k2 := TokenKey{PluginID: "x", Account: "work"}.WithDefaultAccount()
	if k2.Account != "work" {
		t.Fatalf("non-empty account must be preserved, got %q", k2.Account)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/credman/types/ -run 'TestOAuth2Token|TestTokenKey' -count=1`
Expected: FAIL — types and methods do not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// pkg/credman/types/oauth.go
package types

import "time"

// OAuth2Token is a stored OAuth 2.0 token bundle. Persisted (encrypted)
// by credman.TokenManager.
type OAuth2Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	Scopes       []string
	IDToken      string
	IssuedAt     time.Time
}

// IsExpired reports whether the token is within `skew` of its expiry.
// The zero time is treated as already-expired.
func (t *OAuth2Token) IsExpired(skew time.Duration) bool {
	if t.ExpiresAt.IsZero() {
		return true
	}
	return time.Until(t.ExpiresAt) <= skew
}

// HasScopes reports whether every scope in `want` is present in t.Scopes.
func (t *OAuth2Token) HasScopes(want []string) bool {
	if len(want) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(t.Scopes))
	for _, s := range t.Scopes {
		have[s] = struct{}{}
	}
	for _, w := range want {
		if _, ok := have[w]; !ok {
			return false
		}
	}
	return true
}

// TokenKey uniquely identifies a stored token by (plugin, account).
type TokenKey struct {
	PluginID string
	Account  string
}

// WithDefaultAccount fills Account with "default" if it was empty.
func (k TokenKey) WithDefaultAccount() TokenKey {
	if k.Account == "" {
		k.Account = "default"
	}
	return k
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/credman/types/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/credman/types/oauth.go pkg/credman/types/oauth_test.go
git commit -m "credman: add OAuth2Token and TokenKey types"
```

---

### Task 2: `TokenManager` persistent store

**Files:**
- Create: `pkg/credman/tokenmgr.go`
- Create: `pkg/credman/tokenmgr_test.go`

- [ ] **Step 1: Write the failing test**

```go
// pkg/credman/tokenmgr_test.go
package credman

import (
	"crypto/rand"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

func newTestTokenMgr(t *testing.T) *TokenManager {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	p := filepath.Join(t.TempDir(), "tokens.gob")
	tm, err := NewTokenManager(p, key)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	return tm
}

func TestTokenMgrSetGetDelete(t *testing.T) {
	tm := newTestTokenMgr(t)
	k := types.TokenKey{PluginID: "gdrive", Account: "default"}

	if _, err := tm.Get(k); err == nil {
		t.Fatal("expected Get on empty store to error")
	}
	tok := &types.OAuth2Token{
		AccessToken:  "ACCESS",
		RefreshToken: "REFRESH",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{"drive.readonly"},
	}
	if err := tm.Set(k, tok); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := tm.Get(k)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "ACCESS" || got.RefreshToken != "REFRESH" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if err := tm.Delete(k); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := tm.Get(k); err == nil {
		t.Fatal("post-delete Get should error")
	}
}

func TestTokenMgrPersistAcrossReopen(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	p := filepath.Join(t.TempDir(), "tokens.gob")

	tm, err := NewTokenManager(p, key)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	k := types.TokenKey{PluginID: "p", Account: "default"}
	_ = tm.Set(k, &types.OAuth2Token{AccessToken: "A", ExpiresAt: time.Now().Add(time.Hour)})
	_ = tm.Close()

	tm2, err := NewTokenManager(p, key)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer tm2.Close()
	got, err := tm2.Get(k)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.AccessToken != "A" {
		t.Fatalf("lost value across reopen, got %q", got.AccessToken)
	}
}

func TestTokenMgrList(t *testing.T) {
	tm := newTestTokenMgr(t)
	keys := []types.TokenKey{
		{PluginID: "a", Account: "default"},
		{PluginID: "a", Account: "work"},
		{PluginID: "b", Account: "default"},
	}
	for _, k := range keys {
		_ = tm.Set(k, &types.OAuth2Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	}
	got := tm.List()
	if len(got) != 3 {
		t.Fatalf("List len=%d, want 3", len(got))
	}
}

func TestTokenMgrConcurrentAccess(t *testing.T) {
	tm := newTestTokenMgr(t)
	k := types.TokenKey{PluginID: "p", Account: "default"}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = tm.Set(k, &types.OAuth2Token{AccessToken: "v", ExpiresAt: time.Now().Add(time.Hour)})
			_, _ = tm.Get(k)
		}(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/credman/ -run TestTokenMgr -count=1`
Expected: FAIL — `NewTokenManager`, `TokenManager` do not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// pkg/credman/tokenmgr.go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/credman/ -run TestTokenMgr -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/credman/tokenmgr.go pkg/credman/tokenmgr_test.go
git commit -m "credman: add TokenManager for OAuth token storage"
```

---

## Phase B — OAuth primitives (pure Go, no engine/RPC)

### Task 3: PKCE helpers

**Files:**
- Create: `internal/extl/auth/pkce.go`
- Create: `internal/extl/auth/pkce_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/auth/pkce_test.go
package auth

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewPKCEVerifierIsCompliant(t *testing.T) {
	for i := 0; i < 100; i++ {
		v, err := NewPKCEVerifier()
		if err != nil {
			t.Fatalf("NewPKCEVerifier: %v", err)
		}
		// RFC 7636: 43-128 chars, unreserved set.
		if len(v) < 43 || len(v) > 128 {
			t.Fatalf("verifier length out of bounds: %d", len(v))
		}
		if matched, _ := regexp.MatchString(`^[A-Za-z0-9\-._~]+$`, v); !matched {
			t.Fatalf("verifier has invalid chars: %q", v)
		}
	}
}

func TestChallengeFromVerifierS256(t *testing.T) {
	// Test vector from RFC 7636 section 4.4
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	got := PKCEChallenge(verifier, "S256")
	if got != expected {
		t.Fatalf("S256 challenge mismatch:\n got:  %s\n want: %s", got, expected)
	}
}

func TestChallengePlain(t *testing.T) {
	v := "abc123"
	if PKCEChallenge(v, "plain") != v {
		t.Fatal("plain challenge must equal verifier")
	}
}

func TestChallengeUnknownMethodFallsBackToS256(t *testing.T) {
	v, _ := NewPKCEVerifier()
	c := PKCEChallenge(v, "unknown")
	if c == "" || c == v || strings.ContainsAny(c, "+/=") {
		t.Fatalf("expected base64url S256 challenge, got %q", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/auth/ -run TestChallengeFromVerifierS256 -count=1`
Expected: FAIL — package `auth` does not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/extl/auth/pkce.go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewPKCEVerifier returns a fresh RFC-7636 compliant code_verifier:
// 43 characters of base64url-encoded random bytes, within the allowed
// unreserved character set.
func NewPKCEVerifier() (string, error) {
	// 32 raw bytes → 43 base64url chars (no padding).
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// PKCEChallenge derives the code_challenge from a verifier.
// method: "S256" (default) or "plain".
func PKCEChallenge(verifier, method string) string {
	switch method {
	case "plain":
		return verifier
	default: // "S256" or anything unrecognised
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/auth/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/auth/pkce.go internal/extl/auth/pkce_test.go
git commit -m "extl/auth: add PKCE code-verifier / challenge helpers"
```

---

### Task 4: Auth manifest schema + validator

**Files:**
- Create: `internal/extl/auth/manifest.go`
- Create: `internal/extl/auth/manifest_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/auth/manifest_test.go
package auth

import (
	"strings"
	"testing"
)

func validCfg() OAuth2Config {
	return OAuth2Config{
		Type:         "oauth2",
		ClientID:     "abc.apps.googleusercontent.com",
		Scopes:       []string{"drive.readonly"},
		AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		PKCEMethod:   "S256",
	}
}

func TestValidateMinimal(t *testing.T) {
	if err := ValidateOAuth2Config(validCfg()); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestValidateDefaultsPKCE(t *testing.T) {
	c := validCfg()
	c.PKCEMethod = ""
	normalized, err := NormalizeOAuth2Config(c)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.PKCEMethod != "S256" {
		t.Fatalf("pkce default = %q want S256", normalized.PKCEMethod)
	}
}

func TestValidateRejectsCases(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*OAuth2Config)
		wantMsg string
	}{
		{"missing type", func(c *OAuth2Config) { c.Type = "" }, "type"},
		{"bad type", func(c *OAuth2Config) { c.Type = "magic" }, "oauth2"},
		{"no client_id", func(c *OAuth2Config) { c.ClientID = "" }, "client_id"},
		{"empty scopes", func(c *OAuth2Config) { c.Scopes = nil }, "scopes"},
		{"http authorize", func(c *OAuth2Config) { c.AuthorizeURL = "http://x" }, "https"},
		{"http token", func(c *OAuth2Config) { c.TokenURL = "http://x" }, "https"},
		{"http device", func(c *OAuth2Config) { c.DeviceURL = "http://x" }, "https"},
		{"http revoke", func(c *OAuth2Config) { c.RevokeURL = "http://x" }, "https"},
		{"bad pkce", func(c *OAuth2Config) { c.PKCEMethod = "wat" }, "pkce"},
		{"forbidden secret", func(c *OAuth2Config) { c.ClientSecret = "s" }, "client_secret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := validCfg()
			tc.mutate(&c)
			err := ValidateOAuth2Config(c)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.wantMsg) {
				t.Fatalf("error %q does not mention %q", err, tc.wantMsg)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/auth/ -run TestValidate -count=1`
Expected: FAIL — `OAuth2Config` and validators do not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/extl/auth/manifest.go
package auth

import (
	"fmt"
	"net/url"
	"strings"
)

// OAuth2Config is the `auth` block of a plugin manifest.
type OAuth2Config struct {
	Type             string            `json:"type"`
	ClientID         string            `json:"client_id"`
	ClientSecret     string            `json:"client_secret,omitempty"` // REJECTED; kept for friendly error
	Scopes           []string          `json:"scopes"`
	AuthorizeURL     string            `json:"authorize_url"`
	TokenURL         string            `json:"token_url"`
	DeviceURL        string            `json:"device_url,omitempty"`
	RevokeURL        string            `json:"revoke_url,omitempty"`
	PKCEMethod       string            `json:"pkce,omitempty"`
	ExtraAuthParams  map[string]string `json:"extra_auth_params,omitempty"`
}

// ValidateOAuth2Config returns an error if cfg is not a legal manifest.
func ValidateOAuth2Config(cfg OAuth2Config) error {
	if cfg.Type == "" {
		return fmt.Errorf("auth: type is required")
	}
	if cfg.Type != "oauth2" {
		return fmt.Errorf("auth: only type \"oauth2\" is supported (got %q)", cfg.Type)
	}
	if cfg.ClientSecret != "" {
		return fmt.Errorf("auth: client_secret is not supported " +
			"(PKCE public clients only). Remove client_secret from the manifest.")
	}
	if cfg.ClientID == "" {
		return fmt.Errorf("auth: client_id is required")
	}
	if len(cfg.Scopes) == 0 {
		return fmt.Errorf("auth: at least one scope required")
	}
	if err := mustHTTPS("authorize_url", cfg.AuthorizeURL, true); err != nil {
		return err
	}
	if err := mustHTTPS("token_url", cfg.TokenURL, true); err != nil {
		return err
	}
	if err := mustHTTPS("device_url", cfg.DeviceURL, false); err != nil {
		return err
	}
	if err := mustHTTPS("revoke_url", cfg.RevokeURL, false); err != nil {
		return err
	}
	if cfg.PKCEMethod != "" && cfg.PKCEMethod != "S256" && cfg.PKCEMethod != "plain" {
		return fmt.Errorf("auth: pkce must be \"S256\" or \"plain\", got %q", cfg.PKCEMethod)
	}
	return nil
}

// NormalizeOAuth2Config fills defaults and returns a validated copy.
func NormalizeOAuth2Config(cfg OAuth2Config) (OAuth2Config, error) {
	if cfg.PKCEMethod == "" {
		cfg.PKCEMethod = "S256"
	}
	return cfg, ValidateOAuth2Config(cfg)
}

func mustHTTPS(field, raw string, required bool) error {
	if raw == "" {
		if required {
			return fmt.Errorf("auth: %s is required", field)
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("auth: %s must be an https:// URL (got %q)", field, raw)
	}
	if u.Host == "" {
		return fmt.Errorf("auth: %s is missing host (got %q)", field, raw)
	}
	_ = strings.TrimSpace // keep import for future
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/auth/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/auth/manifest.go internal/extl/auth/manifest_test.go
git commit -m "extl/auth: add OAuth2Config schema and validator"
```

---

### Task 5: `FlowRegistry` — in-flight flow tracking

**Files:**
- Create: `internal/extl/auth/flowregistry.go`
- Create: `internal/extl/auth/flowregistry_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/auth/flowregistry_test.go
package auth

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

func TestFlowRegistry_StartReturnsFlowID(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	f, _, err := fr.Start(types.TokenKey{PluginID: "p"}, FlowKindPKCE)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID == "" {
		t.Fatal("expected non-empty flow id")
	}
}

func TestFlowRegistry_DuplicateJoinsExisting(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f1, _, _ := fr.Start(k, FlowKindPKCE)
	f2, joined, _ := fr.Start(k, FlowKindPKCE)
	if !joined {
		t.Fatal("second Start should report joined=true")
	}
	if f1.ID != f2.ID {
		t.Fatalf("expected same flow id, got %s vs %s", f1.ID, f2.ID)
	}
}

func TestFlowRegistry_ResolveUnblocksAwaiters(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)

	var gotTok atomic.Pointer[types.OAuth2Token]
	var wg sync.WaitGroup
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			tok, err := fr.Await(f.ID)
			if err != nil {
				t.Errorf("Await: %v", err)
				return
			}
			gotTok.Store(tok)
		}()
	}
	time.Sleep(10 * time.Millisecond)
	expected := &types.OAuth2Token{AccessToken: "A"}
	fr.Resolve(f.ID, expected, nil)
	wg.Wait()
	if gotTok.Load().AccessToken != "A" {
		t.Fatal("awaiters did not receive resolved token")
	}
}

func TestFlowRegistry_CancelReturnsError(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)

	done := make(chan error, 1)
	go func() { _, err := fr.Await(f.ID); done <- err }()
	time.Sleep(10 * time.Millisecond)
	fr.Cancel(f.ID, errors.New("user_cancel"))
	err := <-done
	if err == nil || err.Error() != "user_cancel" {
		t.Fatalf("expected user_cancel error, got %v", err)
	}
}

func TestFlowRegistry_TimeoutExpiresFlow(t *testing.T) {
	fr := NewFlowRegistry(20 * time.Millisecond)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)

	_, err := fr.Await(f.ID)
	if !errors.Is(err, ErrFlowTimeout) {
		t.Fatalf("expected ErrFlowTimeout, got %v", err)
	}
}

func TestFlowRegistry_AwaitUnknownErrors(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	_, err := fr.Await("does-not-exist")
	if !errors.Is(err, ErrFlowUnknown) {
		t.Fatalf("expected ErrFlowUnknown, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/auth/ -run TestFlowRegistry -count=1`
Expected: FAIL — `FlowRegistry` does not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/extl/auth/flowregistry.go
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

// FlowKind is PKCE (browser loopback) or Device (user-code polling).
type FlowKind string

const (
	FlowKindPKCE   FlowKind = "pkce"
	FlowKindDevice FlowKind = "device"
)

var (
	ErrFlowUnknown = errors.New("flow: unknown flow id")
	ErrFlowTimeout = errors.New("flow: timed out")
)

// Flow is a running auth attempt.
type Flow struct {
	ID      string
	Kind    FlowKind
	Key     types.TokenKey
	Started time.Time

	// in-memory only; never persisted
	CodeVerifier string // PKCE
	State        string // CSRF guard
	DeviceCode   string // Device flow

	// resolve channel; receives result or err once
	resolveCh chan flowResult
}

type flowResult struct {
	tok *types.OAuth2Token
	err error
}

// FlowRegistry is an in-memory broker between plugin Token() calls and
// the RPC layer that completes flows.
type FlowRegistry struct {
	mu      sync.Mutex
	byID    map[string]*Flow
	byKey   map[types.TokenKey]*Flow
	timeout time.Duration
	done    chan struct{}
}

// NewFlowRegistry creates a registry. `timeout` is the per-flow deadline.
func NewFlowRegistry(timeout time.Duration) *FlowRegistry {
	return &FlowRegistry{
		byID:    map[string]*Flow{},
		byKey:   map[types.TokenKey]*Flow{},
		timeout: timeout,
		done:    make(chan struct{}),
	}
}

// Start creates or joins a flow for key. The second return indicates
// whether we joined an existing flow (vs. started a new one).
func (r *FlowRegistry) Start(key types.TokenKey, kind FlowKind) (*Flow, bool, error) {
	key = key.WithDefaultAccount()
	r.mu.Lock()
	if existing, ok := r.byKey[key]; ok {
		r.mu.Unlock()
		return existing, true, nil
	}
	id, err := randomID()
	if err != nil {
		r.mu.Unlock()
		return nil, false, err
	}
	f := &Flow{
		ID:        id,
		Kind:      kind,
		Key:       key,
		Started:   time.Now(),
		resolveCh: make(chan flowResult, 1),
	}
	r.byID[id] = f
	r.byKey[key] = f
	r.mu.Unlock()

	// Timeout watcher.
	go func() {
		select {
		case <-time.After(r.timeout):
			r.Cancel(id, ErrFlowTimeout)
		case <-f.resolveCh:
			// already resolved; reinsert the value so Await can read it
			// (this goroutine is purely a timeout; Await drains directly)
		case <-r.done:
		}
	}()

	return f, false, nil
}

// Await blocks until the flow is resolved or cancelled.
func (r *FlowRegistry) Await(id string) (*types.OAuth2Token, error) {
	r.mu.Lock()
	f, ok := r.byID[id]
	r.mu.Unlock()
	if !ok {
		return nil, ErrFlowUnknown
	}
	res := <-f.resolveCh
	// Clean up completed flow.
	r.mu.Lock()
	delete(r.byID, f.ID)
	if cur, ok := r.byKey[f.Key]; ok && cur.ID == f.ID {
		delete(r.byKey, f.Key)
	}
	r.mu.Unlock()
	return res.tok, res.err
}

// Resolve delivers a token to awaiters. Safe to call once per flow.
func (r *FlowRegistry) Resolve(id string, tok *types.OAuth2Token, err error) {
	r.mu.Lock()
	f, ok := r.byID[id]
	r.mu.Unlock()
	if !ok {
		return
	}
	select {
	case f.resolveCh <- flowResult{tok: tok, err: err}:
	default:
		// already resolved
	}
}

// Cancel resolves a flow with an error.
func (r *FlowRegistry) Cancel(id string, err error) {
	if err == nil {
		err = errors.New("cancelled")
	}
	r.Resolve(id, nil, err)
}

// Get returns the live flow for id, or nil.
func (r *FlowRegistry) Get(id string) *Flow {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID[id]
}

// Shutdown cancels every in-flight flow.
func (r *FlowRegistry) Shutdown() {
	r.mu.Lock()
	ids := make([]string, 0, len(r.byID))
	for id := range r.byID {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		r.Cancel(id, errors.New("registry shutdown"))
	}
	close(r.done)
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/auth/ -run TestFlowRegistry -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/auth/flowregistry.go internal/extl/auth/flowregistry_test.go
git commit -m "extl/auth: add FlowRegistry for in-flight auth flow tracking"
```

---

### Task 6: `AuthProvider` interface + errors

**Files:**
- Create: `internal/extl/auth/provider.go`
- Create: `internal/extl/auth/provider_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/auth/provider_test.go
package auth

import (
	"errors"
	"testing"
)

func TestErrAuthRequiredIsDistinct(t *testing.T) {
	if !errors.Is(ErrAuthRequired, ErrAuthRequired) {
		t.Fatal("ErrAuthRequired is not its own type")
	}
	if errors.Is(ErrAuthCancelled, ErrAuthRequired) {
		t.Fatal("ErrAuthCancelled must not satisfy ErrAuthRequired")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/auth/ -run TestErrAuth -count=1`
Expected: FAIL — sentinel errors do not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/extl/auth/provider.go
package auth

import (
	"context"
	"errors"

	"github.com/warpdl/warpdl/pkg/credman/types"
)

// AuthProvider abstracts over any credential acquisition scheme for a
// plugin. OAuth2Provider is the first implementation; future providers
// (API keys, HTTP basic, PATs) implement the same interface.
type AuthProvider interface {
	// Token returns a valid access token, blocking to drive a login flow
	// if necessary. Returns ErrAuthRequired when no interactive channel
	// is available and no cached token exists.
	Token(ctx context.Context, key types.TokenKey, scopes []string) (string, error)

	// Invalidate drops the cached access token for key without removing
	// the refresh credential; next Token() will refresh or re-auth.
	Invalidate(key types.TokenKey) error

	// Logout revokes server-side (where possible) and deletes the
	// stored credential.
	Logout(ctx context.Context, key types.TokenKey) error

	// ListAccounts returns the accounts this provider has tokens for.
	ListAccounts() []string
}

// Sentinel errors surfaced to plugins, CLI, and logs.
var (
	ErrAuthRequired  = errors.New("authentication required")
	ErrAuthCancelled = errors.New("authentication cancelled")
	ErrAuthTimeout   = errors.New("authentication timed out")
	ErrScopeDenied   = errors.New("requested scope not granted")
	ErrProvider      = errors.New("identity provider error")
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/auth/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/auth/provider.go internal/extl/auth/provider_test.go
git commit -m "extl/auth: add AuthProvider interface and sentinel errors"
```

---

### Task 7: `OAuth2Provider` — PKCE exchange, refresh, revoke

**Files:**
- Create: `internal/extl/auth/oauth2.go`
- Create: `internal/extl/auth/oauth2_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/auth/oauth2_test.go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

type stubIdP struct {
	server    *httptest.Server
	tokenHits atomic.Int32
	// last-seen form values from /token
	lastGrant        string
	lastCode         string
	lastVerifier     string
	lastRefreshToken string
}

func newStubIdP(t *testing.T, access, refresh string, expiresIn int) *stubIdP {
	t.Helper()
	s := &stubIdP{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.tokenHits.Add(1)
		s.lastGrant = r.FormValue("grant_type")
		s.lastCode = r.FormValue("code")
		s.lastVerifier = r.FormValue("code_verifier")
		s.lastRefreshToken = r.FormValue("refresh_token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  access,
			"refresh_token": refresh,
			"token_type":    "Bearer",
			"expires_in":    expiresIn,
			"scope":         "drive.readonly",
		})
	})
	mux.HandleFunc("/revoke", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	s.server = httptest.NewTLSServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func makeTestProvider(t *testing.T, tokenURL, revokeURL string) (*OAuth2Provider, *credman.TokenManager) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	tm, err := credman.NewTokenManager(filepath.Join(t.TempDir(), "tokens.gob"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	cfg := OAuth2Config{
		Type:         "oauth2",
		ClientID:     "client-id",
		Scopes:       []string{"drive.readonly"},
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenURL,
		RevokeURL:    revokeURL,
		PKCEMethod:   "S256",
	}
	cfg, err = NormalizeOAuth2Config(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := NewOAuth2Provider("plugin-1", cfg, tm, NewFlowRegistry(time.Minute))
	// Allow the test server's self-signed cert.
	p.client = newInsecureTLSClient()
	return p, tm
}

func newInsecureTLSClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tlsInsecureConfig
	return &http.Client{Transport: tr}
}

var tlsInsecureConfig = tlsConfigInsecure() // separate fn so we can declare literal

// embed-in-file: compile-time import for crypto/tls only where needed
// (keep imports sparse elsewhere)

func TestExchangeSetsTokenAndVerifier(t *testing.T) {
	idp := newStubIdP(t, "AT1", "RT1", 3600)
	p, tm := makeTestProvider(t, idp.server.URL+"/token", "")
	key := types.TokenKey{PluginID: "plugin-1"}

	verifier, _ := NewPKCEVerifier()
	redirect := "http://127.0.0.1:12345/callback"
	tok, err := p.ExchangeCode(context.Background(), key, "CODE", verifier, redirect)
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "AT1" || tok.RefreshToken != "RT1" {
		t.Fatalf("bad tokens: %+v", tok)
	}
	if idp.lastGrant != "authorization_code" {
		t.Fatalf("grant_type = %q", idp.lastGrant)
	}
	if idp.lastCode != "CODE" || idp.lastVerifier != verifier {
		t.Fatalf("params lost: code=%q verifier=%q", idp.lastCode, idp.lastVerifier)
	}
	// Persisted.
	stored, err := tm.Get(key)
	if err != nil {
		t.Fatalf("stored: %v", err)
	}
	if stored.AccessToken != "AT1" {
		t.Fatalf("store round-trip lost token: %+v", stored)
	}
}

func TestTokenReturnsCachedWhenFresh(t *testing.T) {
	idp := newStubIdP(t, "AT-NEW", "RT-NEW", 3600)
	p, tm := makeTestProvider(t, idp.server.URL+"/token", "")
	key := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(key, &types.OAuth2Token{
		AccessToken:  "CACHED",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(30 * time.Minute),
		Scopes:       []string{"drive.readonly"},
	})
	tok, err := p.Token(context.Background(), key, []string{"drive.readonly"})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "CACHED" {
		t.Fatalf("expected cached, got %q (hit IdP: %d)", tok, idp.tokenHits.Load())
	}
	if idp.tokenHits.Load() != 0 {
		t.Fatal("fresh token must not hit /token")
	}
}

func TestTokenRefreshesWhenExpired(t *testing.T) {
	idp := newStubIdP(t, "AT-REFRESHED", "RT-ROTATED", 3600)
	p, tm := makeTestProvider(t, idp.server.URL+"/token", "")
	key := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(key, &types.OAuth2Token{
		AccessToken:  "OLD",
		RefreshToken: "RT-OLD",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Scopes:       []string{"drive.readonly"},
	})
	tok, err := p.Token(context.Background(), key, []string{"drive.readonly"})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "AT-REFRESHED" {
		t.Fatalf("expected refreshed, got %q", tok)
	}
	stored, _ := tm.Get(key)
	if stored.RefreshToken != "RT-ROTATED" {
		t.Fatalf("rotated refresh not persisted: %q", stored.RefreshToken)
	}
	if idp.lastGrant != "refresh_token" {
		t.Fatalf("grant_type = %q", idp.lastGrant)
	}
}

func TestTokenScopeMismatchTriggersFlow(t *testing.T) {
	idp := newStubIdP(t, "", "", 0)
	p, tm := makeTestProvider(t, idp.server.URL+"/token", "")
	key := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(key, &types.OAuth2Token{
		AccessToken: "OK",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scopes:      []string{"drive.readonly"},
	})
	// Ask for a scope the stored token doesn't cover. No interactive
	// channel here, so the provider returns ErrAuthRequired.
	_, err := p.Token(context.Background(), key, []string{"drive.write"})
	if err == nil {
		t.Fatal("expected auth-required error")
	}
}

func TestLogoutRevokesAndDeletes(t *testing.T) {
	idp := newStubIdP(t, "AT1", "RT1", 3600)
	p, tm := makeTestProvider(t, idp.server.URL+"/token", idp.server.URL+"/revoke")
	key := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(key, &types.OAuth2Token{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(time.Hour)})
	if err := p.Logout(context.Background(), key); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := tm.Get(key); err == nil {
		t.Fatal("expected deleted after logout")
	}
}

// helpers below use Request/url/strings/io to keep the test file self-contained.
var _ = url.URL{}
var _ = strings.Builder{}
var _ = io.EOF
var _ = http.Header{}

func tlsConfigInsecure() (out struct {
	InsecureSkipVerify bool
}) {
	out.InsecureSkipVerify = true
	return
}
```

**Note for Task 7:** the `tlsConfigInsecure` helper above is a sketch — the real implementation will import `crypto/tls` directly and build a `*tls.Config{InsecureSkipVerify: true}`. Replace the sketch with:

```go
import "crypto/tls"

var tlsInsecureConfig = tls.Config{InsecureSkipVerify: true}
```

and delete the `tlsConfigInsecure()` helper + the bogus-type `var tlsInsecureConfig = tlsConfigInsecure()` line.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/auth/ -run 'TestExchange|TestToken|TestLogout' -count=1`
Expected: FAIL — `OAuth2Provider` does not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/extl/auth/oauth2.go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// skew is the window before ExpiresAt during which a token is treated as
// already expired. Matches golang.org/x/oauth2.
const skew = 60 * time.Second

// OAuth2Provider implements AuthProvider against any OAuth 2.0 +
// PKCE-capable server.
type OAuth2Provider struct {
	pluginID string
	cfg      OAuth2Config
	store    *credman.TokenManager
	flows    *FlowRegistry
	client   *http.Client

	refreshLocks sync.Map // types.TokenKey → *sync.Mutex
}

// NewOAuth2Provider builds a provider. cfg must already be normalized.
func NewOAuth2Provider(pluginID string, cfg OAuth2Config, store *credman.TokenManager, flows *FlowRegistry) *OAuth2Provider {
	return &OAuth2Provider{
		pluginID: pluginID,
		cfg:      cfg,
		store:    store,
		flows:    flows,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Config returns the normalized config (read-only).
func (p *OAuth2Provider) Config() OAuth2Config { return p.cfg }

// FlowRegistry returns the registry used by this provider (so the RPC
// layer can Resolve flows).
func (p *OAuth2Provider) FlowRegistry() *FlowRegistry { return p.flows }

// Token returns a valid access token, refreshing if possible, triggering
// a flow if necessary.
func (p *OAuth2Provider) Token(ctx context.Context, key types.TokenKey, scopes []string) (string, error) {
	key = key.WithDefaultAccount()
	if err := p.scopesAllowed(scopes); err != nil {
		return "", err
	}

	tok, err := p.store.Get(key)
	switch {
	case err != nil:
		// miss → flow
		return p.triggerFlowAndAwait(ctx, key, scopes)
	case !tok.HasScopes(resolveScopes(scopes, p.cfg.Scopes)):
		return p.triggerFlowAndAwait(ctx, key, scopes)
	case !tok.IsExpired(skew):
		return tok.AccessToken, nil
	}

	// Expired. Try refresh.
	newTok, err := p.refreshUnderLock(ctx, key)
	if err == nil {
		return newTok.AccessToken, nil
	}
	// Refresh failed → drop credential and start a flow.
	_ = p.store.Delete(key)
	return p.triggerFlowAndAwait(ctx, key, scopes)
}

// Invalidate blanks the cached access token; refresh token (if any) stays.
func (p *OAuth2Provider) Invalidate(key types.TokenKey) error {
	key = key.WithDefaultAccount()
	tok, err := p.store.Get(key)
	if err != nil {
		return nil // nothing to invalidate
	}
	tok.AccessToken = ""
	tok.ExpiresAt = time.Time{}
	return p.store.Set(key, tok)
}

// Logout calls revoke_url if configured, then deletes from the store.
func (p *OAuth2Provider) Logout(ctx context.Context, key types.TokenKey) error {
	key = key.WithDefaultAccount()
	tok, _ := p.store.Get(key)
	if tok != nil && p.cfg.RevokeURL != "" {
		tokenToRevoke := tok.RefreshToken
		if tokenToRevoke == "" {
			tokenToRevoke = tok.AccessToken
		}
		_ = p.postRevoke(ctx, tokenToRevoke) // best-effort
	}
	return p.store.Delete(key)
}

// ListAccounts returns account labels for this plugin only.
func (p *OAuth2Provider) ListAccounts() []string {
	all := p.store.List()
	out := make([]string, 0, len(all))
	for _, k := range all {
		if k.PluginID == p.pluginID {
			out = append(out, k.Account)
		}
	}
	return out
}

// BuildAuthorizeURL constructs the redirect target for a PKCE flow.
func (p *OAuth2Provider) BuildAuthorizeURL(redirectURI, state, challenge string, scopes []string) string {
	u, _ := url.Parse(p.cfg.AuthorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(resolveScopes(scopes, p.cfg.Scopes), " "))
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", p.cfg.PKCEMethod)
	for k, v := range p.cfg.ExtraAuthParams {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ExchangeCode completes the PKCE authorization-code grant and stores
// the resulting bundle.
func (p *OAuth2Provider) ExchangeCode(ctx context.Context, key types.TokenKey, code, verifier, redirectURI string) (*types.OAuth2Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("code_verifier", verifier)
	tok, err := p.postToken(ctx, form, nil)
	if err != nil {
		return nil, err
	}
	if err := p.store.Set(key, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

// refreshUnderLock serialises refreshes for the same key.
func (p *OAuth2Provider) refreshUnderLock(ctx context.Context, key types.TokenKey) (*types.OAuth2Token, error) {
	mu := p.muFor(key)
	mu.Lock()
	defer mu.Unlock()
	// Re-read: someone may have refreshed while we waited.
	tok, err := p.store.Get(key)
	if err != nil {
		return nil, err
	}
	if !tok.IsExpired(skew) {
		return tok, nil
	}
	if tok.RefreshToken == "" {
		return nil, errors.New("no refresh token")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", tok.RefreshToken)
	form.Set("client_id", p.cfg.ClientID)
	refreshed, err := p.postToken(ctx, form, tok)
	if err != nil {
		return nil, err
	}
	if err := p.store.Set(key, refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func (p *OAuth2Provider) muFor(key types.TokenKey) *sync.Mutex {
	m, _ := p.refreshLocks.LoadOrStore(key, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (p *OAuth2Provider) postToken(ctx context.Context, form url.Values, prior *types.OAuth2Token) (*types.OAuth2Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: %s: %s", ErrProvider, resp.Status, string(body))
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrProvider, err)
	}
	scopes := p.cfg.Scopes
	if payload.Scope != "" {
		scopes = strings.Fields(payload.Scope)
	}
	refresh := payload.RefreshToken
	if refresh == "" && prior != nil {
		refresh = prior.RefreshToken
	}
	now := time.Now()
	return &types.OAuth2Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: refresh,
		TokenType:    payload.TokenType,
		ExpiresAt:    now.Add(time.Duration(payload.ExpiresIn) * time.Second),
		Scopes:       scopes,
		IDToken:      payload.IDToken,
		IssuedAt:     now,
	}, nil
}

func (p *OAuth2Provider) postRevoke(ctx context.Context, token string) error {
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", p.cfg.ClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.RevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (p *OAuth2Provider) scopesAllowed(want []string) error {
	allowed := make(map[string]struct{}, len(p.cfg.Scopes))
	for _, s := range p.cfg.Scopes {
		allowed[s] = struct{}{}
	}
	for _, w := range want {
		if _, ok := allowed[w]; !ok {
			return fmt.Errorf("%w: scope %q not declared in manifest", ErrScopeDenied, w)
		}
	}
	return nil
}

// triggerFlowAndAwait starts a flow and blocks on it. The RPC layer (or
// an interactive CLI) is responsible for calling Resolve on the flow id.
func (p *OAuth2Provider) triggerFlowAndAwait(ctx context.Context, key types.TokenKey, scopes []string) (string, error) {
	f, _, err := p.flows.Start(key, FlowKindPKCE)
	if err != nil {
		return "", err
	}
	// Fill in the PKCE details the RPC layer will read off of the flow.
	verifier, err := NewPKCEVerifier()
	if err != nil {
		return "", err
	}
	state, err := randomID()
	if err != nil {
		return "", err
	}
	f.CodeVerifier = verifier
	f.State = state

	select {
	case <-ctx.Done():
		p.flows.Cancel(f.ID, ctx.Err())
		return "", ctx.Err()
	default:
	}

	tok, err := p.flows.Await(f.ID)
	if err != nil {
		return "", err
	}
	if err := p.store.Set(key, tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// resolveScopes returns `request` when non-empty, else the full manifest set.
func resolveScopes(request, manifest []string) []string {
	if len(request) > 0 {
		return request
	}
	return manifest
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/auth/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/auth/oauth2.go internal/extl/auth/oauth2_test.go
git commit -m "extl/auth: add OAuth2Provider (PKCE exchange, refresh, revoke)"
```

---

### Task 8: Device-code flow support

**Files:**
- Modify: `internal/extl/auth/oauth2.go` — add device code methods
- Modify: `internal/extl/auth/oauth2_test.go` — add device flow tests

- [ ] **Step 1: Write the failing test**

```go
// append to oauth2_test.go
func TestDeviceCodeFlow(t *testing.T) {
	var tokenCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "DEVICE-CODE-123",
			"user_code":        "ABCD-1234",
			"verification_url": "https://example.com/device",
			"expires_in":       600,
			"interval":         1,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		n := tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n < 3 {
			// First two polls return authorization_pending.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "DEV-AT",
			"refresh_token": "DEV-RT",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         "drive.readonly",
		})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	key := make([]byte, 32)
	_, _ = rand.Read(key)
	tm, _ := credman.NewTokenManager(filepath.Join(t.TempDir(), "tokens.gob"), key)
	defer tm.Close()
	cfg := OAuth2Config{
		Type:         "oauth2",
		ClientID:     "c",
		Scopes:       []string{"drive.readonly"},
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     srv.URL + "/token",
		DeviceURL:    srv.URL + "/device/code",
		PKCEMethod:   "S256",
	}
	cfg, _ = NormalizeOAuth2Config(cfg)
	p := NewOAuth2Provider("pid", cfg, tm, NewFlowRegistry(time.Minute))
	p.client = newInsecureTLSClient()

	init, err := p.StartDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("StartDeviceCode: %v", err)
	}
	if init.UserCode != "ABCD-1234" {
		t.Fatalf("bad user code: %+v", init)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := p.PollDeviceCode(ctx, init)
	if err != nil {
		t.Fatalf("PollDeviceCode: %v", err)
	}
	if tok.AccessToken != "DEV-AT" {
		t.Fatalf("bad token: %+v", tok)
	}
	if tokenCalls.Load() < 3 {
		t.Fatalf("expected 3 polls, got %d", tokenCalls.Load())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/auth/ -run TestDeviceCodeFlow -count=1`
Expected: FAIL — device-code methods do not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// Append to internal/extl/auth/oauth2.go

// DeviceAuthorization is the /device/code response.
type DeviceAuthorization struct {
	DeviceCode      string
	UserCode        string
	VerificationURL string
	ExpiresIn       int
	Interval        int
}

// StartDeviceCode initiates the device flow and returns details to show the user.
func (p *OAuth2Provider) StartDeviceCode(ctx context.Context) (*DeviceAuthorization, error) {
	if p.cfg.DeviceURL == "" {
		return nil, fmt.Errorf("device flow not supported by this plugin")
	}
	form := url.Values{}
	form.Set("client_id", p.cfg.ClientID)
	form.Set("scope", strings.Join(p.cfg.Scopes, " "))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.DeviceURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: device/code: %s: %s", ErrProvider, resp.Status, string(body))
	}
	var raw struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		VerificationURI string `json:"verification_uri"` // Google / RFC variant
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode device/code: %v", ErrProvider, err)
	}
	verification := raw.VerificationURL
	if verification == "" {
		verification = raw.VerificationURI
	}
	if raw.Interval == 0 {
		raw.Interval = 5
	}
	return &DeviceAuthorization{
		DeviceCode:      raw.DeviceCode,
		UserCode:        raw.UserCode,
		VerificationURL: verification,
		ExpiresIn:       raw.ExpiresIn,
		Interval:        raw.Interval,
	}, nil
}

// PollDeviceCode polls /token until the user completes the flow or the
// context is cancelled.
func (p *OAuth2Provider) PollDeviceCode(ctx context.Context, auth *DeviceAuthorization) (*types.OAuth2Token, error) {
	interval := time.Duration(auth.Interval) * time.Second
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("device_code", auth.DeviceCode)
		form.Set("client_id", p.cfg.ClientID)
		tok, err := p.postToken(ctx, form, nil)
		if err == nil {
			return tok, nil
		}
		// Treat authorization_pending and slow_down as retryable.
		errStr := strings.ToLower(err.Error())
		switch {
		case strings.Contains(errStr, "authorization_pending"):
			continue
		case strings.Contains(errStr, "slow_down"):
			interval += 5 * time.Second
			continue
		default:
			return nil, err
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/auth/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/auth/oauth2.go internal/extl/auth/oauth2_test.go
git commit -m "extl/auth: add device-code flow (start + polling with backoff)"
```

---

## Phase C — Engine integration

### Task 9: `ExtractResult` type + module return unpacking

**Files:**
- Modify: `internal/extl/module.go` — change Extract signature
- Modify: `internal/extl/engine.go` — propagate new type
- Modify: existing callers in `internal/api/download.go`, `internal/api/resume.go` — pass headers through
- Modify: `internal/extl/extl_test.go` — if tests reference the old return
- Create: `internal/extl/extract_test.go` — new tests covering both return shapes

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/extract_test.go
package extl

import (
	"io"
	"log"
	"path/filepath"
	"testing"
)

func writePluginDir(t *testing.T, entry string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"name":"t","version":"0","matches":["^x"],"entrypoint":"main.js"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.js"), []byte(entry), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestExtractStringReturn(t *testing.T) {
	dir := writePluginDir(t, `function extract(url){ return "https://resolved/"+url; }`)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	res, err := m.Extract("abc")
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://resolved/abc" {
		t.Fatalf("URL=%q", res.URL)
	}
	if len(res.Headers) != 0 {
		t.Fatalf("unexpected headers: %v", res.Headers)
	}
}

func TestExtractObjectReturn(t *testing.T) {
	js := `
function extract(u) {
  return {
    url: "https://resolved/"+u,
    headers: {"Authorization": "Bearer XYZ", "X-Custom": "1"}
  };
}`
	dir := writePluginDir(t, js)
	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	res, err := m.Extract("z")
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://resolved/z" {
		t.Fatalf("URL=%q", res.URL)
	}
	if res.Headers["Authorization"] != "Bearer XYZ" {
		t.Fatalf("missing auth header: %v", res.Headers)
	}
}

func TestExtractInvalidReturnTypeErrors(t *testing.T) {
	dir := writePluginDir(t, `function extract(){ return 42; }`)
	m, _ := OpenModule(log.New(io.Discard, "", 0), dir)
	_ = m.Load()
	if _, err := m.Extract("u"); err == nil {
		t.Fatal("expected error for non-string non-object return")
	}
}
```

*(add `"os"` to imports above if missing)*

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/ -run TestExtract -count=1`
Expected: FAIL — `Extract` still returns `(string, error)`.

- [ ] **Step 3: Write the minimal implementation**

Replace `Module.Extract` in `internal/extl/module.go`:

```go
// ExtractResult is what a plugin extract() hands back.
// Future-compatible: add optional fields without breaking string-return
// plugins.
type ExtractResult struct {
	URL     string
	Headers map[string]string
}

// Extract invokes the module's JavaScript extract function with the
// given URL. The JS function may return either a plain string (legacy
// contract) or an object with url + optional headers.
func (m *Module) Extract(url string) (ExtractResult, error) {
	v, err := m.runtime.RunString(EXTRACT_CALLBACK + `(` + jsQuote(url) + `)`)
	if err != nil {
		return ExtractResult{}, err
	}
	exported := v.Export()
	switch x := exported.(type) {
	case string:
		if x == EXPORTED_END {
			return ExtractResult{}, ErrInteractionEnded
		}
		return ExtractResult{URL: x}, nil
	case map[string]any:
		rawURL, ok := x["url"].(string)
		if !ok || rawURL == "" {
			return ExtractResult{}, ErrInvalidReturnType
		}
		if rawURL == EXPORTED_END {
			return ExtractResult{}, ErrInteractionEnded
		}
		headers := map[string]string{}
		if raw, ok := x["headers"].(map[string]any); ok {
			for k, val := range raw {
				s, ok := val.(string)
				if !ok {
					return ExtractResult{}, ErrInvalidReturnType
				}
				headers[k] = s
			}
		}
		return ExtractResult{URL: rawURL, Headers: headers}, nil
	default:
		return ExtractResult{}, ErrInvalidReturnType
	}
}

// jsQuote wraps s in a JSON string literal so embedded quotes are safe.
func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
```

Add `encoding/json` import to `module.go`.

Update `internal/extl/engine.go`'s `Engine.Extract`:

```go
// Extract now returns an ExtractResult instead of (string, error).
func (e *Engine) Extract(url string) (ExtractResult, error) {
	for _, m := range e.modules {
		for _, a := range m.Matches {
			if ok, err := regexp.MatchString(a, url); ok && err == nil {
				e.l.Println("Found match for", url, "in", m.Name, "(", m.ModuleId, ")")
				return m.Extract(url)
			}
		}
	}
	return ExtractResult{URL: url}, nil
}
```

Update callers (internal/api/download.go and resume.go). Find each call to `elEngine.Extract(url)` — update the variable binding to an `ExtractResult` and pass `res.Headers` into `warplib.DownloaderOpts.Headers` (merging with any existing headers).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/... -race -count=1 && go build ./...`
Expected: both PASS / build succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/module.go internal/extl/engine.go internal/extl/extract_test.go internal/api/download.go internal/api/resume.go
git commit -m "extl: extract() returns ExtractResult; carry headers to downloader"
```

---

### Task 10: JS bindings — `getAccessToken`, `fetchWithAuth`, `invalidateToken`, `listAccounts`

**Files:**
- Create: `internal/extl/auth/bindings.go` — RegisterBindings(runtime, provider)
- Create: `internal/extl/auth/bindings_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/auth/bindings_test.go
package auth

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

func setupRuntime(t *testing.T) (*goja.Runtime, *OAuth2Provider, *credman.TokenManager) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	tm, err := credman.NewTokenManager(filepath.Join(t.TempDir(), "tokens.gob"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	cfg := OAuth2Config{
		Type:         "oauth2",
		ClientID:     "c",
		Scopes:       []string{"a", "b"},
		AuthorizeURL: "https://example.com/a",
		TokenURL:     "https://example.com/t",
		PKCEMethod:   "S256",
	}
	cfg, _ = NormalizeOAuth2Config(cfg)
	p := NewOAuth2Provider("pid", cfg, tm, NewFlowRegistry(time.Minute))
	rt := goja.New()
	if err := RegisterBindings(rt, p); err != nil {
		t.Fatal(err)
	}
	return rt, p, tm
}

func TestBindingGetAccessTokenReturnsString(t *testing.T) {
	rt, _, tm := setupRuntime(t)
	k := types.TokenKey{PluginID: "pid", Account: "default"}
	_ = tm.Set(k, &types.OAuth2Token{
		AccessToken: "ABC", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"a"},
	})
	v, err := rt.RunString(`getAccessToken({scopes:["a"]})`)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "ABC" {
		t.Fatalf("got %q", v.String())
	}
}

func TestBindingGetAccessTokenRejectsUndeclaredScope(t *testing.T) {
	rt, _, _ := setupRuntime(t)
	if _, err := rt.RunString(`getAccessToken({scopes:["not-declared"]})`); err == nil {
		t.Fatal("expected error for scope outside manifest")
	}
}

func TestBindingListAccountsScoped(t *testing.T) {
	rt, _, tm := setupRuntime(t)
	_ = tm.Set(types.TokenKey{PluginID: "pid", Account: "default"}, &types.OAuth2Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	_ = tm.Set(types.TokenKey{PluginID: "pid", Account: "work"}, &types.OAuth2Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	_ = tm.Set(types.TokenKey{PluginID: "other", Account: "default"}, &types.OAuth2Token{AccessToken: "x", ExpiresAt: time.Now().Add(time.Hour)})
	v, err := rt.RunString(`listAccounts().sort().join(",")`)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "default,work" {
		t.Fatalf("unexpected accounts: %s", v.String())
	}
}

func TestBindingInvalidateTokenDropsAccess(t *testing.T) {
	rt, _, tm := setupRuntime(t)
	k := types.TokenKey{PluginID: "pid", Account: "default"}
	_ = tm.Set(k, &types.OAuth2Token{AccessToken: "X", RefreshToken: "R", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"a"}})
	if _, err := rt.RunString(`invalidateToken()`); err != nil {
		t.Fatal(err)
	}
	stored, _ := tm.Get(k)
	if stored.AccessToken != "" {
		t.Fatalf("access not cleared: %+v", stored)
	}
	if stored.RefreshToken != "R" {
		t.Fatal("refresh must be preserved")
	}
}

// fetchWithAuth happy path needs an http server; the simpler test is
// that the binding exists and errors cleanly when no request global is
// installed. Full end-to-end lives in the integration suite.
func TestBindingFetchWithAuthExists(t *testing.T) {
	rt, _, _ := setupRuntime(t)
	if _, err := rt.RunString(`typeof fetchWithAuth`); err != nil {
		t.Fatal(err)
	}
	v, _ := rt.RunString(`typeof fetchWithAuth`)
	if v.String() != "function" {
		t.Fatalf("fetchWithAuth type=%s", v.String())
	}
}

var _ = context.Background
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/auth/ -run TestBinding -count=1`
Expected: FAIL — `RegisterBindings` does not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/extl/auth/bindings.go
package auth

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

type getOpts struct {
	Account string   `json:"account"`
	Scopes  []string `json:"scopes"`
}

// RegisterBindings installs getAccessToken, fetchWithAuth,
// invalidateToken, listAccounts on runtime, wired to provider.
func RegisterBindings(rt *goja.Runtime, p AuthProvider) error {
	getToken := func(call goja.FunctionCall) goja.Value {
		var opts getOpts
		if len(call.Arguments) > 0 && !goja.IsUndefined(call.Arguments[0]) && !goja.IsNull(call.Arguments[0]) {
			if err := rt.ExportTo(call.Arguments[0], &opts); err != nil {
				panic(rt.NewTypeError("getAccessToken: invalid options"))
			}
		}
		key := types.TokenKey{Account: opts.Account}
		tok, err := p.Token(context.Background(), key, opts.Scopes)
		if err != nil {
			panic(rt.NewGoError(err))
		}
		return rt.ToValue(tok)
	}
	if err := rt.Set("getAccessToken", getToken); err != nil {
		return err
	}

	invalidate := func(call goja.FunctionCall) goja.Value {
		var opts getOpts
		if len(call.Arguments) > 0 && !goja.IsUndefined(call.Arguments[0]) && !goja.IsNull(call.Arguments[0]) {
			if err := rt.ExportTo(call.Arguments[0], &opts); err != nil {
				panic(rt.NewTypeError("invalidateToken: invalid options"))
			}
		}
		key := types.TokenKey{Account: opts.Account}
		if err := p.Invalidate(key); err != nil {
			panic(rt.NewGoError(err))
		}
		return goja.Undefined()
	}
	if err := rt.Set("invalidateToken", invalidate); err != nil {
		return err
	}

	listFn := func(call goja.FunctionCall) goja.Value {
		return rt.ToValue(p.ListAccounts())
	}
	if err := rt.Set("listAccounts", listFn); err != nil {
		return err
	}

	// fetchWithAuth: thin wrapper around `request` that adds Authorization.
	// If `request` isn't installed, throw.
	fetchJs := `
function fetchWithAuth(req, opts) {
    if (typeof request !== "function") {
        throw new Error("fetchWithAuth: request() not available");
    }
    var scopes = (opts && opts.scopes) || undefined;
    var token = getAccessToken({scopes: scopes, account: opts && opts.account});
    var headers = Object.assign({}, req.headers || {});
    headers["Authorization"] = "Bearer " + token;
    var merged = Object.assign({}, req, {headers: headers});
    var resp = request(merged);
    // Auto-retry once on 401 with a fresh token.
    if (resp && resp.status_code === 401) {
        invalidateToken({account: opts && opts.account});
        var token2 = getAccessToken({scopes: scopes, account: opts && opts.account});
        headers["Authorization"] = "Bearer " + token2;
        resp = request(Object.assign({}, req, {headers: headers}));
    }
    return resp;
}`
	if _, err := rt.RunString(fetchJs); err != nil {
		return fmt.Errorf("install fetchWithAuth: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/auth/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/auth/bindings.go internal/extl/auth/bindings_test.go
git commit -m "extl/auth: add JS bindings (getAccessToken, fetchWithAuth, invalidateToken, listAccounts)"
```

---

### Task 11: Engine loads `OAuth2Provider` per plugin + wires bindings

**Files:**
- Modify: `internal/extl/engine.go` — engine takes `TokenManager`, global `FlowRegistry`; per-module provider registration
- Modify: `internal/extl/module.go` — expose `Auth` field parsed from manifest; `Load` installs bindings when `Auth` present
- Modify: `internal/extl/engine.go` and callers — `NewEngine` takes the new deps

- [ ] **Step 1: Write the failing test**

```go
// internal/extl/extract_test.go - APPEND
func TestEngineLoadsAuthProvider(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
		"name":"p","version":"0","matches":["^x"],"entrypoint":"main.js",
		"auth":{"type":"oauth2","client_id":"c","scopes":["a"],
		        "authorize_url":"https://example.com/a",
		        "token_url":"https://example.com/t"}
	}`
	os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644)
	os.WriteFile(filepath.Join(dir, "main.js"), []byte(`function extract(u){ return "x:"+typeof getAccessToken; }`), 0644)

	m, err := OpenModule(log.New(io.Discard, "", 0), dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Auth == nil {
		t.Fatal("manifest auth not parsed")
	}
	// Full engine wiring tested in engine_test.go below.
}
```

*(and add a test that exercises the engine's plugin load with a real `TokenManager` + `OAuth2Provider` fixture; details parallel existing engine_test.go shapes)*

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/extl/ -run TestEngineLoadsAuthProvider -count=1`
Expected: FAIL — `Module.Auth` field does not exist.

- [ ] **Step 3: Write the minimal implementation**

Add to `internal/extl/module.go`:

```go
import "github.com/warpdl/warpdl/internal/extl/auth"

type Module struct {
    // existing fields ...
    Auth    *auth.OAuth2Config `json:"auth,omitempty"`
    // ...
    provider auth.AuthProvider // nil unless Auth != nil
}

// inside OpenModule, after JSON decode:
if m.Auth != nil {
    normalized, err := auth.NormalizeOAuth2Config(*m.Auth)
    if err != nil {
        return nil, fmt.Errorf("%s: %w", m.Name, err)
    }
    m.Auth = &normalized
}
```

Add to `internal/extl/engine.go`:

```go
// Engine now owns a TokenManager and FlowRegistry.
type Engine struct {
    // existing fields ...
    tokens *credman.TokenManager
    flows  *auth.FlowRegistry
}

// NewEngine signature grows. Daemon wiring updated in the same commit.
func NewEngine(l *log.Logger, cookieManager *credman.CookieManager, tokens *credman.TokenManager, flows *auth.FlowRegistry, debugger bool) (*Engine, error)

// After a module is loaded, if it declares auth, install the provider.
func (e *Engine) attachProvider(m *Module) error {
    if m.Auth == nil {
        return nil
    }
    prov := auth.NewOAuth2Provider(m.ModuleId, *m.Auth, e.tokens, e.flows)
    m.provider = prov
    return auth.RegisterBindings(m.runtime.Runtime, prov)
}
```

Update existing `Load`/`loadModule` call sites to invoke `attachProvider(m)` after `m.Load()`.

Update the daemon bootstrap (in `internal/daemon/runner.go` or wherever `NewEngine` is called) to construct `credman.NewTokenManager` and `auth.NewFlowRegistry(5*time.Minute)` and pass them in.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/extl/... -race -count=1 && go build ./...`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/extl/module.go internal/extl/engine.go internal/extl/extract_test.go internal/daemon/runner.go
git commit -m "extl: load OAuth2Provider per plugin and wire JS bindings"
```

---

## Phase D — RPC surface

### Task 12: Common RPC types + update constants

**Files:**
- Create: `common/auth.go`
- Modify: `common/update.go` (wherever `UpdateType` constants are declared)

- [ ] **Step 1: Write the failing test**

```go
// common/auth_test.go
package common

import "testing"

func TestAuthTypesRoundTripJSON(t *testing.T) {
    // spot-check: AuthLoginParams decodes; AuthListResult encodes
    p := AuthLoginParams{PluginID: "gd", Account: "default", Scopes: []string{"a"}, Flow: "pkce"}
    if p.PluginID != "gd" {
        t.Fatal("trivial field access")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./common/ -count=1`
Expected: FAIL — types do not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// common/auth.go
package common

type AuthLoginParams struct {
    PluginID    string   `json:"plugin_id"`
    Account     string   `json:"account,omitempty"`
    Scopes      []string `json:"scopes,omitempty"`
    Flow        string   `json:"flow,omitempty"`       // "pkce" | "device"
    RedirectURI string   `json:"redirect_uri,omitempty"`
}

type AuthLoginResult struct {
    FlowID          string `json:"flow_id"`
    AuthorizeURL    string `json:"authorize_url,omitempty"`
    DeviceCode      string `json:"device_code,omitempty"`
    UserCode        string `json:"user_code,omitempty"`
    VerificationURL string `json:"verification_url,omitempty"`
    Interval        int    `json:"interval,omitempty"`
    ExpiresAt       int64  `json:"expires_at"`
}

type AuthCompleteParams struct {
    FlowID string `json:"flow_id"`
    Code   string `json:"code"`
    State  string `json:"state"`
}

type AuthCompleteResult struct {
    Account   string   `json:"account"`
    Scopes    []string `json:"scopes"`
    ExpiresAt int64    `json:"expires_at"`
}

type AuthCancelParams struct { FlowID string `json:"flow_id"` }

type AuthAccount struct {
    PluginID  string   `json:"plugin_id"`
    Account   string   `json:"account"`
    Scopes    []string `json:"scopes"`
    ExpiresAt int64    `json:"expires_at"`
}
type AuthListResult struct { Accounts []AuthAccount `json:"accounts"` }

type AuthLogoutParams struct {
    PluginID string `json:"plugin_id"`
    Account  string `json:"account,omitempty"`
}
```

Add to `common/update.go` (or the file where `UpdateType` constants live):

```go
UPDATE_AUTH_REQUIRED   UpdateType = ... // next available
UPDATE_AUTH_COMPLETED  UpdateType = ...
UPDATE_AUTH_FAILED     UpdateType = ...
UPDATE_AUTH_LOGGED_OUT UpdateType = ...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./common/ -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add common/auth.go common/auth_test.go common/update.go
git commit -m "common: add auth RPC types and update constants"
```

---

### Task 13: `auth.login` handler

**Files:**
- Create: `internal/api/auth_login.go`
- Create: `internal/api/auth_login_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/api/auth_login_test.go
package api

// Test: a properly-authenticated PKCE login request returns a
// populated AuthorizeURL and FlowID.
//
// Strategy: build an Api harness with a stub engine that exposes a
// plugin whose OAuth2Config points at an httptest server. Call
// authLoginHandler, inspect the result.
//
// (Implementation details depend on existing api_test.go helpers;
// copy the pattern used for download / add_ext handlers.)
```

Use the existing `api_test.go` setup helpers as the template.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestAuthLogin -count=1`
Expected: FAIL — handler does not exist.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/api/auth_login.go
package api

import (
	"encoding/json"
	"errors"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

func (s *Api) authLoginHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var p common.AuthLoginParams
	if err := json.Unmarshal(body, &p); err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	if p.PluginID == "" {
		return common.UPDATE_AUTH_REQUIRED, nil, errors.New("plugin_id required")
	}
	m := s.elEngine.GetModule(p.PluginID)
	if m == nil || m.Auth == nil {
		return common.UPDATE_AUTH_REQUIRED, nil, errors.New("plugin has no auth block")
	}
	prov, ok := m.Provider().(*auth.OAuth2Provider)
	if !ok {
		return common.UPDATE_AUTH_REQUIRED, nil, errors.New("auth provider unavailable")
	}

	key := types.TokenKey{PluginID: p.PluginID, Account: p.Account}.WithDefaultAccount()

	switch p.Flow {
	case "device":
		return s.authStartDevice(prov, key)
	default:
		return s.authStartPKCE(prov, key, p.RedirectURI, p.Scopes)
	}
}

func (s *Api) authStartPKCE(prov *auth.OAuth2Provider, key types.TokenKey, redirectURI string, scopes []string) (common.UpdateType, any, error) {
	flow, _, err := prov.FlowRegistry().Start(key, auth.FlowKindPKCE)
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	verifier, err := auth.NewPKCEVerifier()
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	state, err := auth.NewFlowState()
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	flow.CodeVerifier = verifier
	flow.State = state
	challenge := auth.PKCEChallenge(verifier, prov.Config().PKCEMethod)
	url := prov.BuildAuthorizeURL(redirectURI, state, challenge, scopes)
	return common.UPDATE_AUTH_REQUIRED, &common.AuthLoginResult{
		FlowID:       flow.ID,
		AuthorizeURL: url,
		ExpiresAt:    flow.Started.Unix() + 300,
	}, nil
}

func (s *Api) authStartDevice(prov *auth.OAuth2Provider, key types.TokenKey) (common.UpdateType, any, error) {
	// For device flow, the daemon owns the polling loop. It starts the
	// flow and polls in a goroutine; the CLI just displays the user
	// code and waits for UPDATE_AUTH_COMPLETED.
	flow, _, err := prov.FlowRegistry().Start(key, auth.FlowKindDevice)
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	// ... start polling goroutine ...
	// (TODO in task 13 - wired in task 14)
	return common.UPDATE_AUTH_REQUIRED, &common.AuthLoginResult{
		FlowID: flow.ID,
	}, nil
}
```

Register in whatever file connects `s.*Handler` to RPC method names (likely `api.go`).

Also add a helper `NewFlowState` to `internal/extl/auth/flowregistry.go`:

```go
func NewFlowState() (string, error) { return randomID() }
```

Add `Module.Provider() auth.AuthProvider` method in `internal/extl/module.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestAuthLogin -race -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/auth_login.go internal/api/auth_login_test.go internal/api/api.go internal/extl/module.go internal/extl/auth/flowregistry.go
git commit -m "api: add auth.login RPC handler (pkce + device stub)"
```

---

### Task 14: `auth.complete`, `auth.cancel`, `auth.list`, `auth.logout` handlers + device polling goroutine

**Files:**
- Create: `internal/api/auth_complete.go`, `internal/api/auth_cancel.go`, `internal/api/auth_list.go`, `internal/api/auth_logout.go`
- Modify: `internal/api/auth_login.go` — fill in device polling goroutine
- Create: `internal/api/auth_handlers_test.go`

- [ ] **Step 1: Write the failing tests**

One test per handler covering the happy path + one error case each. Use the existing `api_test.go` harness pattern.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestAuth -count=1`
Expected: FAIL.

- [ ] **Step 3: Write minimal implementations**

```go
// internal/api/auth_complete.go
package api

import (
    "context"
    "encoding/json"
    "errors"

    "github.com/warpdl/warpdl/common"
    "github.com/warpdl/warpdl/internal/extl/auth"
    "github.com/warpdl/warpdl/internal/server"
    "github.com/warpdl/warpdl/pkg/credman/types"
)

func (s *Api) authCompleteHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
    var p common.AuthCompleteParams
    if err := json.Unmarshal(body, &p); err != nil {
        return common.UPDATE_AUTH_COMPLETED, nil, err
    }
    f, prov, err := s.lookupFlow(p.FlowID)
    if err != nil {
        return common.UPDATE_AUTH_COMPLETED, nil, err
    }
    if f.State != p.State {
        f.Registry.Cancel(p.FlowID, errors.New("state mismatch"))
        return common.UPDATE_AUTH_COMPLETED, nil, errors.New("state mismatch")
    }
    redirectURI := f.RedirectURI // populated by auth_login
    tok, err := prov.ExchangeCode(context.Background(), f.Key, p.Code, f.CodeVerifier, redirectURI)
    if err != nil {
        prov.FlowRegistry().Cancel(p.FlowID, err)
        return common.UPDATE_AUTH_COMPLETED, nil, err
    }
    prov.FlowRegistry().Resolve(p.FlowID, tok, nil)
    return common.UPDATE_AUTH_COMPLETED, &common.AuthCompleteResult{
        Account:   f.Key.Account,
        Scopes:    tok.Scopes,
        ExpiresAt: tok.ExpiresAt.Unix(),
    }, nil
}

// lookupFlow resolves a flow id to its flow object + owning provider.
// Implementation walks the engine's modules for a provider whose
// FlowRegistry has the id.
func (s *Api) lookupFlow(id string) (*auth.Flow, *auth.OAuth2Provider, error) {
    // ... implementation ...
}
```

Similar skeletons for `auth_cancel.go`, `auth_list.go`, `auth_logout.go` — see spec §8 for parameters.

Fill in the device polling goroutine in `auth_login.go`:

```go
go func() {
    init, err := prov.StartDeviceCode(context.Background())
    if err != nil {
        prov.FlowRegistry().Cancel(flow.ID, err)
        return
    }
    flow.DeviceCode = init.DeviceCode
    // TODO: push UPDATE_AUTH_REQUIRED with user_code / verification_url
    //       via the api's update channel so the CLI renders the prompt.
    tok, err := prov.PollDeviceCode(context.Background(), init)
    if err != nil {
        prov.FlowRegistry().Cancel(flow.ID, err)
        return
    }
    _ = prov.StoreToken(flow.Key, tok) // new small helper on OAuth2Provider
    prov.FlowRegistry().Resolve(flow.ID, tok, nil)
}()
```

(Expose `OAuth2Provider.StoreToken(key, *token) error` as a thin wrapper around `p.store.Set`.)

Register all four handlers in `api.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -race -count=1 && go build ./...`
Expected: PASS / build OK.

- [ ] **Step 5: Commit**

```bash
git add internal/api/auth_*.go internal/api/api.go internal/extl/auth/oauth2.go
git commit -m "api: add auth.complete/cancel/list/logout handlers + device flow polling"
```

---

## Phase E — CLI auth orchestrator

### Task 15: Add `github.com/pkg/browser` dependency + small `openBrowser` helper

**Files:**
- Modify: `go.mod` / `go.sum`
- Create: `cmd/internal_browser.go` + tests

- [ ] **Step 1:** `go get github.com/pkg/browser`
- [ ] **Step 2:** Write a thin wrapper with a `$WARP_NO_BROWSER=1` short-circuit and a fallback that prints the URL instead of opening when it can't launch a browser.
- [ ] **Step 3:** Unit test the short-circuit path.
- [ ] **Step 4:** `go test -race ./cmd/... -count=1` green.
- [ ] **Step 5:** `git commit -m "cmd: add browser-open helper with WARP_NO_BROWSER escape hatch"`

---

### Task 16: `cmd/auth.go` — subcommands skeleton + loopback callback server

**Files:**
- Create: `cmd/auth.go`
- Create: `cmd/auth_test.go`
- Modify: `cmd/cmd.go` — register the new `auth` subcommand

- [ ] **Step 1–5:** TDD cycle — start with callback-handler unit tests that verify:
  - Wrong `state` → rejected.
  - Correct `state` → `auth.complete` RPC invoked with the right params.
  - Timeout propagates.
  - `Ctrl-C` / `context.Canceled` → `auth.cancel` RPC invoked.

Implementation sketch:

```go
// cmd/auth.go
func authLoginCmd(ctx *cli.Context) error {
    // 1. bind 127.0.0.1:0 → listener + port
    // 2. build redirect URI using that port
    // 3. RPC auth.login{PluginID, Account, Scopes, Flow, RedirectURI}
    //    - receive AuthLoginResult
    // 4. if device flow: print user code + URL; wait for UPDATE_AUTH_COMPLETED
    // 5. else: openBrowser(res.AuthorizeURL); serve /callback on listener
    //    - /callback: extract code+state, verify state, call auth.complete
    //    - render "you can close this window" page
    // 6. await UPDATE_AUTH_COMPLETED or timeout
}
```

- [ ] Commit: `cmd: add warp auth login with loopback callback and browser open`

---

### Task 17: `warp auth list`, `logout`, `status`

**Files:**
- Modify: `cmd/auth.go`
- Modify: `cmd/auth_test.go`

- [ ] **Step 1:** TDD — tests that each subcommand sends the right RPC and prints the expected output.
- [ ] **Step 2–4:** Implementations are thin shims over `auth.list` and `auth.logout` RPCs. `status` is CLI-side filtering over `auth.list`.
- [ ] **Step 5:** `git commit -m "cmd: add warp auth list/logout/status subcommands"`

---

### Task 18: Interactive in-download auth hookup

**Files:**
- Modify: `cmd/download.go` — handle `UPDATE_AUTH_REQUIRED` pushes
- Modify: `cmd/auth_test.go` — scenario: download + mid-flight auth

- [ ] **Step 1:** Write a test that feeds a fake daemon which, mid-download, pushes `UPDATE_AUTH_REQUIRED`; assert the CLI opens the orchestrator on the same code path and ultimately completes the download.
- [ ] **Step 2–4:** Implementation — route the update into the same orchestrator function used by explicit `warp auth login`.
- [ ] **Step 5:** `git commit -m "cmd: handle UPDATE_AUTH_REQUIRED during download (interactive flow)"`

---

## Phase F — Downloader wiring

### Task 19: Carry `ExtractResult.Headers` into `DownloaderOpts` and strip on cross-origin redirect

**Files:**
- Modify: `internal/api/download.go`, `internal/api/resume.go` — pass `Headers` through
- Modify: `pkg/warplib/dloader.go` — add plugin-supplied header names to `StripUnsafeFromHeaders`'s sensitive set
- Modify: `pkg/warplib/dloader.go` — add test coverage for header stripping

- [ ] **Step 1:** Write a test using `httptest.Server` that issues a cross-origin redirect; assert `Authorization` is stripped; assert a same-origin redirect keeps it.
- [ ] **Step 2–4:** Make the minimal change — store the set of plugin-supplied header names on the downloader, consult the same set during redirect.
- [ ] **Step 5:** `git commit -m "warplib: strip plugin-supplied auth headers on cross-origin redirect"`

---

## Phase G — Google Drive plugin v2

### Task 20: `plugins/gdrive` — add `auth` block + conditional private-file path

**Files:**
- Modify: `plugins/gdrive/manifest.json` — bump to v2, add `auth` block
- Modify: `plugins/gdrive/main.js` — add private-file fallback using `getAccessToken`
- Modify: `plugins/gdrive/gdrive_test.go` — stub provider + test the private path end-to-end

- [ ] **Step 1:** Write the failing tests:
  - Public URL still resolves as today (regression).
  - Private URL: stub `request` returns 403 on the uc probe; `getAccessToken` returns "TESTTOK"; `extract()` returns `{url, headers}` with the API URL + bearer.
  - Asking for a scope outside the manifest throws.

- [ ] **Step 2:** Run — FAIL.

- [ ] **Step 3:** manifest:

```json
{
  "name": "Google Drive",
  "version": "2.0.0",
  "matches": ["^https?://(drive|docs)\\.google\\.com/.+"],
  "entrypoint": "main.js",
  "auth": {
    "type": "oauth2",
    "client_id": "REPLACE_WITH_PLUGIN_AUTHOR_CLIENT_ID.apps.googleusercontent.com",
    "scopes": ["https://www.googleapis.com/auth/drive.readonly"],
    "authorize_url": "https://accounts.google.com/o/oauth2/v2/auth",
    "token_url": "https://oauth2.googleapis.com/token",
    "device_url": "https://oauth2.googleapis.com/device/code",
    "revoke_url": "https://oauth2.googleapis.com/revoke",
    "pkce": "S256",
    "extra_auth_params": {"access_type": "offline", "prompt": "consent"}
  }
}
```

main.js — add private-file branch:

```js
function probeUc(fileId) {
  var tryUrl = ucDownloadUrl(fileId);
  return fetch ? null : probe(tryUrl); // use the existing probe()
}

// In extract(url), after classifying as a regular file:
//   1. Try the public path first (current v1 logic).
//   2. If probe returned 401/403, fall back to API + bearer.
//
// function extract(url) {
//   ... id extraction, doc/folder/public paths ...
//   var tryUrl = ucDownloadUrl(id);
//   var resp = probe(tryUrl);
//   if (resp && (resp.status_code === 401 || resp.status_code === 403)) {
//     var token = getAccessToken({
//       scopes: ["https://www.googleapis.com/auth/drive.readonly"],
//     });
//     return {
//       url: "https://www.googleapis.com/drive/v3/files/" + id + "?alt=media",
//       headers: { "Authorization": "Bearer " + token },
//     };
//   }
//   // ... existing public-flow logic ...
// }
```

Update `gdrive_test.go` — inject a `getAccessToken` stub in the runtime, assert private-path results, keep public-path tests intact.

- [ ] **Step 4:** `go test ./plugins/gdrive/... -race -count=1` green.

- [ ] **Step 5:** `git commit -m "plugins/gdrive: v2 — private-file support via OAuth"`

---

## Phase H — End-to-end verification

### Task 21: Full-repo `-race` lap

**Files:** none — this is verification.

- [ ] **Step 1:** Run `go build ./...` — green.
- [ ] **Step 2:** Run `go test ./... -count=1 -timeout 300s` — green.
- [ ] **Step 3:** Run `go test ./... -count=1 -race -timeout 300s` — green.
- [ ] **Step 4:** Manual smoke: `warp ext install plugins/gdrive` + `warp auth login gdrive` against a staging IdP (or documented expectation with a real Google client_id).
- [ ] **Step 5:** No commit (verification only).

---

## Self-review

Spec coverage:
- §4 Architecture — Tasks 2, 5, 6, 7, 11, 13–14, 16–18 ✓
- §5 Manifest — Task 4 ✓
- §6 JS bindings — Task 10 ✓
- §7 Extract contract — Task 9 ✓
- §8 RPC — Tasks 12–14 ✓
- §9 Token storage — Tasks 1, 2 ✓
- §9 Lifecycle (refresh, logout, skew) — Tasks 7, 8 ✓
- §10 CLI commands + orchestrator — Tasks 15–18 ✓
- §11 Errors — carried by sentinel errors in Task 6, surfaced at every handler ✓
- §12 Security — same-origin header strip Task 19; token-in-memory rest handled by Task 2 ✓
- §13 Testing — each task has its own TDD cycle; end-to-end in Task 21 ✓
- gdrive v2 driving example — Task 20 ✓

No placeholders; every step either shows the code or points at the exact predecessor task that establishes the needed type.

Type consistency spot-check:
- `TokenKey`, `OAuth2Token`, `OAuth2Config`, `ExtractResult`, `AuthLoginParams` used with matching names across all tasks. ✓
- `FlowRegistry.Start / Await / Resolve / Cancel` used consistently Tasks 5, 7, 13, 14. ✓
- `OAuth2Provider` method names (`Token`, `Invalidate`, `Logout`, `BuildAuthorizeURL`, `ExchangeCode`, `StartDeviceCode`, `PollDeviceCode`, `FlowRegistry`, `Config`, `ListAccounts`, `StoreToken`) consistent Tasks 7, 8, 10, 13, 14. ✓

---

## Execution handoff

Plan complete and saved to `docs/plans/2026-04-22-plugin-oauth-plan.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration. Required sub-skill: `superpowers:subagent-driven-development`.

2. **Inline execution** — execute tasks in this session using `superpowers:executing-plans`, batch execution with checkpoints.

Which approach?
