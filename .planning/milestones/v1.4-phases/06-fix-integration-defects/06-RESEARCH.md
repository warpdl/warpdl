# Phase 6: Fix Integration Defects - Research

**Researched:** 2026-02-27
**Domain:** Go defect repair — SFTP resume state persistence, RPC push notification wiring, HTTP redirect policy enforcement
**Confidence:** HIGH — all defects located precisely in source, fix patterns are unambiguous

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| SFTP-04 | User can specify custom SSH key path via `--ssh-key` flag | Defect: `Item.SSHKeyPath` field missing; `ResumeDownloadOpts.SSHKeyPath` missing; fix is add field to both and thread through resume path |
| SFTP-06 | User can resume interrupted SFTP downloads via Seek offset | Defect: resume works mechanically but custom key is lost; fix is same as SFTP-04 — key is carried through persistence |
| RPC-06 | `download.pause` and `download.resume` methods control active downloads | Defect: `downloadResume` passes `nil` opts; fix is build `ResumeDownloadOpts` with notifier handlers just like `downloadAdd` does |
| RPC-11 | WebSocket pushes real-time notifications (download.started, download.progress, download.complete, download.error) | Defect: no `rs.notifier.Broadcast` handlers attached post-resume; same fix as RPC-06 |
| REDIR-04 | Authorization headers are not leaked across cross-origin redirects (CVE-2024-45336 regression guard) | Defect: `web.go processDownload` creates `http.Client{}` with no `CheckRedirect`; fix is set `CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects)` |
</phase_requirements>

---

## Summary

Phase 6 fixes exactly 3 integration defects identified by the v1.0 milestone audit. All defects are precise, located to specific lines, and have straightforward fixes. No architectural changes needed — each fix is a surgical addition of 1–10 lines of code in the right place.

**Defect 1 (SFTP-04, SFTP-06):** `Item` struct has no `SSHKeyPath` field. When an SFTP download started with `--ssh-key /path/to/key` is paused and resumed, the key path is not persisted in the GOB-encoded `Item`. Resume calls `schemeRouter.NewDownloader` with empty `SSHKeyPath`, which falls back to default keys (`~/.ssh/id_ed25519`, `~/.ssh/id_rsa`). Fix: add `SSHKeyPath string` to `Item` struct, populate it in `AddProtocolDownload`, and thread it through `ResumeDownloadOpts` + the SFTP resume dispatch in `manager.go`.

**Defect 2 (RPC-06, RPC-11):** `downloadResume` in `internal/server/rpc_methods.go` calls `rs.manager.ResumeDownload(rs.client, p.GID, nil)` with `nil` opts. This means `opts.Handlers` is nil going into `patchProtocolHandlers`, which is a no-op when `h == nil`. So no `rs.notifier.Broadcast` closures are attached. Fix: build `ResumeDownloadOpts` with `Handlers` containing `rs.notifier.Broadcast` callbacks — the same pattern used in `downloadAdd`.

**Defect 3 (REDIR-04):** `processDownload` in `internal/server/web.go` creates `http.Client{Jar: jar}` with no `CheckRedirect`. The fallback in `NewDownloader` (line 222–224 of dloader.go) catches it for that HTTP call, but the client is reused and the policy is set on first use — violating the explicit-set policy established in Phase 1 and creating a race condition risk in tests. Fix: add `CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects)` to the `http.Client{}` literal at `web.go:56`.

**Primary recommendation:** Fix the three defects in this order — SFTP key persistence (touches the most files but is mechanical), RPC resume notifications (touches 1 file, straightforward), web.go redirect policy (1-line fix). Write tests first (TDD red-green-refactor).

---

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `pkg/warplib` (internal) | current | Item persistence, manager, DownloaderOpts | Already the data layer — fields added here automatically GOB-serialize |
| `internal/server` (internal) | current | RPC methods, web handler | Defect 2 and 3 live here |
| `go test` + `httptest` | Go stdlib | Unit tests for all three fixes | Already established test pattern across all packages |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `encoding/gob` | Go stdlib | Item serialization/deserialization | GOB behavior must be verified for `Item.SSHKeyPath` field addition |
| `github.com/creachadair/jrpc2` | v1.3.4 | JSON-RPC 2.0 server/client | Already wired; RPC fix uses existing `RPCNotifier.Broadcast` pattern |
| `github.com/coder/websocket` | v1.8.14 | WebSocket transport | Already wired; no changes needed |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Persisting `SSHKeyPath` in `Item` | Re-prompting user for key on resume | Breaks the resume UX completely; users expect resume to be transparent |
| Setting `CheckRedirect` in `processDownload` | Relying on NewDownloader fallback | Fallback creates a race window and violates explicit-set policy |

---

## Architecture Patterns

### Pattern 1: Adding a Field to Item (GOB-safe)

**What:** Go's `encoding/gob` handles new fields with zero-value gracefully. A new `SSHKeyPath string` field on `Item` with a `json:"ssh_key_path,omitempty"` tag and no explicit GOB tag will be transparently backward-compatible: old GOB files decode fine (field stays empty string = use default keys), new files include the key path.

**Verified:** The existing `Protocol Protocol` field was added this way in Phase 2, confirmed by `TestGOBBackwardCompatProtocol` and the golden fixture at `pkg/warplib/testdata/pre_phase2_userdata.warp`. Same pattern applies.

**When to use:** Any new field added to `Item` that should survive restart/resume cycles.

**Example:**

```go
// In pkg/warplib/item.go — Item struct
type Item struct {
    // ... existing fields ...
    Protocol Protocol `json:"protocol"`

    // SSHKeyPath is the path to the SSH private key used for SFTP downloads.
    // Persisted so resume uses the same key as the initial download.
    // Empty means default key paths (~/.ssh/id_ed25519, ~/.ssh/id_rsa) are tried.
    SSHKeyPath string `json:"ssh_key_path,omitempty"`

    // ... unexported fields ...
}
```

### Pattern 2: Threading SSHKeyPath Through AddProtocolDownload

**What:** `Manager.AddProtocolDownload` receives `*DownloaderOpts` which already has `SSHKeyPath`. It currently creates the `Item` but does not copy `SSHKeyPath` into it. The fix is:

```go
// In pkg/warplib/manager.go, AddProtocolDownload, after item is created
item.Protocol = proto
item.SSHKeyPath = opts.SSHKeyPath  // ADD THIS LINE
```

Then in `ResumeDownload` (the FTP/FTPS/SFTP branch at line ~593):

```go
pd, err = m.schemeRouter.NewDownloader(item.Url, &DownloaderOpts{
    FileName:          item.Name,
    DownloadDirectory: item.DownloadLocation,
    SSHKeyPath:        item.SSHKeyPath,  // ADD THIS LINE
})
```

### Pattern 3: RPC Resume Notification Wiring (mirrors downloadAdd)

**What:** `downloadResume` must build `ResumeDownloadOpts.Handlers` the same way `downloadAdd` builds `opts.Handlers` — with closures that call `rs.notifier.Broadcast`. Additionally, `ResumeDownloadOpts` needs an `SSHKeyPath` field to carry the SFTP key from the `Item` (but Item already has it, so `ResumeDownload` can read it from the item directly — see Pattern 2 fix).

**Current broken code (rpc_methods.go:282):**
```go
resumedItem, err := rs.manager.ResumeDownload(rs.client, p.GID, nil)
```

**Fixed code:**
```go
var resumeOpts *warplib.ResumeDownloadOpts
if rs.notifier != nil {
    resumeOpts = &warplib.ResumeDownloadOpts{
        Handlers: &warplib.Handlers{
            ErrorHandler: func(hash string, err error) {
                rs.notifier.Broadcast("download.error", &DownloadErrorNotification{
                    GID:   hash,
                    Error: err.Error(),
                })
            },
            DownloadProgressHandler: func(hash string, nread int) {
                rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{
                    GID:             hash,
                    CompletedLength: int64(nread),
                })
            },
            DownloadCompleteHandler: func(hash string, tread int64) {
                rs.notifier.Broadcast("download.complete", &DownloadCompleteNotification{
                    GID:         hash,
                    TotalLength: tread,
                })
            },
        },
    }
}
resumedItem, err := rs.manager.ResumeDownload(rs.client, p.GID, resumeOpts)
```

Also broadcast `download.started` after resume succeeds (mirrors the `downloadAdd` pattern):
```go
if rs.notifier != nil {
    rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
        GID:      resumedItem.Hash,
        FileName: resumedItem.Name,
    })
}
```

### Pattern 4: web.go CheckRedirect Fix (1-line change)

**What:** Apply `RedirectPolicy` explicitly at client construction time, matching the pattern used everywhere else in the codebase (daemon_core.go:111, cmd/info.go:52, rpc_methods_test.go:232, rpc_integration_test.go:51).

**Current broken code (web.go:56):**
```go
client := &http.Client{
    Jar: jar,
}
```

**Fixed code:**
```go
client := &http.Client{
    Jar:           jar,
    CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
}
```

**Source verification:** `cmd/daemon_core.go:111` and `cmd/info.go:52` both use this exact pattern. `dloader.go:222-224` also sets it as fallback, but explicit-first is the established policy.

### Anti-Patterns to Avoid

- **Adding `SSHKeyPath` to `ResumeDownloadOpts` instead of reading from `Item`:** The key is already on the `Item` after Pattern 2 fix. `ResumeDownloadOpts` doesn't need it — `ResumeDownload` reads `item.SSHKeyPath` directly. Do NOT add a redundant field to `ResumeDownloadOpts`.
- **Calling `rs.notifier.Broadcast("download.started")` before `rs.manager.ResumeDownload` succeeds:** Always broadcast after success to avoid phantom notifications for failed resumes.
- **Using `http.DefaultClient` in `processDownload`:** The `processDownload` function builds its own client for cookie isolation. Must not use global client.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| GOB backward compat for new Item field | Custom migration/version scheme | Go's built-in gob zero-value handling | Already proven by Phase 2 Protocol field addition |
| Push notification mechanism for RPC resume | New broadcast infrastructure | Existing `RPCNotifier.Broadcast` already in `rs.notifier` | Fully wired in `handleJSONRPCWebSocket`; just need to pass it through opts |
| Custom redirect enforcement in web.go | Another layer of redirect checking | `warplib.RedirectPolicy` already exported | Identical pattern in 4 other call sites |

---

## Common Pitfalls

### Pitfall 1: Forgetting `SSHKeyPath` in `newItem()` Call

**What goes wrong:** `AddProtocolDownload` calls `newItem(...)` and then sets fields. If `SSHKeyPath` is added to `Item` but not set after `newItem()`, it stays empty.

**Why it happens:** `newItem()` only accepts a fixed set of primitive parameters. Protocol was set as a separate assignment (`item.Protocol = proto`) after `newItem()`. `SSHKeyPath` must follow the same post-creation pattern.

**How to avoid:** Add `item.SSHKeyPath = opts.SSHKeyPath` immediately after `item.Protocol = proto` in `AddProtocolDownload`.

**Warning signs:** Test `TestSFTPResumePreservesCustomKey` (to be written) will catch this.

### Pitfall 2: ResumeDownloadOpts Handlers Nil-Check in patchProtocolHandlers

**What goes wrong:** `patchProtocolHandlers` has an early return `if h == nil { return }`. If `ResumeDownloadOpts.Handlers` is nil (because `rs.notifier == nil`), the protocol handlers won't be wired for item state updates either.

**Why it happens:** The current code passes `nil` opts, which triggers the nil-guard at `manager.go:605`. When `rs.notifier == nil` and we want to pass nil handlers, we must still ensure `patchProtocolHandlers` gets non-nil `Handlers` for item state tracking (the `m.UpdateItem(item)` calls inside).

**How to avoid:** When building `resumeOpts`, even if `rs.notifier == nil`, still pass `Handlers: &warplib.Handlers{}` to ensure item state patches work. The handlers only call `rs.notifier.Broadcast` if `rs.notifier != nil`, so no nil dereference.

**Actually:** Looking at the code more carefully — `patchProtocolHandlers` WRAPS existing handlers by mutating them in-place. If `opts.Handlers` is nil at line 605, the code does `opts.Handlers = &Handlers{}` first. So passing `Handlers: nil` within a non-nil `ResumeDownloadOpts` is safe — the manager nil-initializes it. The broadcast closures need to be set in `opts.Handlers` before passing to `ResumeDownload`, so the wrapping includes them.

**Warning signs:** Test that verifies progress event fires after resume via notifier mock.

### Pitfall 3: GOB Round-Trip Test for SSHKeyPath

**What goes wrong:** `SSHKeyPath` is a plain string field — GOB encodes it cleanly. But if the test fixture `pre_phase2_userdata.warp` is decoded and `SSHKeyPath` is zero-value (empty), that's correct behavior. Do not regenerate the fixture.

**How to avoid:** Add a new test that explicitly writes an `Item` with `SSHKeyPath = "/custom/key"`, encodes/decodes, and checks the value survives. Don't touch `pre_phase2_userdata.warp`.

### Pitfall 4: RPC Resume Notifier Race

**What goes wrong:** The notifier broadcast closures capture `rs.notifier` by reference. If `rs.notifier` is nil, the closure still exists but `rs.notifier.Broadcast(...)` panics.

**How to avoid:** Guard with `if rs.notifier != nil` before building the `resumeOpts` (as shown in Pattern 3 above). Match the exact guard pattern from `downloadAdd`.

### Pitfall 5: `patchProtocolHandlers` Wrapping vs Setting

**What goes wrong:** `patchProtocolHandlers` WRAPS existing handlers — it reads the old handler, replaces it with a new closure that calls the old one. If `opts.Handlers.DownloadProgressHandler` is already set to the notifier broadcast closure when `patchProtocolHandlers` runs, the result is correct: item state update runs AND notifier fires.

**How to avoid:** Set the broadcast closures in `Handlers` before passing to `ResumeDownload`. `patchProtocolHandlers` will wrap them. Don't try to set handlers after `ResumeDownload` returns.

---

## Code Examples

Verified patterns from source:

### SSHKeyPath population in AddProtocolDownload (current code, showing where to insert)

```go
// pkg/warplib/manager.go — AddProtocolDownload function
// Source: confirmed by reading manager.go lines 284-304

item.Protocol = proto         // already here
item.SSHKeyPath = opts.SSHKeyPath  // ADD: persists key for resume

m.patchProtocolHandlers(handlers, item)
item.setDAlloc(pd)
m.UpdateItem(item)
```

### SSHKeyPath threading in ResumeDownload (SFTP branch, current lines ~593-598)

```go
// pkg/warplib/manager.go — ResumeDownload, FTP/FTPS/SFTP branch
// Source: confirmed by reading manager.go lines 592-598

pd, err = m.schemeRouter.NewDownloader(item.Url, &DownloaderOpts{
    FileName:          item.Name,
    DownloadDirectory: item.DownloadLocation,
    SSHKeyPath:        item.SSHKeyPath,  // ADD: was missing
})
```

### RPC resume with notifier (current downloadAdd pattern, adapted for resume)

```go
// internal/server/rpc_methods.go — downloadResume
// Source: confirmed by reading rpc_methods.go lines 169-191 (downloadAdd pattern)

var resumeOpts *warplib.ResumeDownloadOpts
if rs.notifier != nil {
    resumeOpts = &warplib.ResumeDownloadOpts{
        Handlers: &warplib.Handlers{
            ErrorHandler: func(hash string, err error) {
                rs.notifier.Broadcast("download.error", &DownloadErrorNotification{
                    GID: hash, Error: err.Error(),
                })
            },
            DownloadProgressHandler: func(hash string, nread int) {
                rs.notifier.Broadcast("download.progress", &DownloadProgressNotification{
                    GID: hash, CompletedLength: int64(nread),
                })
            },
            DownloadCompleteHandler: func(hash string, tread int64) {
                rs.notifier.Broadcast("download.complete", &DownloadCompleteNotification{
                    GID: hash, TotalLength: tread,
                })
            },
        },
    }
}
resumedItem, err := rs.manager.ResumeDownload(rs.client, p.GID, resumeOpts)
if err != nil {
    return nil, &jrpc2.Error{Code: codeDownloadNotActive, Message: err.Error()}
}
if rs.notifier != nil {
    rs.notifier.Broadcast("download.started", &DownloadStartedNotification{
        GID: resumedItem.Hash, FileName: resumedItem.Name,
    })
}
```

### web.go CheckRedirect fix (1-line change)

```go
// internal/server/web.go:56 — processDownload
// Source: confirmed by reading web.go lines 56-58 and comparing to daemon_core.go:111

client := &http.Client{
    Jar:           jar,
    CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
}
```

---

## File-by-File Change Map

| File | Change | Defect |
|------|--------|--------|
| `pkg/warplib/item.go` | Add `SSHKeyPath string` field to `Item` struct | SFTP-04, SFTP-06 |
| `pkg/warplib/manager.go` | (1) Set `item.SSHKeyPath = opts.SSHKeyPath` in `AddProtocolDownload`; (2) Pass `SSHKeyPath: item.SSHKeyPath` in `ResumeDownload` SFTP branch | SFTP-04, SFTP-06 |
| `internal/server/rpc_methods.go` | Build `ResumeDownloadOpts` with notifier handlers; broadcast `download.started` on success | RPC-06, RPC-11 |
| `internal/server/web.go` | Add `CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects)` to `http.Client{}` literal | REDIR-04 |

New test files:

| File | Tests | Covers |
|------|-------|--------|
| `pkg/warplib/sftp_resume_key_test.go` | `TestSFTPResumePreservesCustomSSHKey`, `TestSFTPResumeDefaultKeyWhenNone`, `TestSSHKeyPathGOBRoundTrip` | SFTP-04, SFTP-06 |
| `internal/server/rpc_resume_notify_test.go` | `TestRPCDownloadResume_PushNotifications`, `TestRPCDownloadResume_NilNotifier` | RPC-06, RPC-11 |
| `internal/server/web_redirect_test.go` | `TestProcessDownload_CheckRedirectSet`, `TestProcessDownload_CheckRedirectApplied` | REDIR-04 |

---

## State of the Art

| Old Approach | Current Approach | Notes |
|--------------|------------------|-------|
| `Item.SSHKeyPath` missing | Add field, persist via GOB zero-value safety | Matches how `Protocol` field was added in Phase 2 |
| Resume passes `nil` opts | Pass `ResumeDownloadOpts` with notifier handlers | Matches how `downloadAdd` wires notifiers |
| `http.Client{}` with no redirect policy | Explicit `CheckRedirect` at construction | Matches daemon_core.go, cmd/info.go, and test fixtures |

---

## Open Questions

1. **Should `ResumeDownloadOpts` also get an `SSHKeyPath` field for CLI resume override?**
   - What we know: Currently no CLI flag allows specifying a different key at resume time. The `warpcli.ResumeOpts` struct and `common.ResumeParams` both lack `SSHKeyPath`.
   - What's unclear: Is there a user story for "resume with a different key"? Probably not — the resume should use the same key as the original.
   - Recommendation: Do NOT add `SSHKeyPath` to `ResumeDownloadOpts`. Read it from `item.SSHKeyPath` in the manager. Keep the API surface minimal. Phase 6 is defect-fix only, not enhancement.

2. **Does `patchProtocolHandlers` need the Handlers set before or after the SFTP DownloaderOpts creation?**
   - What we know: Looking at the code flow in `ResumeDownload` SFTP branch (lines 592-610): `NewDownloader` is called first (creates the protocol downloader), then `patchProtocolHandlers(opts.Handlers, item)` wraps the handlers. The handlers passed to `NewDownloader` via `DownloaderOpts` are the SAME `opts.Handlers` pointer, since `patchProtocolHandlers` mutates in-place.
   - Verdict: The order is correct as-is. The `NewDownloader` call passes `DownloaderOpts` which does NOT include the handlers (only `FileName`, `DownloadDirectory`, `SSHKeyPath`). The handlers are patched separately via `opts.Handlers`. RPC resume must set handlers in `ResumeDownloadOpts.Handlers` before calling `ResumeDownload`.

3. **Should `download.started` notification be sent on RPC resume, or just `download.progress`/`download.complete`?**
   - What we know: `downloadAdd` sends `download.started` with `GID`, `FileName`, `TotalLength`. Resume is semantically a restart. Sending `download.started` on resume lets clients know the download is active again.
   - Recommendation: Send `download.started` on successful resume, matching `downloadAdd`. The client can track state and handle duplicate `download.started` events gracefully.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | `go test` stdlib |
| Config file | none (build tags only) |
| Quick run command | `go test -short ./pkg/warplib/... ./internal/server/...` |
| Full suite command | `go test -race -short ./...` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SFTP-04 | `Item.SSHKeyPath` persisted; resume uses it for `NewDownloader` | unit | `go test -run TestSFTPResume ./pkg/warplib/...` | No — Wave 0 |
| SFTP-06 | Resume SFTP with custom key uses Seek offset AND correct key | unit | `go test -run TestSFTPResume ./pkg/warplib/...` | No — Wave 0 |
| RPC-06 | `download.resume` RPC wires progress/complete/error handlers | unit | `go test -run TestRPCDownloadResume ./internal/server/...` | Partial (only NotFound test) |
| RPC-11 | WebSocket clients receive push notifications after resume | unit | `go test -run TestRPCDownloadResume ./internal/server/...` | No — Wave 0 |
| REDIR-04 | `processDownload` http.Client has CheckRedirect set | unit | `go test -run TestProcessDownload ./internal/server/...` | No — Wave 0 |

### Sampling Rate

- **Per task commit:** `go test -short ./pkg/warplib/... ./internal/server/...`
- **Per wave merge:** `go test -race -short ./...`
- **Phase gate:** Full suite green with race detection before proceeding

### Wave 0 Gaps

- [ ] `pkg/warplib/sftp_resume_key_test.go` — covers SFTP-04 and SFTP-06
- [ ] `internal/server/rpc_resume_notify_test.go` — covers RPC-06 and RPC-11
- [ ] `internal/server/web_redirect_test.go` — covers REDIR-04

*(No new framework or shared fixtures needed — existing test helpers in each package suffice)*

---

## Sources

### Primary (HIGH confidence)

- Source code direct inspection: `pkg/warplib/manager.go` (lines 592-648), `pkg/warplib/item.go`, `pkg/warplib/dloader.go` (lines 117-183, 215-224), `internal/server/rpc_methods.go` (lines 144-291), `internal/server/web.go` (lines 47-143), `pkg/warplib/redirect.go`
- `.planning/v1.0-MILESTONE-AUDIT.md` — audit confirmed all 3 defect locations with exact file:line references
- `go test -short ./pkg/warplib/... ./internal/server/...` — confirmed all existing tests pass as baseline

### Secondary (MEDIUM confidence)

- `pkg/warplib/manager_resume_test.go` — existing resume test patterns establish test structure conventions
- `internal/server/rpc_methods_test.go` — existing RPC test helpers (`newTestRPCHandlerWithManager`, `rpcCall`, `rpcError`) available for new tests
- `.planning/STATE.md` accumulated decisions — confirms GOB safety pattern from Phase 2, SSHKeyPath threading decision from Phase 04-03

### Tertiary (LOW confidence)

- None — all findings verified against source.

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new dependencies; all changes use existing packages
- Architecture: HIGH — fix patterns are mechanical; verified against existing parallel implementations in the codebase
- Pitfalls: HIGH — identified by tracing exact execution paths through code

**Research date:** 2026-02-27
**Valid until:** 2026-03-27 (stable Go codebase; no fast-moving external dependencies)
