# Phase 1: HTTP Redirect - Research

**Researched:** 2026-02-26
**Domain:** Go net/http redirect handling in parallel segment downloader
**Confidence:** HIGH

## Summary

WarpDL's HTTP client (`&http.Client{}`) already follows redirects by default (Go stdlib follows up to 10 hops). The actual bug is that `fetchInfo()` resolves the final URL after redirects but never updates `d.url` — so all parallel segment downloads re-request the original URL, triggering redundant redirect chains on every Range request. For CDNs with ephemeral/signed redirect targets, this means segments may hit a different final URL or fail entirely.

The fix is surgical: capture `resp.Request.URL.String()` after the initial `fetchInfo()` request and update `d.url` to the final URL. Then add a custom `CheckRedirect` function to enforce max hops with a clear error and strip sensitive headers on cross-origin redirects.

**Primary recommendation:** Update `d.url` after `fetchInfo()` resolves redirects; add `CheckRedirect` to the `http.Client` for max-hop enforcement and header security; all subsequent Range requests use the final URL.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- Go's `net/http.Client` already follows redirects by default (up to 10 hops). The current code likely disables or restricts this — the fix is ensuring the client config allows redirect following.
- Default max hops: 10 (Go stdlib default). No need for a user-configurable flag in v1 — hardcode the default.
- After redirect chain resolves, `d.url` must be updated to `resp.Request.URL.String()` so all subsequent parallel segment requests (Range headers) hit the final URL, not the original redirect origin.
- The final resolved URL should be logged/displayed to the user so they know where the file actually came from.
- Authorization header must NOT be forwarded when redirect crosses to a different origin (different host). This prevents credential leakage.
- Go 1.24+ already handles CVE-2024-45336 (Authorization header leak on redirect), but we need a regression test that explicitly validates this behavior.
- Cookies: follow Go's default cookie jar behavior — cookies are domain-scoped and won't leak cross-origin by default.
- Custom headers set by the user (e.g., `--header` flag) should also be stripped on cross-origin redirect, except standard ones like User-Agent and Accept.
- Redirect loop (exceeds max hops): Clear error message like `"redirect loop detected: exceeded 10 hops (last URL: <url>)"`. Include the last URL in the chain so the user can debug.
- Cross-protocol redirect (HTTP -> FTP): Reject with error `"cross-protocol redirect not supported: <from_scheme> -> <to_scheme>"`. Do not silently follow.
- Network error during redirect: Standard retry behavior applies (existing retry logic).

### Claude's Discretion
- Exact implementation of the `CheckRedirect` function
- Whether to log intermediate redirect hops in debug mode
- Test strategy details (mock server vs integration test)

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| REDIR-01 | User can download files behind HTTP 301/302/303/307/308 redirects transparently | Go stdlib already handles this; fix is updating `d.url` to final URL so segments work |
| REDIR-02 | Downloader tracks and uses final URL after redirect chain for all segment requests | `fetchInfo()` gets `resp.Request.URL` after redirects; update `d.url` there |
| REDIR-03 | Redirect chain limited to max hops (default 10) with clear error on loop | Custom `CheckRedirect` function counting hops, returning `ErrTooManyRedirects` |
| REDIR-04 | Authorization headers not leaked across cross-origin redirects | Go 1.24+ handles CVE-2024-45336 by default; add regression test + strip custom headers |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `net/http` | Go 1.24.9+ stdlib | HTTP client with redirect following | Already in use; built-in redirect support via `CheckRedirect` |

### Supporting
No new dependencies needed. This is a behavior fix within existing code.

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom `CheckRedirect` | nil (Go default) | Default handles 10 hops but gives opaque error; custom gives clear "redirect loop" message with last URL |
| Manual redirect following | `http.Client` auto-follow | Manual is unnecessary complexity; Go stdlib handles all redirect codes correctly |

## Architecture Patterns

### Recommended Change: fetchInfo() URL Update

The fix is in `dloader.go:fetchInfo()`. After the initial GET request resolves through redirects:

```go
func (d *Downloader) fetchInfo() (err error) {
    resp, er := d.makeRequest(http.MethodGet)
    if er != nil {
        err = er
        return
    }
    defer resp.Body.Close()

    // REDIR-02: Update URL to final resolved URL after redirect chain
    finalURL := resp.Request.URL.String()
    if finalURL != d.url {
        // Log redirect for user visibility
        d.url = finalURL
    }

    // ... rest of fetchInfo unchanged
}
```

### Pattern: Custom CheckRedirect for Max Hops + Cross-Origin Header Stripping

```go
const DefaultMaxRedirects = 10

// ErrTooManyRedirects is returned when a redirect chain exceeds the max hops.
var ErrTooManyRedirects = errors.New("redirect loop detected")

// redirectPolicy returns a CheckRedirect function that enforces max hops
// and strips sensitive headers on cross-origin redirects.
func redirectPolicy(maxRedirects int) func(*http.Request, []*http.Request) error {
    return func(req *http.Request, via []*http.Request) error {
        if len(via) >= maxRedirects {
            lastURL := via[len(via)-1].URL.String()
            return fmt.Errorf("%w: exceeded %d hops (last URL: %s)",
                ErrTooManyRedirects, maxRedirects, lastURL)
        }

        // Cross-origin check: strip Authorization if host changed
        if len(via) > 0 {
            oldHost := via[len(via)-1].URL.Host
            newHost := req.URL.Host
            if oldHost != newHost {
                req.Header.Del("Authorization")
                // Strip custom headers, keep standard ones
            }
        }

        // Cross-protocol check
        if len(via) > 0 {
            oldScheme := via[len(via)-1].URL.Scheme
            newScheme := req.URL.Scheme
            if (oldScheme == "http" || oldScheme == "https") &&
               (newScheme != "http" && newScheme != "https") {
                return fmt.Errorf("cross-protocol redirect not supported: %s -> %s",
                    oldScheme, newScheme)
            }
        }

        return nil
    }
}
```

### Where the HTTP Client is Created

Critical locations where `http.Client` is instantiated:

1. **`cmd/daemon_core.go:109`** — `client := &http.Client{Jar: jar}` — daemon mode, used for all downloads
2. **`cmd/info.go:51`** — `httpClient = &http.Client{}` — info command (no jar)
3. **`cmd/info.go:46`** — proxy client via `warplib.NewHTTPClientWithProxy(proxyURL)` — info with proxy
4. **`pkg/warplib/proxy.go:85-152`** — `NewHTTPClientWithProxy`, `NewHTTPClientFromEnvironment`, `NewHTTPClientWithProxyAndTimeout`

All of these need the `CheckRedirect` function set. The cleanest approach: add a helper in warplib that wraps client creation with the redirect policy, or set it on the client in `NewDownloader`.

### Anti-Patterns to Avoid
- **Copying all headers in CheckRedirect:** Go 1.24+ fixed CVE-2024-45336 by NOT copying Authorization on cross-origin redirect. A custom CheckRedirect that manually copies headers would REINTRODUCE this vulnerability. The CONTEXT.md explicitly notes this.
- **Configurable max hops in v1:** Over-engineering. Hardcode 10.
- **Following cross-protocol redirects:** HTTP->FTP is a security risk and violates RFC. Reject.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Redirect following | Manual redirect loop | `http.Client` with `CheckRedirect` | Go stdlib handles all 3xx codes, cookies, method changes |
| Header security | Custom header copying | Go 1.24+ default behavior | CVE-2024-45336 is already fixed in Go 1.24+ |
| URL parsing | Manual scheme/host extraction | `net/url.URL` fields | Stdlib URL parsing handles edge cases |

## Common Pitfalls

### Pitfall 1: Not Updating d.url After Redirect
**What goes wrong:** All parallel segment Range requests hit the original URL, triggering N redirect chains. CDNs with signed/ephemeral redirect targets may return different final URLs per request, causing segments to download from different files.
**Why it happens:** `fetchInfo()` resolves the redirect but stores the original URL in `d.url`.
**How to avoid:** Capture `resp.Request.URL.String()` in `fetchInfo()` and update `d.url`.
**Warning signs:** Downloads from CDNs (Fedora mirrors, GitHub releases, SourceForge) fail or produce corrupted files.

### Pitfall 2: Reintroducing CVE-2024-45336 via Custom CheckRedirect
**What goes wrong:** A `CheckRedirect` that copies all request headers from the previous request reintroduces the Authorization header leak on cross-origin redirects.
**Why it happens:** Go's old default (pre-1.24) copied Authorization on redirect. A "helpful" custom function that does the same thing is a security regression.
**How to avoid:** Let Go 1.24+'s default handle Authorization stripping. Custom `CheckRedirect` only needs to: (1) count hops, (2) strip custom user headers on cross-origin, (3) reject cross-protocol.
**Warning signs:** Authorization header appearing in requests to different hosts in test captures.

### Pitfall 3: prepareDownloader() Also Makes a Request
**What goes wrong:** `prepareDownloader()` (called at the end of `fetchInfo()`) makes a second GET request with a Range header. If `d.url` wasn't updated before this call, this request also hits the original URL and triggers another redirect chain.
**Why it happens:** `prepareDownloader()` uses `d.makeRequest()` which uses `d.url`.
**How to avoid:** Update `d.url` BEFORE `prepareDownloader()` is called, i.e., right after the first response in `fetchInfo()`.

### Pitfall 4: Testing Redirect with Only One Status Code
**What goes wrong:** Tests only check 302 and miss that 307/308 preserve the request method while 301/302/303 may change POST to GET.
**Why it happens:** 302 is the "common" redirect code, so tests only cover it.
**How to avoid:** Test all five codes (301, 302, 303, 307, 308). For a download manager using GET, the distinction doesn't matter much, but the regression test should confirm all work.

## Code Examples

### Current fetchInfo() — Where the Fix Goes

```go
// dloader.go:1211-1255
func (d *Downloader) fetchInfo() (err error) {
    resp, er := d.makeRequest(http.MethodGet)
    if er != nil {
        err = er
        return
    }
    defer resp.Body.Close()

    // FIX: Update URL to final resolved URL after any redirects
    if finalURL := resp.Request.URL.String(); finalURL != d.url {
        d.url = finalURL
    }

    h := resp.Header
    // ... existing code continues
}
```

### Test Server for Redirect Testing (httptest)

```go
func TestRedirectChain(t *testing.T) {
    // Final server serving the actual file
    finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Length", "5")
        w.Write([]byte("hello"))
    }))
    defer finalSrv.Close()

    // Redirect server
    redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Redirect(w, r, finalSrv.URL+"/file.bin", http.StatusFound)
    }))
    defer redirectSrv.Close()

    d, err := NewDownloader(&http.Client{}, redirectSrv.URL+"/download", opts)
    // d.url should now be finalSrv.URL+"/file.bin"
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Go copied Authorization on cross-origin redirect | Go strips Authorization on cross-origin redirect | Go 1.24 (CVE-2024-45336 fix) | No custom header copying needed |
| No `CheckRedirect` check for cross-protocol | Still no default protection | Current | Must add custom check for HTTP->non-HTTP |

## Open Questions

1. **Should `initDownloader` (resume path) also update URL?**
   - What we know: `initDownloader` takes the URL from persisted state, doesn't make a fresh request
   - What's unclear: If a persisted download's URL was the original (pre-redirect), resuming it will re-trigger the redirect chain
   - Recommendation: Resume should work fine since the redirect will resolve again. The URL update in `fetchInfo()` only matters for the initial parallel segments. For resume, the stored URL is the original, and each part will follow the redirect. This is acceptable for v1.

2. **Where exactly to set CheckRedirect?**
   - Option A: On every `http.Client` creation site (daemon_core.go, info.go, proxy.go)
   - Option B: Inside `NewDownloader` — set `client.CheckRedirect` before use
   - Recommendation: Option B is cleaner — single point of configuration. But modifying a passed-in client is a side effect. Better: create a wrapper function in warplib that configures the redirect policy and use it at all client creation sites.

## Sources

### Primary (HIGH confidence)
- Codebase analysis: `pkg/warplib/dloader.go` lines 1211-1340 (fetchInfo, prepareDownloader, makeRequest)
- Codebase analysis: `pkg/warplib/parts.go` lines 146-192 (Part.download uses p.url for Range requests)
- Codebase analysis: `cmd/daemon_core.go` line 109 (HTTP client creation with cookie jar)
- Go stdlib documentation: `net/http.Client.CheckRedirect` behavior

### Secondary (MEDIUM confidence)
- CVE-2024-45336 fix in Go 1.24: Authorization header no longer forwarded on cross-origin redirect by default

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - pure Go stdlib, no external dependencies
- Architecture: HIGH - direct codebase analysis, fix location identified precisely
- Pitfalls: HIGH - CVE-2024-45336 behavior verified in Go release notes

**Research date:** 2026-02-26
**Valid until:** 2027-02-26 (Go stdlib redirect behavior is stable)
