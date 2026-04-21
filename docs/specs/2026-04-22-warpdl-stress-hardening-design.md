# warpdl Concurrency Hardening — Stress-First Pass

**Date:** 2026-04-22
**Status:** Approved
**Scope:** `pkg/warplib` only

## 1. Goal

Validate the 8 recent perf/concurrency fixes against adversarial conditions — high concurrency, goroutine leaks, allocation budgets, and misuse scenarios a naive user can realistically produce. Ship stress tests, a small amount of native fuzzing, and only touch production code if a stress test uncovers a real defect.

## 2. Non-goals

- Rewriting any of the 8 fixes
- Adding test dependencies (no `goleak`, `rapid`, `gopter`)
- Lint / style cleanup unrelated to robustness
- Long-running soak tests beyond what a 5-minute CI run can absorb

## 3. Approach

**Primary (Approach 1):** hand-rolled per-component stress tests with goroutine-leak and alloc-budget assertions.

**Secondary (Approach 2, targeted):** Go 1.18 native fuzz (`FuzzXxx`) for two entry points where input-shape coverage matters:
- `ParseSpeedLimit` (text parser, historically bug-prone)
- `getBuf(size)` (numeric boundary + put/get interleaving)

## 4. Design

### 4.1 Shared harness — `stress_util_test.go`

Helpers used by every stress test:

- `assertNoGoroutineLeak(t *testing.T, fn func())`: snapshots `runtime.NumGoroutine()` before and after `fn`, with a 100ms × 10 settle loop; fails if final count > start + tolerance (default 2).
- `stressRun(t *testing.T, workers, iters int, fn func(worker, iter int))`: fan-out helper.
- `mustNoHang(t *testing.T, d time.Duration, fn func())`: starts `fn` in a goroutine, fails test if it doesn't return within `d`.

### 4.2 Per-fix stress matrix

| Fix | File | Tests |
|---|---|---|
| 1 — Persister | `persist_stress_test.go` | `Stress_ProducerFlusherShutdown`, `Stress_PanickingWriteFn`, `Stress_RapidCreateShutdown`, `Stress_UseAfterShutdown` |
| 2 — No per-chunk goroutines | `parts_stress_test.go` | `Stress_BlockingCallback`, `Stress_PanickingCallback` |
| 3 — Work stealing | `worksteal_stress_test.go` | `Stress_RegisterUnregisterSteal`, `Stress_StealAfterUnregister`, `Stress_OwnerStealerInterleave` |
| 5 — Buffer pool | `bufpool_stress_test.go` | `Stress_RandomSizes`, `Stress_DoublePut_DetectedOrSafe`, `AllocBudget_Steady`, `FuzzGetBuf` (native fuzz) |
| 6 — Tail reslice | `parts_stress_test.go` | `AllocBudget_CopyLoopSteady` |
| 7/8 — VMap | `vmap_stress_test.go` | `Stress_MixedOps`, `Stress_RangeCallbackMutates`, `Stress_MakeResetRace`, `AllocBudget_GetSet` |

### 4.3 Cross-cutting "dumb-user" tests — `manager_stress_test.go`

- `TestManagerStress_CloseCalledConcurrently` — 100 goroutines race to `Close()`; must succeed or return a benign error once, no double-close panic.
- `TestManagerStress_UpdateItemAfterClose` — must not panic; result is either a dropped update or error.
- `TestManagerStress_AddWhileClosing` — racing `AddDownload` vs `Close` completes cleanly or errors cleanly.
- `TestDownloaderStress_StopSpam` — 1000 concurrent `Stop()` calls are idempotent.

### 4.4 Native fuzz — `fuzz_test.go`

- `FuzzParseSpeedLimit(f)` — seed with known good/bad inputs; invariant: never panics, returns `>= 0` or a non-nil error.
- `FuzzGetBuf(f)` — input = int32 size + []byte of ops (get/put markers); invariant: no panic, returned buffer length matches request.

### 4.5 Goroutine-leak budget

Every stress test ends with `assertNoGoroutineLeak`. Suite-level leak detection via a `TestMain`-less approach: each test is self-contained.

### 4.6 Alloc-budget assertions (`testing.AllocsPerRun`)

- `getBuf/putBuf` round-trip, `DEF_CHUNK_SIZE`: 0 allocs
- `VMap.Get` on populated map: 0 allocs
- `VMap.Set` on existing key: 0 allocs
- Copy loop one chunk through pooled buffer: 0 allocs

## 5. Action on findings

If a stress test reveals a real defect (race, leak, panic, wrong result), fix the production code inline with the minimum change that makes the test pass. Record each such fix in a short "findings" section in the plan doc.

## 6. Out of scope

- Modifying network protocols, FTP/SFTP layer
- Changing public API surface beyond already-introduced `UpdateItemAsync` / `VMap.RangeLocked` / `VMap.Reset` / `VMap.Len`
- Cross-platform disk-space edge cases (covered by existing tests)
