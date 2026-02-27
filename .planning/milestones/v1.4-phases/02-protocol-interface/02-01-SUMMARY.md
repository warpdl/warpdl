---
phase: 02-protocol-interface
plan: 01
subsystem: core
tags: [go, interface, adapter, http, downloader, tdd]

requires:
  - phase: 01-http-redirect
    provides: "Cross-origin header stripping and redirect policy (fetchInfo uses it)"

provides:
  - "ProtocolDownloader interface for protocol-agnostic download dispatch"
  - "httpProtocolDownloader adapter wrapping *Downloader with compile-time check"
  - "SchemeRouter mapping http/https to httpProtocolDownloader factory"
  - "DownloadError with IsTransient/Unwrap for structured error classification"
  - "Item.dAlloc changed from *Downloader to ProtocolDownloader"
  - "Manager.AddDownload and ResumeDownload wrap *Downloader in adapter before storing"

affects:
  - 02-02-PLAN (item Protocol field uses SchemeRouter)
  - 03-ftp-protocol (implements ProtocolDownloader for FTP)
  - 04-sftp-protocol (implements ProtocolDownloader for SFTP)

tech-stack:
  added: []
  patterns:
    - "Adapter pattern: httpProtocolDownloader wraps *Downloader, no Downloader code changes"
    - "Compile-time interface check: var _ ProtocolDownloader = (*httpProtocolDownloader)(nil)"
    - "Pragmatic wrapping: AddDownload keeps *Downloader signature, wraps internally before setDAlloc"
    - "Guard pattern: ErrProbeRequired returned if Download/Resume called before Probe"

key-files:
  created:
    - pkg/warplib/protocol.go
    - pkg/warplib/protocol_http.go
    - pkg/warplib/protocol_router.go
    - pkg/warplib/protocol_test.go
    - pkg/warplib/protocol_http_test.go
    - pkg/warplib/protocol_router_test.go
  modified:
    - pkg/warplib/item.go
    - pkg/warplib/manager.go
    - pkg/warplib/item_test.go
    - pkg/warplib/item_race_test.go
    - pkg/warplib/manager_race_test.go
    - pkg/warplib/manager_test.go

key-decisions:
  - "AddDownload keeps *Downloader parameter signature; wraps internally as adapter — zero API break for internal/api"
  - "patchHandlers stays concrete *Downloader — no type assertions needed in public API"
  - "ErrUnsupportedDownloadScheme distinct from ErrUnsupportedScheme (proxy.go already had that name)"
  - "Item.Resume passes context.Background() and nil handlers — handlers already installed by patchHandlers"
  - "SkipSetup=true in test adapter creation prevents network calls during unit tests"
  - "Existing tests updated to use httpProtocolDownloader{inner: d} instead of bare *Downloader"

requirements-completed: [PROTO-01, PROTO-02, PROTO-03]

duration: 7min
completed: 2026-02-27
---

# Phase 2 Plan 1: ProtocolDownloader Interface and HTTP Adapter Summary

**Protocol-agnostic ProtocolDownloader interface with httpProtocolDownloader adapter, SchemeRouter (http/https), and Item/Manager refactored to use interface — zero test regressions, 86.4% coverage**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-02-27T07:59:51Z
- **Completed:** 2026-02-27T08:07:00Z
- **Tasks:** 3 (RED test, GREEN implementation, REFACTOR)
- **Files modified:** 12 (6 created, 6 modified)

## Accomplishments

- Defined `ProtocolDownloader` interface with 15 methods covering probe, download, resume, capabilities, lifecycle, and metadata getters
- Built `httpProtocolDownloader` adapter with compile-time check `var _ ProtocolDownloader = (*httpProtocolDownloader)(nil)` — wraps existing `*Downloader` without modifying it
- Created `SchemeRouter` with http/https pre-registered; case-insensitive scheme resolution; descriptive error listing supported schemes for unsupported URLs
- Changed `Item.dAlloc` from `*Downloader` to `ProtocolDownloader` — all existing Item methods compile and work unchanged
- Manager wraps `*Downloader` in adapter before `setDAlloc` — zero regression in `AddDownload` and `ResumeDownload` flows
- `DownloadError` type with `IsTransient`, `Unwrap`, protocol+op fields for structured error classification

## Task Commits

1. **RED: Failing tests** - `2cc2021` (test: add failing tests for interface, adapter, router)
2. **GREEN: Implementation** - `0439048` (feat: ProtocolDownloader interface and HTTP adapter)

## Files Created/Modified

- `pkg/warplib/protocol.go` - ProtocolDownloader interface, DownloadCapabilities, ProbeResult, DownloadError, DownloaderFactory, ErrProbeRequired
- `pkg/warplib/protocol_http.go` - httpProtocolDownloader adapter with compile-time check; all getters delegate to inner *Downloader
- `pkg/warplib/protocol_router.go` - SchemeRouter with Register, NewDownloader, SupportedSchemes; http/https registered
- `pkg/warplib/protocol_test.go` - Interface compliance, DownloadError, mock downloader, probe-guard tests (21 test cases)
- `pkg/warplib/protocol_http_test.go` - httpProtocolDownloader adapter tests against httptest server
- `pkg/warplib/protocol_router_test.go` - Scheme router tests (http/https/unsupported/case-insensitive/empty/invalid)
- `pkg/warplib/item.go` - dAlloc type: `*Downloader` → `ProtocolDownloader`; getDAlloc/setDAlloc types updated; Resume uses context.Background()
- `pkg/warplib/manager.go` - AddDownload and ResumeDownload wrap *Downloader in adapter; patchHandlers stays concrete
- `pkg/warplib/item_test.go` - Updated bare `*Downloader` assignments to use `httpProtocolDownloader{inner: d}`
- `pkg/warplib/item_race_test.go` - Same fix; race tests still pass with -race flag
- `pkg/warplib/manager_race_test.go` - Same fix
- `pkg/warplib/manager_test.go` - Same fix

## Decisions Made

- **AddDownload parameter kept as *Downloader** — changing to ProtocolDownloader would break `internal/api/download.go` which constructs `*Downloader` directly and passes to `AddDownload`. Phase 3 will add a `AddProtocolDownload` or generalize when FTP needs it.
- **ErrUnsupportedDownloadScheme** — `proxy.go` already declared `ErrUnsupportedScheme` for proxy schemes; named distinctly to avoid conflict.
- **patchHandlers stays concrete** — `patchHandlers(*Downloader, *Item)` accesses unexported `d.handlers` struct fields directly. Exposing this through the interface would require adding `GetHandlers()/SetHandlers()` methods which adds complexity for no current benefit. Phase 3 will revisit when FTP handlers are needed.
- **probed=true in AddDownload/ResumeDownload adapters** — The `*Downloader` from `NewDownloader`/`initDownloader` has already fetched file info (equivalent to Probe). Setting `probed=true` allows `Download`/`Resume` on the adapter without a redundant Probe.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Existing test files used *Downloader directly as dAlloc**
- **Found during:** GREEN phase (compilation)
- **Issue:** `item_test.go`, `item_race_test.go`, `manager_race_test.go`, `manager_test.go` directly assigned `&Downloader{...}` to `item.dAlloc` or passed to `setDAlloc`. After type change to `ProtocolDownloader`, these no longer compile since `*Downloader` doesn't implement the interface (missing Probe, Capabilities, new Resume signature).
- **Fix:** Updated all occurrences to use `&httpProtocolDownloader{inner: d, probed: true}` where `d` is the original `*Downloader`. Same behavior, new type.
- **Files modified:** `item_test.go`, `item_race_test.go`, `manager_race_test.go`, `manager_test.go`
- **Verification:** Full test suite passes, race detector clean
- **Committed in:** `0439048` (GREEN phase commit)

**2. [Rule 1 - Bug] ErrUnsupportedScheme name conflict with proxy.go**
- **Found during:** GREEN phase (compilation)
- **Issue:** `proxy.go` declares `ErrUnsupportedScheme = errors.New("unsupported proxy scheme")`. Adding the same name in `protocol.go` caused a redeclaration error.
- **Fix:** Named the new sentinel `ErrUnsupportedDownloadScheme` to clearly distinguish download-scheme errors from proxy-scheme errors.
- **Files modified:** `protocol.go`, `protocol_router.go`
- **Verification:** `go build ./...` succeeds
- **Committed in:** `0439048` (GREEN phase commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 - Bug)
**Impact on plan:** Both fixes were necessary for compilation and correctness. No scope creep.

## Issues Encountered

None beyond the auto-fixed compilation errors above.

## Next Phase Readiness

- `ProtocolDownloader` interface ready for FTP (Phase 3) and SFTP (Phase 4) adapters
- `SchemeRouter.Register()` method ready to accept ftp/sftp factories
- Plan 02-02 (Item Protocol field + GOB-safe enum) can now add `item.Protocol` field that SchemeRouter uses during `ResumeDownload`
- All existing tests pass — zero regression

---
*Phase: 02-protocol-interface*
*Completed: 2026-02-27*
