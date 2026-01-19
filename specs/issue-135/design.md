---
spec: issue-135
phase: design
created: 2026-01-19
generated: auto
---

# Design: Download Queue Manager

## Overview

Introduce `QueueManager` in `pkg/warplib` that wraps download lifecycle, enforcing concurrent limits and auto-starting queued downloads. Integrates with existing `Manager` for persistence.

## Architecture

```
                                    +-----------------+
                                    |   CLI/API       |
                                    +--------+--------+
                                             |
                                             v
+-------------------+             +----------+----------+
|  QueueManager     |<----------->|      Manager        |
|  - maxConcurrent  |             |  - items (ItemsMap) |
|  - active map     |             |  - f (file)         |
|  - waiting slice  |             |  - mu (mutex)       |
|  - paused bool    |             +---------------------+
+--------+----------+
         |
         v
+--------+----------+
|   Downloader      |
|   (existing)      |
+-------------------+
```

## Components

### QueueManager

**Purpose**: Enforce concurrent download limits, manage queue state

**Location**: `pkg/warplib/queue.go`

**Responsibilities**:
- Track active downloads (hash -> download info)
- Maintain waiting queue (ordered slice)
- Start next download when slot available
- Persist queue state

```go
// Priority represents download priority level
type Priority int

const (
    PriorityLow    Priority = 0
    PriorityNormal Priority = 1
    PriorityHigh   Priority = 2
)

// QueuedItem represents a download waiting in queue
type QueuedItem struct {
    Hash     string    `json:"hash"`
    Priority Priority  `json:"priority"`
    AddedAt  time.Time `json:"added_at"`
}

// QueueState represents persistable queue state
type QueueState struct {
    MaxConcurrent int           `json:"max_concurrent"`
    Paused        bool          `json:"paused"`
    Waiting       []*QueuedItem `json:"waiting"`
    Active        []string      `json:"active"` // hashes only
}

// QueueManager manages download queue
type QueueManager struct {
    maxConcurrent int
    paused        bool
    active        map[string]struct{}
    waiting       []*QueuedItem
    mu            sync.Mutex
    onStart       func(hash string) error // callback to start download
}

// Methods
func NewQueueManager(maxConcurrent int, onStart func(string) error) *QueueManager
func (q *QueueManager) SetMaxConcurrent(n int)
func (q *QueueManager) Add(hash string, priority Priority) error
func (q *QueueManager) OnComplete(hash string)
func (q *QueueManager) OnStop(hash string)
func (q *QueueManager) Pause()
func (q *QueueManager) Resume()
func (q *QueueManager) Move(hash string, position int) error
func (q *QueueManager) GetState() *QueueState
func (q *QueueManager) LoadState(state *QueueState)
func (q *QueueManager) GetWaiting() []*QueuedItem
func (q *QueueManager) GetActive() []string
func (q *QueueManager) IsPaused() bool
```

### Manager Integration

**Location**: `pkg/warplib/manager.go`

**Changes**:
- Add `queue *QueueManager` field
- Modify `AddDownload` to use queue
- Add queue state to persistence
- Patch completion handlers to call `queue.OnComplete`

### API Handlers

**Location**: `internal/api/queue.go`

**New handlers**:
- `queueStatusHandler` - GET queue state
- `queuePauseHandler` - Pause queue
- `queueResumeHandler` - Resume queue
- `queueMoveHandler` - Move item in queue

### CLI Commands

**Location**: `cmd/queue.go`

**New commands**:
- `warpdl queue` - List queue
- `warpdl queue pause` - Pause queue
- `warpdl queue resume` - Resume queue
- `warpdl queue move <hash> <position>` - Move item

**Modified commands**:
- `warpdl download` - Add `--max-concurrent`, `--priority` flags
- `warpdl daemon` - Add `--max-concurrent` flag

## Data Flow

### Download Request Flow

```
1. CLI: warpdl download <url> --priority high
2. API: downloadHandler receives request
3. API: Check if QueueManager has capacity
4. If capacity:
   a. Create Downloader
   b. Register with active set
   c. Start download
5. If no capacity:
   a. Create Downloader (setup only, don't start)
   b. Add to waiting queue
   c. Return queued status
```

### Completion Flow

```
1. Downloader calls DownloadCompleteHandler
2. Handler calls QueueManager.OnComplete(hash)
3. QueueManager:
   a. Remove from active set
   b. If not paused and waiting queue not empty:
      i.  Pop highest priority item
      ii. Call onStart callback
4. onStart callback:
   a. Get Item from Manager
   b. Resume download
   c. Add to active set
```

## Technical Decisions

| Decision | Options | Choice | Rationale |
|----------|---------|--------|-----------|
| Queue storage | Separate file vs unified | Unified | Single atomic persistence |
| Priority impl | Heap vs sorted slice | Sorted slice | Simpler, queue size small |
| Slot notification | Channel vs callback | Callback | Matches existing handler pattern |
| Default max concurrent | 1, 3, 5, unlimited | 3 | Balance bandwidth/speed |

## File Structure

| File | Action | Purpose |
|------|--------|---------|
| `pkg/warplib/queue.go` | Create | QueueManager implementation |
| `pkg/warplib/queue_test.go` | Create | QueueManager unit tests |
| `pkg/warplib/manager.go` | Modify | Integrate queue |
| `common/const.go` | Modify | Add UPDATE_QUEUE_* constants |
| `common/types.go` | Modify | Add queue request/response types |
| `internal/api/queue.go` | Create | Queue API handlers |
| `internal/api/queue_test.go` | Create | API handler tests |
| `cmd/queue.go` | Create | CLI queue command |
| `cmd/download.go` | Modify | Add --max-concurrent, --priority flags |
| `pkg/warpcli/methods.go` | Modify | Add queue client methods |

## Error Handling

| Error | Handling | User Impact |
|-------|----------|-------------|
| Queue full | N/A (queue grows) | Download added to end |
| Move invalid hash | Return ErrHashNotFound | CLI shows error |
| Move active download | Return ErrCannotMoveActive | CLI shows error |
| Invalid position | Clamp to valid range | Silent adjustment |
| Persistence failure | Log, retry on next op | Potential state loss on crash |

## State Transitions

```
Download States:
  queued -> active -> completed
  queued -> active -> stopped -> queued (on retry)
  queued -> cancelled

Queue States:
  running -> paused -> running
```

## Existing Patterns to Follow

1. **Mutex pattern** from `pkg/warplib/manager.go`:
   ```go
   m.mu.Lock()
   defer m.mu.Unlock()
   ```

2. **Handler registration** from `internal/api/api.go`:
   ```go
   server.RegisterHandler(common.UPDATE_QUEUE_STATUS, s.queueStatusHandler)
   ```

3. **CLI subcommand** from `cmd/ext/` pattern:
   ```go
   Subcommands: []cli.Command{
       {Name: "pause", Action: queuePause},
   }
   ```

4. **GOB persistence** from `pkg/warplib/manager.go:persistItems()`
