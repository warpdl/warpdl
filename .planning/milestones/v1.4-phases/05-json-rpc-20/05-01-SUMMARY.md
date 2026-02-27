---
phase: 05-json-rpc-20
plan: 01
subsystem: daemon
tags: [go, jsonrpc, http, auth, localhost]
requires:
  - phase: 02-protocol-interface
    provides: "SchemeRouter for protocol dispatch in download.add"
provides:
  - "JSON-RPC 2.0 HTTP endpoint at /jsonrpc"
  - "Auth token middleware (requireToken)"
  - "Localhost binding by default, --rpc-listen-all for all interfaces"
  - "system.getVersion method"
  - "Standard JSON-RPC 2.0 error codes (-32700, -32601, -32602)"
tech-stack:
  added: [github.com/creachadair/jrpc2 v1.3.4]
  patterns: [middleware auth, jrpc2 handler map, localhost binding]
key-files:
  created:
    - internal/server/rpc_auth.go
    - internal/server/rpc_auth_test.go
    - internal/server/rpc_methods.go
    - internal/server/rpc_methods_test.go
  modified:
    - internal/server/web.go
    - internal/server/web_test.go
    - internal/server/server.go
    - cmd/cmd.go
    - cmd/daemon_core.go
    - go.mod
    - go.sum
key-decisions:
  - "Uses creachadair/jrpc2 (not gorilla/rpc or net/rpc) for full JSON-RPC 2.0 compliance"
  - "Auth token via --rpc-secret flag and WARPDL_RPC_SECRET env var"
  - "Localhost binding default protects against browser tab CSRF attacks (CVE-2025-52882 precedent)"
requirements-completed: [RPC-01, RPC-03, RPC-04]
duration: 30min
completed: 2026-02-27
---

# Plan 05-01: JSON-RPC 2.0 HTTP Endpoint Summary

**JSON-RPC 2.0 HTTP endpoint at /jsonrpc with auth token middleware, localhost binding, and system.getVersion method via creachadair/jrpc2**

## Performance

- **Duration:** ~30 min
- **Completed:** 2026-02-27
- **Tasks:** TDD cycle (RED/GREEN/REFACTOR)
- **Files created:** 4
- **Files modified:** 7

## Accomplishments

- HTTP endpoint registered at /jsonrpc on existing web server (port+1)
- Auth token required via requireToken middleware on every request
- Localhost binding by default (127.0.0.1), --rpc-listen-all opt-in for all interfaces
- system.getVersion returns daemon version info
- Standard JSON-RPC 2.0 error codes: -32700 (parse error), -32601 (method not found), -32602 (invalid params)
- --rpc-secret flag and WARPDL_RPC_SECRET env var for token configuration

## Task Commits

1. **JSON-RPC HTTP endpoint with auth** - `a1484b2` (feat: add JSON-RPC 2.0 HTTP endpoint with auth and localhost binding)

## Files Created/Modified

- `internal/server/rpc_auth.go` - requireToken middleware for auth enforcement
- `internal/server/rpc_auth_test.go` - Auth middleware tests (no token, wrong token, correct token)
- `internal/server/rpc_methods.go` - system.getVersion method, jrpc2 handler map
- `internal/server/rpc_methods_test.go` - Method tests
- `internal/server/web.go` - /jsonrpc route registration
- `internal/server/web_test.go` - Endpoint integration tests
- `internal/server/server.go` - RPC config fields (secret, listenAll)
- `cmd/cmd.go` - --rpc-secret and --rpc-listen-all flags
- `cmd/daemon_core.go` - RPC config threading to WebServer
- `go.mod` / `go.sum` - github.com/creachadair/jrpc2 v1.3.4

## Decisions Made

- creachadair/jrpc2 chosen for full JSON-RPC 2.0 compliance (not gorilla/rpc -- limited, not net/rpc -- different wire format)
- Localhost binding default protects against CSRF from browser tabs (CVE-2025-52882 precedent)
- Auth token is required on every request (no anonymous access)

## Deviations from Plan

None - plan executed as specified.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- HTTP endpoint ready for method suite (05-02)
- Auth middleware reused by WebSocket endpoint (05-03)
- system.getVersion available immediately

---
*Phase: 05-json-rpc-20, Plan: 01*
*Completed: 2026-02-27*
