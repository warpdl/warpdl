# Architecture Research

**Domain:** Daemon-based download manager — protocol expansion and JSON-RPC API
**Researched:** 2026-02-26
**Confidence:** HIGH (based on direct codebase inspection + verified library docs)

## Existing Architecture (Reference Baseline)

The existing system is a clean layered architecture. Every new feature must integrate at the correct layer — not bypass it.

```
┌─────────────────────────────────────────────────────────────────┐
│  CLI Layer  (cmd/)                                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │ download │  │  resume  │  │   list   │  │  daemon  │  ...   │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘        │
├───────┴─────────────┴─────────────┴──────────────┴──────────────┤
│  Client Layer  (pkg/warpcli/)                                    │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │  client.go — IPC dial, request marshal, auto-spawn      │    │
│  └──────────────────────────┬──────────────────────────────┘    │
├──────────────────────────────┼──────────────────────────────────┤
│  IPC Transport                                                   │
│  Unix socket / named pipe / TCP fallback                         │
├──────────────────────────────┼──────────────────────────────────┤
│  Server Layer  (internal/server/)                                │
│  ┌──────────┐  ┌──────────┐  ┌────────────────────────────┐    │
│  │ server.go│  │  pool.go │  │  web.go (WebSocket, :port+1)│    │
│  └────┬─────┘  └──────────┘  └────────────────────────────┘    │
├───────┴─────────────────────────────────────────────────────────┤
│  API Handler Layer  (internal/api/)                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │download.go│ │ resume.go│  │  list.go │  │queue.go  │  ...   │
│  └────┬──────┘ └────┬─────┘  └────┬─────┘  └──────────┘        │
├───────┴─────────────┴─────────────┴─────────────────────────────┤
│  Download Engine  (pkg/warplib/)                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │
│  │dloader.go│  │manager.go│  │  queue.go│  │handlers.go│       │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │
└─────────────────────────────────────────────────────────────────┘
```

---

## System Overview — After New Features

The four new features integrate at three distinct layers:

```
┌──────────────────────────────────────────────────────────────────┐
│  CLI Layer  (cmd/)                                                │
│  No changes needed — existing download command handles ftp://     │
│  and sftp:// URLs once the engine supports them                   │
├──────────────────────────────────────────────────────────────────┤
│  Server Layer  (internal/server/)                                 │
│  ┌───────────────────────────────────────────────────────────┐   │
│  │  NEW: jsonrpc.go — JSON-RPC 2.0 handler mux on :port+1    │   │
│  │  (replaces / coexists with raw WebSocket forward path)    │   │
│  └───────────────────────────────────────────────────────────┘   │
├──────────────────────────────────────────────────────────────────┤
│  API Handler Layer  (internal/api/)                               │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │  NEW: jsonrpc_handler.go — bridges JSON-RPC to Api struct │    │
│  └──────────────────────────────────────────────────────────┘    │
├──────────────────────────────────────────────────────────────────┤
│  Download Engine  (pkg/warplib/)                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────┐     │
│  │ dloader.go   │  │ ftp_dloader.go│  │ sftp_dloader.go    │     │
│  │ (HTTP, fixed)│  │ (NEW, FTP)   │  │ (NEW, SFTP)        │     │
│  └──────────────┘  └──────────────┘  └────────────────────┘     │
│  ┌─────────────────────────────────────────────────────────┐     │
│  │  NEW: protocol.go — Downloader interface + URL routing  │     │
│  └─────────────────────────────────────────────────────────┘     │
│  ┌─────────────────────────────────────────────────────────┐     │
│  │  MODIFIED: http client — AllowRedirects (CheckRedirect) │     │
│  └─────────────────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────────────────┘
```

---

## Component Boundaries

### New and Modified Components

| Component | Location | Responsibility | Communicates With |
|-----------|----------|----------------|-------------------|
| FTP Downloader | `pkg/warplib/ftp_dloader.go` | Single-stream FTP/FTPS download using `jlaffaye/ftp`. Implements same `Start()`/`Resume()`/`Stop()` interface. | `pkg/warplib.Manager`, `pkg/warplib.Handlers` |
| SFTP Downloader | `pkg/warplib/sftp_dloader.go` | Single-stream SFTP download using `pkg/sftp` + `golang.org/x/crypto/ssh`. Implements same interface. | `pkg/warplib.Manager`, `pkg/warplib.Handlers` |
| Protocol Router | `pkg/warplib/protocol.go` | Inspect URL scheme (`ftp://`, `ftps://`, `sftp://`, `http://`, `https://`) and return the correct Downloader implementation. Called by `NewDownloader`. | `pkg/warplib` internals |
| HTTP Redirect Fix | `pkg/warplib/http_client.go` (modify existing) | Configure `http.Client.CheckRedirect = nil` (which is already the default — follow up to 10 hops). Verify the existing client isn't capping or blocking redirects; add configurable max-redirect option to `DownloaderOpts`. | `pkg/warplib.Downloader` |
| JSON-RPC Handler | `internal/server/jsonrpc.go` | JSON-RPC 2.0 request parsing, dispatch to Api methods, response framing. Runs on the existing `port+1` HTTP server alongside the WebSocket path. | `internal/api.Api`, `internal/server.WebServer` |
| JSON-RPC API Bridge | `internal/api/jsonrpc_handler.go` | Adapts the existing `Api` handler methods to JSON-RPC method signatures. Reuses all existing download/resume/list/stop handler logic. | `internal/api.Api`, `internal/server.Pool` |

### Unchanged Components (explicitly — do NOT modify these)

| Component | Reason Not to Touch |
|-----------|---------------------|
| `pkg/warpcli/` | CLI-to-daemon IPC protocol is independent of new protocols |
| `cmd/` commands | CLI dispatch is URL-agnostic — passes URL as string |
| `internal/server/server.go` | IPC server is unchanged; JSON-RPC is on the separate web server |
| `pkg/warplib/manager.go` | Manager tracks Items by hash regardless of protocol |
| `pkg/warplib/handlers.go` | Event callback types are protocol-agnostic |

---

## Data Flow

### FTP/SFTP Download Request

```
User: warpdl download ftp://user:pass@host/file.zip
  |
  v
cmd/download.go — passes URL string to daemon unchanged
  |
  v
warpcli → IPC socket → internal/server.handleConnection()
  |
  v
internal/api.downloadHandler()
  |
  v
warplib.NewDownloader(client, "ftp://...", opts)
  |
  v
pkg/warplib/protocol.go — scheme detection
  - "ftp://" or "ftps://" → FTPDownloader
  - "sftp://" → SFTPDownloader
  - "http://" or "https://" → existing Downloader (unchanged)
  |
  v
FTPDownloader.Start()
  - ftp.Dial() → passive mode connection
  - ftp.RetrFrom(path, offset) for resume; ftp.Retr(path) for fresh
  - io.Copy to local file, calling DownloadProgressHandler(hash, n)
  - On complete: DownloadCompleteHandler(MAIN_HASH, totalBytes)
  |
  v
Manager.AddDownload(d, opts) — state persisted same as HTTP
  |
  v
Pool broadcasts progress updates to connected CLI clients
```

### HTTP Redirect Following

```
existing Downloader.fetchInfo() → makeRequest(GET)
  |
  v
http.Client.Do(req)
  - Default net/http follows up to 10 redirects automatically
  - CheckRedirect=nil means: follow all standard 3xx
  - CHANGE: ensure DownloaderOpts.MaxRedirects is wired in
  - If MaxRedirects > 0: custom CheckRedirect counts hops
  - Final URL after redirects is used for file name extraction
  |
  v
d.url = resp.Request.URL.String()  // store resolved URL post-redirect
```

### JSON-RPC API Request Flow

```
External client: POST http://localhost:6801/jsonrpc
  (or WebSocket ws://localhost:6801/jsonrpc)
  {
    "jsonrpc": "2.0",
    "method": "warpdl.addUri",
    "params": { "url": "...", "dir": "/downloads" },
    "id": 1
  }
  |
  v
internal/server/web.go — http.ServeMux routes /jsonrpc to jsonrpc.go
  |
  v
internal/server/jsonrpc.go — parse JSON-RPC 2.0 envelope
  - Validate jsonrpc field, method, params
  - Validate auth token (Bearer or secret param)
  - Dispatch to registered method handler
  |
  v
internal/api/jsonrpc_handler.go — method adapter
  e.g. "warpdl.addUri" → constructs DownloadParams → calls s.downloadHandler()
  |
  v
internal/api/download.go — existing handler (reused, not duplicated)
  |
  v
Returns JSON-RPC response envelope:
  {"jsonrpc": "2.0", "result": {...}, "id": 1}
```

### State Persistence (unchanged for all new protocols)

```
FTPDownloader / SFTPDownloader
  |
  v (calls Manager.AddDownload same as HTTP)
  v
Manager.ItemsMap[hash] → Item{Parts, Downloaded, Protocol}
  |
  v
GOB encode → ~/.config/warpdl/userdata.warp
```

---

## Architectural Patterns

### Pattern 1: Downloader Interface (Protocol Abstraction)

**What:** Extract a `Downloader` interface from the existing concrete struct so FTP and SFTP can implement the same contract.

**When to use:** Mandatory — Manager currently accepts `*Downloader` (concrete). It must accept an interface to support multiple protocol backends.

**Trade-offs:** Requires a refactor of `Manager.AddDownload` to accept the interface. Low risk because the API surface is small (`GetHash()`, `GetFileName()`, `GetContentLength()`, `GetDownloadDirectory()`, `GetSavePath()`, `Start()`, `Resume()`, `Stop()`, `IsStopped()`).

```go
// pkg/warplib/protocol.go
type DownloaderI interface {
    GetHash() string
    GetFileName() string
    GetContentLength() ContentLength
    GetDownloadDirectory() string
    GetSavePath() string
    Start() error
    Resume(parts map[int64]*ItemPart) error
    Stop()
    IsStopped() bool
}

// NewDownloaderFromURL replaces direct NewDownloader calls when protocol routing matters
func NewDownloaderFromURL(url string, opts *DownloaderOpts, ...) (DownloaderI, error) {
    scheme := extractScheme(url)
    switch scheme {
    case "ftp", "ftps":
        return newFTPDownloader(url, opts)
    case "sftp":
        return newSFTPDownloader(url, opts)
    default:
        return NewDownloader(httpClient, url, opts)
    }
}
```

### Pattern 2: Single-Stream Downloader for FTP/SFTP

**What:** FTP and SFTP do not support HTTP Range requests or parallel segment downloads. Both implementations will be single-stream, calling `DownloadProgressHandler` incrementally and `DownloadCompleteHandler` at end.

**When to use:** Always for FTP/SFTP — there is no practical workaround; the protocol does not support concurrent reads from a single connection on a single file.

**Trade-offs:** No work stealing, no multi-part compilation, no speed-based spawning. These features are HTTP-only. Item.Parts for FTP/SFTP will always have a single entry. Resume is supported via `ftp.RetrFrom(path, offset)` (byte offset) and `sftp.File.Seek(offset, 0)`.

```go
// pkg/warplib/ftp_dloader.go — conceptual
func (d *FTPDownloader) Start() error {
    conn, err := ftp.DialWithContext(d.ctx, d.addr, ftp.DialWithTLS(d.tlsConfig))
    // ...
    r, err := conn.Retr(d.remotePath)
    buf := make([]byte, 32*KB)
    for {
        n, err := r.Read(buf)
        if n > 0 {
            d.f.Write(buf[:n])
            atomic.AddInt64(&d.nread, int64(n))
            d.handlers.DownloadProgressHandler(MAIN_HASH, n)
        }
        if err == io.EOF { break }
        if err != nil { d.handlers.ErrorHandler(MAIN_HASH, err); return err }
    }
    d.handlers.DownloadCompleteHandler(MAIN_HASH, d.nread)
    return nil
}
```

### Pattern 3: JSON-RPC as a Thin Adapter Layer

**What:** The JSON-RPC server does not reimplement business logic. It is a transport adapter that maps JSON-RPC method calls to existing `internal/api` handler functions.

**When to use:** Mandatory — duplicating logic between warpcli protocol and JSON-RPC would create divergence. The adapter pattern ensures both paths use the same `Api` methods.

**Trade-offs:** Requires the `Api` methods (currently taking `*SyncConn` and `*Pool`) to be accessible without IPC. The cleanest approach is to extract the core logic of each handler into a service method, then call it from both the IPC handler and the JSON-RPC adapter.

**Method Naming (no aria2 compatibility required per PROJECT.md):**

| JSON-RPC Method | Maps To |
|----------------|---------|
| `warpdl.addUri` | `api.downloadHandler` |
| `warpdl.resume` | `api.resumeHandler` |
| `warpdl.stop` | `api.stopHandler` |
| `warpdl.tellStatus` | `api.attachHandler` (poll mode) |
| `warpdl.tellActive` | `api.listHandler` (active filter) |
| `warpdl.tellStopped` | `api.listHandler` (completed filter) |
| `warpdl.getVersion` | `api.versionHandler` |
| `warpdl.getGlobalStat` | `api.queueStatusHandler` |

### Pattern 4: HTTP Redirect Following (Minimal Change)

**What:** `net/http` already follows up to 10 redirects by default when `CheckRedirect` is nil. The actual issue being fixed is that the Downloader makes a second request to probe range support (`prepareDownloader`), and the resolved URL after the first redirect must be used for all subsequent requests — otherwise each part hits the original URL and re-redirects independently.

**When to use:** Always for HTTP downloads — capture `resp.Request.URL` after the initial `fetchInfo()` GET and update `d.url` to the final resolved URL.

**Trade-offs:** Zero breaking changes. The fix is a 2-line change in `fetchInfo()`.

```go
// pkg/warplib/dloader.go — fetchInfo()
resp, er := d.makeRequest(http.MethodGet)
// AFTER: capture resolved URL post-redirect
d.url = resp.Request.URL.String()
```

---

## Recommended Project Structure (New Files Only)

```
pkg/warplib/
├── protocol.go           # DownloaderI interface + URL scheme router
├── ftp_dloader.go        # FTP/FTPS downloader implementation
├── ftp_dloader_test.go   # Unit tests (mock FTP server)
├── sftp_dloader.go       # SFTP downloader implementation
├── sftp_dloader_test.go  # Unit tests (mock SFTP server)
└── (modify) dloader.go   # fetchInfo: capture resolved URL post-redirect

internal/server/
└── jsonrpc.go            # JSON-RPC 2.0 request handler (HTTP + WS paths)

internal/api/
└── jsonrpc_handler.go    # Maps JSON-RPC method names to Api methods
```

---

## Build Order (Dependencies Between Components)

The components have clear dependencies. Build in this order:

**Phase 1 — HTTP Redirect (no new dependencies)**
1. Modify `dloader.go`: capture `resp.Request.URL.String()` after `fetchInfo()` GET
2. Add `MaxRedirects` field to `DownloaderOpts` and wire `CheckRedirect` if set
3. Tests: verify redirect chains are followed, final URL is used for parts

**Phase 2 — Protocol Interface Extraction (prerequisite for FTP/SFTP)**
1. Define `DownloaderI` interface in `protocol.go`
2. Verify existing `Downloader` struct satisfies `DownloaderI` (compiler check)
3. Update `Manager.AddDownload` to accept `DownloaderI` (type change only)
4. Add `NewDownloaderFromURL` factory function in `protocol.go`
5. Tests: existing Manager tests still pass

**Phase 3 — FTP Downloader (depends on Phase 2)**
1. Add `github.com/jlaffaye/ftp` to go.mod
2. Implement `FTPDownloader` in `ftp_dloader.go`
3. Wire into `NewDownloaderFromURL` for `ftp://` and `ftps://` schemes
4. Implement credential extraction from URL userinfo
5. Tests: mock FTP server using `net` listener

**Phase 4 — SFTP Downloader (depends on Phase 2)**
1. Add `golang.org/x/crypto/ssh` and `github.com/pkg/sftp` to go.mod
2. Implement `SFTPDownloader` in `sftp_dloader.go`
3. Wire into `NewDownloaderFromURL` for `sftp://` scheme
4. Implement password auth and key-file auth from `DownloaderOpts`
5. Tests: mock SSH server or in-process test server

**Phase 5 — JSON-RPC API (depends on nothing new — uses existing Api)**
1. Add JSON-RPC request/response types to `internal/server/jsonrpc.go`
2. Implement `ServeHTTP` for JSON-RPC over HTTP POST
3. Implement WebSocket upgrade path for persistent connections
4. Add auth token validation (Bearer header or `token` param)
5. Register `/jsonrpc` route in `web.go`'s `handler()` method
6. Implement method adapter in `internal/api/jsonrpc_handler.go`
7. Tests: HTTP POST tests, WebSocket tests, auth validation tests

---

## Integration Points

### External Libraries

| Library | Version | Integration Point | Notes |
|---------|---------|------------------|-------|
| `github.com/jlaffaye/ftp` | latest (v0.2.0+) | `pkg/warplib/ftp_dloader.go` | Passive mode default (EPSV), TLS via `DialWithTLS`. Resume via `RetrFrom(path, offset)`. HIGH confidence — docs verified. |
| `github.com/pkg/sftp` | v1.13.10 | `pkg/warplib/sftp_dloader.go` | `sftp.NewClient(*ssh.Client)`. Resume via `File.Seek(offset, 0)`. HIGH confidence — docs verified. |
| `golang.org/x/crypto/ssh` | current | `pkg/warplib/sftp_dloader.go` | Already an indirect dependency via `go-keyring`. Add direct. |
| `net/http` (stdlib) | 1.24+ | `pkg/warplib/dloader.go` | Redirect following is already built in. Change is minimal. HIGH confidence. |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `protocol.go` → `ftp_dloader.go` | Direct factory call | `NewFTPDownloader(url, opts)` returns `DownloaderI` |
| `protocol.go` → `sftp_dloader.go` | Direct factory call | `NewSFTPDownloader(url, opts)` returns `DownloaderI` |
| `internal/api` → `internal/server/jsonrpc.go` | Direct method call | JSON-RPC handler calls `Api` methods directly, not via IPC |
| `internal/server/web.go` → `jsonrpc.go` | HTTP mux routing | `/jsonrpc` path registered on existing `http.Server` at `port+1` |
| `FTPDownloader` → `warplib.Manager` | Same as HTTP | `Manager.AddDownload(d DownloaderI, opts)` |
| `FTPDownloader` → `warplib.Handlers` | Same as HTTP | Progress callbacks are protocol-agnostic |

---

## Anti-Patterns to Avoid

### Anti-Pattern 1: Bypassing the Manager for FTP/SFTP

**What people do:** Implement FTP/SFTP download directly in `internal/api/download.go` with a type-switch on URL scheme, skipping the `Manager`.

**Why it's wrong:** Downloads won't persist. Resume won't work. Queue integration breaks. List command won't show FTP/SFTP downloads.

**Do this instead:** FTP/SFTP downloaders implement `DownloaderI`. Manager handles them identically to HTTP. Protocol routing happens inside `warplib`, invisible to the API layer.

### Anti-Pattern 2: Duplicating API Logic in JSON-RPC Handler

**What people do:** Re-implement download logic inside `jsonrpc_handler.go` — re-read parameters, call `warplib.NewDownloader` directly.

**Why it's wrong:** Any bug fix or feature in `api/download.go` must then be duplicated in the JSON-RPC path. This happened with aria2's own codebase and created years of drift.

**Do this instead:** `jsonrpc_handler.go` is a thin adapter that constructs `common.DownloadParams` and delegates to `api.Api.downloadHandler()` internal method (or an extracted service function). Zero business logic in the adapter.

### Anti-Pattern 3: Parallel Segments for FTP/SFTP

**What people do:** Open multiple FTP connections to the same file to simulate parallel downloading (connection pool hack).

**Why it's wrong:** Most FTP servers limit concurrent connections per user. The REST command positions a single stream — you can't use it to download ranges simultaneously like HTTP Range does. This approach is fragile and server-configuration-dependent.

**Do this instead:** Single-stream download for FTP/SFTP. Accept the performance limitation. The download queue still provides concurrency across different files.

### Anti-Pattern 4: Embedding Credentials in the Download Hash

**What people do:** Include FTP username/password in the URL that generates the download hash, causing the hash to change if the password changes.

**Why it's wrong:** The hash is used to identify the download for resume. If password changes, the Item can't be found for resume.

**Do this instead:** Strip credentials from the URL before hashing. Store credentials separately in `warplib.DownloaderOpts.Credentials` or in the existing `credman` package. The hash should be derived from scheme + host + path only.

### Anti-Pattern 5: Exposing JSON-RPC Without Auth by Default

**What people do:** Enable JSON-RPC server without requiring token, citing "it's localhost-only anyway."

**Why it's wrong:** Any process on the local machine can make requests. Malicious extensions, other apps, or SSRF in a browser tab can trigger downloads to arbitrary paths.

**Do this instead:** Require auth token by default (`--rpc-secret` flag). Localhost-only binding is a defense-in-depth layer, not a replacement for auth.

---

## Scalability Considerations

This is a local download manager daemon. Scalability concerns are about file descriptor limits and connection limits per machine, not horizontal scale.

| Concern | Current (HTTP-only) | After FTP/SFTP/JSON-RPC |
|---------|-------------------|------------------------|
| Open connections per download | maxConnections (default: 24) | FTP/SFTP: 1 per download. JSON-RPC clients: bounded by OS |
| Concurrent downloads | Queue manager, configurable max | Unchanged — FTP/SFTP queue same as HTTP |
| Memory per download | Part buffers (chunk * numParts) | FTP/SFTP: single buffer (32KB). Lower memory. |
| JSON-RPC clients | N/A | WebSocket connections held in memory. Bounded by OS fd limit. |

---

## Sources

- Codebase direct inspection: `pkg/warplib/dloader.go`, `internal/api/download.go`, `internal/server/web.go`, `internal/server/server.go`, `pkg/warplib/handlers.go`, `common/types.go` — HIGH confidence
- `github.com/jlaffaye/ftp` pkg.go.dev documentation — HIGH confidence. `RetrFrom(path, offset)` confirmed for resume. TLS via `DialWithTLS` confirmed. — https://pkg.go.dev/github.com/jlaffaye/ftp
- `github.com/pkg/sftp` v1.13.10 pkg.go.dev documentation — HIGH confidence. `ReadAt` and `Seek` confirmed. Requires `golang.org/x/crypto/ssh`. — https://pkg.go.dev/github.com/pkg/sftp
- `net/http` redirect behavior — HIGH confidence (stdlib, Go 1.24+). Default: follows up to 10 redirects. `CheckRedirect=nil` means follow. — https://pkg.go.dev/net/http
- aria2 JSON-RPC method reference — MEDIUM confidence (external project, for naming inspiration only). — https://aria2.github.io/manual/en/html/aria2c.html

---
*Architecture research for: WarpDL protocol expansion milestone*
*Researched: 2026-02-26*
