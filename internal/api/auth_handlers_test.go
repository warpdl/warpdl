package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/extl/auth"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// insecureTLSClient returns an http.Client that skips TLS verification.
// Used to talk to httptest.NewTLSServer instances from handler tests
// where we can't otherwise wire a test CA into the provider.
func insecureTLSClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &http.Client{Transport: tr}
}

// newStubTokenServer returns an httptest.NewTLSServer that handles
// POST /token by echoing back the provided access/refresh tokens and
// scope string.
func newStubTokenServer(t *testing.T, access, refresh, scope string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  access,
			"refresh_token": refresh,
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         scope,
		})
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// startPKCEFlow calls the login handler and returns the flow id and
// the state the handler stamped on it. Used as the setup step for
// auth.complete tests.
func startPKCEFlow(t *testing.T, api *Api, pluginID string) (string, string, *auth.OAuth2Provider) {
	t.Helper()
	body, _ := json.Marshal(common.AuthLoginParams{
		PluginID:    pluginID,
		RedirectURI: "http://127.0.0.1:54321/callback",
	})
	_, msg, err := api.authLoginHandler(nil, nil, body)
	if err != nil {
		t.Fatalf("authLoginHandler: %v", err)
	}
	res := msg.(*common.AuthLoginResult)

	prov, ok := api.elEngine.GetModule(pluginID).Provider().(*auth.OAuth2Provider)
	if !ok {
		t.Fatalf("provider not *OAuth2Provider")
	}
	f := prov.FlowRegistry().Get(res.FlowID)
	if f == nil {
		t.Fatalf("flow %q not registered", res.FlowID)
	}
	return res.FlowID, f.State, prov
}

func TestAuthCompleteHappyPath(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	// Swap the token endpoint to a local stub; swap the provider's
	// http client so it accepts the httptest self-signed cert.
	stub := newStubTokenServer(t, "AT1", "RT1", "read")
	prov := api.elEngine.GetModule(ext.ModuleId).Provider().(*auth.OAuth2Provider)
	prov.OverrideTokenURL(stub.URL + "/token")
	prov.SetHTTPClient(insecureTLSClient())

	flowID, state, _ := startPKCEFlow(t, api, ext.ModuleId)

	body, _ := json.Marshal(common.AuthCompleteParams{
		FlowID: flowID,
		Code:   "AUTH_CODE",
		State:  state,
	})
	tag, msg, err := api.authCompleteHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("authCompleteHandler: %v", err)
	}
	if tag != common.UPDATE_AUTH_COMPLETED {
		t.Fatalf("tag = %q, want UPDATE_AUTH_COMPLETED", tag)
	}
	res, ok := msg.(*common.AuthCompleteResult)
	if !ok {
		t.Fatalf("expected *AuthCompleteResult, got %T", msg)
	}
	if res.Account != "default" {
		t.Errorf("Account = %q, want default", res.Account)
	}
	if len(res.Scopes) != 1 || res.Scopes[0] != "read" {
		t.Errorf("Scopes = %v, want [read]", res.Scopes)
	}
	if res.ExpiresAt == 0 {
		t.Error("ExpiresAt not populated")
	}

	// The flow must now be resolved: Await returns the token quickly.
	awaitDone := make(chan struct{})
	go func() {
		tok, _ := prov.FlowRegistry().Await(flowID)
		if tok == nil {
			t.Errorf("Await returned nil token")
		}
		close(awaitDone)
	}()
	select {
	case <-awaitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after Resolve")
	}
}

// TestAuthCompleteExchangeFailureCancelsFlow regression-tests the error
// branch where the IdP's /token endpoint fails: the handler must
// propagate the error AND cancel the flow so any awaiter (e.g. a
// plugin Token() call) wakes up with the failure rather than blocking
// until the FlowRegistry timeout.
func TestAuthCompleteExchangeFailureCancelsFlow(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	// /token returns HTTP 500 with an OAuth2 server_error payload.
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	prov := api.elEngine.GetModule(ext.ModuleId).Provider().(*auth.OAuth2Provider)
	prov.OverrideTokenURL(srv.URL + "/token")
	prov.SetHTTPClient(insecureTLSClient())

	flowID, state, _ := startPKCEFlow(t, api, ext.ModuleId)

	body, _ := json.Marshal(common.AuthCompleteParams{
		FlowID: flowID,
		Code:   "AUTH_CODE",
		State:  state,
	})
	_, _, err = api.authCompleteHandler(nil, pool, body)
	if err == nil {
		t.Fatal("expected exchange-failure error, got nil")
	}

	// Flow must be cancelled — a subsequent Await wakes immediately
	// with an error rather than blocking on the registry's timeout.
	awaitDone := make(chan struct {
		tok *types.OAuth2Token
		err error
	}, 1)
	go func() {
		tok, awaitErr := prov.FlowRegistry().Await(flowID)
		awaitDone <- struct {
			tok *types.OAuth2Token
			err error
		}{tok, awaitErr}
	}()
	select {
	case r := <-awaitDone:
		if r.err == nil {
			t.Fatal("expected Await to return exchange error")
		}
		if r.tok != nil {
			t.Fatalf("expected no token after failed exchange, got %+v", r.tok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await did not return after exchange failure; flow was not cancelled")
	}
}

func TestAuthCompleteStateMismatch(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	flowID, state, prov := startPKCEFlow(t, api, ext.ModuleId)

	body, _ := json.Marshal(common.AuthCompleteParams{
		FlowID: flowID,
		Code:   "AUTH_CODE",
		State:  state + "tampered",
	})
	_, _, err = api.authCompleteHandler(nil, pool, body)
	if err == nil {
		t.Fatal("expected state-mismatch error")
	}

	// The flow must have been cancelled — a subsequent Await sees the
	// cancellation error, not the would-be happy result.
	tok, awaitErr := prov.FlowRegistry().Await(flowID)
	if awaitErr == nil {
		t.Fatal("expected Await to return the cancel error")
	}
	if tok != nil {
		t.Fatalf("expected no token after cancel, got %+v", tok)
	}
}

func TestAuthCompleteUnknownFlow(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.AuthCompleteParams{
		FlowID: "does-not-exist",
		Code:   "X",
		State:  "Y",
	})
	_, _, err := api.authCompleteHandler(nil, pool, body)
	if err == nil {
		t.Fatal("expected unknown-flow error")
	}
}

func TestAuthCancel(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}

	flowID, _, prov := startPKCEFlow(t, api, ext.ModuleId)

	body, _ := json.Marshal(common.AuthCancelParams{FlowID: flowID})
	tag, _, err := api.authCancelHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("authCancelHandler: %v", err)
	}
	if tag != common.UPDATE_AUTH_FAILED {
		t.Fatalf("tag = %q, want UPDATE_AUTH_FAILED", tag)
	}

	tok, awaitErr := prov.FlowRegistry().Await(flowID)
	if awaitErr == nil {
		t.Fatal("expected Await to return cancel error")
	}
	if tok != nil {
		t.Fatalf("expected no token, got %+v", tok)
	}
}

func TestAuthCancelUnknownFlow(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.AuthCancelParams{FlowID: "nope"})
	_, _, err := api.authCancelHandler(nil, pool, body)
	if err == nil {
		t.Fatal("expected error for unknown flow")
	}
}

func TestAuthListEmpty(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	// Even with no modules / no tokens the result should be an empty
	// slice, not nil — so JSON renders `[]` and CLI doesn't NPE.
	tag, msg, err := api.authListHandler(nil, pool, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("authListHandler: %v", err)
	}
	if tag != common.UPDATE_AUTH_LIST {
		t.Fatalf("tag = %q, want UPDATE_AUTH_LIST", tag)
	}
	res, ok := msg.(*common.AuthListResult)
	if !ok {
		t.Fatalf("expected *AuthListResult, got %T", msg)
	}
	if res.Accounts == nil {
		t.Fatal("Accounts should be non-nil empty slice")
	}
	if len(res.Accounts) != 0 {
		t.Fatalf("expected empty, got %+v", res.Accounts)
	}
}

// seedToken writes a token directly via the provider's StoreToken
// helper, bypassing the full PKCE flow.
func seedToken(t *testing.T, prov *auth.OAuth2Provider, account string, scopes []string) {
	t.Helper()
	tok := &types.OAuth2Token{
		AccessToken:  "AT-" + account,
		RefreshToken: "RT-" + account,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       scopes,
		IssuedAt:     time.Now(),
	}
	if err := prov.StoreToken(types.TokenKey{Account: account}, tok); err != nil {
		t.Fatalf("StoreToken %q: %v", account, err)
	}
}

func TestAuthListReturnsStoredAccounts(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}
	prov := api.elEngine.GetModule(ext.ModuleId).Provider().(*auth.OAuth2Provider)

	seedToken(t, prov, "alice", []string{"read"})
	seedToken(t, prov, "bob", []string{"read", "write"})

	_, msg, err := api.authListHandler(nil, pool, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("authListHandler: %v", err)
	}
	res := msg.(*common.AuthListResult)
	if len(res.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d: %+v", len(res.Accounts), res.Accounts)
	}

	byAccount := map[string]common.AuthAccount{}
	for _, a := range res.Accounts {
		byAccount[a.Account] = a
	}
	alice, ok := byAccount["alice"]
	if !ok {
		t.Fatal("alice missing")
	}
	if alice.PluginID != ext.ModuleId {
		t.Errorf("alice.PluginID = %q, want %q", alice.PluginID, ext.ModuleId)
	}
	if len(alice.Scopes) != 1 || alice.Scopes[0] != "read" {
		t.Errorf("alice.Scopes = %v, want [read]", alice.Scopes)
	}
	if alice.ExpiresAt == 0 {
		t.Error("alice.ExpiresAt = 0")
	}
	bob, ok := byAccount["bob"]
	if !ok {
		t.Fatal("bob missing")
	}
	if len(bob.Scopes) != 2 {
		t.Errorf("bob.Scopes = %v, want 2 entries", bob.Scopes)
	}
}

func TestAuthLogoutDeletesToken(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}
	prov := api.elEngine.GetModule(ext.ModuleId).Provider().(*auth.OAuth2Provider)
	seedToken(t, prov, "alice", []string{"read"})

	// Sanity: account is visible before logout.
	if got := prov.ListAccounts(); len(got) != 1 {
		t.Fatalf("pre-logout ListAccounts = %v, want [alice]", got)
	}

	body, _ := json.Marshal(common.AuthLogoutParams{
		PluginID: ext.ModuleId,
		Account:  "alice",
	})
	tag, _, err := api.authLogoutHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("authLogoutHandler: %v", err)
	}
	if tag != common.UPDATE_AUTH_LOGGED_OUT {
		t.Fatalf("tag = %q, want UPDATE_AUTH_LOGGED_OUT", tag)
	}

	if got := prov.ListAccounts(); len(got) != 0 {
		t.Fatalf("post-logout ListAccounts = %v, want empty", got)
	}
}

// newStubDeviceServer returns an httptest.NewTLSServer exposing a
// /device/code and /token endpoint suitable for the device flow. The
// /token endpoint returns a successful token immediately — no
// authorization_pending roundtrip — so the polling goroutine resolves
// the flow on its first tick.
func newStubDeviceServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "DEV123",
			"user_code":        "USER-CODE",
			"verification_url": "https://example.invalid/verify",
			"expires_in":       600,
			"interval":         1,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "DEVICE_AT",
			"refresh_token": "DEVICE_RT",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"scope":         "read",
		})
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAuthLoginHandler_Device_HappyPath(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	path := writeAuthExtension(t, t.TempDir())
	ext, err := api.elEngine.AddModule(path)
	if err != nil {
		t.Fatalf("AddModule: %v", err)
	}
	prov := api.elEngine.GetModule(ext.ModuleId).Provider().(*auth.OAuth2Provider)

	stub := newStubDeviceServer(t)
	prov.OverrideTokenURL(stub.URL + "/token")
	prov.SetHTTPClient(insecureTLSClient())
	// Provider's Config is returned by value, so we can't mutate
	// DeviceURL through it — inject via a dedicated override to avoid
	// surgery on the provider internals from outside the auth pkg.
	prov.OverrideDeviceURL(stub.URL + "/device/code")

	body, _ := json.Marshal(common.AuthLoginParams{
		PluginID: ext.ModuleId,
		Flow:     "device",
	})
	tag, msg, err := api.authLoginHandler(nil, pool, body)
	if err != nil {
		t.Fatalf("authLoginHandler: %v", err)
	}
	if tag != common.UPDATE_AUTH_REQUIRED {
		t.Fatalf("tag = %q, want UPDATE_AUTH_REQUIRED", tag)
	}
	res := msg.(*common.AuthLoginResult)
	if res.FlowID == "" {
		t.Fatal("FlowID empty")
	}
	if res.UserCode != "USER-CODE" {
		t.Errorf("UserCode = %q, want USER-CODE", res.UserCode)
	}
	if res.VerificationURL != "https://example.invalid/verify" {
		t.Errorf("VerificationURL = %q", res.VerificationURL)
	}
	if res.Interval <= 0 {
		t.Errorf("Interval = %d, want > 0", res.Interval)
	}

	// Polling goroutine should resolve the flow almost immediately.
	done := make(chan struct {
		tok *types.OAuth2Token
		err error
	}, 1)
	go func() {
		tok, err := prov.FlowRegistry().Await(res.FlowID)
		done <- struct {
			tok *types.OAuth2Token
			err error
		}{tok, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Await err = %v", r.err)
		}
		if r.tok == nil || r.tok.AccessToken != "DEVICE_AT" {
			t.Fatalf("bad token: %+v", r.tok)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("device polling goroutine did not resolve the flow")
	}

	// Token must also be persisted in the store.
	if got := prov.ListAccounts(); len(got) != 1 {
		t.Errorf("expected 1 stored account, got %v", got)
	}
}

func TestAuthLogoutUnknownPluginErrors(t *testing.T) {
	api, pool, cleanup := newAuthTestApi(t)
	defer cleanup()

	body, _ := json.Marshal(common.AuthLogoutParams{
		PluginID: "does-not-exist",
		Account:  "alice",
	})
	_, _, err := api.authLogoutHandler(nil, pool, body)
	if err == nil {
		t.Fatal("expected error for unknown plugin")
	}
}
