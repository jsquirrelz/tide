---
phase: 09-cross-namespace-envelope-return-in-namespace-reporter
plan: 04
subsystem: reporter
tags: [materialization, rbac, helm-chart, reporter-package]
dependency_graph:
  requires: []
  provides: [internal/reporter, tide-reporter-rbac]
  affects: [internal/controller/dispatch_helpers.go, charts/tide/templates, examples/projects/medium]
tech_stack:
  added: [internal/reporter package]
  patterns: [delegate-to-package, helm-sot-augment, per-namespace-rbac-fan-out]
key_files:
  created:
    - internal/reporter/materialize.go
    - internal/reporter/materialize_test.go
    - hack/helm/reporter-rbac.yaml
    - charts/tide/templates/reporter-rbac.yaml
  modified:
    - internal/controller/dispatch_helpers.go
    - internal/controller/dispatch_helpers_test.go
    - hack/helm/augment-tide-chart.sh
    - examples/projects/medium/per-namespace-resources.yaml
decisions:
  - "Delegator pattern in dispatch_helpers.go: kept existing callers (4 controllers) unmodified; thin wrappers forward to internal/reporter"
  - "ChildKindAllowlist exported as package var (not func) for direct map-access at call sites"
  - "childrenAlreadyMaterialized renamed ChildrenAlreadyMaterialized (exported) since cmd/tide-reporter will call it"
  - "Per-namespace fan-out inline in reporter-rbac.yaml (same SA+Role+RoleBinding triple per namespace) vs separate template — inline mirrors push-rbac + per-namespace-rolebinding shape combined"
metrics:
  duration: "~13 minutes"
  completed: "2026-06-08T17:51:43Z"
  tasks: 3
  files: 8
---

# Phase 09 Plan 04: Lift Materialization + Reporter RBAC Summary

Materialization machinery lifted into `internal/reporter` and least-privilege `tide-reporter` SA+RBAC provisioned in chart (SOT) and medium sample. Foundation for the Option-C reader Job binary (`cmd/tide-reporter`, plan 09-05).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Lift MaterializeChildCRDs + guard + allowlist into internal/reporter | 80b403e | internal/reporter/materialize.go, internal/reporter/materialize_test.go, internal/controller/dispatch_helpers.go, internal/controller/dispatch_helpers_test.go |
| 2 | tide-reporter RBAC — chart SOT + augment + rendered template | b64ad10 | hack/helm/reporter-rbac.yaml, hack/helm/augment-tide-chart.sh, charts/tide/templates/reporter-rbac.yaml |
| 3 | Medium sample provisions tide-reporter RBAC | d46df0b | examples/projects/medium/per-namespace-resources.yaml |

## What Was Built

**Task 1 — `internal/reporter` package:** The three materialization artifacts moved verbatim from `internal/controller/dispatch_helpers.go`:
- `MaterializeChildCRDs` — creates child CRDs from EnvelopeOut.ChildCRDs with Kind-allowlist pre-flight (T-308) and AlreadyExists idempotency (SUB-03)
- `ChildrenAlreadyMaterialized` — cascade-9/10/11 spec-parent-ref idempotency guard; matches by `Spec.{Project,Milestone,Phase,Plan}Ref` with `metav1.IsControlledBy` fallback (Pitfall 3: guard lives at create-site)
- `ChildKindAllowlist` — T-308 defense-in-depth gate; moves here with materialization

`dispatch_helpers.go` now has thin delegators for the three symbols, keeping all 4 controller call sites unchanged. All 4 materialization tests pass in the new package. No import cycle — `internal/reporter` imports only `api/v1alpha1`, `pkg/dispatch`, `internal/owner`, and K8s client libs.

**Task 2 — chart SOT:** `hack/helm/reporter-rbac.yaml` is the source of truth; `augment-tide-chart.sh` cp line mirrors it to `charts/tide/templates/reporter-rbac.yaml`. The template renders a SA+Role+RoleBinding triple in `.Release.Namespace` plus one triple per entry in `.Values.projectNamespaces`. Role is least-privilege: `create,get` on `milestones,phases,plans,tasks,waves` (apiGroup `tideproject.k8s`) — no list/watch/delete/patch/update, no secrets, no core resources.

**Task 3 — medium sample:** `examples/projects/medium/per-namespace-resources.yaml` now provisions `tide-reporter` SA+Role+RoleBinding in `tide-sample-medium`, parallel to the existing `tide-push` block. File header updated to document the new identity.

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None. This plan provisions RBAC configuration and moves existing logic — no data stubs.

## Threat Surface Scan

Threat T-09-07 (elevation of privilege via over-privileged reporter SA) is mitigated as planned: the rendered Role grants only `create,get` on the 5 TIDE child Kinds, no wildcards, no cluster-scoped resources.

No new trust-boundary surface introduced beyond what the plan's `<threat_model>` specified.

## Self-Check: PASSED

| Check | Result |
|-------|--------|
| internal/reporter/materialize.go exists | FOUND |
| internal/reporter/materialize_test.go exists | FOUND |
| hack/helm/reporter-rbac.yaml exists | FOUND |
| charts/tide/templates/reporter-rbac.yaml exists | FOUND |
| Commit 80b403e exists | FOUND |
| Commit b64ad10 exists | FOUND |
| Commit d46df0b exists | FOUND |
