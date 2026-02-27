---
phase: 02-protocol-interface
plan: "02"
subsystem: core
tags: [gob, serialization, backward-compat, protocol, enum, warplib, fixture]

# Dependency graph
requires:
  - phase: 02-protocol-interface/02-01
    provides: ProtocolDownloader interface, httpProtocolDownloader adapter, Item.dAlloc as ProtocolDownloader

provides:
  - "Protocol uint8 enum: ProtoHTTP=0, ProtoFTP=1, ProtoFTPS=2, ProtoSFTP=3 in protocol.go"
  - "Protocol.String() returning http/ftp/ftps/sftp/unknown(N)"
  - "ValidateProtocol() rejecting unknown values with upgrade hint"
  - "Item.Protocol field (zero value = ProtoHTTP, GOB backward compat)"
  - "InitManager validates Protocol after GOB decode (rejects unknown values)"
  - "testdata/pre_phase2_userdata.warp — golden fixture for GOB backward compat regression"
  - "protocol_gob_test.go — 19 test cases covering constants, String, ValidateProtocol, round-trip, fixture, unknown"

affects:
  - "03-ftp — will set Item.Protocol = ProtoFTP when adding FTP downloads"
  - "04-sftp — will set Item.Protocol = ProtoSFTP when adding SFTP downloads"
  - "manager.go — InitManager validates Protocol on load; persistent GOB files now include Protocol"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "GOB backward compat: zero value fields (Protocol=0=ProtoHTTP) are automatically set when old files lack the field"
    - "Golden fixture: binary GOB file committed to testdata/ as regression guard for backward compat"
    - "ValidateProtocol guard: InitManager rejects unknown Protocol values to prevent silent degradation"
    - "iota enum with explicit value comments: ProtoHTTP Protocol = iota // 0"

key-files:
  created:
    - pkg/warplib/protocol_gob_test.go
    - pkg/warplib/testdata/pre_phase2_userdata.warp
    - pkg/warplib/testdata/gen_fixture.go
    - pkg/warplib/generate_fixture_test.go
  modified:
    - pkg/warplib/protocol.go
    - pkg/warplib/item.go
    - pkg/warplib/manager.go

key-decisions:
  - "Protocol type lives in protocol.go (not item.go) for cohesion with ProtocolDownloader interface"
  - "gen_fixture.go must use QueuedItemState struct (not []string) for Waiting field — types must match warplib exactly or GOB fails even for nil pointers"
  - "ValidateProtocol called in InitManager after GOB decode — rejects unknown values with clear error"
  - "ProtoHTTP=0 is invariant — must remain iota start forever, documented in multiple places"

patterns-established:
  - "Protocol: zero value = default protocol (ProtoHTTP) for GOB backward compat"
  - "Fixture generation: use gen_fixture.go (go run) not test runner to avoid stdlib build conflicts"
  - "Protocol validation: validate after decode, not silently degrade"

requirements-completed: [PROTO-03]

# Metrics
duration: 7min
completed: 2026-02-27
---

# Phase 02 Plan 02: Protocol Enum and GOB Backward Compatibility Summary

**Protocol uint8 enum (ProtoHTTP=0/FTP/FTPS/SFTP) added to Item with GOB zero-value backward compat, golden fixture test locking the invariant permanently**

## Performance

- **Duration:** 7 min
- **Started:** 2026-02-27T08:12:00Z
- **Completed:** 2026-02-27T08:19:00Z
- **Tasks:** 3 (RED/GREEN/REFACTOR)
- **Files modified:** 7

## Accomplishments

- Protocol type (uint8 iota) with ProtoHTTP=0/FTP=1/FTPS=2/SFTP=3 and String()/ValidateProtocol()
- Item.Protocol field added — zero value is ProtoHTTP ensuring all pre-Phase-2 GOB files decode correctly
- Golden fixture (pre_phase2_userdata.warp) committed before Protocol field was added — permanently locks backward compat
- InitManager validates Protocol after GOB decode — rejects unknown values with "upgrade warpdl" error, no silent degradation
- 19 test cases: constants, String, ValidateProtocol, round-trip x4, fixture, unknown protocol, persistence integration
- Coverage: 86.4% (above 80% minimum), all tests pass with race detection

## Task Commits

TDD tasks with multiple commits:

1. **RED: Failing tests + fixture generation** - `4cfc096` (test)
2. **GREEN: Protocol type + field + manager validation** - `58e754b` (feat)
3. **REFACTOR: Fix generator documentation comment** - `985de6e` (refactor)

**Plan metadata:** (docs commit follows)

_Note: TDD plan — RED commit added tests that fail, GREEN commit made them pass, REFACTOR cleaned up_

## Files Created/Modified

- `pkg/warplib/protocol.go` - Added Protocol type, constants, String(), ValidateProtocol()
- `pkg/warplib/item.go` - Added Protocol field after Resumable (GOB field order irrelevant but readability follows natural order)
- `pkg/warplib/manager.go` - Added ValidateProtocol check in InitManager after GOB decode
- `pkg/warplib/protocol_gob_test.go` - 19 test cases covering all plan behavior specs
- `pkg/warplib/testdata/pre_phase2_userdata.warp` - Binary GOB fixture (1004 bytes) encoded WITHOUT Protocol field
- `pkg/warplib/testdata/gen_fixture.go` - Generator (build:ignore) with correct warplib types
- `pkg/warplib/generate_fixture_test.go` - Documentation file (build:ignore) referencing generator

## Decisions Made

- **Protocol lives in protocol.go not item.go**: Natural home next to ProtocolDownloader interface; keeps item.go focused on Item data
- **Fixture generator uses correct QueuedItemState struct**: gen_fixture.go initially used `Waiting []string` but warplib uses `[]QueuedItemState` — GOB registers type info even for nil pointers, causing "type mismatch in decoder" at test time. Fixed by using exact warplib types in generator
- **ProtoHTTP=0 invariant documented in 3 places**: Protocol type comment, iota comment, Item.Protocol field comment — belt-and-suspenders for permanent invariant
- **ValidateProtocol in InitManager**: "Unknown protocol values fail with clear error — no silent degradation" per CONTEXT.md spec

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] gen_fixture.go used wrong QueueState.Waiting type**
- **Found during:** GREEN phase (TestGOBBackwardCompatProtocol test run)
- **Issue:** Generator used `Waiting []string` but warplib.QueueState has `Waiting []QueuedItemState`. Even with nil QueueState pointer, GOB registers type metadata causing "type mismatch in decoder: want struct type warplib.QueuedItemState; got non-struct"
- **Fix:** Updated gen_fixture.go to define QueuedItemState and QueueState with exact types matching warplib; regenerated fixture
- **Files modified:** pkg/warplib/testdata/gen_fixture.go, pkg/warplib/testdata/pre_phase2_userdata.warp
- **Verification:** TestGOBBackwardCompatProtocol passes, fixture decodes correctly
- **Committed in:** 58e754b (GREEN phase commit)

**2. [Rule 1 - Bug] Test helper name conflicts with existing test files**
- **Found during:** RED compile attempt
- **Issue:** `newTestItem` conflicts with integrity_test.go; `containsSubstring` conflicts with manager_test.go
- **Fix:** Renamed to `newProtoTestItem` and `protoContains` in protocol_gob_test.go
- **Files modified:** pkg/warplib/protocol_gob_test.go
- **Verification:** Build succeeds, no redeclaration errors
- **Committed in:** 4cfc096 (RED phase commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 bugs)
**Impact on plan:** Both auto-fixes necessary for correct test execution. No scope creep.

## Issues Encountered

- GOB type registration issue: Even a `nil *QueueState` pointer causes the encoder to register type information for all fields of QueueState in the GOB stream. Using incorrect types in the fixture generator caused decoder failures. Solution: use exact field types matching warplib.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Protocol enum and GOB backward compat are complete
- Phase 3 (FTP) can set `Item.Protocol = ProtoFTP` when creating FTP downloads
- Phase 4 (SFTP) can set `Item.Protocol = ProtoSFTP` when creating SFTP downloads
- InitManager will validate and reject unknown Protocol values from newer warpdl versions
- TestGOBBackwardCompatProtocol permanently locks the ProtoHTTP=0 invariant

---
*Phase: 02-protocol-interface*
*Completed: 2026-02-27*
