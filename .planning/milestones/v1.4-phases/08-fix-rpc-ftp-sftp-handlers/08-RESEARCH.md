# Phase 8: Fix RPC FTP/SFTP Download Add Handlers - Research

**Researched:** 2026-02-27
**Domain:** Go event handler wiring in RPC download.add FTP/SFTP code path
**Confidence:** HIGH

## Summary

The defect is precisely identified in the v1.0 audit as INT-01. In `internal/server/rpc_methods.go`, the `downloadAdd` function correctly builds `opts.Handlers` with notifier closures (lines 170-191) for HTTP/HTTPS downloads. However, in the `default` branch (lines 219-260) that handles FTP/FTPS/SFTP, two calls pass `nil` instead of `opts.Handlers`:
1. `rs.manager.AddProtocolDownload(pd, probe, cleanURL, proto, nil, ...)` — line 242 passes `nil` for handlers, so `patchProtocolHandlers` in `manager.go` receives nil and returns immediately (line 323: `if h == nil { return }`), meaning item progress (`Downloaded`) is never updated during FTP/SFTP download.
2. `go pd.Download(context.Background(), nil)` — line 259 passes `nil` handlers, so no `ErrorHandler`, `DownloadProgressHandler`, or `DownloadCompleteHandler` closures execute, meaning no WebSocket push notifications fire.

The fix is surgical: replace both `nil` arguments with `opts.Handlers`. The existing handler wiring infrastructure in `patchProtocolHandlers` and the notifier `Broadcast` paths are already correct and battle-tested. No new abstractions are needed.

The parallel CLI path (`internal/api/download.go:downloadProtocolHandler`) demonstrates the exact correct pattern: it builds `handlers := &warplib.Handlers{...}` with closures, passes them to `AddProtocolDownload`, and passes them again to `pd.Download`. The RPC `downloadResume` path (fixed in Phase 6) also demonstrates the correct pattern with `opts.Handlers` wired through `ResumeDownloadOpts`.

**Primary recommendation:** Replace the two `nil` arguments with `opts.Handlers` in the FTP/SFTP branch of `downloadAdd`. Add unit tests that verify handler closures are called during FTP/SFTP download via a mock `ProtocolDownloader` with a real `SchemeRouter.Register` injection.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| RPC-05 | `download.add` method accepts URL and options, starts download | Fix ensures FTP/SFTP download.add fully starts and tracks item state (Downloaded updated) |
| RPC-11 | WebSocket pushes real-time notifications (download.started, download.progress, download.complete, download.error) | Fix wires handlers so notifier.Broadcast calls fire for all four notification types on FTP/SFTP download.add path |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/warpdl/warpdl/pkg/warplib` | local | ProtocolDownloader interface, SchemeRouter, Handlers, Manager | The entire protocol dispatch and item-update infrastructure lives here |
| `github.com/warpdl/warpdl/internal/server` | local | RPCServer, RPCNotifier, rpc_methods.go | The file containing the defect |
| Go standard `testing` | 1.24.9+ | Unit tests | Project uses standard `go test` with no extra test framework |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/creachadair/jrpc2` | v1.3.4 | JSON-RPC server and Notify | Already wired; notifier uses `srv.Notify` for WebSocket push |
| `github.com/coder/websocket` | v1.8.14 | WebSocket integration tests | Already used in rpc_integration_test.go for end-to-end notification tests |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Mock ProtocolDownloader via SchemeRouter.Register | Real FTP server (ftpserverlib) | Mock is faster, deterministic, no network; real server tests protocol but not needed here since we're testing handler wiring not FTP protocol |
| Adding new test file | Extending rpc_resume_notify_test.go | New file is cleaner given the subject is download.add FTP/SFTP specifically |

## Architecture Patterns

### Recommended Project Structure

The fix touches one file. New tests go in a new file:

```
internal/server/
├── rpc_methods.go          ← MODIFY: two nil → opts.Handlers (lines 242, 259)
└── rpc_ftp_sftp_notify_test.go   ← NEW: tests for FTP/SFTP handler wiring
```

### Pattern 1: Mock ProtocolDownloader via SchemeRouter.Register
**What:** Register a test-controlled factory for "ftp"/"sftp" schemes into a `SchemeRouter`, then pass that router to `NewRPCServer`. The mock `ProtocolDownloader.Download` synchronously calls handler callbacks so tests can assert handler calls.
**When to use:** When testing handler wiring without real network connections.
**Example:**
```go
// Source: pkg/warplib/protocol_router_test.go (existing pattern)
router := warplib.NewSchemeRouter(http.DefaultClient)
called := false
router.Register("ftp", func(rawURL string, opts *warplib.DownloaderOpts) (warplib.ProtocolDownloader, error) {
    return &mockFTPDownloader{handlers: opts.Handlers}, nil
})
// Then pass router to NewRPCServer
```

**CRITICAL NOTE:** The mock `ProtocolDownloader` must be defined in the `server` package test file (not the `warplib` package), because `mockProtocolDownloader` in `pkg/warplib/protocol_test.go` is package-private to `warplib`. The `server` package tests need their own mock.

### Pattern 2: Existing handler-wiring test pattern (from rpc_resume_notify_test.go)
**What:** Verify no panics, verify handler closures are invoked, or verify `item.Downloaded` was updated.
**When to use:** For structural verification that handler construction occurs.
**Example:**
```go
// Source: internal/server/rpc_resume_notify_test.go
// Tests call the RPC method, wait for completion, check item state
```

### Pattern 3: Existing notifier broadcast pattern
**What:** The notifier has a `Count()` method and `Register/Unregister`. For notification delivery tests, use the `newTestServer` helper from `rpc_notify_test.go` that wires a jrpc2 server and drains notifications via `cli.Recv()`.
**When to use:** When verifying actual WebSocket push delivery.

### Anti-Patterns to Avoid
- **Passing nil for handlers in FTP/SFTP branch**: This is the exact defect. After the fix, nil must never be passed to `AddProtocolDownload` or `pd.Download` when `opts.Handlers` is non-nil.
- **Building opts.Handlers in HTTP branch only**: Current code builds handlers before the switch, so `opts.Handlers` is already set when the `default` case runs. The fix just needs to use it.
- **Adding handler construction inside the default branch**: Handlers are already built at line 170-191 before the scheme switch. Don't duplicate them.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Mock FTP/SFTP protocol downloader for tests | Custom full FTP protocol | `mockProtocolDownloader` struct (new in server test package) implementing `warplib.ProtocolDownloader` | The mock pattern is already established in `pkg/warplib/protocol_test.go` — replicate it in server tests |
| Handler invocation tracking | Custom callback registry | Capture closures with `sync/atomic` counter or channel signal in test-local mock | Go channels or atomics are the standard pattern for synchronous handler callback verification |
| WebSocket notification verification in FTP/SFTP test | New WS infrastructure | Reuse `newTestServer` + `cli.Recv()` from `rpc_notify_test.go` | Already tested and working |

## Common Pitfalls

### Pitfall 1: mock.Download ignoring handlers
**What goes wrong:** The mock `ProtocolDownloader.Download` method ignores the `*Handlers` argument, so progress/complete callbacks never fire, making the test verify nothing meaningful.
**Why it happens:** Lazy mock implementation.
**How to avoid:** The mock's `Download` method must explicitly call `handlers.DownloadProgressHandler(hash, nread)` and `handlers.DownloadCompleteHandler(MAIN_HASH, total)` to exercise the wiring.
**Warning signs:** Test passes even before the fix is applied (since nil handlers also "work" without panicking due to nil-guarded callbacks).

### Pitfall 2: Race on item.Downloaded check
**What goes wrong:** Test reads `item.Downloaded` without waiting for the download goroutine to complete, seeing 0 even after the fix.
**Why it happens:** `pd.Download` is called in a goroutine (`go pd.Download(...)`). The test must wait for completion.
**How to avoid:** Either make the mock synchronous (call handlers before `Download` returns) OR poll `item.GetDownloaded()` with a deadline. The mock approach (synchronous handler calls before return) is simpler and removes timing dependencies.

### Pitfall 3: mock.Probe not setting probed=true
**What goes wrong:** If `Probe()` is called but the mock doesn't set internal state, `Download()` returns `ErrProbeRequired`.
**Why it happens:** `downloadAdd` calls `pd.Probe(ctx)` before `pd.Download(ctx, handlers)`. The mock must accept the probe call and allow subsequent `Download` calls.
**How to avoid:** Set `m.probed = true` in the mock's `Probe()` method (pattern already established in `pkg/warplib/protocol_test.go:mockProtocolDownloader`).

### Pitfall 4: SchemeRouter.Register overrides real FTP factory
**What goes wrong:** If a real `SchemeRouter` is used and the test registers a mock factory for "ftp", the mock is used for the test. If the test then also calls `sftp://`, a separate Register call is needed for "sftp".
**Why it happens:** Each scheme ("ftp", "ftps", "sftp") has its own factory entry in `routes`.
**How to avoid:** Register mock factories for all schemes under test ("ftp", "ftps", "sftp") when testing the full matrix.

### Pitfall 5: Coverage regression (currently 86.0%)
**What goes wrong:** New code paths (opts.Handlers being used in FTP/SFTP branch) must be covered by tests or coverage drops.
**Why it happens:** Lines 242 and 259 change from `nil` (untestable path for handlers) to `opts.Handlers` (new code path).
**How to avoid:** New tests must exercise the FTP/SFTP `downloadAdd` path to cover the modified lines. The 80% CI minimum is not at risk (86% now), but new lines should be covered.

### Pitfall 6: MAIN_HASH constant needed in mock
**What goes wrong:** `patchProtocolHandlers` only finalizes item state when `hash == MAIN_HASH` in `DownloadCompleteHandler`. If the mock calls the complete handler with the wrong hash, `item.Downloaded` won't be set to `item.TotalSize`.
**Why it happens:** Design decision: multi-part HTTP downloads have per-part hashes; the final "all done" event uses `MAIN_HASH`. FTP/SFTP are single-stream and must use `MAIN_HASH` for the complete event.
**How to avoid:** The mock's `Download` method must call `handlers.DownloadCompleteHandler(warplib.MAIN_HASH, totalBytes)` — not a random hash.

## Code Examples

### The Fix (surgical, 2-line change)
```go
// Source: internal/server/rpc_methods.go (lines ~242, ~259)
// BEFORE (defective):
if err := rs.manager.AddProtocolDownload(pd, probe, cleanURL, proto, nil, &warplib.AddDownloadOpts{...}); err != nil {
// ...
go pd.Download(context.Background(), nil)

// AFTER (correct):
if err := rs.manager.AddProtocolDownload(pd, probe, cleanURL, proto, opts.Handlers, &warplib.AddDownloadOpts{...}); err != nil {
// ...
go pd.Download(context.Background(), opts.Handlers)
```

### Mock ProtocolDownloader Pattern for Server Tests
```go
// Source: pkg/warplib/protocol_test.go (existing mock — replicate in server pkg)
// In internal/server/rpc_ftp_sftp_notify_test.go:
type mockFTPDownloader struct {
    hash          string
    fileName      string
    downloadDir   string
    contentLength warplib.ContentLength
    probed        bool
    stopped       bool
    // progressCalls captures handler invocations for assertion
    progressCalled  int32 // atomic
    completeCalled  int32 // atomic
}

func (m *mockFTPDownloader) Probe(_ context.Context) (warplib.ProbeResult, error) {
    m.probed = true
    return warplib.ProbeResult{
        FileName:      m.fileName,
        ContentLength: int64(m.contentLength),
        Resumable:     true,
    }, nil
}

func (m *mockFTPDownloader) Download(_ context.Context, h *warplib.Handlers) error {
    if !m.probed {
        return warplib.ErrProbeRequired
    }
    if h == nil {
        return nil // handlers not wired — defect still present
    }
    // Simulate download progress
    if h.DownloadProgressHandler != nil {
        h.DownloadProgressHandler(m.hash, 512)
        atomic.AddInt32(&m.progressCalled, 1)
    }
    // Signal completion — MUST use MAIN_HASH for patchProtocolHandlers to finalize item
    if h.DownloadCompleteHandler != nil {
        h.DownloadCompleteHandler(warplib.MAIN_HASH, int64(m.contentLength))
        atomic.AddInt32(&m.completeCalled, 1)
    }
    return nil
}
```

### Test: Verify item.Downloaded Updated (RPC-05)
```go
func TestRPCDownloadAdd_FTP_ProgressTracked(t *testing.T) {
    // Setup: register mock FTP factory in router
    // Call download.add with ftp:// URL
    // Wait for download goroutine to complete
    // Assert item.GetDownloaded() == item.GetTotalSize()
}
```

### Test: Verify WebSocket Push Notifications (RPC-11)
```go
func TestRPCDownloadAdd_FTP_NotifierCalled(t *testing.T) {
    // Setup: create RPCServer with notifier + mock router
    // Track notifier.Broadcast calls via custom notifier wrapper OR
    // use jrpc2 server + cli.Recv() pattern from rpc_notify_test.go
    // Call download.add with ftp:// URL
    // Assert download.started and download.complete notifications delivered
}
```

### How patchProtocolHandlers Works (Reference)
```go
// Source: pkg/warplib/manager.go:323
func (m *Manager) patchProtocolHandlers(h *Handlers, item *Item) {
    if h == nil {
        return  // ← This is why nil currently causes silent no-op
    }
    // ... wraps h.DownloadProgressHandler to also update item.Downloaded
    // ... wraps h.DownloadCompleteHandler to set item.Downloaded = item.TotalSize
}
```

### Correct Reference: CLI Path (api/download.go)
```go
// Source: internal/api/download.go:downloadProtocolHandler
// This is the CORRECT pattern that the RPC path must mirror:
handlers := &warplib.Handlers{
    ErrorHandler: func(_ string, err error) { ... pool.Broadcast ... },
    DownloadProgressHandler: func(hash string, nread int) { ... pool.Broadcast ... },
    DownloadCompleteHandler: func(hash string, tread int64) { ... pool.Broadcast ... },
    DownloadStoppedHandler: func() { ... pool.Broadcast ... },
}
// ...
err = s.manager.AddProtocolDownload(pd, probe, cleanURL, proto, handlers, &warplib.AddDownloadOpts{...})
// ...
go pd.Download(context.Background(), handlers)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| HTTP-only download.add | Protocol-dispatched download.add | Phase 5 | FTP/SFTP handled in `default:` branch |
| No handler wiring for FTP/SFTP via RPC | After Phase 8: same wiring as HTTP path | Phase 8 | WebSocket notifications and item progress work for all protocols |
| nil handlers passed to AddProtocolDownload | opts.Handlers passed (post-fix) | Phase 8 | patchProtocolHandlers no longer silently no-ops |

## Open Questions

1. **Should `download.started` notification include the full probe.FileName for FTP/SFTP?**
   - What we know: Current HTTP path uses `d.GetFileName()`. FTP/SFTP path uses `probe.FileName` in the `download.started` broadcast (lines 253-257 in rpc_methods.go — already correct for that part).
   - What's unclear: Whether probe.FileName is always populated for FTP/SFTP (it is, per Phase 3/4 implementation).
   - Recommendation: No change needed. The existing `download.started` broadcast at line 253 is correct and unaffected by the fix.

2. **Should the mock ProtocolDownloader block until handler calls are verified?**
   - What we know: `pd.Download` is called in a goroutine. The test must synchronize.
   - What's unclear: Best synchronization primitive — channel, atomic poll, or WaitGroup.
   - Recommendation: Use a buffered channel with a deadline in the mock's `Download` to signal completion. The test polls `item.GetDownloaded()` with a 1-second deadline (same pattern as existing tests like `TestRPCDownloadAdd_Success`).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` package |
| Config file | none (project uses `go test`) |
| Quick run command | `go test -run TestRPCDownloadAdd_FTP ./internal/server/` |
| Full suite command | `go test ./internal/server/...` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| RPC-05 | FTP/SFTP item.Downloaded updated via RPC download.add | unit | `go test -run TestRPCDownloadAdd_FTP_ProgressTracked ./internal/server/ -x` | ❌ Wave 0 |
| RPC-11 | WebSocket push notifications delivered for FTP/SFTP download.add | unit | `go test -run TestRPCDownloadAdd_FTP_NotifierCalled ./internal/server/ -x` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test -run TestRPCDownloadAdd_FTP ./internal/server/ -race`
- **Per wave merge:** `go test ./internal/server/... -race`
- **Phase gate:** `go test ./... -race -short` (full suite green before verify)

### Wave 0 Gaps
- [ ] `internal/server/rpc_ftp_sftp_notify_test.go` — covers RPC-05 and RPC-11 with mock protocol downloader
- [ ] Mock `ProtocolDownloader` struct in that test file — implements `warplib.ProtocolDownloader`, calls handlers synchronously in `Download()`

*(Existing test infrastructure covers all other needs — no framework install needed)*

## Sources

### Primary (HIGH confidence)
- Direct codebase read: `internal/server/rpc_methods.go` — confirmed nil at lines 242 and 259 in `downloadAdd` default branch
- Direct codebase read: `pkg/warplib/manager.go:323` — confirmed `if h == nil { return }` in `patchProtocolHandlers`
- Direct codebase read: `internal/api/download.go:downloadProtocolHandler` — confirmed correct handler wiring pattern in CLI path
- Direct codebase read: `.planning/v1.0-MILESTONE-AUDIT.md` — INT-01 defect description with exact line numbers
- `go test -cover ./internal/server/...` — confirmed current coverage is 86.0%, passing

### Secondary (MEDIUM confidence)
- `pkg/warplib/protocol_test.go` — mock ProtocolDownloader pattern used in Phase 2 tests (same package, not accessible from server tests directly)
- `internal/server/rpc_resume_notify_test.go` — handler wiring test pattern for download.resume (Phase 6 fix)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all code is in-repo, no external dependencies needed
- Architecture: HIGH — defect and fix are fully characterized; patterns established in Phase 3-6 are directly applicable
- Pitfalls: HIGH — race conditions and mock correctness pitfalls verified by reading existing test patterns and the patchProtocolHandlers nil guard

**Research date:** 2026-02-27
**Valid until:** Stable — this is a local codebase fix with no external API dependencies
