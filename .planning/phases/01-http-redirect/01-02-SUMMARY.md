---
phase: 01-http-redirect
plan: 02
status: complete
started: 2026-02-27
completed: 2026-02-27
requirements-completed: [REDIR-04]
---

## Summary

Implemented cross-origin credential protection and hardened all HTTP client creation sites with redirect policy. The key finding was that `fetchInfo()` captured the final URL after redirect but continued applying original-origin credentials (`d.headers`) on all subsequent requests to the new origin — a real security vulnerability beyond what Go's `CheckRedirect` alone can protect against.

## RED Phase
Wrote failing tests proving:
- Authorization header leaked to cross-origin server after redirect (CVE-2024-45336 regression vector via `d.headers` reapplication)
- Custom headers (X-Custom-Token) leaked to cross-origin server after redirect
- `NewHTTPClientWithProxy`, `NewHTTPClientFromEnvironment`, `NewHTTPClientWithProxyAndTimeout` all had nil `CheckRedirect`

Root cause identified: `fetchInfo()` updates `d.url` to the final (cross-origin) URL, but `d.headers` still contains the original-origin credentials. `prepareDownloader()` and all segment downloads then send those credentials directly to the new origin via `makeRequest()`, bypassing `CheckRedirect` entirely (no redirect occurs on direct requests).

## GREEN Phase
Three changes made all tests pass:
1. Added `StripUnsafeFromHeaders()` to `redirect.go` — filters a `Headers` slice keeping only safe headers (User-Agent, Accept, Accept-Language, Accept-Encoding, Range)
2. Updated `fetchInfo()` in `dloader.go` to detect cross-origin redirect (compare original URL host with final URL host) and strip unsafe headers from `d.headers` using `StripUnsafeFromHeaders()`
3. Set `CheckRedirect = RedirectPolicy(DefaultMaxRedirects)` on all proxy client creation functions and daemon/info client creation sites

## REFACTOR Phase
No refactoring needed — code was clean from GREEN phase.

## Commits
1. `test(01-02): add failing tests for cross-origin header stripping and CVE regression` (64a3e5c)
2. `core: feat: cross-origin header stripping on redirect and client hardening` (e194548)

## Key Files

### Modified
- `pkg/warplib/redirect.go` — Added `StripUnsafeFromHeaders()` for filtering `Headers` slices
- `pkg/warplib/redirect_test.go` — Added 6 tests: CVE-2024-45336 regression (3 subtests), proxy client CheckRedirect (3 tests)
- `pkg/warplib/dloader.go` — `fetchInfo()` now strips `d.headers` on cross-origin redirect; added `net/url` import
- `pkg/warplib/proxy.go` — All 3 `NewHTTPClient*` functions set `CheckRedirect`
- `cmd/daemon_core.go` — Daemon HTTP client sets `CheckRedirect`
- `cmd/info.go` — Non-proxy client path sets `CheckRedirect`

## Self-Check: PASSED
- All 6 new tests pass (were failing in RED phase)
- Full test suite passes with race detection (zero regression)
- go vet clean
- Coverage: 87.1% (above 80% threshold)
- Requirement REDIR-04 addressed
