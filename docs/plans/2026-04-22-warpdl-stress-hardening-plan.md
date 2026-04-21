# warpdl Stress Hardening — Implementation Plan

**Spec:** `docs/specs/2026-04-22-warpdl-stress-hardening-design.md`

## Execution order

Low-risk, fast-feedback first. Each phase ends with `go test ./pkg/warplib/ -count=1 -race` green.

### Phase 0 — Shared harness
1. Create `pkg/warplib/stress_util_test.go`
   - `assertNoGoroutineLeak`
   - `stressRun`
   - `mustNoHang`
2. Smoke-test the harness: deliberate leak/hang triggers failure.

### Phase 1 — Buffer pool (lowest-risk, highest-signal)
3. `pkg/warplib/bufpool_stress_test.go`
   - `TestBufPoolStress_RandomSizes` (500 workers × 10k ops, size 1..4×DEF_CHUNK_SIZE)
   - `TestBufPoolStress_DoublePutSafety` (exercises the "same pointer returned twice" scenario)
   - `TestBufPoolAllocBudget_SteadyState`
4. `pkg/warplib/fuzz_test.go`
   - `FuzzGetBuf` (seeded corpus)

### Phase 2 — VMap
5. `pkg/warplib/vmap_stress_test.go`
   - `TestVMapStress_MixedOps` (random Set/Get/Delete/Range/Make/Reset)
   - `TestVMapStress_RangeCallbackMutates`
   - `TestVMapStress_MakeResetRace`
   - `TestVMapAllocBudget_GetSet`

### Phase 3 — Persister
6. `pkg/warplib/persist_stress_test.go`
   - `TestPersisterStress_ProducerFlusherShutdown`
   - `TestPersisterStress_PanickingWriteFn`
   - `TestPersisterStress_RapidCreateShutdown`
   - `TestPersisterStress_UseAfterShutdown`

### Phase 4 — Parts / callback
7. `pkg/warplib/parts_stress_test.go`
   - `TestPartCallbackStress_BlockingCallback`
   - `TestPartCallbackStress_PanickingCallback`
   - `TestCopyLoopAllocBudget`

### Phase 5 — Work stealing
8. `pkg/warplib/worksteal_stress_test.go`
   - `TestWorkStealStress_RegisterUnregisterSteal`
   - `TestWorkStealStress_StealAfterUnregister`
   - `TestWorkStealStress_OwnerStealerInterleave`

### Phase 6 — Parser fuzz
9. Add `FuzzParseSpeedLimit` to `pkg/warplib/fuzz_test.go`
   - Seed with edge cases: "", "0", "-1", "1KB", "1.5MB", "999GB", "abc", "1.0.0"

### Phase 7 — Cross-cutting manager/downloader stress
10. `pkg/warplib/manager_stress_test.go`
    - `TestManagerStress_CloseCalledConcurrently`
    - `TestManagerStress_UpdateItemAfterClose`
    - `TestManagerStress_AddWhileClosing`
11. `pkg/warplib/downloader_stress_test.go`
    - `TestDownloaderStress_StopSpam`

### Phase 8 — Verification lap
12. `go test ./pkg/warplib/ -count=3 -race -timeout 300s`
13. `go test ./pkg/warplib/ -run '^$' -bench BenchmarkBufPool -benchmem`
14. `go test ./... -count=1 -timeout 300s` (whole repo)

## Findings log

### Finding #1 — `writeFn` panic crashes persister writer goroutine
**Where:** `pkg/warplib/persist.go` `drainAndWrite`.
**Symptom:** A panic inside the caller-supplied `writeFn` propagated through `loop()` and terminated the process.
**Fix:** Added `defer recover()` inside `drainAndWrite`, converts panic to error that flows out through `flush()`. Writer goroutine survives and keeps serving subsequent flushes.
**Test:** `TestPersisterStress_PanickingWriteFn` exercises the recovery path and asserts the writer stays alive.

### Finding #2 — `Downloader.Stop()` nil-pointer dereference
**Where:** `pkg/warplib/dloader.go` line 1087.
**Symptom:** `Stop()` called on a Downloader whose `cancel` field was nil (manually constructed, not via `NewDownloader`) panicked with SIGSEGV.
**Fix:** Added `nil` guard before `d.cancel()`.
**Test:** `TestDownloaderStopNilCancelDoesNotPanic` and repeated invocations in `TestDownloaderStress_StopSpam`.

### Finding #3 — `Manager.persister` use-after-close race
**Where:** `pkg/warplib/manager.go` `Close()` wrote `m.persister = nil` without synchronization; `UpdateItem/UpdateItemAsync` read `m.persister` concurrently from other goroutines.
**Symptom:** `-race` flagged the concurrent read/write. Worse: a reader observing the non-nil pointer just before `Close` nilled it, then calling a method on a shut-down persister, could deadlock on a send to a closed channel.
**Fix:** Changed `m.persister` to `atomic.Pointer[persister]`. `Close()` uses `CompareAndSwap` to ensure exactly one caller shuts it down. `UpdateItem*` load the pointer atomically and fall through if it's nil.
**Test:** `TestManagerStress_CloseCalledConcurrently`, `TestManagerStress_UpdateItemAfterClose`, `TestManagerStress_AddWhileClosing`.

### Finding #4 — `Manager.Close()` not idempotent
**Where:** same file. Calling `Close()` twice tried to close `m.f` twice, returning "file already closed" on the second call.
**Fix:** Guarded with `if m.f == nil { return nil }` and cleared `m.f = nil` after close.
**Test:** `TestManagerStress_CloseCalledConcurrently` — 100 concurrent closes, only one meaningful error/no error overall.

### Finding #5 — `persistItems` panicked after Close
**Where:** called transitively from `UpdateItem` post-Close.
**Fix:** Early-return if `m.f == nil`.
**Test:** `TestManagerStress_UpdateItemAfterClose`.

## Done criteria

- [x] Every phase's new tests green under `-race`.
- [x] Every `AllocBudget_*` assertion holds.
- [x] `assertNoGoroutineLeak` passes for every stress test.
- [x] Whole-repo tests still green.
- [x] Fuzz corpora added under `pkg/warplib/testdata/fuzz/…`.
- [x] 3× run under `-race` green (no flakes).

## Done criteria

- Every phase's new tests green under `-race`.
- Every `AllocBudget_*` assertion holds.
- `assertNoGoroutineLeak` passes for every stress test.
- Whole-repo tests still green.
- Fuzz corpora added under `pkg/warplib/testdata/fuzz/…`.
