---
spec: issue-135
phase: tasks
total_tasks: 24
created: 2026-01-19
generated: auto
---

# Tasks: Download Queue Manager

## Phase 1: Make It Work (POC)

Focus: Validate queue concept works end-to-end. Hardcoded limit, minimal error handling.

- [x] 1.1 Write failing test for QueueManager.Add with capacity check
  - **Do**: Create `pkg/warplib/queue_test.go`. Write test that adds 4 downloads with maxConcurrent=3, expect 3 active, 1 waiting.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue_test.go`
  - **Done when**: Test exists and fails (QueueManager not implemented)
  - **Verify**: `go test -run TestQueueManager_AddWithCapacity ./pkg/warplib/ 2>&1 | grep FAIL`
  - **Commit**: `core: test: add failing test for QueueManager capacity`
  - _Requirements: FR-1_
  - _Design: QueueManager_

- [x] 1.2 Implement QueueManager struct and Add method
  - **Do**: Create `pkg/warplib/queue.go`. Implement `QueueManager` struct with `maxConcurrent`, `active` map, `waiting` slice, `mu` mutex. Implement `Add()` that either activates or queues.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue.go`
  - **Done when**: Test from 1.1 passes
  - **Verify**: `go test -run TestQueueManager_AddWithCapacity ./pkg/warplib/`
  - **Commit**: `core: feat: implement QueueManager with Add method`
  - _Requirements: FR-1_
  - _Design: QueueManager_

- [x] 1.3 Write failing test for OnComplete triggering next download
  - **Do**: Add test that fills queue, calls OnComplete on one active, expects waiting item to become active via onStart callback.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue_test.go`
  - **Done when**: Test exists and fails
  - **Verify**: `go test -run TestQueueManager_OnComplete ./pkg/warplib/ 2>&1 | grep FAIL`
  - **Commit**: `core: test: add failing test for OnComplete auto-start`
  - _Requirements: FR-4, AC-2.1_
  - _Design: Completion Flow_

- [x] 1.4 Implement OnComplete and onStart callback
  - **Do**: Add `OnComplete(hash)` that removes from active, pops waiting, calls `onStart` callback. Add `onStart` callback field to QueueManager.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue.go`
  - **Done when**: Test from 1.3 passes
  - **Verify**: `go test -run TestQueueManager_OnComplete ./pkg/warplib/`
  - **Commit**: `core: feat: implement OnComplete with auto-start`
  - _Requirements: FR-4_
  - _Design: Completion Flow_

- [x] 1.5 Write failing test for Priority sorting
  - **Do**: Add test that queues low, normal, high priority items, expects high to dequeue first.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue_test.go`
  - **Done when**: Test exists and fails
  - **Verify**: `go test -run TestQueueManager_Priority ./pkg/warplib/ 2>&1 | grep FAIL`
  - **Commit**: `core: test: add failing test for priority ordering`
  - _Requirements: FR-10, AC-7.2_
  - _Design: QueuedItem_

- [x] 1.6 Implement Priority type and priority-based dequeue
  - **Do**: Add `Priority` type (Low=0, Normal=1, High=2). Modify queue insertion to maintain priority order. High priority items inserted before lower priority.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue.go`
  - **Done when**: Test from 1.5 passes
  - **Verify**: `go test -run TestQueueManager_Priority ./pkg/warplib/`
  - **Commit**: `core: feat: implement priority-based queue ordering`
  - _Requirements: FR-10_
  - _Design: Priority impl_

- [x] 1.7 Integrate QueueManager with Manager
  - **Do**: Add `queue *QueueManager` to Manager. Modify `AddDownload` to use queue. Wire `onStart` to call `ResumeDownload`. Patch `DownloadCompleteHandler` to call `queue.OnComplete`.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/manager.go`
  - **Done when**: Downloads use queue when limit set
  - **Verify**: `go test ./pkg/warplib/...`
  - **Commit**: `core: feat: integrate QueueManager with Manager`
  - _Requirements: FR-1, FR-4_
  - _Design: Manager Integration_

- [x] 1.8 Add --max-concurrent flag to daemon
  - **Do**: Add `--max-concurrent` flag to daemon command. Pass to Manager initialization. Add `WARPDL_MAX_CONCURRENT` env var support.
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/daemon.go`, `/Users/divkix/GitHub/warpdl/cmd/daemon_core.go`
  - **Done when**: `warpdl daemon --max-concurrent 2` limits concurrent downloads
  - **Verify**: Manual test: start daemon with flag, queue multiple downloads
  - **Commit**: `daemon: feat: add --max-concurrent flag`
  - _Requirements: FR-2, FR-3_
  - _Design: CLI Commands_

- [x] 1.9 POC Checkpoint - End-to-end queue verification
  - **Do**: Manual E2E test: 1) Start daemon with --max-concurrent 2, 2) Download 4 files, 3) Verify only 2 active, 2 waiting, 4) Verify auto-start on completion.
  - **Done when**: Queue limits and auto-start work end-to-end
  - **Verify**: Manual CLI testing with multiple downloads
  - **Commit**: `core: feat: complete download queue POC`

## Phase 2: Refactoring

After POC validated, clean up code and add missing features.

- [x] 2.1 Add queue state persistence
  - **Do**: Create `QueueState` struct. Add `GetState()` and `LoadState()` methods. Integrate with Manager's GOB persistence.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue.go`, `/Users/divkix/GitHub/warpdl/pkg/warplib/manager.go`
  - **Done when**: Queue state survives daemon restart
  - **Verify**: Restart daemon, verify queue preserved
  - **Commit**: `core: feat: add queue state persistence`
  - _Requirements: FR-5, AC-3.1_
  - _Design: QueueState_

- [x] 2.2 Implement Pause/Resume methods
  - **Do**: Add `paused` field to QueueManager. `Pause()` sets flag, prevents auto-start. `Resume()` clears flag, starts waiting if capacity.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue.go`
  - **Done when**: Pause/Resume work correctly
  - **Verify**: `go test -run TestQueueManager_Pause ./pkg/warplib/`
  - **Commit**: `core: feat: implement queue pause/resume`
  - _Requirements: FR-7, AC-5.1, AC-5.2, AC-5.3_
  - _Design: QueueManager_

- [x] 2.3 Implement Move method
  - **Do**: Add `Move(hash, position)` that reorders waiting queue. Validate hash exists in waiting (not active). Clamp position to valid range.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue.go`
  - **Done when**: Move reorders queue correctly
  - **Verify**: `go test -run TestQueueManager_Move ./pkg/warplib/`
  - **Commit**: `core: feat: implement queue move`
  - _Requirements: FR-8, AC-6.1, AC-6.2, AC-6.3, AC-6.4_
  - _Design: QueueManager_

- [x] 2.4 Add common types for queue API
  - **Do**: Add `UPDATE_QUEUE_STATUS`, `UPDATE_QUEUE_PAUSE`, `UPDATE_QUEUE_RESUME`, `UPDATE_QUEUE_MOVE` constants. Add `QueueStatusResponse`, `QueueMoveParams` types.
  - **Files**: `/Users/divkix/GitHub/warpdl/common/const.go`, `/Users/divkix/GitHub/warpdl/common/types.go`
  - **Done when**: Types compile, no errors
  - **Verify**: `go build ./common/...`
  - **Commit**: `common: feat: add queue API types`
  - _Design: API Handlers_

- [x] 2.5 Implement queue API handlers
  - **Do**: Create `internal/api/queue.go` with handlers: `queueStatusHandler`, `queuePauseHandler`, `queueResumeHandler`, `queueMoveHandler`. Register in `api.go`.
  - **Files**: `/Users/divkix/GitHub/warpdl/internal/api/queue.go`, `/Users/divkix/GitHub/warpdl/internal/api/api.go`
  - **Done when**: API handlers registered
  - **Verify**: `go build ./internal/api/...`
  - **Commit**: `api: feat: implement queue API handlers`
  - _Design: API Handlers_

- [x] 2.6 Add queue client methods
  - **Do**: Add `QueueStatus()`, `QueuePause()`, `QueueResume()`, `QueueMove()` methods to Client in `pkg/warpcli/methods.go`.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warpcli/methods.go`
  - **Done when**: Client methods compile
  - **Verify**: `go build ./pkg/warpcli/...`
  - **Commit**: `cli: feat: add queue client methods`
  - _Design: Client methods_

- [x] 2.7 Implement CLI queue command
  - **Do**: Create `cmd/queue.go` with `queue`, `queue pause`, `queue resume`, `queue move` subcommands. Register in main command list.
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/queue.go`, `/Users/divkix/GitHub/warpdl/cmd/cmd.go`
  - **Done when**: `warpdl queue` commands work
  - **Verify**: `warpdl queue --help` shows subcommands
  - **Commit**: `cli: feat: implement queue CLI commands`
  - _Requirements: FR-6, FR-7, FR-8_
  - _Design: CLI Commands_

- [x] 2.8 Add --priority flag to download command
  - **Do**: Add `--priority high|normal|low` flag to download command. Pass priority to API. Default to "normal".
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/download.go`
  - **Done when**: `warpdl download --priority high <url>` works
  - **Verify**: Manual test with priority flag
  - **Commit**: `cli: feat: add --priority flag to download`
  - _Requirements: FR-9, AC-7.1, AC-7.4_
  - _Design: CLI Commands_

- [ ] 2.9 Add error handling and validation
  - **Do**: Add error types: `ErrQueueHashNotFound`, `ErrCannotMoveActive`. Validate inputs in all queue methods. Return descriptive errors.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/errors.go`, `/Users/divkix/GitHub/warpdl/pkg/warplib/queue.go`
  - **Done when**: Invalid operations return proper errors
  - **Verify**: `go test ./pkg/warplib/...`
  - **Commit**: `core: refactor: add queue error handling`
  - _Design: Error Handling_

## Phase 3: Testing

- [ ] 3.1 Unit tests for QueueManager edge cases
  - **Do**: Add tests for: empty queue, single item, max concurrent = 1, concurrent modifications, invalid move positions.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue_test.go`
  - **Done when**: Edge cases covered
  - **Verify**: `go test -cover ./pkg/warplib/... | grep queue`
  - **Commit**: `core: test: add QueueManager edge case tests`
  - _Requirements: NFR-4_

- [ ] 3.2 Race condition tests for QueueManager
  - **Do**: Add parallel tests that hammer Add/OnComplete/Move concurrently. Run with `-race` flag.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue_race_test.go`
  - **Done when**: No races detected
  - **Verify**: `go test -race -run TestQueueManager ./pkg/warplib/`
  - **Commit**: `core: test: add QueueManager race tests`
  - _Requirements: NFR-3, NFR-5_

- [ ] 3.3 API handler tests
  - **Do**: Add unit tests for queue API handlers. Mock Manager and test request/response.
  - **Files**: `/Users/divkix/GitHub/warpdl/internal/api/queue_test.go`
  - **Done when**: API tests pass
  - **Verify**: `go test ./internal/api/...`
  - **Commit**: `api: test: add queue handler tests`
  - _Requirements: NFR-4_

- [ ] 3.4 Integration test for queue persistence
  - **Do**: Add test that: 1) Creates queue, 2) Closes Manager, 3) Reopens Manager, 4) Verifies queue restored.
  - **Files**: `/Users/divkix/GitHub/warpdl/pkg/warplib/queue_integration_test.go`
  - **Done when**: Persistence test passes
  - **Verify**: `go test -run TestQueue_Persistence ./pkg/warplib/`
  - **Commit**: `core: test: add queue persistence integration test`
  - _Requirements: AC-3.1, AC-3.2, AC-3.3_

## Phase 4: Quality Gates

- [ ] 4.1 Coverage check
  - **Do**: Run coverage for all modified packages. Ensure 80%+ per package.
  - **Verify**: `go test -cover ./pkg/warplib/... ./internal/api/... ./pkg/warpcli/...`
  - **Done when**: All packages >= 80% coverage
  - **Commit**: `core: test: improve coverage to 80%+` (if needed)
  - _Requirements: NFR-4_

- [ ] 4.2 Race detector pass
  - **Do**: Run full test suite with race detector.
  - **Verify**: `go test -race -short ./...`
  - **Done when**: No races detected
  - **Commit**: `core: fix: address race conditions` (if needed)
  - _Requirements: NFR-5_

- [ ] 4.3 Lint and format
  - **Do**: Run go fmt and go vet on all files.
  - **Verify**: `go fmt ./... && go vet ./...`
  - **Done when**: No warnings
  - **Commit**: `chore: format and lint queue code` (if needed)

- [ ] 4.4 Create PR and verify CI
  - **Do**: Push branch, create PR with gh CLI. Watch CI for all-green.
  - **Verify**: `gh pr checks --watch` all green
  - **Done when**: PR ready for review
  - **Commit**: N/A (PR creation, not code)

## Notes

- **POC shortcuts taken**: Hardcoded config in integration, minimal CLI validation
- **Production TODOs**: Consider channel-based notification, batched persistence
- **TDD discipline**: Each feature task preceded by failing test task
