# Specification Quality Audit: Download Scheduling & Browser Cookie Import

**Purpose**: Comprehensive requirements quality validation for PR review — covers completeness, clarity, consistency, security/privacy, cross-platform parity, and edge case coverage across both scheduling and cookie import features
**Created**: 2026-02-28
**Feature**: [spec.md](../spec.md) | [plan.md](../plan.md) | [data-model.md](../data-model.md) | [contracts/cli-commands.md](../contracts/cli-commands.md)
**Audience**: PR reviewer (review gate before implementation)
**Depth**: Standard
**Focus**: Full spec audit, Security & privacy, Cross-platform, Edge cases & error handling

## Requirement Completeness

- [ ] CHK001 - Are requirements defined for what happens when the daemon is not running at submission time (CLI sends `--start-at` but daemon is down)? [Gap]
- [ ] CHK002 - Is the behavior specified when a recurring download (`--schedule`) completes but the next occurrence's URL returns a different file (e.g., filename collision)? [Completeness, Gap]
- [ ] CHK003 - Are requirements defined for displaying cookie source metadata in `warpdl list` output (which browser, path)? [Completeness, Gap]
- [ ] CHK004 - Is the maximum number of concurrent recurring schedules specified or intentionally unbounded? [Completeness, Gap]
- [ ] CHK005 - Are requirements defined for what happens when a recurring download fails — does the next cron occurrence still trigger? [Completeness, Gap]
- [ ] CHK006 - Is the behavior specified when `warpdl stop` targets a recurring schedule mid-download (download active, next occurrence pending)? Does it cancel only the current run or all future runs? [Completeness, Spec §FR-008]
- [ ] CHK007 - Are requirements defined for how `--cookies-from auto` resolves when multiple Firefox profiles exist? [Completeness, Spec §FR-016]
- [ ] CHK008 - Is the cookie re-import behavior specified for all trigger scenarios: resume, retry, recurring, and work-steal segment respawn? [Completeness, Spec §FR-023]
- [ ] CHK009 - Are requirements defined for the speed limit scheduling format (`09:00-17:00:512KB`) — is the syntax formally specified with a grammar or just shown by example? [Completeness, Spec §User Story 7]
- [ ] CHK010 - Is the interaction between `--schedule` combined with `--start-at` or `--start-in` specified in the spec (not just in contracts)? [Completeness, Gap — only in cli-commands.md]
- [ ] CHK011 - Are requirements defined for notifying the user when a missed schedule fires on daemon restart (e.g., message format, where it appears)? [Completeness, Spec §FR-006]
- [ ] CHK012 - Is the behavior specified when the user provides `--cookies-from` with a directory path instead of a file path? [Completeness, Gap]

## Requirement Clarity

- [ ] CHK013 - Is "within 60 seconds of the scheduled time" (SC-001) clarified — does this mean ±60s or strictly 0-60s late? [Clarity, Spec §SC-001]
- [ ] CHK014 - Is "clear error message" (SC-007) defined with specific formatting or structural requirements, or is it left to implementer discretion? [Clarity, Spec §SC-007]
- [ ] CHK015 - Is "documented priority order" (FR-016) specified — documented where? In CLI help text, man page, README, or just the spec? [Clarity, Spec §FR-016]
- [ ] CHK016 - Is "relevant cookies for that domain" (User Story 3, acceptance scenario 1) defined with precision — does "relevant" mean exact domain, subdomains, parent domain, or path-matched? [Clarity — FR-017 partially answers but "including subdomains" is vague on direction]
- [ ] CHK017 - Is "prompt for confirmation" (FR-007, past-time `--start-at`) specified for non-interactive contexts (piped input, scripts, daemon API calls)? [Clarity, Spec §FR-007]
- [ ] CHK018 - Is the `--start-in` duration format exhaustively defined — are units like `d` (days), `s` (seconds), or compound forms like `1d2h30m` supported? [Clarity, Spec §FR-002]
- [ ] CHK019 - Is "first valid cookie store found" (FR-016) defined — does "valid" mean file exists, file is readable, file has correct format, or file contains cookies for the target domain? [Clarity, Spec §FR-016]
- [ ] CHK020 - Is the `list` output format for missed downloads specified precisely — does "was 2026-02-27 03:00 (starting now)" come from requirements or is it an implementation example? [Clarity — only in contracts, not spec]

## Requirement Consistency

- [ ] CHK021 - Does FR-007 ("warn and prompt or start immediately") conflict with the acceptance scenario 4 ("warns the user... and prompts for confirmation or starts immediately")? The spec says "prompt OR start" but contracts say "starting download immediately" — which is canonical? [Consistency, Spec §FR-007 vs contracts/cli-commands.md]
- [ ] CHK022 - Is the ScheduleState enum consistent between spec (§Key Entities: 5 states) and data-model.md (5 states)? Do all state names match exactly? [Consistency, Spec §Key Entities vs data-model.md]
- [ ] CHK023 - Are the auto-detection browser priority orders consistent between spec (User Story 5: "Firefox, Chrome, Chromium, Edge, LibreWolf") and research.md (Firefox, LibreWolf, Chrome, Chromium, Edge, Brave)? LibreWolf and Brave ordering differs. [Consistency, Spec §User Story 5 vs research.md §4]
- [ ] CHK024 - Does the spec's "no timezone suffix" (FR-001) align with the contracts' `YYYY-MM-DD HH:MM` format — is the format string consistent across spec, contracts, and error messages? [Consistency, Spec §FR-001 vs contracts]
- [ ] CHK025 - Is the cookie domain matching rule consistent between FR-017 ("matching the download URL's domain including subdomains") and research.md's SQL queries (exact match, dot-prefix, and LIKE wildcard)? [Consistency, Spec §FR-017 vs research.md §3]
- [ ] CHK026 - Is the `warpdl stop` behavior for recurring schedules consistent between contracts ("cancels ALL future occurrences") and the spec (FR-008 only says "cancel a scheduled download before it starts")? [Consistency, Spec §FR-008 vs contracts/cli-commands.md]

## Acceptance Criteria Quality

- [ ] CHK027 - Can SC-001 ("within 60 seconds") be objectively measured in automated tests without flakiness from CI timing jitter? [Measurability, Spec §SC-001]
- [ ] CHK028 - Can SC-002 ("100% of persisted schedules restored") be verified without exhaustive enumeration — is the expected set of schedule states defined? [Measurability, Spec §SC-002]
- [ ] CHK029 - Can SC-004 ("auto-detection works correctly on both macOS and Linux") be verified in CI — are macOS and Linux both CI targets? [Measurability, Spec §SC-004]
- [ ] CHK030 - Is SC-005 ("no more than 2 seconds for 10,000 cookies") measurable with a specific test methodology — cold start vs warm, what hardware baseline? [Measurability, Spec §SC-005]
- [ ] CHK031 - Are acceptance scenarios for User Story 6 (recurring) sufficient — is there a scenario for cron expression that fires during daemon downtime? [Acceptance Criteria, Spec §User Story 6]
- [ ] CHK032 - Are acceptance scenarios for User Story 7 (speed limits) sufficient — is there a scenario for overlapping time ranges or invalid format? [Acceptance Criteria, Spec §User Story 7]

## Security & Privacy Coverage

- [ ] CHK033 - Is FR-020 ("MUST NOT log or display actual cookie values") specific enough — does "display" include debug mode, error messages containing cookie headers, or crash dumps? [Clarity, Spec §FR-020]
- [ ] CHK034 - Are requirements defined for sanitizing cookie values from HTTP request/response logs that may exist elsewhere in the codebase (e.g., debug mode logging in `pkg/logger/`)? [Coverage, Gap]
- [ ] CHK035 - Is FR-023 ("cookies held in-memory only") specific enough — are requirements defined for ensuring cookie values are not captured in core dumps, swap files, or Go heap profiles? [Clarity, Spec §FR-023]
- [ ] CHK036 - Are requirements defined for what happens when the cookie source path points to a file outside the user's home directory (e.g., `/etc/passwd`, symlink attacks)? [Security, Gap]
- [ ] CHK037 - Is the temp file cleanup requirement specified — when the SQLite copy is made to temp, are requirements defined for secure deletion (especially on failure paths)? [Security, Gap]
- [ ] CHK038 - Are requirements defined for preventing the `CookieSourcePath` from being exposed in `warpdl list` output to other users on shared systems? [Security, Gap]
- [ ] CHK039 - Is the scope of "names and domains only for debugging" (FR-020) defined — does this apply to all log levels or only debug mode? [Clarity, Spec §FR-020]
- [ ] CHK040 - Are requirements defined for what happens when a Netscape cookie file contains cookies for domains the user did not intend to share (i.e., overly broad import from a full export)? Is there a confirmation or dry-run mode? [Security, Gap]

## Cross-Platform Coverage

- [ ] CHK041 - Are Windows browser cookie paths specified in the same detail as macOS and Linux? Research.md covers macOS/Linux but Windows paths are absent. [Completeness, Gap — research.md §4]
- [ ] CHK042 - Are requirements defined for Windows support of `--cookies-from auto` auto-detection? The spec mentions platform-specific paths (FR-022) but Windows is only implied. [Coverage, Spec §FR-022]
- [ ] CHK043 - Is the Firefox Snap path (`~/snap/firefox/common/.mozilla/`) included in auto-detection requirements or only in research? [Coverage, Gap — only in research.md §4]
- [ ] CHK044 - Is the Chrome `Network/Cookies` migration path (v96+) documented as a requirement, or is it only an implementation detail in research? [Coverage, Gap — only in research.md §4]
- [ ] CHK045 - Are requirements defined for the Brave browser in auto-detection? Research lists it but spec's User Story 5 and FR-016 do not mention it. [Consistency, Spec §FR-016 vs research.md §4]
- [ ] CHK046 - Is the Windows named pipe behavior specified for scheduled download notifications? The spec doesn't address how scheduled-download status updates reach Windows CLI clients. [Coverage, Gap]
- [ ] CHK047 - Are requirements defined for how `--start-at` behaves across DST transitions on Windows (where Go's time zone handling differs from Unix)? [Coverage, Gap]
- [ ] CHK048 - Is the Chromium-based browser cookie path resolution specified for Flatpak/AppImage installations on Linux? [Coverage, Gap]

## Edge Case & Error Handling Coverage

- [ ] CHK049 - Is the behavior specified when the cookie SQLite file exists but is zero bytes or truncated? [Edge Case, Gap]
- [ ] CHK050 - Are requirements defined for handling the WAL file being present but the main SQLite file missing or corrupted? [Edge Case, Gap]
- [ ] CHK051 - Is the behavior specified when `--start-in 0s` or `--start-in 0m` is provided (zero delay)? [Edge Case, Gap]
- [ ] CHK052 - Is the behavior specified when a cron expression resolves to "never fires" (e.g., February 30th: `0 0 30 2 *`)? [Edge Case, Gap]
- [ ] CHK053 - Are requirements defined for handling disk full conditions during the temp-file copy of a cookie database? [Edge Case, Gap]
- [ ] CHK054 - Is the behavior specified when the download URL changes between cron occurrences (e.g., URL now returns 301 redirect)? [Edge Case, Gap]
- [ ] CHK055 - Is the behavior specified when `--cookies-from auto` succeeds but the found cookie store is for an older browser version with a different schema? [Edge Case, Gap]
- [ ] CHK056 - Are requirements defined for the maximum length of a cron expression or `--start-at` value to prevent abuse? [Edge Case, Gap]
- [ ] CHK057 - Is the behavior specified when the daemon receives a `--start-at` time that is valid but represents an ambiguous local time (e.g., during fall-back DST when 1:30 AM occurs twice)? [Edge Case, Spec §Edge Cases — mentions DST but not ambiguous times]
- [ ] CHK058 - Is the behavior specified when `--cookies-from` points to a cookie file that is a symlink to another file? [Edge Case, Gap]
- [ ] CHK059 - Are requirements defined for handling concurrent `warpdl download --schedule` commands that create overlapping recurring schedules for the same URL? [Edge Case, Gap]
- [ ] CHK060 - Is the behavior specified when a Netscape cookie file uses CRLF line endings vs LF? [Edge Case, Gap]

## Dependencies & Assumptions

- [ ] CHK061 - Is the assumption that "GOB zero-initializes missing fields" validated against the actual Go GOB specification — are all new field types zero-safe? [Assumption, data-model.md]
- [ ] CHK062 - Is the assumption that "Firefox cookies are stored unencrypted" validated for all Firefox versions currently in the wild (ESR, Developer Edition, Nightly)? [Assumption, Spec §Assumptions]
- [ ] CHK063 - Is the dependency on queue manager (#135) explicitly bounded — what specific queue interface does the scheduler depend on, and is that interface stable? [Dependency, Spec §Assumptions]
- [ ] CHK064 - Is the assumption that `modernc.org/sqlite` handles WAL mode correctly validated with a specific test case or reference? [Assumption, research.md §1]
- [ ] CHK065 - Is the bandwidth throttling capability (required by User Story 7) documented as a prerequisite — does it exist today or is it assumed to be built? [Dependency, Spec §Assumptions]
- [ ] CHK066 - Is the assumption that `adhocore/gronx` correctly handles DST transitions documented and validated? [Assumption, research.md §2]

## Ambiguities & Conflicts

- [ ] CHK067 - The spec says Chrome encrypted cookies are "out of scope" but FR-014 says "MUST support Chrome cookie stores when unencrypted." Is it specified what percentage of real-world Chrome installations have unencrypted cookies, and is the feature practically useful? [Ambiguity, Spec §FR-014 + Assumptions]
- [ ] CHK068 - The spec defines `ScheduleState` as an enum with 5 values but does not specify transitions from "missed" — can "missed" transition to "cancelled"? [Ambiguity, data-model.md state machine]
- [ ] CHK069 - FR-024 says "persist the cookie source path" and FR-023 says "MUST NOT persist cookie values." Is it specified whether the cookie source path counts as sensitive data (it reveals the user's browser choice and profile path)? [Conflict, Spec §FR-023 vs §FR-024]
- [ ] CHK070 - The spec mentions `--cookies-from auto` uses "first valid cookie store found" but does not specify whether to prefer unencrypted stores (Firefox) over encrypted ones (Chrome) — is the priority order a requirement or an implementation detail? [Ambiguity, Spec §FR-016]

## Notes

- Existing `requirements.md` checklist was a lightweight pre-planning gate. This checklist audits the full artifact set (spec + plan + data-model + contracts + research) at PR-review depth.
- Items tagged `[Gap]` indicate requirements that likely need to be added to spec.md before implementation.
- Items tagged `[Consistency]` indicate discrepancies between artifacts that need reconciliation.
- Cross-platform items heavily weighted toward Windows gaps since macOS/Linux paths are well-documented in research.md but Windows is underspecified.
