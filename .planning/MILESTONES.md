# Milestones

## v1.4 Issues Fix (Shipped: 2026-02-27)

**Phases completed:** 10 phases, 21 plans
**Timeline:** 2 days (2026-02-26 → 2026-02-27)
**Commits:** 90 | **Go files changed:** 58 | **Lines added:** ~11,663

**Key accomplishments:**
- HTTP redirect following with cross-origin credential protection (CVE-2024-45336 guard)
- Pluggable protocol interface with URL-scheme router and GOB backward compatibility
- FTP/FTPS downloader with passive mode, credential auth, resume, and explicit TLS
- SFTP downloader with TOFU host key verification, password/key auth, and resume
- JSON-RPC 2.0 API (HTTP + WebSocket) with auth token, real-time push notifications
- Full integration fix pass — RPC FTP/SFTP handler wiring, resume handler pass-through, SFTP key persistence

**Known Tech Debt:**
- FTP/SFTP credential-based downloads strip URL credentials at add-time; resume re-authenticates as anonymous (design limitation, SSH key auth unaffected)
- 4 requirements have only 2 SUMMARY frontmatter sources (below 3-source target, not a blocker)

**Archive:** `.planning/milestones/v1.4-ROADMAP.md`, `.planning/milestones/v1.4-REQUIREMENTS.md`, `.planning/milestones/v1.4-MILESTONE-AUDIT.md`

---

