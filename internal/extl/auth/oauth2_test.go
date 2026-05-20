package auth

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

type stubIdP struct {
	server           *httptest.Server
	tokenHits        atomic.Int32
	lastGrant        string
	lastCode         string
	lastVerifier     string
	lastRefreshToken string
	lastClientID     string
	lastClientSecret string
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
		s.lastClientID = r.FormValue("client_id")
		s.lastClientSecret = r.FormValue("client_secret")
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

func newInsecureTLSClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: tr}
}

func makeTestProvider(t *testing.T, tokenURL, revokeURL string) (*OAuth2Provider, *credman.TokenManager) {
	return makeTestProviderWithSecret(t, tokenURL, revokeURL, "")
}

func makeTestProviderWithSecret(t *testing.T, tokenURL, revokeURL, clientSecret string) (*OAuth2Provider, *credman.TokenManager) {
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
		ClientSecret: clientSecret,
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
	p.client = newInsecureTLSClient()
	return p, tm
}

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
	// Ask for a scope the stored token doesn't cover. Because "drive.write"
	// is not in the manifest scope set either, scopesAllowed will reject
	// it with ErrScopeDenied before any flow attempt.
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

func TestRefreshIsSerialized(t *testing.T) {
	var tokenHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenHits.Add(1)
		// Small sleep to widen the race window.
		time.Sleep(30 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "AT-NEW",
			"refresh_token": "RT-NEW",
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
		AuthorizeURL: "https://example.com/a",
		TokenURL:     srv.URL + "/token",
		PKCEMethod:   "S256",
	}
	cfg, _ = NormalizeOAuth2Config(cfg)
	p := NewOAuth2Provider("pid", cfg, tm, NewFlowRegistry(time.Minute))
	p.client = newInsecureTLSClient()

	tk := types.TokenKey{PluginID: "pid"}
	_ = tm.Set(tk, &types.OAuth2Token{
		AccessToken:  "OLD",
		RefreshToken: "RT-OLD",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Scopes:       []string{"drive.readonly"},
	})

	var wg sync.WaitGroup
	var errCnt atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Token(context.Background(), tk, []string{"drive.readonly"})
			if err != nil {
				errCnt.Add(1)
			}
		}()
	}
	wg.Wait()

	if errCnt.Load() != 0 {
		t.Fatalf("%d Token calls failed", errCnt.Load())
	}
	if got := tokenHits.Load(); got != 1 {
		t.Fatalf("expected 1 /token hit (refresh serialized), got %d", got)
	}
}

func TestPostTokenRejectsEmptyAccessToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	p, _ := makeTestProvider(t, srv.URL+"/token", "")
	verifier, _ := NewPKCEVerifier()
	_, err := p.ExchangeCode(context.Background(), types.TokenKey{PluginID: "plugin-1"}, "CODE", verifier, "http://127.0.0.1/cb")
	if err == nil {
		t.Fatal("expected error for empty access_token")
	}
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("expected ErrProvider, got %v", err)
	}
}

func TestRefreshPreservesNarrowerScopesWhenServerOmits(t *testing.T) {
	var firstCall atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		// Refresh response omits `scope` — server-side behavior for
		// Google and similar when scopes haven't changed.
		firstCall.Store(true)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "REFRESHED",
			"token_type":   "Bearer",
			"expires_in":   3600,
			// no refresh_token, no scope
		})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	key := make([]byte, 32)
	_, _ = rand.Read(key)
	tm, _ := credman.NewTokenManager(filepath.Join(t.TempDir(), "tokens.gob"), key)
	defer tm.Close()

	// Manifest lists TWO scopes, but user consented only to one.
	cfg := OAuth2Config{
		Type:         "oauth2",
		ClientID:     "c",
		Scopes:       []string{"drive.readonly", "drive.metadata"},
		AuthorizeURL: "https://example.com/a",
		TokenURL:     srv.URL + "/token",
		PKCEMethod:   "S256",
	}
	cfg, _ = NormalizeOAuth2Config(cfg)
	p := NewOAuth2Provider("pid", cfg, tm, NewFlowRegistry(time.Minute))
	p.client = newInsecureTLSClient()

	tk := types.TokenKey{PluginID: "pid"}
	_ = tm.Set(tk, &types.OAuth2Token{
		AccessToken:  "OLD",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Scopes:       []string{"drive.readonly"}, // narrower than manifest
	})

	_, err := p.Token(context.Background(), tk, []string{"drive.readonly"})
	if err != nil {
		t.Fatal(err)
	}
	if !firstCall.Load() {
		t.Fatal("expected token endpoint called")
	}
	stored, _ := tm.Get(tk)
	if len(stored.Scopes) != 1 || stored.Scopes[0] != "drive.readonly" {
		t.Fatalf("stored scopes widened after refresh: %v", stored.Scopes)
	}
}

func TestTokenTransientRefreshFailurePreservesCreds(t *testing.T) {
	var attempt atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		// Return 503 (transient) rather than 4xx invalid_grant.
		attempt.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error": "server_error"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	p, tm := makeTestProvider(t, srv.URL+"/token", "")
	tk := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(tk, &types.OAuth2Token{
		AccessToken:  "OLD",
		RefreshToken: "RT",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Scopes:       []string{"drive.readonly"},
	})

	_, err := p.Token(context.Background(), tk, []string{"drive.readonly"})
	if err == nil {
		t.Fatal("expected error on transient failure")
	}
	// CRITICAL: credentials must still be there.
	stored, getErr := tm.Get(tk)
	if getErr != nil {
		t.Fatalf("credentials nuked on transient failure: %v", getErr)
	}
	if stored.RefreshToken != "RT" {
		t.Fatalf("refresh token lost: %+v", stored)
	}
}

func TestTokenInvalidGrantNukesCreds(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid_grant", "error_description": "Token revoked"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	p, tm := makeTestProvider(t, srv.URL+"/token", "")
	tk := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(tk, &types.OAuth2Token{
		AccessToken:  "OLD",
		RefreshToken: "DEAD",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Scopes:       []string{"drive.readonly"},
	})

	// Put a 10ms timeout on the context so triggerFlowAndAwait returns
	// quickly when it tries to await a flow no one will complete.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := p.Token(ctx, tk, []string{"drive.readonly"})
	if err == nil {
		t.Fatal("expected error")
	}
	// CRITICAL: credentials must be GONE after invalid_grant.
	if _, getErr := tm.Get(tk); getErr == nil {
		t.Fatal("credentials should have been deleted after invalid_grant")
	}
}

func TestLogoutCleansRefreshLock(t *testing.T) {
	p, tm := makeTestProvider(t, "https://example.com/t", "")
	tk := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(tk, &types.OAuth2Token{AccessToken: "A", RefreshToken: "R", ExpiresAt: time.Now().Add(time.Hour)})
	// Warm the mutex map.
	_ = p.muFor(tk.WithDefaultAccount())
	if err := p.Logout(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	// Mutex should be gone.
	if _, loaded := p.refreshLocks.Load(tk.WithDefaultAccount()); loaded {
		t.Fatal("refreshLocks leaked after Logout")
	}
}

func TestTokenCtxCancellationUnblocks(t *testing.T) {
	// Provider has no stored token → triggerFlowAndAwait will block.
	p, _ := makeTestProvider(t, "https://example.com/t", "")
	tk := types.TokenKey{PluginID: "plugin-1"}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := p.Token(ctx, tk, []string{"drive.readonly"})
		done <- err
	}()
	// Let it register the flow then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Token did not return after ctx.Cancel")
	}
}

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

// Google Desktop-app OAuth clients require client_secret on /token even
// though the flow is PKCE. Regression test for the "client_secret is
// missing" rejection from Google's token endpoint.
func TestExchangeCodeSendsClientSecretWhenConfigured(t *testing.T) {
	idp := newStubIdP(t, "AT1", "RT1", 3600)
	p, _ := makeTestProviderWithSecret(t, idp.server.URL+"/token", "", "GOCSPX-test-secret")
	verifier, _ := NewPKCEVerifier()
	if _, err := p.ExchangeCode(context.Background(), types.TokenKey{PluginID: "plugin-1"}, "CODE", verifier, "http://127.0.0.1/cb"); err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if idp.lastClientID != "client-id" {
		t.Fatalf("client_id = %q", idp.lastClientID)
	}
	if idp.lastClientSecret != "GOCSPX-test-secret" {
		t.Fatalf("client_secret not sent: got %q", idp.lastClientSecret)
	}
}

// When the manifest supplies no client_secret, pure PKCE must stay pure —
// never inject anything into the form.
func TestExchangeCodeOmitsClientSecretByDefault(t *testing.T) {
	idp := newStubIdP(t, "AT1", "RT1", 3600)
	p, _ := makeTestProvider(t, idp.server.URL+"/token", "")
	verifier, _ := NewPKCEVerifier()
	if _, err := p.ExchangeCode(context.Background(), types.TokenKey{PluginID: "plugin-1"}, "CODE", verifier, "http://127.0.0.1/cb"); err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if idp.lastClientSecret != "" {
		t.Fatalf("client_secret leaked into PKCE flow: %q", idp.lastClientSecret)
	}
}

// Refresh grant must also carry client_secret; Google's /token rejects
// refresh_token grants from Desktop clients without it.
func TestRefreshSendsClientSecretWhenConfigured(t *testing.T) {
	idp := newStubIdP(t, "AT-REFRESHED", "RT-ROTATED", 3600)
	p, tm := makeTestProviderWithSecret(t, idp.server.URL+"/token", "", "GOCSPX-test-secret")
	key := types.TokenKey{PluginID: "plugin-1"}
	_ = tm.Set(key, &types.OAuth2Token{
		AccessToken:  "OLD",
		RefreshToken: "RT-OLD",
		ExpiresAt:    time.Now().Add(-time.Minute),
		Scopes:       []string{"drive.readonly"},
	})
	if _, err := p.Token(context.Background(), key, []string{"drive.readonly"}); err != nil {
		t.Fatalf("Token: %v", err)
	}
	if idp.lastGrant != "refresh_token" {
		t.Fatalf("grant_type = %q", idp.lastGrant)
	}
	if idp.lastClientSecret != "GOCSPX-test-secret" {
		t.Fatalf("client_secret not sent on refresh: got %q", idp.lastClientSecret)
	}
}

// Device-code grant (RFC 8628) runs through postToken too; client_secret
// must be included when configured.
func TestDeviceCodeGrantSendsClientSecretWhenConfigured(t *testing.T) {
	var tokenCalls atomic.Int32
	var seenSecret atomic.Value
	seenSecret.Store("")
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
		_ = r.ParseForm()
		seenSecret.Store(r.FormValue("client_secret"))
		tokenCalls.Add(1)
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
		ClientSecret: "GOCSPX-device-secret",
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := p.PollDeviceCode(ctx, init); err != nil {
		t.Fatalf("PollDeviceCode: %v", err)
	}
	if got := seenSecret.Load().(string); got != "GOCSPX-device-secret" {
		t.Fatalf("client_secret not sent on device_code grant: got %q", got)
	}
	if tokenCalls.Load() == 0 {
		t.Fatal("/token never called")
	}
}

// A caller that has already set client_secret on the form (future-proof
// against exotic grants) must not be overwritten by the manifest's value.
func TestStartDeviceCodeUnsupported(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/t", "")
	p.cfg.DeviceURL = ""
	if _, err := p.StartDeviceCode(context.Background()); err == nil {
		t.Fatal("expected error when device flow is not configured")
	}
}

func TestNewPKCEVerifier(t *testing.T) {
	v, err := NewPKCEVerifier()
	if err != nil {
		t.Fatal(err)
	}
	if len(v) < 20 {
		t.Fatalf("verifier too short: %q", v)
	}
}

func TestResolveScopes(t *testing.T) {
	t.Parallel()
	if got := resolveScopes([]string{"a"}, []string{"x", "y"}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("request scopes: got %v", got)
	}
	if got := resolveScopes(nil, []string{"x", "y"}); len(got) != 2 {
		t.Fatalf("manifest scopes: got %v", got)
	}
}

func TestTriggerFlowAndAwait_ImmediateCancel(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/t", "")
	tk := types.TokenKey{PluginID: "plugin-1"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Token(ctx, tk, []string{"drive.readonly"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Token err = %v", err)
	}
}

func TestTriggerFlowAndAwait_ResolveError(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/t", "")
	tk := types.TokenKey{PluginID: "plugin-1"}

	go func() {
		time.Sleep(20 * time.Millisecond)
		f, _, err := p.flows.Start(tk, FlowKindPKCE)
		if err != nil || f == nil {
			return
		}
		p.flows.Resolve(f.ID, nil, errors.New("auth denied"))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := p.Token(ctx, tk, []string{"drive.readonly"})
	if err == nil || !strings.Contains(err.Error(), "auth denied") {
		t.Fatalf("Token err = %v", err)
	}
}

func TestPollDeviceCodeSlowDown(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "dev", "user_code": "X", "verification_url": "https://x", "interval": 1,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		n := polls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "AT", "token_type": "Bearer", "expires_in": 3600,
		})
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	p, _ := makeTestProvider(t, srv.URL+"/token", "")
	p.cfg.DeviceURL = srv.URL + "/device/code"
	p.client = newInsecureTLSClient()
	init, err := p.StartDeviceCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tok, err := p.PollDeviceCode(ctx, init)
	if err != nil || tok.AccessToken != "AT" {
		t.Fatalf("PollDeviceCode: tok=%+v err=%v", tok, err)
	}
	if polls.Load() < 2 {
		t.Fatalf("expected at least 2 token polls, got %d", polls.Load())
	}
}

func TestStartDeviceCodeProviderError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	p, _ := makeTestProvider(t, srv.URL+"/token", "")
	p.cfg.DeviceURL = srv.URL + "/device/code"
	p.client = newInsecureTLSClient()
	if _, err := p.StartDeviceCode(context.Background()); err == nil {
		t.Fatal("expected provider error")
	}
}

func TestTriggerFlowAndAwait_Success(t *testing.T) {
	p, tm := makeTestProvider(t, "https://example.com/t", "")
	tk := types.TokenKey{PluginID: "plugin-1"}

	go func() {
		time.Sleep(20 * time.Millisecond)
		f, _, err := p.flows.Start(tk, FlowKindPKCE)
		if err != nil || f == nil {
			return
		}
		p.flows.Resolve(f.ID, &types.OAuth2Token{
			AccessToken: "fresh",
			ExpiresAt:   time.Now().Add(time.Hour),
			Scopes:      []string{"drive.readonly"},
		}, nil)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tok, err := p.Token(ctx, tk, []string{"drive.readonly"})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "fresh" {
		t.Fatalf("token = %q", tok)
	}
	stored, err := tm.Get(tk)
	if err != nil || stored.AccessToken != "fresh" {
		t.Fatalf("stored = %+v err=%v", stored, err)
	}
}

func TestPostTokenPreservesExplicitClientSecret(t *testing.T) {
	idp := newStubIdP(t, "AT", "", 3600)
	p, _ := makeTestProviderWithSecret(t, idp.server.URL+"/token", "", "MANIFEST-SECRET")
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "x")
	form.Set("redirect_uri", "http://127.0.0.1/cb")
	form.Set("client_id", "client-id")
	form.Set("code_verifier", "verifier")
	form.Set("client_secret", "EXPLICIT-SECRET")
	if _, err := p.postToken(context.Background(), form, nil); err != nil {
		t.Fatalf("postToken: %v", err)
	}
	if idp.lastClientSecret != "EXPLICIT-SECRET" {
		t.Fatalf("caller-provided client_secret clobbered: got %q", idp.lastClientSecret)
	}
}
