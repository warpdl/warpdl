---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: unknown
last_updated: "2026-02-27T12:00:00.000Z"
progress:
  total_phases: 3
  completed_phases: 3
  total_plans: 7
  completed_plans: 7
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-02-26)

**Core value:** Expand WarpDL's protocol coverage and integration surface so it can download from more sources (FTP, SFTP, redirect chains) and be controlled programmatically by external tools
**Current focus:** Phase 3 COMPLETE. All 3 plans done. Ready for Phase 4 (SFTP).

## Current Position

Phase: 3 of 5 (FTP/FTPS) — COMPLETE
Plan: 3 of 3 in current phase (03-01 done, 03-02 done, 03-03 done)
Status: Phase 3 complete — all plans executed
Last activity: 2026-02-27 — Phase 3 complete (FTP/FTPS downloader, resume, API integration)

Progress: [██████░░░░] 60% (3 of 5 phases complete)

## Performance Metrics

**Velocity:**
- Total plans completed: 7
- Average duration: ~15 min per plan
- Total execution time: ~2h

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. HTTP Redirect | 2/2 | ~1h | ~30min |
| 2. Protocol Interface | 2/2 | ~14min | ~7min |
| 3. FTP/FTPS | 3/3 | ~45min | ~15min |

**Recent Trend:**
- Last 7 plans: 01-01, 01-02, 02-01, 02-02, 03-01, 03-02, 03-03 (all complete)
- Trend: Steady, efficient on protocol implementation tasks

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [Pre-phase]: Use `github.com/jlaffaye/ftp` v0.2.0 for FTP (mature, TLS support, RetrFrom resume)
- [Pre-phase]: Use `golang.org/x/crypto/ssh` v0.48.0 + `github.com/pkg/sftp` v1.13.10 for SFTP (patches GO-2025-4134/4135)
- [Pre-phase]: Use `github.com/creachadair/jrpc2` v1.3.4 + `github.com/coder/websocket` v1.8.14 for JSON-RPC (do NOT use gorilla/websocket — archived)
- [Pre-phase]: HTTP redirect fix uses default CheckRedirect (nil) — custom header copying reintroduces CVE-2024-45336
- [Pre-phase]: FTP ServerConn is not goroutine-safe — enforce maxConnections=1 at type level, not config
- [02-01]: AddDownload keeps *Downloader parameter signature; wraps internally as adapter to avoid API break
- [02-01]: patchHandlers stays concrete *Downloader — accesses unexported fields directly; no SetHandlers needed for Phase 2
- [02-01]: ErrUnsupportedDownloadScheme distinct from ErrUnsupportedScheme (proxy.go already had that name)
- [02-01]: Item.Resume passes context.Background() and nil handlers — handlers already installed by patchHandlers
- [Phase 02-02]: Protocol type lives in protocol.go for cohesion with ProtocolDownloader interface
- [Phase 02-02]: gen_fixture.go must use exact warplib types (QueuedItemState not []string) — GOB registers type metadata even for nil pointers causing decoder failures with mismatched types
- [Phase 02-02]: ValidateProtocol called in InitManager after GOB decode — rejects unknown Protocol values with 'upgrade warpdl' error, no silent degradation
- [Phase 03-01]: FTP uses single-stream download (SupportsParallel=false, MaxConnections=1) — jlaffaye/ftp ServerConn is not goroutine-safe
- [Phase 03-01]: StripURLCredentials exported from warplib for cross-package use in API layer
- [Phase 03-01]: classifyFTPError uses standard errors.As (not generic wrapper) — 4xx transient, 5xx permanent, net.Error transient
- [Phase 03-02]: Manager.ResumeDownload uses protocol guard (switch on item.Protocol) to skip validateDownloadIntegrity for FTP items
- [Phase 03-02]: FTP resume offset derived from destination file size on disk (WarpStat), not from parts map
- [Phase 03-03]: downloadHandler refactored into downloadHTTPHandler + downloadFTPHandler — zero logic change to HTTP path
- [Phase 03-03]: Api struct gains schemeRouter field; NewApi signature updated with router parameter (nil-safe for tests)

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 4 gate]: SFTP TOFU UX design must be finalized before implementation. Specifically: daemon is headless — how does first-use key acceptance work? Plan 04-01 is a design plan, not code.
- [Phase 3 security RESOLVED]: FTP credentials in URL are stripped by StripURLCredentials before persistence — verified by GOB round-trip test (TestFTPCredentialSecurityGOBRoundTrip)
- [Phase 2 resolved]: GOB backward compatibility — golden fixture committed (pre_phase2_userdata.warp), Protocol=0=ProtoHTTP invariant locked by TestProtocolConstants and TestGOBBackwardCompatProtocol.
- [Phase 5 security]: JSON-RPC WebSocket CSRF — localhost binding alone does not prevent browser tab attacks. Auth token required on every request including WebSocket upgrade (CVE-2025-52882 precedent).

## Session Continuity

Last session: 2026-02-27
Stopped at: Completed Phase 3 (FTP/FTPS) — all 3 plans: 03-01 (core FTP downloader), 03-02 (resume + Manager FTP dispatch), 03-03 (API layer + daemon init). All tests pass. Coverage 85.8% warplib, 79.6% api. Binary builds. Ready for Phase 4 (SFTP).
Resume file: None
