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
	// ErrInvalidGrant is a definitive rejection of the credential by the
	// identity provider (RFC 6749 errors: invalid_grant, invalid_client,
	// unauthorized_client, invalid_request, unsupported_grant_type).
	// Callers use errors.Is to distinguish these from transient failures
	// (network, 5xx) which should not cause the stored credential to be
	// deleted.
	ErrInvalidGrant = errors.New("identity provider rejected credentials")
)
