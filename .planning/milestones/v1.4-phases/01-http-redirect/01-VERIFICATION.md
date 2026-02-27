---
phase: 01-http-redirect
verified: 2026-02-27
result: PASS
requirements-verified: [REDIR-01, REDIR-02, REDIR-03, REDIR-04]
---

# Phase 1: HTTP Redirect -- Verification

## Success Criteria

### SC1 (REDIR-01): User can download files behind HTTP 301/302/303/307/308 redirects transparently

**Result: PASS**

Evidence:
- `pkg/warplib/redirect.go` -- RedirectPolicy function handles all 3xx status codes
- `pkg/warplib/dloader.go` -- fetchInfo() follows redirects and captures final URL
- `pkg/warplib/redirect_test.go` -- 20+ test cases covering all redirect status codes (301, 302, 303, 307, 308)
- `NewDownloader` sets `CheckRedirect = RedirectPolicy(DefaultMaxRedirects)` automatically

### SC2 (REDIR-02): All segment requests use final URL after redirect chain resolves

**Result: PASS**

Evidence:
- `dloader.go` fetchInfo() sets `d.url = resp.Request.URL.String()` after GET request follows redirects
- All subsequent segment downloads use the updated `d.url` (final URL, not original)
- Integration tests verify `d.url` is updated after redirect resolution

### SC3 (REDIR-03): Redirect chain limited to max hops with clear error on loop

**Result: PASS**

Evidence:
- `redirect.go` -- `RedirectPolicy(DefaultMaxRedirects)` enforces 10-hop limit
- `ErrTooManyRedirects` sentinel error returned when limit exceeded
- `redirect_test.go` tests loop detection and max hops enforcement

### SC4 (REDIR-04): Authorization headers not leaked across cross-origin redirects

**Result: PASS**

Evidence:
- `redirect.go` -- `StripUnsafeFromHeaders()` filters unsafe headers, keeping only safe headers (User-Agent, Accept, Accept-Language, Accept-Encoding, Range)
- `dloader.go` fetchInfo() detects cross-origin redirect (host comparison) and strips `d.headers` via StripUnsafeFromHeaders
- `proxy.go` -- all 3 NewHTTPClient* functions set CheckRedirect
- Phase 6 added defense-in-depth: `web.go` processDownload explicitly sets CheckRedirect
- 6 tests: CVE-2024-45336 regression (3 subtests), proxy client CheckRedirect (3 tests)

## Requirements Verification

| Requirement | Status | Evidence |
|-------------|--------|----------|
| REDIR-01 | PASS | RedirectPolicy handles all 3xx codes; 20+ test cases |
| REDIR-02 | PASS | fetchInfo() captures final URL; segments use updated d.url |
| REDIR-03 | PASS | 10-hop limit enforced; ErrTooManyRedirects on loop |
| REDIR-04 | PASS | StripUnsafeFromHeaders strips Auth/cookies on cross-origin; all HTTP clients set CheckRedirect; defense-in-depth in web.go |

## Gate Results

- All tests pass: YES (`go test ./...` -- 19 packages, 0 failures)
- Race detection: CLEAN (`go test -race -short ./...`)
- Build: CLEAN (`go build -ldflags="-w -s" .`)
- Vet: CLEAN (`go vet ./...`)

## Files Modified

### Plan 01-01 (Redirect following + final URL capture)
- `pkg/warplib/redirect.go` -- RedirectPolicy, ErrTooManyRedirects, isCrossOrigin, stripUnsafeHeaders
- `pkg/warplib/redirect_test.go` -- 20+ test cases
- `pkg/warplib/dloader.go` -- fetchInfo() captures final URL, sets CheckRedirect

### Plan 01-02 (Cross-origin header stripping)
- `pkg/warplib/redirect.go` -- StripUnsafeFromHeaders for Headers slice filtering
- `pkg/warplib/redirect_test.go` -- CVE regression tests, proxy client CheckRedirect tests
- `pkg/warplib/dloader.go` -- Cross-origin detection and header stripping in fetchInfo()
- `pkg/warplib/proxy.go` -- All NewHTTPClient* functions set CheckRedirect
- `cmd/daemon_core.go` -- Daemon HTTP client sets CheckRedirect
- `cmd/info.go` -- Non-proxy client path sets CheckRedirect

---
*Verified: 2026-02-27*
