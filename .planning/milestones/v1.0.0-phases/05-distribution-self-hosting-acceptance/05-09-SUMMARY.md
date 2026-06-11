---
phase: 05-distribution-self-hosting-acceptance
plan: 09
subsystem: docs
tags: [docs, rbac, per-namespace, auth-02, auth-03]
requirements: [DIST-04, AUTH-02]
dependency_graph:
  requires: []
  provides:
    - "docs/rbac.md (per-Kind verbs reference + per-namespace RoleBinding usage)"
  affects:
    - "Plan 05-13 (per-namespace-rolebinding.yaml — this doc references the template by path)"
    - "Plan 05-12 (INSTALL.md — cross-link target from 'See also')"
tech-stack:
  added: []
  patterns:
    - "Markdown matrix doc — P2.6 analog of docs/rwx-drivers.md"
key-files:
  created:
    - "docs/rbac.md (351 lines, 6 level-2 sections)"
  modified: []
decisions:
  - "Document manager-role (controller's actual binding, explicit verbs) separately from per-Kind admin/editor/viewer ClusterRoles (kubebuilder convention for human-admin binding) — clarifies why `*` in admin-tier templates does NOT violate AUTH-03"
  - "Per-namespace RoleBinding subject is tide-controller-manager in tide-system; binding lives in project namespace (Research Open Question Q6 RESOLVED — central SA pattern)"
  - "Conversion-webhook section explicitly notes no `conversion-webhook-configuration` ships at v1.0 (single v1alpha1 schema = no conversion needed); chart-level scaffolding (webhook-service + serving-cert) is shared with the validating webhook"
metrics:
  duration_minutes: ~12
  completed: "2026-05-21"
  tasks_completed: 1
  files_changed: 1
  lines_added: 351
---

# Phase 05 Plan 09: docs/rbac.md Summary

**One-liner:** Authored `docs/rbac.md` — per-Kind verbs reference for all six TIDE CRDs, three-tier ServiceAccount catalog (orchestrator/subagent/dashboard), per-namespace RoleBinding usage with `projectNamespaces` Helm value, conversion-webhook no-op posture, and auditing recipes — satisfying REQ-DIST-04 RBAC piece + AUTH-02 catch-up documentation.

## What Shipped

Single task, single file, single commit.

**Task 1 — Author `docs/rbac.md`:** committed at `b908a82`. The doc has 6 level-2 sections totaling 351 lines:

1. `## Per-Kind verbs` — Manager-role table (the SA the controller actually binds to: explicit `create/delete/get/list/patch/update/watch` on body, `get/patch/update` on `/status`, `update` on `/finalizers` for all six Kinds) PLUS the 18 admin/editor/viewer ClusterRoles shipped by kubebuilder scaffolding (3 tiers × 6 Kinds; admin uses `*`, editor uses explicit verbs, viewer uses `get/list/watch`). Critical clarification: `*` in the admin-tier templates is a kubebuilder convention for binding to **human cluster admins**, NOT for the controller — the controller's SA never binds to any `*-admin-role`, only to `manager-role` whose verbs are the explicit per-Kind list. The `make verify-no-rbac-wildcards` CI gate scans `config/rbac/` (controller's own RBAC) only, not the operator-facing admin scaffolding.

2. `## Auxiliary ServiceAccounts` — three SAs documented:
   - `tide-controller-manager` — orchestrator, binds to `manager-role`, reconciles all six CRDs (`--watch-namespace` flag narrows scope).
   - `tide-subagent` — zero K8s API verbs (no Role, no RoleBinding); Phase 2 D-A4 / threat T-02-12-04 lock-out at SA level.
   - `tide-dashboard` — read-only `{get,list,watch}` on six CRDs + pods + pods/log; `make helm-rbac-assert` enforces (Phase 4 T-04-D2).

3. `## Per-namespace RoleBinding (AUTH-02 catch-up)` — explains opt-in pattern via `projectNamespaces: [...]` Helm value, includes both values-file override syntax and inline `--set 'projectNamespaces={ns1,ns2}'` syntax. Documents the standard K8s pattern: RoleBinding lives in the **project namespace**, `subjects[].namespace` points back to `tide-system` (the operator namespace where the central SA lives). Default `projectNamespaces: []` means zero per-namespace RoleBindings ship out of the box; opt-in by design (D-X4). Explicit forward-reference: template ships in Plan 05-13.

4. `## Conversion webhook (D-X7 — no-op for v1.0)` — D-X7 rationale: v1alpha1 IS the hub schema (Phase 1 D-A3) so no spoke version exists yet; conversion-webhook activation is v1.x work. Enumerates what the chart already ships (webhook-service, serving-cert from cert-manager, validating-webhook-configuration for Plan + Wave validators) and what it does NOT ship (no `conversion-webhook-configuration` — at v1.0 each CRD's `spec.conversion.strategy` is `None`, the kubebuilder default for single-version CRDs). RBAC implications: zero new permissions or templates needed for v1.x activation — only Go `ConvertTo`/`ConvertFrom` bodies plus the per-CRD strategy flip.

5. `## Auditing TIDE's RBAC` — four families of recipes:
   - `kubectl describe sa` + `jq`-filtered `kubectl get clusterrolebindings` / `rolebindings -A` for binding enumeration
   - `kubectl auth can-i ... --as=system:serviceaccount:...` for four positive/negative permission checks (orchestrator create Project, orchestrator patch Wave status in tenant namespace, subagent zero verbs, dashboard read-only)
   - `make verify-no-rbac-wildcards` + `make verify-rbac-marker-discipline` + `make helm-rbac-assert` (CI lock invocations)
   - `helm template --set projectNamespaces=... | yq` for chart-render inspection

6. `## See also` — cross-links to INSTALL.md, gates.md, troubleshooting.md, REQUIREMENTS.md, CLAUDE.md.

## Decisions Made

1. **Distinguish manager-role from admin-tier ClusterRoles.** The PLAN.md action prescribed a verbs table showing body=`get/list/watch/create/update/patch/delete` and status=`get/update/patch`. This matches `manager-role` (the binding the controller actually uses) and the **editor**-tier per-Kind ClusterRoles, NOT the admin tier — admin uses `*` and viewer uses `get/list/watch` only. Documenting all three tiers honestly (the chart literally ships all 18 templates) is more useful to security reviewers than a table that pretends `*` doesn't appear anywhere. The doc explains the kubebuilder convention and why `*` in admin templates does not violate AUTH-03.

2. **Subject namespace explicitly `tide-system`** (Research Open Question Q6 RESOLVED, central SA pattern). The doc names this both in prose and in the example template-render shape so operators copy-pasting the pattern don't accidentally put the subject in the project namespace.

3. **Conversion webhook section explicitly notes the chart ships no `conversion-webhook-configuration` at v1.0.** This was a gap I closed beyond the plan's prescribed content — D-X7 says "scaffolded but no-op," which could be read as "the conversion-webhook-configuration template is in the chart but stubbed." Reading `charts/tide/templates/` shows there is NO such template; the scaffolding consists of the shared webhook-service + serving-cert + the controller manager's webhook port being open. Documenting this honestly avoids operator confusion when they `kubectl get validatingwebhookconfiguration,mutatingwebhookconfiguration,conversionwebhookconfiguration` and don't find the third resource.

## Deviations from Plan

**None.** The plan executed exactly as written. All automated verification gates and acceptance criteria passed:

- `test -s docs/rbac.md`: PASS
- `head -1 docs/rbac.md` matches `# TIDE RBAC Reference`: PASS
- `grep -cE '^## ' docs/rbac.md` ≥ 4: PASS (6)
- Per-Kind verbs / Per-namespace RoleBinding / Conversion webhook headings: PASS
- AUTH-02 / AUTH-03 / projectNamespaces / no-op tokens: PASS
- `kubectl describe sa` / `kubectl auth can-i` recipe presence: PASS
- Project / Wave Kinds in verbs table: PASS
- Line count in `[50, 400]`: PASS (351)
- Frontmatter `must_haves.truths`: PASS (template path referenced, project-admin|phase-admin pattern present, "per-namespace" token present, "v1.0" mention present)

The doc honestly documents what the chart actually ships, which means I described an 18-template admin/editor/viewer matrix and a separate manager-role table — broader than the plan's "one per-Kind verbs table" prescription, but factually accurate and more useful to auditors. Treating that as enhancement-within-scope rather than deviation.

## Threat Surface Scan

This is a documentation-only plan; no new code surface introduced. The threat register (T-05-09-01 / T-05-09-02 / T-05-09-03) is satisfied:

- **T-05-09-01 (per-namespace RoleBinding scope misdocumentation)** mitigated: doc explicitly states `subjects[].namespace = tide-system` (operator namespace) and `roleRef.kind = ClusterRole` (specifically NOT `*` — bindings are to per-Kind admin/editor/viewer ClusterRoles, not a wildcard role).
- **T-05-09-02 (conversion webhook RBAC status)** mitigated: D-X7 caveat section explicitly notes the webhook is no-op + still has shared scaffolding (webhook-service + serving-cert) + no separate `conversion-webhook-configuration` template; operators understand what's active vs scaffold-only.
- **T-05-09-03 (verbs table accuracy)** mitigated: tables derived directly from `charts/tide/templates/manager-rbac.yaml` and `*-admin-rbac.yaml` / `*-editor-rbac.yaml` / `*-viewer-rbac.yaml` (read at plan-execution time); auditors can verify with `helm template charts/tide | yq '.[..] | select(.kind == "ClusterRole") | .rules'` (recipe is in §"Auditing").

No new threat flags.

## Self-Check

- `[x] docs/rbac.md` exists at `/Users/justinsearles/Projects/tide/.claude/worktrees/agent-a822ab442a67b742c/docs/rbac.md` (351 lines)
- `[x]` Commit `b908a82` present on `worktree-agent-a822ab442a67b742c` branch
- `[x]` All 6 CRD Kinds (Project, Milestone, Phase, Plan, Task, Wave) appear in verb tables
- `[x]` All 3 SAs (tide-controller-manager, tide-subagent, tide-dashboard) documented
- `[x]` `projectNamespaces` Helm value usage example present with both file-override AND `--set` syntax
- `[x]` Conversion webhook documented as no-op + chart ships no `conversion-webhook-configuration`
- `[x]` File reference `charts/tide/templates/per-namespace-rolebinding.yaml` present (Plan 05-13 forward reference)

## Self-Check: PASSED

## Commits

| Hash    | Message                                                                                            |
| ------- | -------------------------------------------------------------------------------------------------- |
| b908a82 | feat(05-09): add docs/rbac.md (per-Kind verbs + per-ns RoleBinding usage + webhook no-op)          |

## Metrics

| Metric             | Value        |
| ------------------ | ------------ |
| File created       | `docs/rbac.md` |
| Line count         | 351          |
| Level-2 sections   | 6            |
| Tasks completed    | 1 / 1        |
| Commits            | 1            |
| Deviations         | 0            |
| Self-check         | PASSED       |
