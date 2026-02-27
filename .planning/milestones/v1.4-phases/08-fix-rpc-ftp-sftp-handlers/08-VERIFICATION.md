---
phase: 08-fix-rpc-ftp-sftp-handlers
status: passed
verified: 2026-02-27
verifier: automated
requirements_verified: [RPC-05, RPC-11]
---

# Phase 8: Fix RPC FTP/SFTP Download Add Handlers - Verification

## Phase Goal
Wire missing notifier handlers in RPC `download.add` FTP/SFTP code path so WebSocket push notifications are delivered and item progress is persisted during FTP/SFTP downloads started via JSON-RPC.

## Must-Have Verification

### 1. FTP/SFTP downloads emit WebSocket push notifications
**Status:** PASSED
**Evidence:** `opts.Handlers` (containing notifier closures for download.progress, download.complete, download.error) is now passed to both `AddProtocolDownload` (line 242) and `pd.Download` (line 259) in `rpc_methods.go`. Previously `nil` was passed in both positions.

### 2. item.Downloaded updated during FTP/SFTP RPC downloads
**Status:** PASSED
**Evidence:** `TestRPCDownloadAdd_FTP_ProgressTracked` asserts `item.GetDownloaded() >= item.GetTotalSize()` and `item.GetTotalSize() > 0`. `TestRPCDownloadAdd_SFTP_ProgressTracked` does the same for SFTP. Both pass.

### 3. Tests prove handler callbacks are invoked
**Status:** PASSED
**Evidence:** `TestRPCDownloadAdd_FTP_HandlerCallbacksFired` uses atomic counters on mock to assert `progressCalled > 0` and `completeCalled > 0`. This proves the handler callbacks are actually invoked, not just syntactically present.

### 4. All tests pass with -race flag
**Status:** PASSED
**Evidence:** `go test -race -short ./internal/server/... -count=1` passes with zero failures. The `go pd.Download(context.Background(), opts.Handlers)` goroutine has no data races because handler callbacks use thread-safe item methods.

### 5. Server package coverage >= 80%
**Status:** PASSED
**Evidence:** `go test -cover ./internal/server/...` reports 89.9% coverage (threshold: 80%).

## Requirement Verification

### RPC-05: download.add method accepts URL and options, starts download
**Status:** PASSED (strengthened)
**Verification:** `download.add` now works correctly for FTP/SFTP URLs with handler callbacks wired. Previously FTP/SFTP downloads via RPC worked but silently dropped notifications and progress tracking.

### RPC-11: WebSocket pushes real-time notifications
**Status:** PASSED (strengthened)
**Verification:** FTP/SFTP downloads now emit download.progress, download.complete, and download.error via WebSocket. Previously only HTTP downloads emitted these notifications from the RPC path. The fix wires the same `opts.Handlers` (built at lines 170-191 with notifier closures) into both the manager and downloader.

## Key Artifacts

| Artifact | Path |
|----------|------|
| Test file | `internal/server/rpc_ftp_sftp_notify_test.go` |
| Production fix | `internal/server/rpc_methods.go` (lines 242, 259) |
| Summary | `.planning/phases/08-fix-rpc-ftp-sftp-handlers/08-01-SUMMARY.md` |

## Key Links Verified

| From | To | Via | Verified |
|------|----|-----|----------|
| `rpc_methods.go:downloadAdd` | `manager.go:AddProtocolDownload` | `opts.Handlers` passed as 5th arg | Yes |
| `rpc_methods.go:downloadAdd` | `pd.Download` | `opts.Handlers` passed as 2nd arg | Yes |

## Conclusion

Phase 8 goal fully achieved. The 2-line fix (`nil` -> `opts.Handlers` in two positions) closes the INT-01 tech debt identified in the v1.0 milestone audit. FTP/SFTP downloads started via JSON-RPC now have full parity with HTTP downloads for handler callback wiring.
