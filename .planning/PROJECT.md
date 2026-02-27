# WarpDL Issues Fix Milestone

## What This Is

A targeted milestone addressing 4 open GitHub issues for WarpDL — a daemon-based download manager with parallel segment downloading. This work adds HTTP redirect following (#144), FTP/FTPS protocol support (#138), SFTP protocol support (#139), and a JSON-RPC API for third-party integrations (#137).

## Core Value

Expand WarpDL's protocol coverage and integration surface so it can download from more sources (FTP, SFTP, redirect chains) and be controlled programmatically by external tools.

## Requirements

### Validated

- HTTP/HTTPS parallel segment downloading — existing
- Resume support with byte-range requests — existing
- Daemon-based architecture with CLI client — existing
- Download queue with priority management — existing
- Browser extension integration via native messaging — existing
- JavaScript extension engine for URL transformation — existing
- Credential management with OS keyring — existing
- Checksum validation (MD5, SHA1, SHA256, SHA512) — existing
- Work stealing between segments — existing
- Cross-platform support (Linux, macOS, Windows) — existing
- Batch URL download from input file (#136) — existing
- Download queue manager with concurrent limits (#142) — existing

### Active

- [ ] HTTP redirect following (#144) — Follow HTTP 3xx redirects transparently
- [ ] FTP/FTPS protocol support (#138) — Download from ftp:// URLs with auth, passive mode, resume
- [ ] SFTP protocol support (#139) — Download from sftp:// URLs with password/key auth
- [ ] JSON-RPC 2.0 API (#137) — HTTP/WebSocket API for third-party integration

### Out of Scope

- SFTP parallel segments — SFTP is single-stream by design
- FTP parallel segments — FTP doesn't support range requests natively
- FTP active mode — NAT-unfriendly, passive mode is default
- SFTP SSH agent support — P1 enhancement, not MVP
- JSON-RPC batch requests — P1 enhancement
- JSON-RPC rate limiting — P1 enhancement
- AriaNG web UI — Enabled by JSON-RPC but separate project
- Mobile app — Enabled by JSON-RPC but separate project

## Context

- WarpDL is a Go 1.24.9+ project using urfave/cli, Goja, mpb for progress bars
- Daemon architecture: CLI → warpcli → Unix socket → server → API → warplib
- Downloads persist as GOB-encoded data at `~/.config/warpdl/userdata.warp`
- The server already has a secondary HTTP web server on `port+1` that can host the JSON-RPC endpoint
- Current download engine is HTTP-only — FTP/SFTP require new downloader implementations
- HTTP redirect support is likely a small change in the existing HTTP client configuration
- Issue #138 and #139 suggest specific Go libraries: `github.com/jlaffaye/ftp` and `golang.org/x/crypto/ssh` + `github.com/pkg/sftp`
- E2E tests exist in `tests/e2e/` with build tag `e2e`
- CI enforces 80% coverage per package

## Constraints

- **Tech Stack**: Go 1.24.9+, must maintain cross-platform compatibility (Linux, macOS, Windows)
- **Coverage**: 80% minimum per package enforced by CI
- **Protocol Limits**: FTP/SFTP are single-stream protocols — no parallel segments
- **Security**: JSON-RPC must require auth token by default, bind localhost only
- **Backwards Compatibility**: Existing CLI behavior must not change — new features are additive

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Use `github.com/jlaffaye/ftp` for FTP | Mature Go FTP library with TLS support, suggested in issue | — Pending |
| Use `golang.org/x/crypto/ssh` + `github.com/pkg/sftp` for SFTP | Standard Go SSH library + established SFTP client | — Pending |
| JSON-RPC 2.0 over HTTP/WebSocket | Industry standard, aria2 compatible, enables AriaNG | — Pending |
| Localhost-only binding for JSON-RPC by default | Security — remote access requires explicit opt-in | — Pending |
| Single-stream for FTP/SFTP downloads | Protocol limitation — no range request support | — Pending |

---
*Last updated: 2026-02-26 after initialization*
