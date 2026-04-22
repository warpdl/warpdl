# warpdl Plugin OAuth — Design Spec

**Date:** 2026-04-22
**Status:** Approved — ready for implementation plan
**Scope:** `pkg/credman`, `internal/extl`, `internal/api`, `cmd/`, plus new types in `common/` and a new `plugins/gdrive` v2 build as the driving example

## 1. Goal

Let a warpdl plugin request an OAuth 2.0 access token for the user and attach the resulting credentials to the downloader, so private resources behind OAuth (Google Drive private files, Dropbox, OneDrive, GitHub API, Microsoft Graph, …) become downloadable via `warp download <url>` with a first-run browser prompt.

## 2. Non-goals

- Generic credential types beyond OAuth 2.0 in this iteration (API keys, cookies, HTTP basic, personal access tokens) — architected to extend, not shipped.
- OAuth 2.0 client-secret / confidential-client flows — plugin manifest rejects `client_secret`.
- Token sharing across machines or encrypted sync.
- OIDC claim validation — `id_token` is stored verbatim but not parsed.
- Per-plugin encryption keys for tokens.

## 3. Decisions (from brainstorm)

| Question | Decision |
|---|---|
| OAuth flow type | Authorization Code + PKCE baseline; device-code opt-in via manifest. |
| What the plugin gets | Both: `getAccessToken()` returns the bearer; `fetchWithAuth()` convenience; plugin can return `{url, headers}` from `extract()`. |
| UX trigger | Both: interactive when driven from the CLI (`AUTH_REQUIRED` push); explicit `warp auth login` for headless / native-host / scripted use. |
| Multi-account | Yes, from day one. Storage keyed by `(plugin_id, account_label)`. |
| Approach | OAuth-specific subsystem, with Go-side `AuthProvider` interface so future credential types plug in without a rewrite. |

## 4. Architecture

Five new units:

1. **`AuthProvider` interface** (Go, inside daemon). One concrete impl: `OAuth2Provider`. One instance per loaded plugin that declared an `auth` block. Exposes `Token(ctx, key, scopes) (string, error)`, `Refresh`, `Invalidate`, `Logout`, and flow-driver methods for `login.Start` / `login.Complete`.
2. **`TokenManager`** (Go, in `pkg/credman`). Sibling of `CookieManager`: encrypted persistent store for `OAuth2Token` keyed by `(PluginID, Account)`. Same AES-GCM + OS-keyring model, new file `tokens.gob`.
3. **`FlowRegistry`** (Go, inside `OAuth2Provider`). In-memory map of `FlowID → *flow`. Dedups concurrent triggers for the same `(plugin_id, account)`, expires entries after 5 min, and brokers the rendezvous between the engine-side `Token()` call that blocks on a flow and the RPC handler that resolves it.
4. **Auth orchestrator** (Go, in `cmd/`). Drives the browser loopback, or renders device-code prompts. Used by both explicit `warp auth login` and the interactive in-download trigger.
5. **New RPC surface** (Go, in `internal/api` + `common/`). Request methods `auth.login`, `auth.complete`, `auth.cancel`, `auth.list`, `auth.logout`; server-pushed update types `UPDATE_AUTH_REQUIRED`, `UPDATE_AUTH_COMPLETED`, `UPDATE_AUTH_FAILED`, `UPDATE_AUTH_LOGGED_OUT`. (`warp auth status` is CLI-only sugar over `auth.list` — it does not add a new RPC.)

**Component boundary:** daemon owns tokens and OAuth crypto (client_id, PKCE verifier, refresh tokens); CLI owns user interaction (browser, stdin prompts, terminal output). Refresh tokens never leave the daemon. Access tokens cross the JS boundary into plugin memory (intentional, per Q2-C).

### Data flow — private Drive download

```
user: warp download https://drive.google.com/file/d/PRIVATE/view
  CLI  --RPC-->  daemon: Download{url}
  daemon: engine.Extract(url)
    plugin.extract(url)
      plugin: getAccessToken("default", ["drive.readonly"])  [BLOCKS]
        engine: OAuth2Provider.Token(plugin_id, "default", scopes)
          TokenManager.Get -> miss
          FlowRegistry.StartPKCE(plugin_id, "default") -> flow_id, authorize_url
  daemon  --UPDATE_AUTH_REQUIRED-->  CLI
    CLI: auth orchestrator
      binds 127.0.0.1:<ephemeral>, opens browser to authorize_url
      /callback?code=&state= -> verify state
  CLI  --auth.complete{flow_id, code, state}-->  daemon
    OAuth2Provider.exchange(code, verifier) -> tokens
    TokenManager.Set(plugin_id, "default", tokens)
    FlowRegistry.Resolve(flow_id) -> [UNBLOCKS getAccessToken]
  daemon  --UPDATE_AUTH_COMPLETED-->  CLI
  plugin: returns {url: "https://www.googleapis.com/drive/v3/files/.../alt=media",
                   headers: {"Authorization": "Bearer ..."}}
  engine.Extract returns {URL, Headers}
  daemon -> downloader: GET url with bearer header
  download bytes -> disk; progress updates stream to CLI
```

Subsequent downloads for the same plugin+account skip the CLI round-trip: `TokenManager.Get` hits, (or refresh happens silently), plugin gets a token, download proceeds.

## 5. Manifest schema

Plugins that need auth add an `auth` block to `manifest.json`:

```jsonc
{
  "name": "Google Drive",
  "version": "2.0.0",
  "matches": ["^https?://(drive|docs)\\.google\\.com/.+"],
  "entrypoint": "main.js",
  "auth": {
    "type": "oauth2",
    "client_id": "123456-abcdef.apps.googleusercontent.com",
    "scopes": ["https://www.googleapis.com/auth/drive.readonly"],
    "authorize_url": "https://accounts.google.com/o/oauth2/v2/auth",
    "token_url":     "https://oauth2.googleapis.com/token",
    "device_url":    "https://oauth2.googleapis.com/device/code",   // optional
    "revoke_url":    "https://oauth2.googleapis.com/revoke",        // optional
    "pkce": "S256",                                                 // "S256" | "plain"; default "S256"
    "extra_auth_params": { "access_type": "offline", "prompt": "consent" }
  }
}
```

**Validator rules** (enforced at `warp ext install` time; runtime never sees an invalid manifest):

- `type == "oauth2"` required (future types fail v1 install).
- `client_id` non-empty.
- `scopes` non-empty array of strings.
- `authorize_url`, `token_url` required, both must parse as `https://` URLs.
- `device_url`, `revoke_url` optional; if present must also be `https://`.
- `pkce ∈ {S256, plain}`; default `S256` when absent.
- `extra_auth_params` is `map[string]string`, optional.
- **Forbidden**: `client_secret`. Installer hard-fails with a link to the "why no client secrets" section of docs.

Plugins without `auth` load unchanged. A plugin ships an auth-less v1 for public URLs and adds auth in v2 — full backward compatibility.

## 6. JS binding surface

All synchronous from the plugin's view. Engine blocks the JS call while handling any async work (token refresh, CLI round-trip).

### `getAccessToken(opts?) → string`

```js
const token = getAccessToken();                              // default account, all manifest scopes
const token = getAccessToken({ account: "work" });
const token = getAccessToken({ scopes: ["drive.readonly"] }); // subset of manifest scopes
```

Engine behaviour:

1. Look up stored token for `(plugin_id, account)`.
2. Stored scopes must be a superset of requested scopes. If not, drop to step 4.
3. `ExpiresAt − now > 60s`: return access token.
4. Otherwise attempt refresh under a per-key mutex; success → return new token.
5. Miss / refresh failure → trigger login flow (interactive or `auth_required` error).
6. Throws a JS error on user cancel, 5-min timeout, or unrecoverable failure.

### `fetchWithAuth(req, opts?) → Response`

Same shape as `request()` but engine injects `Authorization: Bearer <token>` and handles one auto-retry on 401 (refresh, retry; on second 401 → error).

```js
const resp = fetchWithAuth({
  method: "GET",
  url: "https://www.googleapis.com/drive/v3/files/" + id,
  headers: { "Accept": "application/json" },
}, { account: "work", scopes: ["drive.readonly"] });
```

### `invalidateToken(opts?)`

Drop the cached access token for `(plugin, account)`; keep the refresh token. Next `getAccessToken()` refreshes or re-auths.

### `listAccounts() → string[]`

Returns the account labels the user has logged in under **for this plugin** — the engine filters `TokenManager.List()` to the calling plugin's `ModuleId` before returning. A plugin cannot enumerate accounts for other plugins.

### Deliberately absent

- No `login()` / `logout()` from JS. Login and logout are user-driven through the CLI; plugins cannot initiate them.
- No `setToken()`. Plugins cannot plant tokens.
- No refresh-token accessor.

## 7. Extract contract extension

`extract(url)` may now return either:

```js
return "https://some.direct.url/file.zip";        // today's contract, unchanged
// OR:
return {
  url: "https://www.googleapis.com/.../alt=media",
  headers: { "Authorization": "Bearer " + getAccessToken() }
};
```

Engine side (`extl/module.go`):

```go
type ExtractResult struct {
    URL     string
    Headers map[string]string
}
```

`Module.Extract` inspects the returned JS value: string → `ExtractResult{URL: s}`; map → validate `url` (required, string) + `headers` (optional, `map[string]string`). Everything else → `ErrInvalidReturnType`.

**Header forwarding policy** (crucial for `Authorization`): plugin-supplied headers are attached to the downloader; they are forwarded on same-origin redirects and **stripped on cross-origin redirects**. Reuse the existing `StripUnsafeFromHeaders` path in `pkg/warplib/dloader.go` with the plugin-supplied header names added to its sensitive set.

Out of scope for the return object in v1: `method`, `body`, `cookies`, `expires_at`, alternate download protocols. All future additions.

## 8. RPC protocol changes

### Request methods (CLI → daemon)

```go
// auth.login
type AuthLoginParams struct {
    PluginID    string   // extension ModuleId
    Account     string   // "" → "default"
    Scopes      []string // empty → all manifest scopes
    Flow        string   // "pkce" | "device"; default "pkce"
    RedirectURI string   // loopback URL CLI is ready to receive on (pkce only)
}
type AuthLoginResult struct {
    FlowID          string
    AuthorizeURL    string   // populated for pkce
    DeviceCode      string   // populated for device
    UserCode        string
    VerificationURL string
    Interval        int      // poll seconds, device flow
    ExpiresAt       int64    // unix seconds
}

// auth.complete (pkce only; device flow self-polls on the daemon)
type AuthCompleteParams struct { FlowID, Code, State string }
type AuthCompleteResult struct {
    Account   string
    Scopes    []string
    ExpiresAt int64
}

// auth.cancel
type AuthCancelParams struct { FlowID string }

// auth.list
type AuthListResult struct { Accounts []AuthAccount }
type AuthAccount struct {
    PluginID  string
    Account   string
    Scopes    []string
    ExpiresAt int64
}

// auth.logout
type AuthLogoutParams struct { PluginID, Account string }
```

`warp auth status` is CLI-side filtering over the `auth.list` response; exit code 0 when the matching row exists and hasn't expired, nonzero otherwise. No new RPC method.

### Server-pushed update types

```go
UPDATE_AUTH_REQUIRED   // { FlowID, PluginID, Account, Scopes, authorize/device fields }
UPDATE_AUTH_COMPLETED  // { FlowID, Account, Scopes, ExpiresAt }
UPDATE_AUTH_FAILED     // { FlowID, Reason }  // "timeout" | "cancelled" | "provider_error" | …
UPDATE_AUTH_LOGGED_OUT // { PluginID, Account }
```

### Concurrency & lifecycle

- `FlowID` is a 128-bit random. Daemon-generated.
- At most one in-flight flow per `(plugin_id, account)`. Second trigger joins the first.
- Default flow timeout: 300 seconds. `UPDATE_AUTH_FAILED{Reason: "timeout"}` on expiry.
- User Ctrl-C or daemon shutdown → clean cancel, in-flight state discarded (not persisted).

### What never crosses the wire

- `client_id` (CLI doesn't need it; daemon has the manifest).
- PKCE `code_verifier`.
- `access_token`, `refresh_token`.
- `client_secret` (unsupported).

## 9. Token storage & lifecycle

### Types

```go
// pkg/credman/types/oauth.go
type OAuth2Token struct {
    AccessToken  string
    RefreshToken string    // may be empty
    TokenType    string    // "Bearer"
    ExpiresAt    time.Time
    Scopes       []string
    IDToken      string    // OIDC, optional
    IssuedAt     time.Time
}

type TokenKey struct {
    PluginID string
    Account  string
}
```

### Manager

```go
// pkg/credman/tokenmgr.go
type TokenManager struct { /* mirrors CookieManager */ }

func (tm *TokenManager) Get(k TokenKey) (*OAuth2Token, error)
func (tm *TokenManager) Set(k TokenKey, t *OAuth2Token) error
func (tm *TokenManager) Delete(k TokenKey) error
func (tm *TokenManager) List() []TokenKey
```

- File: `<config>/tokens.gob`.
- Encryption: AES-GCM, per-entry random nonce, same master key the existing `CookieManager` uses (one keyring entry shared between the two managers).
- GOB format identical to the cookie store pattern.

### Provider logic

`OAuth2Provider.Token(ctx, key, scopes)` decision tree:

1. `TokenManager.Get` → miss: acquire or join a login flow; block; return new token.
2. Scope subset check. Mismatch: trigger login flow.
3. `ExpiresAt − now > 60s`: return `AccessToken`.
4. Take `refreshLocks[key]` mutex. Re-read token (may have been refreshed by another goroutine). Still expired?
5. If `RefreshToken != ""`: POST to `token_url`, `grant_type=refresh_token`. Merge the response into the stored bundle (refresh-token rotation — new `refresh_token` field, if any, replaces old). Return new access token.
6. Else: delete the bundle, trigger login flow.

Skew: 60 seconds (matches `golang.org/x/oauth2`).

### Logout

1. If `revoke_url` in manifest → POST `{token: refresh_token ?: access_token}`; ignore errors.
2. `TokenManager.Delete(key)`.
3. Emit `UPDATE_AUTH_LOGGED_OUT`.

### Not persisted (in-memory only)

PKCE `code_verifier`, authorization `state`, in-flight `FlowID`, device-flow `device_code`.

## 10. CLI commands + auth orchestrator

### Subcommands

```
warp auth login <plugin> [--as <label>] [--scopes ...] [--device] [--no-browser]
warp auth list
warp auth logout <plugin> [--as <label>]
warp auth status <plugin> [--as <label>]
```

- **login**: starts a flow. `--as <label>` (default "default"). `--scopes` narrows below manifest. `--device` forces device code. `--no-browser` prints URL instead of opening.
- **list**: table `PLUGIN  ACCOUNT  SCOPES  EXPIRES`.
- **logout**: RPC `auth.logout`.
- **status**: exit 0 iff authenticated and not expired.

### Orchestrator

- CLI binds `127.0.0.1:0` to get an ephemeral port, builds `http://127.0.0.1:<port>/callback`, passes as `AuthLoginParams.RedirectURI`.
- Callback server accepts only `/callback` (extracts `code`+`state`, calls `auth.complete`, renders a "you may close this window" page). Anything else → 404.
- Browser opening: `github.com/pkg/browser`. Fallback to printing URL if `$DISPLAY` unset, `$WARP_NO_BROWSER=1`, or browser open fails.
- Handles both explicit `warp auth login` and `UPDATE_AUTH_REQUIRED` events from an in-flight download through the same code path.
- SIGINT → `auth.cancel` + exit 130.
- Hard timeout: 5 min, matches daemon.

### Interactive flow UX

```
$ warp download https://drive.google.com/file/d/PRIVATE/view
authentication required for gdrive ⇢ opening browser...
✓ authenticated; resuming download
[progress bar]
```

### Device flow UX

```
$ warp auth login gdrive --device
Open: https://www.google.com/device
Enter code: WDJB-MJHT    (copied to clipboard)
Waiting for confirmation...
✓ logged in to gdrive as "default"
```

## 11. Error handling

Every error is one of:

| Code | Trigger | Message shown |
|---|---|---|
| `auth_required` | No token, no interactive channel. | "authentication required; run `warp auth login <plugin>`" |
| `auth_cancelled` | User Ctrl-C. | Silent; exit 130. |
| `auth_timeout` | 300s elapsed. | "authentication timed out after 5 minutes" |
| `provider_error` | IdP 4xx/5xx on `/token` or `/device/code`; body attached. | Provider error + status code. |
| `scope_denied` | Consent granted narrower scopes. | "granted only X, needed Y — re-run login and grant all" |
| `network_error` | Transient on refresh. | Refresh auto-retried 3× with backoff; login errors fail fast. |
| `config_error` | Manifest schema violation. | Caught at install; never at runtime. |
| `storage_error` | `tokens.gob` read/write fails. | "cannot access token store: <path>" |

All errors carry `PluginID` + `Account` for logging context.

## 12. Security threat model

### Malicious plugin
- Scope whitelist in manifest enforced by engine — plugin cannot ask for broader grants than the user saw at install.
- Plugin sees `access_token`; cannot see `refresh_token`, cannot plant tokens, cannot enumerate other plugins' entries.
- Trust baseline: same as installing a browser extension. `warp ext install` displays the declared scopes before completing the install.

### Network attacker
- All IdP URLs must be `https://` (validator-enforced).
- CSRF at callback: daemon-generated `state` required on `auth.complete`.
- Loopback callback: `127.0.0.1` only, random ephemeral port, 5-min window, `/callback`-only routing.

### Local attacker / disk snapshot
- Tokens encrypted at rest (AES-GCM, per-entry nonce, keyring-held master key).
- Loopback port bound only during active flow.

### Refresh token theft
- Rotated on every refresh when IdP supports it.
- `warp auth logout` calls `revoke_url` before local delete.

### Explicit non-goals in v1
- Per-plugin encryption keys.
- Client-secret flows.
- Cross-machine sharing.
- OS-level memory protection (swap disable, core dump suppression).

## 13. Testing strategy

### Unit (deterministic, no network)

- `TokenManager`: get/set/delete/list, persistence round-trip, encryption integrity, corruption handling, `-race`-clean concurrent access.
- `OAuth2Provider` vs a `httptest.Server` IdP stub: PKCE full round-trip (verifier used on `/token`); device polling (`authorization_pending`, `slow_down`); refresh happy path; refresh-token rotation; refresh failure → re-auth; skew; scope-superset match.
- `FlowRegistry`: dedup, timeout, cancel.
- Manifest validator: every required field, URL scheme, `pkce` value, forbidden `client_secret`.
- `extl.Module.Extract`: string return, object return, invalid returns.
- CLI orchestrator (`cmd/auth_test.go`): callback with correct `state` succeeds; mismatched `state` rejected; timeout; Ctrl-C.

### Integration

- Full private-Drive flow: daemon + stub IdP + stub Drive API; install test plugin; `warp download <url>`; assert bearer attached, file content correct.
- Interactive trigger: download pushes `AUTH_REQUIRED`, orchestrator drives stub IdP, token persists, download resumes.
- Device-code end-to-end: polling, slow-down handling.
- Logout: assert stub IdP's `revoke_url` was called.

### Race + fuzz

- `-race` on every concurrency path.
- `FuzzManifestAuthValidator` — random JSON; never panic, invalid → clear error.
- `FuzzCallbackHandler` — random HTTP to loopback; never panic, only valid `/callback` advances flow.

### Plugin migration: `plugins/gdrive` v2

- Ship alongside implementation as the driving example.
- Public URLs continue using today's v1 flow (no regression).
- Private URLs: probe public path first; on 401/403 fall back to API + `getAccessToken`; return `{url, headers}`.
- Test suite extends existing `gdrive_test.go` with mocked auth stub.

## 14. Out of scope / future work

- Non-OAuth credential types (API keys, HTTP basic, cookies injected from browser). The `AuthProvider` interface is shaped for these; adding one is a new implementation, not a redesign.
- Client-secret / confidential-client flows.
- Cross-machine token sync.
- Folder downloads in the Drive plugin (orthogonal — needs multi-URL extract).
- OIDC claim validation.
- Per-plugin encryption keys.
