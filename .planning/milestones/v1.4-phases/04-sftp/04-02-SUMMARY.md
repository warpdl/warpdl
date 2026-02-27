---
phase: 04-sftp
plan: 02
subsystem: core
tags: [sftp, resume, manager, scheme-router]

requires:
  - phase: 04-sftp/01
    provides: sftpProtocolDownloader with Resume() already implemented
  - phase: 03-ftp-ftps
    provides: Manager.ResumeDownload FTP/FTPS pattern (ProtoFTP/ProtoFTPS cases)
provides:
  - Manager.ResumeDownload dispatches ProtoSFTP items through SchemeRouter
  - Protocol guard: SFTP skips validateDownloadIntegrity, checks dest file when Downloaded>0
affects: [04-03-sftp-api]

tech-stack:
  added: []
  patterns: [ProtoSFTP added to existing FTP/FTPS case lists in manager.go]

key-files:
  modified:
    - pkg/warplib/manager.go
    - pkg/warplib/protocol_sftp_test.go

key-decisions:
  - "Resume() was already implemented in 04-01 (not a stub) — this plan only wires Manager dispatch"
  - "Two-line change: add ProtoSFTP to existing case lists in protocol guard and dispatch"
  - "Error messages made protocol-agnostic using item.Protocol.String()"

patterns-established:
  - "ProtoSFTP shares exact same Manager.ResumeDownload path as ProtoFTP/ProtoFTPS"

requirements-completed: [SFTP-06]

duration: 15min
completed: 2026-02-27
---

# Plan 04-02: SFTP Resume Manager Dispatch Summary

**Manager.ResumeDownload wired for ProtoSFTP via two-line case list extension alongside FTP/FTPS**

## Performance

- **Duration:** ~15 min
- **Completed:** 2026-02-27
- **Tasks:** 2 (tests + implementation)
- **Files modified:** 2

## Accomplishments
- Manager.ResumeDownload dispatches ProtoSFTP items through SchemeRouter (not initDownloader)
- Protocol guard: SFTP items skip validateDownloadIntegrity, check dest file when Downloaded>0
- 7 new subtests covering dispatch, integrity guard, and regression

## Task Commits

1. **Tests** - `6e41902` (test: Manager.ResumeDownload SFTP dispatch and integrity guard tests)
2. **Implementation** - `ac666f4` (feat: wire Manager.ResumeDownload for SFTP protocol dispatch)

## Files Created/Modified
- `pkg/warplib/manager.go` - Added ProtoSFTP to protocol guard and dispatch switches
- `pkg/warplib/protocol_sftp_test.go` - Added TestResumeDownloadSFTP and TestResumeDownloadSFTPIntegrityGuard

## Decisions Made
- Resume() was fully implemented in 04-01, so this plan only adds Manager dispatch (minimal diff)
- Error messages made protocol-agnostic using item.Protocol instead of hardcoded "FTP"

## Deviations from Plan
None significant. Plan expected Resume() implementation here, but it was already done in 04-01.

## User Setup Required
None.

## Next Phase Readiness
- SFTP download and resume fully wired through Manager
- Plan 04-03 needs: API handler (downloadSFTPHandler), --ssh-key CLI flag

---
*Phase: 04-sftp, Plan: 02*
*Completed: 2026-02-27*
