---
spec: issue-136
phase: requirements
created: 2026-01-19
generated: auto
---

# Requirements: Batch URL Download from Input File

## Summary

Enable downloading multiple URLs from an input file, similar to aria2's `-i` option. Integrates with existing queue manager for concurrent download control.

## User Stories

### US-1: Download URLs from File
As a user, I want to download multiple URLs listed in a file so that I can batch download files without typing each URL.

**Acceptance Criteria**:
- AC-1.1: `-i, --input-file` flag accepts file path
- AC-1.2: Each non-empty, non-comment line treated as URL
- AC-1.3: Lines starting with `#` are skipped as comments
- AC-1.4: Empty lines and whitespace-only lines are skipped
- AC-1.5: All URLs queued for download (respects `--max-concurrent`)

### US-2: Mixed Input Sources
As a user, I want to combine input file with direct URLs so that I can add extra files to my batch.

**Acceptance Criteria**:
- AC-2.1: `warpdl download -i urls.txt https://extra.com/file.zip` works
- AC-2.2: Direct URL(s) added to queue along with file URLs
- AC-2.3: Order: file URLs first, then direct URL args

### US-3: Batch Download Summary
As a user, I want to see a summary of batch results so that I know which downloads succeeded or failed.

**Acceptance Criteria**:
- AC-3.1: Summary printed after all downloads complete
- AC-3.2: Format: "Batch complete: N succeeded, M failed"
- AC-3.3: Failed URLs listed with error reason

### US-4: Graceful Error Handling
As a user, I want batch downloads to continue on individual failures so that one bad URL doesn't abort the entire batch.

**Acceptance Criteria**:
- AC-4.1: Invalid URLs logged but batch continues
- AC-4.2: Download errors logged but batch continues
- AC-4.3: File read errors abort batch with clear error

### US-5: Apply Options to All URLs
As a user, I want to apply download options to all URLs in batch so that I can set speed limits or headers once.

**Acceptance Criteria**:
- AC-5.1: `--speed-limit`, `--max-connection`, etc. apply to all
- AC-5.2: `--download-path` applies to all (unless per-file override in P1)
- AC-5.3: `--background` mode works for entire batch

## Functional Requirements

| ID | Requirement | Priority | Source |
|----|-------------|----------|--------|
| FR-1 | Parse input file with one URL per line | Must | US-1 |
| FR-2 | Skip comment lines (starting with `#`) | Must | US-1 |
| FR-3 | Skip empty and whitespace-only lines | Must | US-1 |
| FR-4 | Combine input file URLs with direct URL args | Must | US-2 |
| FR-5 | Print success/failure summary at end | Must | US-3 |
| FR-6 | Continue batch on individual download failures | Must | US-4 |
| FR-7 | Apply global options to all URLs | Must | US-5 |
| FR-8 | Support `-` for stdin input | Should | P1 scope |
| FR-9 | Extended format with per-file options | Should | P1 scope |

## Non-Functional Requirements

| ID | Requirement | Category |
|----|-------------|----------|
| NFR-1 | Input file parsing < 100ms for 1000 URLs | Performance |
| NFR-2 | Memory usage O(n) where n = URL count | Performance |
| NFR-3 | Clear error messages for malformed input | Usability |
| NFR-4 | 80%+ test coverage for new code | Quality |

## Out of Scope (P1/Future)

- Extended format with per-file options (`out=`, `dir=`, `header=`)
- Stdin input (`-i -`)
- JSON input format (`--input-format json`)
- Progress bar for batch overall (individual progress exists)
- Retry failed downloads from batch

## Dependencies

- Queue Manager (issue #142) - already merged
- Existing download command infrastructure
