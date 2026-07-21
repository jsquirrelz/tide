---
phase: 53-chart-config-dashboard-provenance-surfacing
plan: 01
subsystem: infra
tags: [helm, chart, kind, ci-gate, chart-reproducibility]

# Dependency graph
requires:
  - phase: 51-the-task-loop
    provides: "TaskReconciler.VerifierImage field + cmd/manager/main.go TIDE_VERIFIER_IMAGE envOrDefault read"
provides:
  - "images.tideLanggraphVerifier {repository,tag,pullPolicy} chart values block"
  - "TIDE_VERIFIER_IMAGE env injection on the manager Deployment (regenerated deployment.yaml)"
  - "static chart-contract test guarding the phase53-verifier-image-env-injected augment block"
  - "kind harness resolves the verifier image purely through helm --set, zero post-install patches"
affects: [53-05, dashboard-provenance-surfacing, release-preflight]

# Tech tracking
tech-stack:
  added: []
  patterns: ["hack/helm/ source -> make helm-controller regeneration -> committed charts/tide/ output, same commit"]

key-files:
  created:
    - test/integration/kind/verify_chart_config_test.go
  modified:
    - hack/helm/tide-values.yaml
    - hack/helm/augment-tide-chart.sh
    - charts/tide/values.yaml
    - charts/tide/templates/deployment.yaml
    - test/integration/kind/suite_test.go

key-decisions:
  - "phase53-verifier-image-env-injected marker anchored immediately after phase47-otlp-headers-env-injected (the most recent marker), byte-mirroring the ENV9_BLOCK/idempotent-guard idiom exactly"
  - "images.tideLanggraphVerifier.tag=test / pullPolicy=IfNotPresent --set pair inserted next to the tideImport pair in helmControllerArgs, mirroring the tideReporter/tideImport precedent"
  - "removed the Phase-51 kubectl-set-env post-install patch entirely rather than leaving it as a redundant fallback -- the new static test exists specifically to catch a future chart regression this workaround would mask"

patterns-established: []

requirements-completed: [CFG-01]

# Metrics
duration: ~12min
completed: 2026-07-21
---

# Phase 53 Plan 01: Chart wiring for TIDE_VERIFIER_IMAGE Summary

**Closed the TIDE_VERIFIER_IMAGE chart-wiring gap: `images.tideLanggraphVerifier` now flows from `hack/helm/tide-values.yaml` through a regenerated `charts/tide/` into the manager's env, guarded by a new static go-test and a retired kind-suite workaround.**

## Performance

- **Duration:** ~12 min
- **Tasks:** 2
- **Files modified:** 6 (2 hack/helm/ sources, 2 regenerated charts/tide/ outputs, 1 new test file, 1 modified kind suite file)

## Accomplishments
- `hack/helm/tide-values.yaml` gained an `images.tideLanggraphVerifier` block (repository/tag/pullPolicy), sibling to `tideReporter`/`tideImport`
- `hack/helm/augment-tide-chart.sh` gained the `phase53-verifier-image-env-injected` marker block, anchored after `phase47-otlp-headers-env-injected`, emitting `TIDE_VERIFIER_IMAGE` from the new values block
- `make helm-controller` regenerated `charts/tide/values.yaml` and `charts/tide/templates/deployment.yaml` — committed alongside the `hack/helm/` sources in the same commit (Pitfall 1)
- A new plain go-test, `TestHelmDeploymentTemplateRendersVerifierEnv`, statically asserts the deployment template renders the verifier env — guards against a dropped augment block surviving a future `helmify` regen
- The kind harness now pins `images.tideLanggraphVerifier.tag=test` via helm `--set` (mirroring `tideReporter`/`tideImport`) and the Phase-51 post-install `kubectl set env TIDE_VERIFIER_IMAGE=...` patch is retired — zero post-install env patches remain for the verifier image

## Task Commits

1. **Task 1: Author images.tideLanggraphVerifier + TIDE_VERIFIER_IMAGE env in hack/helm/ and regenerate the chart** - `7e421473` (feat)
2. **Task 2: Static chart-contract test + kind-suite image pin + retire the kubectl set env workaround** - `b66a6f0b` (test)

_Note: SUMMARY.md commit follows this document (worktree mode — orchestrator merges shared-artifact updates after all wave agents complete)._

## Files Created/Modified
- `hack/helm/tide-values.yaml` - Added `images.tideLanggraphVerifier` block (repository/tag/pullPolicy)
- `hack/helm/augment-tide-chart.sh` - Added `phase53-verifier-image-env-injected` marker block (ENV53), anchored after the phase47 marker
- `charts/tide/values.yaml` - Regenerated output carrying the new `tideLanggraphVerifier` block
- `charts/tide/templates/deployment.yaml` - Regenerated output carrying the `TIDE_VERIFIER_IMAGE` env entry
- `test/integration/kind/verify_chart_config_test.go` - New `TestHelmDeploymentTemplateRendersVerifierEnv` static chart-contract test
- `test/integration/kind/suite_test.go` - Added `images.tideLanggraphVerifier.tag=test`/`pullPolicy=IfNotPresent` to `helmControllerArgs`; removed the Phase-51 `kubectl set env TIDE_VERIFIER_IMAGE` post-install workaround block

## Decisions Made
- Anchored the new augment marker after `phase47-otlp-headers-env-injected` (the most recent marker in the file), per RESEARCH Finding 2's recommendation — keeps the marker-chain append-only and avoids reordering existing anchors.
- Inserted the new `--set` pair adjacent to the `tideImport` pair in `helmControllerArgs` rather than at the end of the slice, keeping the reporter/import/verifier image-pin trio visually grouped.
- Fully removed the kubectl-set-env workaround (rather than leaving it commented out or as a fallback) — its continued presence would silently mask a future regression in the chart wiring that the new static test exists to catch.

## Deviations from Plan

None - plan executed exactly as written. One self-correction during execution: the first draft of the retirement comment on the new `--set` pair literally contained the phrase "kubectl set env" (in backticks), which tripped the acceptance grep `kubectl.*set.*env|TIDE_VERIFIER_IMAGE=ghcr` intended to confirm the workaround was gone. Reworded the comment to describe the removed behavior without using the matched phrase; verified with a re-run of the grep (count 0) before committing. Not a deviation from plan intent — comment wording, no logic change.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- CFG-01 requirement satisfied: `images.tideLanggraphVerifier` chart values, `TIDE_VERIFIER_IMAGE` env injection, and reproducibility are all locked and verified (`make helm-controller && git diff --exit-code charts/`, `make verify-chart-reproducible`, and the new static go-test all pass clean on the current worktree HEAD).
- Plan 53-05 (subagent.verify values + model env + posture marker) can build on this chart-tier foundation without re-touching the `images.tideLanggraphVerifier` block or the ENV53 augment marker.
- No blockers for subsequent Phase 53 plans in this wave or later waves.

---
*Phase: 53-chart-config-dashboard-provenance-surfacing*
*Completed: 2026-07-21*
