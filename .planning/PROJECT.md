# WarpDL

## What This Is

A daemon-based download manager with parallel segment downloading, supporting HTTP/HTTPS, FTP/FTPS, and SFTP protocols. Features transparent redirect following, pluggable protocol backends, and a JSON-RPC 2.0 API for programmatic control via HTTP and WebSocket.

## Core Value

Fast, reliable, multi-protocol downloads with a daemon architecture that supports parallel segments (HTTP), single-stream protocols (FTP/SFTP), and third-party integration via JSON-RPC.

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
- HTTP redirect following (#144) — v1.4
- FTP/FTPS protocol support (#138) — v1.4
- SFTP protocol support (#139) — v1.4
- JSON-RPC 2.0 API (#137) — v1.4

### Active

(None — define in next milestone)

### Out of Scope

- SFTP parallel segments — SFTP is single-stream by design
- FTP parallel segments — FTP doesn't support range requests natively
- FTP active mode — NAT-unfriendly, passive mode is default
- SFTP SSH agent support — P1 enhancement, not MVP (issue #139)
- JSON-RPC batch requests — P1 enhancement
- JSON-RPC rate limiting — P1 enhancement
- AriaNG web UI — Enabled by JSON-RPC but separate project
- Mobile app — Enabled by JSON-RPC but separate project
- FTP/SFTP directory/recursive download — Major scope expansion
- FTP/SFTP upload — Different use case, different command surface
- OAuth2/JWT authentication for RPC — Inappropriate for local desktop tool

## Context

Shipped v1.4 with ~69,414 LOC Go.
Tech stack: Go 1.24.9+, urfave/cli, Goja, mpb/v8, jlaffaye/ftp, pkg/sftp, creachadair/jrpc2, coder/websocket.
Architecture: CLI → warpcli → Unix socket → server → API → warplib (protocol router → HTTP/FTP/SFTP downloaders).
JSON-RPC 2.0 on existing port+1 web server with auth token and localhost binding.
FTP/SFTP credential resume limitation: URL-embedded credentials stripped at add-time for security; resume uses anonymous/key-only auth.

## Constraints

- **Tech Stack**: Go 1.24.9+, cross-platform (Linux, macOS, Windows)
- **Coverage**: 80% minimum per package enforced by CI
- **Protocol Limits**: FTP/SFTP are single-stream — no parallel segments
- **Security**: JSON-RPC requires auth token, localhost-only by default
- **Backwards Compatibility**: GOB persistence backward-compatible (Protocol=0=HTTP)

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Use `github.com/jlaffaye/ftp` for FTP | Mature Go FTP library with TLS support, suggested in issue | Good |
| Use `golang.org/x/crypto/ssh` + `github.com/pkg/sftp` for SFTP | Standard Go SSH library + established SFTP client | Good |
| Use `github.com/creachadair/jrpc2` + `github.com/coder/websocket` for JSON-RPC | Standards-compliant, not archived (unlike gorilla/websocket) | Good |
| Localhost-only binding for JSON-RPC by default | Security — remote access requires explicit opt-in | Good |
| Single-stream for FTP/SFTP downloads | Protocol limitation — no range request support | Good |
| HTTP redirect uses Go default CheckRedirect (nil) | Custom header copying reintroduces CVE-2024-45336 | Good |
| TOFU auto-accepts unknown SSH hosts silently | Daemon is headless, no interactive prompt | Good |
| FTP ServerConn maxConnections=1 at type level | jlaffaye/ftp ServerConn is not goroutine-safe | Good |
| Strip URL credentials before GOB persistence | Security — credentials must not persist in plaintext | Good |
| Item.SSHKeyPath field for SFTP resume | SSH key path must survive pause/resume cycles | Good |
| Protocol=0=ProtoHTTP GOB invariant | Backward compatible zero value for existing downloads | Good |

---
*Last updated: 2026-02-27 after v1.4 milestone*
