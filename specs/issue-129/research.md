---
spec: issue-129
phase: research
created: 2026-01-16
generated: auto
---

# Research: Auto-install Native Messaging Host via Package Manager Hooks

## Executive Summary

Native messaging host implementation exists in `internal/nativehost/` and CLI in `cmd/nativehost/`. Current install requires manual `--chrome-extension-id` and `--firefox-extension-id` flags. Package manager hooks already exist for daemon lifecycle (Homebrew, DEB/RPM) but not for native host installation. Feasibility is high - straightforward extension of existing patterns.

## Codebase Analysis

### Existing Patterns

| File | Pattern |
|------|---------|
| `cmd/nativehost/install.go` | CLI flag parsing, `ManifestInstaller` usage |
| `cmd/nativehost/cmd.go` | Flag definitions: `chrome-extension-id`, `firefox-extension-id` |
| `internal/nativehost/manifest.go` | `ManifestInstaller` struct, `InstallChrome()`, `InstallFirefox()` |
| `.goreleaser.yml:171-179` | Homebrew `post_install` hook pattern (Ruby) |
| `scripts/postinstall.sh` | DEB/RPM post-install pattern (POSIX sh) |
| `scripts/preremove.sh` | DEB/RPM pre-remove pattern (POSIX sh) |
| `scripts/patch-scoop-manifest.sh` | Scoop manifest patching via jq |

### Dependencies

- `urfave/cli` - CLI framework (already used)
- goreleaser - Handles Homebrew, Scoop, nfpm (DEB/RPM)
- jq - Used for Scoop manifest patching

### Constraints

1. **Extension IDs unknown** - Official browser extensions not published yet
2. **Platform-specific paths** - Native host manifests go to different locations per OS/browser
3. **Silent failures OK** - Package manager hooks should not fail installation if native host setup fails
4. **No root for user dirs** - DEB/RPM run as root but native host needs user home directory

## Feasibility Assessment

| Aspect | Assessment | Notes |
|--------|------------|-------|
| Technical Viability | High | Simple flag defaulting + hook additions |
| Effort Estimate | S | ~2-3 hours implementation |
| Risk Level | Low | Non-breaking, additive changes only |

## Key Findings

### 1. Current CLI Validation

```go
// cmd/nativehost/install.go:17-19
if chromeID == "" && firefoxID == "" {
    return cli.NewExitError("at least one extension ID is required", 1)
}
```

This check needs modification to use defaults when flags not provided.

### 2. Homebrew Formula Pattern

```ruby
# .goreleaser.yml:171-179
post_install: |
  begin
    system "#{bin}/warpdl", "stop-daemon"
  rescue
  end
```

Add native host install after this pattern.

### 3. Scoop Patching Exists

`scripts/patch-scoop-manifest.sh` already patches Scoop manifests post-generation. Extend to add `post_install` hook.

### 4. DEB/RPM Scripts

`scripts/postinstall.sh` runs post-install but only shows instructions. Needs native host install added.
`scripts/preremove.sh` handles daemon cleanup. Needs native host uninstall added.

## Recommendations

1. Create `internal/nativehost/defaults.go` with placeholder extension IDs (empty strings initially)
2. Modify `cmd/nativehost/install.go` to fall back to defaults when flags empty
3. Add `--auto` flag for silent operation (no error on missing IDs, for package manager use)
4. Update all package manager hooks to call `warpdl native-host install --auto`
5. Update uninstall hooks to call `warpdl native-host uninstall --browser all`
