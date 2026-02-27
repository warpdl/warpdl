---
phase: 09-fix-rpc-ftp-sftp-resume-handlers
status: passed
verified: 2026-02-27
requirements: [RPC-06, RPC-11]
---

# Phase 9: Fix RPC FTP/SFTP Resume Handler Pass-Through -- Verification

## Goal
Fix `Item.Resume()` to pass handlers through to `ProtocolDownloader.Resume()` so FTP/SFTP downloads resumed via JSON-RPC deliver WebSocket push notifications.

## Success Criteria Verification

### 1. FTP/SFTP downloads resumed via JSON-RPC `download.resume` emit WebSocket push notifications
**Status: PASSED**

Evidence:
- `TestRPCDownloadResume_FTP_HandlersFired` passes: mock.progressCalled=1, mock.completeCalled=1 after resume
- `TestRPCDownloadResume_SFTP_HandlersFired` passes: mock.progressCalled=1, mock.completeCalled=1 after resume
- Handler callbacks invoked during resume trigger `rs.notifier.Broadcast("download.progress", ...)` and `rs.notifier.Broadcast("download.complete", ...)` via the patched handler chain

### 2. `Item.Resume()` passes patched handlers to `ProtocolDownloader.Resume()` instead of `nil`
**Status: PASSED**

Evidence:
- `grep -c 'partsCopy, nil' pkg/warplib/item.go` returns 0 (nil removed)
- `pkg/warplib/item.go:300`: `return d.Resume(context.Background(), partsCopy, h)` -- passes `h` (stored resumeHandlers)
- `h` is non-nil for FTP/SFTP items (set by Manager.ResumeDownload) and nil for HTTP items (setResumeHandlers never called)

### 3. Existing HTTP resume path remains unaffected (no regression)
**Status: PASSED**

Evidence:
- `TestRPCDownloadResume_BroadcastsStarted` PASS
- `TestRPCDownloadResume_NilNotifier` PASS
- `TestRPCDownloadResume_HandlerWiring` PASS
- All 3 existing HTTP resume tests pass with `-race` flag
- HTTP items never call `setResumeHandlers`, so `resumeHandlers` stays nil
- `httpProtocolDownloader.Resume` ignores nil handlers (uses struct field handlers from patchHandlers)

## Requirements Traceability

| Requirement | Status | Evidence |
|-------------|--------|----------|
| RPC-06 | Complete | download.resume RPC method works for FTP/SFTP with handler callbacks |
| RPC-11 | Complete | WebSocket push notifications fire during FTP/SFTP resume via patched handler chain |

## Coverage

- Server package: 90.1% (>= 80% threshold)
- Warplib package: 85.8% (>= 80% threshold)
- Full suite: `go test -race -short ./...` passes with zero failures

## Conclusion

Phase 9 goal fully achieved. INT-02 tech debt is closed. FTP/SFTP resume via RPC now emits WebSocket push notifications through the stored handler pass-through pattern.
