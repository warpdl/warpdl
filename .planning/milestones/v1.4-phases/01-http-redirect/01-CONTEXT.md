# Phase 1: HTTP Redirect - Context

**Gathered:** 2026-02-26
**Status:** Ready for planning

<domain>
## Phase Boundary

WarpDL transparently follows HTTP redirect chains (301/302/303/307/308) so users can download files from CDNs, URL shorteners, and servers that redirect before serving content. This is issue #144. The fix is in the existing HTTP downloader — no new protocols, no new dependencies.

</domain>

<decisions>
## Implementation Decisions

### Redirect behavior
- Go's `net/http.Client` already follows redirects by default (up to 10 hops). The current code likely disables or restricts this — the fix is ensuring the client config allows redirect following.
- Default max hops: 10 (Go stdlib default). No need for a user-configurable flag in v1 — hardcode the default.
- After redirect chain resolves, `d.url` must be updated to `resp.Request.URL.String()` so all subsequent parallel segment requests (Range headers) hit the final URL, not the original redirect origin.
- The final resolved URL should be logged/displayed to the user so they know where the file actually came from.

### Header security on cross-origin redirect
- Authorization header must NOT be forwarded when redirect crosses to a different origin (different host). This prevents credential leakage.
- Go 1.24+ already handles CVE-2024-45336 (Authorization header leak on redirect), but we need a regression test that explicitly validates this behavior.
- Cookies: follow Go's default cookie jar behavior — cookies are domain-scoped and won't leak cross-origin by default.
- Custom headers set by the user (e.g., `--header` flag) should also be stripped on cross-origin redirect, except standard ones like User-Agent and Accept.

### Error messaging
- Redirect loop (exceeds max hops): Clear error message like `"redirect loop detected: exceeded 10 hops (last URL: <url>)"`. Include the last URL in the chain so the user can debug.
- Cross-protocol redirect (HTTP → FTP): Reject with error `"cross-protocol redirect not supported: <from_scheme> → <to_scheme>"`. Do not silently follow.
- Network error during redirect: Standard retry behavior applies (existing retry logic).

### Claude's Discretion
- Exact implementation of the `CheckRedirect` function
- Whether to log intermediate redirect hops in debug mode
- Test strategy details (mock server vs integration test)

</decisions>

<specifics>
## Specific Ideas

- Issue #144 specifically mentions the Fedora download URL (`download.fedoraproject.org`) as a failing case — this should be a test case (or at minimum documented as the motivating example).
- The fix should be as minimal as possible — this is a behavior fix, not a feature addition. Ideally 5-15 lines of actual logic change.
- Research confirmed: the existing `dloader.go` likely has a custom `CheckRedirect` or a transport that blocks redirects. The fix is in `prepareDownloader()` where the HTTP client is configured.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 01-http-redirect*
*Context gathered: 2026-02-26*
