# External Integrations

**Analysis Date:** 2026-02-26

## APIs & External Services

**HTTP Downloads:**
- Target: Any HTTP/HTTPS server with parallel segment download support
  - SDK/Client: Go standard library `net/http`
  - Implementation: `pkg/warplib/dloader.go` - Parallel segment downloader with range requests
  - Features: Accept-Ranges header detection, force-parts override, resumable downloads

**JavaScript Extension APIs:**
- Goja JavaScript runtime for custom URL extraction and download rules
  - SDK/Client: `github.com/dop251/goja` and `github.com/dop251/goja_nodejs`
  - Implementation: `internal/extl/engine.go`
  - Data flow: URL passed through extension Extract() → custom logic → final URL
  - Use case: Site-specific download URL transformation (e.g., redirect handling)

## Data Storage

**Databases:**
- Not used - This is a stateless download manager

**File Storage:**
- Local filesystem only - All downloads are saved to user-specified directories
- Directory configuration: Per-download or via CLI flags `--output-dir`
- Metadata storage: GOB-encoded binary format in `~/.config/warpdl/userdata.warp`

**Caching:**
- HTTP client uses Go's standard caching via `http.Client`
- Cookie jar: Optional, configured per client in `pkg/credman/types/cookie.go`
- No external cache service

## Authentication & Identity

**Auth Provider:**
- Custom implementation - No external OAuth/SSO provider
- Cookie-based for HTTP requests: `pkg/credman/types/cookie.go`
- Custom headers support: `common.DownloadParams.Headers` field

**Credential Management:**
- Primary: OS keyring integration
  - macOS: Native Keychain via `zalando/go-keyring`
  - Linux: D-Bus Secret Service API via `zalando/go-keyring` + `godbus/dbus/v5`
  - Windows: Windows Credential Manager via `zalando/go-keyring` + `danieljoos/wincred`
  - Implementation: `pkg/credman/manager.go` - Centralized credential manager
  - Encryption: AES-256-GCM for file-based fallback in `pkg/credman/encryption/`

**Browser Extension Authentication:**
- Native messaging host for Chrome/Firefox/Edge/Brave
- Native host identifier: `com.warpdl.host`
- Communication: stdio-based JSON protocol
- Implementation: `internal/nativehost/protocol.go` - Bi-directional message handling

## Monitoring & Observability

**Error Tracking:**
- None - Errors logged to stdout/logger only

**Logs:**
- Console logging via `pkg/logger/logger.go`
- File logging: Windows Event Log support via `pkg/logger/eventlog_windows.go`
- Debug mode: Enabled via `-d` or `--debug` CLI flags
- Log level: Centralized in `pkg/logger/multi.go` for stdout + file output

## CI/CD & Deployment

**Hosting:**
- GitHub Releases - Primary distribution point
- Homebrew (macOS): Formula repo at `warpdl/homebrew-tap`
- Scoop (Windows): Bucket at `warpdl/scoop-bucket`
- Docker: Container registry at `ghcr.io/warpdl/warp-cli`
- Linux: Package repositories via Cloudsmith (auto-updates via apt/yum)

**CI Pipeline:**
- GitHub Actions (`.github/workflows/`)
  - CI workflow: `ci.yml` - Runs on push to main/dev, pull_request
    - Test matrix: Ubuntu (coverage 80%+, race detection), macOS (coverage 80%+, race detection)
    - Coverage enforcement: `scripts/check_coverage.sh` - Per-package minimum 80%
    - Build: `go build -ldflags="-w -s"`
    - Race detection: `go test -race -short ./...`
  - Release workflow: `release.yml` - Automated release on tag creation
    - Invokes GoReleaser for multi-platform builds
    - GPG signing of artifacts
    - Automatic Homebrew formula update
    - Docker image push to GHCR
  - Docs workflow: `docs.yml` - Documentation generation

**Dependabot:**
- Configured in `.github/dependabot.yml` for go.mod updates

## Environment Configuration

**Required Environment Variables:**
- None mandatory - All features have sensible defaults

**Optional Environment Variables:**
- `HTTP_PROXY`, `http_proxy` - HTTP proxy URL
- `HTTPS_PROXY`, `https_proxy` - HTTPS proxy URL
- `ALL_PROXY`, `all_proxy` - Fallback proxy for all protocols
- `NO_PROXY` - Hosts to bypass proxy (handled by Go's http.ProxyFromEnvironment)
- `GPG_PASSPHRASE` - Used during release signing (CI only, stored in GitHub Secrets)

**Secrets Location:**
- GitHub Secrets (CI/CD):
  - `GPG_PASSPHRASE` - For artifact signing during releases
  - Automated Homebrew/Scoop updates via bot credentials (in release workflow)
- OS Keyring (Runtime):
  - Cookie storage for authenticated downloads
  - User credentials for proxy authentication

## Webhooks & Callbacks

**Incoming:**
- None - This is a CLI download manager, not a server

**Outgoing:**
- GitHub API: Used by GoReleaser for release creation and Homebrew formula updates
  - Authenticated via GITHUB_TOKEN (available in release workflow context)
  - Implementation: GoReleaser (external tool), not in application code

**Browser Extension Communication:**
- Native messaging protocol via stdio
  - Message format: JSON with length-prefixed encoding
  - Handlers: `internal/nativehost/protocol.go`
  - Supported browsers: Chrome, Firefox, Chromium, Edge, Brave
  - Bidirectional: Browser → Host (download requests) and Host → Browser (status/responses)

## Proxy & Network Configuration

**Proxy Support:**
- HTTP/HTTPS proxies: Via `http.ProxyURL()` in `net/http` standard library
- SOCKS5 proxies: Via `golang.org/x/net/proxy.SOCKS5()`
- Implementation: `pkg/warplib/proxy.go`:
  - `ParseProxyURL()` - Validate proxy URL format
  - `NewHTTPClientWithProxy()` - Create client with specified proxy
  - `NewHTTPClientFromEnvironment()` - Use environment proxy settings
  - `NewHTTPClientWithProxyAndTimeout()` - Proxy with custom timeout
- Per-download proxy override: `common.DownloadParams.Proxy` field in API

**Network Features:**
- HTTP Range requests: For resumable/parallel downloads (Accept-Ranges header)
- Custom headers: Per-download via `common.DownloadParams.Headers`
- Timeout support: Per-request timeout in milliseconds
- Retry configuration: Exponential backoff with configurable max retries and base delay

## SSL/TLS Configuration

**Certificate Handling:**
- Default: OS certificate store (via Go runtime)
- Insecure mode: Optional `InsecureSkipVerify` in `crypto/tls.Config` (test-only, `pkg/warplib/proxy_test.go`)
- No custom certificate pinning

## Download Quality & Validation

**Checksum Validation:**
- Automatic extraction from HTTP headers: `pkg/warplib/checksum.go`
  - Supported algorithms: MD5, SHA1, SHA256, SHA512
  - Header names checked: `Content-MD5`, `X-Checksum-*`
- Configurable validation: Optional checksum config per download
- On-the-fly validation: Computed while downloading

**Speed Limiting:**
- Per-download speed limit: `pkg/warplib/speed.go`
- Configurable: Via `common.DownloadParams.SpeedLimit` field
- Format parsing: `warplib.ParseSpeedLimit()` supports human-readable formats

**Resumable Downloads:**
- State persistence: GOB-encoded to `userdata.warp`
- Resume implementation: `warplib.ResumeDownload()` method
- Queue state preservation: Partial downloads kept in queue until resumed/resumed

---

*Integration audit: 2026-02-26*
