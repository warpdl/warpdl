package api

import (
	"context"
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
//
// If the registry Start call returned joined=true, a previous caller
// (e.g. a plugin's triggerFlowAndAwait path) has already committed the
// flow's PKCE verifier, CSRF state, and redirect URI. We MUST echo
// those back instead of overwriting them, otherwise a second RPC call
// would (a) break the original authorize URL commitment and (b) allow
// any subsequent caller to rotate the CSRF state on a live flow.
func (s *Api) authStartPKCE(prov *auth.OAuth2Provider, key types.TokenKey, redirectURI string, scopes []string) (common.UpdateType, any, error) {
	flow, joined, err := prov.FlowRegistry().Start(key, auth.FlowKindPKCE)
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	var verifier, state string
	if joined {
		verifier = flow.CodeVerifier
		state = flow.State
		// Keep the original redirect URI — don't let a second caller
		// redirect-hijack the flow.
		redirectURI = flow.RedirectURI
	} else {
		verifier, err = auth.NewPKCEVerifier()
		if err != nil {
			return common.UPDATE_AUTH_REQUIRED, nil, err
		}
		state, err = auth.NewFlowState()
		if err != nil {
			return common.UPDATE_AUTH_REQUIRED, nil, err
		}
		flow.CodeVerifier = verifier
		flow.State = state
		flow.RedirectURI = redirectURI
	}

	challenge := auth.PKCEChallenge(verifier, prov.Config().PKCEMethod)
	url := prov.BuildAuthorizeURL(redirectURI, state, challenge, scopes)

	return common.UPDATE_AUTH_REQUIRED, &common.AuthLoginResult{
		FlowID:       flow.ID,
		AuthorizeURL: url,
		ExpiresAt:    flow.Started.Unix() + 300,
	}, nil
}

// authStartDevice kicks off an RFC 8628 device-code flow. It hits the
// upstream /device/code endpoint synchronously so the CLI can receive
// the user_code/verification_url in its response, and spawns a polling
// goroutine that resolves the registry flow once the user completes
// authorization (or cancels / times out).
//
// The goroutine uses the daemon-lifetime context (not a per-RPC one)
// so that a short-lived RPC deadline does not kill the poll
// prematurely; daemon shutdown WILL cancel it, bounding goroutine
// lifetime at process lifetime. Flow timeout enforcement also lives
// in FlowRegistry.
//
// If the registry Start call returned joined=true, a concurrent
// caller has already issued the /device/code request and launched
// the polling goroutine; we simply echo the cached init result to
// keep the second caller in sync with the first — avoiding a second
// /device/code hit AND a duplicate polling goroutine.
func (s *Api) authStartDevice(prov *auth.OAuth2Provider, key types.TokenKey) (common.UpdateType, any, error) {
	flow, joined, err := prov.FlowRegistry().Start(key, auth.FlowKindDevice)
	if err != nil {
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}

	if joined {
		// A polling goroutine is already running; just echo the
		// previously-captured upstream state.
		return common.UPDATE_AUTH_REQUIRED, &common.AuthLoginResult{
			FlowID:          flow.ID,
			DeviceCode:      flow.DeviceCode,
			UserCode:        flow.UserCode,
			VerificationURL: flow.VerificationURL,
			Interval:        flow.Interval,
			ExpiresAt:       flow.Started.Unix() + int64(flow.DeviceExpiresIn),
		}, nil
	}

	init, err := prov.StartDeviceCode(s.daemonCtx())
	if err != nil {
		prov.FlowRegistry().Cancel(flow.ID, err)
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}
	// Stash the upstream device code + init metadata on the flow so
	// observers (and joined callers) can correlate later
	// pushes/completions without re-hitting /device/code.
	flow.DeviceCode = init.DeviceCode
	flow.UserCode = init.UserCode
	flow.VerificationURL = init.VerificationURL
	flow.Interval = init.Interval
	flow.DeviceExpiresIn = init.ExpiresIn

	if !s.goBackground(func(ctx context.Context) {
		s.runDeviceFlow(ctx, prov, flow, init)
	}) {
		err := context.Canceled
		prov.FlowRegistry().Cancel(flow.ID, err)
		return common.UPDATE_AUTH_REQUIRED, nil, err
	}

	return common.UPDATE_AUTH_REQUIRED, &common.AuthLoginResult{
		FlowID:          flow.ID,
		DeviceCode:      init.DeviceCode,
		UserCode:        init.UserCode,
		VerificationURL: init.VerificationURL,
		Interval:        init.Interval,
		ExpiresAt:       flow.Started.Unix() + int64(init.ExpiresIn),
	}, nil
}

// runDeviceFlow polls the IdP until the user completes the flow, the
// daemon shuts down, or the flow is cancelled. Extracted from
// authStartDevice so there is a single owner of the polling
// goroutine (avoids a subtle duplicate-goroutine bug when Start
// returns joined=true).
func (s *Api) runDeviceFlow(ctx context.Context, prov *auth.OAuth2Provider, flow *auth.Flow, init *auth.DeviceAuthorization) {
	tok, err := prov.PollDeviceCode(ctx, init)
	if err != nil {
		prov.FlowRegistry().Cancel(flow.ID, err)
		return
	}
	if err := prov.StoreToken(flow.Key, tok); err != nil {
		prov.FlowRegistry().Cancel(flow.ID, err)
		return
	}
	prov.FlowRegistry().Resolve(flow.ID, tok, nil)
}

// lookupFlow walks all registered modules searching for the owner of
// flowID. Returns the flow and its provider, or an error if no module's
// FlowRegistry knows about it. Used by auth.complete and auth.cancel
// which receive a FlowID without a plugin context.
func (s *Api) lookupFlow(flowID string) (*auth.Flow, *auth.OAuth2Provider, error) {
	for _, info := range s.elEngine.ListModules(false) {
		m := s.elEngine.GetModule(info.ExtensionId)
		if m == nil {
			continue
		}
		prov, ok := m.Provider().(*auth.OAuth2Provider)
		if !ok || prov == nil {
			continue
		}
		if f := prov.FlowRegistry().Get(flowID); f != nil {
			return f, prov, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown flow: %s", flowID)
}
