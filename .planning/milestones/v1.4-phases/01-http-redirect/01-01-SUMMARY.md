---
phase: 01-http-redirect
plan: 01
status: complete
started: 2026-02-27
completed: 2026-02-27
requirements-completed: [REDIR-01, REDIR-02, REDIR-03]
---

## Summary

Implemented HTTP redirect following with final URL capture and max-hop enforcement for WarpDL. Created `redirect.go` with `RedirectPolicy()` function, updated `fetchInfo()` to capture the final URL after redirect chains resolve, and configured `NewDownloader` to set `CheckRedirect` automatically.

## RED Phase
Wrote failing integration tests proving:
- `d.url` was NOT updated after redirect resolution (all 3xx status codes)
- Redirect loop did NOT produce our custom `ErrTooManyRedirects` error
Unit tests for `RedirectPolicy`, `isCrossOrigin`, `isHTTPScheme`, and `stripUnsafeHeaders` passed immediately (testing the policy function itself, not the integration).

## GREEN Phase
Two changes made all tests pass:
1. Added `d.url = resp.Request.URL.String()` in `fetchInfo()` after the initial GET request
2. Added `client.CheckRedirect = RedirectPolicy(DefaultMaxRedirects)` in `NewDownloader` when not already set

## REFACTOR Phase
Applied `gofmt` formatting (Go uses tabs, not spaces).

## Commits
1. `test(01-01): add failing tests for HTTP redirect following and URL capture` (cb4cedf)
2. `core: feat: implement HTTP redirect following with final URL capture` (ac7cfdf)
3. `core: refactor: apply gofmt formatting to redirect files` (8ff6c56)

## Key Files

### Created
- `pkg/warplib/redirect.go` — RedirectPolicy, ErrTooManyRedirects, ErrCrossProtocolRedirect, isCrossOrigin, stripUnsafeHeaders
- `pkg/warplib/redirect_test.go` — 20+ test cases covering all requirements

### Modified
- `pkg/warplib/dloader.go` — fetchInfo() now captures final URL; NewDownloader sets CheckRedirect

## Self-Check: PASSED
- All redirect tests pass
- Full test suite passes (zero regression)
- go vet clean
- Requirements REDIR-01, REDIR-02, REDIR-03 addressed
