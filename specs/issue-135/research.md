---
spec: issue-135
phase: research
created: 2026-01-19
generated: auto
---

# Research: Download Queue Manager

## Executive Summary

Implementing download queue manager is technically viable. The existing `Manager` pattern in `pkg/warplib/manager.go` provides a clear template for state management and GOB persistence. The daemon architecture already supports concurrent download tracking via `internal/server/pool.go`.

## Codebase Analysis

### Existing Patterns

| Pattern | Location | Relevance |
|---------|----------|-----------|
| State persistence | `pkg/warplib/manager.go:persistItems()` | GOB encoding for queue state |
| Handler callbacks | `pkg/warplib/handlers.go` | Event-driven progress, completion |
| Pool management | `internal/server/pool.go` | Concurrent download tracking |
| CLI flags | `cmd/download.go:dlFlags` | Pattern for new flags |
| API handlers | `internal/api/api.go:RegisterHandlers()` | Handler registration pattern |
| Client methods | `pkg/warpcli/methods.go` | Generic invoke pattern |
| Update types | `common/const.go` | Enum pattern for new operations |

### Key Files to Modify

| File | Action | Purpose |
|------|--------|---------|
| `pkg/warplib/queue.go` | Create | QueueManager implementation |
| `pkg/warplib/manager.go` | Modify | Integrate queue with Manager |
| `internal/api/queue.go` | Create | Queue API handlers |
| `cmd/queue.go` | Create | CLI queue command |
| `pkg/warpcli/methods.go` | Modify | Queue client methods |
| `common/types.go` | Modify | Queue request/response types |
| `common/const.go` | Modify | UPDATE_QUEUE_* constants |

### Dependencies

- `sync.Mutex` - Thread-safe queue operations
- `encoding/gob` - State persistence (already used)
- `github.com/urfave/cli` - CLI framework (already used)
- No new external dependencies required

### Constraints

1. **GOB compatibility**: Queue state must be serializable alongside existing `ItemsMap`
2. **Daemon lifecycle**: Queue must persist across daemon restarts
3. **Backward compatibility**: Existing downloads must continue working
4. **Race conditions**: Queue operations concurrent with download callbacks

## Feasibility Assessment

| Aspect | Assessment | Notes |
|--------|------------|-------|
| Technical Viability | High | All patterns exist; no architectural changes needed |
| Effort Estimate | M | ~15-20 tasks across 4 phases |
| Risk Level | Low | Isolated changes; existing test infrastructure |

## Data Flow

```
CLI (warpdl download --max-concurrent 3)
    |
    v
pkg/warpcli → invoke("download", {maxConcurrent: 3})
    |
    v
internal/api → downloadHandler checks queue capacity
    |
    v
pkg/warplib/queue.go → QueueManager.Add() or QueueManager.Enqueue()
    |
    v
Queue persisted → userdata.warp (GOB)
```

## Recommendations

1. **Start with QueueManager struct** - Isolate queue logic from Manager
2. **Persist queue alongside items** - Single GOB file, atomic writes
3. **Use channels for slot notification** - Decouple completion from start
4. **Test with race detector** - CI already runs `-race` tests
