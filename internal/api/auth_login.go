package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// authLoginHandler starts a new authentication flow for a plugin. It is
// the RPC entry point used by the CLI's `warpdl auth login` command and
// by any plugin-initiated login prompt surfaced through the CLI.
//
// The response type is tagged UPDATE_AUTH_REQUIRED for consistency with
// the server-pushed event that the daemon emits when a plugin's
// getAccessToken call finds no token. CLI code can therefore decode the
// same payload type for both paths.
//
// For the PKCE flow the handler returns the authorize URL the CLI must
// open in a browser, plus a FlowID the CLI later uses to POST the
// authorization code to auth.complete (Task 14).
//
// For the device flow the handler registers a flow and returns a
// FlowID; the polling goroutine that produces a user_code/verification_url
// lands in Task 14 and will push a second UPDATE_AUTH_REQUIRED event
// once the upstream device endpoint responds.
func (s *Api) authLoginHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var p common.AuthLoginParams
	if err := json.Unmarshal(body, &p); err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	if p.PluginID == "" {
		return common.UPDATE_AUTH_REQUIRED, nil, errors.New("plugin_id required")
	}
	m := s.elEngine.GetModule(p.PluginID)
	if m == nil {
		return common.UPDATE_AUTH_REQUIRED, nil, fmt.Errorf("unknown plugin: %s", p.PluginID)
	}
	if m.Auth == nil {
		return common.UPDATE_AUTH_REQUIRED, nil, fmt.Errorf("plugin %s has no auth block", p.PluginID)
	}
	prov, ok := m.Provider().(*auth.OAuth2Provider)
	if !ok || prov == nil {
		return common.UPDATE_AUTH_REQUIRED, nil, errors.New("auth provider unavailable")
	}

	key := types.TokenKey{PluginID: p.PluginID, Account: p.Account}.WithDefaultAccount()

	switch p.Flow {
	case "device":
		return s.authStartDevice(prov, key)
	default:
		return s.authStartPKCE(prov, key, p.RedirectURI, p.Scopes)
	}
}

// authStartPKCE kicks off a PKCE authorization-code flow: registers a
// flow, generates verifier + state, and returns the authorize URL.
func (s *Api) authStartPKCE(prov *auth.OAuth2Provider, key types.TokenKey, redirectURI string, scopes []string) (common.UpdateType, any, error) {
	flow, _, err := prov.FlowRegistry().Start(key, auth.FlowKindPKCE)
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	verifier, err := auth.NewPKCEVerifier()
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	state, err := auth.NewFlowState()
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	flow.CodeVerifier = verifier
	flow.State = state

	challenge := auth.PKCEChallenge(verifier, prov.Config().PKCEMethod)
	url := prov.BuildAuthorizeURL(redirectURI, state, challenge, scopes)

	return common.UPDATE_AUTH_REQUIRED, &common.AuthLoginResult{
		FlowID:       flow.ID,
		AuthorizeURL: url,
		ExpiresAt:    flow.Started.Unix() + 300,
	}, nil
}

// authStartDevice stubs the device-code flow for Task 13. It registers
// a flow so the CLI has a FlowID to reference, but does not contact the
// upstream device endpoint — that lives in Task 14's polling goroutine.
func (s *Api) authStartDevice(prov *auth.OAuth2Provider, key types.TokenKey) (common.UpdateType, any, error) {
	flow, _, err := prov.FlowRegistry().Start(key, auth.FlowKindDevice)
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	return common.UPDATE_AUTH_REQUIRED, &common.AuthLoginResult{
		FlowID: flow.ID,
	}, nil
}
