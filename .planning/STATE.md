---
gsd_state_version: 1.0
milestone: v1.0
milestone_name: milestone
status: unknown
last_updated: "2026-02-27T22:08:54Z"
progress:
  total_phases: 8
  completed_phases: 8
  total_plans: 19
  completed_plans: 19
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-02-26)

**Core value:** Expand WarpDL's protocol coverage and integration surface so it can download from more sources (FTP, SFTP, redirect chains) and be controlled programmatically by external tools
**Current focus:** All phases complete including gap closure. v1.0 milestone done.

## Current Position

Phase: 8 of 8 (Fix RPC FTP/SFTP Handlers) -- COMPLETE
Plan: 1 of 1 in current phase (all plans done)
Status: All 8 phases complete. Phase 8 closed INT-01 tech debt.
Last activity: 2026-02-27 -- Phase 8 complete (RPC FTP/SFTP handler wiring fix)

Progress: [██████████] 100% (8 of 8 phases complete, 19/19 plans done)

## Performance Metrics

**Velocity:**
- Total plans completed: 19
- Average duration: ~15 min per plan
- Total execution time: ~5h 2min

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. HTTP Redirect | 2/2 | ~1h | ~30min |
| 2. Protocol Interface | 2/2 | ~14min | ~7min |
| 3. FTP/FTPS | 3/3 | ~45min | ~15min |
| 4. SFTP | 3/3 | ~1h 10min | ~23min |
| 5. JSON-RPC 2.0 | 4/4 | ~2h | ~30min |
| 6. Fix Defects | 2/2 | ~7min | ~3.5min |
| 7. Verification | 2/2 | ~10min | ~5min |
| 8. Fix RPC FTP/SFTP | 1/1 | ~2min | ~2min |

**Recent Trend:**
- 19 of 19 plans complete across 8 phases
- Trend: Steady execution with thorough testing

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- [Pre-phase]: Use `github.com/jlaffaye/ftp` v0.2.0 for FTP (mature, TLS support, RetrFrom resume)
- [Pre-phase]: Use `golang.org/x/crypto/ssh` v0.48.0 + `github.com/pkg/sftp` v1.13.10 for SFTP (patches GO-2025-4134/4135)
- [Pre-phase]: Use `github.com/creachadair/jrpc2` v1.3.4 + `github.com/coder/websocket` v1.8.14 for JSON-RPC (do NOT use gorilla/websocket -- archived)
- [Pre-phase]: HTTP redirect fix uses default CheckRedirect (nil) -- custom header copying reintroduces CVE-2024-45336
- [Pre-phase]: FTP ServerConn is not goroutine-safe -- enforce maxConnections=1 at type level, not config
- [02-01]: AddDownload keeps *Downloader parameter signature; wraps internally as adapter to avoid API break
- [02-01]: patchHandlers stays concrete *Downloader -- accesses unexported fields directly; no SetHandlers needed for Phase 2
- [02-01]: ErrUnsupportedDownloadScheme distinct from ErrUnsupportedScheme (proxy.go already had that name)
- [02-01]: Item.Resume passes context.Background() and nil handlers -- handlers already installed by patchHandlers
- [Phase 02-02]: Protocol type lives in protocol.go for cohesion with ProtocolDownloader interface
- [Phase 02-02]: gen_fixture.go must use exact warplib types (QueuedItemState not []string) -- GOB registers type metadata even for nil pointers causing decoder failures with mismatched types
- [Phase 02-02]: ValidateProtocol called in InitManager after GOB decode -- rejects unknown Protocol values with 'upgrade warpdl' error, no silent degradation
- [Phase 03-01]: FTP uses single-stream download (SupportsParallel=false, MaxConnections=1) -- jlaffaye/ftp ServerConn is not goroutine-safe
- [Phase 03-01]: StripURLCredentials exported from warplib for cross-package use in API layer
- [Phase 03-01]: classifyFTPError uses standard errors.As (not generic wrapper) -- 4xx transient, 5xx permanent, net.Error transient
- [Phase 03-02]: Manager.ResumeDownload uses protocol guard (switch on item.Protocol) to skip validateDownloadIntegrity for FTP items
- [Phase 03-02]: FTP resume offset derived from destination file size on disk (WarpStat), not from parts map
- [Phase 03-03]: downloadHandler refactored into downloadHTTPHandler + downloadFTPHandler -- zero logic change to HTTP path
- [Phase 03-03]: Api struct gains schemeRouter field; NewApi signature updated with router parameter (nil-safe for tests)
- [Phase 04-01]: TOFU auto-accepts unknown hosts silently (daemon is headless, no interactive prompt)
- [Phase 04-01]: Known hosts file isolated at ~/.config/warpdl/known_hosts (not system ~/.ssh/known_hosts)
- [Phase 04-01]: Resume fully implemented in 04-01 since pattern mirrors FTP exactly
- [Phase 04-01]: Password auth takes priority over key auth when both available
- [Phase 04-02]: Manager.ResumeDownload ProtoSFTP wired alongside ProtoFTP/ProtoFTPS in both guard and dispatch switches
- [Phase 04-03]: downloadFTPHandler generalized to downloadProtocolHandler for FTP/FTPS/SFTP (zero code duplication)
- [Phase 04-03]: SSHKeyPath threaded end-to-end: CLI --ssh-key -> warpcli -> DownloadParams -> API -> DownloaderOpts -> SFTP factory
- [Phase 06-01]: Item.SSHKeyPath persists SSH key path for SFTP resume across pause/resume cycles
- [Phase 06-01]: web.go processDownload now explicitly sets CheckRedirect (defense-in-depth, NewDownloader also patches nil)

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 4 gate RESOLVED]: SFTP TOFU resolved -- daemon auto-accepts unknown hosts silently, rejects changed keys with MITM error. Known hosts stored at ~/.config/warpdl/known_hosts.
- [Phase 3 security RESOLVED]: FTP credentials in URL are stripped by StripURLCredentials before persistence -- verified by GOB round-trip test (TestFTPCredentialSecurityGOBRoundTrip)
- [Phase 4 security RESOLVED]: SFTP credential stripping verified by TestDownloadSFTPHandlerCredentialStripping (4 subtests)
- [Phase 2 resolved]: GOB backward compatibility -- golden fixture committed (pre_phase2_userdata.warp), Protocol=0=ProtoHTTP invariant locked by TestProtocolConstants and TestGOBBackwardCompatProtocol.
- [Phase 5 security]: JSON-RPC WebSocket CSRF -- localhost binding alone does not prevent browser tab attacks. Auth token required on every request including WebSocket upgrade (CVE-2025-52882 precedent).

## Session Continuity

Last session: 2026-02-27
Stopped at: Phase 8 complete (08-01-PLAN.md). RPC FTP/SFTP handler wiring fix done. All 8 phases complete. INT-01 tech debt closed.
Resume file: None
