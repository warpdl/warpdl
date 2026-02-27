---
phase: 09-fix-rpc-ftp-sftp-resume-handlers
plan: 01
subsystem: core
tags: [ftp, sftp, resume, handlers, rpc, websocket, tdd]

requires:
  - phase: 08-rpc-ftp-sftp-handler-wiring
    provides: "FTP/SFTP download.add handler wiring, mockProtocolDownloader, SchemeRouter"
provides:
  - "FTP/SFTP downloads resumed via RPC download.resume emit handler callbacks"
  - "Item.resumeHandlers field for protocol resume path"
  - "setResumeHandlers method on Item for Manager to store patched handlers"
  - "Integration tests proving resume handler callbacks fire for FTP and SFTP"
affects: [core, daemon, api]

tech-stack:
  added: []
  patterns:
    - "Stored handler pattern: Manager stores patched handlers on Item for deferred use in Resume()"
    - "Safe doneCh close: select-default pattern prevents double-close panic in mock"

key-files:
  created:
    - "internal/server/rpc_ftp_sftp_resume_test.go"
  modified:
    - "pkg/warplib/item.go"
    - "pkg/warplib/manager.go"
    - "internal/server/rpc_ftp_sftp_notify_test.go"

key-decisions:
  - "Used unexported resumeHandlers field (GOB-safe) protected by existing dAllocMu"
  - "HTTP resume path intentionally left unchanged — resumeHandlers stays nil for HTTP items"
  - "Added m.SetSchemeRouter(router) to test helper for resume to dispatch through Manager"

patterns-established:
  - "Stored handler pattern: set handlers on Item before goroutine launch for happens-before guarantee"

requirements-completed: [RPC-06, RPC-11]

duration: 9min
completed: 2026-02-27
---

# Phase 9 Plan 01: RPC FTP/SFTP Resume Handler Pass-Through Fix Summary

**FTP/SFTP resume handler pass-through via stored resumeHandlers field on Item, wired through Manager.ResumeDownload**

## Performance

- **Duration:** 9 min
- **Started:** 2026-02-27T22:40:30Z
- **Completed:** 2026-02-27T22:49:04Z
- **Tasks:** 3 (RED, GREEN, GATE)
- **Files modified:** 4

## Accomplishments
- FTP/SFTP downloads resumed via RPC download.resume now emit handler callbacks (progress/complete/error)
- Item struct has unexported resumeHandlers field (GOB-safe, protected by dAllocMu)
- HTTP resume path unaffected (resumeHandlers stays nil, preserving patchHandlers-installed struct field handlers)
- Integration tests prove handler callbacks fire during FTP and SFTP resume (atomic counters on mock)
- All tests pass with -race flag (no data races)
- Server package coverage: 90.1%, warplib package coverage: 85.8%

## Task Commits

Each task was committed atomically:

1. **Task 1: Write failing tests for FTP/SFTP handler wiring in RPC downloadResume (RED)** - `7898f41` (test)
2. **Task 2: Add resumeHandlers field to Item and wire through manager (GREEN)** - `6ee1570` (feat)

_TDD plan: RED produced failing tests, GREEN fixed them. No refactor needed._

## Files Created/Modified
- `internal/server/rpc_ftp_sftp_resume_test.go` - Integration tests for FTP/SFTP resume handler callbacks (2 test functions)
- `pkg/warplib/item.go` - Added unexported resumeHandlers field, setResumeHandlers method, updated Resume() to pass stored handlers
- `pkg/warplib/manager.go` - Added item.setResumeHandlers(opts.Handlers) in ResumeDownload FTP/SFTP branch
- `internal/server/rpc_ftp_sftp_notify_test.go` - Updated mock Resume to invoke callbacks, added m.SetSchemeRouter to test helper

## Decisions Made
- Used unexported field (not exported) to prevent GOB serialization of func values
- Protected resumeHandlers with existing dAllocMu (no new mutex needed)
- HTTP resume path intentionally unchanged: passing non-nil handlers to httpProtocolDownloader.Resume would bypass patchHandlers wrapping
- Added SetSchemeRouter call to test helper so Manager.ResumeDownload can dispatch protocol resumes

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Test helper missing SetSchemeRouter call**
- **Found during:** Task 1 (RED phase)
- **Issue:** newTestRPCHandlerWithRouter did not call m.SetSchemeRouter(router), causing Manager.ResumeDownload to fail with "scheme router not initialized"
- **Fix:** Added m.SetSchemeRouter(router) call in newTestRPCHandlerWithRouter
- **Files modified:** internal/server/rpc_ftp_sftp_notify_test.go
- **Verification:** Resume tests reach the actual handler assertion (fail for correct reason: nil handlers)
- **Committed in:** 7898f41 (Task 1 commit)

**2. [Rule 3 - Blocking] Resume integrity check requires destination file**
- **Found during:** Task 1 (RED phase)
- **Issue:** ResumeDownload validates destination file exists when item.Downloaded > 0, but mock doesn't create actual files
- **Fix:** Tests create dummy destination files before calling download.resume
- **Files modified:** internal/server/rpc_ftp_sftp_resume_test.go
- **Verification:** Resume tests pass the integrity check and reach handler assertions
- **Committed in:** 7898f41 (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (2 blocking)
**Impact on plan:** Both fixes necessary for test correctness. No scope creep.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase complete, ready for transition
- INT-02 tech debt fully resolved: FTP/SFTP resume via RPC now emits WebSocket push notifications

## Self-Check: PASSED

---
*Phase: 09-fix-rpc-ftp-sftp-resume-handlers*
*Completed: 2026-02-27*
