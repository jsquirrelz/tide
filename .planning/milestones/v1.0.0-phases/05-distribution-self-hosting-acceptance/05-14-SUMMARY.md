---
phase: 05-distribution-self-hosting-acceptance
plan: 14
subsystem: helm-distribution
tags: [crd-subchart, resource-policy, helm-upgrade-safety, augment-script, regen-resilience]

dependency_graph:
  requires: []
  provides:
    - "charts/tide-crds/templates/*-crd.yaml carries helm.sh/resource-policy: keep — prevents `helm uninstall tide-crds` from cascade-deleting all Project/Milestone/Phase/Plan/Task/Wave resources"
    - "hack/helm/augment-tide-crds-chart.sh is idempotent re-injector — `make helm-crds` is regen-stable post-augment"
  affects:
    - "Plan 05-07 (INSTALL.md upgrade-order section can now cite the annotation as the canonical CRD-preservation mechanism)"
    - "Plan 05-10 (troubleshooting.md row 7 — uninstall+reinstall no longer destroys cluster state)"
    - "Plan 05-16 (release-pipeline helmify-verify job mirrors the regen-resilience gate verified here)"

tech_stack:
  added: []
  patterns:
    - "Idempotent post-helmify shell augmentation — grep-q pre-check gates awk-based annotation injection; re-runs are no-ops"
    - "Portable awk substitution (BSD + GNU compatible) — no GNU-only sed flags; atomic via .tmp + mv"
    - "Source-of-truth chain: api/v1alpha1/*.go → controller-gen → config/crd/bases/ → kustomize → helmify → augment-script → charts/tide-crds/templates/ — augment script is the canonical re-injector for chart-only concerns helmify can't infer"

key_files:
  created: []
  modified:
    - "hack/helm/augment-tide-crds-chart.sh — added 6-CRD loop with grep-q-gated awk insertion of `helm.sh/resource-policy: keep` after the existing `controller-gen.kubebuilder.io/version:` line"
    - "charts/tide-crds/templates/milestone-crd.yaml — annotation injected + controller-gen catch-up (additionalPrinterColumns)"
    - "charts/tide-crds/templates/phase-crd.yaml — annotation injected + controller-gen catch-up (additionalPrinterColumns)"
    - "charts/tide-crds/templates/plan-crd.yaml — annotation injected + controller-gen catch-up (additionalPrinterColumns)"
    - "charts/tide-crds/templates/project-crd.yaml — annotation injected + controller-gen catch-up (rollingWindowDuration, allowedRoutes, git spec, subagent spec, git status, additionalPrinterColumns)"
    - "charts/tide-crds/templates/task-crd.yaml — annotation injected + controller-gen catch-up (wait-for-signal stub mode, additionalPrinterColumns)"
    - "charts/tide-crds/templates/wave-crd.yaml — annotation injected + controller-gen catch-up (additionalPrinterColumns)"

decisions:
  - "Pattern A (extend augment script with awk) over Pattern B (kubebuilder source markers) per PATTERNS.md §P3.4 — kubebuilder marker support for CRD-level annotations is incomplete, and the augment script is already the canonical Chart.yaml/values.yaml re-injector for helmify post-processing."
  - "Inserted the annotation as a sibling of the existing `controller-gen.kubebuilder.io/version:` annotation (under metadata.annotations at 4-space indent) — not under spec.versions.schema. The additional_context explicitly called this out."
  - "Treated the controller-gen-driven `additionalPrinterColumns` + Phase-3/4 schema content drift as a Rule 2 deviation (auto-add missing critical regen output): the plan's acceptance criterion `make helm-crds && git diff --exit-code charts/tide-crds/` cannot pass without landing the stale-template catch-up, because the previously committed templates were stale relative to api/v1alpha1/*.go. Documented prominently in the commit body so reviewers see it."

metrics:
  duration_minutes: 12
  task_count: 1
  files_modified: 7
  completed_date: 2026-05-22
---

# Phase 05 Plan 14: CRD-subchart resource-policy annotation Summary

Extended `hack/helm/augment-tide-crds-chart.sh` to inject `helm.sh/resource-policy: keep` into every CRD template under `charts/tide-crds/templates/` on every `make helm-crds` regeneration. Closes Phase 05 RESEARCH §"Topic 8" / Pitfall 2: without this annotation, `helm uninstall tide-crds` cascade-deletes the 6 CRDs and (via Kubernetes CRD-delete cascade) every Project/Milestone/Phase/Plan/Task/Wave resource in the cluster.

## What changed

### `hack/helm/augment-tide-crds-chart.sh` (extended; 38-line delta)

Added a per-CRD injection loop after the existing Chart.yaml copy step. For each `charts/tide-crds/templates/*-crd.yaml`:

1. `grep -q 'helm.sh/resource-policy: keep' "${crd}"` — pre-check gates re-injection (idempotency).
2. If absent, `awk` prints every line as-is, except when a line matches `^    controller-gen\.kubebuilder\.io/version:` it prints that line and immediately follows with `    helm.sh/resource-policy: keep` (same 4-space indent helmify emits).
3. Atomic via `${crd}.tmp` + `mv` (no in-place edit, no GNU-only `sed -i` quirks).

Script emits a per-run count (`OK: ... on N CRD(s) (already-present skipped)`) — 6 on a fresh helmify, 0 on subsequent re-runs.

### 6 CRD templates updated

All six (`milestone-crd.yaml`, `phase-crd.yaml`, `plan-crd.yaml`, `project-crd.yaml`, `task-crd.yaml`, `wave-crd.yaml`) now carry:

```yaml
metadata:
  name: <kind>s.tideproject.k8s
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
    helm.sh/resource-policy: keep   # ← injected by augment script
  labels:
  {{- include "tide-crds.labels" . | nindent 4 }}
```

## Verification (all acceptance criteria pass)

| Check | Command | Expected | Actual |
|---|---|---|---|
| Script runs cleanly | `bash hack/helm/augment-tide-crds-chart.sh` | exit 0 | exit 0 |
| All 6 CRDs annotated | `grep -l 'helm.sh/resource-policy: keep' charts/tide-crds/templates/*-crd.yaml \| wc -l` | 6 | 6 |
| Idempotent (1st run) | `grep -c '...' charts/tide-crds/templates/project-crd.yaml` after 1st run | 1 | 1 |
| Idempotent (2nd run) | re-run script; same `grep -c` on project-crd.yaml | 1 (not 2) | 1 |
| Script references annotation | `grep -c "helm.sh/resource-policy" hack/helm/augment-tide-crds-chart.sh` | ≥ 1 | 6 (2 in header comments, 1 in grep -q gate, 1 in awk print, 1 in injection-comment, 1 in OK echo) |
| helm lint clean | `helm lint charts/tide-crds` | exit 0 | exit 0 (1 info: icon recommended — pre-existing) |
| helm template renders 6 | `helm template charts/tide-crds 2>/dev/null \| grep -c 'helm.sh/resource-policy: keep'` | 6 | 6 |
| Regen-stable | `make helm-crds` 2x; SHA of templates dir unchanged | identical SHA | `4b7343ea5bdebb4232272358a68af5327ffb78c4` (both runs) |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Pre-existing helmified-template drift caught up to source**

- **Found during:** Task 1 Step 7 (`make helm-crds` regen-resilience check)
- **Issue:** `make helm-crds && git diff --exit-code charts/tide-crds/` returned a 333-insertion diff that went well beyond just the 6 annotation lines. controller-gen pulled in schema content that exists in `api/v1alpha1/*.go` (Phase 3 and Phase 04.1 work) but was never propagated into the committed `charts/tide-crds/templates/*-crd.yaml`. Specifically:
  - `additionalPrinterColumns` on all 6 CRDs (Phase/Status/Age columns for `kubectl get`)
  - `Project.Spec.Budget.RollingWindowDuration` field (Phase 04.1 P4.1)
  - `Project.Spec.Providers[].AllowedRoutes` (Phase 04.1 P4.2 credproxy allowlist)
  - `Project.Spec.Git` block (Phase 3 D-B6 git push target/creds)
  - `Project.Spec.Subagent` block (provider/model selection)
  - `Project.Status.Git` block (push lease state)
  - `Task.Spec.Lifecycle.TestMode: wait-for-signal` enum value (Phase 3 D-D3 chaos-resume stub mode)
  - Trailing-newline normalization on all 6 files
- **Root cause:** `charts/tide-crds/templates/` was last touched in Phase 02 commit `5c81db9` (WR-02 fix). `api/v1alpha1/*.go` has been heavily extended in Phase 03 (`9f688cb`, `217c1fb`, `cc22bad`) and Phase 04.1 (`31f8a75`, `00b74f1`). `config/crd/bases/` was kept in sync (controller-gen always re-emits there), but the helmified subchart wasn't regenerated.
- **Fix:** Landed the full `make helm-crds` output. The plan's own acceptance criterion `make helm-crds && git diff --exit-code charts/tide-crds/` exits 0 demands this — there is no way to make that exit 0 without committing the stale-template catch-up.
- **Files modified:** the 6 CRD templates (already in the plan's `files_modified` allowlist — no scope expansion)
- **Commit:** `614ef60`
- **Justification under Rule 2:** This is missing critical correctness output. The `tide-crds` chart users would otherwise install a stale CRD schema that rejects valid `Project` specs (e.g. `spec.git` would be rejected by the OpenAPI validation as an unknown field). The acceptance criterion explicitly tests this surface; landing the regen is the only way to satisfy it. The drift was not introduced by this plan — it was surfaced by this plan's regen-resilience check.

## Authentication gates

None.

## Threat-model status

| Threat ID | Disposition | Mitigation status |
|---|---|---|
| T-05-14-01 (DoS via uninstall cascade) | mitigate | **CLOSED** — annotation present in all 6 CRDs; `helm template` renders 6 instances; reading `helm.sh/resource-policy: keep` on the rendered output confirms Helm will skip these resources on `helm uninstall tide-crds`. |
| T-05-14-02 (Augment-script regen drift) | mitigate | **CLOSED** — `make helm-crds` is now regen-stable: running it twice produces identical SHA on the templates dir. The augment script's grep-q-gated awk injection is the source-of-truth re-injector that survives every helmify regen. |
| T-05-14-03 (Duplicate-annotation insertion) | mitigate | **CLOSED** — running the augment script twice keeps annotation count at exactly 1 per CRD (verified). The `grep -q` pre-check prevents the awk insertion from firing when the line is already present. |

## Known Stubs

None. The annotation lands on real CRDs against the live `api/v1alpha1/*.go` source; there are no placeholder values or unwired components.

## Threat Flags

None — this plan REMOVES a security/availability gap; it does not introduce new attack surface. The annotation is a Helm-canonical opt-out for resource preservation, not a new endpoint, auth path, or trust-boundary change.

## Commits

| Commit | Message |
|---|---|
| `614ef60` | `feat(05-14): inject helm.sh/resource-policy: keep on all 6 CRD templates` |

## Self-Check: PASSED

- `hack/helm/augment-tide-crds-chart.sh`: present, 52 lines (was 19), `grep -c 'helm.sh/resource-policy' hack/helm/augment-tide-crds-chart.sh` returns 6 (2 in header comments, 1 in grep -q gate, 1 in awk print insertion, 1 in inline comment, 1 in OK echo message).
- All 6 `charts/tide-crds/templates/*-crd.yaml`: present, each contains `helm.sh/resource-policy: keep` exactly once at metadata.annotations 4-space indent.
- `helm lint charts/tide-crds`: exit 0.
- `helm template charts/tide-crds 2>/dev/null | grep -c 'helm.sh/resource-policy: keep'`: returns `6`.
- `make helm-crds` regen-stable: SHA of templates dir matches across two consecutive runs.
- Commit `614ef60`: present in `git log` on `worktree-agent-a2ae92975ba45023b`.
