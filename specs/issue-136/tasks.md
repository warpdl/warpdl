---
spec: issue-136
phase: tasks
total_tasks: 17
created: 2026-01-19
generated: auto
---

# Tasks: Batch URL Download from Input File

## Phase 1: Make It Work (POC)

Focus: End-to-end batch download working. Skip tests initially, hardcode acceptable.

- [x] 1.1 Write failing test for input file parser
  - **Do**: Create `cmd/input_file_test.go` with test for `ParseInputFile()`
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/input_file_test.go`
  - **Test cases**:
    - Parse file with 3 URLs
    - Skip comment lines
    - Skip empty lines
    - Trim whitespace
  - **Done when**: Test fails with "undefined: ParseInputFile"
  - **Verify**: `go test -run TestParseInputFile ./cmd/... 2>&1 | grep "undefined"`
  - **Commit**: `cli: test: add input file parser tests (red)`
  - _Requirements: FR-1, FR-2, FR-3_
  - _Design: Component A_

- [x] 1.2 Implement input file parser to pass tests
  - **Do**: Create `cmd/input_file.go` with `ParseInputFile()` function
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/input_file.go`
  - **Implementation**:
    - Read file with `os.ReadFile()`
    - Split by newlines
    - For each line: trim, skip if empty/comment, collect URL
    - Return `ParseResult` struct
  - **Done when**: Parser tests pass
  - **Verify**: `go test -run TestParseInputFile ./cmd/...`
  - **Commit**: `cli: feat: implement input file parser (green)`
  - _Requirements: FR-1, FR-2, FR-3_
  - _Design: Component A_

- [x] 1.3 Write failing test for -i flag registration
  - **Do**: Add test in `cmd/cmd_test.go` verifying `-i` flag exists on download command
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/cmd_test.go`
  - **Test**: Check `download` command has `input-file` flag
  - **Done when**: Test fails (flag not found)
  - **Verify**: `go test -run TestDownloadInputFileFlag ./cmd/... 2>&1 | grep "FAIL"`
  - **Commit**: `cli: test: add input-file flag test (red)`
  - _Requirements: AC-1.1_

- [x] 1.4 Add -i flag to download command
  - **Do**: Add `cli.StringFlag` for `input-file, i` to `dlFlags` in `cmd/download.go`
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/download.go`
  - **Done when**: Flag test passes, `warpdl download --help` shows `-i` flag
  - **Verify**: `go test -run TestDownloadInputFileFlag ./cmd/... && go build . && ./warpdl download --help | grep input-file`
  - **Commit**: `cli: feat: add -i/--input-file flag to download command`
  - _Requirements: AC-1.1_

- [x] 1.5 Write failing test for batch download function
  - **Do**: Create `cmd/download_batch_test.go` with test for batch download logic
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/download_batch_test.go`
  - **Test cases**:
    - Download 2 URLs from file
    - Mix file URLs with direct URL arg
    - Handle download error (continue batch)
  - **Done when**: Test fails (function undefined)
  - **Verify**: `go test -run TestDownloadBatch ./cmd/... 2>&1 | grep "undefined"`
  - **Commit**: `cli: test: add batch download tests (red)`
  - _Requirements: FR-4, FR-6_
  - _Design: Component B_

- [x] 1.6 Implement batch download logic
  - **Do**: Add `downloadFromInputFile()` function in `cmd/download.go`
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/download.go`
  - **Implementation**:
    - Parse input file
    - Collect direct URL args
    - For each URL: call `client.Download()`
    - Track success/failure counts
    - Print summary
  - **Done when**: Batch download tests pass
  - **Verify**: `go test -run TestDownloadBatch ./cmd/...`
  - **Commit**: `cli: feat: implement batch download from input file (green)`
  - _Requirements: FR-4, FR-5, FR-6_
  - _Design: Component B_

- [x] 1.7 Integrate batch logic into download command
  - **Do**: Modify `download()` function to check `-i` flag and route to batch logic
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/download.go`
  - **Done when**: `warpdl download -i urls.txt` downloads all URLs
  - **Verify**: Create test file, run `warpdl download -i /tmp/test-urls.txt` with daemon running
  - **Commit**: `cli: feat: integrate input file into download command`
  - _Requirements: US-1_

- [x] 1.8 POC Checkpoint - Manual E2E Test
  - **Do**: Create test input file, verify batch download works end-to-end
  - **Test scenario**:
    1. Create `/tmp/urls.txt` with 3 URLs (mix valid/invalid)
    2. Start daemon: `warpdl daemon`
    3. Run: `warpdl download -i /tmp/urls.txt`
    4. Verify: Downloads queued, summary printed
  - **Done when**: Batch download works, shows summary
  - **Verify**: Manual test completes successfully
  - **Commit**: `cli: feat: complete batch download POC`
  - _Requirements: US-1, US-3_

## Phase 2: Refactoring

After POC validated, clean up code.

- [x] 2.1 Extract result tracking to separate struct
  - **Do**: Create `BatchResult` and `BatchError` types, extract tracking logic
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/input_file.go`
  - **Done when**: Code follows single responsibility, types documented
  - **Verify**: `go build ./cmd/...`
  - **Commit**: `cli: refactor: extract batch result types`
  - _Design: Component C_

- [x] 2.2 Add comprehensive error handling
  - **Do**: Add error types for file not found, permission denied, empty file
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/input_file.go`
  - **Error messages**: Clear, actionable
  - **Done when**: All error paths have proper messages
  - **Verify**: `go build ./cmd/...`
  - **Commit**: `cli: refactor: add comprehensive error handling for input file`
  - _Design: Error Handling_

- [ ] 2.3 Add input validation
  - **Do**: Validate URLs have scheme (http/https), log warnings for suspicious lines
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/input_file.go`
  - **Done when**: Invalid URLs logged with line numbers
  - **Verify**: `go test -run TestParseInputFile ./cmd/...`
  - **Commit**: `cli: refactor: add URL validation in input file parser`
  - _Requirements: NFR-3_

## Phase 3: Testing

- [ ] 3.1 Add edge case tests for parser
  - **Do**: Add tests for: empty file, only comments, Unicode, Windows line endings
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/input_file_test.go`
  - **Done when**: All edge cases covered
  - **Verify**: `go test -v -run TestParseInputFile ./cmd/...`
  - **Commit**: `cli: test: add edge case tests for input file parser`
  - _Requirements: NFR-4_

- [ ] 3.2 Add integration tests for batch download
  - **Do**: Add tests using mock client to verify batch flow
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/download_batch_test.go`
  - **Test scenarios**:
    - All succeed
    - Some fail
    - All fail
    - Mix with direct URL args
  - **Done when**: Integration tests pass
  - **Verify**: `go test -v -run TestDownloadBatch ./cmd/...`
  - **Commit**: `cli: test: add batch download integration tests`
  - _Requirements: NFR-4_

- [ ] 3.3 Verify test coverage
  - **Do**: Run coverage report, ensure 80%+ for new files
  - **Files**: N/A
  - **Done when**: Coverage >= 80% for `input_file.go`
  - **Verify**: `go test -coverprofile=cover.out ./cmd/... && go tool cover -func=cover.out | grep input_file`
  - **Commit**: N/A (no commit, just verification)
  - _Requirements: NFR-4_

## Phase 4: Quality Gates

- [ ] 4.1 Local quality check
  - **Do**: Run all quality checks locally
  - **Commands**:
    - `go fmt ./...`
    - `go vet ./...`
    - `go test -race -short ./...`
    - `go build .`
  - **Done when**: All commands pass
  - **Verify**: Exit codes all 0
  - **Commit**: `cli: fix: address lint/type issues` (if needed)

- [ ] 4.2 Update help text and documentation
  - **Do**: Update download command description, add examples
  - **Files**: `/Users/divkix/GitHub/warpdl/cmd/templ.go`
  - **Add**: Example showing `-i` usage in help
  - **Done when**: `warpdl download --help` shows input file usage
  - **Verify**: `./warpdl download --help`
  - **Commit**: `docs: update download command help with input file examples`

- [ ] 4.3 Create PR and verify CI
  - **Do**: Push branch, create PR with gh CLI
  - **PR title**: "feat(cli): add batch URL download from input file (issue #136)"
  - **Verify**: `gh pr checks --watch` all green
  - **Done when**: PR ready for review
  - **Commit**: N/A

## Notes

- **POC shortcuts taken**:
  - Minimal URL validation (let daemon validate)
  - No stdin support yet (P1)
  - No per-file options yet (P1)

- **Production TODOs for Phase 2**:
  - Proper error types
  - URL validation with line numbers
  - Warning messages for skipped lines

- **TDD Approach**:
  - Each implementation task has corresponding test task first
  - Tests written to fail initially (red)
  - Implementation makes tests pass (green)
  - Refactor in Phase 2
