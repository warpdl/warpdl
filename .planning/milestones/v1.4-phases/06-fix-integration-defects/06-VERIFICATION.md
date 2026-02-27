---
phase: 06-fix-integration-defects
verified: 2026-02-27
result: PASS
requirements-verified: [SFTP-04, SFTP-06, RPC-06, RPC-11, REDIR-04]
---

# Phase 6: Fix Integration Defects -- Verification

## Success Criteria

### SC1: SFTP download started with `--ssh-key /custom/key` can be resumed and still uses the custom key (not default)

**Result: PASS**

Evidence:
- `Item.SSHKeyPath` field added to `pkg/warplib/item.go` -- persisted via GOB encoding
- `manager.go:297` sets `item.SSHKeyPath = opts.SSHKeyPath` in `AddProtocolDownload`
- `manager.go:600` threads `SSHKeyPath: item.SSHKeyPath` into `DownloaderOpts` in `ResumeDownload`
- GOB round-trip test: `TestSSHKeyPathGOBRoundTrip` confirms field survives encode/decode
- Backward compatibility test: `TestSSHKeyPathGOBBackwardCompat` confirms pre-Phase-2 fixtures decode with empty SSHKeyPath
- Resume threading tests: `TestSFTPResumePreservesCustomSSHKey` and `TestSFTPResumeDefaultKeyWhenNone`

### SC2: RPC `download.resume` delivers push notifications (progress/complete/error) to connected WebSocket clients

**Result: PASS**

Evidence:
- `rpc_methods.go:287-310` constructs `ResumeDownloadOpts` with `Handlers` wired to `rs.notifier.Broadcast` for all 3 event types (error, progress, complete)
- `rpc_methods.go:322` broadcasts `download.started` after successful resume
- Handler wiring test: `TestRPCDownloadResume_HandlerWiring` exercises code path without panic
- Nil-notifier test: `TestRPCDownloadResume_NilNotifier` confirms no panic when notifier has no registered servers
- Broadcast-started test: `TestRPCDownloadResume_BroadcastsStarted` exercises full add->pause->resume flow

### SC3: `web.go processDownload` creates `http.Client` with explicit `CheckRedirect` matching Phase 1 redirect policy

**Result: PASS**

Evidence:
- `web.go:58` sets `CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects)` on the http.Client
- Behavioral test: `TestProcessDownload_RedirectLoopBlocked` verifies redirect loop detection

**Deviation noted:** The behavioral test passed even before the web.go fix because `NewDownloader` at `dloader.go:222` already patches `client.CheckRedirect` if nil. The explicit web.go fix is defense-in-depth. Both layers now enforce the policy.

## Requirements Verification

| Requirement | Status | Evidence |
|-------------|--------|----------|
| SFTP-04 | PASS | SSHKeyPath persisted in Item, threaded through resume (4 tests) |
| SFTP-06 | PASS | Resume threading verified by mock downloader tests |
| RPC-06 | PASS | downloadResume wires handlers and broadcasts started (3 tests) |
| RPC-11 | PASS | All 4 notification types broadcast in resume path |
| REDIR-04 | PASS | web.go explicitly sets CheckRedirect with RedirectPolicy |

## Gate Results

- All tests pass: YES (`go test -race -short ./...` -- 0 failures)
- Race detection: CLEAN
- Build: CLEAN (`go build -ldflags="-w -s" .`)
- Vet: CLEAN (`go vet ./...`)

## Files Modified

### Plan 06-01 (SFTP key persistence + web.go redirect)
- `pkg/warplib/item.go` -- Added SSHKeyPath field
- `pkg/warplib/manager.go` -- Added SSHKeyPath to AddDownloadOpts, persistence in AddProtocolDownload, threading in ResumeDownload
- `internal/api/download.go` -- Pass SSHKeyPath through to AddDownloadOpts
- `internal/server/rpc_methods.go` -- Pass SSHKeyPath through to AddDownloadOpts
- `internal/server/web.go` -- Added explicit CheckRedirect

### Plan 06-02 (RPC resume notifications)
- `internal/server/rpc_methods.go` -- Wired handlers and download.started broadcast in downloadResume

### Tests Created
- `pkg/warplib/sftp_resume_key_test.go` -- 4 tests
- `internal/server/web_redirect_test.go` -- 1 test
- `internal/server/rpc_resume_notify_test.go` -- 3 tests

---
*Verified: 2026-02-27*
