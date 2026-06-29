---
phase: 33-d4-planner-failure-semantics
plan: "02"
subsystem: helm-chart
tags: [docs, comment-only, sizing-policy, plannerConcurrency]
dependency_graph:
  requires: []
  provides: [PLANFAIL-01-doc]
  affects: [charts/tide, hack/helm]
tech_stack:
  added: []
  patterns: [canonical-source-pattern (hack/helm/ → charts/tide/)]
key_files:
  created: []
  modified:
    - hack/helm/tide-values.yaml
    - charts/tide/values.yaml
decisions:
  - "Edit hack/helm/tide-values.yaml (the canonical source), not charts/tide/values.yaml directly — the pre-commit chart-reproducibility hook runs 'make helm' which overwrites charts/tide/values.yaml from hack/helm/"
metrics:
  duration: ~5 minutes
  completed_date: "2026-06-29"
  tasks_completed: 1
  tasks_total: 1
  files_modified: 2
---

# Phase 33 Plan 02: Soften plannerConcurrency Sizing-Policy Comment Summary

Softened the `plannerConcurrency` sizing-policy comment in the Helm values files from an overstated hard correctness requirement to a per-workload tuning note documenting the single-node throughput-for-safety tradeoff (D-04 carried-in debt from Phase 32 review).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Soften plannerConcurrency comment (D-04) | a8a567b | hack/helm/tide-values.yaml, charts/tide/values.yaml |

## What Changed

Removed: "Must be sized at least as wide as the widest expected planning wave (e.g. a 6-phase milestone needs plannerConcurrency >= 6 to avoid serialising phase dispatch). Increase for multi-node clusters where memory constraints are relaxed."

Added: "The single-node default intentionally trades throughput for safety: sizing the cap below a milestone's widest planning wave serialises that wave's dispatch (degraded throughput, not a deadlock — single-shot planner Jobs drain and the next dispatches). Tune upward for multi-node clusters where memory constraints are relaxed."

The `plannerConcurrency: 4` default value is unchanged (D-04 locks the value at 4; raising it is deferred to multi-node infrastructure).

## Deviations from Plan

### Process Deviation

**[Rule 3 - Blocking] Canonical source is hack/helm/tide-values.yaml, not charts/tide/values.yaml directly**
- **Found during:** Task 1 — first commit attempt
- **Issue:** The pre-commit `chart-reproducibility` hook runs `make helm` which copies `hack/helm/tide-values.yaml` → `charts/tide/values.yaml`, reverting any direct edit to the generated file
- **Fix:** Edited `hack/helm/tide-values.yaml` (the canonical hand-maintained source), then ran `make helm` to regenerate `charts/tide/values.yaml` to match; staged both files
- **Files modified:** hack/helm/tide-values.yaml, charts/tide/values.yaml
- **Commit:** a8a567b

## Verification Results

- `grep -q '^plannerConcurrency: 4$' charts/tide/values.yaml` — PASSED (value unchanged)
- `! grep -q 'needs plannerConcurrency >= 6' charts/tide/values.yaml` — PASSED (overstated requirement removed)
- `helm template ./charts/tide` — PASSED (YAML valid)
- `git diff` shows only comment-line changes within the plannerConcurrency block — CONFIRMED
- Pre-commit `chart-reproducibility` hook — PASSED

## Known Stubs

None.

## Threat Flags

None — comment-only edit to a FIXED chart contract. No new code, no value change, no new input/output surface.

## Self-Check: PASSED

- hack/helm/tide-values.yaml: exists and contains softened comment
- charts/tide/values.yaml: regenerated and matches canonical source
- Commit a8a567b: verified via `git rev-parse --short HEAD`
