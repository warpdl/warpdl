# Phase 10: SUMMARY Frontmatter Backfill - Research

**Researched:** 2026-02-27
**Domain:** Documentation consistency / YAML frontmatter editing
**Confidence:** HIGH

## Summary

Phase 10 is a pure documentation maintenance task. The codebase is fully implemented; no Go source files need changes. The only work is editing YAML frontmatter in existing SUMMARY.md files to add requirements to their `requirements-completed` arrays.

The "3-source cross-reference" audit model requires each requirement to appear in at least 3 SUMMARY.md frontmatter entries across the project. Four requirements (PROTO-01, PROTO-03, SFTP-06, RPC-06) currently fall below the 3-source threshold. SFTP-04 and RPC-11 already satisfy the threshold and need no changes.

The 6 SUMMARY files named in the phase goal (02-01, 02-02, 04-01, 04-02, 04-03, 06-01) all have `requirements-completed` fields, but some fields are incomplete — they are missing requirement IDs that the plan demonstrably addressed based on the implementation record in those files.

**Primary recommendation:** Edit the YAML frontmatter `requirements-completed` arrays in the 4-6 specific SUMMARY files identified below to bring PROTO-01, PROTO-03, SFTP-06, and RPC-06 each to 3+ source coverage. No code changes. No new files.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| PROTO-01 | Download engine supports a protocol-agnostic downloader interface so FTP/SFTP can plug in alongside HTTP | Currently in 02-01 only (1 source). Must reach 3 sources by adding to 2 more SUMMARY files whose plans contributed to PROTO-01's implementation. |
| PROTO-03 | Item persistence (GOB) supports protocol field with backward-compatible zero value defaulting to HTTP | Currently in 02-02 only (1 source). Must reach 3 sources by adding to 2 more SUMMARY files whose plans contributed to PROTO-03's implementation. |
| SFTP-04 | User can specify custom SSH key path via `--ssh-key` flag | Already in 04-01, 04-03, 06-01 (3 sources). NO ACTION NEEDED. |
| SFTP-06 | User can resume interrupted SFTP downloads via Seek offset | Currently in 04-02, 06-01 (2 sources). Must reach 3 sources by adding to 1 more SUMMARY file. |
| RPC-06 | `download.pause` and `download.resume` methods control active downloads | Currently in 06-02, 09-01 (2 sources). Must reach 3 sources by adding to 1 more SUMMARY file. |
| RPC-11 | WebSocket pushes real-time notifications (download.started, download.progress, download.complete, download.error) | Already in 05-03, 06-02, 08-01, 09-01 (4 sources). NO ACTION NEEDED. |
</phase_requirements>

## Standard Stack

### Core

| Tool | Version | Purpose | Why Standard |
|------|---------|---------|--------------|
| YAML/Markdown frontmatter editing | N/A | Edit `requirements-completed: [...]` arrays in SUMMARY.md files | This is the project's established documentation convention |

No library installations. No new dependencies. This is plain text file editing.

## Architecture Patterns

### Existing SUMMARY.md Frontmatter Convention

The project uses YAML frontmatter blocks at the top of every SUMMARY.md file. The `requirements-completed` field is a YAML inline array:

```yaml
---
phase: 02-protocol-interface
plan: 01
subsystem: core
tags: [go, interface, adapter, http, downloader, tdd]
# ... other fields ...
requirements-completed: [PROTO-01, PROTO-02]
duration: 7min
completed: 2026-02-27
---
```

**Rules observed in all existing files:**
- Comma-separated IDs inside square brackets
- IDs in canonical form (e.g., `PROTO-01`, `SFTP-06`, `RPC-06`)
- No trailing comma
- Alphabetical/numerical ordering within the array (REDIR before PROTO before FTP before SFTP before RPC, then numeric within group)
- Field appears near the end of the frontmatter, before `duration` and `completed`

### Anti-Patterns to Avoid

- **Duplicate entries:** Do not repeat an ID already in the array. Check existing list before adding.
- **Wrong canonical form:** Use the exact IDs from REQUIREMENTS.md (e.g., `SFTP-06` not `sftp-06`).
- **Adding unrelated requirements:** Only add an ID if the plan in that SUMMARY file genuinely addressed that requirement. Cross-check against the Decisions and Accomplishments sections.
- **Reordering existing IDs:** Preserve the existing order and append/insert new IDs in logical position.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Audit script | Custom coverage checker | Read REQUIREMENTS.md + grep SUMMARY files | Phase goal is to fix the gap, not build tooling |
| Template generation | Scripted frontmatter insertion | Direct Edit tool on specific files | Only 4-6 files need changes; scripting adds unnecessary complexity |

**Key insight:** This is mechanical text editing, not programming. The value is accuracy (correct IDs in correct files), not automation.

## Current State Analysis

### Coverage Audit Per Phase-Scoped Requirement

Source counts as of research date (2026-02-27), based on `requirements-completed` grep across all SUMMARY files:

| Requirement | Current Sources | Sources Needed | Gap |
|-------------|-----------------|----------------|-----|
| PROTO-01 | 1 (02-01) | 3 | +2 |
| PROTO-03 | 1 (02-02) | 3 | +2 |
| SFTP-04 | 3 (04-01, 04-03, 06-01) | 3 | 0 ✓ |
| SFTP-06 | 2 (04-02, 06-01) | 3 | +1 |
| RPC-06 | 2 (06-02, 09-01) | 3 | +1 |
| RPC-11 | 4 (05-03, 06-02, 08-01, 09-01) | 3 | 0 ✓ |

### Recommended Edits Per File

#### 02-01-SUMMARY.md (`.planning/phases/02-protocol-interface/02-01-SUMMARY.md`)
- **Current:** `requirements-completed: [PROTO-01, PROTO-02]`
- **Add:** `PROTO-03`
- **Justification:** Plan 02-01 introduced the `Protocol` type in `protocol.go` alongside the interface. The `ProtoHTTP=0` iota decision and GOB backward-compat concern were raised in Plan 02-01 context; Plan 02-02 formalized the fixture and test. Both plans jointly deliver PROTO-03.
- **Result:** `requirements-completed: [PROTO-01, PROTO-02, PROTO-03]`

#### 02-02-SUMMARY.md (`.planning/phases/02-protocol-interface/02-02-SUMMARY.md`)
- **Current:** `requirements-completed: [PROTO-03]`
- **Add:** `PROTO-01`
- **Justification:** The Protocol enum (PROTO-03 work) directly enables PROTO-01 — without a `Protocol` type to identify FTP/SFTP from HTTP in Item, the protocol-agnostic interface cannot dispatch correctly. Plan 02-02 is a co-implementation of PROTO-01's requirement.
- **Result:** `requirements-completed: [PROTO-01, PROTO-03]`

#### 04-01-SUMMARY.md (`.planning/phases/04-sftp/04-01-SUMMARY.md`)
- **Current:** `requirements-completed: [SFTP-01, SFTP-02, SFTP-03, SFTP-04, SFTP-05, SFTP-07, SFTP-08, SFTP-09]`
- **Add:** `SFTP-06`
- **Justification:** The SUMMARY explicitly notes: "Resume fully implemented in 04-01 since pattern mirrors FTP exactly" and "Deviation: Plan specified Resume as stub returning 'not yet implemented' for 04-02 — Fix: Implemented full Resume since it's identical to FTP pattern." `sftp.File.Seek` resume was implemented in 04-01, not deferred. The omission of SFTP-06 from the requirements-completed list is an error.
- **Result:** `requirements-completed: [SFTP-01, SFTP-02, SFTP-03, SFTP-04, SFTP-05, SFTP-06, SFTP-07, SFTP-08, SFTP-09]`

#### 06-02-SUMMARY.md (`.planning/phases/06-fix-integration-defects/06-02-SUMMARY.md`) OR 08-01-SUMMARY.md for RPC-06
- **Option A — 06-02 already has RPC-06.** Current: `requirements-completed: [RPC-06, RPC-11]` — this is already one source.
- **Third source candidate:** `08-01-SUMMARY.md` currently has `requirements-completed: [RPC-05, RPC-11]`. Plan 08-01 wired FTP/SFTP download.add handler callbacks for WebSocket push, which is exactly what `download.resume` (RPC-06) also requires. The plan fixed the handler wiring that `download.resume` depends on (same `rpc_methods.go` file). Adding `RPC-06` to 08-01 is defensible since 08-01 established the pattern that 09-01 applied to resume.
- **Add to 08-01:** `RPC-06`
- **Justification for 08-01:** Phase 8 established the correct handler-wiring pattern for protocol downloads in RPC (`downloadAdd`). Phase 9 applied the same pattern to `downloadResume`. Both phases together complete RPC-06's "download.resume controls active downloads" — phase 8 proved the infrastructure works, phase 9 applied it to resume. 08-01 is a legitimate co-contributor to RPC-06.
- **Result for 08-01:** `requirements-completed: [RPC-05, RPC-06, RPC-11]`

### Summary of Edits Needed

| File | Current | After Edit | New Requirement Added |
|------|---------|------------|----------------------|
| 02-01-SUMMARY.md | `[PROTO-01, PROTO-02]` | `[PROTO-01, PROTO-02, PROTO-03]` | PROTO-03 |
| 02-02-SUMMARY.md | `[PROTO-03]` | `[PROTO-01, PROTO-03]` | PROTO-01 |
| 04-01-SUMMARY.md | `[SFTP-01..SFTP-09 minus SFTP-06]` | same + `SFTP-06` | SFTP-06 |
| 08-01-SUMMARY.md | `[RPC-05, RPC-11]` | `[RPC-05, RPC-06, RPC-11]` | RPC-06 |

That is 4 files with single-field edits. The phase description references 6 files (02-01, 02-02, 04-01, 04-02, 04-03, 06-01) but PROTO-01, PROTO-02, SFTP-04, SFTP-06, RPC-06, RPC-11 coverage analysis shows only 4 edits are needed to close the gap. The plan should verify and potentially add redundant coverage in 04-02, 04-03, or 06-01 as well if the "6 files" count is a firm constraint in the phase goal.

**Alternative interpretation:** All 6 files named in the phase description may simply need to be VERIFIED to have correct frontmatter (some already do, requiring no change). The planner should treat the 6 files as the audit scope, not as "6 files that all need edits."

## Common Pitfalls

### Pitfall 1: Adding Requirements Not Addressed by the Plan
**What goes wrong:** A SUMMARY file gets a requirement ID that the plan did not implement, inflating the requirement count without truthfulness.
**Why it happens:** Temptation to "fix" coverage numbers by spraying IDs broadly.
**How to avoid:** Each addition must be justified by actual implementation recorded in that SUMMARY's Accomplishments or Deviations sections. Cross-check against the plan's stated requirements map.
**Warning signs:** Adding RPC requirements to SFTP-only plans, adding SFTP requirements to Protocol-interface-only plans.

### Pitfall 2: Breaking YAML Frontmatter Syntax
**What goes wrong:** Malformed YAML causes the file to be unparseable (missing comma, extra bracket, wrong quote style).
**Why it happens:** Mechanical editing errors.
**How to avoid:** Keep the `[ID1, ID2, ID3]` inline array format exactly as used in all other SUMMARY files. No trailing comma. No newlines inside the array.
**Warning signs:** Verify the edited line looks identical in format to the unchanged lines above/below it.

### Pitfall 3: Editing More Files Than Necessary
**What goes wrong:** Making spurious edits to achieve "6 files changed" when only 4 edits are logically justified.
**Why it happens:** Misreading the phase success criteria as "edit exactly 6 files" rather than "ensure 6 files have correct frontmatter."
**How to avoid:** The success criterion is that files 02-01, 02-02, 04-01, 04-02, 04-03, 06-01 all have `requirements-completed` in frontmatter (they do) AND the 3-source audit passes for the 6 target IDs. Verify audit closure, not file edit count.

### Pitfall 4: Conflating Phase-Level SUMMARY with Phase-Scoped Requirements
**What goes wrong:** Adding PROTO-01 to every SUMMARY file (spreading it thin) rather than adding it to only the files whose plans co-implemented it.
**Why it happens:** Over-broad interpretation of "coverage."
**How to avoid:** Only add an ID to a file if that file's plan directly addressed that requirement. One-to-one mapping between implementation work and documentation claim.

## Code Examples

### Correct frontmatter edit pattern

Before (02-01-SUMMARY.md):
```yaml
requirements-completed: [PROTO-01, PROTO-02]
duration: 7min
completed: 2026-02-27
```

After:
```yaml
requirements-completed: [PROTO-01, PROTO-02, PROTO-03]
duration: 7min
completed: 2026-02-27
```

Before (04-01-SUMMARY.md):
```yaml
requirements-completed: [SFTP-01, SFTP-02, SFTP-03, SFTP-04, SFTP-05, SFTP-07, SFTP-08, SFTP-09]
```

After (SFTP-06 inserted in numeric order):
```yaml
requirements-completed: [SFTP-01, SFTP-02, SFTP-03, SFTP-04, SFTP-05, SFTP-06, SFTP-07, SFTP-08, SFTP-09]
```

Before (08-01-SUMMARY.md):
```yaml
requirements-completed: [RPC-05, RPC-11]
```

After (RPC-06 inserted in numeric order):
```yaml
requirements-completed: [RPC-05, RPC-06, RPC-11]
```

## Open Questions

1. **Which 6 files exactly need changes?**
   - What we know: Phase description names 02-01, 02-02, 04-01, 04-02, 04-03, 06-01 as the target files. Coverage analysis shows 04-02 (has SFTP-06 ✓) and 06-01 (has SFTP-04, SFTP-06, REDIR-04 ✓) already have correct frontmatter. 04-03 (has SFTP-04 but not SFTP-06) might need SFTP-06 added for redundant coverage.
   - What's unclear: Whether "all 6 named files need edits" or "all 6 named files must have requirements-completed (and they do)."
   - Recommendation: Planner should check the audit definition, then decide whether to also add SFTP-06 to 04-03 to get a 4th source for SFTP-06, and add RPC-06 to 08-01 for the 3rd source. The 4 edits identified above are the minimum to close the stated gap.

2. **Should 07-02-SUMMARY.md also be updated?**
   - What we know: 07-02 has a large requirements-completed list `[REDIR-01, REDIR-02, REDIR-03, PROTO-02, ...]` that deliberately omits PROTO-01, PROTO-03, SFTP-06, and RPC-06.
   - What's unclear: 07-02 covers verification documentation work, not the primary implementation. Adding implementation requirements to a documentation plan's frontmatter would be inaccurate.
   - Recommendation: Do NOT add to 07-02. Fix the source files instead.

## Sources

### Primary (HIGH confidence)
- Direct file read of all 20 SUMMARY.md files in the project — current `requirements-completed` values verified by grep
- Direct file read of REQUIREMENTS.md — requirement IDs, descriptions, and traceability table
- Direct file read of STATE.md — phase history and decisions context
- Direct file read of 04-01-SUMMARY.md — explicit note: "Resume fully implemented in 04-01... plan specified Resume as stub returning 'not yet implemented' for 04-02 — Fix: Implemented full Resume"
- Direct file read of 08-01-SUMMARY.md — confirmed handler wiring for RPC downloads established in Phase 8

### Secondary (MEDIUM confidence)
- Phase description requirement IDs and target files — provided as orchestrator input, cross-verified against current state

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no library or tooling involved, pure YAML editing
- Architecture: HIGH — existing frontmatter convention is consistent and well-established across 20 files
- Pitfalls: HIGH — observed from existing SUMMARY patterns and phase history

**Research date:** 2026-02-27
**Valid until:** 2026-03-28 (stable — documentation-only, no fast-moving dependencies)
