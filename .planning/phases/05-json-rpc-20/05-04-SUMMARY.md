# Plan 05-04 Summary: Integration Tests and CI Gate

## Status: COMPLETE

## What was done

### Integration test suite (rpc_integration_test.go)
- 14 end-to-end integration tests covering all 12 RPC requirements:
  - RPC-01: HTTP endpoint returns JSON-RPC 2.0 response
  - RPC-02: WebSocket endpoint accepts method calls
  - RPC-03: Auth enforcement for HTTP and WebSocket (no token, wrong token, correct token)
  - RPC-04: Localhost binding verification
  - RPC-05: download.add starts download and returns GID
  - RPC-07: download.remove clears item from manager
  - RPC-08: download.status returns correct fields
  - RPC-09: download.list with status filtering
  - RPC-10: system.getVersion returns configured values
  - RPC-11: WebSocket push notifications (download.started, download.complete)
  - RPC-12: Error codes (-32700, -32601, -32602, -32001)
- Full lifecycle test: add -> complete -> remove -> verify gone
- Concurrent downloads test: 2 parallel download.add calls both complete

### Race condition fixes
- Added `GetDownloaded()` and `GetTotalSize()` thread-safe getters to `warplib.Item`
- Fixed `GetPercentage()` to acquire RLock and guard against division by zero
- Updated `itemStatus()`, `downloadStatus()`, `downloadList()` in rpc_methods.go to use getters
- Fixed `GetIncompleteItems()` and `GetCompletedItems()` in manager.go to use getters
- Fixed `part.Compiled = true` in `patchHandlers` and `patchProtocolHandlers` to run under item lock (race with GOB encoder)
- Pre-set `CheckRedirect` on shared `*http.Client` in tests to avoid race in `NewDownloader`

### Test isolation fixes
- Added `t.Chdir(base)` to web_test.go tests to prevent downloads from polluting the source tree
- Created download directories (`os.MkdirAll(dlDir, 0755)`) in test helpers
- Added `dir` parameter to all download.add calls in unit and integration tests

## Gate verification results
- `go test ./...` -- all 19 packages pass
- `go test -race -short ./...` -- zero data races
- `scripts/check_coverage.sh` -- all packages >= 80% (server: 84.9%)
- `go build -ldflags='-w -s' .` -- clean build
- `go vet ./...` -- clean
- `WARPDL_TEST_SKIP_DAEMON=1 go test ./cmd/` -- passes
- `go test ./internal/api/` -- passes

## Files modified
- `internal/server/rpc_integration_test.go` (new) -- 14 integration tests
- `internal/server/rpc_methods.go` -- thread-safe getters, reordered itemStatus
- `internal/server/rpc_methods_test.go` -- download dir isolation, race-safe field access
- `internal/server/web_test.go` -- t.Chdir(base) for test isolation
- `pkg/warplib/item.go` -- GetDownloaded(), GetTotalSize(), fixed GetPercentage()
- `pkg/warplib/manager.go` -- locked part.Compiled mutation, thread-safe getters in list methods
