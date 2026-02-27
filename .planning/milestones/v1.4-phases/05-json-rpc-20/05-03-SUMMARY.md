---
phase: 05-json-rpc-20
plan: 03
subsystem: daemon
tags: [go, jsonrpc, websocket, push-notifications, realtime]
requires:
  - phase: 05-json-rpc-20
    plan: 01
    provides: "JSON-RPC 2.0 HTTP endpoint and auth"
  - phase: 05-json-rpc-20
    plan: 02
    provides: "RPC method suite (download.add triggers notifications)"
provides:
  - "WebSocket endpoint at /jsonrpc/ws"
  - "RPCNotifier for broadcasting push notifications to all connected clients"
  - "Real-time notifications: download.started, download.progress, download.complete, download.error"
tech-stack:
  added: [github.com/coder/websocket v1.8.14]
  patterns: [WebSocket upgrade, server push, broadcast registry]
key-files:
  created:
    - internal/server/rpc_notify.go
    - internal/server/rpc_notify_test.go
  modified:
    - internal/server/rpc_methods.go
    - internal/server/web.go
    - internal/server/server.go
key-decisions:
  - "Uses coder/websocket (not gorilla/websocket — archived)"
  - "RPCNotifier maintains thread-safe registry of jrpc2.Server instances"
  - "Broadcast errors log and unregister disconnected servers (no block/panic)"
requirements-completed: [RPC-02, RPC-11]
duration: 30min
completed: 2026-02-27
---

# Plan 05-03: WebSocket Push Notifications Summary

**WebSocket endpoint at /jsonrpc/ws with RPCNotifier broadcasting download.started/progress/complete/error to all connected clients**

## Performance

- **Duration:** ~30 min
- **Completed:** 2026-02-27
- **Tasks:** TDD cycle (RED/GREEN/REFACTOR)
- **Files created:** 2
- **Files modified:** 3

## Accomplishments

- WebSocket endpoint at /jsonrpc/ws with auth-required upgrade
- RPCNotifier broadcasts real-time notifications to all connected WebSocket clients
- Notification types: download.started, download.progress, download.complete, download.error
- download.add handler wires event callbacks to notifier.Broadcast
- Thread-safe registry of jrpc2.Server instances
- Disconnected servers gracefully unregistered (no block, no panic)

Note: RPC-11 had a defect in the resume path (no push notifications for resumed downloads) -- fixed in Phase 6.

## Task Commits

1. **WebSocket and push notifications** - `b62f9cf` (feat: add WebSocket endpoint with RPCNotifier push notifications)

## Files Created/Modified

- `internal/server/rpc_notify.go` - RPCNotifier, Register/Unregister/Broadcast, thread-safe server registry
- `internal/server/rpc_notify_test.go` - Notification broadcast tests, disconnection handling
- `internal/server/rpc_methods.go` - download.add wires event callbacks to notifier
- `internal/server/web.go` - /jsonrpc/ws route with WebSocket upgrade and auth check
- `internal/server/server.go` - RPCNotifier lifecycle management

## Decisions Made

- coder/websocket chosen over gorilla/websocket (archived, no longer maintained)
- RPCNotifier uses mutex-protected map for server registry (not channel-based -- simpler, sufficient for expected client count)
- Broadcast errors trigger unregister, not panic or block

## Deviations from Plan

None - plan executed as specified.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- WebSocket notifications working for download.add flow
- Plan 05-04 adds integration tests verifying notifications end-to-end
- Phase 6 fixes resume notification defect (download.resume path lacked callback wiring)

---
*Phase: 05-json-rpc-20, Plan: 03*
*Completed: 2026-02-27*
