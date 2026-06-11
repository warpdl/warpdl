package cmd

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
)

// newAuthCtxOnServer builds a *cli.Context for the auth subcommand produced
// by mkCmd, whose Action will dial the fake daemon on socketPath via
// getClient(). It takes the command constructor rather than a cli.Command
// value to avoid copying the large struct at every call site. The returned
// builder captures the command's user-facing output.
func newAuthCtxOnServer(mkCmd func() cli.Command, args []string, flagVals map[string]string) (*cli.Context, *strings.Builder) {
	app := cli.NewApp()
	out := &strings.Builder{}
	app.Writer = out
	ctx := newAuthActionContext(mkCmd(), args, flagVals)
	ctx.App = app
	return ctx, out
}

// ---- authList (full path through getClient) ----

// TestAuthList_Empty drives authList end-to-end against the fake daemon
// with an empty credential store. It must print the friendly empty
// message and return nil.
func TestAuthList_Empty(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authListCmd, nil, nil)
	if err := authList(ctx); err != nil {
		t.Fatalf("authList: %v", err)
	}
	if !strings.Contains(out.String(), "No stored credentials") {
		t.Fatalf("expected empty-store message, got: %q", out.String())
	}
}

// TestAuthList_WithAccounts drives authList against a daemon that returns
// two accounts, asserting both rows reach the writer.
func TestAuthList_WithAccounts(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	authListOverride = &common.AuthListResult{Accounts: []common.AuthAccount{
		{PluginID: "gdrive", Account: "default", Scopes: []string{"read"}, ExpiresAt: time.Now().Add(time.Hour).Unix()},
		{PluginID: "dropbox", Account: "work", ExpiresAt: 0},
	}}
	defer func() { authListOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authListCmd, nil, nil)
	if err := authList(ctx); err != nil {
		t.Fatalf("authList: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "gdrive") || !strings.Contains(got, "dropbox") {
		t.Fatalf("expected both accounts in output, got: %q", got)
	}
	if !strings.Contains(got, "PLUGIN") {
		t.Fatalf("expected table header, got: %q", got)
	}
}

// TestAuthList_RPCError exercises the auth.list error-wrapping branch:
// the daemon returns an error for UPDATE_AUTH_LIST and authList must
// surface it wrapped with the "auth.list" prefix.
func TestAuthList_RPCError(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_AUTH_LIST: "store unavailable",
	})
	defer srv.close()

	ctx, _ := newAuthCtxOnServer(authListCmd, nil, nil)
	err := authList(ctx)
	if err == nil {
		t.Fatal("expected error from authList")
	}
	if !strings.Contains(err.Error(), "auth.list") || !strings.Contains(err.Error(), "store unavailable") {
		t.Fatalf("err = %v, want wrapped auth.list error", err)
	}
}

// TestAuthList_ConnectError covers the connect-daemon failure branch:
// with a custom invalid daemon URI, getClient() fails before any RPC.
func TestAuthList_ConnectError(t *testing.T) {
	old := daemonURI
	daemonURI = "://invalid-uri"
	defer func() { daemonURI = old }()

	ctx, _ := newAuthCtxOnServer(authListCmd, nil, nil)
	err := authList(ctx)
	if err == nil {
		t.Fatal("expected connect error")
	}
	if !strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("err = %v, want connect daemon error", err)
	}
}

// ---- authLogout (full path through getClient) ----

// TestAuthLogout_Success drives authLogout end-to-end: the daemon
// acknowledges the logout and the command prints a confirmation and
// returns nil.
func TestAuthLogout_Success(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authLogoutCmd, []string{"gdrive"}, nil)
	if err := authLogout(ctx); err != nil {
		t.Fatalf("authLogout: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Logged out of gdrive") {
		t.Fatalf("expected logout confirmation, got: %q", got)
	}
	// Default account label must be used when --as is absent.
	if !strings.Contains(got, "default") {
		t.Fatalf("expected default account label, got: %q", got)
	}
}

// TestAuthLogout_CustomAccount confirms the --as flag flows through to
// the printed confirmation line.
func TestAuthLogout_CustomAccount(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authLogoutCmd, []string{"gdrive"}, map[string]string{"as": "work"})
	if err := authLogout(ctx); err != nil {
		t.Fatalf("authLogout: %v", err)
	}
	if !strings.Contains(out.String(), "(work)") {
		t.Fatalf("expected work account in output, got: %q", out.String())
	}
}

// TestAuthLogout_RPCError exercises the auth.logout error-wrapping branch.
func TestAuthLogout_RPCError(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_AUTH_LOGGED_OUT: "no such credential",
	})
	defer srv.close()

	ctx, _ := newAuthCtxOnServer(authLogoutCmd, []string{"gdrive"}, nil)
	err := authLogout(ctx)
	if err == nil {
		t.Fatal("expected error from authLogout")
	}
	if !strings.Contains(err.Error(), "auth.logout") || !strings.Contains(err.Error(), "no such credential") {
		t.Fatalf("err = %v, want wrapped auth.logout error", err)
	}
}

// TestAuthLogout_ConnectError covers the connect-daemon failure branch.
func TestAuthLogout_ConnectError(t *testing.T) {
	old := daemonURI
	daemonURI = "://invalid-uri"
	defer func() { daemonURI = old }()

	ctx, _ := newAuthCtxOnServer(authLogoutCmd, []string{"gdrive"}, nil)
	err := authLogout(ctx)
	if err == nil {
		t.Fatal("expected connect error")
	}
	if !strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("err = %v, want connect daemon error", err)
	}
}

// ---- authStatus (full path through getClient) ----

// TestAuthStatus_Authenticated drives authStatus end-to-end against the
// fake daemon: a valid unexpired credential yields exit 0 (nil error).
func TestAuthStatus_Authenticated(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	authListOverride = &common.AuthListResult{Accounts: []common.AuthAccount{
		{PluginID: "gdrive", Account: "default", ExpiresAt: time.Now().Add(time.Hour).Unix()},
	}}
	defer func() { authListOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authStatusCmd, []string{"gdrive"}, nil)
	if err := authStatus(ctx); err != nil {
		t.Fatalf("authStatus: %v, want nil", err)
	}
	if !strings.Contains(out.String(), "authenticated") || strings.Contains(out.String(), "not authenticated") {
		t.Fatalf("expected authenticated, got: %q", out.String())
	}
}

// TestAuthStatus_Expired drives authStatus with a past-expiry credential:
// must print "expired" and return *cli.ExitError with code 2.
func TestAuthStatus_Expired(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	authListOverride = &common.AuthListResult{Accounts: []common.AuthAccount{
		{PluginID: "gdrive", Account: "default", ExpiresAt: time.Now().Add(-time.Hour).Unix()},
	}}
	defer func() { authListOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authStatusCmd, []string{"gdrive"}, nil)
	err := authStatus(ctx)
	ee, ok := err.(*cli.ExitError)
	if !ok {
		t.Fatalf("err = %v (%T), want *cli.ExitError", err, err)
	}
	if ee.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", ee.ExitCode())
	}
	if !strings.Contains(out.String(), "expired") {
		t.Fatalf("expected expired marker, got: %q", out.String())
	}
}

// TestAuthStatus_NotAuthenticated covers exit code 1: no matching
// (plugin, account) row in the daemon's store.
func TestAuthStatus_NotAuthenticated(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	authListOverride = &common.AuthListResult{Accounts: []common.AuthAccount{
		{PluginID: "other", Account: "default", ExpiresAt: time.Now().Add(time.Hour).Unix()},
	}}
	defer func() { authListOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authStatusCmd, []string{"gdrive"}, nil)
	err := authStatus(ctx)
	ee, ok := err.(*cli.ExitError)
	if !ok {
		t.Fatalf("err = %v (%T), want *cli.ExitError", err, err)
	}
	if ee.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", ee.ExitCode())
	}
	if !strings.Contains(out.String(), "not authenticated") {
		t.Fatalf("expected not-authenticated, got: %q", out.String())
	}
}

// TestAuthStatus_CustomAccount confirms the --as flag is load-bearing:
// a matching plugin but mismatched account falls through to exit 1.
func TestAuthStatus_CustomAccount(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	authListOverride = &common.AuthListResult{Accounts: []common.AuthAccount{
		{PluginID: "gdrive", Account: "default", ExpiresAt: time.Now().Add(time.Hour).Unix()},
	}}
	defer func() { authListOverride = nil }()
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	ctx, out := newAuthCtxOnServer(authStatusCmd, []string{"gdrive"}, map[string]string{"as": "work"})
	err := authStatus(ctx)
	ee, ok := err.(*cli.ExitError)
	if !ok {
		t.Fatalf("err = %v (%T), want *cli.ExitError", err, err)
	}
	if ee.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", ee.ExitCode())
	}
	if !strings.Contains(out.String(), "work") {
		t.Fatalf("expected work account in output, got: %q", out.String())
	}
}

// TestAuthStatus_RPCError exercises the auth.list error-wrapping branch
// reached from authStatus (distinct from authList's own wrapper).
func TestAuthStatus_RPCError(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_AUTH_LIST: "store unavailable",
	})
	defer srv.close()

	ctx, _ := newAuthCtxOnServer(authStatusCmd, []string{"gdrive"}, nil)
	err := authStatus(ctx)
	if err == nil {
		t.Fatal("expected error from authStatus")
	}
	if _, ok := err.(*cli.ExitError); ok {
		t.Fatalf("auth.list error should not be a *cli.ExitError: %v", err)
	}
	if !strings.Contains(err.Error(), "auth.list") {
		t.Fatalf("err = %v, want wrapped auth.list error", err)
	}
}

// TestAuthStatus_ConnectError covers the connect-daemon failure branch.
func TestAuthStatus_ConnectError(t *testing.T) {
	old := daemonURI
	daemonURI = "://invalid-uri"
	defer func() { daemonURI = old }()

	ctx, _ := newAuthCtxOnServer(authStatusCmd, []string{"gdrive"}, nil)
	err := authStatus(ctx)
	if err == nil {
		t.Fatal("expected connect error")
	}
	if !strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("err = %v, want connect daemon error", err)
	}
}

// ---- authLogin dispatch (device + PKCE entry points via getClient) ----

// TestAuthLogin_DeviceDispatch drives authLogin with --device against the
// fake daemon. The daemon returns a login error for UPDATE_AUTH_REQUIRED
// so runDeviceFlow surfaces it immediately without entering its polling
// loop — this exercises authLogin's flag parsing, getClient(), and the
// device-flow dispatch branch without any unbounded wait.
func TestAuthLogin_DeviceDispatch(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_AUTH_REQUIRED: "device flow unavailable",
	})
	defer srv.close()

	ctx, _ := newAuthCtxOnServer(authLoginCmd, []string{"gdrive"}, map[string]string{"device": "true"})
	err := authLogin(ctx)
	if err == nil {
		t.Fatal("expected error from device-flow login")
	}
	if !strings.Contains(err.Error(), "auth.login") || !strings.Contains(err.Error(), "device flow unavailable") {
		t.Fatalf("err = %v, want wrapped auth.login error", err)
	}
}

// TestAuthLogin_PKCEDispatch drives authLogin with --no-browser (PKCE
// path) against the fake daemon, which returns a login error so
// runPKCEFlow surfaces it after binding+closing the loopback listener.
// This covers authLogin's PKCE dispatch branch and runPKCEFlow's
// login-failure cleanup without blocking on a browser callback.
func TestAuthLogin_PKCEDispatch(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_AUTH_REQUIRED: "pkce flow unavailable",
	})
	defer srv.close()

	ctx, _ := newAuthCtxOnServer(authLoginCmd, []string{"gdrive"},
		map[string]string{"no-browser": "true", "as": "work"})
	err := authLogin(ctx)
	if err == nil {
		t.Fatal("expected error from pkce-flow login")
	}
	if !strings.Contains(err.Error(), "auth.login") || !strings.Contains(err.Error(), "pkce flow unavailable") {
		t.Fatalf("err = %v, want wrapped auth.login error", err)
	}
}

// TestAuthLogin_ScopesFlow confirms the --scopes slice flag is parsed and
// threaded into the AuthLogin params for the device path. The daemon
// returns an error so we never block; the assertion is purely that the
// login call was attempted (wrapped error), proving scope parsing did
// not panic on a multi-value slice flag.
func TestAuthLogin_ScopesFlow(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_AUTH_REQUIRED: "stop here",
	})
	defer srv.close()

	ctx, _ := newAuthCtxOnServer(authLoginCmd, []string{"gdrive"},
		map[string]string{"device": "true", "scopes": "read"})
	err := authLogin(ctx)
	if err == nil || !strings.Contains(err.Error(), "auth.login") {
		t.Fatalf("err = %v, want wrapped auth.login error", err)
	}
}

// TestAuthLogin_ConnectError covers authLogin's connect-daemon failure
// branch (before any flow dispatch).
func TestAuthLogin_ConnectError(t *testing.T) {
	old := daemonURI
	daemonURI = "://invalid-uri"
	defer func() { daemonURI = old }()

	ctx, _ := newAuthCtxOnServer(authLoginCmd, []string{"gdrive"}, nil)
	err := authLogin(ctx)
	if err == nil {
		t.Fatal("expected connect error")
	}
	if !strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("err = %v, want connect daemon error", err)
	}
}

// ---- runDeviceFlow: success + timeout paths ----

// TestRunDeviceFlow_Success drives runDeviceFlow to a successful login:
// AuthLogin returns a short poll interval and AuthList immediately
// reports the target account, so the first tick resolves the flow and
// prints the success line. A tiny Interval keeps the test well under the
// 10s budget without sleeping as a synchronization primitive (the ticker
// fires; AuthList returns the match deterministically).
func TestRunDeviceFlow_Success(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{
		loginRes: &common.AuthLoginResult{
			FlowID:          "f-dev",
			UserCode:        "WARP-1",
			VerificationURL: "https://idp/activate",
			Interval:        1, // 1s poll; first tick resolves immediately
		},
		listRes: &common.AuthListResult{Accounts: []common.AuthAccount{
			{PluginID: "gdrive", Account: "default", ExpiresAt: time.Now().Add(time.Hour).Unix()},
		}},
	}
	var buf strings.Builder
	done := make(chan error, 1)
	go func() { done <- runDeviceFlow(&buf, fake, "gdrive", "default", nil) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runDeviceFlow: %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runDeviceFlow did not resolve within budget")
	}
	out := buf.String()
	if !strings.Contains(out, "WARP-1") || !strings.Contains(out, "Logged in to gdrive") {
		t.Fatalf("unexpected output: %q", out)
	}
}

// TestRunDeviceFlow_Timeout exercises the deadline branch: the daemon's
// reported ExpiresAt is already in the past, so on the first tick
// runDeviceFlow detects the timeout, issues AuthCancel, and returns the
// "device flow timed out" error. Interval is 1s so the tick fires fast.
func TestRunDeviceFlow_Timeout(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{
		loginRes: &common.AuthLoginResult{
			FlowID:    "f-timeout",
			UserCode:  "WARP-9",
			Interval:  1,
			ExpiresAt: time.Now().Add(-time.Second).Unix(), // already past
		},
		// No matching account, so only the deadline check can fire.
		listRes: &common.AuthListResult{},
	}
	var buf strings.Builder
	done := make(chan error, 1)
	go func() { done <- runDeviceFlow(&buf, fake, "gdrive", "default", nil) }()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "device flow timed out") {
			t.Fatalf("err = %v, want device flow timed out", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runDeviceFlow did not time out within budget")
	}
	if len(fake.cancelCalls) != 1 {
		t.Fatalf("AuthCancel calls = %d, want 1 on timeout", len(fake.cancelCalls))
	}
	if fake.cancelCalls[0].FlowID != "f-timeout" {
		t.Fatalf("AuthCancel FlowID = %q, want f-timeout", fake.cancelCalls[0].FlowID)
	}
}

// TestRunDeviceFlow_ListErrorThenSuccess covers the branch where an
// intermediate AuthList call fails (lerr != nil) and is silently
// retried on the next tick. We script the fake to error once, then
// succeed. The loop must not abort on the transient list error.
func TestRunDeviceFlow_ListErrorThenSuccess(t *testing.T) {
	t.Parallel()

	fake := &scriptedDeviceRPC{
		loginRes: &common.AuthLoginResult{FlowID: "f", Interval: 1},
		listSeq: []listOutcome{
			{err: errors.New("transient list failure")},
			{res: &common.AuthListResult{Accounts: []common.AuthAccount{
				{PluginID: "gdrive", Account: "default"},
			}}},
		},
	}
	var buf strings.Builder
	done := make(chan error, 1)
	go func() { done <- runDeviceFlow(&buf, fake, "gdrive", "default", nil) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runDeviceFlow: %v, want nil after transient list error", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("runDeviceFlow did not resolve within budget")
	}
	if !strings.Contains(buf.String(), "Logged in to gdrive") {
		t.Fatalf("expected success line, got: %q", buf.String())
	}
}

// scriptedDeviceRPC is an authRPC whose AuthList returns a scripted
// sequence of outcomes, advancing one entry per call (saturating on the
// last). It lets us model a transient list failure followed by success.
type scriptedDeviceRPC struct {
	loginRes    *common.AuthLoginResult
	loginErr    error
	listSeq     []listOutcome
	listIdx     int
	cancelCalls []common.AuthCancelParams
}

type listOutcome struct {
	res *common.AuthListResult
	err error
}

func (s *scriptedDeviceRPC) AuthLogin(*common.AuthLoginParams) (*common.AuthLoginResult, error) {
	return s.loginRes, s.loginErr
}

func (s *scriptedDeviceRPC) AuthComplete(*common.AuthCompleteParams) (*common.AuthCompleteResult, error) {
	return nil, errors.New("not used")
}

func (s *scriptedDeviceRPC) AuthCancel(p *common.AuthCancelParams) error {
	s.cancelCalls = append(s.cancelCalls, *p)
	return nil
}

func (s *scriptedDeviceRPC) AuthList() (*common.AuthListResult, error) {
	o := s.listSeq[s.listIdx]
	if s.listIdx < len(s.listSeq)-1 {
		s.listIdx++
	}
	return o.res, o.err
}

func (s *scriptedDeviceRPC) AuthLogout(*common.AuthLogoutParams) error {
	return errors.New("not used")
}

var _ authRPC = (*scriptedDeviceRPC)(nil)

// ---- runPKCEFlow: callback success path (full happy path) ----

// TestRunPKCEFlow_CallbackSuccess drives runPKCEFlow's happy path end to
// end without a browser: the daemon returns a login result, runPKCEFlow
// binds a real loopback listener and prints the authorize URL
// (no-browser=true), and the test then performs the OAuth redirect by
// issuing an HTTP GET to the loopback /callback endpoint. The callback
// handler calls AuthComplete on the fake (success), writes nil to the
// callback channel, and runPKCEFlow returns nil after printing the
// "Logged in" line.
//
// We extract the bound port from the AuthLogin params (RedirectURI),
// which is the only place runPKCEFlow exposes it, and poll the endpoint
// with a deadline rather than sleeping.
func TestRunPKCEFlow_CallbackSuccess(t *testing.T) {
	// Not parallel: installs signal.Notify on os.Interrupt for the
	// duration of the call (process-global), matching production.
	fake := newCapturingPKCERPC()
	fake.loginRes = &common.AuthLoginResult{
		FlowID:       "f-pkce-ok",
		AuthorizeURL: "https://idp.example/authorize?client_id=x",
	}
	fake.completeRes = &common.AuthCompleteResult{Account: "default"}

	out := newSyncBuffer()
	done := make(chan error, 1)
	go func() {
		done <- runPKCEFlow(out, fake, "gdrive", "default", nil, true)
	}()

	// Wait for runPKCEFlow to record the AuthLogin call so we can read
	// the loopback redirect URI it bound.
	redirect := fake.waitForRedirectURI(t, 5*time.Second)
	if !strings.HasPrefix(redirect, "http://127.0.0.1:") {
		t.Fatalf("redirect URI = %q, want loopback", redirect)
	}

	// Drive the OAuth redirect: hit the loopback /callback with code+state.
	pokeCallback(t, redirect+"?code=abc&state=xyz", 5*time.Second)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runPKCEFlow: %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPKCEFlow did not complete after callback")
	}

	if !strings.Contains(out.String(), "Logged in to gdrive") {
		t.Fatalf("expected success line, got: %q", out.String())
	}
	if got := fake.completeCount(); got != 1 {
		t.Fatalf("AuthComplete calls = %d, want 1", got)
	}
}

// TestRunPKCEFlow_BrowserBranchCallbackSuccess drives runPKCEFlow's happy
// path with noBrowser=false to cover the openBrowser dispatch arm.
// WARP_NO_BROWSER=1 keeps it hermetic (openBrowser prints the URL instead
// of launching a process). The loopback callback is then poked to resolve
// the flow, exactly as in TestRunPKCEFlow_CallbackSuccess.
func TestRunPKCEFlow_BrowserBranchCallbackSuccess(t *testing.T) {
	// Not parallel: mutates WARP_NO_BROWSER and installs signal.Notify.
	t.Setenv("WARP_NO_BROWSER", "1")

	fake := newCapturingPKCERPC()
	fake.loginRes = &common.AuthLoginResult{
		FlowID:       "f-pkce-browser",
		AuthorizeURL: "https://idp.example/authorize?client_id=z",
	}
	fake.completeRes = &common.AuthCompleteResult{Account: "default"}

	out := newSyncBuffer()
	done := make(chan error, 1)
	go func() {
		done <- runPKCEFlow(out, fake, "gdrive", "default", nil, false)
	}()

	redirect := fake.waitForRedirectURI(t, 5*time.Second)
	pokeCallback(t, redirect+"?code=abc&state=xyz", 5*time.Second)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runPKCEFlow: %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPKCEFlow did not complete after callback")
	}
	// openBrowser fallback (WARP_NO_BROWSER) printed the authorize URL.
	if !strings.Contains(out.String(), fake.loginRes.AuthorizeURL) {
		t.Fatalf("expected authorize URL in output, got: %q", out.String())
	}
	if !strings.Contains(out.String(), "Logged in to gdrive") {
		t.Fatalf("expected success line, got: %q", out.String())
	}
}

// TestRunPKCEFlow_CallbackProviderError drives runPKCEFlow where the
// browser redirect carries an ?error= param. The callback handler
// writes a provider-error verdict to the channel; runPKCEFlow then
// issues AuthCancel and returns the error.
func TestRunPKCEFlow_CallbackProviderError(t *testing.T) {
	fake := newCapturingPKCERPC()
	fake.loginRes = &common.AuthLoginResult{
		FlowID:       "f-pkce-err",
		AuthorizeURL: "https://idp.example/authorize",
	}

	out := newSyncBuffer()
	done := make(chan error, 1)
	go func() {
		done <- runPKCEFlow(out, fake, "gdrive", "default", nil, true)
	}()

	redirect := fake.waitForRedirectURI(t, 5*time.Second)
	pokeCallback(t, redirect+"?error=access_denied&error_description=nope", 5*time.Second)

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "access_denied") {
			t.Fatalf("err = %v, want provider error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPKCEFlow did not return after provider error")
	}
	if got := fake.cancelCount(); got != 1 {
		t.Fatalf("AuthCancel calls = %d, want 1 on provider error", got)
	}
}

// TestHandleAuthRequiredPKCE_BrowserBranch exercises HandleAuthRequired's
// PKCE branch with noBrowser=false. With WARP_NO_BROWSER=1 set,
// openBrowser prints the URL instead of launching a real browser, so the
// else-branch (the openBrowser call) is covered hermetically. We pass a
// short-deadline context to unwind the blocking select via ctx.Done(),
// which then issues AuthCancel. This complements the existing
// no-browser=true PKCE test, covering the distinct dispatch arm.
func TestHandleAuthRequiredPKCE_BrowserBranch(t *testing.T) {
	// Not parallel: mutates the process-global WARP_NO_BROWSER env.
	t.Setenv("WARP_NO_BROWSER", "1")

	fake := &fakeAuthRPC{completeRes: &common.AuthCompleteResult{Account: "default"}}
	var buf strings.Builder
	res := &common.AuthLoginResult{
		FlowID:       "f-pkce-browser",
		AuthorizeURL: "https://idp.example/authorize?client_id=y",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	err := HandleAuthRequired(ctx, &buf, fake, "gdrive", "default", res, false)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	out := buf.String()
	// openBrowser fallback prints the URL (via WARP_NO_BROWSER guard).
	if !strings.Contains(out, res.AuthorizeURL) {
		t.Fatalf("authorize URL missing from output: %q", out)
	}
	// The PKCE branch announces it is opening a browser.
	if !strings.Contains(out, "opening browser") {
		t.Fatalf("expected opening-browser banner, got: %q", out)
	}
	if len(fake.cancelCalls) != 1 {
		t.Fatalf("AuthCancel calls = %d, want 1 on ctx cancellation", len(fake.cancelCalls))
	}
}

// TestHandleAuthRequiredPKCE_CallbackSuccess drives HandleAuthRequired's
// PKCE happy path through a real loopback round-trip. Because the
// function does not expose the bound loopback port, we discover it by
// stubbing openBrowser's transport: with noBrowser=false and
// WARP_NO_BROWSER unset, openBrowser shells to a fake browser provider
// we install on PATH (a no-op script). That keeps the test hermetic (the
// "browser" is a local script that exits 0) while letting us reach the
// post-open select. We then resolve the flow by writing to the callback
// channel indirectly: since the port is hidden, we instead assert the
// timeout/cancel wiring. To actually fire the callback we reuse the
// loopback by binding the same way and probing the known-open port set.
//
// NOTE: rather than scan ports (non-hermetic), we cover the
// callback-success select arm via runPKCEFlow (which exposes the
// redirect URI) — see TestRunPKCEFlow_CallbackSuccess. HandleAuthRequired
// and runPKCEFlow share the exact same newCallbackServer + select
// plumbing, so the success arm is exercised there. This test instead
// pins the device-flow short-circuit arm of HandleAuthRequired.
func TestHandleAuthRequiredDeviceShortCircuit(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	var buf strings.Builder
	res := &common.AuthLoginResult{
		FlowID:          "f-dev",
		UserCode:        "DEV-7777",
		VerificationURL: "https://idp.example/activate",
	}
	// Device flow (UserCode set) returns immediately without binding a
	// listener or touching AuthComplete/AuthCancel.
	if err := HandleAuthRequired(context.Background(), &buf, fake, "gdrive", "default", res, false); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	out := buf.String()
	if !strings.Contains(out, "DEV-7777") || !strings.Contains(out, "https://idp.example/activate") {
		t.Fatalf("device prompt missing: %q", out)
	}
	if len(fake.completeCalls) != 0 || len(fake.cancelCalls) != 0 {
		t.Fatalf("device short-circuit must not call AuthComplete/AuthCancel")
	}
}

// capturingPKCERPC is an authRPC for the PKCE happy/error path tests. It
// records the RedirectURI from AuthLogin (so the test can find the bound
// loopback port) and counts AuthComplete / AuthCancel calls. All access
// is mutex-guarded because runPKCEFlow's callback handler runs on a
// separate goroutine (the HTTP server) from the test's pollers.
type capturingPKCERPC struct {
	mu            sync.Mutex
	loginRes      *common.AuthLoginResult
	loginErr      error
	completeRes   *common.AuthCompleteResult
	completeErr   error
	redirectURI   string
	completeCalls int
	cancelCalls   int
}

func newCapturingPKCERPC() *capturingPKCERPC { return &capturingPKCERPC{} }

func (c *capturingPKCERPC) AuthLogin(p *common.AuthLoginParams) (*common.AuthLoginResult, error) {
	c.mu.Lock()
	c.redirectURI = p.RedirectURI
	c.mu.Unlock()
	return c.loginRes, c.loginErr
}

func (c *capturingPKCERPC) AuthComplete(*common.AuthCompleteParams) (*common.AuthCompleteResult, error) {
	c.mu.Lock()
	c.completeCalls++
	res, err := c.completeRes, c.completeErr
	c.mu.Unlock()
	return res, err
}

func (c *capturingPKCERPC) AuthCancel(*common.AuthCancelParams) error {
	c.mu.Lock()
	c.cancelCalls++
	c.mu.Unlock()
	return nil
}

func (c *capturingPKCERPC) AuthList() (*common.AuthListResult, error) {
	return &common.AuthListResult{}, nil
}

func (c *capturingPKCERPC) AuthLogout(*common.AuthLogoutParams) error { return nil }

func (c *capturingPKCERPC) waitForRedirectURI(t *testing.T, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		u := c.redirectURI
		c.mu.Unlock()
		if u != "" {
			return u
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("AuthLogin redirect URI not recorded within deadline")
	return ""
}

func (c *capturingPKCERPC) completeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.completeCalls
}

func (c *capturingPKCERPC) cancelCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancelCalls
}

var _ authRPC = (*capturingPKCERPC)(nil)

// pokeCallback issues GET requests to the loopback callback URL until one
// succeeds (the loopback server may take a beat to begin serving) or the
// deadline elapses. Polling avoids a fixed sleep as synchronization.
func pokeCallback(t *testing.T, url string, d time.Duration) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(d)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url) //nolint:bodyclose // closed on next line
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		lastErr = err
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("callback never reachable within deadline: %v", lastErr)
}
