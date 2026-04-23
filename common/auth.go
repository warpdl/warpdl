package common

// AuthLoginParams starts a new auth flow for a plugin.
type AuthLoginParams struct {
	PluginID    string   `json:"plugin_id"`
	Account     string   `json:"account,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	Flow        string   `json:"flow,omitempty"`         // "pkce" | "device"
	RedirectURI string   `json:"redirect_uri,omitempty"` // loopback URL for PKCE
}

// AuthLoginResult is the daemon's response to a login-start. Only the
// fields relevant to the requested flow type are populated.
type AuthLoginResult struct {
	FlowID          string `json:"flow_id"`
	AuthorizeURL    string `json:"authorize_url,omitempty"`
	DeviceCode      string `json:"device_code,omitempty"`
	UserCode        string `json:"user_code,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	Interval        int    `json:"interval,omitempty"` // seconds between device-flow polls
	ExpiresAt       int64  `json:"expires_at"`         // unix seconds
}

// AuthCompleteParams delivers the authorization code from the CLI
// loopback callback to the daemon (PKCE flow only).
type AuthCompleteParams struct {
	FlowID string `json:"flow_id"`
	Code   string `json:"code"`
	State  string `json:"state"`
}

// AuthCompleteResult confirms the flow succeeded and returns the
// account/scopes the token was issued under.
type AuthCompleteResult struct {
	Account   string   `json:"account"`
	Scopes    []string `json:"scopes"`
	ExpiresAt int64    `json:"expires_at"`
}

// AuthCancelParams aborts an in-flight flow.
type AuthCancelParams struct {
	FlowID string `json:"flow_id"`
}

// AuthAccount is one row in the account listing.
type AuthAccount struct {
	PluginID  string   `json:"plugin_id"`
	Account   string   `json:"account"`
	Scopes    []string `json:"scopes"`
	ExpiresAt int64    `json:"expires_at"`
}

// AuthListResult enumerates stored credentials.
type AuthListResult struct {
	Accounts []AuthAccount `json:"accounts"`
}

// AuthLogoutParams names a (plugin, account) pair to forget.
type AuthLogoutParams struct {
	PluginID string `json:"plugin_id"`
	Account  string `json:"account,omitempty"`
}
