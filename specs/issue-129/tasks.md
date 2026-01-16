---
spec: issue-129
phase: tasks
total_tasks: 14
created: 2026-01-16
generated: auto
---

# Tasks: Auto-install Native Messaging Host via Package Manager Hooks

## Phase 1: Make It Work (POC)

Focus: Get defaults working and one package manager hook functional. Skip tests initially.

- [x] 1.1 Create defaults.go with placeholder extension IDs
  - **Do**: Create `internal/nativehost/defaults.go` with `OfficialChromeExtensionID`, `OfficialFirefoxExtensionID` constants (empty strings), and `HasOfficialExtensions()` function
  - **Files**: `/Users/divkix/GitHub/warpdl/internal/nativehost/defaults.go`
  - **Done when**: File compiles, `go build .` succeeds
  - **Verify**: `go build . && grep -q "OfficialChromeExtensionID" internal/nativehost/defaults.go`
  - **Commit**: `core: feat: add default extension ID constants for native host`
  - _Requirements: FR-1_
  - _Design: Component defaults.go_

- [x] 1.2 Add --auto flag to install command
  - **Do**: Add `--auto` BoolFlag to `installFlags` in `cmd/nativehost/cmd.go`
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/nativehost/cmd.go`
  - **Done when**: `warpdl native-host install --help` shows `--auto` flag
  - **Verify**: `go build . && ./warpdl native-host install --help | grep -q "\-\-auto"`
  - **Commit**: `cli: feat: add --auto flag to native-host install`
  - _Requirements: FR-3_
  - _Design: CLI Changes_

- [x] 1.3 Modify install.go to use defaults and handle --auto
  - **Do**:
    1. Import `nativehost` package for defaults
    2. Read `--auto` flag
    3. If chrome/firefox flags empty, use `nativehost.OfficialChromeExtensionID` / `OfficialFirefoxExtensionID`
    4. If auto mode and no IDs available, return nil (success) silently
    5. Keep existing validation for non-auto mode
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/nativehost/install.go`
  - **Done when**: `warpdl native-host install --auto` exits 0 even with empty default IDs
  - **Verify**: `go build . && ./warpdl native-host install --auto; echo "Exit code: $?"`
  - **Commit**: `cli: feat: use default extension IDs in native-host install`
  - _Requirements: FR-2, FR-3, FR-10_
  - _Design: Component Modified install.go_

- [x] 1.4 Update Homebrew formula with post_install hook
  - **Do**: Add native host install call to `.goreleaser.yml` brews[0].post_install section after daemon stop
  - **Files**: `/Users/divkix/GitHub/warpdl/.goreleaser.yml`
  - **Done when**: post_install contains `native-host install --auto` command
  - **Verify**: `grep -A5 "post_install" .goreleaser.yml | grep -q "native-host"`
  - **Commit**: `cli: feat: add native host install to Homebrew post_install hook`
  - _Requirements: FR-4_
  - _Design: Homebrew Formula Hook_

- [x] 1.5 POC Checkpoint
  - **Do**: Verify core flow works - defaults created, auto flag works, Homebrew hook present
  - **Done when**: Manual test of `warpdl native-host install --auto` succeeds silently
  - **Verify**: `go build . && ./warpdl native-host install --auto && echo "POC works"`
  - **Commit**: `cli: feat: complete POC for auto native host installation`

## Phase 2: Complete All Hooks

Add remaining package manager hooks.

- [ ] 2.1 Add Homebrew post_uninstall hook
  - **Do**: Add `post_uninstall` to `.goreleaser.yml` brews section with `native-host uninstall --browser all`
  - **Files**: `/Users/divkix/GitHub/warpdl/.goreleaser.yml`
  - **Done when**: post_uninstall section exists with native-host uninstall
  - **Verify**: `grep -A3 "post_uninstall" .goreleaser.yml | grep -q "native-host"`
  - **Commit**: `cli: feat: add native host uninstall to Homebrew post_uninstall`
  - _Requirements: FR-5_
  - _Design: Homebrew Formula Hook_

- [ ] 2.2 Update Scoop manifest patching for post_install
  - **Do**: Modify `scripts/patch-scoop-manifest.sh` to add `post_install` array with native host install command
  - **Files**: `/Users/divkix/GitHub/warpdl/scripts/patch-scoop-manifest.sh`
  - **Done when**: Script adds post_install with native-host install to manifest
  - **Verify**: `echo '{}' > /tmp/test.json && ./scripts/patch-scoop-manifest.sh /tmp/test.json && cat /tmp/test.json | grep -q "native-host"`
  - **Commit**: `cli: feat: add native host install to Scoop post_install hook`
  - _Requirements: FR-6_
  - _Design: Scoop Manifest Hook_

- [ ] 2.3 Update Scoop manifest patching for pre_uninstall native host
  - **Do**: Modify `scripts/patch-scoop-manifest.sh` to add native host uninstall to `pre_uninstall` array (before existing daemon stop)
  - **Files**: `/Users/divkix/GitHub/warpdl/scripts/patch-scoop-manifest.sh`
  - **Done when**: pre_uninstall includes native-host uninstall before daemon stop
  - **Verify**: `./scripts/patch-scoop-manifest.sh /tmp/test.json && cat /tmp/test.json | jq '.pre_uninstall[0]' | grep -q "native-host"`
  - **Commit**: `cli: feat: add native host uninstall to Scoop pre_uninstall hook`
  - _Requirements: FR-7_
  - _Design: Scoop Manifest Hook_

- [ ] 2.4 Update DEB/RPM postinstall.sh
  - **Do**: Add native host install call at end of `scripts/postinstall.sh` with proper error handling
  - **Files**: `/Users/divkix/GitHub/warpdl/scripts/postinstall.sh`
  - **Done when**: Script contains `warpdl native-host install --auto` with `|| true`
  - **Verify**: `grep -q "native-host install" scripts/postinstall.sh`
  - **Commit**: `cli: feat: add native host install to DEB/RPM postinstall`
  - _Requirements: FR-8_
  - _Design: DEB/RPM Scripts_

- [ ] 2.5 Update DEB/RPM preremove.sh
  - **Do**: Add native host uninstall call near top of `scripts/preremove.sh` (before daemon stop)
  - **Files**: `/Users/divkix/GitHub/warpdl/scripts/preremove.sh`
  - **Done when**: Script contains `warpdl native-host uninstall --browser all` with `|| true`
  - **Verify**: `grep -q "native-host uninstall" scripts/preremove.sh`
  - **Commit**: `cli: feat: add native host uninstall to DEB/RPM preremove`
  - _Requirements: FR-9_
  - _Design: DEB/RPM Scripts_

## Phase 3: Testing

- [ ] 3.1 Unit tests for defaults.go
  - **Do**: Create `internal/nativehost/defaults_test.go` with tests for `HasOfficialExtensions()` - test with empty IDs, test table-driven approach
  - **Files**: `/Users/divkix/GitHub/warpdl/internal/nativehost/defaults_test.go`
  - **Done when**: Tests cover both true/false return cases
  - **Verify**: `go test -v ./internal/nativehost/ -run TestHasOfficialExtensions`
  - **Commit**: `core: test: add unit tests for default extension IDs`
  - _Requirements: AC-1.1_

- [ ] 3.2 Integration tests for install --auto
  - **Do**: Add test cases to `cmd/nativehost/cmd_test.go` for:
    1. `--auto` with no defaults (should succeed silently)
    2. `--auto` with defaults set (should install)
    3. Explicit flags override defaults
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/nativehost/cmd_test.go`
  - **Done when**: Tests pass and cover auto flag behavior
  - **Verify**: `go test -v ./cmd/nativehost/ -run TestInstallAuto`
  - **Commit**: `cli: test: add integration tests for native-host install --auto`
  - _Requirements: AC-1.3, AC-1.4_

## Phase 4: Quality Gates

- [ ] 4.1 Local quality check
  - **Do**: Run full test suite, coverage check, lint
  - **Verify**: `go test -race -short ./... && go vet ./... && go build .`
  - **Done when**: All commands pass
  - **Commit**: `chore: fix any lint/test issues` (if needed)

- [ ] 4.2 Create PR and verify CI
  - **Do**: Push branch, create PR with summary referencing #129
  - **Verify**: `gh pr checks --watch` all green
  - **Done when**: PR created, CI passes
  - **PR Title**: `feat: auto-install native messaging host via package manager hooks`
  - **PR Body**: Reference issue #129, list changes to each package manager

## Notes

- **POC shortcuts taken**: No tests in Phase 1, single package manager hook only
- **Production TODOs**:
  - Update extension IDs when browser extensions published
  - Consider environment variable override for CI testing
- **Blocked**: Real extension IDs require browser extension publication
- **Platform notes**:
  - macOS: Homebrew hook tested
  - Windows: Scoop uses PowerShell
  - Linux: DEB/RPM use POSIX sh
