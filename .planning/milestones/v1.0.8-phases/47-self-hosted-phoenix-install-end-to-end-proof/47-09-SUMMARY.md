---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 09
subsystem: docs
tags: [kubernetes, pvc, kind, examples, medium-sample, storage-class]

# Dependency graph
requires:
  - phase: 47-self-hosted-phoenix-install-end-to-end-proof
    provides: "47-04's live proof run and 47-EVIDENCE Defect #3 / 47-VERIFICATION gap #4 identifying the missing RWO override step"
provides:
  - "Inline README step 3b in the medium sample's apply sequence: delete + recreate tide-projects PVC as ReadWriteOnce, scoped to kind/RWO-only provisioners"
  - "per-namespace-resources.yaml comments (header note + PVC inline comment) now route operators to README step 3b instead of requiring manual file edits"
affects: [examples-medium-sample, kind-e2e-proof]

# Tech tracking
tech-stack:
  added: []
  patterns: ["inline kubectl apply -f - <<EOF heredoc override step in README apply sequences, mirroring hack/scripts/acceptance-v1.sh's small-sample RWO override"]

key-files:
  created: []
  modified:
    - examples/projects/medium/README.md
    - examples/projects/medium/per-namespace-resources.yaml

key-decisions:
  - "Delete+recreate (not kubectl patch) for the PVC override, because accessModes is an immutable field on PersistentVolumeClaim"
  - "New step labeled 3b (not renumbered as 4) to keep the existing 9-step numbering stable for anyone already referencing step numbers elsewhere"
  - "Shipped per-namespace-resources.yaml PVC accessModes left as ReadWriteMany (production default unchanged); the override lives only in the README's optional kind-scoped step"

patterns-established:
  - "kind/RWO-only override steps in example READMEs are explicitly scoped ('ONLY — skip on RWX-capable clusters') and heredoc-shaped to mirror the acceptance script's house pattern, rather than requiring hand-editing the shipped YAML"

requirements-completed: [PROOF-01]

# Metrics
duration: 12min
completed: 2026-07-17
---

# Phase 47 Plan 09: Medium Sample kind/RWO PVC Override Step Summary

**Added an inline, explicitly-scoped README step (3b) that deletes and recreates the medium sample's `tide-projects` PVC as ReadWriteOnce for kind/RWO-only provisioners, closing the gap where 47-04's proof run had to improvise this workaround operationally.**

## Performance

- **Duration:** 12 min
- **Started:** 2026-07-17T18:00:00Z (approx)
- **Completed:** 2026-07-17T18:10:17Z
- **Tasks:** 1
- **Files modified:** 2

## Accomplishments
- `examples/projects/medium/README.md` now has a step 3b, immediately after step 3 (per-namespace-resources.yaml apply), explicitly scoped to "kind / RWO-only provisioners ONLY — skip on RWX-capable clusters", with a WHY note explaining the WaitForFirstConsumer deadlock and PVC accessModes immutability, and an inline `kubectl apply -f - <<EOF` heredoc mirroring `hack/scripts/acceptance-v1.sh`'s small-sample RWO override pattern.
- `examples/projects/medium/per-namespace-resources.yaml`'s kind/minikube header note and the PVC's inline comment both now point operators to README step 3b instead of instructing them to hand-edit the shipped YAML.
- Shipped PVC default (`accessModes: [ReadWriteMany]`) is unchanged — RWX remains correct for RWX-capable clusters; the override is opt-in and scoped to kind/RWO-only.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the inline RWO override step to the medium sample apply sequence** - `a916837` (docs)

**Plan metadata:** (pending — this SUMMARY commit)

## Files Created/Modified
- `examples/projects/medium/README.md` - Inserted step 3b (kind/RWO-only PVC delete+recreate override) between existing steps 3 and 4
- `examples/projects/medium/per-namespace-resources.yaml` - Updated header kind/minikube note and PVC inline comment to reference README step 3b instead of requiring manual edits

## Decisions Made
- Used delete+recreate rather than `kubectl patch` because `accessModes` is an immutable PVC spec field — matches the plan's explicit instruction and the acceptance script's own rationale comment.
- Labeled the new step "3b" rather than renumbering steps 4-9, preserving stable step references elsewhere in the docs.
- Left the shipped YAML's `accessModes: [ReadWriteMany]` untouched per the plan's explicit constraint (production default; RWX-capable clusters still get the correct default out of the box).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Gap #4 (47-VERIFICATION missing field, 47-EVIDENCE Defect #3) is closed: the medium sample's documented apply sequence is now kind-clean first-try, with the exact operational workaround 47-04 had to improvise now codified as scoped step 3b.
- No blockers for downstream phase-47 plans.

---
*Phase: 47-self-hosted-phoenix-install-end-to-end-proof*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: examples/projects/medium/README.md
- FOUND: examples/projects/medium/per-namespace-resources.yaml
- FOUND: .planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/47-09-SUMMARY.md
- FOUND: commit a916837 (Task 1)
- FOUND: commit 9e5fef6 (SUMMARY.md)
