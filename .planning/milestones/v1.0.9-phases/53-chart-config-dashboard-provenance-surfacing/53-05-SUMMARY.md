---
phase: 53-chart-config-dashboard-provenance-surfacing
plan: 05
subsystem: infra
tags: [helm, chart, verify-tier, loop-policy, install-vs-upgrade, kind]

# Dependency graph
requires:
  - phase: 53-01
    provides: "images.tideLanggraphVerifier chart block + TIDE_VERIFIER_IMAGE env injection"
  - phase: 53-02
    provides: "ParseVerifyLevelDefaults fail-closed parser + VerifyDefaults on both Deps + --verify-levels-json flag + TIDE_VERIFIER_MODEL env read, all wired in cmd/manager/main.go"
provides:
  - "hack/helm/tide-values.yaml subagent.verify block (image/model scalars, per-level enabled/maxIterations/onExhaustion map, D-03 fresh-install posture)"
  - "hack/helm/verify-posture-configmap.yaml — sticky install-time posture marker (D-05 lookup + resource-policy: keep idiom)"
  - "charts/tide/ regenerated: conditional TIDE_VERIFIER_MODEL env + conditional --verify-levels-json arg computing the D-05 posture"
  - "D-06 render-pair contract test proving install-ON / upgrade-OFF + both explicit-override directions, no cluster"
affects: [53-06-dispatch-gates, 53-09-live-sticky-proof, release-preflight]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Go-template comment blocks ({{-/* ... */-}}) for chart-template prose, not YAML # comments, when the comment sits ahead of a conditional guard — plain # comments render unconditionally and can leak literal strings into every helm template invocation"

key-files:
  created:
    - hack/helm/verify-posture-configmap.yaml
    - charts/tide/templates/verify-posture-configmap.yaml
  modified:
    - hack/helm/tide-values.yaml
    - hack/helm/augment-tide-chart.sh
    - charts/tide/values.yaml
    - charts/tide/templates/deployment.yaml
    - test/integration/kind/verify_chart_config_test.go

key-decisions:
  - "verify-posture-configmap.yaml's explanatory prose uses a Go-template comment block ({{-/* ... */-}}), not YAML # comments — a plain # comment ahead of the {{- if }} guard renders unconditionally on every helm template invocation regardless of guard outcome, which leaked the literal --verify-levels-json flag name into disabled/upgrade renders and inflated grep counts during self-review"
  - "the posture-marker ConfigMap's creation guard is NOT conditioned on subagent.verify.posture — an install with posture=disabled still records enabled lineage, since the explicit values override always outranks the marker (D-05); only matters if the operator later returns posture to auto"
  - "the args-region posture computation checks the marker's actual data.posture value (eq $verifyMarker.data.posture \"enabled\", short-circuited via Go template's and) rather than mere marker existence, so a hand-edited marker (T-53-10, accepted risk) is honored correctly if an operator ever edits it to something other than enabled"

patterns-established: []

requirements-completed: [CFG-01, CFG-02]

# Metrics
duration: ~25min
completed: 2026-07-21
---

# Phase 53 Plan 05: Verify-Tier Chart Config Surface + Sticky Posture Marker + D-06 Render-Pair Test Summary

**`subagent.verify` chart block (image/model scalars + per-level LoopPolicy defaults) flowing through a conditional `--verify-levels-json` manager arg gated on a signing-secret-style sticky install-time posture marker, proven by a 4-subtest no-cluster helm-template contract test.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-21T04:35:00Z (approx, first file read)
- **Completed:** 2026-07-21T04:59:57Z
- **Tasks:** 2 completed
- **Files modified:** 7 (3 hack/helm/ sources incl. 1 new, 3 regenerated charts/tide/ outputs incl. 1 new, 1 test file)

## Accomplishments
- `hack/helm/tide-values.yaml` gained a `subagent.verify` block (sibling of `subagent.defaults`/`subagent.levels`, per D-01): `model`/`posture` scalars plus a per-level `levels` map with the D-03 fresh-install posture (task/milestone/project `enabled: true`, plan/phase `enabled: false`; phase/milestone/project deliberately omit `maxIterations` since `ResolveLoopPolicy` clamps them to 0 regardless)
- `hack/helm/verify-posture-configmap.yaml` (NEW) mirrors `signing-secret.yaml`'s `lookup` + if-not + `resource-policy: keep` idiom verbatim (Secret → ConfigMap) — the sticky install-time posture marker that makes CFG-02's install-ON/upgrade-OFF contract survive every subsequent `helm upgrade`
- `hack/helm/augment-tide-chart.sh` gained two new marker-anchored blocks: a conditional `TIDE_VERIFIER_MODEL` env (`phase53-verify-model-env-injected`, anchored after the 53-01 verifier-image block) and the posture-computed conditional `--verify-levels-json` arg (`phase53-verify-args-injected`, anchored after the pricing-overrides-json conditional) — the latter inlines the full D-05 decision (explicit `enabled`/`disabled` override > marker's recorded `data.posture` > `.Release.IsInstall`) since `deployment.yaml` is generated and no hand-placed `_helpers.tpl` change is warranted
- `make helm-controller` regenerated `charts/tide/values.yaml` and `charts/tide/templates/deployment.yaml`, plus a new `charts/tide/templates/verify-posture-configmap.yaml` — committed alongside the `hack/helm/` sources in the same commit (Pitfall 1); a second regen run produces a byte-identical tree (idempotent) and `make verify-chart-reproducible` passes clean
- A new `TestHelmDeploymentTemplateVerifyPostureInstallVsUpgrade` (4 subtests, real `helm template` invocations, no cluster) pins: plain install renders both `--verify-levels-json` and the `tide-verify-posture` marker; `--is-upgrade` renders neither; `--is-upgrade --set subagent.verify.posture=enabled` forces the arg back ON; `--set subagent.verify.posture=disabled` forces it OFF even on a plain install. `TestHelmDeploymentTemplateRendersVerifierModelEnv` statically guards the new model-env augment block.

## Task Commits

1. **Task 1: subagent.verify values block + posture-marker ConfigMap + model env + conditional levels arg, regenerated** - `6c21ae80` (feat)
2. **Task 2: D-06 render-pair contract test (install vs --is-upgrade vs explicit overrides)** - `05f80e45` (test)

## Files Created/Modified
- `hack/helm/tide-values.yaml` - Added `subagent.verify` block (model/posture scalars + per-level enabled/maxIterations/onExhaustion map) with a resolution-chain doc comment matching the existing subagent-comment style
- `hack/helm/verify-posture-configmap.yaml` (NEW) - Sticky install-time posture marker template, mirrors `signing-secret.yaml`'s lookup+keep idiom; explanatory prose lives in a Go-template comment block (not YAML `#` comments) so it never leaks into rendered output
- `hack/helm/augment-tide-chart.sh` - `cp` line for the new template; `ENVMODEL53` conditional-env block; `ARGS53` conditional-arg block computing the full D-05 posture decision inline
- `charts/tide/values.yaml`, `charts/tide/templates/deployment.yaml`, `charts/tide/templates/verify-posture-configmap.yaml` - Regenerated outputs
- `test/integration/kind/verify_chart_config_test.go` - Added `TestHelmDeploymentTemplateVerifyPostureInstallVsUpgrade` (4 subtests) and `TestHelmDeploymentTemplateRendersVerifierModelEnv`

## Decisions Made
- Converted the posture-marker ConfigMap's explanatory comments from YAML `#` prose to a Go-template comment block (`{{-/* ... */-}}`, the `NOTES.txt` precedent) after self-review caught the literal flag name `--verify-levels-json` leaking into every render's output regardless of the `{{- if }}` guard — plain `#` comments ahead of a template action are not template syntax and render unconditionally.
- The posture computation in the args block checks the marker's actual `data.posture` value (`eq $verifyMarker.data.posture "enabled"`, guarded by Go template's short-circuiting `and` so a missing marker never dereferences `.data` on an empty map) rather than mere marker existence — this makes the accepted T-53-10 threat (a hand-edited marker) behave correctly if an operator ever sets `data.posture` to something other than `"enabled"`.
- Kept the marker-creation guard unconditioned on `subagent.verify.posture` (an install with `posture=disabled` still records `enabled` lineage) per the plan's explicit instruction — the values override always outranks the marker, so the recorded value only matters if the operator later returns to `auto`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed leaking `#` prose comments contaminating every `helm template` render**
- **Found during:** Task 1, self-review of the render-pair assertions before committing
- **Issue:** The first draft of `verify-posture-configmap.yaml` used plain YAML `#` comments (mirroring most of the codebase's chart-comment style) ahead of the `{{- if and (not $marker) .Release.IsInstall }}` guard. Because these lines are template text, not template *actions*, Helm renders them unconditionally on every invocation — one comment sentence happened to contain the literal substring `--verify-levels-json`, which inflated `grep -c "verify-levels-json"` counts on every render (2 instead of 1 on install; 1 instead of the expected 0 on `--set subagent.verify.posture=disabled`).
- **Fix:** Rewrote the header comment as a single Go-template comment block (`{{- /* ... */ -}}`), the exact idiom `hack/helm/augment-tide-chart.sh`'s `NOTES.txt` heredoc already uses, and rephrased the prose to avoid the literal flag/name strings as an additional defense-in-depth measure.
- **Files modified:** `hack/helm/verify-posture-configmap.yaml`
- **Verification:** Re-ran `make helm-controller` and the full render-pair assertion set; all 8 manual grep checks plus both new go-tests now report the expected counts (install: `verify-levels-json` count 1, `tide-verify-posture` count 1; `--is-upgrade`: both counts 0; override cases: 1/0 as expected).
- **Committed in:** `6c21ae80` (Task 1 commit — caught and fixed before the first commit, so only the corrected version landed)

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** The fix was caught during self-review before any commit landed a broken version; no scope creep, no follow-up needed.

## Issues Encountered
None beyond the auto-fixed comment-leak above.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- CFG-01/CFG-02 chart surface is complete and self-consistent: `subagent.verify` values, the sticky posture marker, the conditional `TIDE_VERIFIER_MODEL` env, and the conditional `--verify-levels-json` arg all regenerate cleanly and idempotently from `hack/helm/`.
- `make helm-controller && git diff --exit-code charts/ hack/`, `make verify-chart-reproducible`, and `helm lint charts/tide` all pass clean on the current worktree HEAD.
- Plan 53-06 (the D-04 AND-gates at the three verifier dispatch chokepoints) can consume `VerifyDefaults` populated by this chart surface without re-touching any file this plan modified.
- Plan 53-09 (live kind sticky-posture proof) has its render-pair half (D-06) fully closed by this plan's `TestHelmDeploymentTemplateVerifyPostureInstallVsUpgrade`; only the live `helm install` → `helm upgrade` cluster-level proof remains.
- No blockers for subsequent Phase 53 plans in this wave or later waves.

## Self-Check: PASSED

Verified all created/modified files exist on disk and both commit hashes are present in git history (see below).

---
*Phase: 53-chart-config-dashboard-provenance-surfacing*
*Completed: 2026-07-21*
