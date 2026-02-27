# Phase 9: Fix RPC FTP/SFTP Resume Handler Pass-Through - Research

**Researched:** 2026-02-27
**Domain:** Go — warplib item lifecycle, protocol downloader interface, RPC handler wiring
**Confidence:** HIGH

## Summary

Phase 9 closes INT-02 from the v1.0 audit: FTP/SFTP downloads resumed via JSON-RPC `download.resume` silently drop all WebSocket push notifications because `Item.Resume()` passes `nil` handlers to `ProtocolDownloader.Resume()`.

The defect is a single-site nil-pass in `pkg/warplib/item.go:283`. The handler flow is:
1. `downloadResume` in `rpc_methods.go` builds `resumeOpts.Handlers` with notifier closures (lines 288-310).
2. `manager.ResumeDownload` calls `patchProtocolHandlers(opts.Handlers, item)` which wraps the handlers in-place and stores them in the `*Handlers` struct.
3. `manager.ResumeDownload` sets `item.dAlloc` to the new `ProtocolDownloader`.
4. `downloadResume` then calls `go resumedItem.Resume()` (line 329).
5. `Item.Resume()` calls `d.Resume(context.Background(), partsCopy, nil)` — the third argument is **nil**.
6. FTP/SFTP `Resume` methods receive `nil` for `handlers` and use it directly for all callbacks, so no notifications fire.

The HTTP path is unaffected because `patchHandlers` installs callbacks as struct fields on `*Downloader` — those survive as long as the `*Downloader` lives, regardless of what `ProtocolDownloader.Resume` receives as `handlers`. FTP/SFTP protocol downloaders do not store handlers on the struct; they use the `handlers` parameter passed to `Resume` directly.

**Primary recommendation:** Modify `Item.Resume()` to accept an optional `*warplib.Handlers` parameter and pass it through to `ProtocolDownloader.Resume()`, falling back to `nil` when not provided. The callers that do not need handlers (CLI path via `api/resume.go`) continue passing `nil`; the RPC path stores the patched handlers somewhere accessible and passes them through. The cleanest approach is to store the patched handlers pointer on `Item` itself after `patchProtocolHandlers` completes.

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| RPC-06 | `download.pause` and `download.resume` control active downloads | The resume path needs to call `item.Resume()` with handlers so progress/complete/error events reach WebSocket clients |
| RPC-11 | WebSocket pushes real-time notifications (download.started, download.progress, download.complete, download.error) | FTP/SFTP `Resume` methods use the `handlers` parameter directly — passing non-nil handlers is the only way to deliver notifications |
</phase_requirements>

---

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib | 1.24.9+ | All code — no external deps for this fix | No external libraries needed; this is a pure Go wiring fix |
| `pkg/warplib` | project-local | Item, Handlers, ProtocolDownloader types | These are the types being fixed |
| `internal/server` | project-local | RPCServer, downloadResume, mockProtocolDownloader | Test file already exists (rpc_ftp_sftp_notify_test.go) for reference |

### No New Dependencies

This phase requires zero new imports. The fix touches existing files only.

## Architecture Patterns

### Current Flow (BROKEN for FTP/SFTP via RPC)

```
downloadResume (rpc_methods.go:280-329)
  → builds resumeOpts.Handlers with notifier closures
  → manager.ResumeDownload(rs.client, p.GID, resumeOpts)
      → patchProtocolHandlers(opts.Handlers, item)  ← wraps handlers in-place
      → item.setDAlloc(pd)                          ← stores protocol downloader
  → go resumedItem.Resume()                         ← ITEM method
      → d.Resume(ctx, partsCopy, nil)               ← nil loses the handlers!
          → ftpProtocolDownloader.Resume(ctx, parts, nil)  ← nil: no notifications
```

### Fixed Flow (Target)

```
downloadResume (rpc_methods.go:280-329)
  → builds resumeOpts.Handlers with notifier closures
  → manager.ResumeDownload(rs.client, p.GID, resumeOpts)
      → patchProtocolHandlers(opts.Handlers, item)  ← wraps handlers in-place
      → item.setResumeHandlers(opts.Handlers)       ← NEW: store for Resume
      → item.setDAlloc(pd)
  → go resumedItem.Resume()                         ← ITEM method
      → d.Resume(ctx, partsCopy, item.resumeHandlers)  ← passes stored handlers
          → ftpProtocolDownloader.Resume(ctx, parts, h)  ← h != nil: notifications fire
```

### Two Viable Fix Approaches

#### Approach A: Store handlers on Item (RECOMMENDED)

Add a `resumeHandlers *Handlers` field to `Item`. After `patchProtocolHandlers` in `ResumeDownload`, call `item.setResumeHandlers(opts.Handlers)`. `Item.Resume()` reads this field and passes it to `d.Resume()`.

**Pros:**
- Zero signature change to `Item.Resume()` — no callers need updating
- Thread-safe with existing `dAllocMu` mutex pattern
- Handlers survive the goroutine boundary (set before `go resumedItem.Resume()`)
- HTTP path unaffected: `patchHandlers` doesn't call `setResumeHandlers`, so `resumeHandlers` stays nil, `d.Resume(ctx, parts, nil)` for HTTP is correct (HTTP `Downloader.Resume` ignores the handlers parameter entirely)
- Consistent with how `SSHKeyPath` was added to Item for similar persistence needs

**Cons:**
- Adds one field to Item (GOB-serialized struct). Must use `omitempty`-equivalent to avoid breaking GOB backward compatibility. Handlers are NOT serialized (func values aren't GOB-able), so the field must be declared with a non-serializable type — this works fine because GOB skips unexported fields and func fields aren't registered.

**GOB safety:** `Handlers` contains only func fields. GOB ignores func fields — they cannot be encoded. The field should be unexported (`resumeHandlers`) and not be part of the GOB-encoded Item struct. Since `Item` uses json tags for its exported fields and GOB encodes all exported fields, `resumeHandlers` MUST be unexported to avoid GOB attempting to encode it. This is safe — unexported fields are always skipped by GOB.

#### Approach B: Add handlers parameter to Item.Resume()

Change `func (i *Item) Resume() error` to `func (i *Item) Resume(handlers *Handlers) error`. Update all callers.

**Current callers of Item.Resume():**
- `internal/api/resume.go:94` — `resumeItem(item)` → `item.Resume()` — this is the CLI path; pass `nil`
- `internal/server/rpc_methods.go:329` — `go resumedItem.Resume()` — RPC path; pass handlers

**Pros:** Explicit, no hidden state on Item.

**Cons:** API surface change. `resumeItem` helper in `internal/api/resume.go` would need updating. The Phase 8 pattern (for `download.add`) used passing handlers through function args, so this follows the same pattern as `pd.Download(ctx, handlers)`.

**Assessment:** Approach A is cleaner because it avoids propagating the signature change through the call chain. The CLI path (`resumeItem`) does not need handlers — the CLI uses `api/resume.go:getHandler` which wires directly via `ResumeDownloadOpts.Handlers`, and `patchHandlers` (HTTP path) installs callbacks on the `*Downloader` struct. The RPC path is the only path needing this fix.

**However**, Approach B (signature change) is more explicit and consistent with the `ProtocolDownloader.Resume(ctx, parts, handlers)` interface. Both approaches are acceptable. The planner should choose based on scope preference.

### Pattern 1: HTTP vs FTP/SFTP Handlers Asymmetry

This is the core architectural insight:

**HTTP path:** `patchHandlers(d *Downloader, item)` mutates `d.handlers` (a struct field on `*Downloader`). When `Item.Resume()` calls `d.Resume(ctx, parts, nil)`, the `nil` argument is ignored — `Downloader.Resume` uses `d.handlers` (the struct field), not the parameter. The `ProtocolDownloader.Resume` interface signature includes `handlers *Handlers` as parameter 3, but the HTTP adapter (`httpProtocolDownloader.Resume`) ignores it and delegates to `d.Resume(parts)` (the concrete `*Downloader.Resume` method, which only takes parts).

Wait — let me re-examine the actual HTTP adapter.

**Verification needed:** Does `httpProtocolDownloader.Resume` use the `handlers` parameter or the struct field?

Looking at `protocol_http.go` (not yet read). This is critical to the fix.

### Pattern 2: FTP/SFTP use handlers parameter directly

From `protocol_ftp.go:265` and confirmed in the code:
```go
func (d *ftpProtocolDownloader) Resume(ctx context.Context, parts map[int64]*ItemPart, handlers *Handlers) error {
    // ... uses handlers directly:
    pw := &ftpProgressWriter{handlers: handlers, hash: d.hash}
    // handlers.DownloadCompleteHandler(MAIN_HASH, d.fileSize)
}
```

All handler invocations inside FTP/SFTP `Resume` use the `handlers` parameter, not any stored struct field. Passing `nil` means zero notifications.

### Critical Discovery: httpProtocolDownloader.Resume USES the handlers parameter

From `protocol_http.go:69-77`:
```go
func (h *httpProtocolDownloader) Resume(_ context.Context, parts map[int64]*ItemPart, handlers *Handlers) error {
    if !h.probed || h.inner == nil {
        return ErrProbeRequired
    }
    if handlers != nil {
        h.inner.handlers = handlers  // ← REPLACES struct field when non-nil
    }
    return h.inner.Resume(parts)
}
```

This means `httpProtocolDownloader.Resume` will **replace** `h.inner.handlers` if a non-nil handlers pointer is passed. The current behavior (passing nil) preserves the handlers installed by `patchHandlers`, which is correct. If we change `Item.Resume()` to pass a non-nil handlers pointer for HTTP items, it would bypass `patchHandlers` — breaking item state updates.

**Implication for Approach A:** `setResumeHandlers` must only be called for FTP/FTPS/SFTP items. HTTP items must keep `resumeHandlers = nil` so `Item.Resume()` continues passing nil to `httpProtocolDownloader.Resume`. This is naturally achieved by only calling `setResumeHandlers` in the FTP/SFTP branch of `ResumeDownload`.

**Implication for Approach B (signature change):** The RPC caller must pass nil for HTTP items and the actual handlers only for FTP/SFTP items. But `Item.Resume()` doesn't know the protocol — it would need to check `i.Protocol` to decide what to pass. This makes Approach B more complex.

**Updated recommendation: Approach A is definitively better** because it stores handlers only when set (FTP/SFTP branch), and the HTTP path never sets `resumeHandlers`, so nil is passed to `httpProtocolDownloader.Resume` preserving the existing behavior.

### Anti-Patterns to Avoid

- **Storing handlers as a named field in GOB-exported Item struct:** func values cannot be GOB-encoded; this would cause a panic at encode time. Must use unexported field.
- **Calling `patchProtocolHandlers` again inside `Item.Resume()`:** Double-patching wraps the handlers twice, creating infinite recursion in the closures.
- **Constructing new handlers inside `Item.Resume()`:** `Item` has no access to the notifier — it's a warplib type with no knowledge of the server layer.
- **Passing non-nil handlers to httpProtocolDownloader.Resume:** It replaces `h.inner.handlers` with the provided value, bypassing `patchHandlers` wrapping and breaking item state updates (Downloaded counter, Parts map).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Handler storage on Item | New mutex, sync map | Reuse `dAllocMu` or add simple unexported field | One field, same lock pattern as SSHKeyPath persistence |
| Handler thread safety | Custom atomic wrapper | Standard Go unexported field + nil-check | Handlers set before goroutine launch; happens-before guarantee |

## Common Pitfalls

### Pitfall 1: GOB Encoding of Handlers

**What goes wrong:** Adding a `Handlers` or `*Handlers` field to `Item` with an exported name causes GOB to attempt encoding. GOB cannot encode func values and will panic at runtime.

**Why it happens:** GOB encodes all exported fields of a struct. `Handlers` contains only func fields, which are unsupported.

**How to avoid:** Use an **unexported** field name (`resumeHandlers *Handlers`). GOB skips unexported fields entirely. This is safe and the established pattern in this codebase (see `mu`, `dAllocMu`, `dAlloc`, `memPart` — all unexported).

**Warning signs:** `gob: type not supported: func` panic at runtime, or compilation error if you try to register the func type.

### Pitfall 2: HTTP Path Regression

**What goes wrong:** If `Item.Resume()` passes `resumeHandlers` to `d.Resume()` for HTTP items, and `httpProtocolDownloader.Resume` also uses the handlers parameter, handlers could be applied twice (both through `d.handlers` struct field AND through the parameter).

**Why it happens:** HTTP path uses `patchHandlers` which mutates `d.handlers` struct field AND optionally the parameter. Double-patching causes double progress counts.

**How to avoid:** Verify `httpProtocolDownloader.Resume` ignores the `handlers` parameter (delegates to concrete `*Downloader.Resume` which has no handlers parameter). If it does ignore the parameter, passing nil or non-nil makes no difference for HTTP. If it doesn't ignore it, must ensure HTTP items don't set `resumeHandlers`.

**Key check:** Read `protocol_http.go` to confirm `httpProtocolDownloader.Resume` signature.

### Pitfall 3: Nil resumeHandlers for HTTP items

**What goes wrong:** HTTP items never call `setResumeHandlers` (Approach A), so `item.resumeHandlers` stays nil. `Item.Resume()` passes nil to `d.Resume()` for HTTP. If `httpProtocolDownloader.Resume` uses the handlers parameter, this breaks HTTP.

**How to avoid:** Same as Pitfall 2. Verify HTTP adapter behavior before finalizing approach.

### Pitfall 4: Race between setResumeHandlers and Item.Resume goroutine

**What goes wrong:** `downloadResume` calls `setResumeHandlers` then launches `go resumedItem.Resume()`. If there's a happens-before violation, the goroutine could see `resumeHandlers = nil`.

**Why it doesn't happen:** Go's goroutine launch semantics guarantee a happens-before relationship between the code before `go ...` and the start of the goroutine. Any write before `go resumedItem.Resume()` is visible to the goroutine.

**How to avoid:** No action needed — standard Go memory model applies. No mutex needed for the set/read pair here.

### Pitfall 5: patchProtocolHandlers called twice

**What goes wrong:** If the fix calls `patchProtocolHandlers` inside `Item.Resume()` in addition to the existing call in `manager.ResumeDownload`, the handlers get wrapped twice. The `DownloadProgressHandler` closure calls `item.Downloaded += nread` and then calls the original handler which also calls `item.Downloaded += nread` — double counting.

**How to avoid:** Never call `patchProtocolHandlers` again. The fix only needs to pass the already-patched `*Handlers` pointer to `d.Resume()`.

## Code Examples

### The Defect (item.go:265-283 — current code)

```go
// Resume resumes the download of the item.
// Pass nil handlers — Manager.patchHandlers already installed them on the inner downloader.
func (i *Item) Resume() error {
    // Take snapshot of Parts under Item lock first
    i.mu.RLock()
    partsCopy := make(map[int64]*ItemPart, len(i.Parts))
    for k, v := range i.Parts {
        partsCopy[k] = v
    }
    i.mu.RUnlock()

    i.dAllocMu.RLock()
    d := i.dAlloc
    i.dAllocMu.RUnlock()

    if d == nil {
        return ErrItemDownloaderNotFound
    }
    // Pass nil handlers — Manager.patchHandlers already installed them on the inner downloader.
    return d.Resume(context.Background(), partsCopy, nil)
}
```

The comment is correct for HTTP but wrong for FTP/SFTP. HTTP `*Downloader` has handlers on the struct. FTP/SFTP use the parameter.

### Fix: Approach A (store on Item)

**Step 1: Add unexported field to Item struct in item.go**
```go
// resumeHandlers holds the patched handler callbacks for the resume path.
// Set by Manager.ResumeDownload after patchProtocolHandlers completes.
// Unexported to prevent GOB serialization (func values cannot be GOB-encoded).
// nil for HTTP items (handled via *Downloader.handlers struct field).
resumeHandlers *Handlers
```

**Step 2: Add accessor methods to item.go**
```go
// setResumeHandlers stores handlers for use during Resume.
func (i *Item) setResumeHandlers(h *Handlers) {
    i.dAllocMu.Lock()
    defer i.dAllocMu.Unlock()
    i.resumeHandlers = h
}

// getResumeHandlers returns stored resume handlers.
func (i *Item) getResumeHandlers() *Handlers {
    i.dAllocMu.RLock()
    defer i.dAllocMu.RUnlock()
    return i.resumeHandlers
}
```

**Step 3: Update Item.Resume() to use stored handlers**
```go
func (i *Item) Resume() error {
    i.mu.RLock()
    partsCopy := make(map[int64]*ItemPart, len(i.Parts))
    for k, v := range i.Parts {
        partsCopy[k] = v
    }
    i.mu.RUnlock()

    i.dAllocMu.RLock()
    d := i.dAlloc
    h := i.resumeHandlers
    i.dAllocMu.RUnlock()

    if d == nil {
        return ErrItemDownloaderNotFound
    }
    return d.Resume(context.Background(), partsCopy, h)
}
```

Note: `h` may be nil (for HTTP items or items resumed without notifier). FTP/SFTP `Resume` is nil-guarded on all handler calls (e.g., `if handlers != nil && handlers.DownloadProgressHandler != nil`), so nil is safe.

**Step 4: Call setResumeHandlers in manager.ResumeDownload after patchProtocolHandlers**

In `pkg/warplib/manager.go` inside `ResumeDownload`, after `patchProtocolHandlers`:
```go
// FTP/FTPS/SFTP resume path (lines 589-615):
m.patchProtocolHandlers(opts.Handlers, item)
item.setResumeHandlers(opts.Handlers)   // NEW: store for Item.Resume()
item.setDAlloc(pd)
m.UpdateItem(item)
```

### Fix: Approach B (signature change)

**Step 1: Change Item.Resume() signature**
```go
// Resume resumes the download of the item.
// handlers are passed through to ProtocolDownloader.Resume for FTP/SFTP protocols.
// Pass nil for HTTP items (handlers already installed on *Downloader struct).
func (i *Item) Resume(handlers *Handlers) error {
    // ... (same body but use handlers instead of nil)
    return d.Resume(context.Background(), partsCopy, handlers)
}
```

**Step 2: Update api/resume.go caller**
```go
// In internal/api/resume.go:94
func resumeItem(i *warplib.Item) error {
    if i.Downloaded >= i.TotalSize {
        return nil
    }
    return i.Resume(nil)  // CLI path: nil handlers (HTTP uses struct field)
}
```

**Step 3: Update rpc_methods.go caller**
```go
// In internal/server/rpc_methods.go:329
go resumedItem.Resume(resumeHandlers)  // RPC path: pass patched handlers
```

Where `resumeHandlers` is extracted from `resumeOpts.Handlers` after `ResumeDownload` returns.

**Problem with Approach B:** `resumeOpts.Handlers` was passed into `manager.ResumeDownload` and mutated in-place by `patchProtocolHandlers`. After `ResumeDownload` returns, `resumeOpts.Handlers` holds the patched handlers (the same pointer, now with wrapped closures). So the caller can pass `resumeOpts.Handlers` directly to `go resumedItem.Resume(resumeOpts.Handlers)`.

But `resumeOpts` is only set when `rs.notifier != nil`. If notifier is nil, `resumeOpts` is nil, and `resumeOpts.Handlers` would panic. Must nil-check.

### Critical: httpProtocolDownloader.Resume behavior

**Must verify:** Does `httpProtocolDownloader.Resume` use the `handlers *Handlers` parameter or ignore it?

Looking at the `protocol_http.go` file (referenced in the codebase). The `httpProtocolDownloader` wraps a concrete `*Downloader`. The `*Downloader.Resume` method (in `dloader.go:460`) does NOT take a `handlers` parameter — it uses `d.handlers` struct field. So `httpProtocolDownloader.Resume` likely ignores the `handlers` parameter and delegates to `d.Resume(parts)`.

This means for HTTP: passing nil or non-nil handlers in Approach A makes no difference. No regression risk.

## Open Questions

1. **RESOLVED: `httpProtocolDownloader.Resume` DOES use the handlers parameter.**
   - Confirmed from `protocol_http.go:73-75`: if `handlers != nil`, it replaces `h.inner.handlers`.
   - **Impact:** Approach A must NOT call `setResumeHandlers` for HTTP items. Only the FTP/FTPS/SFTP branch of `ResumeDownload` should call `setResumeHandlers`. HTTP items retain `resumeHandlers = nil`, so nil is passed to `httpProtocolDownloader.Resume`, preserving the `patchHandlers`-installed struct field.
   - **Confidence:** HIGH — code confirmed by direct inspection.

2. **Should `setResumeHandlers` be called for HTTP items too?**
   - Answer: NO. `httpProtocolDownloader.Resume` replaces `h.inner.handlers` when non-nil is passed. HTTP items use `patchHandlers` which wraps the handlers on the struct. Passing non-nil would bypass that wrapping.
   - Only call `setResumeHandlers` in the FTP/FTPS/SFTP branch of `ResumeDownload`.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | `testing` (Go stdlib) |
| Config file | none — standard `go test` |
| Quick run command | `go test -run "TestRPCResume.*FTP\|TestRPCResume.*SFTP\|TestItemResume" ./internal/server/... ./pkg/warplib/... -v -count=1` |
| Full suite command | `go test -race -short ./...` |
| Coverage command | `go test -cover ./internal/server/... ./pkg/warplib/...` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| RPC-06 | FTP/SFTP downloads resumed via RPC deliver progress/complete events | integration | `go test -run TestRPCResume_FTP_HandlersFired ./internal/server/ -v` | No — Wave 0 gap |
| RPC-11 | WebSocket push notifications emitted for FTP/SFTP resume | integration | `go test -run TestRPCResume_SFTP_HandlersFired ./internal/server/ -v` | No — Wave 0 gap |
| RPC-11 | HTTP resume path unaffected (no regression) | unit | `go test -run TestRPCDownloadResume ./internal/server/ -v` | Yes — rpc_resume_notify_test.go |

### Sampling Rate

- **Per task commit:** `go test -run "TestRPCResume" ./internal/server/ -race -count=1`
- **Per wave merge:** `go test -race -short ./...`
- **Phase gate:** `go test -race -short ./... && go test -cover ./internal/server/... && go vet ./...`

### Wave 0 Gaps

- [ ] `internal/server/rpc_ftp_sftp_resume_test.go` — new test file covering RPC-06 and RPC-11 for the resume path (mirrors `rpc_ftp_sftp_notify_test.go` pattern but for Resume, not Download)

Existing infrastructure that covers adjacent paths:
- `internal/server/rpc_ftp_sftp_notify_test.go` — tests for `download.add` FTP/SFTP (Phase 8 fix; reuse `mockProtocolDownloader` and `newTestRPCHandlerWithRouter`)
- `internal/server/rpc_resume_notify_test.go` — tests for HTTP `download.resume` handler wiring
- `pkg/warplib/item_test.go` — unit tests for `Item` methods

## Sources

### Primary (HIGH confidence)

- Direct code inspection: `/pkg/warplib/item.go:265-284` — `Item.Resume()` passes `nil` at line 283
- Direct code inspection: `/pkg/warplib/protocol_ftp.go:265-351` — `ftpProtocolDownloader.Resume` uses `handlers` parameter directly at lines 307, 342, 348
- Direct code inspection: `/pkg/warplib/manager.go:589-615` — `ResumeDownload` FTP/SFTP branch calls `patchProtocolHandlers(opts.Handlers, item)` then discards `opts.Handlers`
- Direct code inspection: `/internal/server/rpc_methods.go:280-329` — `downloadResume` builds handlers at 288-310, passes to `ResumeDownload` at 312, calls `go resumedItem.Resume()` at 329
- Audit document: `.planning/v1.0-MILESTONE-AUDIT.md:27-33` — INT-02 root cause analysis with exact line references
- Phase 8 plan: `.planning/phases/08-fix-rpc-ftp-sftp-handlers/08-01-PLAN.md` — exact same bug class, same fix pattern; shows how to write the test with `mockProtocolDownloader`

### Secondary (MEDIUM confidence)

- Go memory model: goroutine launch creates happens-before boundary, so writes before `go ...` are visible inside goroutine. No additional synchronization needed for the handlers field.
- GOB encoding rules: func values cannot be encoded; unexported fields are skipped. Verified by established pattern in codebase (item.go has multiple unexported fields: `mu`, `dAllocMu`, `dAlloc`, `memPart`).

## Metadata

**Confidence breakdown:**
- Root cause identification: HIGH — exact lines pinpointed by audit, confirmed by code inspection
- Fix approach (Approach A): HIGH — follows established Item field pattern; GOB-safe by using unexported field
- Fix approach (Approach B): HIGH — simpler but requires caller updates; both approaches viable
- HTTP regression risk: HIGH (no regression) — pending confirmation that `httpProtocolDownloader.Resume` ignores handlers parameter
- Test pattern: HIGH — exact same mock (`mockProtocolDownloader`) and helper (`newTestRPCHandlerWithRouter`) already exist from Phase 8

**Research date:** 2026-02-27
**Valid until:** This is purely code-level analysis of a stable codebase; valid indefinitely until code changes.
