package types

import "time"

// OAuth2Token is a stored OAuth 2.0 token bundle. Persisted (encrypted)
// by credman.TokenManager.
type OAuth2Token struct {
	// AccessToken is the bearer token used to authorize API calls.
	AccessToken string
	// RefreshToken is the long-lived token used to obtain new access tokens.
	RefreshToken string
	// TokenType is the token's type (e.g. "Bearer").
	TokenType string
	// ExpiresAt is the absolute timestamp at which AccessToken becomes invalid.
	ExpiresAt time.Time
	// Scopes are the OAuth scopes granted with this token.
	Scopes []string
	// IDToken is the optional OpenID Connect ID token.
	IDToken string
	// IssuedAt is the timestamp at which this token bundle was issued.
	IssuedAt time.Time
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
	// PluginID is the identifier of the plugin that owns the token.
	PluginID string
	// Account is the per-plugin account label used to distinguish multiple
	// tokens for the same plugin.
	Account string
}

// WithDefaultAccount fills Account with "default" if it was empty.
func (k TokenKey) WithDefaultAccount() TokenKey {
	if k.Account == "" {
		k.Account = "default"
	}
	return k
}
