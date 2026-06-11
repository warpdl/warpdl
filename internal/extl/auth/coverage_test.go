package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/credman"
	"github.com/warpdl/warpdl/pkg/credman/types"
)

// makeDeviceProvider builds a provider wired to device + token endpoints with
// the insecure TLS client. Used by the device-flow edge-case tests.
func makeDeviceProvider(t *testing.T, tokenURL, deviceURL string) *OAuth2Provider {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	tm, err := credman.NewTokenManager(filepath.Join(t.TempDir(), "tokens.gob"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	cfg := OAuth2Config{
		Type:         "oauth2",
		ClientID:     "client-id",
		Scopes:       []string{"drive.readonly"},
		AuthorizeURL: "https://example.com/authorize",
		TokenURL:     tokenURL,
		DeviceURL:    deviceURL,
		PKCEMethod:   "S256",
	}
	cfg, err = NormalizeOAuth2Config(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := NewOAuth2Provider("plugin-1", cfg, tm, NewFlowRegistry(time.Minute))
	p.client = newInsecureTLSClient()
	return p
}

// makeTestProviderWithExtraParams builds a provider whose manifest carries
// ExtraAuthParams, used to verify BuildAuthorizeURL appends them. Mirrors
// makeTestProvider's wiring (insecure TLS client, temp token store).
func makeTestProviderWithExtraParams(t *testing.T, extra map[string]string) (*OAuth2Provider, *credman.TokenManager) {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	tm, err := credman.NewTokenManager(filepath.Join(t.TempDir(), "tokens.gob"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tm.Close() })
	cfg := OAuth2Config{
		Type:            "oauth2",
		ClientID:        "client-id",
		Scopes:          []string{"drive.readonly"},
		AuthorizeURL:    "https://example.com/authorize",
		TokenURL:        "https://example.com/token",
		PKCEMethod:      "S256",
		ExtraAuthParams: extra,
	}
	cfg, err = NormalizeOAuth2Config(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := NewOAuth2Provider("plugin-1", cfg, tm, NewFlowRegistry(time.Minute))
	p.client = newInsecureTLSClient()
	return p, tm
}

// pollUntil polls fn until it returns true or the deadline elapses. Used in
// place of bare sleeps so timer-driven cleanup paths can be observed
// deterministically under -race.
func pollUntil(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return fn()
}

// --- trivial accessors -----------------------------------------------------

func TestConfigReturnsNormalizedConfig(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "https://example.com/revoke")
	cfg := p.Config()
	if cfg.ClientID != "client-id" {
		t.Fatalf("Config client_id = %q", cfg.ClientID)
	}
	if cfg.TokenURL != "https://example.com/token" {
		t.Fatalf("Config token_url = %q", cfg.TokenURL)
	}
	if cfg.PKCEMethod != "S256" {
		t.Fatalf("Config pkce = %q (normalize should default S256)", cfg.PKCEMethod)
	}
}

func TestSetHTTPClientSwapsClient(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	// makeTestProvider already injected an insecure client; swap to a fresh
	// one and confirm the provider uses the new pointer.
	newClient := newInsecureTLSClient()
	p.SetHTTPClient(newClient)
	if p.client != newClient {
		t.Fatal("SetHTTPClient did not swap the client pointer")
	}
}

func TestOverrideTokenURL(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	p.OverrideTokenURL("https://override.example.com/token")
	if got := p.Config().TokenURL; got != "https://override.example.com/token" {
		t.Fatalf("OverrideTokenURL: token_url = %q", got)
	}
}

func TestOverrideDeviceURL(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	if p.Config().DeviceURL != "" {
		t.Fatalf("expected empty device_url before override, got %q", p.Config().DeviceURL)
	}
	p.OverrideDeviceURL("https://override.example.com/device")
	if got := p.Config().DeviceURL; got != "https://override.example.com/device" {
		t.Fatalf("OverrideDeviceURL: device_url = %q", got)
	}
}

func TestFlowRegistryAccessorReturnsRegistry(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	fr := p.FlowRegistry()
	if fr == nil {
		t.Fatal("FlowRegistry returned nil")
	}
	// It must be the very registry the provider drives: a flow started on it
	// is visible via the same accessor.
	f, _, err := fr.Start(types.TokenKey{PluginID: "plugin-1"}, FlowKindPKCE)
	if err != nil {
		t.Fatal(err)
	}
	if p.FlowRegistry().Get(f.ID) == nil {
		t.Fatal("FlowRegistry accessor returned a different registry instance")
	}
	fr.Cancel(f.ID, nil)
	_, _ = fr.Await(f.ID) // drain
}

func TestStoreTokenPersistsUnderPluginAccount(t *testing.T) {
	p, tm := makeTestProvider(t, "https://example.com/token", "")
	// StoreToken forces the provider's plugin id + default account regardless
	// of what the caller supplies.
	in := &types.OAuth2Token{
		AccessToken:  "STORED-AT",
		RefreshToken: "STORED-RT",
		ExpiresAt:    time.Now().Add(time.Hour),
		Scopes:       []string{"drive.readonly"},
	}
	if err := p.StoreToken(types.TokenKey{}, in); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	// Read back under the normalized key the provider would have used.
	stored, err := tm.Get(types.TokenKey{PluginID: "plugin-1", Account: "default"})
	if err != nil {
		t.Fatalf("token not persisted under normalized key: %v", err)
	}
	if stored.AccessToken != "STORED-AT" {
		t.Fatalf("round-trip lost token: %+v", stored)
	}
}

func TestStoreTokenHonorsExplicitAccount(t *testing.T) {
	p, tm := makeTestProvider(t, "https://example.com/token", "")
	in := &types.OAuth2Token{AccessToken: "WORK-AT", ExpiresAt: time.Now().Add(time.Hour)}
	if err := p.StoreToken(types.TokenKey{Account: "work"}, in); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}
	stored, err := tm.Get(types.TokenKey{PluginID: "plugin-1", Account: "work"})
	if err != nil {
		t.Fatalf("token not persisted under account=work: %v", err)
	}
	if stored.AccessToken != "WORK-AT" {
		t.Fatalf("round-trip lost token: %+v", stored)
	}
}

func TestAccountDetailsReturnsScopesAndExpiry(t *testing.T) {
	p, tm := makeTestProvider(t, "https://example.com/token", "")
	exp := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	_ = tm.Set(types.TokenKey{PluginID: "plugin-1", Account: "default"}, &types.OAuth2Token{
		AccessToken: "A",
		ExpiresAt:   exp,
		Scopes:      []string{"drive.readonly", "drive.metadata"},
	})
	scopes, unixExp, err := p.AccountDetails("default")
	if err != nil {
		t.Fatalf("AccountDetails: %v", err)
	}
	if len(scopes) != 2 || scopes[0] != "drive.readonly" || scopes[1] != "drive.metadata" {
		t.Fatalf("scopes = %v", scopes)
	}
	if unixExp != exp.Unix() {
		t.Fatalf("expiry unix = %d, want %d", unixExp, exp.Unix())
	}
}

func TestAccountDetailsDefaultsEmptyAccount(t *testing.T) {
	p, tm := makeTestProvider(t, "https://example.com/token", "")
	_ = tm.Set(types.TokenKey{PluginID: "plugin-1", Account: "default"}, &types.OAuth2Token{
		AccessToken: "A",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scopes:      []string{"drive.readonly"},
	})
	// Empty account must resolve to "default".
	scopes, _, err := p.AccountDetails("")
	if err != nil {
		t.Fatalf("AccountDetails(\"\"): %v", err)
	}
	if len(scopes) != 1 || scopes[0] != "drive.readonly" {
		t.Fatalf("scopes = %v", scopes)
	}
}

func TestAccountDetailsMissingReturnsError(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	if _, _, err := p.AccountDetails("nobody"); err == nil {
		t.Fatal("expected error for unknown account")
	}
}

// --- BuildAuthorizeURL (also exercises resolveScopes both branches) --------

func TestBuildAuthorizeURLWithRequestedScopes(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	raw := p.BuildAuthorizeURL(
		"http://127.0.0.1:9999/callback",
		"STATE-XYZ",
		"CHALLENGE-ABC",
		[]string{"drive.readonly"},
	)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("authorize URL not parseable: %v", err)
	}
	if u.Scheme != "https" || u.Host != "example.com" || u.Path != "/authorize" {
		t.Fatalf("unexpected base URL: %s", raw)
	}
	q := u.Query()
	checks := map[string]string{
		"response_type":         "code",
		"client_id":             "client-id",
		"redirect_uri":          "http://127.0.0.1:9999/callback",
		"scope":                 "drive.readonly",
		"state":                 "STATE-XYZ",
		"code_challenge":        "CHALLENGE-ABC",
		"code_challenge_method": "S256",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Fatalf("query[%q] = %q, want %q", k, got, want)
		}
	}
}

// Empty requested scopes must fall back to the full manifest set — this hits
// the `return manifest` branch of resolveScopes that no other test reaches.
func TestBuildAuthorizeURLFallsBackToManifestScopes(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	raw := p.BuildAuthorizeURL("http://127.0.0.1/cb", "S", "C", nil)
	u, _ := url.Parse(raw)
	if got := u.Query().Get("scope"); got != "drive.readonly" {
		t.Fatalf("expected manifest scope fallback, got %q", got)
	}
}

func TestBuildAuthorizeURLIncludesExtraAuthParams(t *testing.T) {
	p, _ := makeTestProviderWithExtraParams(t, map[string]string{
		"access_type": "offline",
		"prompt":      "consent",
	})
	raw := p.BuildAuthorizeURL("http://127.0.0.1/cb", "S", "C", []string{"drive.readonly"})
	u, _ := url.Parse(raw)
	q := u.Query()
	if q.Get("access_type") != "offline" {
		t.Fatalf("access_type missing: %s", raw)
	}
	if q.Get("prompt") != "consent" {
		t.Fatalf("prompt missing: %s", raw)
	}
}

// resolveScopes is also reachable directly; cover both branches explicitly so
// the function shows full coverage regardless of caller wiring.
func TestResolveScopes(t *testing.T) {
	manifest := []string{"a", "b"}
	if got := resolveScopes([]string{"a"}, manifest); len(got) != 1 || got[0] != "a" {
		t.Fatalf("non-empty request should win, got %v", got)
	}
	if got := resolveScopes(nil, manifest); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("empty request should fall back to manifest, got %v", got)
	}
	if got := resolveScopes([]string{}, manifest); len(got) != 2 {
		t.Fatalf("zero-length request should fall back to manifest, got %v", got)
	}
}

// --- FlowRegistry: Get, NewFlowState, Cancel(nil), sweepAfter --------------

func TestFlowRegistry_GetReturnsLiveFlow(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)
	got := fr.Get(f.ID)
	if got == nil {
		t.Fatal("Get returned nil for a live flow")
	}
	if got.ID != f.ID {
		t.Fatalf("Get returned wrong flow: %s vs %s", got.ID, f.ID)
	}
}

func TestFlowRegistry_GetUnknownReturnsNil(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	if got := fr.Get("nope"); got != nil {
		t.Fatalf("Get for unknown id should be nil, got %+v", got)
	}
}

func TestNewFlowStateReturnsHexToken(t *testing.T) {
	s, err := NewFlowState()
	if err != nil {
		t.Fatalf("NewFlowState: %v", err)
	}
	// randomID encodes 16 random bytes as hex → 32 chars.
	if len(s) != 32 {
		t.Fatalf("expected 32 hex chars, got %d (%q)", len(s), s)
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("non-hex char %q in flow state %q", c, s)
		}
	}
	// Two consecutive states must differ (entropy sanity).
	s2, _ := NewFlowState()
	if s == s2 {
		t.Fatal("two flow states collided; entropy broken")
	}
}

// Cancel with a nil error substitutes a generic "cancelled" error. The
// existing suite only covers Cancel with an explicit error.
func TestFlowRegistry_CancelNilErrorUsesGeneric(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)

	done := make(chan error, 1)
	go func() { _, err := fr.Await(f.ID); done <- err }()
	if !pollUntil(t, time.Second, func() bool { return fr.Get(f.ID) != nil }) {
		t.Fatal("flow never registered")
	}
	fr.Cancel(f.ID, nil)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Cancel(nil) should still surface an error")
		}
		if err.Error() != "cancelled" {
			t.Fatalf("expected generic \"cancelled\", got %q", err.Error())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Await never returned after Cancel(nil)")
	}
}

// sweepAfter runs when Resolve fires with zero awaiters: the flow must be
// reaped from the registry maps after the grace window. We poll Get until it
// returns nil rather than sleeping for a fixed duration.
func TestFlowRegistry_ResolveWithoutAwaiterSweepsFlow(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)
	// Drop the awaiter count to 0 so Resolve schedules sweepAfter. Start set
	// it to 1; mirror what a leaving Await would do.
	f.awaiters.Add(-1)

	// Resolve with no awaiter present → sweepAfter(f, 5s) is scheduled.
	fr.Resolve(f.ID, &types.OAuth2Token{AccessToken: "ORPHAN"}, nil)

	// The grace window is 5s in production. We cannot shorten it without
	// touching prod code, so we instead trigger the registry-shutdown branch
	// of sweepAfter, which fires immediately. Shutdown closes r.done which
	// the select inside sweepAfter observes, then it reaps the flow.
	fr.Shutdown()

	if !pollUntil(t, 3*time.Second, func() bool { return fr.Get(f.ID) == nil }) {
		t.Fatal("sweepAfter did not reap the orphaned flow after shutdown")
	}
}

// Directly drive sweepAfter's timer path (grace elapses, no awaiter) using the
// shortest workable grace duration, then poll for reaping. This exercises the
// `<-time.After(grace)` arm rather than the shutdown arm.
func TestFlowRegistry_SweepAfterTimerReapsOrphan(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE)
	f.awaiters.Add(-1) // simulate "no awaiters left"

	// Resolve so the flow is terminal, then invoke sweepAfter with a tiny
	// grace directly (sweepAfter is package-private, available to the test).
	fr.Resolve(f.ID, &types.OAuth2Token{AccessToken: "ORPHAN"}, nil)
	go fr.sweepAfter(f, time.Millisecond)

	if !pollUntil(t, 3*time.Second, func() bool { return fr.Get(f.ID) == nil }) {
		t.Fatal("sweepAfter timer arm did not reap the orphaned flow")
	}
}

// sweepAfter must NOT reap a flow that an awaiter has since claimed: if the
// awaiter count is non-zero when the grace elapses, the flow stays so the
// awaiter's own cleanup runs. Covers the early `return` in sweepAfter.
func TestFlowRegistry_SweepAfterSkipsWhenAwaiterPresent(t *testing.T) {
	fr := NewFlowRegistry(time.Minute)
	defer fr.Shutdown()
	k := types.TokenKey{PluginID: "p"}
	f, _, _ := fr.Start(k, FlowKindPKCE) // awaiters == 1

	fr.Resolve(f.ID, &types.OAuth2Token{AccessToken: "CLAIMED"}, nil)
	// awaiters is still 1 here, so sweepAfter must bail without deleting.
	done := make(chan struct{})
	go func() {
		fr.sweepAfter(f, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sweepAfter did not return")
	}
	// Flow must still be present (awaiter has not run Await yet).
	if fr.Get(f.ID) == nil {
		t.Fatal("sweepAfter wrongly reaped a flow with a live awaiter")
	}
	// Now let the awaiter drain so the registry cleans up normally.
	if _, err := fr.Await(f.ID); err != nil {
		t.Fatalf("Await: %v", err)
	}
}

// --- triggerFlowAndAwait additional branches -------------------------------

// A context already cancelled before Token is called drives the pre-check
// select's <-ctx.Done() arm inside triggerFlowAndAwait (distinct from the
// post-Await cancellation path the existing suite covers).
func TestTokenPrecancelledContextHitsPreCheck(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	tk := types.TokenKey{PluginID: "plugin-1"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	_, err := p.Token(ctx, tk, []string{"drive.readonly"})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// When the awaited flow resolves with an error (e.g. the user rejected it),
// triggerFlowAndAwait must propagate that error — covering the `r.err != nil`
// arm of the post-Await select.
func TestTokenFlowResolvesWithErrorPropagates(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	tk := types.TokenKey{PluginID: "plugin-1"}

	flowErr := errors.New("user rejected consent")
	done := make(chan error, 1)
	go func() {
		_, err := p.Token(context.Background(), tk, []string{"drive.readonly"})
		done <- err
	}()

	// Wait for triggerFlowAndAwait to register the PKCE flow, then resolve it
	// with an error from the "RPC layer" side.
	fr := p.FlowRegistry()
	var flowID string
	if !pollUntil(t, 2*time.Second, func() bool {
		flowID = lookupFlowID(fr, tk.WithDefaultAccount())
		return flowID != ""
	}) {
		t.Fatal("flow never registered")
	}
	fr.Resolve(flowID, nil, flowErr)

	select {
	case err := <-done:
		if !errors.Is(err, flowErr) {
			t.Fatalf("expected flow error to propagate, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Token did not return after flow resolved with error")
	}
}

// When the awaited flow resolves successfully, triggerFlowAndAwait stores the
// token and returns its access token — covering the success arm including the
// store.Set call on the flow-trigger path (as opposed to the refresh path).
func TestTokenFlowResolvesSuccessfullyStoresToken(t *testing.T) {
	p, tm := makeTestProvider(t, "https://example.com/token", "")
	tk := types.TokenKey{PluginID: "plugin-1"}

	type res struct {
		tok string
		err error
	}
	done := make(chan res, 1)
	go func() {
		tok, err := p.Token(context.Background(), tk, []string{"drive.readonly"})
		done <- res{tok, err}
	}()

	fr := p.FlowRegistry()
	var flowID string
	if !pollUntil(t, 2*time.Second, func() bool {
		flowID = lookupFlowID(fr, tk.WithDefaultAccount())
		return flowID != ""
	}) {
		t.Fatal("flow never registered")
	}
	want := &types.OAuth2Token{
		AccessToken: "FLOW-AT",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scopes:      []string{"drive.readonly"},
	}
	fr.Resolve(flowID, want, nil)

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Token returned error: %v", r.err)
		}
		if r.tok != "FLOW-AT" {
			t.Fatalf("Token = %q, want FLOW-AT", r.tok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Token did not return after successful flow resolution")
	}
	// The token must have been persisted by triggerFlowAndAwait.
	stored, err := tm.Get(tk.WithDefaultAccount())
	if err != nil {
		t.Fatalf("token not stored after flow success: %v", err)
	}
	if stored.AccessToken != "FLOW-AT" {
		t.Fatalf("stored token = %+v", stored)
	}
}

// triggerFlowAndAwait surfaces Start errors: a registry shut down before the
// call makes flows.Start return ErrRegistryShutDown, which Token must relay.
func TestTokenStartErrorPropagates(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	tk := types.TokenKey{PluginID: "plugin-1"}
	// Shut the registry so Start fails immediately inside triggerFlowAndAwait.
	p.FlowRegistry().Shutdown()

	_, err := p.Token(context.Background(), tk, []string{"drive.readonly"})
	if err == nil {
		t.Fatal("expected error when flow registry is shut down")
	}
	if !errors.Is(err, ErrRegistryShutDown) {
		t.Fatalf("expected ErrRegistryShutDown, got %v", err)
	}
}

// --- StartDeviceCode / PollDeviceCode edge cases ---------------------------

// StartDeviceCode must refuse when the manifest declares no device_url.
func TestStartDeviceCodeUnsupportedWhenNoDeviceURL(t *testing.T) {
	p, _ := makeTestProvider(t, "https://example.com/token", "")
	if p.Config().DeviceURL != "" {
		t.Fatal("precondition: device_url should be empty")
	}
	if _, err := p.StartDeviceCode(context.Background()); err == nil {
		t.Fatal("expected error when device flow unsupported")
	}
}

// A non-2xx /device/code response must surface ErrProvider.
func TestStartDeviceCodeServerErrorReturnsProviderErr(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	p := makeDeviceProvider(t, srv.URL+"/token", srv.URL+"/device/code")
	if _, err := p.StartDeviceCode(context.Background()); err == nil {
		t.Fatal("expected error on 500 device/code")
	} else if !errors.Is(err, ErrProvider) {
		t.Fatalf("expected ErrProvider, got %v", err)
	}
}

// Malformed JSON from /device/code must surface a decode ErrProvider.
func TestStartDeviceCodeDecodeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	p := makeDeviceProvider(t, srv.URL+"/token", srv.URL+"/device/code")
	if _, err := p.StartDeviceCode(context.Background()); err == nil {
		t.Fatal("expected decode error")
	} else if !errors.Is(err, ErrProvider) {
		t.Fatalf("expected ErrProvider, got %v", err)
	}
}

// The RFC/Google variant uses `verification_uri` and may omit `interval`; the
// provider must fall back to the URI field and default the interval to 5.
func TestStartDeviceCodeRFCVariantFallbacks(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		// No verification_url, no interval → exercise both fallbacks.
		_, _ = w.Write([]byte(`{"device_code":"DC","user_code":"UC","verification_uri":"https://idp/device","expires_in":600}`))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	p := makeDeviceProvider(t, srv.URL+"/token", srv.URL+"/device/code")
	auth, err := p.StartDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("StartDeviceCode: %v", err)
	}
	if auth.VerificationURL != "https://idp/device" {
		t.Fatalf("verification_uri fallback failed: %q", auth.VerificationURL)
	}
	if auth.Interval != 5 {
		t.Fatalf("interval default should be 5, got %d", auth.Interval)
	}
}

// PollDeviceCode returns immediately when the context is already cancelled,
// covering the top-of-loop <-ctx.Done() arm.
func TestPollDeviceCodeContextCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		t.Error("/token must not be hit when context is pre-cancelled")
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	p := makeDeviceProvider(t, srv.URL+"/token", srv.URL+"/device/code")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.PollDeviceCode(ctx, &DeviceAuthorization{DeviceCode: "DC", Interval: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// A definitive (non-pending, non-slow_down) error from /token must abort
// polling and propagate — covers the default arm of the poll switch.
func TestPollDeviceCodeHardErrorAborts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"access_denied"}`))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	p := makeDeviceProvider(t, srv.URL+"/token", srv.URL+"/device/code")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Interval 0 → poll fires on the very next scheduler tick.
	_, err := p.PollDeviceCode(ctx, &DeviceAuthorization{DeviceCode: "DC", Interval: 0})
	if err == nil {
		t.Fatal("expected hard error to abort polling")
	}
	if strings.Contains(strings.ToLower(err.Error()), "authorization_pending") {
		t.Fatalf("hard error should not be treated as pending: %v", err)
	}
}

// A slow_down response widens the interval and keeps polling; the next poll
// then succeeds. Covers the slow_down arm. Intervals are kept at 0/short and
// we rely on the eventual success rather than fixed sleeps.
func TestPollDeviceCodeSlowDownThenSuccess(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"SD-AT","token_type":"Bearer","expires_in":3600,"scope":"drive.readonly"}`))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	p := makeDeviceProvider(t, srv.URL+"/token", srv.URL+"/device/code")

	// Interval 0; after slow_down the code adds 5s, so cap the test with a
	// generous deadline and assert success.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tok, err := p.PollDeviceCode(ctx, &DeviceAuthorization{DeviceCode: "DC", Interval: 0})
	if err != nil {
		t.Fatalf("PollDeviceCode after slow_down: %v", err)
	}
	if tok.AccessToken != "SD-AT" {
		t.Fatalf("token = %+v", tok)
	}
}

// lookupFlowID returns the id of the flow registered for key, or "" if none.
// Helper for tests that must resolve a flow started internally by Token.
func lookupFlowID(fr *FlowRegistry, key types.TokenKey) string {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if f, ok := fr.byKey[key]; ok {
		return f.ID
	}
	return ""
}
