---
spec: issue-135
phase: requirements
created: 2026-01-19
generated: auto
---

# Requirements: Download Queue Manager

## Summary

Implement download queue manager that limits concurrent active downloads, supports prioritization, and persists queue state across daemon restarts.

## User Stories

### US-1: Limit concurrent downloads

As a user, I want to limit the number of concurrent downloads so that my bandwidth is not saturated.

**Acceptance Criteria**:
- AC-1.1: Default concurrent limit is 3 downloads
- AC-1.2: `--max-concurrent N` flag overrides default
- AC-1.3: `WARPDL_MAX_CONCURRENT` env var overrides default
- AC-1.4: Flag takes precedence over env var
- AC-1.5: Downloads beyond limit are queued (not rejected)

### US-2: Auto-start queued downloads

As a user, I want queued downloads to auto-start when a slot becomes available so that I don't have to manually resume them.

**Acceptance Criteria**:
- AC-2.1: When active download completes, next queued download starts automatically
- AC-2.2: When active download is stopped, next queued download starts
- AC-2.3: Order respects queue position (FIFO by default)
- AC-2.4: User is notified when queued download starts

### US-3: Queue persistence

As a user, I want queue state persisted across daemon restarts so that I don't lose my download queue.

**Acceptance Criteria**:
- AC-3.1: Queue order persisted to disk
- AC-3.2: Active downloads resume on daemon restart
- AC-3.3: Queued downloads remain queued after restart
- AC-3.4: Queue state survives daemon crash

### US-4: View queue status

As a user, I want to view the current queue so that I know what's downloading and what's waiting.

**Acceptance Criteria**:
- AC-4.1: `warpdl queue` shows active and waiting downloads
- AC-4.2: Output shows position, name, size, status
- AC-4.3: Distinguishes between "active" and "waiting" states

### US-5: Pause/resume queue

As a user, I want to pause the entire queue so that I can temporarily stop all downloads without losing queue state.

**Acceptance Criteria**:
- AC-5.1: `warpdl queue pause` stops starting new downloads
- AC-5.2: Active downloads continue until complete
- AC-5.3: `warpdl queue resume` resumes auto-starting
- AC-5.4: Queue pause state persists across daemon restart

### US-6: Reorder queue

As a user, I want to move downloads within the queue so that I can prioritize important downloads.

**Acceptance Criteria**:
- AC-6.1: `warpdl queue move <hash> <position>` moves download
- AC-6.2: Position 1 is next in queue
- AC-6.3: Moving to position beyond queue length moves to end
- AC-6.4: Cannot move active downloads

### US-7: Priority levels

As a user, I want to assign priority levels so that important downloads start sooner.

**Acceptance Criteria**:
- AC-7.1: `--priority high|normal|low` flag on download
- AC-7.2: High priority downloads queue before normal/low
- AC-7.3: Within same priority, FIFO order applies
- AC-7.4: Default priority is "normal"

## Functional Requirements

| ID | Requirement | Priority | Source |
|----|-------------|----------|--------|
| FR-1 | Queue limits concurrent downloads to configurable max | Must | US-1 |
| FR-2 | `--max-concurrent N` CLI flag | Must | US-1 |
| FR-3 | `WARPDL_MAX_CONCURRENT` env var | Must | US-1 |
| FR-4 | Auto-start next download on completion | Must | US-2 |
| FR-5 | Persist queue state (GOB encoding) | Must | US-3 |
| FR-6 | `warpdl queue` command (list) | Should | US-4 |
| FR-7 | `warpdl queue pause/resume` commands | Should | US-5 |
| FR-8 | `warpdl queue move` command | Should | US-6 |
| FR-9 | `--priority` flag (high/normal/low) | Should | US-7 |
| FR-10 | Priority-based queue ordering | Should | US-7 |

## Non-Functional Requirements

| ID | Requirement | Category |
|----|-------------|----------|
| NFR-1 | Queue operations complete in <10ms | Performance |
| NFR-2 | No data loss on daemon crash | Reliability |
| NFR-3 | Thread-safe concurrent access | Concurrency |
| NFR-4 | 80%+ test coverage per package | Quality |
| NFR-5 | Pass race detector tests | Quality |
| NFR-6 | Backward compatible with existing downloads | Compatibility |

## Out of Scope

- Per-domain concurrent limits (P2)
- Bandwidth allocation across queue (P2)
- Queue scheduling policies beyond priority
- Web UI for queue management
- Distributed queue across multiple daemons

## Dependencies

- Existing `Manager` for download state
- Existing `Pool` for connection tracking
- Existing GOB persistence infrastructure
