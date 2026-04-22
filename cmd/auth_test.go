package cmd

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
)

// fakeAuthRPC is a test double for the authRPC interface. Each method
// can be individually scripted with an error or a return payload; the
// calls slice captures every invocation for post-hoc assertions.
type fakeAuthRPC struct {
	mu sync.Mutex

	loginRes *common.AuthLoginResult
	loginErr error

	completeRes *common.AuthCompleteResult
	completeErr error

	listRes *common.AuthListResult
	listErr error

	cancelErr error

	loginCalls    []common.AuthLoginParams
	completeCalls []common.AuthCompleteParams
	cancelCalls   []common.AuthCancelParams
	listCallCount int32
}

func (f *fakeAuthRPC) AuthLogin(p *common.AuthLoginParams) (*common.AuthLoginResult, error) {
	f.mu.Lock()
	f.loginCalls = append(f.loginCalls, *p)
	res := f.loginRes
	err := f.loginErr
	f.mu.Unlock()
	return res, err
}

func (f *fakeAuthRPC) AuthComplete(p *common.AuthCompleteParams) (*common.AuthCompleteResult, error) {
	f.mu.Lock()
	f.completeCalls = append(f.completeCalls, *p)
	res := f.completeRes
	err := f.completeErr
	f.mu.Unlock()
	return res, err
}

func (f *fakeAuthRPC) AuthCancel(p *common.AuthCancelParams) error {
	f.mu.Lock()
	f.cancelCalls = append(f.cancelCalls, *p)
	err := f.cancelErr
	f.mu.Unlock()
	return err
}

func (f *fakeAuthRPC) AuthList() (*common.AuthListResult, error) {
	atomic.AddInt32(&f.listCallCount, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listRes, f.listErr
}

// newAuthLoginContext builds a minimal *cli.Context with the given
// positional args and flag set, suitable for invoking authLogin.
func newAuthLoginContext(args []string, flagVals map[string]string) *cli.Context {
	app := cli.NewApp()
	app.Writer = io.Discard
	cmd := authLoginCmd()
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	for _, f := range cmd.Flags {
		f.Apply(fs)
	}
	// Inject flag values first, then positional args.
	rawArgs := []string{}
	for k, v := range flagVals {
		rawArgs = append(rawArgs, "--"+k, v)
	}
	rawArgs = append(rawArgs, args...)
	_ = fs.Parse(rawArgs)
	ctx := cli.NewContext(app, fs, nil)
	ctx.Command = cmd
	return ctx
}

// TestRenderCallbackPage validates the HTML body contains both the
// title and message substrings and that the Content-Type header is
// the expected HTML UTF-8 value.
func TestRenderCallbackPage(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	renderCallbackPage(rr, "My Title", "My Message")

	body := rr.Body.String()
	if !strings.Contains(body, "My Title") {
		t.Fatalf("body missing title; got %q", body)
	}
	if !strings.Contains(body, "My Message") {
		t.Fatalf("body missing message; got %q", body)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html*", ct)
	}
	if !strings.Contains(ct, "utf-8") {
		t.Fatalf("content-type missing utf-8 charset; got %q", ct)
	}
}

// TestRenderCallbackPageEscaping ensures html/template escapes <script>
// tags so a malicious provider cannot inject HTML via the redirect.
func TestRenderCallbackPageEscaping(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	renderCallbackPage(rr, "T", "<script>alert(1)</script>")
	body := rr.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("unescaped script tag in body: %q", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped script; got %q", body)
	}
}

// TestAuthLoginCmdShape guards the CLI surface: the subcommand name
// and every documented flag must exist.
func TestAuthLoginCmdShape(t *testing.T) {
	t.Parallel()

	c := authLoginCmd()
	if c.Name != "login" {
		t.Fatalf("name = %q, want login", c.Name)
	}
	if c.ArgsUsage == "" {
		t.Fatalf("ArgsUsage is empty")
	}
	names := map[string]bool{}
	for _, f := range c.Flags {
		names[f.GetName()] = true
	}
	for _, want := range []string{"as", "scopes", "device", "no-browser"} {
		if !names[want] {
			t.Fatalf("flag %q missing; got %v", want, names)
		}
	}
}

// TestAuthCmdShape validates the top-level `auth` command and its
// login subcommand registration.
func TestAuthCmdShape(t *testing.T) {
	t.Parallel()

	c := authCmd()
	if c.Name != "auth" {
		t.Fatalf("name = %q, want auth", c.Name)
	}
	if len(c.Subcommands) == 0 {
		t.Fatalf("expected at least one subcommand")
	}
	found := false
	for _, sc := range c.Subcommands {
		if sc.Name == "login" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("subcommand \"login\" missing")
	}
}

// TestAuthLoginMissingPluginArg checks that invoking `warp auth login`
// without a plugin-id returns without attempting a daemon connection.
// cli.ShowCommandHelp is the expected exit path; whether it returns nil
// (command registered on app) or a "No help topic" error (command not
// registered, as in this test's stub app) is incidental — the load-bearing
// invariant is that no RPC call is made.
func TestAuthLoginMissingPluginArg(t *testing.T) {
	t.Parallel()

	ctx := newAuthLoginContext(nil, nil)
	err := authLogin(ctx)
	if err != nil && strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("should not attempt daemon connect without plugin-id: %v", err)
	}
	// Also: whitespace-only plugin id must be treated as missing.
	ctxBlank := newAuthLoginContext([]string{"   "}, nil)
	err = authLogin(ctxBlank)
	if err != nil && strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("whitespace plugin-id should not reach daemon: %v", err)
	}
}

// TestCallbackServerSuccess drives the full PKCE callback happy path
// via an in-process HTTP round-trip. Verifies the /callback handler
// invokes AuthComplete with the right flow id + code + state, renders
// a success page, and writes nil to the callback channel.
func TestCallbackServerSuccess(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{completeRes: &common.AuthCompleteResult{Account: "default"}}
	ch := make(chan error, 1)
	srv := newCallbackServer(fake, "flow-123", ch)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?code=abc&state=xyz", nil)
	srv.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Authentication complete") {
		t.Fatalf("body missing success title: %q", body)
	}

	select {
	case err := <-ch:
		if err != nil {
			t.Fatalf("callbackCh err = %v, want nil", err)
		}
	default:
		t.Fatalf("no verdict on callbackCh")
	}

	if got := len(fake.completeCalls); got != 1 {
		t.Fatalf("AuthComplete calls = %d, want 1", got)
	}
	call := fake.completeCalls[0]
	if call.FlowID != "flow-123" || call.Code != "abc" || call.State != "xyz" {
		t.Fatalf("AuthComplete params = %+v", call)
	}
}

// TestCallbackServerProviderError checks the branch where the IdP
// redirected with an ?error= query param.
func TestCallbackServerProviderError(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	ch := make(chan error, 1)
	srv := newCallbackServer(fake, "f", ch)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?error=access_denied&error_description=user+denied", nil)
	srv.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Authentication failed") {
		t.Fatalf("body missing failure title: %q", rr.Body.String())
	}

	select {
	case err := <-ch:
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "access_denied") {
			t.Fatalf("err = %v, want one containing access_denied", err)
		}
	default:
		t.Fatalf("no verdict on callbackCh")
	}

	if len(fake.completeCalls) != 0 {
		t.Fatalf("AuthComplete should not be called on provider error")
	}
}

// TestCallbackServerMissingFields covers a callback with neither code
// nor state — malformed URL case.
func TestCallbackServerMissingFields(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	ch := make(chan error, 1)
	srv := newCallbackServer(fake, "f", ch)

	cases := []string{
		"/callback",
		"/callback?code=abc",  // missing state
		"/callback?state=xyz", // missing code
	}
	for _, target := range cases {
		t.Run(target, func(t *testing.T) {
			// reset channel buffer between subtests
			select {
			case <-ch:
			default:
			}

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			srv.Handler.ServeHTTP(rr, req)

			if !strings.Contains(rr.Body.String(), "Authentication failed") {
				t.Fatalf("target=%s body = %q", target, rr.Body.String())
			}
			select {
			case err := <-ch:
				if err == nil {
					t.Fatalf("target=%s expected err", target)
				}
				if !strings.Contains(err.Error(), "code") && !strings.Contains(err.Error(), "state") {
					t.Fatalf("target=%s err = %v", target, err)
				}
			default:
				t.Fatalf("target=%s no verdict", target)
			}
		})
	}
}

// TestCallbackServerCompleteFails exercises the path where
// AuthComplete itself fails (e.g. token exchange blew up at the IdP).
func TestCallbackServerCompleteFails(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{completeErr: errors.New("bad token")}
	ch := make(chan error, 1)
	srv := newCallbackServer(fake, "f", ch)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?code=abc&state=xyz", nil)
	srv.Handler.ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "Token exchange failed") {
		t.Fatalf("body = %q", rr.Body.String())
	}
	select {
	case err := <-ch:
		if err == nil || !strings.Contains(err.Error(), "bad token") {
			t.Fatalf("err = %v", err)
		}
	default:
		t.Fatalf("no verdict")
	}
}

// TestCallbackServerChannelFull verifies the handler does not block
// if a verdict was already written (buffered channel + default case).
func TestCallbackServerChannelFull(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{completeRes: &common.AuthCompleteResult{}}
	ch := make(chan error, 1)
	ch <- errors.New("prefill") // saturate the buffer

	srv := newCallbackServer(fake, "f", ch)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/callback?code=c&state=s", nil)

	done := make(chan struct{})
	go func() {
		srv.Handler.ServeHTTP(rr, req)
		close(done)
	}()

	select {
	case <-done:
		// ok: handler did not block
	default:
		// extremely short time slice; retry-wait a bit in a CI-safe way
		<-done
	}
}

// TestErrorMessage covers both branches of the OAuth error formatter.
func TestErrorMessage(t *testing.T) {
	t.Parallel()

	if got := errorMessage("access_denied", ""); got != "access_denied" {
		t.Fatalf("got %q", got)
	}
	if got := errorMessage("access_denied", "user denied"); got != "access_denied (user denied)" {
		t.Fatalf("got %q", got)
	}
}

// TestRunPKCEFlow_LoginFailure ensures a failed auth.login call
// propagates cleanly and does not leak the listener. We can't easily
// stub net.Listen, so this test calls runPKCEFlow with a client whose
// AuthLogin fails — runPKCEFlow must close the listener and return.
func TestRunPKCEFlow_LoginFailure(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{loginErr: errors.New("daemon offline")}
	var buf bytes.Buffer
	err := runPKCEFlow(&buf, fake, "plugin", "default", nil, true)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "daemon offline") {
		t.Fatalf("err = %v", err)
	}
	if len(fake.loginCalls) != 1 {
		t.Fatalf("login calls = %d", len(fake.loginCalls))
	}
}

// TestRunPKCEFlow_LoginParams verifies runPKCEFlow sends a non-empty
// loopback redirect URI that points at 127.0.0.1.
func TestRunPKCEFlow_LoginParams(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{loginErr: errors.New("stop here")}
	var buf bytes.Buffer
	_ = runPKCEFlow(&buf, fake, "plug", "acct", []string{"read"}, true)
	if len(fake.loginCalls) != 1 {
		t.Fatalf("expected exactly one login call")
	}
	call := fake.loginCalls[0]
	if call.PluginID != "plug" {
		t.Fatalf("PluginID = %q", call.PluginID)
	}
	if call.Account != "acct" {
		t.Fatalf("Account = %q", call.Account)
	}
	if call.Flow != "pkce" {
		t.Fatalf("Flow = %q, want pkce", call.Flow)
	}
	if !strings.HasPrefix(call.RedirectURI, "http://127.0.0.1:") {
		t.Fatalf("RedirectURI = %q, want loopback", call.RedirectURI)
	}
	if !strings.HasSuffix(call.RedirectURI, "/callback") {
		t.Fatalf("RedirectURI = %q, want /callback suffix", call.RedirectURI)
	}
	if len(call.Scopes) != 1 || call.Scopes[0] != "read" {
		t.Fatalf("Scopes = %v", call.Scopes)
	}
}

// TestRunDeviceFlow_LoginFailure checks the device flow surfaces the
// daemon error without leaking timers or goroutines.
func TestRunDeviceFlow_LoginFailure(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{loginErr: errors.New("not implemented")}
	var buf bytes.Buffer
	err := runDeviceFlow(&buf, fake, "plug", "acct", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("err = %v", err)
	}
	if got := len(fake.loginCalls); got != 1 {
		t.Fatalf("login calls = %d", got)
	}
	if fake.loginCalls[0].Flow != "device" {
		t.Fatalf("Flow = %q", fake.loginCalls[0].Flow)
	}
}

// Compile-time check: the interface must stay in sync with the methods
// used inside runPKCEFlow / runDeviceFlow.
var _ authRPC = (*fakeAuthRPC)(nil)
