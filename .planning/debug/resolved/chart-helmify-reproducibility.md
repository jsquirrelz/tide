---
slug: chart-helmify-reproducibility
status: resolved
trigger: "Chart reproducibility CI gate red — `make helm` regeneration drifts from committed charts/tide/ tree"
created: 2026-06-14
updated: 2026-06-14
---

# Debug Session: chart-helmify-reproducibility

## Symptoms

- **Expected:** A fresh `make helm` regeneration produces a `charts/` tree byte-identical to what is committed — `git diff --quiet charts/` exits 0. The CI "Verify chart tree is reproducible" step passes.
- **Actual:** Regeneration drifts. CI job `Helm chart pair lint (D-E1 / REQ-DIST-01)` and the `release` workflow's `Helmify chart reproducibility (D-X2 verify-only)` both fail at "Verify chart tree is reproducible" with `FAIL: charts/ tree drifted from a fresh make helm regeneration` (2 files, deployment.yaml + values.yaml).
- **Errors / drift detail:** Regeneration REVERTS Phase 13/14/16 chart features:
  - RE-ADDS the old `--subagent-image={{ .Values.images.stubSubagent.repository }}:...` startup flag that Phase 13 D-01 deliberately removed.
  - DROPS the Phase 13 D-01 `CLAUDE_SUBAGENT_IMAGE` resolution-chain comment block + the `{{- $img := required "subagent.defaults.image must be a non-empty image ref..." .Values.subagent.defaults.image }}` guard (replaced by a plain `value: "{{ .Values.images.claudeSubagent... }}"`).
  - DROPS the Phase 14 `--budget-reserve-per-dispatch-cents={{ .Values.budget.reservePerDispatchCents | default 100 }}` flag and the `{{- if .Values.pricing.overrides }} - --pricing-overrides-json=... {{- end }}` conditional block in `charts/tide/templates/deployment.yaml`.
  - DROPS the Phase 14 `pricing.overrides` comment block in `charts/tide/values.yaml`.
- **Timeline:** Red on `ci` since at least 2026-06-11 (commit d9e8249). First surfaced on the `release` workflow when the `v1.0.1` tag was pushed (release workflow only runs on tags; prior tag was v1.0.0). NOT caused by the v1.0.1 milestone-close commits (those touched only `.planning/`).
- **Reproduction:** `make helm && git diff --stat charts/` shows the drift. CI runs this as a verify-only gate.

## Root Cause (pre-localized via direct observation)

The chart pipeline is: `kustomize build config/default | helmify charts/tide` then `hack/helm/augment-tide-chart.sh` re-applies project-specific augmentations (helmify wipes them on every run). **`hack/helm/augment-tide-chart.sh` (last modified Jun 8) was never updated when Phases 13/14/16 hand-edited the rendered `charts/tide/` (Jun 11–12).** So regeneration reverts those features. The augment script — and possibly its sibling override source `hack/helm/tide-values.yaml` — must be brought back in sync with the committed chart.

Commits that edited the chart without updating the augment generator: `edb4361` (13-06), `4fb43fd` (14-05), `8ce99f8` (16-08).

## Fix Direction

Update `hack/helm/augment-tide-chart.sh` (the generator, NOT the chart contract) so `make helm` reproduces the committed `charts/tide/` with zero git diff. **`charts/tide/values.yaml` and the committed chart are the FIXED contract / source of truth per CLAUDE.md — the augment script catches up to the chart, never the reverse.** Do NOT regenerate-and-commit the chart (that would delete the Phase 13/14 features). Verify with `make helm` then `git diff --quiet charts/` (exit 0).

## Current Focus

- status: RESOLVED — fix verified locally (zero chart drift, idempotent) by orchestrator; committed.
- next_action: human confirms CI "Verify chart tree is reproducible" passes on the fix commit, OR reports remaining drift.

## Resolution

root_cause: Three stale stanzas in the chart generator (NOT the chart contract) caused `make helm` to revert Phase 13/14/16 features: (a) `augment-tide-chart.sh` PHASE2_ARGS_REPLACEMENT re-added the Phase-13-removed `--subagent-image` stub flag and lacked the Phase 14 `--budget-reserve-per-dispatch-cents` + conditional `--pricing-overrides-json` flags; (b) the script's ENV3_BLOCK emitted a plain `CLAUDE_SUBAGENT_IMAGE: value:` instead of the Phase 13 resolution-chain comment + `required` guard + regex passthrough; (c) `tide-values.yaml` (the helmify values source) lacked the Phase 14 `pricing.overrides` block and `budget.reservePerDispatchCents: 100`.
fix: Updated `hack/helm/augment-tide-chart.sh` (args block: dropped stub flag, added budget + pricing-conditional; CLAUDE_SUBAGENT_IMAGE: replaced plain value with the resolution-chain comment + required-guard regex block; updated step-8 comment) and `hack/helm/tide-values.yaml` (added pricing.overrides block + budget.reservePerDispatchCents). Chart contract untouched.
verification: `make helm && git diff --quiet charts/` exits 0 (zero drift). Run twice — idempotent, still clean. Only changed files are the two generator-source files. `helm template` with `--set subagent.defaults.image=...` and `--set pricing.overrides...` renders the budget flag, the pricing-overrides-json conditional, no --subagent-image flag, and the CLAUDE_SUBAGENT_IMAGE resolution all correctly.
files_changed: [hack/helm/augment-tide-chart.sh, hack/helm/tide-values.yaml]

## Evidence

- timestamp: 2026-06-14 — CI `ci` run 27499716124 job "Helm chart pair lint" failed at "Verify chart tree is reproducible"; release run 27499716260 same. Diff: charts/tide/templates/deployment.yaml (+stub flag, −resolution-chain/−budget/−pricing) and charts/tide/values.yaml (−pricing.overrides block).
- timestamp: 2026-06-14 — `hack/helm/augment-tide-chart.sh` mtime Jun 8 14:34, predates Phase 13/14/16 chart edits (Jun 11–12). Milestone-close commits touched only `.planning/`.
- timestamp: 2026-06-14 — Direct read of committed deployment.yaml (lines 28-69): args block has NO --subagent-image, HAS budget + pricing-conditional; CLAUDE_SUBAGENT_IMAGE uses required-guard regex. Committed values.yaml lines 124-138 (pricing) + 261-266 (budget reserve) confirm the two source-values gaps. `tide-values.yaml` was missing both.
- timestamp: 2026-06-14 — After fixing all three stanzas: `make helm` → `git diff --quiet charts/` exit 0. Second `make helm` still exit 0 (idempotent). `git status` shows only the two hack/helm/ files modified.

## Eliminated

(none — root cause was pre-localized and confirmed by direct observation on first pass)
