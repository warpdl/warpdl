---
phase: 05-json-rpc-20
verified: 2026-02-27
result: PASS
requirements-verified: [RPC-01, RPC-02, RPC-03, RPC-04, RPC-05, RPC-06, RPC-07, RPC-08, RPC-09, RPC-10, RPC-11, RPC-12]
---

# Phase 5: JSON-RPC 2.0 -- Verification

## Success Criteria

### SC1 (RPC-01, RPC-05): curl POST to /jsonrpc with download.add starts download and returns ID

**Result: PASS**

Evidence:
- `internal/server/rpc_methods.go` -- downloadAdd method accepts URL+options, starts download, returns GID
- `internal/server/rpc_integration_test.go` -- 14 E2E tests including download.add lifecycle
- HTTP endpoint at /jsonrpc registered on existing web server (port+1)

### SC2 (RPC-02, RPC-11): WebSocket at /jsonrpc/ws receives real-time progress notifications

**Result: PASS**

Evidence:
- `internal/server/rpc_notify.go` -- RPCNotifier broadcasts download.started/progress/complete/error
- `internal/server/rpc_integration_test.go` -- WebSocket notification test verifies real-time delivery
- download.add handler wires event callbacks to notifier.Broadcast
- Thread-safe registry of jrpc2.Server instances

Note: RPC-11 had a defect in the resume path (no push notifications for resumed downloads) -- fixed in Phase 6.

### SC3 (RPC-03): Auth token required; bad/missing token returns error

**Result: PASS**

Evidence:
- `internal/server/rpc_auth.go` -- requireToken middleware enforces auth on every request
- Integration tests verify 401 for missing token, wrong token, and correct token acceptance
- WebSocket upgrade also requires auth token

### SC4 (RPC-04): Localhost binding by default; --rpc-listen-all for all interfaces

**Result: PASS**

Evidence:
- WebServer.addr() returns 127.0.0.1:<port> by default
- --rpc-listen-all flag switches to 0.0.0.0:<port>
- Integration test verifies localhost binding behavior

### SC5 (RPC-05, RPC-07, RPC-08, RPC-09, RPC-10): Method suite returns correct responses

**Result: PASS**

Evidence:
- All methods in rpc_methods.go: download.add, download.pause, download.resume, download.remove, download.status, download.list, system.getVersion
- Full lifecycle test: add -> complete -> remove -> verify gone
- Concurrent downloads test: 2 parallel download.add calls both complete

### SC6 (RPC-12): Malformed requests return standard JSON-RPC error codes

**Result: PASS**

Evidence:
- Error codes: -32700 (parse error), -32601 (method not found), -32602 (invalid params), -32001 (download not found), -32002 (download not running)
- Integration tests verify each error code scenario

## Requirements Verification

| Requirement | Status | Evidence |
|-------------|--------|----------|
| RPC-01 | PASS | HTTP endpoint at /jsonrpc on port+1; 14 integration tests |
| RPC-02 | PASS | WebSocket at /jsonrpc/ws; RPCNotifier broadcasts notifications |
| RPC-03 | PASS | requireToken middleware; 401 for missing/wrong token |
| RPC-04 | PASS | 127.0.0.1 default; --rpc-listen-all for 0.0.0.0 |
| RPC-05 | PASS | download.add accepts URL+options, returns GID |
| RPC-06 | PASS | download.pause/resume control lifecycle; resume notifications fixed in Phase 6 |
| RPC-07 | PASS | download.remove clears from manager |
| RPC-08 | PASS | download.status returns state, totalLength, completedLength, speed |
| RPC-09 | PASS | download.list filters by active/waiting/stopped |
| RPC-10 | PASS | system.getVersion returns daemon version info |
| RPC-11 | PASS | Push notifications for all 4 event types; resume path fixed in Phase 6 |
| RPC-12 | PASS | Standard error codes (-32700, -32601, -32602) plus custom (-32001, -32002) |

Note: RPC-06 and RPC-11 were initially implemented in Phase 5 but had defects (resume path lacked push notification wiring). Both defects were fixed in Phase 6.

## Gate Results

- All tests pass: YES (`go test ./...` -- 19 packages, 0 failures)
- Race detection: CLEAN (`go test -race -short ./...`)
- Build: CLEAN (`go build -ldflags="-w -s" .`)
- Vet: CLEAN (`go vet ./...`)
- Coverage: >= 80% per package (server: 84.9%)

## Files Modified

### Plan 05-01 (HTTP endpoint + auth)
- `internal/server/rpc_auth.go` -- requireToken middleware
- `internal/server/rpc_auth_test.go` -- Auth tests
- `internal/server/rpc_methods.go` -- system.getVersion, handler map
- `internal/server/rpc_methods_test.go` -- Method tests
- `internal/server/web.go` -- /jsonrpc route registration
- `internal/server/server.go` -- RPC config fields
- `cmd/cmd.go` -- --rpc-secret, --rpc-listen-all flags
- `cmd/daemon_core.go` -- RPC config threading
- `go.mod` / `go.sum` -- creachadair/jrpc2 v1.3.4

### Plan 05-02 (Method suite)
- `internal/server/rpc_methods.go` -- download.add/pause/resume/remove/status/list
- `internal/server/rpc_methods_test.go` -- Unit tests per method
- `internal/server/web.go` -- Method registration
- `internal/server/server.go` -- Server config
- `cmd/daemon_core.go` -- Manager/SchemeRouter threading

### Plan 05-03 (WebSocket + push notifications)
- `internal/server/rpc_notify.go` -- RPCNotifier, broadcast registry
- `internal/server/rpc_notify_test.go` -- Notification tests
- `internal/server/rpc_methods.go` -- Callback wiring in download.add
- `internal/server/web.go` -- /jsonrpc/ws route
- `internal/server/server.go` -- Notifier lifecycle

### Plan 05-04 (Integration tests + race fixes)
- `internal/server/rpc_integration_test.go` -- 14 E2E tests
- `pkg/warplib/item.go` -- GetDownloaded(), GetTotalSize() thread-safe getters
- `pkg/warplib/manager.go` -- Locked part.Compiled mutation
- `internal/server/rpc_methods.go` -- Thread-safe getter usage
- `internal/server/web_test.go` -- Test isolation (t.Chdir)

---
*Verified: 2026-02-27*
