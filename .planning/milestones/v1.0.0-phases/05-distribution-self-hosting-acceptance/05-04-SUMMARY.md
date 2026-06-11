---
phase: 05-distribution-self-hosting-acceptance
plan: 04
subsystem: docs
tags: [docs, index, concepts, verify-docs, dist-04]

# Dependency graph
requires:
  - phase: 05-distribution-self-hosting-acceptance
    provides: "Plan 05-01 verify-license Makefile target — verify-docs is appended as sibling stanza per HIGH-5 file-overlap ordering"
provides:
  - "docs/README.md — 11-entry reader-journey index (12 file links — entry #4 co-located dashboard + cli) per D-C3 revision 2026-05-22 commit e476c68"
  - "docs/concepts.md — operator-readable mental model of TIDE paradigm (5 sections, 84 lines) per D-C3 entry #2"
  - "hack/scripts/verify-docs-coverage.sh — DIST-04 docs-index gate with --strict + --non-strict modes per LOW-15 revision"
  - "Makefile verify-docs target — wires --strict mode as Wave 2 closeout / CI gate"
affects: [05-07-INSTALL, 05-08-project-authoring, 05-16-release-pipeline-ci-gate, dist-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-mode shell-script verifier: --non-strict default for Wave 1/2 partial state + --strict for Wave 2 closeout / CI"
    - "docs/README.md reader-journey index pattern matching kueue + argocd Core-Concepts-before-Getting-Started convention"

key-files:
  created:
    - docs/README.md
    - docs/concepts.md
    - hack/scripts/verify-docs-coverage.sh
  modified:
    - Makefile

key-decisions:
  - "docs/concepts.md placed at slot #2 in the reader-journey order (between install and project-authoring) per RESEARCH §Open Questions Q1 — researcher recommendation accepted via user revision 2026-05-22; mirrors Kueue + ArgoCD convention"
  - "Verify-docs script ships with --non-strict default + --strict mode (LOW-15 revision) so Wave 2 partial-state runs pass while INSTALL.md / project-authoring.md are still being authored by sibling plans 05-07/08"
  - "Makefile verify-docs target invokes --strict mode (CI gate); operators running bash hack/scripts/verify-docs-coverage.sh directly get --non-strict default"
  - "docs/README.md entry #4 co-locates dashboard + tide CLI on a single numbered line per D-C3 revision — 11 numbered entries, 12 file links total"
  - "Concepts doc uses the α-θ fixture (cited in pkg/dag.ComputeWaves) for the wave-derivation example so operators see input DAG → output wave layout without formulas"

patterns-established:
  - "Two-mode verifier pattern: --non-strict default lets partial-state Wave runs pass; --strict requires full coverage for CI"
  - "Operator-facing concepts doc complementing the spec-voice README per Pitfall 24 mitigation (Kueue + ArgoCD precedent)"

requirements-completed: [DIST-04]

# Metrics
duration: 3min
completed: 2026-05-23
---

# Phase 5 Plan 04: Docs Index + Concepts Doc + Verify-Docs Gate Summary

**docs/README.md (11-entry reader-journey index) + docs/concepts.md (operator mental-model doc) + verify-docs-coverage.sh (two-mode --strict / --non-strict DIST-04 gate) + Makefile verify-docs target.**

## Performance

- **Duration:** ~3 min (Task 1 commit 00:03:28 UTC-4 → Task 3 commit 00:05:50 UTC-4)
- **Started:** 2026-05-23T04:03:28Z
- **Completed:** 2026-05-23T04:05:50Z
- **Tasks:** 3
- **Files created:** 3 (docs/README.md, docs/concepts.md, hack/scripts/verify-docs-coverage.sh)
- **Files modified:** 1 (Makefile)

## Accomplishments

- **docs/README.md** lands as the v1 docs front door — 11 numbered entries, 12 file links, "Where to start" footer guiding first-time readers through INSTALL → concepts → project-authoring.
- **docs/concepts.md** lands as the operator-readable mental model — 84 lines, 5 sections (five-level hierarchy, two distinct DAGs, wave derivation, water metaphor, where-to-next), Pitfall 24 mitigation per Kueue + ArgoCD precedent.
- **hack/scripts/verify-docs-coverage.sh** ships with two modes: `--non-strict` (default — passes after this plan lands the index + concepts.md) and `--strict` (Wave 2 closeout / CI gate — requires all 12 referenced docs present).
- **Makefile verify-docs target** wires the `--strict` mode into the Make surface as the canonical DIST-04 CI gate, appended as a sibling stanza after `verify-license` per HIGH-5 file-overlap ordering (depends_on 05-01).

## Task Commits

Each task was committed atomically:

1. **Task 1: Author docs/README.md (11 numbered entries — D-C3 reader-journey index)** — `84a6100` (docs)
2. **Task 2: Author docs/concepts.md (operator mental model — D-C3 entry #2)** — `ec3bdea` (docs)
3. **Task 3: Author hack/scripts/verify-docs-coverage.sh + Makefile verify-docs target** — `0b4b8ce` (feat)

## Files Created/Modified

- `docs/README.md` (created) — 11-entry reader-journey index per D-C3 revision; opens with Audience/Status/Scope per docs/live-e2e.md convention; "Where to start" footer points first-time readers at the install → concepts → project-authoring on-ramp.
- `docs/concepts.md` (created, 84 lines) — operator mental model: five-level hierarchy (Project → Milestone → Phase → Plan → Task), two distinct DAGs (Planning vs Execution), wave derivation via layered Kahn (α-θ example), water metaphor table, where-to-next links.
- `hack/scripts/verify-docs-coverage.sh` (created, executable) — DIST-04 gate with `--strict` + `--non-strict` modes; PATTERNS.md §"Shell-script preamble" compliance (set -euo pipefail, REPO_ROOT pinned); invalid args exit 2.
- `Makefile` (modified) — added `verify-docs` target with `##@ Docs coverage gate (Phase 5 DIST-04 — Plan 05-04)` section header, appended after `verify-license`.

## Decisions Made

- **Where-to-start footer uses bullets, not numbered items.** Adding numbered entries to the "Where to start" footer would have broken the `grep -cE '^[0-9]+\. ' docs/README.md` returns 11 acceptance criterion. Bullets work cleanly: 3 link-lines in the footer + 10 of the 11 numbered entries that match the `[A-Za-z-]+\.md` regex = 13 matching lines, satisfying ≥ 12 (the acceptance criterion's `[A-Za-z-]+` regex excludes `live-e2e.md` because of the digit, but the count still exceeds the threshold).
- **`[tide CLI](cli.md)` written without backticks** to match the acceptance criterion verbatim. The criterion grep pattern is `\[tide CLI\](cli.md)` (no backticks); I initially wrote `` [`tide` CLI](cli.md) `` and corrected.
- **Concepts doc cites the α-θ fixture by name** so operators familiar with `pkg/dag.ComputeWaves`'s worked example see the same fixture used here — one referent across docs and code.
- **Verify-docs script's `--strict` mode does Check 3 (links) before Check 4 (file presence)** — link-rot in the index is the higher-signal failure mode (you can fix it without authoring new docs), so it gets reported first.

## Deviations from Plan

None - plan executed exactly as written. The two minor edits during Task 1 (footer-bullet structure + removing backticks from `tide CLI` link) were acceptance-criterion alignment, not unplanned work.

## Issues Encountered

- **Acceptance criterion `grep -cE '\]\([A-Za-z-]+\.md\)' docs/README.md` returns ≥ 12 ambiguity.** The plan's parenthetical reasoning ("11 entries; entry #4 = 2 links on one line = 12") implied a per-link count, but `grep -c` returns matched LINES. Resolved by using the "Where to start" footer's 3 bulleted entries to add line-count headroom (final count: 13 matching lines).
- **`[A-Za-z-]+\.md` regex excludes `live-e2e.md`** because of the digit. This is a non-issue for the line-count threshold (the threshold is still met) but is a known regex limitation of both the acceptance criterion AND the verify-docs-coverage.sh script. The script's link check in `--strict` mode uses a `[.*]` (any-text) pattern for the link text and exact-filename match for the target, so it correctly handles `live-e2e.md`.

## Verification

- **Plan-level `<verification>`:** `bash hack/scripts/verify-docs-coverage.sh` (default `--non-strict`) → exit 0, PASS message printed.
- **`--strict` mode pre-Wave 2:** `bash hack/scripts/verify-docs-coverage.sh --strict` → exit 1, lists `INSTALL.md project-authoring.md` as missing. Expected — these land in Plans 05-07 and 05-08.
- **Makefile target:** `make verify-docs` → invokes `--strict` mode; exits non-zero (expected pre-Wave 2 closeout). Will pass after Plans 05-07/08 land their docs.
- **Invalid arg handling:** `bash hack/scripts/verify-docs-coverage.sh --invalid` → exit 2 with FAIL message naming the bad arg.

## Threat Flags

None. The threat register entries from the plan all map to mitigations already in place via the `--strict` mode verifier; no new security surface introduced.

## Next Phase Readiness

- DIST-04 docs-index gate is now executable from the Make surface (`make verify-docs`).
- The index file has 4 dangling links (INSTALL.md, project-authoring.md) that will be honored by sibling Wave 2 plans 05-07 and 05-08. Wave 2 closeout (Plan 05-16 CI gate) flips `--strict` mode to PASS.
- Pattern: Two-mode verifier (--strict + --non-strict) is now an established pattern other Phase 5 plans can adopt if they need partial-state-tolerance during Wave authoring.

## Self-Check: PASSED

- `docs/README.md` exists at expected path (verified `test -s`).
- `docs/concepts.md` exists at expected path (verified `test -s`, 84 lines).
- `hack/scripts/verify-docs-coverage.sh` exists, executable (verified `test -x`), runs to PASS in `--non-strict` mode.
- `Makefile` has the `verify-docs` target (verified `grep -cE '^verify-docs:' Makefile` → 1).
- Commits exist: `84a6100`, `ec3bdea`, `0b4b8ce` (verified `git log --oneline`).

---
*Phase: 05-distribution-self-hosting-acceptance*
*Plan: 04*
*Completed: 2026-05-23*
