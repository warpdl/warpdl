# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.4 — Issues Fix

**Shipped:** 2026-02-27
**Phases:** 10 | **Plans:** 21 | **Sessions:** ~5

### What Was Built
- HTTP redirect following with cross-origin credential protection across 6 HTTP client call sites
- Pluggable protocol interface (ProtocolDownloader + SchemeRouter) with GOB backward compatibility
- FTP/FTPS single-stream downloader with anonymous/credential auth, passive mode, resume, explicit TLS
- SFTP single-stream downloader with TOFU host key, password/SSH key auth, resume, custom port
- JSON-RPC 2.0 API with 7 methods, auth token, WebSocket real-time push notifications
- Full integration fix pass (3 phases) for RPC handler wiring and SFTP key persistence

### What Worked
- Strict TDD (red-green-refactor) caught real integration defects early — the 3 fix phases (6, 8, 9) came from audit, not production bugs
- Protocol interface abstraction in Phase 2 paid off immediately — FTP and SFTP plugged in cleanly
- GOB golden fixture test prevented backward compatibility regression throughout all 10 phases
- Reusing existing port+1 web server for JSON-RPC avoided daemon architecture changes

### What Was Inefficient
- Phases 7-10 were all gap closure / documentation — 4 phases of rework that proper frontmatter discipline from Phase 1 would have prevented
- Audit discovered INT-01 and INT-02 (nil handler pass-through) that testing should have caught in original phases 5-6
- FTP credential resume limitation (stripped at add-time, can't resume with auth) is a known design debt that will affect users

### Patterns Established
- ProtocolDownloader interface + SchemeRouter factory pattern for all new protocols
- StripURLCredentials before GOB persistence (security invariant)
- downloadProtocolHandler unified handler for FTP/FTPS/SFTP in API layer (zero duplication)
- TOFU host key verification with isolated known_hosts at ~/.config/warpdl/
- Auth token on every JSON-RPC request including WebSocket upgrade (CSRF defense)

### Key Lessons
1. Wire integration tests for handler pass-through early — nil handlers produce silent failures that only surface under specific conditions (RPC + FTP/SFTP + resume)
2. SUMMARY frontmatter should be enforced by the executor, not backfilled later — 4 phases of documentation rework is pure waste
3. GOB golden fixtures are essential for persistence format changes — the Phase 2 fixture prevented every potential regression

### Cost Observations
- Model mix: ~70% sonnet (execution), ~20% opus (planning), ~10% haiku (verification)
- Sessions: ~5 (1 planning + 4 execution)
- Notable: 21 plans in ~5h total execution, ~14min average per plan. Gap closure phases (6-10) averaged <5min each.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Sessions | Phases | Key Change |
|-----------|----------|--------|------------|
| v1.4 | ~5 | 10 | First milestone — established TDD + audit + gap closure pattern |

### Cumulative Quality

| Milestone | Tests | Coverage | Protocols Added |
|-----------|-------|----------|-----------------|
| v1.4 | ~400+ | 80%+ | FTP, FTPS, SFTP, JSON-RPC |

### Top Lessons (Verified Across Milestones)

1. GOB golden fixtures prevent persistence format regressions
2. Integration tests for handler wiring catch nil-pointer silent failures
