---
phase: 05-distribution-self-hosting-acceptance
plan: 10
subsystem: docs
tags: [docs, troubleshooting, table, dist-04, operator-incident-reference]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: CRD subchart split (D-E1), per-Kind ClusterRoles; cited in row 7 (CRDs not registered) + row 13 (lockstep upgrade)
  - phase: 02-dispatch-plan-validation-innermost-reconcilers-harness
    provides: Budget cap infra (D-D2) + admission webhook 422 surface (D-E1..E4); cited in rows 8 (admission 422) + 9 (BudgetExceeded)
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: per-run branch + push-lease (D-B6); chaos-resume + leader-election (D-D1); cited in rows 3 (PushLeaseFailed) + 12 (leader election lost)
  - phase: 04-gates-observability-dashboard-cli
    provides: Dashboard install + port-forward (D-D2); gate annotations (D-G3); metric cardinality bound (D-O2); cited in rows 6 (dashboard 404) + 10 (gate awaiting approval) + 4 (gitleaks observability)
provides:
  - docs/troubleshooting.md — single Markdown Symptom/Cause/Recipe table covering 13 canonical operator failure modes (DIST-04 deliverable)
  - Cross-links to existing v1 docs (rwx-drivers, dashboard, gates, observability, git-hosts, live-e2e) + Phase 5 sibling docs (INSTALL.md, rbac.md, README.md)
  - Finalizer-stuck recipe explicit per ROADMAP Phase 5 SC #3 mandate
affects: [DIST-04, docs-readiness, operator-onboarding, post-v1-runbooks (per-incident runbook hooks)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single-table-as-doc Markdown format (analog of docs/rwx-drivers.md from Phase 04.1)"
    - "Symptom-driven row ordering (operator scans down the Symptom column to find their failure mode)"
    - "Recipe column = one runnable command OR one Markdown link to a deeper doc (no prose; no runbooks)"
    - "Pipe characters inside cells escaped as backslash-pipe per GFM table grammar"

key-files:
  created:
    - docs/troubleshooting.md
  modified: []

key-decisions:
  - "Honored D-C4 lock: single Markdown table, 13 rows (≥ 12 D-C4 mandate + Research Pitfall 2 lockstep-upgrade row)"
  - "Ordered rows by install-time → first-apply → steady-state likelihood (finalizer first per ROADMAP P5 mandate)"
  - "Used Audience/Status/Scope opener pattern matching docs/live-e2e.md style (the operator-doc canon in this repo)"
  - "Cross-linked to Phase 5 sibling docs (INSTALL.md, rbac.md, README.md) by name even though those files don't exist yet — sibling plans 05-04/05-06/05-09/etc. land them in this phase per D-C3 doc index"
  - "Generic 'leader election' lowercase term used in the cause cell (acceptance-test grep is case-sensitive lowercase)"
  - "Forward-link anchor '#install-order-pitfall-4--crds-first' picked for INSTALL.md per the PLAN's <action> spec; sibling Plan 05-04 (or wherever INSTALL.md lands) owns that anchor's existence"

patterns-established:
  - "Pattern: troubleshooting docs as scan-tables, not paragraph blocks (D-C4 lock; supersedes the paragraph-block style at docs/live-e2e.md lines 108-132)"
  - "Pattern: Audience/Status/Scope opener for operator docs (3 bold lines + bulleted Scope-of-this-doc list)"

requirements-completed: [DIST-04]

# Metrics
duration: ~8min
completed: 2026-05-21
---

# Phase 5 Plan 10: Troubleshooting Table Summary

**Single-page operator troubleshooting reference at `docs/troubleshooting.md` — 13-row Symptom/Cause/Recipe table covering install, first-apply, and steady-state failure modes (DIST-04).**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-05-21
- **Completed:** 2026-05-21
- **Tasks:** 1
- **Files modified:** 1

## Accomplishments

- Authored `docs/troubleshooting.md` (44 lines, 13 data rows + 1 header + 1 separator = 15 table lines).
- All 12 D-C4-mandated entries present + 13th row from RESEARCH Pitfall 2 (lockstep CRD chart upgrade).
- Finalizer-stuck row leads the table with the `kubectl patch ... finalizers=[]` recipe (ROADMAP Phase 5 SC #3 mandate satisfied).
- 15 Markdown cross-links across 10 unique targets (rwx-drivers.md, dashboard.md, gates.md, observability.md, git-hosts.md, live-e2e.md, INSTALL.md (+ anchor variant), rbac.md, README.md).
- Each Recipe column entry is a runnable command OR a cross-link — no prose, no runbooks (per D-C4 single-page-lookup design).

## Task Commits

Each task was committed atomically:

1. **Task 1: Author docs/troubleshooting.md** — `56c7f95` (docs)

## Files Created/Modified

- `docs/troubleshooting.md` — 13-row Symptom/Cause/Recipe table (Audience/Status/Scope opener; "See also" footer with 8 cross-links; 44 lines total).

## Decisions Made

- **Table-as-doc format** (D-C4 lock). Single Markdown table beats paragraph blocks for ops-in-incident scannability. The exact analog is `docs/rwx-drivers.md` (the Phase 04.1 P13 single-table doc). Paragraph-block alternatives (e.g., `docs/live-e2e.md` lines 108-132) are intentionally superseded for this surface.
- **Row ordering by install-time → first-apply → steady-state.** Finalizer stuck leads (per ROADMAP P5 mandate); ANTHROPIC_API_KEY 401 + PushLeaseFailed + gitleaks blocked + RWX missing + dashboard 404 + CRDs not registered cluster as install-time issues; admission webhook 422 + BudgetExceeded + gate-awaiting-approval are first-apply; ImagePullBackoff + leader election lost are steady-state.
- **Audience/Status/Scope opener.** Matches the `docs/live-e2e.md` operator-doc pattern (the doc canon in this repo) — three bold lines + bulleted Scope-of-this-doc list. Gives ops in incident immediate orientation.
- **Forward-link to Phase 5 sibling docs even though they don't exist yet.** `INSTALL.md` (Plan 05-04 or sibling) + `rbac.md` (Plan 05-09 or sibling) + `README.md` (Plan 05-03 docs index) are linked by name. The sibling plans land those files in the same phase per D-C3; this troubleshooting doc references them per the PLAN's `<action>` spec.
- **Generic `leader election` lowercase phrasing.** Acceptance-test grep was case-sensitive (`grep -q "leader election"`); had to reflow the row to use the lowercase term in the cause cell, not the symptom cell.

## Deviations from Plan

None — plan executed exactly as written. One minor iteration was needed mid-task: the first draft had "Leader election" capitalized in the cause cell, which failed the case-sensitive `grep -q "leader election"` acceptance check. Reworded the cause cell to use lowercase "leader election lock" alongside the capitalized "Leader election lease conflict" sentence-start. Same commit, no rework.

## Issues Encountered

- **Case-sensitive acceptance grep on `leader election`.** Initial draft capitalized "Leader election lease conflict" at the start of the cause cell. The acceptance criterion's grep was lowercase, so re-flowed the cell to include a lowercase use mid-sentence ("the leader election lock under `coordination.k8s.io/Lease` is stuck"). Resolved by edit pre-commit; no separate commit needed.

## User Setup Required

None — docs-only plan; no env vars, no Secrets, no deployment.

## Next Phase Readiness

- DIST-04 troubleshooting piece complete. Other DIST-04 sibling docs (INSTALL.md, rbac.md, project-authoring.md) land in their own plans per D-C3 doc index ordering.
- Forward-link anchor `INSTALL.md#install-order-pitfall-4--crds-first` is pending — sibling plan that lands INSTALL.md must include that section/anchor for the link to resolve. Flag this for the INSTALL.md plan's `verify` block.
- No blockers to subsequent Phase 5 plans.

## Self-Check: PASSED

- `docs/troubleshooting.md` exists at expected path: FOUND
- Commit `56c7f95` exists in git log: FOUND
- All acceptance checks from `<acceptance_criteria>` block return 0
- Row count: 15 (header + separator + 13 data rows ≥ 12 D-C4 entries)
- ROADMAP mandate verified: `grep -q "kubectl patch.*finalizers"` returns 0
- Cross-link pattern `\]\(` matches 15 occurrences (≥ 1 per frontmatter `key_links.pattern`)

---

*Phase: 05-distribution-self-hosting-acceptance*
*Completed: 2026-05-21*
