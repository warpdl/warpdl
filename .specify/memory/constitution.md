<!--
  Sync Impact Report
  ==================================================
  Version change: N/A (initial) -> 1.0.0
  Modified principles: N/A (initial fill from template)
  Added sections:
    - 7 Core Principles (I-VII)
    - Technology & Platform Constraints
    - Development Workflow & Quality Gates
    - Governance rules
  Removed sections: None
  Templates requiring updates:
    - .specify/templates/plan-template.md ✅ compatible (Constitution Check generic)
    - .specify/templates/spec-template.md ✅ compatible (no constitution refs)
    - .specify/templates/tasks-template.md ✅ compatible (test-first aligns)
  Follow-up TODOs: None
  ==================================================
-->

# WarpDL Constitution

## Core Principles

### I. Cross-Platform First

All features MUST work on Linux, macOS, and Windows. Platform-specific
code MUST be isolated behind build tags (`//go:build unix`,
`//go:build windows`, etc.) with shared interfaces. FreeBSD, OpenBSD,
and NetBSD receive best-effort support: binaries are provided but
automated CI testing is not required.

- Platform-specific files MUST follow the `*_unix.go` / `*_windows.go`
  naming convention.
- Shared behavior MUST be defined via interfaces so platform backends
  are swappable.
- IPC MUST provide fallback transports (Unix socket -> TCP,
  named pipe -> TCP).
- CGO MUST remain disabled (`CGO_ENABLED=0`) to enable clean
  cross-compilation.

### II. Proven Libraries Over Custom Solutions

Every dependency decision MUST favor battle-tested, widely-adopted
libraries over in-house implementations. Inventing a new solution is
only acceptable when no suitable library exists or existing options
introduce unacceptable constraints (license, performance, maintenance
risk).

- New dependencies MUST be justified with a brief rationale in the
  PR description.
- Vendored or forked dependencies MUST be documented with the
  upstream source and reason for forking.
- Current approved stack: `urfave/cli` (CLI), `dop251/goja` (JS
  runtime), `vbauerster/mpb` (progress bars), `zalando/go-keyring`
  (credentials), `jlaffaye/ftp` (FTP), `pkg/sftp` (SFTP),
  `creachadair/jrpc2` (JSON-RPC), `coder/websocket` (WebSocket).

### III. Test-First Development (NON-NEGOTIABLE)

All new code MUST follow strict TDD with the Red-Green-Refactor cycle.
No implementation code may be written before a failing test exists that
defines the expected behavior. This is the single most non-negotiable
principle in the project.

- **Red**: Write a test that fails for the right reason.
- **Green**: Write the minimum code to make the test pass.
- **Refactor**: Clean up without changing behavior; tests MUST
  remain green.
- Minimum coverage: 80% per package, enforced by CI via
  `scripts/check_coverage.sh`.
- Race detection tests (`go test -race`) MUST pass on all CI
  platforms (Ubuntu, macOS).
- E2E tests (`//go:build e2e`) MUST cover critical download paths
  against real servers.

### IV. Package Isolation & Code Reuse

Packages MUST have clear, single-responsibility boundaries. Code
duplication across packages is a defect. Shared types belong in
`common/`, core engine logic in `pkg/warplib/`, CLI-daemon protocol in
`pkg/warpcli/`, and internal implementation details in `internal/`.

- `pkg/` packages are importable by external consumers; their APIs
  MUST be stable and well-defined.
- `internal/` packages are private; they MAY change without notice.
- New packages MUST justify their existence. Do not create packages
  for organizational convenience alone.
- Handler/callback patterns (`handlers.go`) MUST be used for event
  propagation rather than tight coupling.

### V. Daemon Architecture Integrity

WarpDL operates as a daemon serving multiple CLI clients concurrently.
All changes MUST preserve this architecture. The daemon owns download
state; CLI clients are stateless consumers.

- State persistence MUST go through `Manager` (GOB-encoded at
  `~/.config/warpdl/userdata.warp`).
- All client-daemon communication MUST use the established binary
  protocol (4-byte length prefix + JSON payload).
- Connection pool broadcasting MUST remain the mechanism for
  progress updates to attached clients.
- Daemon lifecycle (start, stop, PID management) MUST remain
  platform-abstracted.

### VI. Performance & Reliability

Downloads MUST be parallelized via HTTP range requests with
configurable segment counts. The work-stealing algorithm MUST
redistribute remaining bytes from slow segments to fast adjacent
segments. Retry logic MUST use exponential backoff.

- Disk space MUST be validated before starting a download.
- Checksum validation (MD5, SHA1, SHA256, SHA512) MUST be performed
  automatically when HTTP headers provide hash values.
- Priority queue (low/normal/high) MUST manage concurrent download
  limits.
- Protocol support (HTTP, FTP, SFTP) MUST be extensible via the
  `SchemeRouter` without modifying existing protocol handlers.

### VII. Simplicity & YAGNI

Start simple. Do not build for hypothetical future requirements.
Complexity MUST be justified and documented. If a simpler alternative
exists that meets current needs, use it.

- No abstraction layers unless two or more concrete implementations
  exist today.
- No feature flags or configuration options for behavior that can
  simply be the default.
- `go fmt` and `go vet` are the only required static analysis tools.
  Do not add linters unless they catch real, recurring defects.
- Error handling MUST use `fmt.Errorf` with `%w` wrapping. Do not
  build custom error frameworks.

## Technology & Platform Constraints

- **Language**: Go 1.24.9+ (see `go.mod`).
- **Build**: `make build` produces stripped binaries
  (`-ldflags="-w -s"`). GoReleaser handles releases with
  `-trimpath` and ldflags injection for version/commit/date.
- **Platforms**: darwin/amd64, darwin/arm64, linux/amd64, linux/arm64,
  linux/386, linux/arm, windows/amd64, windows/386, windows/arm64,
  freebsd/amd64, openbsd/amd64, netbsd/amd64, android/arm64.
- **Packaging**: Homebrew (macOS), Scoop (Windows), deb/rpm (Linux
  via Cloudsmith), Docker (ghcr.io/warpdl/warp-cli).
- **CI**: GitHub Actions. Ubuntu + macOS run tests + coverage + race
  detection. Windows is build-only.
- **Storage**: GOB-encoded files for download state, JSON for
  extension engine state. No external databases.
- **IPC**: Custom binary protocol over Unix sockets / Windows named
  pipes / TCP fallback. Max message size 16 MB.
- **Extensions**: Goja JavaScript runtime with Node.js compatibility
  for URL transformation hooks.

## Development Workflow & Quality Gates

- **Branching**: `dev` is the main development branch. PRs target
  `dev`, not `main`. `main` receives merges from `dev` for releases.
- **Commit format**: `<area>...: <type>: <message>`. Areas: `core`,
  `daemon`, `cli`, `api`, `extl`, `credman`, `docs`, `debug`. Types:
  `feat`, `fix`, `refactor`, `perf`, `test`, `chore`, `temp`.
- **PR requirements**:
  1. All CI checks MUST pass (tests, coverage, race detection).
  2. Coverage MUST meet 80% minimum per package.
  3. New dependencies MUST be justified.
  4. Platform-specific code MUST be isolated with build tags.
- **Test isolation**: `cmd/` tests MUST set
  `WARPDL_TEST_SKIP_DAEMON=1`. E2E tests MUST use `//go:build e2e`
  tag and tolerate network failures (any-pass logic across mirrors).
- **Release process**: Tag push triggers GoReleaser, which builds
  all platforms, signs artifacts, pushes to package managers, and
  publishes Docker images.

## Governance

This constitution is the authoritative source of project principles
and constraints. It supersedes all other documentation when conflicts
arise.

- **Amendments**: Any change to this constitution MUST be documented
  with a version bump, rationale, and migration plan if principles
  are removed or redefined. Changes MUST be reviewed and approved
  via PR.
- **Versioning**: Semantic versioning (MAJOR.MINOR.PATCH). MAJOR for
  principle removals or incompatible redefinitions. MINOR for new
  principles or material expansions. PATCH for clarifications and
  typo fixes.
- **Compliance**: All PRs and code reviews MUST verify adherence to
  these principles. Violations MUST be flagged and resolved before
  merge.
- **Runtime guidance**: See `CLAUDE.md` for operational development
  guidance that complements this constitution.

**Version**: 1.0.0 | **Ratified**: 2026-02-28 | **Last Amended**: 2026-02-28
