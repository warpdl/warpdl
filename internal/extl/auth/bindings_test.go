package auth

import (
	"crypto/rand"
	"path/filepath"
	"strings"
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

func TestBindingFetchWithAuthAddsBearerAndKeepsHeaders(t *testing.T) {
	rt, _, tm := setupRuntime(t)
	k := types.TokenKey{PluginID: "pid", Account: "default"}
	_ = tm.Set(k, &types.OAuth2Token{
		AccessToken: "ABC", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{"a"},
	})

	// The documented wrapper contract: request(req) is called with the stored
	// token as a Bearer header merged into the caller's headers, and the
	// response is returned unchanged for non-401 results.
	if _, err := rt.RunString(`
		var seen = [];
		function request(req) {
			seen.push(req.headers["Authorization"] + ";" + req.headers["X-Trace"]);
			return {status_code: 200, body: "ok"};
		}
		var response = fetchWithAuth(
			{url: "https://api.example.com/me", headers: {"X-Trace": "t"}},
			{scopes: ["a"]}
		);
	`); err != nil {
		t.Fatal(err)
	}
	result, err := rt.RunString(`seen.join(",") + "|" + response.status_code + "|" + response.body`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.String(), "Bearer ABC;t|200|ok"; got != want {
		t.Fatalf("fetchWithAuth result = %q, want %q", got, want)
	}
}

func TestBindingFetchWithAuthRequiresRequestGlobal(t *testing.T) {
	rt, _, _ := setupRuntime(t)
	_, err := rt.RunString(`fetchWithAuth({url: "https://api.example.com/me"}, {scopes: ["a"]})`)
	if err == nil || !strings.Contains(err.Error(), "request() not available") {
		t.Fatalf("fetchWithAuth without request() = %v, want 'request() not available'", err)
	}
}
