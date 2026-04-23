package api

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/internal/server"
)

// authCompleteHandler finishes a PKCE flow by exchanging the
// authorization code delivered from the CLI's loopback callback for an
// OAuth2 token. The device flow does NOT go through this handler — it
// self-polls the upstream /token endpoint from a goroutine started in
// authStartDevice.
//
// On success the stored token is persisted by ExchangeCode (which goes
// through the TokenManager), the flow is Resolved so any awaiter
// wakes up, and the account + granted scopes + expiry are returned.
//
// On state mismatch or exchange failure the flow is Cancelled so
// awaiters observe the error, and the error is propagated to the
// caller.
func (s *Api) authCompleteHandler(sconn *server.SyncConn, pool *server.Pool, body json.RawMessage) (common.UpdateType, any, error) {
	var p common.AuthCompleteParams
	if err := json.Unmarshal(body, &p); err != nil {
		return common.UPDATE_AUTH_COMPLETED, nil, err
	}
	if p.FlowID == "" {
		return common.UPDATE_AUTH_COMPLETED, nil, errors.New("flow_id required")
	}

	flow, prov, err := s.lookupFlow(p.FlowID)
	if err != nil {
		return common.UPDATE_AUTH_COMPLETED, nil, err
	}

	// auth.complete is only valid for PKCE flows. Device flows resolve
	// themselves from the polling goroutine started in authStartDevice
	// and must not be "completed" by an RPC caller here — accepting
	// one would bypass the /token exchange's device_code proof.
	if flow.Kind != auth.FlowKindPKCE {
		return common.UPDATE_AUTH_COMPLETED, nil, fmt.Errorf("flow %s is not a PKCE flow", flow.ID)
	}

	// CSRF guard: the state echoed back from the IdP must match the
	// state we stamped on the authorize URL. Mismatch (or an empty
	// state on our end, which would make "" == "" trivially pass) →
	// cancel the flow and reject, since accepting the code would
	// enable a cross-site login attack on the CLI user.
	if flow.State == "" || flow.State != p.State {
		mismatch := errors.New("state mismatch")
		prov.FlowRegistry().Cancel(flow.ID, mismatch)
		return common.UPDATE_AUTH_COMPLETED, nil, mismatch
	}

	tok, err := prov.ExchangeCode(s.daemonCtx(), flow.Key, p.Code, flow.CodeVerifier, flow.RedirectURI)
	if err != nil {
		prov.FlowRegistry().Cancel(flow.ID, err)
		return common.UPDATE_AUTH_COMPLETED, nil, fmt.Errorf("exchange code: %w", err)
	}
	prov.FlowRegistry().Resolve(flow.ID, tok, nil)

	return common.UPDATE_AUTH_COMPLETED, &common.AuthCompleteResult{
		Account:   flow.Key.Account,
		Scopes:    tok.Scopes,
		ExpiresAt: tok.ExpiresAt.Unix(),
	}, nil
}
