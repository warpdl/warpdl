# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-02-26)

**Core value:** Expand WarpDL's protocol coverage and integration surface so it can download from more sources (FTP, SFTP, redirect chains) and be controlled programmatically by external tools
**Current focus:** Phase 2 in progress. Plan 02-01 complete.

## Current Position

Phase: 2 of 5 (Protocol Interface) — IN PROGRESS
Plan: 1 of 2 in current phase (02-01 done, 02-02 next)
Status: Phase 2 plan 1 complete
Last activity: 2026-02-27 — Phase 2 Plan 01 executed (ProtocolDownloader interface + HTTP adapter)

Progress: [███░░░░░░░] 30% (1.5 of 5 phases complete)

## Performance Metrics

**Velocity:**
- Total plans completed: 3
- Average duration: ~20 min per plan
- Total execution time: ~1h 10min

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. HTTP Redirect | 2/2 | ~1h | ~30min |
| 2. Protocol Interface | 1/2 | ~7min | ~7min |

**Recent Trend:**
- Last 5 plans: 01-01 (complete), 01-02 (complete), 02-01 (complete)
- Trend: Steady

*Updated after each plan completion*

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

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 4 gate]: SFTP TOFU UX design must be finalized before implementation. Specifically: daemon is headless — how does first-use key acceptance work? Plan 04-01 is a design plan, not code.
- [Phase 3 security]: FTP/SFTP credentials in URL must not be persisted to `~/.config/warpdl/userdata.warp`
- [Phase 2 risk]: GOB backward compatibility — any Item struct change needs a fixture test before merge. HTTP must be iota=0.
- [Phase 5 security]: JSON-RPC WebSocket CSRF — localhost binding alone does not prevent browser tab attacks. Auth token required on every request including WebSocket upgrade (CVE-2025-52882 precedent).

## Session Continuity

Last session: 2026-02-27
Stopped at: Completed 02-01-PLAN.md — ProtocolDownloader interface and HTTP adapter. All tests pass with race detection. Coverage 86.4%. Ready for 02-02 (Item Protocol field + GOB-safe enum).
Resume file: None
