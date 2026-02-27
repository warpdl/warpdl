# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-02-26)

**Core value:** Expand WarpDL's protocol coverage and integration surface so it can download from more sources (FTP, SFTP, redirect chains) and be controlled programmatically by external tools
**Current focus:** Phase 1 complete. Phase 2 next.

## Current Position

Phase: 1 of 5 (HTTP Redirect) — COMPLETE
Plan: 2 of 2 in current phase (all done)
Status: Phase 1 complete
Last activity: 2026-02-27 — Phase 1 fully executed (Plans 01-01 and 01-02)

Progress: [██░░░░░░░░] 20% (1 of 5 phases complete)

## Performance Metrics

**Velocity:**
- Total plans completed: 2
- Average duration: ~30 min per plan
- Total execution time: ~1 hour

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. HTTP Redirect | 2/2 | ~1h | ~30min |

**Recent Trend:**
- Last 5 plans: 01-01 (complete), 01-02 (complete)
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

### Pending Todos

None yet.

### Blockers/Concerns

- [Phase 4 gate]: SFTP TOFU UX design must be finalized before implementation. Specifically: daemon is headless — how does first-use key acceptance work? Plan 04-01 is a design plan, not code.
- [Phase 3 security]: FTP/SFTP credentials in URL must not be persisted to `~/.config/warpdl/userdata.warp`
- [Phase 2 risk]: GOB backward compatibility — any Item struct change needs a fixture test before merge. HTTP must be iota=0.
- [Phase 5 security]: JSON-RPC WebSocket CSRF — localhost binding alone does not prevent browser tab attacks. Auth token required on every request including WebSocket upgrade (CVE-2025-52882 precedent).

## Session Continuity

Last session: 2026-02-27
Stopped at: Phase 1 complete. Both plans executed via TDD (red-green-refactor). All tests pass with race detection. Coverage 87.1%. Ready for Phase 2 planning.
Resume file: None
