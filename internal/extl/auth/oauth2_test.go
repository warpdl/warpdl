package auth

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func newInsecureTLSClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: tr}
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
