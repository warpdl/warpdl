---
phase: 03-ftp-ftps
plan: 03
subsystem: api
tags: [go, ftp, api, daemon, integration]
requires:
  - phase: 03-ftp-ftps
    plan: 01
    provides: "ftpProtocolDownloader"
  - phase: 03-ftp-ftps
    plan: 02
    provides: "Manager.ResumeDownload FTP dispatch"
provides:
  - "API layer FTP dispatch (downloadFTPHandler extracted from downloadHTTPHandler)"
  - "SchemeRouter initialization in daemon core"
  - "Credential stripping verified by GOB round-trip test"
tech-stack:
  added: []
  patterns: [handler extraction, scheme routing, credential stripping]
key-files:
  created:
    - internal/api/download_ftp_test.go
  modified:
    - internal/api/api.go
    - internal/api/download.go
    - internal/api/resume.go
    - internal/api/api_test.go
    - cmd/daemon_core.go
key-decisions:
  - "downloadHandler refactored into downloadHTTPHandler + downloadFTPHandler — zero logic change to HTTP path"
  - "Api struct gains schemeRouter field; NewApi signature updated with router parameter (nil-safe for tests)"
requirements-completed: [FTP-01, FTP-03, FTP-05, FTP-08]
duration: 15min
completed: 2026-02-27
---

# Plan 03-03: FTP API Layer Wiring Summary

**API layer FTP dispatch via downloadFTPHandler, daemon_core SchemeRouter initialization, and credential stripping verification**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-02-27
- **Tasks:** TDD cycle (RED/GREEN/REFACTOR)
- **Files created:** 1
- **Files modified:** 5

## Accomplishments

- downloadHandler refactored into downloadHTTPHandler + downloadFTPHandler with zero logic change to HTTP path
- downloadFTPHandler detects ftp/ftps URLs and routes through SchemeRouter
- daemon_core.go initializes SchemeRouter and passes to NewApi
- Api struct gains schemeRouter field with nil-safe handling for tests
- Credential stripping verified by GOB round-trip test (TestFTPCredentialSecurityGOBRoundTrip)

## Task Commits

1. **API FTP handler extraction** - `9e61af2` (feat: extract downloadFTPHandler and wire SchemeRouter in daemon_core)
2. **Credential stripping test** - `74c97e0` (test: add FTP credential security GOB round-trip test)

## Files Created/Modified

- `internal/api/download_ftp_test.go` - FTP handler tests, credential stripping GOB round-trip test
- `internal/api/api.go` - Api struct gains schemeRouter field; NewApi signature updated
- `internal/api/download.go` - downloadHTTPHandler + downloadFTPHandler split
- `internal/api/resume.go` - FTP/FTPS resume dispatch through SchemeRouter
- `internal/api/api_test.go` - Updated for new NewApi signature
- `cmd/daemon_core.go` - SchemeRouter initialization and injection into NewApi

## Decisions Made

- Handler extraction preserves 100% of HTTP download path logic (zero regression risk)
- NewApi accepts router parameter (nil-safe: tests pass nil, daemon passes real router)
- Credential stripping verified end-to-end via GOB encode/decode round-trip

## Deviations from Plan

None - plan executed as specified.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- FTP/FTPS fully wired end-to-end: CLI -> warpcli -> daemon -> API -> warplib -> FTP server
- Phase 4 (SFTP) can register sftp factory in same SchemeRouter
- Phase 5 (JSON-RPC) can use SchemeRouter for protocol dispatch in RPC methods

---
*Phase: 03-ftp-ftps, Plan: 03*
*Completed: 2026-02-27*
