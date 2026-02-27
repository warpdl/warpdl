---
phase: 05-json-rpc-20
plan: 02
subsystem: daemon
tags: [go, jsonrpc, methods, download, rpc]
requires:
  - phase: 05-json-rpc-20
    plan: 01
    provides: "JSON-RPC 2.0 HTTP endpoint and auth middleware"
provides:
  - "download.add method (start downloads via RPC)"
  - "download.pause and download.resume methods"
  - "download.remove method"
  - "download.status method (state, progress, speed)"
  - "download.list method (filter by active/waiting/stopped)"
tech-stack:
  added: []
  patterns: [jrpc2 handler methods, manager delegation, error code mapping]
key-files:
  created: []
  modified:
    - internal/server/rpc_methods.go
    - internal/server/rpc_methods_test.go
    - internal/server/web.go
    - internal/server/server.go
    - cmd/daemon_core.go
key-decisions:
  - "download.add dispatches HTTP URLs via warplib.NewDownloader, FTP/SFTP via schemeRouter.NewDownloader"
  - "download.pause calls item.StopDownload(), not Manager-level stop"
  - "Custom error codes: -32001 (download not found), -32002 (download not running)"
requirements-completed: [RPC-05, RPC-07, RPC-08, RPC-09, RPC-10, RPC-12]
duration: 30min
completed: 2026-02-27
---

# Plan 05-02: RPC Method Suite Summary

**Full download lifecycle method suite: download.add/pause/resume/remove/status/list with custom error codes and protocol-aware dispatch**

## Performance

- **Duration:** ~30 min
- **Completed:** 2026-02-27
- **Tasks:** TDD cycle (RED/GREEN/REFACTOR)
- **Files modified:** 5

## Accomplishments

- download.add accepts URL+options and starts download, dispatches HTTP via NewDownloader, FTP/SFTP via SchemeRouter
- download.pause/resume control download lifecycle (pause calls StopDownload, resume restarts)
- download.remove clears item from manager
- download.status returns full state (progress, speed, file info)
- download.list filters by status (active/waiting/stopped)
- system.getVersion returns configured version info
- Custom error codes: -32001 (download not found), -32002 (download not running)

Note: RPC-06 (pause/resume) was implemented but had a defect in resume path (no push notifications for resumed downloads) -- fixed in Phase 6.

## Task Commits

1. **Method suite implementation** - `2df0db3` (feat: implement download.add/pause/resume/remove/status/list methods)

## Files Created/Modified

- `internal/server/rpc_methods.go` - All download.* methods, system.getVersion, error code constants
- `internal/server/rpc_methods_test.go` - Unit tests for each method
- `internal/server/web.go` - Method registration in handler map
- `internal/server/server.go` - Server config for method dependencies
- `cmd/daemon_core.go` - Manager and SchemeRouter threading to RPC methods

## Decisions Made

- download.add dispatches based on URL scheme (HTTP/HTTPS direct, FTP/SFTP via SchemeRouter)
- download.pause uses item.StopDownload() (not Manager-level stop) for granular control
- Custom error codes (-32001, -32002) in the application range per JSON-RPC 2.0 spec

## Deviations from Plan

None - plan executed as specified.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Method suite complete for HTTP and WebSocket access
- Plan 05-03 adds WebSocket endpoint and push notifications
- Plan 05-04 adds integration tests covering all methods end-to-end

---
*Phase: 05-json-rpc-20, Plan: 02*
*Completed: 2026-02-27*
