---
spec: issue-129
phase: requirements
created: 2026-01-16
generated: auto
---

# Requirements: Auto-install Native Messaging Host via Package Manager Hooks

## Summary

Enable zero-configuration native messaging host installation when users install WarpDL via package managers (Homebrew, Scoop, APT, RPM).

## User Stories

### US-1: Zero-flag installation with official extension

As a WarpDL user installing via package manager,
I want native messaging host to be configured automatically,
so that the browser extension works immediately after installation.

**Acceptance Criteria**:
- AC-1.1: Running `warpdl native-host install` without flags uses official extension IDs (when configured)
- AC-1.2: Package manager installation automatically runs native host setup
- AC-1.3: Installation succeeds silently even if extension IDs not yet configured
- AC-1.4: Manual flag override still works for custom extensions

### US-2: Custom extension support

As a developer building a custom WarpDL browser extension,
I want to specify my own extension IDs via flags,
so that my extension can communicate with WarpDL.

**Acceptance Criteria**:
- AC-2.1: `--chrome-extension-id` flag overrides default Chrome ID
- AC-2.2: `--firefox-extension-id` flag overrides default Firefox ID
- AC-2.3: Both custom and default IDs can coexist (official + custom)

### US-3: Clean uninstallation

As a user uninstalling WarpDL,
I want native messaging manifests removed automatically,
so that no orphaned configuration files remain.

**Acceptance Criteria**:
- AC-3.1: Homebrew uninstall removes native messaging manifests
- AC-3.2: Scoop uninstall removes native messaging manifests
- AC-3.3: DEB/RPM removal removes native messaging manifests
- AC-3.4: Uninstall succeeds even if manifests don't exist

## Functional Requirements

| ID | Requirement | Priority | Source |
|----|-------------|----------|--------|
| FR-1 | Create `internal/nativehost/defaults.go` with placeholder extension IDs | Must | US-1 |
| FR-2 | Modify install command to use defaults when flags empty | Must | US-1, AC-1.1 |
| FR-3 | Add `--auto` flag for silent/non-failing operation | Must | US-1, AC-1.3 |
| FR-4 | Update Homebrew formula with post_install hook | Must | US-1, AC-1.2 |
| FR-5 | Update Homebrew formula with post_uninstall hook | Must | US-3, AC-3.1 |
| FR-6 | Update Scoop manifest patching with post_install hook | Must | US-1, AC-1.2 |
| FR-7 | Update Scoop manifest patching with pre_uninstall hook for native host | Must | US-3, AC-3.2 |
| FR-8 | Update DEB/RPM postinstall.sh with native host install | Must | US-1, AC-1.2 |
| FR-9 | Update DEB/RPM preremove.sh with native host uninstall | Must | US-3, AC-3.3 |
| FR-10 | Flag override must take precedence over defaults | Must | US-2 |

## Non-Functional Requirements

| ID | Requirement | Category |
|----|-------------|----------|
| NFR-1 | Package manager hooks must not fail installation on native host errors | Reliability |
| NFR-2 | Native host install must complete in < 2 seconds | Performance |
| NFR-3 | All scripts must be POSIX sh compatible (not bash) | Portability |
| NFR-4 | Windows scripts must be PowerShell compatible | Portability |

## Out of Scope

- Actual browser extension publishing (blocked dependency)
- Real extension ID values (placeholders until extensions published)
- Windows registry-based native messaging (existing limitation)
- System-wide installation (user-level only)

## Dependencies

- Official WarpDL browser extension publication (for real extension IDs)
- goreleaser for package generation
- jq for Scoop manifest patching

## Glossary

| Term | Definition |
|------|------------|
| Native Messaging Host | Browser feature allowing extensions to communicate with native applications |
| Extension ID | Unique identifier for browser extensions (Chrome: 32-char alphanumeric, Firefox: email-like or UUID) |
| Manifest | JSON file declaring native messaging host configuration |
