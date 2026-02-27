---
phase: 08-fix-rpc-ftp-sftp-handlers
plan: 01
subsystem: daemon
tags: [json-rpc, ftp, sftp, handlers, tdd, websocket]

requires:
  - phase: 05-json-rpc
    provides: "RPC downloadAdd with handler wiring for HTTP downloads"
  - phase: 03-ftp-ftps
    provides: "FTP ProtocolDownloader implementation"
  - phase: 04-sftp
    provides: "SFTP ProtocolDownloader implementation"
provides:
  - "FTP/SFTP downloads via RPC download.add emit WebSocket push notifications"
  - "item.Downloaded updated during FTP/SFTP RPC downloads (not only at completion)"
  - "Mock ProtocolDownloader test helper for RPC handler testing"
affects: []

tech-stack:
  added: []
  patterns:
    - "mockProtocolDownloader pattern for testing ProtocolDownloader handler wiring"
    - "newTestRPCHandlerWithRouter helper for injecting custom SchemeRouter in tests"

key-files:
  created:
    - "internal/server/rpc_ftp_sftp_notify_test.go"
  modified:
    - "internal/server/rpc_methods.go"

key-decisions:
  - "Used SchemeRouter.Register to inject mock factories in tests (no real FTP/SFTP connections)"
  - "Mock Download calls DownloadCompleteHandler with MAIN_HASH (not mock hash) to match patchProtocolHandlers gate"

patterns-established:
  - "mockProtocolDownloader: reusable mock for testing ProtocolDownloader handler wiring"
  - "newTestRPCHandlerWithRouter: RPC test helper with custom SchemeRouter injection"

requirements-completed: [RPC-05, RPC-11]

duration: 2min
completed: 2026-02-27
---

# Phase 8 Plan 1: RPC FTP/SFTP Handler Wiring Fix Summary

**TDD fix wiring opts.Handlers in RPC downloadAdd FTP/SFTP branch: 2-line production fix, 3 tests proving handler callbacks fire and item.Downloaded updates**

## Performance

- **Duration:** 2 min
- **Started:** 2026-02-27T22:06:29Z
- **Completed:** 2026-02-27T22:08:54Z
- **Tasks:** 3
- **Files modified:** 2

## Accomplishments
- FTP/SFTP downloads started via JSON-RPC download.add now emit WebSocket push notifications (download.progress, download.complete)
- download.status reports non-zero completedLength for FTP/SFTP downloads in progress
- Tests prove handler callbacks are invoked during FTP/SFTP RPC download.add with atomic counters
- All tests pass with -race flag (no data races in goroutine-based pd.Download)
- Server package coverage increased from 86.0% to 89.7%

## Task Commits

Each task was committed atomically (TDD red-green-refactor):

1. **Task 1 (RED): Write failing tests** - `a42badc` (test)
2. **Task 2 (GREEN): Fix nil handler pass** - `c6873e9` (fix)
3. **Task 3 (GATE): Format + full suite verification** - `9ce43c3` (style)

## Files Created/Modified
- `internal/server/rpc_ftp_sftp_notify_test.go` - Mock ProtocolDownloader, test helper, and 3 test functions verifying handler wiring for FTP/SFTP RPC download.add
- `internal/server/rpc_methods.go` - Two nil->opts.Handlers fixes in downloadAdd default branch (lines 242, 259)

## Decisions Made
- Used SchemeRouter.Register to inject mock factories in tests rather than creating real FTP/SFTP connections -- faster, more reliable, no network dependency
- Mock Download calls DownloadCompleteHandler with warplib.MAIN_HASH (not mock.hash) because patchProtocolHandlers only finalizes item.Downloaded = item.TotalSize when hash == MAIN_HASH

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase complete, ready for transition
- All requirements (RPC-05, RPC-11) verified

## Self-Check: PASSED
- `internal/server/rpc_ftp_sftp_notify_test.go` exists on disk
- `internal/server/rpc_methods.go` exists on disk
- `git log --oneline --all --grep="08-01"` returns 3 commits
- Server package coverage: 89.7% (>= 80% threshold)
- Full test suite: zero failures with -race -short

---
*Phase: 08-fix-rpc-ftp-sftp-handlers*
*Completed: 2026-02-27*
