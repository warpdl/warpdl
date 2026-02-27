---
phase: 03-ftp-ftps
plan: 01
subsystem: core
tags: [go, ftp, protocol, tdd]
requires:
  - phase: 02-protocol-interface
    provides: "ProtocolDownloader interface and SchemeRouter"
provides:
  - "ftpProtocolDownloader implementing ProtocolDownloader for ftp:// and ftps:// URLs"
  - "StripURLCredentials for secure URL persistence"
  - "patchProtocolHandlers for wiring event callbacks to protocol downloaders"
tech-stack:
  added: [github.com/jlaffaye/ftp v0.2.0]
  patterns: [single-stream download, factory registration, progress writer adapter]
key-files:
  created:
    - pkg/warplib/protocol_ftp.go
    - pkg/warplib/protocol_ftp_test.go
  modified:
    - pkg/warplib/protocol_router.go
    - pkg/warplib/manager.go
    - go.mod
    - go.sum
key-decisions:
  - "FTP uses single-stream download (SupportsParallel=false, MaxConnections=1) — jlaffaye/ftp ServerConn is not goroutine-safe"
  - "StripURLCredentials exported from warplib for cross-package use in API layer"
  - "classifyFTPError uses standard errors.As (not generic wrapper) — 4xx transient, 5xx permanent, net.Error transient"
  - "Commit e053d93 consolidated Resume() and FTPS TLS into this plan (originally scoped for 03-02)"
requirements-completed: [PROTO-01, PROTO-03, FTP-01, FTP-02, FTP-03, FTP-04, FTP-05, FTP-06, FTP-07, FTP-08]
duration: 15min
completed: 2026-02-27
---

# Plan 03-01: FTP Protocol Downloader Summary

**ftpProtocolDownloader with Probe/Download/Resume, anonymous and credential auth, passive mode, FTPS explicit TLS, and single-stream download via jlaffaye/ftp**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-02-27
- **Tasks:** TDD cycle (RED/GREEN/REFACTOR)
- **Files created:** 2
- **Files modified:** 4

## Accomplishments

- Implemented ftpProtocolDownloader satisfying ProtocolDownloader interface with compile-time check
- Factory function parses ftp:// and ftps:// URLs, extracts host:port, path, credentials, and TLS flag
- Anonymous auth by default (anonymous/anonymous) when no credentials in URL
- Credential auth via URL userinfo (ftp://user:pass@host/path)
- Passive mode (EPSV default via jlaffaye/ftp library)
- FTPS explicit TLS via DialWithExplicitTLS with tls.Config{ServerName: hostname}
- Single-stream download (SupportsParallel=false, MaxConnections=1) due to ServerConn not being goroutine-safe
- File size reporting via Probe() calling FTP SIZE command
- Resume via RetrFrom(path, offset) with offset derived from destination file size on disk
- StripURLCredentials for secure persistence (credentials never stored in GOB)
- classifyFTPError with transient/permanent error classification

## Task Commits

1. **FTP downloader implementation** - `e053d93` (feat: implement FTP protocol downloader with Resume and FTPS TLS)

Note: Commit e053d93 consolidated Resume (originally 03-02 scope) and FTPS TLS into this plan.

## Files Created/Modified

- `pkg/warplib/protocol_ftp.go` - ftpProtocolDownloader, newFTPProtocolDownloader factory, Probe/Download/Resume, classifyFTPError, StripURLCredentials
- `pkg/warplib/protocol_ftp_test.go` - 48+ test cases with ftpserverlib mock server
- `pkg/warplib/protocol_router.go` - ftp/ftps factory registered in SchemeRouter
- `pkg/warplib/manager.go` - AddProtocolDownload, patchProtocolHandlers
- `go.mod` / `go.sum` - github.com/jlaffaye/ftp v0.2.0

## Decisions Made

- FTP uses single-stream download because jlaffaye/ftp ServerConn is not goroutine-safe
- StripURLCredentials exported for cross-package use in API layer
- classifyFTPError uses standard errors.As (4xx transient, 5xx permanent, net.Error transient)
- Resume and FTPS consolidated into 03-01 from originally planned 03-02 scope

## Deviations from Plan

### Auto-fixed Issues

**1. Scope consolidation: Resume and FTPS pulled into 03-01**
- **Found during:** Implementation
- **Issue:** Resume and FTPS TLS were originally scoped for 03-02 but the implementation naturally included them
- **Fix:** Implemented Resume() and FTPS in the same commit, reducing 03-02 work
- **Verification:** All FTP-01 through FTP-08 requirements pass

---

**Total deviations:** 1 (scope consolidation, positive impact)
**Impact on plan:** Reduced 03-02 scope; all FTP requirements addressed in 03-01

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- ftpProtocolDownloader fully functional for all FTP operations
- Plan 03-02 focuses on Manager.ResumeDownload dispatch (Resume() itself already implemented)
- Plan 03-03 needs API handler and daemon_core wiring

---
*Phase: 03-ftp-ftps, Plan: 01*
*Completed: 2026-02-27*
