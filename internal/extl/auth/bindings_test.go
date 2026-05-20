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

func TestParseOpts(t *testing.T) {
	rt := goja.New()
	acc, scopes, err := parseOpts(rt, goja.Undefined())
	if err != nil || acc != "" || scopes != nil {
		t.Fatalf("undefined: acc=%q scopes=%v err=%v", acc, scopes, err)
	}
	v, err := rt.RunString(`({account: "work", scopes: ["a","b"]})`)
	if err != nil {
		t.Fatal(err)
	}
	acc, scopes, err = parseOpts(rt, v)
	if err != nil || acc != "work" || len(scopes) != 2 {
		t.Fatalf("object: acc=%q scopes=%v err=%v", acc, scopes, err)
	}
	if _, _, err := parseOpts(rt, rt.ToValue("nope")); err == nil {
		t.Fatal("expected error for non-object options")
	}
	v, err = rt.RunString(`({scopes: 123})`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseOpts(rt, v); err == nil {
		t.Fatal("expected error for non-string-array scopes")
	}
}

func TestBindingFetchWithAuthRequiresRequest(t *testing.T) {
	rt, _, _ := setupRuntime(t)
	if _, err := rt.RunString(`fetchWithAuth({url: "https://example.com"}, {})`); err == nil {
		t.Fatal("expected error when request() is not installed")
	}
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

func TestBindingFetchWithAuthExists(t *testing.T) {
	rt, _, _ := setupRuntime(t)
	v, err := rt.RunString(`typeof fetchWithAuth`)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "function" {
		t.Fatalf("fetchWithAuth type=%s", v.String())
	}
}

var _ = context.Background
