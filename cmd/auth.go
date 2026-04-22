package cmd

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

// authLoginTimeout bounds how long `warp auth login` waits for the
// user to complete the browser flow. 5 minutes matches the daemon's
// FlowRegistry expiry so neither side outlives the other.
const authLoginTimeout = 5 * time.Minute

// authCallbackShutdown bounds the loopback server's graceful shutdown
// so a stuck client cannot prevent CLI exit.
const authCallbackShutdown = 2 * time.Second

// authCmd returns the `warp auth` subcommand tree. Tasks 17 and 18
// append `list`, `logout`, and `status` subcommands; this file keeps
// only the Task 16 surface (`login`).
func authCmd() cli.Command {
	return cli.Command{
		Name:  "auth",
		Usage: "Manage OAuth credentials for warpdl plugins",
		Subcommands: []cli.Command{
			authLoginCmd(),
		},
	}
}

// authLoginCmd builds the `warp auth login <plugin-id>` subcommand.
func authLoginCmd() cli.Command {
	return cli.Command{
		Name:      "login",
		Usage:     "Log in to an OAuth-backed plugin",
		ArgsUsage: "<plugin-id>",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "as",
				Usage: "account label (default \"default\")",
			},
			cli.StringSliceFlag{
				Name:  "scopes",
				Usage: "scope subset to request (default: manifest scopes)",
			},
			cli.BoolFlag{
				Name:  "device",
				Usage: "use device-code flow instead of loopback PKCE",
			},
			cli.BoolFlag{
				Name:  "no-browser",
				Usage: "print the authorize URL instead of opening a browser",
			},
		},
		Action: authLogin,
	}
}

// authLogin is the Action for `warp auth login`. It validates the
// plugin-id arg, establishes a daemon client, then dispatches to
// either the PKCE loopback flow or the device-code flow.
func authLogin(ctx *cli.Context) error {
	pluginID := strings.TrimSpace(ctx.Args().First())
	if pluginID == "" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	account := ctx.String("as")
	if account == "" {
		account = "default"
	}
	// Normalize the scopes slice so downstream code never sees a nil
	// that a caller may have passed through StringSlice.
	scopes := append([]string(nil), ctx.StringSlice("scopes")...)

	client, err := getClient()
	if err != nil {
		return fmt.Errorf("connect daemon: %w", err)
	}
	defer client.Close()

	if ctx.Bool("device") {
		return runDeviceFlow(ctx.App.Writer, client, pluginID, account, scopes)
	}
	return runPKCEFlow(ctx.App.Writer, client, pluginID, account, scopes, ctx.Bool("no-browser"))
}

// authRPC is the subset of warpcli.Client methods the auth subcommand
// touches. Breaking it out as an interface lets tests stub the daemon
// without pulling in the real dial/listen plumbing.
type authRPC interface {
	AuthLogin(*common.AuthLoginParams) (*common.AuthLoginResult, error)
	AuthComplete(*common.AuthCompleteParams) (*common.AuthCompleteResult, error)
	AuthCancel(*common.AuthCancelParams) error
	AuthList() (*common.AuthListResult, error)
}

// compile-time check: the concrete client must satisfy the interface.
var _ authRPC = (*warpcli.Client)(nil)

// runPKCEFlow binds a loopback HTTP server on 127.0.0.1:<random>,
// asks the daemon to start a PKCE flow with that redirect URI, opens
// the user's browser (or prints the URL), and blocks until the
// browser hits /callback, SIGINT fires, or the 5-minute deadline
// elapses. On any terminal outcome the loopback server shuts down
// and — if the flow did not complete — the daemon flow is cancelled.
func runPKCEFlow(out io.Writer, client authRPC, pluginID, account string, scopes []string, noBrowser bool) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("bind loopback: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	loginRes, err := client.AuthLogin(&common.AuthLoginParams{
		PluginID:    pluginID,
		Account:     account,
		Scopes:      scopes,
		Flow:        "pkce",
		RedirectURI: redirectURI,
	})
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("auth.login: %w", err)
	}

	callbackCh := make(chan error, 1)
	srv := newCallbackServer(client, loginRes.FlowID, callbackCh)
	go func() {
		// Serve returns ErrServerClosed on graceful shutdown; we only
		// surface other errors, and only if the callback channel
		// hasn't already delivered a verdict.
		if serr := srv.Serve(listener); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			select {
			case callbackCh <- fmt.Errorf("loopback server: %w", serr):
			default:
			}
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), authCallbackShutdown)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if noBrowser {
		fmt.Fprintf(out, "Please open this URL in your browser:\n  %s\n", loginRes.AuthorizeURL)
	} else {
		_ = openBrowser(out, loginRes.AuthorizeURL)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	fmt.Fprintln(out, "Waiting for browser callback...")
	select {
	case err := <-callbackCh:
		if err != nil {
			// Tell the daemon to drop its flow state. We intentionally
			// ignore cancel errors because the primary error is the
			// one the user cares about.
			_ = client.AuthCancel(&common.AuthCancelParams{FlowID: loginRes.FlowID})
			return err
		}
		fmt.Fprintf(out, "Logged in to %s as %q\n", pluginID, account)
		return nil
	case <-sigCh:
		_ = client.AuthCancel(&common.AuthCancelParams{FlowID: loginRes.FlowID})
		return errors.New("cancelled by user")
	case <-time.After(authLoginTimeout):
		_ = client.AuthCancel(&common.AuthCancelParams{FlowID: loginRes.FlowID})
		return fmt.Errorf("authentication timed out after %s", authLoginTimeout)
	}
}

// newCallbackServer builds the single-endpoint HTTP server that handles
// the OAuth2 loopback redirect. On every terminal branch it writes a
// verdict to callbackCh (non-blocking: the channel is buffered 1 and we
// use a default branch to avoid double-writes in case of buggy retries).
func newCallbackServer(client authRPC, flowID string, callbackCh chan<- error) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			renderCallbackPage(w, "Authentication failed", fmt.Sprintf("Provider error: %s", errorMessage(e, desc)))
			select {
			case callbackCh <- fmt.Errorf("provider error: %s", errorMessage(e, desc)):
			default:
			}
			return
		}
		code := q.Get("code")
		state := q.Get("state")
		if code == "" || state == "" {
			renderCallbackPage(w, "Authentication failed", "Missing code or state parameter.")
			select {
			case callbackCh <- errors.New("callback missing code or state"):
			default:
			}
			return
		}
		if _, cerr := client.AuthComplete(&common.AuthCompleteParams{
			FlowID: flowID,
			Code:   code,
			State:  state,
		}); cerr != nil {
			renderCallbackPage(w, "Authentication failed", fmt.Sprintf("Token exchange failed: %v", cerr))
			select {
			case callbackCh <- fmt.Errorf("auth.complete: %w", cerr):
			default:
			}
			return
		}
		renderCallbackPage(w, "Authentication complete", "You can close this window.")
		select {
		case callbackCh <- nil:
		default:
		}
	})
	// ReadHeaderTimeout guards against slowloris on the loopback even
	// though the attack surface is tiny (local only, short lifetime).
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

// runDeviceFlow asks the daemon to start a device-code flow, prints the
// user_code + verification_url, then polls auth.list until the target
// (plugin_id, account) pair appears. Task 18 will replace the polling
// loop with a proper UPDATE_AUTH_COMPLETED subscription.
func runDeviceFlow(out io.Writer, client authRPC, pluginID, account string, scopes []string) error {
	loginRes, err := client.AuthLogin(&common.AuthLoginParams{
		PluginID: pluginID,
		Account:  account,
		Scopes:   scopes,
		Flow:     "device",
	})
	if err != nil {
		return fmt.Errorf("auth.login: %w", err)
	}

	fmt.Fprintf(out, "Open: %s\n", loginRes.VerificationURL)
	fmt.Fprintf(out, "Enter code: %s\n", loginRes.UserCode)
	fmt.Fprintln(out, "Waiting for confirmation...")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	deadline := time.Now().Add(authLoginTimeout)
	if loginRes.ExpiresAt > 0 {
		if exp := time.Unix(loginRes.ExpiresAt, 0); exp.Before(deadline) {
			deadline = exp
		}
	}
	interval := time.Duration(loginRes.Interval) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-tick.C:
			if time.Now().After(deadline) {
				_ = client.AuthCancel(&common.AuthCancelParams{FlowID: loginRes.FlowID})
				return errors.New("device flow timed out")
			}
			if listRes, lerr := client.AuthList(); lerr == nil && listRes != nil {
				for _, a := range listRes.Accounts {
					if a.PluginID == pluginID && a.Account == account {
						fmt.Fprintf(out, "Logged in to %s as %q\n", pluginID, account)
						return nil
					}
				}
			}
		case <-sigCh:
			_ = client.AuthCancel(&common.AuthCancelParams{FlowID: loginRes.FlowID})
			return errors.New("cancelled by user")
		}
	}
}

// errorMessage formats an OAuth2 error response. When error_description
// is present we surface it because it's often the only human-readable
// hint the provider gave us.
func errorMessage(code, description string) string {
	if description == "" {
		return code
	}
	return fmt.Sprintf("%s (%s)", code, description)
}

// callbackTmpl renders the user-facing HTML after the browser hits
// /callback. Plain HTML + minimal CSS so it works offline and without
// any external resources.
var callbackTmpl = template.Must(template.New("cb").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
<style>
body { font-family: -apple-system, system-ui, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; margin: 0; background: #f5f5f5; }
.card { background: #fff; padding: 2em 3em; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); text-align: center; max-width: 480px; }
h1 { margin: 0 0 0.5em; font-size: 1.4em; }
p { margin: 0; color: #555; }
</style>
</head>
<body>
<div class="card">
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
</div>
</body>
</html>`))

// renderCallbackPage writes the small HTML document shown in the user's
// browser after a PKCE callback. It deliberately swallows template
// errors because by the time renderCallbackPage is called the
// flow's success/failure has already been decided — a render failure
// should not cascade into a second callbackCh write.
func renderCallbackPage(w http.ResponseWriter, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = callbackTmpl.Execute(w, struct{ Title, Message string }{Title: title, Message: message})
}
