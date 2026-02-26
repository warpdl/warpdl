# Technology Stack

**Analysis Date:** 2026-02-26

## Languages

**Primary:**
- Go 1.24.9 - Core daemon and CLI application for cross-platform download management

**JavaScript (Optional):**
- Goja runtime for extension scripting - Used in `internal/extl/` for JavaScript-based URL extraction and custom download rules

## Runtime

**Environment:**
- Go compiler (native binary compilation)
- Cross-platform: Linux, macOS, Windows, FreeBSD, OpenBSD, NetBSD, Android

**Package Manager:**
- Go Modules (go.mod/go.sum)
- Lockfile: `go.sum` present

## Frameworks

**Core:**
- `github.com/urfave/cli v1.22.17` - CLI framework for command parsing and subcommand routing in `cmd/` package
- `github.com/dop251/goja v0.0.0-20260106131823-651366fbe6e3` - JavaScript runtime engine for extension system
- `github.com/dop251/goja_nodejs v0.0.0-20251015164255-5e94316bedaf` - Node.js compatibility layer for Goja

**HTTP & Networking:**
- `golang.org/x/net v0.49.0` - Extended net package (includes proxy support via `golang.org/x/net/proxy` for SOCKS5)
- `golang.org/x/sys v0.40.0` - Platform-specific system calls (Unix/Windows disk space, named pipes, event logs)

**UI & Progress:**
- `github.com/vbauerster/mpb/v8 v8.11.3` - Multi-part progress bar for download visualization

**Credentials & Security:**
- `github.com/zalando/go-keyring v0.2.6` - OS keyring integration for credential storage (with file-based fallback)
- `github.com/danieljoos/wincred v1.2.3` - Windows credential manager backend
- `github.com/godbus/dbus/v5 v5.2.2` - D-Bus support for Linux keyring (Secret Service API)
- `github.com/Microsoft/go-winio v0.6.2` - Windows-specific I/O (named pipes for daemon IPC)

**Testing:**
- `github.com/stretchr/testify v1.11.1` - Assertion library for tests

**Build & Release:**
- GoReleaser (external tool, configured in `.goreleaser.yml`) - Multi-platform build automation
- Goreleaser Docker builder - Container image builds for `ghcr.io/warpdl/warp-cli`

## Key Dependencies

**Critical:**
- `github.com/urfave/cli` - All CLI command routing depends on this. Entry point: `cmd/Execute()`
- `github.com/dop251/goja` - JavaScript extension engine. Location: `internal/extl/engine.go`
- `golang.org/x/net/proxy` - Proxy and SOCKS5 support. Location: `pkg/warplib/proxy.go`
- `github.com/zalando/go-keyring` - Secure credential storage. Location: `pkg/credman/keyring/`

**Infrastructure:**
- `github.com/vbauerster/mpb/v8` - Download progress display in daemon and CLI
- `golang.org/x/sys` - Platform-specific operations:
  - Disk space checking: `pkg/warplib/diskspace_*.go`
  - Windows services: `internal/service/`
  - Event logging: `pkg/logger/eventlog_windows.go`

**Indirect (Goja dependencies):**
- `github.com/google/pprof` - CPU profiling support in Goja
- `github.com/dlclark/regexp2` - JavaScript regex implementation
- `github.com/go-sourcemap/sourcemap` - JavaScript sourcemap support

## Configuration

**Build Configuration:**
- `.goreleaser.yml` - Multi-platform release configuration:
  - Builds for: darwin, linux, windows, freebsd, netbsd, openbsd, android
  - Architectures: 386, amd64, arm, arm64
  - Outputs: tar.gz (Unix), zip (Windows), deb/rpm packages, Docker images
  - Code signing enabled with GPG
  - Changelog auto-generation from commits

**Build Flags:**
- Version, commit, date, and buildType injected at link time via `-ldflags`
- Stripped and optimized builds with `-w -s` flags for production

**Environment Configuration:**
- Proxy configuration: `HTTP_PROXY`, `http_proxy`, `HTTPS_PROXY`, `https_proxy`, `ALL_PROXY`, `all_proxy`
- Windows-specific: Named pipe fallback to TCP for daemon communication
- Unix: Unix domain socket at `/tmp/warpdl.sock` with TCP fallback

## Platform Requirements

**Development:**
- Go 1.24.9+
- CGO_ENABLED=0 for cross-compilation (pure Go binaries)
- CGO_ENABLED=1 for credential manager on some platforms

**Production - Linux:**
- Native packages: deb/rpm via systemd service
- Cloudsmith repository support for auto-updates
- D-Bus daemon for keyring access (Secret Service API)

**Production - macOS:**
- Homebrew formula via `warpdl/tap/warpdl`
- Launchd service management via Homebrew services
- Native keychain integration via `zalando/go-keyring`

**Production - Windows:**
- Named pipes for IPC (primary), TCP fallback
- Windows Credential Manager for credential storage
- Scoop package manager support

**Production - Docker:**
- Alpine Linux base image (`FROM alpine`)
- FFmpeg included for media processing
- Multi-arch builds: linux/amd64, linux/arm64

## Storage & Persistence

**Data Files:**
- Manager state (GOB format): `~/.config/warpdl/userdata.warp` - Persisted download metadata
- Extension engine state: `~/.config/warpdl/extstore/module_engine.json`
- Extension modules: `~/.config/warpdl/extstore/` directory

**IPC & Sockets:**
- Unix: `/tmp/warpdl.sock` - Daemon listening socket
- Windows: Named pipes (implementation in `internal/server/listener_windows*.go`)
- Fallback: TCP on port configurable via daemon

**Cookies & Credentials:**
- OS keyring (primary): System keychain/credential manager
- File-based fallback: `~/.config/warpdl/` (fallback storage: `pkg/credman/keyring/fallback.go`)

---

*Stack analysis: 2026-02-26*
