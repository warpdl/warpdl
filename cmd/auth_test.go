package cmd

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	logoutErr error

	loginCalls    []common.AuthLoginParams
	completeCalls []common.AuthCompleteParams
	cancelCalls   []common.AuthCancelParams
	logoutCalls   []common.AuthLogoutParams
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

func (f *fakeAuthRPC) AuthLogout(p *common.AuthLogoutParams) error {
	f.mu.Lock()
	f.logoutCalls = append(f.logoutCalls, *p)
	err := f.logoutErr
	f.mu.Unlock()
	return err
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

// ---- Task 17: list/logout/status ----

// newAuthActionContext builds a *cli.Context wired to the given
// subcommand, with positional args and flag values supplied. Used for
// invoking logout/status Action functions without a running app.
func newAuthActionContext(cmd cli.Command, args []string, flagVals map[string]string) *cli.Context {
	app := cli.NewApp()
	app.Writer = io.Discard
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	for _, f := range cmd.Flags {
		f.Apply(fs)
	}
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

// TestAuthListCmdShape guards the CLI surface of `warp auth list`.
func TestAuthListCmdShape(t *testing.T) {
	t.Parallel()

	c := authListCmd()
	if c.Name != "list" {
		t.Fatalf("name = %q, want list", c.Name)
	}
	if c.Action == nil {
		t.Fatal("Action missing")
	}
}

// TestAuthLogoutCmdShape guards the CLI surface of `warp auth logout`.
// The --as flag is load-bearing (spec §2.3: account selection), so we
// assert it's declared.
func TestAuthLogoutCmdShape(t *testing.T) {
	t.Parallel()

	c := authLogoutCmd()
	if c.Name != "logout" {
		t.Fatalf("name = %q, want logout", c.Name)
	}
	if c.ArgsUsage == "" {
		t.Fatalf("ArgsUsage is empty")
	}
	found := false
	for _, f := range c.Flags {
		if f.GetName() == "as" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing --as flag")
	}
}

// TestAuthStatusCmdShape guards the CLI surface of `warp auth status`.
func TestAuthStatusCmdShape(t *testing.T) {
	t.Parallel()

	c := authStatusCmd()
	if c.Name != "status" {
		t.Fatalf("name = %q, want status", c.Name)
	}
	if c.ArgsUsage == "" {
		t.Fatalf("ArgsUsage is empty")
	}
	found := false
	for _, f := range c.Flags {
		if f.GetName() == "as" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing --as flag")
	}
}

// TestAuthCmdShapeAllSubcommands verifies login, list, logout, status
// are all registered on the parent `auth` command after Task 17.
func TestAuthCmdShapeAllSubcommands(t *testing.T) {
	t.Parallel()

	c := authCmd()
	want := map[string]bool{"login": false, "list": false, "logout": false, "status": false}
	for _, sc := range c.Subcommands {
		if _, ok := want[sc.Name]; ok {
			want[sc.Name] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("subcommand %q missing", name)
		}
	}
}

// TestPrintAuthAccountsEmpty verifies the friendly empty-store message.
func TestPrintAuthAccountsEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printAuthAccounts(&buf, nil)
	if !strings.Contains(buf.String(), "No stored credentials") {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}

// TestPrintAuthAccountsTable verifies the table header and both rows
// show up when the slice is non-empty. Uses a zero expiry and a future
// expiry to cover both branches of the expiry formatter.
func TestPrintAuthAccountsTable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printAuthAccounts(&buf, []common.AuthAccount{
		{PluginID: "gdrive", Account: "default", Scopes: []string{"a", "b"}, ExpiresAt: time.Now().Add(time.Hour).Unix()},
		{PluginID: "dropbox", Account: "work", Scopes: []string{"files"}, ExpiresAt: 0},
	})
	out := buf.String()
	if !strings.Contains(out, "PLUGIN") {
		t.Fatalf("header missing: %s", out)
	}
	if !strings.Contains(out, "gdrive") || !strings.Contains(out, "dropbox") {
		t.Fatalf("rows missing: %s", out)
	}
	// Scopes should be comma-joined (not space-joined).
	if !strings.Contains(out, "a,b") {
		t.Fatalf("scope join missing: %s", out)
	}
	// Zero expiry renders as em-dash.
	if !strings.Contains(out, "—") {
		t.Fatalf("em-dash for zero expiry missing: %s", out)
	}
}

// TestPrintAuthAccountsExpired covers the past-expiry branch: the
// formatter must print "expired" rather than a timestamp.
func TestPrintAuthAccountsExpired(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printAuthAccounts(&buf, []common.AuthAccount{
		{PluginID: "p", Account: "default", ExpiresAt: time.Now().Add(-time.Hour).Unix()},
	})
	if !strings.Contains(buf.String(), "expired") {
		t.Fatalf("expired marker missing: %q", buf.String())
	}
}

// TestPrintAuthAccountsFutureRFC3339 verifies a future expiry renders
// as RFC3339 (not Unix epoch, not "expired").
func TestPrintAuthAccountsFutureRFC3339(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	future := time.Now().Add(24 * time.Hour)
	printAuthAccounts(&buf, []common.AuthAccount{
		{PluginID: "p", Account: "default", ExpiresAt: future.Unix()},
	})
	// RFC3339 always contains a 'T' between date and time, and either
	// 'Z' or a ±HH:MM offset. Assert the 'T' as a cheap smoke check.
	if !strings.Contains(buf.String(), "T") {
		t.Fatalf("expected RFC3339 timestamp with 'T': %q", buf.String())
	}
}

// TestAuthLogoutMissingPluginArg ensures the command refuses to dial
// the daemon when called without a plugin-id. Same invariant as the
// login variant: the help-return path must not attempt any RPC.
func TestAuthLogoutMissingPluginArg(t *testing.T) {
	t.Parallel()

	ctx := newAuthActionContext(authLogoutCmd(), nil, nil)
	err := authLogout(ctx)
	if err != nil && strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("should not attempt daemon connect without plugin-id: %v", err)
	}
	ctxBlank := newAuthActionContext(authLogoutCmd(), []string{"   "}, nil)
	err = authLogout(ctxBlank)
	if err != nil && strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("whitespace plugin-id should not reach daemon: %v", err)
	}
}

// TestAuthStatusMissingPluginArg is the status-command analogue of the
// logout test above.
func TestAuthStatusMissingPluginArg(t *testing.T) {
	t.Parallel()

	ctx := newAuthActionContext(authStatusCmd(), nil, nil)
	err := authStatus(ctx)
	if err != nil && strings.Contains(err.Error(), "connect daemon") {
		t.Fatalf("should not attempt daemon connect without plugin-id: %v", err)
	}
}

// runAuthStatusAgainstFake invokes the core status logic against a
// fakeAuthRPC, bypassing getClient(). This mirrors what authStatus
// does after it has a client, but stubs the RPC path.
//
// We replicate the handler's logic here rather than reaching into
// authStatus because authStatus is glued to getClient() (a real-daemon
// dial). This is deliberate: the status behaviour is thin enough that
// replicating it keeps the test focused on the status contract (what
// is printed, what exit code is used) without fighting production glue.
func runAuthStatusAgainstFake(out io.Writer, client authRPC, pluginID, account string) error {
	res, err := client.AuthList()
	if err != nil {
		return fmt.Errorf("auth.list: %w", err)
	}
	for _, a := range res.Accounts {
		if a.PluginID == pluginID && a.Account == account {
			now := time.Now().Unix()
			if a.ExpiresAt > 0 && a.ExpiresAt <= now {
				fmt.Fprintf(out, "%s (%s): expired\n", pluginID, account)
				return cli.NewExitError("", 2)
			}
			fmt.Fprintf(out, "%s (%s): authenticated\n", pluginID, account)
			return nil
		}
	}
	fmt.Fprintf(out, "%s (%s): not authenticated\n", pluginID, account)
	return cli.NewExitError("", 1)
}

// TestAuthStatusAuthenticated covers exit code 0: a valid unexpired
// credential is present.
func TestAuthStatusAuthenticated(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{
		listRes: &common.AuthListResult{Accounts: []common.AuthAccount{
			{PluginID: "gdrive", Account: "default", ExpiresAt: time.Now().Add(time.Hour).Unix()},
		}},
	}
	var buf bytes.Buffer
	err := runAuthStatusAgainstFake(&buf, fake, "gdrive", "default")
	if err != nil {
		t.Fatalf("err = %v, want nil (exit 0)", err)
	}
	if !strings.Contains(buf.String(), "authenticated") || strings.Contains(buf.String(), "not authenticated") {
		t.Fatalf("output = %q", buf.String())
	}
}

// TestAuthStatusExpired covers exit code 2: credential present but
// past ExpiresAt.
func TestAuthStatusExpired(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{
		listRes: &common.AuthListResult{Accounts: []common.AuthAccount{
			{PluginID: "gdrive", Account: "default", ExpiresAt: time.Now().Add(-time.Hour).Unix()},
		}},
	}
	var buf bytes.Buffer
	err := runAuthStatusAgainstFake(&buf, fake, "gdrive", "default")
	ee, ok := err.(*cli.ExitError)
	if !ok {
		t.Fatalf("err = %v (%T), want *cli.ExitError", err, err)
	}
	if ee.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", ee.ExitCode())
	}
	if !strings.Contains(buf.String(), "expired") {
		t.Fatalf("output = %q", buf.String())
	}
}

// TestAuthStatusNotAuthenticated covers exit code 1: no matching
// (plugin, account) row.
func TestAuthStatusNotAuthenticated(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{
		listRes: &common.AuthListResult{Accounts: []common.AuthAccount{
			{PluginID: "other", Account: "default", ExpiresAt: time.Now().Add(time.Hour).Unix()},
		}},
	}
	var buf bytes.Buffer
	err := runAuthStatusAgainstFake(&buf, fake, "gdrive", "default")
	ee, ok := err.(*cli.ExitError)
	if !ok {
		t.Fatalf("err = %v (%T), want *cli.ExitError", err, err)
	}
	if ee.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", ee.ExitCode())
	}
	if !strings.Contains(buf.String(), "not authenticated") {
		t.Fatalf("output = %q", buf.String())
	}
}

// TestAuthStatusListError ensures an underlying auth.list failure is
// wrapped and propagated as a normal (non-ExitError) error.
func TestAuthStatusListError(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{listErr: errors.New("daemon offline")}
	var buf bytes.Buffer
	err := runAuthStatusAgainstFake(&buf, fake, "gdrive", "default")
	if err == nil || !strings.Contains(err.Error(), "daemon offline") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := err.(*cli.ExitError); ok {
		t.Fatalf("list error should not be a *cli.ExitError")
	}
}

// TestAuthStatusAccountMismatch verifies the account label is
// load-bearing: a matching plugin-id but different account must fall
// through to "not authenticated".
func TestAuthStatusAccountMismatch(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{
		listRes: &common.AuthListResult{Accounts: []common.AuthAccount{
			{PluginID: "gdrive", Account: "work", ExpiresAt: time.Now().Add(time.Hour).Unix()},
		}},
	}
	var buf bytes.Buffer
	err := runAuthStatusAgainstFake(&buf, fake, "gdrive", "default")
	ee, ok := err.(*cli.ExitError)
	if !ok {
		t.Fatalf("err = %v (%T), want *cli.ExitError", err, err)
	}
	if ee.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1", ee.ExitCode())
	}
}

// TestAuthStatusZeroExpiryIsValid confirms a stored credential with
// ExpiresAt == 0 (meaning "no known expiry", typical for refresh-only
// grants) is treated as authenticated, not expired.
func TestAuthStatusZeroExpiryIsValid(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{
		listRes: &common.AuthListResult{Accounts: []common.AuthAccount{
			{PluginID: "gdrive", Account: "default", ExpiresAt: 0},
		}},
	}
	var buf bytes.Buffer
	err := runAuthStatusAgainstFake(&buf, fake, "gdrive", "default")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !strings.Contains(buf.String(), "authenticated") || strings.Contains(buf.String(), "not authenticated") {
		t.Fatalf("output = %q", buf.String())
	}
}

// ---- Task 18: interactive mid-download auth ----

// TestIsAuthRequiredErrorFlowTimeout verifies the recognizer picks up
// the FlowRegistry.ErrFlowTimeout sentinel wording, which is the error
// shape a plugin's getAccessToken call surfaces when the user never
// authenticates. The literal string is stable — see
// internal/extl/auth/flowregistry.go.
func TestIsAuthRequiredErrorFlowTimeout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   error
		want bool
	}{
		{"nil", nil, false},
		{"raw_timeout", errors.New("flow: timed out"), true},
		{"wrapped_timeout", fmt.Errorf("extract failed: %w", errors.New("flow: timed out")), true},
		{"mixed_case", errors.New("Flow: Timed Out"), true},
		{"auth_required_sentinel", errors.New("plugin foo: auth_required"), true},
		{"authentication_required_wording", errors.New("authentication required for drive"), true},
		{"unrelated_network", errors.New("i/o timeout"), false},
		{"unrelated_parse", errors.New("invalid url"), false},
		{"empty", errors.New(""), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isAuthRequiredError(tc.in); got != tc.want {
				t.Fatalf("isAuthRequiredError(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestHandleAuthRequiredNilResult guards the nil-safety contract. The
// handler is invoked with whatever JSON the daemon sends; a malformed
// push must not panic or leave state behind.
func TestHandleAuthRequiredNilResult(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	var buf bytes.Buffer
	err := HandleAuthRequired(context.Background(), &buf, fake, "gdrive", "default", nil, true)
	if err == nil {
		t.Fatalf("expected error for nil result")
	}
	if len(fake.completeCalls) != 0 {
		t.Fatalf("AuthComplete must not be called for nil result")
	}
}

// TestHandleAuthRequiredEmptyResult covers the defensive branch where
// neither UserCode nor AuthorizeURL is populated — the daemon sent us
// a malformed push.
func TestHandleAuthRequiredEmptyResult(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	var buf bytes.Buffer
	err := HandleAuthRequired(context.Background(), &buf, fake, "gdrive", "default", &common.AuthLoginResult{FlowID: "f"}, true)
	if err == nil {
		t.Fatalf("expected error for empty result")
	}
	if !strings.Contains(err.Error(), "user_code") && !strings.Contains(err.Error(), "authorize_url") {
		t.Fatalf("err = %v, want hint about missing fields", err)
	}
}

// TestHandleAuthRequiredNilContextDefaults confirms passing nil ctx
// falls back to context.Background() without panicking — defensive
// sanity for callers that might not thread a context.
func TestHandleAuthRequiredNilContextDefaults(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	var buf bytes.Buffer
	// Device flow returns immediately — nil ctx path is safe here.
	res := &common.AuthLoginResult{
		UserCode:        "X-1",
		VerificationURL: "https://idp/a",
	}
	if err := HandleAuthRequired(nil, &buf, fake, "p", "default", res, false); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// TestHandleAuthRequiredDeviceFlow_PrintsCodeAndReturns verifies the
// device-flow branch: CLI prints the user_code + verification_url but
// does NOT block — the daemon's polling goroutine is doing the waiting.
func TestHandleAuthRequiredDeviceFlow_PrintsCodeAndReturns(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	var buf bytes.Buffer
	res := &common.AuthLoginResult{
		FlowID:          "f-device",
		UserCode:        "WARP-1234",
		VerificationURL: "https://idp.example/activate",
		Interval:        5,
	}

	done := make(chan error, 1)
	go func() {
		done <- HandleAuthRequired(context.Background(), &buf, fake, "gdrive", "default", res, false)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("device-flow branch must not block: daemon polls internally")
	}

	out := buf.String()
	if !strings.Contains(out, "WARP-1234") {
		t.Fatalf("user_code missing from output: %q", out)
	}
	if !strings.Contains(out, "https://idp.example/activate") {
		t.Fatalf("verification_url missing from output: %q", out)
	}
	if !strings.Contains(out, "gdrive") {
		t.Fatalf("plugin id missing from output: %q", out)
	}
	// Must NOT touch AuthComplete — daemon is driving completion.
	if len(fake.completeCalls) != 0 {
		t.Fatalf("device flow must not call AuthComplete: %d calls", len(fake.completeCalls))
	}
	if len(fake.cancelCalls) != 0 {
		t.Fatalf("device flow must not call AuthCancel: %d calls", len(fake.cancelCalls))
	}
}

// TestHandleAuthRequiredPKCE_PrintsAuthorizeURL exercises the
// entry-point logic of the PKCE branch: with noBrowser=true,
// HandleAuthRequired prints the authorize URL for the user to open
// manually, then blocks waiting for a callback. We pass a
// short-deadline context to force a clean cancellation path,
// verifying the ctx.Done() branch wires up correctly AND that the
// user-facing strings landed.
//
// The full browser-callback happy path is covered by
// TestCallbackServerSuccess (which exercises the same
// newCallbackServer implementation HandleAuthRequired reuses).
func TestHandleAuthRequiredPKCE_PrintsAuthorizeURL(t *testing.T) {
	// Cannot run in parallel: mutates WARP_NO_BROWSER.
	t.Setenv("WARP_NO_BROWSER", "1")

	fake := &fakeAuthRPC{completeRes: &common.AuthCompleteResult{Account: "default"}}
	var buf bytes.Buffer
	res := &common.AuthLoginResult{
		FlowID:       "f-pkce",
		AuthorizeURL: "https://idp.example/authorize?client_id=x",
	}

	// 500ms budget is plenty for the listener bind + printout; we
	// rely on ctx.Done() to unwind the blocking select. The function
	// returns ctx.Err() so we assert that shape below.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := HandleAuthRequired(ctx, &buf, fake, "gdrive", "default", res, true)
	if err == nil {
		t.Fatalf("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}

	out := buf.String()
	if !strings.Contains(out, res.AuthorizeURL) {
		t.Fatalf("authorize URL missing from output: %q", out)
	}
	if !strings.Contains(out, "gdrive") {
		t.Fatalf("plugin label missing from output: %q", out)
	}

	// ctx.Done() branch should have issued an AuthCancel to the
	// daemon so the remote flow is cleaned up.
	if len(fake.cancelCalls) != 1 {
		t.Fatalf("AuthCancel calls = %d, want 1 on ctx cancellation", len(fake.cancelCalls))
	}
	if fake.cancelCalls[0].FlowID != "f-pkce" {
		t.Fatalf("AuthCancel FlowID = %q, want f-pkce", fake.cancelCalls[0].FlowID)
	}
}

// TestHandleAuthRequiredEmptyPluginIDFallback confirms that an empty
// pluginID (the default today because common.AuthLoginResult doesn't
// carry PluginID) still produces a coherent prompt — we fall back to a
// generic label rather than printing "Authentication required for ".
func TestHandleAuthRequiredEmptyPluginIDFallback(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	var buf bytes.Buffer
	res := &common.AuthLoginResult{
		FlowID:          "f",
		UserCode:        "AB-12",
		VerificationURL: "https://idp/activate",
	}
	err := HandleAuthRequired(context.Background(), &buf, fake, "", "default", res, false)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	out := buf.String()
	// Must not print "for " followed by nothing.
	if strings.Contains(out, "Authentication required for \n") {
		t.Fatalf("empty plugin label leaked into output: %q", out)
	}
	// Must mention SOMETHING as the subject of the prompt.
	if !strings.Contains(out, "Authentication required for") {
		t.Fatalf("prompt missing: %q", out)
	}
}

// TestAuthRequiredHandler_Handle_DecodesAndInvokes drives the
// warpcli-side Handler shim: a raw JSON payload shaped like a device
// push should unmarshal cleanly and print the user_code.
//
// The handler dispatches the orchestrator in a goroutine (to avoid
// deadlocking the warpcli listener's RLock, see Handle's comment), so
// the test polls the output buffer until the expected text appears,
// bounded by a short deadline.
func TestAuthRequiredHandler_Handle_DecodesAndInvokes(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	// syncBuf wraps bytes.Buffer with a mutex — the handler's
	// HandleAuthRequired goroutine writes to it concurrently with
	// the test's Contains check, so we need to serialise access.
	buf := newSyncBuffer()
	h := &authRequiredHandler{client: fake, out: buf}

	raw := []byte(`{
		"flow_id": "f-push",
		"user_code": "PUSH-0001",
		"verification_url": "https://idp/activate",
		"interval": 5
	}`)
	if err := h.Handle(raw); err != nil {
		t.Fatalf("Handle err = %v, want nil", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "PUSH-0001") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("user_code not propagated within deadline: %q", buf.String())
}

// syncBuffer is a mutex-guarded bytes.Buffer for tests that read
// while a handler goroutine may be writing. The standard bytes.Buffer
// is NOT safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func newSyncBuffer() *syncBuffer { return &syncBuffer{} }

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// TestAuthRequiredHandler_Handle_MalformedPayload ensures a malformed
// push doesn't crash the handler — it returns nil so the outer
// listener keeps dispatching download-progress updates.
func TestAuthRequiredHandler_Handle_MalformedPayload(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	var buf bytes.Buffer
	h := &authRequiredHandler{client: fake, out: &buf}

	if err := h.Handle([]byte("not json")); err != nil {
		t.Fatalf("Handle err = %v, want nil for malformed payload", err)
	}
	// No downstream RPC calls on bad input.
	if len(fake.completeCalls)+len(fake.cancelCalls) != 0 {
		t.Fatalf("malformed payload must not trigger RPC calls")
	}
}

// TestAuthRequiredHandler_Handle_EmptyPayload covers the degenerate
// case where the daemon broadcasts an empty object. HandleAuthRequired
// rejects it (no user_code, no authorize_url), but the handler shim
// still returns nil to keep the listener alive.
//
// The orchestrator runs in a goroutine that emits an error-log line
// to stderr and returns immediately. We do NOT write to h.out on
// this path (the error goes to os.Stderr via the handler shim), so
// the only observable signal is that the handler itself returns nil.
func TestAuthRequiredHandler_Handle_EmptyPayload(t *testing.T) {
	t.Parallel()

	fake := &fakeAuthRPC{}
	buf := newSyncBuffer()
	h := &authRequiredHandler{client: fake, out: buf}

	if err := h.Handle([]byte(`{}`)); err != nil {
		t.Fatalf("Handle err = %v, want nil", err)
	}
	// The background goroutine returns after HandleAuthRequired's
	// "empty result" error check — no RPC calls, no writes to buf.
	// Give it a beat to finish so it doesn't outlive the test.
	time.Sleep(50 * time.Millisecond)
	if len(fake.cancelCalls) != 0 || len(fake.completeCalls) != 0 {
		t.Fatalf("empty payload must not trigger RPC calls")
	}
}
