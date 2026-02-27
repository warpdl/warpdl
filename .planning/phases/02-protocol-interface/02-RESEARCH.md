# Phase 2: Protocol Interface - Research

**Researched:** 2026-02-27
**Domain:** Go interface design, GOB backward compatibility, scheme routing in `pkg/warplib`
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Interface contract**
- Separate `Probe` method for metadata fetching (file size, content type, checksum headers) before download starts — manager calls Probe first, then Download
- Single `Downloader` interface with a `Capabilities()` method reporting whether the protocol supports parallel segments, resume, etc. — no separate ParallelDownloader/StreamDownloader interfaces
- Each protocol downloader manages its own connection lifecycle internally (create, reuse, close) — manager just calls Download/Close
- Shared handler/callback contract: all protocols accept the same handler callbacks (progress, completion, error) that HTTP uses today — manager and UI code stays protocol-agnostic

**Error semantics**
- Generic `DownloadError` wrapping protocol-specific errors — callers see uniform type but can `errors.Unwrap`/`errors.As` to get FTP/SFTP-specific details when needed
- Manager owns retry logic with exponential backoff, applies uniformly to all protocols — downloaders just report transient vs permanent errors
- Errors implement `IsTransient() bool` method — manager checks this to decide whether to retry; each protocol classifies its own errors (e.g., FTP 421 = transient, 530 = permanent)
- Authentication failures fail immediately with a clear "authentication failed" message — no interactive re-prompts mid-download (daemon architecture constraint)

**Registration & routing**
- Static scheme-to-downloader map built at startup (`map[string]DownloaderFactory`) — no dynamic registry, no self-registration
- Unsupported schemes fail immediately with error listing supported schemes: `unsupported scheme "magnet" — supported: http, https, ftp, ftps, sftp`
- `http://` and `https://` share the same HTTPDownloader; `ftp://` and `ftps://` share the same FTPDownloader — TLS handled at transport level, not downloader level
- Scheme router lives inside `pkg/warplib` as part of the manager package — not a separate package

**GOB migration strategy**
- New `Protocol` field added to `Item` struct — zero value (`0`) defaults to HTTP, no migration step needed
- Protocol field is a typed enum (`uint8` with iota constants): `ProtoHTTP = 0`, `ProtoFTP = 1`, `ProtoFTPS = 2`, `ProtoSFTP = 3` — zero value gives free backward compat
- Golden fixture test: capture a GOB-encoded `userdata.warp` from the current version as a test fixture, decode it, assert all items load with `Protocol == ProtoHTTP`
- Unknown protocol values (from newer versions) fail with clear error: `unknown protocol type 7 — upgrade warpdl` — no silent degradation

### Claude's Discretion
- Exact method signatures and parameter types on the Downloader interface
- Internal structure of the capability flags (bitfield vs struct vs method set)
- How the existing HTTP download code gets refactored to implement the new interface
- Test helper patterns for mock downloaders

### Deferred Ideas (OUT OF SCOPE)

None — discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| PROTO-01 | Download engine supports a protocol-agnostic downloader interface so FTP/SFTP can plug in alongside HTTP | Interface design pattern, capability flags approach, `net.Error` analogy for `IsTransient()` |
| PROTO-02 | Manager dispatches to correct downloader based on URL scheme (http/https/ftp/ftps/sftp) | Scheme router placement in `pkg/warplib`, `map[string]DownloaderFactory` pattern, `url.Parse` scheme extraction |
| PROTO-03 | Item persistence (GOB) supports protocol field with backward-compatible zero value defaulting to HTTP | GOB zero-value semantics, `uint8` iota enum, golden fixture test pattern |
</phase_requirements>

## Summary

Phase 2 is purely structural — it defines the abstraction seam that FTP (Phase 3) and SFTP (Phase 4) plug into. No new protocol implementations. The work is: define one Go interface, refactor the existing `Downloader` struct to implement it (or wrap it), build a scheme router inside `pkg/warplib`, and add a `Protocol` field to `Item` with a binary fixture test for GOB backward compat.

The existing codebase is well-structured for this surgery. The `Downloader` struct in `dloader.go` is HTTP-only but its public contract (constructor, `Start()`, `Resume()`, `Stop()`, `Close()`, progress handlers) maps cleanly onto what the interface needs. The `Manager` already passes `*Downloader` around — the refactor replaces that concrete type with the interface at the boundaries where new protocols need to plug in.

The GOB concern is minimal. Go's `encoding/gob` silently skips fields present in the stream but absent in the target struct (forward compat), and silently zero-initializes fields present in the target but absent in the stream (backward compat). Adding `Protocol uint8` to `Item` with `ProtoHTTP = 0` means all existing encoded files decode correctly with zero cost — no migration code needed. The golden fixture test locks this down.

**Primary recommendation:** Define `ProtocolDownloader` interface with `Probe`, `Download`, `Resume`, `Close`, and `Capabilities` methods. Wrap the existing `Downloader` in an `httpProtocolDownloader` adapter that implements this interface. Keep the current `Downloader` struct intact — it's test-covered at 87.1% and race-tested; avoid disrupting it.

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `encoding/gob` | stdlib | Persist `Item` state to `userdata.warp` | Already used; zero migration path for field additions |
| `net/url` | stdlib | Parse URL scheme for protocol routing | Already used in `dloader.go` and `redirect.go` |
| `errors` | stdlib | `errors.As`, `errors.Unwrap` for typed error chain | Already used throughout `warplib`; pattern for `DownloadError` |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `context` | stdlib | Cancel propagation through `ProtocolDownloader.Download` | Already present in `Downloader`; interface must thread it through |
| `testing` | stdlib | Table-driven tests, `t.TempDir()`, `httptest.NewServer` | Existing test pattern in this package |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Struct-based `Capabilities` return | Bitfield `uint32` | Struct is more readable, self-documenting, zero-value safe. Bitfield saves 8 bytes per downloader — irrelevant. Use struct. |
| Adapter wrapping existing `Downloader` | Rewrite `Downloader` to implement interface directly | Adapter isolates the HTTP implementation from the interface definition. Safer refactor with zero test disruption. |
| Typed enum `uint8` for Protocol field | `string` field ("http", "ftp") | `uint8` with iota: smaller GOB encoding, zero-value is ProtoHTTP, type-safe. String would complicate switch statements and waste space. |

## Architecture Patterns

### Recommended Project Structure

New files added in `pkg/warplib/`:

```
pkg/warplib/
├── protocol.go              # ProtocolDownloader interface, Capabilities struct, DownloadError type
├── protocol_http.go         # httpProtocolDownloader adapter wrapping existing *Downloader
├── protocol_router.go       # scheme-to-factory map, NewProtocolDownloader(url) dispatcher
├── protocol_test.go         # mock downloader, interface compliance test, router tests
├── protocol_http_test.go    # httpProtocolDownloader adapter tests
├── item.go                  # MODIFIED: add Protocol field + ProtoXxx constants
└── manager.go               # MODIFIED: use ProtocolDownloader interface at AddDownload/ResumeDownload
```

No new packages — everything stays inside `pkg/warplib`.

### Pattern 1: Protocol Downloader Interface

**What:** Single interface with Probe (metadata), Download (fresh start), Resume (from offset), Close, and Capabilities.

**When to use:** Any code path that creates or operates on a downloader — AddDownload, ResumeDownload, Item.StopDownload.

**Example:**

```go
// Source: design decision from CONTEXT.md; follows net.Error interface convention
// in Go standard library for IsTransient.

// DownloadCapabilities describes what a protocol backend can do.
type DownloadCapabilities struct {
    SupportsParallel bool // HTTP with Accept-Ranges: true; FTP/SFTP: false
    SupportsResume   bool // HTTP with Accept-Ranges: true; FTP REST: true; SFTP Seek: true
}

// ProbeResult contains metadata fetched before the download starts.
type ProbeResult struct {
    FileName      string
    ContentLength int64   // -1 if unknown
    Resumable     bool
    Checksums     []ExpectedChecksum // extracted from protocol headers where available
}

// ProtocolDownloader is the interface every protocol backend must implement.
// The manager calls Probe first to get metadata, then Download or Resume.
// Connection lifecycle is internal to each implementation.
type ProtocolDownloader interface {
    // Probe fetches file metadata without downloading content.
    // Must be called before Download or Resume.
    Probe(ctx context.Context) (ProbeResult, error)

    // Download starts a fresh download and blocks until complete or error.
    // handlers receives progress/completion/error callbacks.
    Download(ctx context.Context, handlers *Handlers) error

    // Resume continues a download from the given parts map.
    // For single-stream protocols (FTP/SFTP), parts must contain exactly one entry.
    Resume(ctx context.Context, parts map[int64]*ItemPart, handlers *Handlers) error

    // Capabilities returns what this protocol backend supports.
    Capabilities() DownloadCapabilities

    // Close releases all resources (connections, files).
    // Safe to call multiple times.
    Close() error
}
```

### Pattern 2: DownloadError with IsTransient

**What:** Typed error wrapping a protocol-specific cause with a transient/permanent classification.

**When to use:** All errors returned from `ProtocolDownloader.Probe`, `Download`, `Resume`.

**Example:**

```go
// Source: follows net.Error interface convention in Go stdlib.
// Manager calls errors.As(err, &de) to check IsTransient().

// DownloadError wraps a protocol error with transient classification.
type DownloadError struct {
    Protocol string // "http", "ftp", "sftp"
    Op       string // "probe", "download", "resume"
    Cause    error  // protocol-specific underlying error
    transient bool
}

func (e *DownloadError) Error() string {
    return fmt.Sprintf("%s %s: %v", e.Protocol, e.Op, e.Cause)
}

func (e *DownloadError) Unwrap() error {
    return e.Cause
}

// IsTransient returns true if the error is likely recoverable by retrying.
// Manager uses this to decide whether RetryConfig.ShouldRetry() applies.
func (e *DownloadError) IsTransient() bool {
    return e.transient
}
```

### Pattern 3: Static Scheme Router

**What:** A `map[string]DownloaderFactory` keyed on URL scheme. Built once at package init or manager startup. No dynamic registration.

**When to use:** AddDownload and ResumeDownload in Manager.

**Example:**

```go
// Source: design decision from CONTEXT.md.

// DownloaderFactory creates a ProtocolDownloader for the given URL.
// url has already been parsed; scheme is guaranteed to be supported.
type DownloaderFactory func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error)

// defaultSchemeRouter maps URL schemes to factories.
// http and https share one factory; ftp and ftps share one factory.
var defaultSchemeRouter = map[string]DownloaderFactory{
    "http":  newHTTPProtocolDownloader,
    "https": newHTTPProtocolDownloader,
    // "ftp":   newFTPProtocolDownloader,  — registered in Phase 3
    // "ftps":  newFTPProtocolDownloader,  — registered in Phase 3
    // "sftp":  newSFTPProtocolDownloader, — registered in Phase 4
}

// NewProtocolDownloader resolves a URL to a ProtocolDownloader.
// Returns a descriptive error for unsupported schemes.
func NewProtocolDownloader(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return nil, fmt.Errorf("parse url: %w", err)
    }
    factory, ok := defaultSchemeRouter[strings.ToLower(u.Scheme)]
    if !ok {
        supported := sortedKeys(defaultSchemeRouter)
        return nil, fmt.Errorf("unsupported scheme %q — supported: %s", u.Scheme, strings.Join(supported, ", "))
    }
    return factory(rawURL, opts)
}
```

### Pattern 4: Protocol Enum in Item

**What:** `uint8` typed enum field added to `Item`. Zero value is `ProtoHTTP` — no migration needed.

**When to use:** Any code that needs to know which protocol handles an `Item` (currently only the router when resuming).

**Example:**

```go
// Source: design decision from CONTEXT.md.

// Protocol identifies the download protocol for an Item.
// Zero value (ProtoHTTP = 0) is the default — backward compatible with all
// existing GOB-encoded userdata.warp files that predate this field.
type Protocol uint8

const (
    ProtoHTTP  Protocol = iota // 0 — default, backward compat
    ProtoFTP                   // 1
    ProtoFTPS                  // 2
    ProtoSFTP                  // 3
)

// String returns a human-readable protocol name.
func (p Protocol) String() string {
    switch p {
    case ProtoHTTP:
        return "http"
    case ProtoFTP:
        return "ftp"
    case ProtoFTPS:
        return "ftps"
    case ProtoSFTP:
        return "sftp"
    default:
        return fmt.Sprintf("unknown(%d)", uint8(p))
    }
}

// In Item struct — add this field:
type Item struct {
    // ... existing fields ...

    // Protocol identifies the download protocol for this item.
    // Zero value defaults to ProtoHTTP for backward compatibility
    // with pre-Phase-2 userdata.warp files.
    Protocol Protocol `json:"protocol"`
}
```

### Pattern 5: httpProtocolDownloader Adapter

**What:** Thin wrapper around the existing `*Downloader` that satisfies `ProtocolDownloader`. Keeps all existing HTTP logic intact, including retry, work stealing, checksum, proxy, redirect, speed limiting.

**When to use:** For HTTP and HTTPS schemes only. This is the Phase 2 implementation of the interface — FTP/SFTP adapters come in Phases 3/4.

**Key insight:** The existing `Downloader` struct already does most of what `ProtocolDownloader` needs. The adapter's `Probe` method calls `NewDownloader` (which internally calls `fetchInfo()`). `Download` calls `d.Start()`. `Resume` calls `d.Resume(parts)`. `Close` calls `d.Close()`. `Capabilities` returns `{SupportsParallel: true, SupportsResume: d.resumable}` (set during `fetchInfo()`).

**Problem with this mapping:** The existing `NewDownloader` constructor runs `fetchInfo()` (the Probe step) as part of construction. The adapter must call `NewDownloader` in its `Probe` method. This means `httpProtocolDownloader` holds the `*Downloader` after `Probe` is called, not before. The adapter needs an internal `*Downloader` field that's nil until `Probe` is called.

### Pattern 6: GOB Golden Fixture Test

**What:** Encode a `ManagerData` struct with the pre-Phase-2 `Item` schema to a binary file as a test fixture. Test decodes it and asserts `Protocol == ProtoHTTP` (i.e., the zero value).

**When to use:** Once, in `protocol_test.go` or `manager_test.go`. The fixture file is checked into the repository alongside the test.

**Example:**

```go
// TestGOBBackwardCompatProtocol verifies that pre-Phase-2 GOB files decode
// with Protocol == ProtoHTTP (the zero value).
// The fixture was generated by: (see generate comment below)
func TestGOBBackwardCompatProtocol(t *testing.T) {
    data, err := os.ReadFile("testdata/pre_phase2_userdata.warp")
    if err != nil {
        t.Fatalf("read fixture: %v", err)
    }

    var md ManagerData
    if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&md); err != nil {
        t.Fatalf("decode fixture: %v", err)
    }

    for hash, item := range md.Items {
        if item.Protocol != ProtoHTTP {
            t.Errorf("item %q: expected Protocol==ProtoHTTP (0), got %d", hash, item.Protocol)
        }
    }
}
```

The fixture is generated once before adding the `Protocol` field:

```go
// Run manually: go test -run TestGenerateGOBFixture -write-fixture
// This creates testdata/pre_phase2_userdata.warp.
```

### Anti-Patterns to Avoid

- **Embedding `*Downloader` in the interface type:** The manager currently stores `*Downloader` in `Item.dAlloc`. Changing `Item.dAlloc` to `ProtocolDownloader` is the correct refactor. Do NOT keep `*Downloader` as a concrete field alongside the interface — dual-field confusion.
- **Dynamic protocol registration:** `RegisterProtocol("ftp", factory)` patterns introduce global mutable state. The static map is simpler and race-safe.
- **Calling `Probe` inside `Download`:** The Probe/Download separation is intentional. The manager needs metadata (filename, size) before `AddDownload` stores the `Item`. If `Download` probes internally, the manager has no way to know the filename before the download starts.
- **Returning `bool` instead of typed error from `IsTransient`:** The `DownloadError.IsTransient()` method allows the manager to ask "should I retry this?" without the manager knowing protocol-specific error codes. Returning bare booleans from Download() as a second return value would be an anti-pattern.
- **Changing `manager.ResumeDownload` to take an interface:** `ResumeDownload` currently takes `*http.Client`. For HTTP this is fine. For Phase 2, ResumeDownload should dispatch based on `item.Protocol` to `NewProtocolDownloader`. Don't add a second `client` parameter for every protocol — let each factory handle its own connection config.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Error wrapping | Custom error registry | `errors.As` + struct with `Unwrap()` | Go stdlib error chain; already used in warplib |
| GOB compatibility | Migration/upgrade script | Zero-value `uint8` field + decoder's natural behavior | GOB decodes missing fields as zero — free compat |
| URL parsing | Custom scheme extraction | `net/url.Parse()` + `.Scheme` field | Already used in `redirect.go` and `dloader.go` |
| Mock downloader in tests | Full network stub | Struct implementing `ProtocolDownloader` interface | Interface makes mocking trivial in pure unit tests |

**Key insight:** The Go type system does the heavy lifting here. Interfaces and GOB zero-value semantics are the tools — resist adding runtime machinery (registries, migrations) where compile-time constructs work.

## Common Pitfalls

### Pitfall 1: GOB Breaking Existing Files on Unknown Protocol Value

**What goes wrong:** After Phase 3/4 add `ProtoFTP`/`ProtoSFTP`, a file encoded with those values is opened by an older binary. The older binary decodes `Protocol` as a raw `uint8` but does nothing with it. **Not a problem** — GOB just reads the field into whatever type it finds. The issue is the reverse: a new binary opens an old file and finds `Protocol == 0`, which must decode as `ProtoHTTP`. This works with `iota` starting at 0.

**Why it happens:** Confusion between forward compat (old binary reading new data) and backward compat (new binary reading old data). Phase 2 only needs to guarantee backward compat (new binary, old data).

**How to avoid:** Keep `ProtoHTTP = iota` (= 0). The golden fixture test catches any regression.

**Warning signs:** If `ProtoHTTP` is ever reassigned to a non-zero value in refactoring, all old GOB files will be silently misclassified.

### Pitfall 2: Adapter Probe/Download Ordering Bug

**What goes wrong:** `httpProtocolDownloader.Download()` is called without `Probe()` first. The internal `*Downloader` is nil, causing a nil pointer panic.

**Why it happens:** The interface contract says "call Probe first", but Go has no enforce-at-compile-time way to express this ordering constraint.

**How to avoid:** In `Download` and `Resume`, check `d.inner == nil` and return a descriptive error: `"Probe must be called before Download"`. Add a test that calls Download without Probe and asserts the error.

### Pitfall 3: Item.dAlloc Type Change Breaks Existing Tests

**What goes wrong:** Changing `Item.dAlloc` from `*Downloader` to `ProtocolDownloader` requires updating every test that directly accesses `item.dAlloc` as a `*Downloader`.

**Why it happens:** `manager_test.go` and `manager_resume_test.go` use `item.dAlloc` directly (e.g., `resumed.dAlloc.Close()`). After the type change, `dAlloc.Close()` still compiles (Close is on the interface), but fields like `resumed.dAlloc.GetMaxConnections()` also still compile because those are on the interface too.

**How to avoid:** Check which `*Downloader` methods are called on `item.dAlloc` in tests. Ensure all used methods exist on `ProtocolDownloader` or add them. In practice: `Close()`, `GetMaxConnections()`, `GetMaxParts()` — all must be on the interface OR accessed via type assertion to `*Downloader` in tests. Prefer moving them to the interface.

**Warning signs:** Compile errors in `_test.go` files after the type change — fix these rather than using `.(* Downloader)` type assertions in tests (that defeats the abstraction).

### Pitfall 4: Manager's patchHandlers Coupled to *Downloader

**What goes wrong:** `Manager.patchHandlers(d *Downloader, item *Item)` in `manager.go` directly accesses `d.handlers` (unexported field). When the interface is introduced, `patchHandlers` must accept `ProtocolDownloader` — but `ProtocolDownloader` has no `handlers` field.

**Why it happens:** The handler patching is deep HTTP implementation detail currently exposed through the concrete type.

**How to avoid:** Two options:
1. Add `SetHandlers(*Handlers)` and `GetHandlers() *Handlers` to the interface. Clean, generic.
2. Keep `patchHandlers` taking `*Downloader` specifically for HTTP, and have the interface expose a `PatchHandlers(original *Handlers) *Handlers` method so each implementation can accept patched handlers.

**Recommendation:** Option 1. Add `SetHandlers(*Handlers)` to `ProtocolDownloader`. The `httpProtocolDownloader` adapter delegates to `d.inner.handlers`. FTP/SFTP adapters will need handlers too — the interface already includes them in `Download`/`Resume` parameters (see Pattern 1), which is cleaner than a setter. **Revisit:** the CONTEXT.md says "all protocols accept the same handler callbacks" — thread them as parameters in `Download`/`Resume`, NOT as a setter method. Remove `SetHandlers` from the interface. Instead, `patchHandlers` must change to work through whatever mechanism the interface provides. The cleanest approach: `patchHandlers` receives the `*Handlers` the caller passed to `Download/Resume`, wraps it, and passes the wrapped `*Handlers` back into the `Download/Resume` call.

This is a non-trivial refactor. See Code Examples below for the concrete approach.

### Pitfall 5: Scheme Comparison is Case-Sensitive in net/url

**What goes wrong:** `url.Parse("HTTP://example.com")` returns `.Scheme == "http"` (lowercase), but `url.Parse("ftp://...")` also returns lowercase. Go's `net/url` lowercases the scheme during parsing. However, user input might pass scheme-only strings (not full URLs) through the router lookup without parsing.

**Why it happens:** The router uses `u.Scheme` from `url.Parse`, which always lowercases. But if someone calls the router with a pre-extracted scheme string, case comparison fails.

**How to avoid:** Always use `strings.ToLower(u.Scheme)` when doing the map lookup, even though `url.Parse` already lowercases. Belt-and-suspenders. The test should include `HTTP://` and `HTTPS://` URLs to verify.

## Code Examples

Verified patterns from the existing codebase:

### Existing Handler Patching Pattern (manager.go:177-231)

```go
// Source: /pkg/warplib/manager.go - patchHandlers
// This is the pattern Phase 2 must preserve for all protocol backends.
func (m *Manager) patchHandlers(d *Downloader, item *Item) {
    oSPH := d.handlers.SpawnPartHandler
    d.handlers.SpawnPartHandler = func(hash string, ioff, foff int64) {
        item.addPart(hash, ioff, foff)
        m.UpdateItem(item)
        oSPH(hash, ioff, foff)
    }
    // ... 4 more patches ...
}
```

After Phase 2, this must work with `ProtocolDownloader`. The cleanest path: pass a `*Handlers` into `Download`/`Resume`, and make the manager pre-patch the handlers before calling `Download`/`Resume`. The `httpProtocolDownloader` adapter internally calls `d.Start()` with the already-patched handlers — no need for `patchHandlers` to reach into the adapter's internals.

This means `Manager.AddDownload` flow becomes:
1. Call `pd.Probe(ctx)` → get `ProbeResult`
2. Create `Item` from `ProbeResult`
3. Build `*Handlers` for the item
4. Patch the handlers to update the item on events (the patching logic moves here)
5. Call `pd.Download(ctx, patchedHandlers)` in a goroutine

### Existing Item.dAlloc Pattern

```go
// Source: /pkg/warplib/item.go
// Currently dAlloc is *Downloader. After Phase 2 it becomes ProtocolDownloader.
type Item struct {
    // ...
    dAlloc *Downloader  // CHANGE TO: dAlloc ProtocolDownloader
}

// All accessors (getDAlloc, setDAlloc, clearDAlloc) remain identical in structure.
// Methods that delegate to dAlloc (Resume, StopDownload, CloseDownloader,
// IsDownloading, IsStopped, GetMaxConnections, GetMaxParts) require the
// interface to expose those operations OR use type assertion.
```

**Decision needed (Claude's discretion):** Keep `GetMaxConnections()` and `GetMaxParts()` on the interface or only on HTTP? For FTP/SFTP (single-stream, always 1 connection), these return (1, nil). Best to include them in the interface with sensible defaults for non-parallel protocols. This avoids type assertions in `item.go`.

### GOB Encoding — Zero Value Behavior (stdlib documented)

```go
// Source: Go stdlib encoding/gob documentation.
// "If a field is missing from the incoming stream, the corresponding
// field in the destination struct will be set to the zero value for
// that type." — This guarantees backward compat for Protocol uint8.

// Encoding a ManagerData with NO Protocol field:
var data ManagerData
gob.NewEncoder(buf).Encode(data)

// Decoding into a struct WITH Protocol field:
var result ManagerData // result.Items[x].Protocol will be 0 == ProtoHTTP
gob.NewDecoder(buf).Decode(&result)
// result.Items[x].Protocol == 0 == ProtoHTTP ✓
```

### net.Error IsTransient() Analogy (stdlib)

```go
// Source: Go stdlib net package — net.Error interface.
// The IsTransient() pattern follows the same convention.
type Error interface {
    error
    Timeout() bool   // Is the error a timeout?
    Temporary() bool // Is the error temporary?
}

// Our equivalent:
type DownloadError struct { ... }
func (e *DownloadError) IsTransient() bool { return e.transient }
// Manager: var de *DownloadError; errors.As(err, &de) && de.IsTransient()
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| HTTP-only `*Downloader` directly stored in `Item.dAlloc` | `ProtocolDownloader` interface stored in `Item.dAlloc` | Phase 2 | Manager and Item become protocol-agnostic |
| No scheme routing — HTTP assumed | Static `map[string]DownloaderFactory` dispatching on `url.Parse().Scheme` | Phase 2 | Enables FTP/SFTP in Phase 3/4 without touching Manager |
| `Item` has no Protocol field | `Item.Protocol` (`uint8`, iota from 0=HTTP) | Phase 2 | Manager knows how to resume an item (which factory to use) |

**Deprecated/outdated:**
- Direct `*Downloader` access in `manager.AddDownload` and `manager.ResumeDownload`: replaced by `ProtocolDownloader` interface calls after Phase 2

## Open Questions

1. **GetMaxConnections / GetMaxParts on interface vs HTTP-only**
   - What we know: `Item.GetMaxConnections()` delegates to `dAlloc.GetMaxConnections()`. If `dAlloc` becomes `ProtocolDownloader`, the interface must expose these.
   - What's unclear: Whether CLI/daemon code actually calls `item.GetMaxConnections()` externally, or only internally in item.go.
   - Recommendation: Grep the call sites (`internal/api/`, `cmd/`) to determine if these are on the public surface. If yes, add to interface. If only used in `item.go`, consider removing them from `Item` entirely and putting the logic in the manager. Either is defensible; the grep will settle it.

2. **Context threading through the existing Downloader**
   - What we know: `Downloader` creates its own `context.WithCancel` internally in the constructor. The interface signature proposed above takes `ctx context.Context` in `Download`/`Resume`.
   - What's unclear: Should the adapter's `ctx` parameter replace the internal context, or be composed with it (`context.WithCancel` of the passed ctx)?
   - Recommendation: Use `context.WithCancel(ctx)` inside the adapter. This lets the caller's context cancellation propagate while preserving the adapter's ability to self-cancel on `Stop()`.

3. **patchHandlers refactor scope**
   - What we know: The current `patchHandlers(d *Downloader, item *Item)` reaches into `d.handlers` directly. Post-interface, this coupling must be broken.
   - What's unclear: Exactly how `AddDownload`/`ResumeDownload` will thread patched handlers into `Download`/`Resume` calls.
   - Recommendation: The `ProtocolDownloader.Download(ctx, handlers)` signature already threads handlers in. `Manager.AddDownload` should build the patched `*Handlers` before calling `pd.Download()`. The patching logic stays in manager.go, just restructured to produce a `*Handlers` rather than mutating the downloader in place.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` |
| Config file | none (uses `go test`) |
| Quick run command | `go test -run TestProto ./pkg/warplib/` |
| Full suite command | `go test -race -short ./pkg/warplib/` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PROTO-01 | `ProtocolDownloader` interface defined; `httpProtocolDownloader` implements it; `var _ ProtocolDownloader = (*httpProtocolDownloader)(nil)` compile check | unit | `go test -run TestProtocolDownloaderInterface ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-01 | Mock downloader implementing interface integrates with manager (AddDownload accepts it) | unit | `go test -run TestManagerWithMockDownloader ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-01 | Capabilities() returns correct values for HTTP (parallel=true, resume=true) | unit | `go test -run TestHTTPCapabilities ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-01 | DownloadError.IsTransient() returns correct value for transient vs permanent cases | unit | `go test -run TestDownloadErrorIsTransient ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-01 | Calling Download without Probe returns descriptive error | unit | `go test -run TestProbeRequiredBeforeDownload ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-02 | Router resolves "http" and "https" to httpProtocolDownloader | unit | `go test -run TestSchemeRouter ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-02 | Router returns descriptive error for unsupported scheme "magnet" | unit | `go test -run TestSchemeRouterUnsupported ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-02 | Router handles mixed-case scheme ("HTTP://") correctly | unit | `go test -run TestSchemeRouterCaseInsensitive ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-03 | Existing GOB fixture (pre-Phase-2) decodes with Protocol==ProtoHTTP | unit | `go test -run TestGOBBackwardCompatProtocol ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-03 | Item with Protocol==7 (unknown) returns clear error on decode | unit | `go test -run TestGOBUnknownProtocol ./pkg/warplib/` | ❌ Wave 0 |
| PROTO-03 | Round-trip: encode Item with ProtoHTTP, decode, assert Protocol==ProtoHTTP | unit | `go test -run TestGOBRoundTrip ./pkg/warplib/` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test -run TestProto -run TestScheme -run TestGOB ./pkg/warplib/`
- **Per wave merge:** `go test -race -short ./pkg/warplib/`
- **Phase gate:** Full suite green before `/gsd:verify-work` — coverage must remain ≥80%

### Wave 0 Gaps

- [ ] `pkg/warplib/testdata/pre_phase2_userdata.warp` — binary fixture for PROTO-03 backward compat test
- [ ] `pkg/warplib/protocol_test.go` — interface compliance + mock downloader + error tests
- [ ] `pkg/warplib/protocol_router_test.go` — scheme routing tests (or fold into protocol_test.go)

## Sources

### Primary (HIGH confidence)

- Go stdlib `encoding/gob` documentation — zero-value field behavior for backward compatibility (verified from Go 1.24.9 behavior in this codebase)
- Go stdlib `net/url` documentation — `.Scheme` field is lowercased by `Parse`
- Go stdlib `net` package — `net.Error` interface as the `IsTransient()` pattern model
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/dloader.go` — existing Downloader implementation inspected in full
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/manager.go` — patchHandlers coupling inspected
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/item.go` — dAlloc field type and Item struct inspected
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/handlers.go` — Handlers struct inspected
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/errors.go` — error catalog inspected

### Secondary (MEDIUM confidence)

- Existing test pattern from `manager_test.go`, `dloader_test.go`, `manager_resume_test.go` — confirmed Go stdlib testing patterns match what's needed for Phase 2 tests

### Tertiary (LOW confidence)

- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all stdlib, already in use
- Architecture: HIGH — derived from direct codebase inspection and locked CONTEXT.md decisions
- Pitfalls: HIGH — derived from direct inspection of the code paths that will be affected (patchHandlers, Item.dAlloc, GOB encoding)
- Open questions: MEDIUM — grep needed for call sites of GetMaxConnections/GetMaxParts

**Research date:** 2026-02-27
**Valid until:** 2026-04-30 (stable Go stdlib, no fast-moving dependencies)
