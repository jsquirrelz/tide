---
phase: quick-260610-vcp
plan: "01"
subsystem: audit
tags: [audit, k8s-best-practices, helm, operator, supply-chain]
dependency_graph:
  requires: []
  provides: [docs/audit/README.md, docs/audit/operator.md, docs/audit/helm-and-supply-chain.md]
  affects: []
tech_stack:
  added: []
  patterns: [evidence-first audit, PASS/DRIFT/DEVIATION classification]
key_files:
  created:
    - docs/audit/README.md
    - docs/audit/operator.md
    - docs/audit/helm-and-supply-chain.md
  modified: []
decisions:
  - "Metrics auth gap (OBS-01): FilterProvider absent despite RBAC provisioned — unauthenticated metrics is the highest-priority security hardening item"
  - "CRD subchart uses templates not crds/ dir — upgradeable but helm uninstall is destructive (HELM-05)"
  - "No ship-blockers found: all DRIFT items are NICE-TO-HAVE post-1.0 hardening"
  - "Claimed capability level: Level 1 (Basic Install) + Level 2 partial + Level 4 partial"
metrics:
  duration: "45 minutes"
  completed: "2026-06-10"
  tasks: 3
  files: 3
---

# Quick Task 260610-VCP: K8s Operator + Helm Best-Practices Audit Summary

**One-liner:** Evidence-backed PASS/DRIFT/DEVIATION audit of TIDE v1.0.0 against K8s operator and Helm best-practices checklist, producing 76 classified findings across 9 sections with a 27-item post-1.0 hardening backlog.

## What Was Done

Audited the TIDE v1.0.0 codebase (`02d3481`) against the 260610-vcp-RESEARCH.md checklist across all 9 sections (CRD design, reconciler patterns, RBAC, workload security, Helm conventions, supply chain, webhooks, observability, operator maturity). Every finding cites `file:line` evidence from actual grep/read of source. No source files, chart templates, Dockerfiles, or CI workflows were modified — the audit is strictly read-only.

## Outputs

| File | Contents |
|------|----------|
| `docs/audit/operator.md` | 56 classified findings for sections 1-4, 7-9 |
| `docs/audit/helm-and-supply-chain.md` | 20 classified findings for sections 5-6 |
| `docs/audit/README.md` | Index, 9-section summary table, 13-row deviations register with source confirmations, capability level claim, 27-item backlog |

## Finding Summary

| Section | PASS | DRIFT | DEVIATION |
|---------|------|-------|-----------|
| 1. CRD Design | 5 | 5 | 1 |
| 2. Reconciler Patterns | 10 | 1 | 1 |
| 3. RBAC | 5 | 1 | 0 |
| 4. Workload Security | 5 | 3 | 0 |
| 5. Helm Conventions | 2 | 7 | 2 |
| 6. Supply Chain | 5 | 4 | 0 |
| 7. Webhooks | 4 | 2 | 1 |
| 8. Observability | 4 | 2 | 1 |
| 9. Maturity | 3 | 2 | 0 |
| **Total** | **43** | **27** | **6** |

## Open Questions Resolved

**Open Question 1 (CRD subchart upgrade story):** `charts/tide-crds/templates/` contains CRDs as standard templates (not `crds/` dir). This means `helm upgrade tide-crds` WILL upgrade CRDs (desirable), and `helm uninstall tide-crds` WILL delete CRDs and cascade-delete all CRs (destructive, undocumented). Finding: HELM-05 (DEVIATION + residual DRIFT for missing uninstall warning in INSTALL.md).

**Open Question 2 (metrics auth posture):** `cmd/manager/main.go:263` uses `metricsserver.Options{BindAddress: metricsAddr}` without a `FilterProvider`. The `tokenreviews`/`subjectaccessreviews` RBAC is provisioned (`config/rbac/metrics_auth_role.yaml`, `charts/tide/templates/metrics-auth-rbac.yaml`) but the in-process `filters.WithAuthenticationAndAuthorization(...)` call was never wired. Metrics are currently unauthenticated. Finding: OBS-01 (DRIFT, top security hardening item).

## Key Findings

**No ship-blockers** — v1.0.0 is installable and functional. The most operationally significant DRIFT items are:

1. **OBS-01/02:** Metrics unauthenticated — `FilterProvider` not wired in `cmd/manager/main.go:263` despite RBAC infrastructure being in place. One-line fix.
2. **SUPPLY-04:** Base images not digest-pinned — `golang:1.26` and `gcr.io/distroless/static:nonroot` use mutable tags in both Dockerfiles.
3. **CRD-02/03:** `observedGeneration` not set on conditions or status struct — clients cannot distinguish stale from current status.
4. **HELM-05 (residual drift):** `helm uninstall tide-crds` destroys all CRs — no warning in INSTALL.md.
5. **HELM-01/02/07/08/11:** Missing `kubeVersion`, `values.schema.json`, `NOTES.txt`, `ct lint`, Helm test hooks — polish/DX gaps.

## Deviations Confirmed

All 13 deliberate-deviation rows confirmed in source — none misclassified as DRIFT. Key citations:
- Row 1 (CRD-status-only): no DB imports, etcd-bounded status structs
- Row 2 (CEL + webhook-only for cycles): single CEL rule at `project_types.go:299`, cycle detection at `plan_webhook.go:157`
- Row 3 (prometheus.enabled=false): `servicemonitor.yaml:1` gated
- Row 11 (CRDs subchart as templates): confirmed in `charts/tide-crds/templates/`

## Deviations from Plan

None — plan executed exactly as written. Read-only constraint held throughout (verified: `git status --porcelain` shows zero modified tracked files outside `docs/audit/`).

## Self-Check

- `docs/audit/operator.md` exists: confirmed (56 classified findings, >30 required)
- `docs/audit/helm-and-supply-chain.md` exists: confirmed (20 classified findings, >15 required)
- `docs/audit/README.md` exists: confirmed (links both detail docs, contains summary table, backlog, deviations register)
- Commit `2bffb70` exists: confirmed
- All three classification types in use: PASS=43, DRIFT=27, DEVIATION=6 (all present — zero-DRIFT audit not accepted)
- Read-only constraint: PASSED (zero tracked files modified outside `docs/audit/`)
- Research Open Questions 1 and 2: both answered with evidence

## Self-Check: PASSED
