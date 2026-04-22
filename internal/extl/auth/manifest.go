package auth

import (
	"fmt"
	"net/url"
)

// OAuth2Config is the `auth` block of a plugin manifest.
type OAuth2Config struct {
	Type            string            `json:"type"`
	ClientID        string            `json:"client_id"`
	ClientSecret    string            `json:"client_secret,omitempty"` // REJECTED; kept for friendly error
	Scopes          []string          `json:"scopes"`
	AuthorizeURL    string            `json:"authorize_url"`
	TokenURL        string            `json:"token_url"`
	DeviceURL       string            `json:"device_url,omitempty"`
	RevokeURL       string            `json:"revoke_url,omitempty"`
	PKCEMethod      string            `json:"pkce,omitempty"`
	ExtraAuthParams map[string]string `json:"extra_auth_params,omitempty"`
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
		return fmt.Errorf("auth: scopes must contain at least one entry")
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
	return nil
}
