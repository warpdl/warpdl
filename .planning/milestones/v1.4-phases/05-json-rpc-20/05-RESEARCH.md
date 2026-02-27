# Phase 5: JSON-RPC 2.0 - Research

**Researched:** 2026-02-27
**Domain:** JSON-RPC 2.0 over HTTP/WebSocket in Go, auth token middleware, real-time push notifications
**Confidence:** HIGH

## Summary

Phase 5 adds a JSON-RPC 2.0 API on top of the existing HTTP web server (`port+1`) already running in WarpDL's daemon. The web server is a standard `net/http` server started in `internal/server/web.go`, currently serving only one WebSocket endpoint for browser extension capture. The JSON-RPC endpoints need to be grafted onto that same `http.ServeMux` (or replaced with a multi-route mux), adding two routes: `POST /jsonrpc` (HTTP transport) and `GET /jsonrpc/ws` (WebSocket transport with Upgrade).

The canonical library stack is already decided in STATE.md: `github.com/creachadair/jrpc2` v1.3.4 for the RPC core, `github.com/coder/websocket` v1.8.14 for WebSocket (already the decided WebSocket library), and `github.com/creachadair/wschannel` (which directly wraps coder/websocket) for the WebSocket-to-jrpc2 bridge. This is the exact stack aria2 and similar tools use conceptually: token-in-params for HTTP, token-in-header for WebSocket upgrade. The architecture requires implementing auth middleware on the HTTP layer, not inside jrpc2 handlers.

The critical security point flagged in STATE.md stands: localhost binding alone does not prevent browser CSRF attacks against the WebSocket endpoint (CVE-2025-52882 precedent). The `wschannel.ListenOptions.CheckAccept` hook is the exact mechanism to reject WebSocket upgrades missing the auth token header. For HTTP POST, a standard `http.Handler` wrapper checks the token before delegating to `jhttp.Bridge`.

**Primary recommendation:** Use `jrpc2` + `jhttp.Bridge` for HTTP POST and `wschannel.Listener` with `CheckAccept` for WebSocket; wire both into the existing `WebServer.handler()` mux; thread `--rpc-secret` and `--rpc-listen-all` through `initDaemonComponents`; implement six RPC methods as thin wrappers over existing `Api` logic.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| RPC-01 | Daemon exposes JSON-RPC 2.0 endpoint over HTTP at `/jsonrpc` on port+1 | jhttp.Bridge is an http.Handler; mount on existing WebServer mux |
| RPC-02 | Daemon exposes WebSocket endpoint at `/jsonrpc/ws` | wschannel.Listener is an http.Handler; mount same mux |
| RPC-03 | Auth token required for all RPC requests (`--rpc-secret` flag, `WARPDL_RPC_SECRET` env) | HTTP middleware wrapper + wschannel CheckAccept hook; secret threaded from CLI flag to WebServer |
| RPC-04 | RPC binds to localhost only by default; `--rpc-listen-all` opts into all interfaces | WebServer.addr() currently hardcodes `:port`; needs host prefix "127.0.0.1:port" or "0.0.0.0:port" |
| RPC-05 | `download.add` starts a download, returns download ID | Reuse downloadHandler logic; construct DownloadParams; call Manager.AddDownload |
| RPC-06 | `download.pause` and `download.resume` control active downloads | Reuse stopHandler/resumeHandler logic; pool.HasDownload check |
| RPC-07 | `download.remove` removes download from queue | Manager.FlushItem or equivalent; check item existence |
| RPC-08 | `download.status` returns state, totalLength, completedLength, speed | Item.IsDownloading, Item.TotalSize, Item.Downloaded, Item.GetPercentage |
| RPC-09 | `download.list` returns downloads by state (active/waiting/stopped) | Manager.GetItems/GetIncompleteItems/GetCompletedItems filter by state |
| RPC-10 | `system.getVersion` returns daemon version info | Api.version/commit/buildType fields already populated |
| RPC-11 | WebSocket pushes real-time notifications (started/progress/complete/error) | wschannel conn registry; jrpc2 AllowPush + server.Push or manual JSON writes |
| RPC-12 | Standard JSON-RPC 2.0 error codes for parse/invalid/method-not-found errors | jrpc2 returns these automatically; custom codes use jrpc2.Error{Code: -32000...} |
</phase_requirements>

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/creachadair/jrpc2` | v1.3.4 | JSON-RPC 2.0 server/client runtime | Mature, stable API, active, BSD-3. Already decided in STATE.md. |
| `github.com/creachadair/jrpc2/jhttp` | (included in jrpc2) | HTTP-to-jrpc2 Bridge (http.Handler) | Single-call setup, handles framing, batching, content-type validation |
| `github.com/creachadair/jrpc2/handler` | (included in jrpc2) | handler.Map, handler.New function adapters | Converts plain Go functions to jrpc2.Handler without boilerplate |
| `github.com/coder/websocket` | v1.8.14 | WebSocket transport | Already in go.mod decision; non-archived replacement for gorilla/websocket |
| `github.com/creachadair/wschannel` | latest (uses jrpc2 v1.3.4 + coder/websocket v1.8.14) | WebSocket channel for jrpc2 | Directly wraps coder/websocket to implement jrpc2 Channel interface |

### Supporting

| Library | Purpose | When to Use |
|---------|---------|-------------|
| `net/http` stdlib | ServeMux routing, auth middleware | Always |
| `crypto/subtle` | Constant-time token comparison | Auth middleware to prevent timing attacks |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `creachadair/wschannel` | Roll WebSocket+jrpc2 manually | wschannel is 100 lines; rolling it adds test surface and correctness risk |
| `jhttp.Bridge` | Roll HTTP JSON-RPC handler | Bridge handles ID virtualization, batching edge cases, content-type enforcement |
| `gorilla/websocket` | `coder/websocket` | gorilla is archived; coder is the actively maintained fork. Already decided. |

**Installation:**
```bash
go get github.com/creachadair/jrpc2@v1.3.4
go get github.com/creachadair/wschannel@latest
# github.com/coder/websocket already in go.sum from wschannel transitive dep
```

## Architecture Patterns

### Recommended Project Structure

```
internal/
├── server/
│   ├── web.go          # MODIFY: add RPCConfig, update handler() and addr()
│   ├── rpc/            # NEW package
│   │   ├── rpc.go      # RPCServer struct, NewRPCServer, Mount(mux)
│   │   ├── methods.go  # download.add/pause/resume/remove/status/list + system.getVersion
│   │   ├── notify.go   # WebSocket notification broadcaster
│   │   ├── auth.go     # requireToken middleware (HTTP) + checkAccept (WS)
│   │   └── rpc_test.go # tests
cmd/
│   ├── cmd.go          # ADD --rpc-secret, --rpc-listen-all flags to daemon command
│   ├── daemon_core.go  # MODIFY: pass RPCConfig to initDaemonComponents
```

OR alternatively (simpler, lower package count):

```
internal/server/
├── web.go              # MODIFY: add RPCConfig, HTTP middleware, mux routing
├── rpc_methods.go      # NEW: jrpc2 handler functions
├── rpc_notify.go       # NEW: WebSocket broadcaster
├── rpc_test.go         # NEW: tests
```

The simpler approach (all in `internal/server`) is preferred given the existing pattern where `web.go` already contains the WebServer that serves `port+1`. Adding RPC methods alongside it avoids a new package import cycle (api package already imports server package).

### Pattern 1: HTTP Bridge Setup

**What:** `jhttp.NewBridge` wraps a `handler.Map` and becomes an `http.Handler` mounted at `/jsonrpc`.
**When to use:** All HTTP POST JSON-RPC requests.

```go
// Source: https://pkg.go.dev/github.com/creachadair/jrpc2/jhttp
import (
    "github.com/creachadair/jrpc2"
    "github.com/creachadair/jrpc2/handler"
    "github.com/creachadair/jrpc2/jhttp"
)

methods := handler.Map{
    "download.add":    handler.New(rpcServer.downloadAdd),
    "download.status": handler.New(rpcServer.downloadStatus),
    "download.list":   handler.New(rpcServer.downloadList),
    "download.pause":  handler.New(rpcServer.downloadPause),
    "download.resume": handler.New(rpcServer.downloadResume),
    "download.remove": handler.New(rpcServer.downloadRemove),
    "system.getVersion": handler.New(rpcServer.systemGetVersion),
}

bridge := jhttp.NewBridge(methods, nil)
// mount with auth middleware:
mux.Handle("/jsonrpc", requireToken(secret, bridge))
```

### Pattern 2: WebSocket Listener Setup

**What:** `wschannel.NewListener` is an `http.Handler` that upgrades connections to WebSocket and feeds channels to a jrpc2 server loop.
**When to use:** WebSocket endpoint at `/jsonrpc/ws`.

```go
// Source: https://github.com/creachadair/wschannel/blob/main/listener.go
import "github.com/creachadair/wschannel"

lst := wschannel.NewListener(&wschannel.ListenOptions{
    MaxPending: 16,
    CheckAccept: func(req *http.Request) (int, error) {
        // Auth during WebSocket upgrade (only chance — header not available after)
        if !validToken(secret, req.Header.Get("Authorization")) {
            return http.StatusUnauthorized, errors.New("unauthorized")
        }
        return 0, nil
    },
})

// Accept connections and serve jrpc2 over each
go func() {
    for {
        ch, err := lst.Accept(ctx)
        if err != nil { break }
        go jrpc2.NewServer(methods, &jrpc2.ServerOptions{
            AllowPush: true,
        }).Start(ch)
    }
}()

mux.Handle("/jsonrpc/ws", lst)
```

### Pattern 3: Auth Middleware (HTTP)

**What:** Thin `http.Handler` wrapper that checks `Authorization: Bearer <token>` header before passing to bridge.
**When to use:** All HTTP POST requests to `/jsonrpc`.

```go
// Source: standard Go HTTP middleware pattern
func requireToken(secret string, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        auth := r.Header.Get("Authorization")
        token := strings.TrimPrefix(auth, "Bearer ")
        if secret != "" && !subtle.ConstantTimeCompare([]byte(token), []byte(secret)) {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusUnauthorized)
            // Return proper JSON-RPC 2.0 error (id=null since request not parsed yet)
            json.NewEncoder(w).Encode(map[string]any{
                "jsonrpc": "2.0",
                "error": map[string]any{
                    "code":    -32600, // Invalid Request
                    "message": "Unauthorized",
                },
                "id": nil,
            })
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Pattern 4: Real-Time WebSocket Notifications (RPC-11)

**What:** Push-based notifications sent from download event handlers to all connected WebSocket jrpc2 sessions.
**When to use:** `download.progress`, `download.complete`, `download.error`, `download.started` notifications.

jrpc2 with `AllowPush: true` supports server-initiated push via `jrpc2.ServerPush`. However, each WebSocket connection spawns its own `jrpc2.Server` instance, making a centralized broadcaster pattern necessary.

```go
// Broadcaster pattern - registry of active WS servers
type wsNotifier struct {
    mu      sync.RWMutex
    servers map[string]*jrpc2.Server // downloadID -> servers set (actually set of all servers)
}

// Called from download event handlers
func (n *wsNotifier) Broadcast(method string, params any) {
    n.mu.RLock()
    defer n.mu.RUnlock()
    for _, srv := range n.servers {
        srv.Push(context.Background(), method, params)
    }
}
```

**Alternative:** Write raw JSON notifications directly to WebSocket connections managed by `wschannel`, bypassing jrpc2 push. This is lower complexity but bypasses jrpc2's push semantics. Given the download event handlers already exist in `internal/api/download.go` and write to `server.Pool`, the cleanest approach is to wire the `wsNotifier.Broadcast` into those same handlers as an additional callback, OR to have the WebSocket notification layer subscribe to pool broadcasts independently.

**Recommended approach for RPC-11:** Add a `notifier` interface to WebServer/RPCServer that the download handlers call after pool.Broadcast, sending JSON-RPC notifications in the format:
```json
{"jsonrpc":"2.0","method":"download.progress","params":{"id":"abc","completedLength":1234,"speed":50000}}
```

### Pattern 5: Handler Function Signatures

jrpc2 `handler.New` accepts functions with these signatures (source: pkg.go.dev/github.com/creachadair/jrpc2/handler):

```go
// Named-params object (recommended for named JSON parameters)
func(ctx context.Context, params MyParamsStruct) (MyResultStruct, error)

// No params
func(ctx context.Context) (MyResultStruct, error)

// Raw request access (for custom param parsing)
func(ctx context.Context, req *jrpc2.Request) (any, error)
```

JSON-RPC 2.0 error return uses `*jrpc2.Error`:
```go
return nil, &jrpc2.Error{Code: jrpc2.SystemError(1), Message: "download not found"}
// OR use predefined codes:
return nil, jrpc2.Errorf(jrpc2.ErrMethodNotFound, "unknown method")
```

### Anti-Patterns to Avoid

- **Token in URL query param for WebSocket:** Do not pass secret as `?token=...`. It ends up in server logs, proxy logs, and browser history. Use `Authorization: Bearer` header during the HTTP upgrade request.
- **Token in jrpc2 params:** aria2 uses `token:secret` as first param. This is legacy design. Modern Go daemons use HTTP middleware, not per-method token checking.
- **Rolling WebSocket-to-jrpc2 bridge manually:** wschannel exists precisely to avoid this.
- **CORS wildcard (`*`):** Do not set `OriginPatterns: []string{"*"}` in `websocket.Accept`. The localhost binding provides protection, but a wildcard CORS header removes it.
- **Gorilla/websocket:** Archived. Use coder/websocket. Already decided.
- **Blocking goroutines per method:** jrpc2 handles concurrency internally. Don't block the handler goroutine.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| JSON-RPC 2.0 framing, error codes, ID echo | Custom JSON marshaler | `creachadair/jrpc2` | ID virtualization, batch handling, standard error codes |
| WebSocket upgrade + jrpc2 channel bridge | `websocket.Accept` + manual read/write loop | `creachadair/wschannel` | wschannel is 100 lines of correct code already tested |
| HTTP POST to jrpc2 bridge | Decode JSON, dispatch method, encode response | `jhttp.NewBridge` | Handles content-type validation, 405 on non-POST, 204 on notifications, batch requests |

**Key insight:** The combination of `jhttp.Bridge` (HTTP) + `wschannel.Listener` (WebSocket) is the officially designed integration point for creachadair/jrpc2. Don't fragment this.

## Common Pitfalls

### Pitfall 1: WebSocket CSRF via Browser Tab

**What goes wrong:** localhost binding alone doesn't prevent a browser tab from connecting to `ws://localhost:<port+1>/jsonrpc/ws` via JavaScript. A malicious page can exfiltrate data or trigger downloads.
**Why it happens:** Browsers send cookies but also allow WebSocket connections cross-origin to localhost.
**How to avoid:** Use `wschannel.ListenOptions.CheckAccept` to validate the `Authorization: Bearer <secret>` header during the HTTP upgrade. The `coder/websocket.Accept` does NOT verify origin by default when `OriginPatterns` is nil or empty — it will return an error if origin doesn't match. Explicitly set `OriginPatterns: []string{"127.0.0.1", "localhost"}` or rely purely on the token.
**Warning signs:** STATE.md already flagged this as "(Phase 5 security): JSON-RPC WebSocket CSRF". Do not skip the CheckAccept hook.

### Pitfall 2: WebServer.addr() is bind-all by default

**What goes wrong:** Current `WebServer.addr()` returns `fmt.Sprintf(":%d", s.port)` — this binds to all interfaces. RPC-04 requires localhost-only by default.
**Why it happens:** Existing browser capture WebSocket has no security; the current web.go doesn't need host restriction because it has no auth.
**How to avoid:** Add `listenAll bool` field to WebServer. When false (default), `addr()` returns `127.0.0.1:<port>`. When true, returns `:<port>`. The `--rpc-listen-all` flag sets this. **Existing tests** for WebServer.addr() hardcode `:9999` — they will need updating.
**Warning signs:** Check `web_test.go:TestWebServerAddr` — currently asserts `":9999"`. Must update.

### Pitfall 3: jrpc2 Server vs. Bridge Lifecycle

**What goes wrong:** `jhttp.Bridge` creates an internal jrpc2 Server. Calling `bridge.Close()` is required during daemon shutdown. Missing this leaks goroutines.
**Why it happens:** Bridge's internal server goroutine keeps running.
**How to avoid:** Call `bridge.Close()` in `WebServer.Shutdown()`. Same for `wschannel.Listener.Close()` to drain pending connections.

### Pitfall 4: jrpc2 Error Codes for Standard Cases

**What goes wrong:** Returning a plain `error` from a handler method produces an Internal Error (-32603). For "download not found", the correct code is -32000 (server-defined), not -32603 (internal).
**Why it happens:** jrpc2 wraps non-`*jrpc2.Error` returns as -32603 by default.
**How to avoid:** Use `*jrpc2.Error{Code: jrpc2.Code(-32001), Message: "download not found"}` for semantic errors. Reserve -32603 for actual internal errors (panics, bugs).

Standard codes to use:
```
-32700  Parse error        (jrpc2 automatic)
-32600  Invalid Request    (jrpc2 automatic)
-32601  Method not found   (jrpc2 automatic)
-32602  Invalid params     (return when required param is missing)
-32603  Internal error     (jrpc2 automatic for non-jrpc2.Error returns)
-32001  Download not found (custom server error)
-32002  Download not running (custom server error)
```

### Pitfall 5: Pool.Broadcast vs. RPC Notifications are different channels

**What goes wrong:** `server.Pool.Broadcast` writes to CLI client `SyncConn` connections (the Unix socket protocol), NOT to WebSocket connections. The RPC WebSocket clients need a separate notification path.
**Why it happens:** The existing Pool is wired for the binary CLI protocol, not JSON-RPC.
**How to avoid:** Create a separate `RPCNotifier` that maintains a set of active jrpc2.Server instances (one per WebSocket connection) and calls `server.Push()` on each. The download event handlers in `internal/api/download.go` must call BOTH `pool.Broadcast` (existing CLI clients) AND `rpcNotifier.Notify` (new WebSocket clients). This requires passing `notifier` into the Api struct or into `initDaemonComponents`.

### Pitfall 6: go.mod Requires Go 1.25 for wschannel

**What goes wrong:** wschannel's `go.mod` requires Go 1.25. WarpDL's `go.mod` declares Go 1.24.9. Using wschannel as a dependency will cause `go get` to fail or require upgrading the module's Go directive.
**Why it happens:** wschannel uses language features from Go 1.25.
**How to avoid:** Check whether wschannel actually uses Go 1.25 features or just declares it unnecessarily. If it does use 1.25 features, the option is: (a) bump WarpDL go.mod to `go 1.25` (requires CI toolchain update), or (b) copy wschannel's ~100 lines of code directly into `internal/server` (it's BSD-3-licensed, tiny, and has no internal dependencies beyond coder/websocket). Option (b) avoids a toolchain dependency risk and is practical given the library size.
**Warning signs:** Run `go get github.com/creachadair/wschannel@latest` during Wave 0 setup task and check for errors.

### Pitfall 7: Timing Attack on Token Comparison

**What goes wrong:** Using `secret != token` is a string comparison that short-circuits, creating a timing oracle that leaks token length/prefix.
**Why it happens:** Default string equality in Go.
**How to avoid:** Use `subtle.ConstantTimeCompare([]byte(secret), []byte(token)) == 1`.

## Code Examples

### download.add Handler

```go
// Source: jrpc2 handler.New pattern + existing warplib.Manager API
type AddParams struct {
    URL               string            `json:"url"`
    FileName          string            `json:"fileName,omitempty"`
    DownloadDirectory string            `json:"dir,omitempty"`
    Headers           warplib.Headers   `json:"headers,omitempty"`
    MaxConnections    int32             `json:"connections,omitempty"`
}

type AddResult struct {
    GID string `json:"gid"` // download ID, mirrors aria2 naming for compatibility
}

func (rs *RPCServer) downloadAdd(ctx context.Context, p AddParams) (*AddResult, error) {
    d, err := warplib.NewDownloader(rs.client, p.URL, &warplib.DownloaderOpts{
        FileName:          p.FileName,
        DownloadDirectory: p.DownloadDirectory,
        MaxConnections:    p.MaxConnections,
        Headers:           p.Headers,
        Handlers:          rs.buildHandlers(d), // calls notifier.Notify on events
    })
    if err != nil {
        return nil, &jrpc2.Error{Code: jrpc2.Code(-32602), Message: err.Error()}
    }
    if err := rs.manager.AddDownload(d, nil); err != nil {
        return nil, err
    }
    go d.Start()
    return &AddResult{GID: d.GetHash()}, nil
}
```

### download.status Handler

```go
type StatusResult struct {
    GID             string `json:"gid"`
    Status          string `json:"status"`   // "active", "waiting", "complete", "error"
    TotalLength     int64  `json:"totalLength"`
    CompletedLength int64  `json:"completedLength"`
    Percentage      int64  `json:"percentage"`
}

func (rs *RPCServer) downloadStatus(ctx context.Context, p struct{ GID string `json:"gid"` }) (*StatusResult, error) {
    item := rs.manager.GetItem(p.GID)
    if item == nil {
        return nil, &jrpc2.Error{Code: -32001, Message: "download not found"}
    }
    status := "waiting"
    if item.IsDownloading() {
        status = "active"
    } else if item.Downloaded >= item.TotalSize && item.TotalSize > 0 {
        status = "complete"
    }
    return &StatusResult{
        GID:             item.Hash,
        Status:          status,
        TotalLength:     int64(item.TotalSize),
        CompletedLength: int64(item.Downloaded),
        Percentage:      item.GetPercentage(),
    }, nil
}
```

### system.getVersion Handler

```go
type VersionResult struct {
    Version string `json:"version"`
    Commit  string `json:"commit,omitempty"`
}

func (rs *RPCServer) systemGetVersion(ctx context.Context) (*VersionResult, error) {
    return &VersionResult{Version: rs.version, Commit: rs.commit}, nil
}
```

### WebServer mux update

```go
// In WebServer.handler() — MODIFIED to support multiple routes
func (s *WebServer) handler() http.Handler {
    mux := http.NewServeMux()
    // Existing browser extension capture endpoint
    mux.Handle("/", websocket.Handler(s.handleConnection))

    if s.rpc != nil {
        // HTTP JSON-RPC endpoint
        mux.Handle("/jsonrpc", requireToken(s.rpc.secret, s.rpc.bridge))
        // WebSocket JSON-RPC endpoint
        mux.Handle("/jsonrpc/ws", s.rpc.listener)
    }
    return mux
}
```

### go get commands for new deps

```bash
# In the worktree directory
go get github.com/creachadair/jrpc2@v1.3.4
# If wschannel Go 1.25 issue applies, inline the code instead:
go get github.com/creachadair/wschannel@latest
# coder/websocket is already a transitive dep but pin explicitly:
go get github.com/coder/websocket@v1.8.14
go mod tidy
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `gorilla/websocket` | `coder/websocket` (nhooyr fork) | 2023 (gorilla archived) | Must use coder; already decided |
| Hand-rolled JSON-RPC dispatch | `creachadair/jrpc2` library | 2024+ | Library handles all spec edge cases |
| Token-in-params (aria2 style) | Bearer token in HTTP header | 2024 best practice | Prevents CSRF and log leakage |

**Deprecated/outdated:**
- `gorilla/websocket`: Archived 2022. `coder/websocket` is the drop-in.
- `golang.org/x/net/websocket`: Already in use in existing `web.go` for browser capture. This is the old stdlib websocket — it's fine for the existing browser capture endpoint but NOT suitable for the new JSON-RPC WebSocket (no context support, no binary frame support that wschannel needs).

## Open Questions

1. **wschannel Go 1.25 requirement**
   - What we know: wschannel's go.mod declares `go 1.25`; WarpDL currently uses `go 1.24.9`
   - What's unclear: Does wschannel actually use Go 1.25 language features or just the directive?
   - Recommendation: Attempt `go get github.com/creachadair/wschannel@latest` as first step of Wave 0. If it fails, inline the ~100 lines of wschannel code (it's MIT/BSD, tiny). The Channel, Listener, and Dial functions are straightforward wrappers around coder/websocket.

2. **RPC-11 notification threading through existing event handlers**
   - What we know: download.go handlers already call `pool.Broadcast` (CLI binary protocol). RPC WebSocket clients need JSON-RPC push notifications.
   - What's unclear: Whether to modify `Api.downloadHandler` to also call a notifier, or build a pool bridge that intercepts broadcasts and re-emits as JSON-RPC.
   - Recommendation: Add an optional `RPCNotifier` interface to the Api struct. When set, download/progress/complete/error handlers call `notifier.Push(method, params)` after calling `pool.Broadcast`. This is the minimal-diff approach.

3. **download.pause semantics**
   - What we know: The existing `stopHandler` stops (not pauses) a download. The CLI doesn't distinguish pause vs. stop at the API level.
   - What's unclear: Should `download.pause` map to `item.StopDownload()` (same as stop) and `download.resume` to the existing `resumeHandler` path?
   - Recommendation: Yes. `download.pause` = stop the in-progress downloader (state persisted), `download.resume` = re-initiate from persisted state. This matches aria2 semantics.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | `testing` stdlib + `net/http/httptest` |
| Config file | None (no framework config) |
| Quick run command | `go test -run TestRPC ./internal/server/...` |
| Full suite command | `go test -count=1 ./...` |

### Phase Requirements to Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| RPC-01 | POST /jsonrpc returns JSON-RPC 2.0 response | integration | `go test -run TestRPCHTTPBridge ./internal/server/...` | Wave 0 |
| RPC-02 | WS /jsonrpc/ws accepts upgrade and returns response | integration | `go test -run TestRPCWebSocket ./internal/server/...` | Wave 0 |
| RPC-03 | Request without token returns 401/error | unit | `go test -run TestRPCAuth ./internal/server/...` | Wave 0 |
| RPC-04 | Default bind is 127.0.0.1 not 0.0.0.0 | unit | `go test -run TestWebServerAddr ./internal/server/...` | MODIFY existing |
| RPC-05 | download.add creates download, returns GID | integration | `go test -run TestRPCDownloadAdd ./internal/server/...` | Wave 0 |
| RPC-06 | download.pause stops; download.resume restarts | integration | `go test -run TestRPCDownloadPauseResume ./internal/server/...` | Wave 0 |
| RPC-07 | download.remove removes from manager | integration | `go test -run TestRPCDownloadRemove ./internal/server/...` | Wave 0 |
| RPC-08 | download.status returns correct fields | unit | `go test -run TestRPCDownloadStatus ./internal/server/...` | Wave 0 |
| RPC-09 | download.list filters by state | unit | `go test -run TestRPCDownloadList ./internal/server/...` | Wave 0 |
| RPC-10 | system.getVersion returns version | unit | `go test -run TestRPCSystemGetVersion ./internal/server/...` | Wave 0 |
| RPC-11 | WS client receives progress notification | integration | `go test -run TestRPCNotify ./internal/server/...` | Wave 0 |
| RPC-12 | Malformed JSON returns -32700; unknown method -32601 | unit | `go test -run TestRPCErrorCodes ./internal/server/...` | Wave 0 |

### Sampling Rate

- **Per task commit:** `go test -race -short ./internal/server/... ./internal/api/...`
- **Per wave merge:** `go test -count=1 ./...`
- **Phase gate:** `scripts/check_coverage.sh` green (80% minimum per package)

### Wave 0 Gaps

- [ ] `internal/server/rpc_test.go` — covers RPC-01 through RPC-12 (or split into rpc_http_test.go, rpc_ws_test.go, rpc_methods_test.go)
- [ ] Resolve wschannel Go 1.25 / inline decision before writing any impl code
- [ ] `go get` jrpc2 + wschannel (or inline) and run `go mod tidy`
- [ ] `web_test.go:TestWebServerAddr` — update to expect `127.0.0.1:9999` instead of `:9999`

## Sources

### Primary (HIGH confidence)

- `/tunnckocore/jsonrpc-v2.0-spec` (Context7) - JSON-RPC 2.0 error codes, request/response format
- `/coder/websocket` (Context7) - WebSocket Accept, OriginPatterns, AcceptOptions, wsjson
- `https://pkg.go.dev/github.com/creachadair/jrpc2@v1.3.4` - Server, Client, handler.Map, handler.New, jrpc2.Error
- `https://pkg.go.dev/github.com/creachadair/jrpc2@v1.3.4/jhttp` - Bridge, NewBridge, BridgeOptions
- `https://github.com/creachadair/wschannel/blob/main/wschannel.go` - Channel, New, DialContext (source-verified)
- `https://github.com/creachadair/wschannel/blob/main/listener.go` - Listener, ServeHTTP, Accept, CheckAccept (source-verified)
- `https://github.com/creachadair/wschannel/blob/main/go.mod` - Module path, version, dependencies (go 1.25 requirement confirmed)

### Secondary (MEDIUM confidence)

- WebSearch: aria2 rpc-secret token format (`token:secret` in params, bearer in headers for modern approach)
- WebSearch: wschannel uses `github.com/coder/websocket` v1.8.14 (confirmed via go.mod)

### Tertiary (LOW confidence)

- None

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Verified via pkg.go.dev, GitHub source, go.mod
- Architecture: HIGH - Based on verified library APIs and existing codebase structure
- Pitfalls: HIGH - Security pitfalls from STATE.md + source-verified library behavior; Go 1.25 issue from actual go.mod content

**Research date:** 2026-02-27
**Valid until:** 2026-03-27 (libraries are stable; jrpc2 has semantic versioning guarantees from v1.0.0)
