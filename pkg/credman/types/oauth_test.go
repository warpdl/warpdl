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
