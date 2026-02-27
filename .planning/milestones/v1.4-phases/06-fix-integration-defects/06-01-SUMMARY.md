---
phase: 06-fix-integration-defects
plan: 01
subsystem: core
tags: [sftp, ssh, gob, redirect, persistence, resume]

requires:
  - phase: 04-sftp
    provides: SFTP downloader with SSHKeyPath in DownloaderOpts
  - phase: 01-http-redirect
    provides: RedirectPolicy function for CheckRedirect enforcement
provides:
  - Item.SSHKeyPath persistence for SFTP resume with custom keys
  - web.go processDownload redirect policy enforcement
affects: [06-fix-integration-defects]

tech-stack:
  added: []
  patterns:
    - "Item field persistence for protocol-specific resume state"

key-files:
  created:
    - pkg/warplib/sftp_resume_key_test.go
    - internal/server/web_redirect_test.go
  modified:
    - pkg/warplib/item.go
    - pkg/warplib/manager.go
    - internal/api/download.go
    - internal/server/rpc_methods.go
    - internal/server/web.go

key-decisions:
  - "SSHKeyPath placed after Protocol field in Item struct for GOB backward compatibility (zero value = empty string)"
  - "AddDownloadOpts gets SSHKeyPath field to thread key from callers through to Item persistence"

patterns-established:
  - "Protocol-specific Item fields persist resume state beyond what the URL carries"

requirements-completed: [SFTP-04, SFTP-06, REDIR-04]

duration: 4min
completed: 2026-02-27
---

# Phase 6 Plan 1: SFTP Resume SSH Key Persistence and web.go Redirect Policy Summary

**Item.SSHKeyPath field persists SFTP SSH key path across resume cycles; web.go processDownload enforces RedirectPolicy**

## Performance

- **Duration:** 4 min
- **Started:** 2026-02-27T20:20:16Z
- **Completed:** 2026-02-27T20:24:23Z
- **Tasks:** 2 (SFTP key persistence + web.go redirect fix)
- **Files modified:** 7

## Accomplishments
- SFTP downloads with `--ssh-key /custom/key` now survive pause/resume cycles
- GOB backward compatibility verified: pre-Phase-2 fixtures decode with empty SSHKeyPath
- SSHKeyPath threaded end-to-end: CLI -> API -> AddProtocolDownload -> Item -> ResumeDownload -> NewDownloader
- web.go processDownload creates http.Client with explicit CheckRedirect matching Phase 1 policy

## Task Commits

Each task was committed atomically:

1. **Task 1 RED: Failing tests** - `20eb0c6` (test)
2. **Task 1+2 GREEN: Implementation** - `b793f61` (feat)
3. **Task REFACTOR: go fmt cleanup** - `f43f3f3` (refactor)

## Files Created/Modified
- `pkg/warplib/item.go` - Added SSHKeyPath field to Item struct
- `pkg/warplib/manager.go` - Added SSHKeyPath to AddDownloadOpts, threading in AddProtocolDownload/ResumeDownload
- `pkg/warplib/sftp_resume_key_test.go` - GOB round-trip, backward compat, resume threading tests
- `internal/api/download.go` - Pass SSHKeyPath from DownloadParams to AddDownloadOpts
- `internal/server/rpc_methods.go` - Pass SSHKeyPath from AddParams to AddDownloadOpts
- `internal/server/web.go` - Added CheckRedirect to processDownload http.Client
- `internal/server/web_redirect_test.go` - Behavioral test for redirect loop blocking

## Decisions Made
- SSHKeyPath placed after Protocol field in Item struct -- GOB backward compatible (zero value = empty string)
- AddDownloadOpts gets SSHKeyPath to thread from both API and RPC callers

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] web.go redirect test passes before fix due to NewDownloader safety net**
- **Found during:** RED phase (TestProcessDownload_RedirectLoopBlocked)
- **Issue:** NewDownloader already patches client.CheckRedirect if nil, so the behavioral test passes even before the web.go fix
- **Fix:** Applied the web.go fix anyway for defense-in-depth (explicit is better than implicit). Test validates correct behavior end-to-end.
- **Files modified:** internal/server/web.go, internal/server/web_redirect_test.go
- **Verification:** Test passes with correct warplib.RedirectPolicy error message
- **Committed in:** b793f61 (part of GREEN commit)

---

**Total deviations:** 1 auto-fixed (1 bug/defense-in-depth)
**Impact on plan:** Minimal -- fix still applied as planned, test validates behavior correctly.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Plan 06-01 complete, ready for Plan 06-02 (RPC resume notification wiring)
- All SFTP resume state persistence is now in place

---
*Phase: 06-fix-integration-defects*
*Completed: 2026-02-27*
