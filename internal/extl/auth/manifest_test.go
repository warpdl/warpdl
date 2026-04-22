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
