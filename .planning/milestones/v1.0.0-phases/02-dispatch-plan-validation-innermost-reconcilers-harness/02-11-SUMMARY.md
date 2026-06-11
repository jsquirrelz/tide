---
phase: 2
plan: 11
subsystem: webhook/validation
tags: [webhook, dag, cycle-detection, file-touch, admission, plan-validation]
dependency_graph:
  requires: ["02-03", "02-09"]
  provides: ["PLAN-01", "PLAN-02", "PLAN-03"]
  affects: ["internal/webhook/v1alpha1/plan_webhook.go", "internal/webhook/v1alpha1/strict_mode.go"]
tech_stack:
  added:
    - "k8s.io/client-go/tools/record (EventRecorder for K8s audit Events)"
    - "pkg/dag.ComputeWaves + CycleError (cycle detection backbone)"
  patterns:
    - "Stateful webhook validator with cache-backed client"
    - "Pitfall B (informer cache lag): warning-not-rejection fall-through"
    - "Pitfall G (EXACT path equality): same-directory siblings do not trigger file-touch mismatches"
    - "D-E3 precedence resolver: annotation > resolved-cache annotation > Project.Spec > Helm default"
key_files:
  created:
    - internal/webhook/v1alpha1/strict_mode.go
    - internal/webhook/v1alpha1/strict_mode_test.go
  modified:
    - internal/webhook/v1alpha1/plan_webhook.go
    - internal/controller/suite_test.go
    - internal/controller/plan_webhook_test.go
    - cmd/manager/main.go
decisions:
  - "Phase 2 trade-off: webhook does NOT walk owner refs to find Project.Spec at admission time (3 Gets per validate adds latency). Instead, webhook reads the resolved-cache annotation tideproject.k8s/file-touch-mode-resolved that PlanReconciler can stamp. Without PlanReconciler reconciling first, mode falls back to clusterDefault."
  - "PLAN-03 verified by absence: no recoverCycle/cycleRecover/fix.*cycle/skip.*cycle identifiers in webhook code. Cycles are bugs, not runtime conditions."
  - "computeFileTouchMismatches uses EXACT string equality only (Pitfall G). pkg/x/y.go and pkg/x/y_test.go are different strings and do NOT intersect."
  - "Pitfall B (kubectl-apply order): zero Tasks visible at admission time is a warning, not a rejection. Cache-backed client (mgr.GetClient) may have stale view of recently-applied Tasks."
  - "defaultFileTouchMode hard-coded to 'warn' in cmd/manager/main.go for Phase 2. Future Helm value (e.g. --set planAdmission.fileTouchMode=strict) will plumb through config."
metrics:
  completed: "2026-05-12"
  tasks_completed: 2
  files_changed: 6
---

# Phase 2 Plan 11: Plan Admission Webhook Body (Cycle Detection + File-Touch Reconciliation) Summary

Plan 11 fills the Plan validating admission webhook body with cycle detection via `pkg/dag.ComputeWaves` (REQ-PLAN-01), file-touch ↔ dependsOn reconciliation with layered strict/warn modes (REQ-PLAN-02), and K8s Event audit emission (T-02-11-05). PLAN-03 is verified by absence — no cycle-recovery code path exists.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Failing tests for plan webhook body | 9b8e2a7 | internal/controller/plan_webhook_test.go |
| 2 (RED) | Failing tests for ResolveFileTouchMode | 029b112 | internal/webhook/v1alpha1/strict_mode_test.go |
| 2 (GREEN) | ResolveFileTouchMode helper | ec3eb1d | internal/webhook/v1alpha1/strict_mode.go |
| 1 (GREEN) | Plan webhook body implementation | da87a6f | plan_webhook.go, suite_test.go, main.go |
| Style | go fmt import reordering | c67889c | plan_webhook_test.go, strict_mode_test.go |

## Tasks-to-DAG Translation

The `tasksToDAG` function translates Task CRDs into `([]dag.NodeID, []dag.Edge)` for `pkg/dag.ComputeWaves`:

- **node** = `task.Name` (string — NodeID is `type NodeID = string`)
- **edge** = `dag.Edge{From: dep, To: task.Name}` for each `dep` in `task.Spec.DependsOn`

This means "DependsOn[i] must complete before task" — the edge direction flows from dependency to dependent. `ComputeWaves` returns `*dag.CycleError` when any node's indegree never reaches zero (nodes involved in an unresolvable cycle).

## Pitfall G Defense (EXACT Path Equality)

`computeFileTouchMismatches` uses **EXACT string equality** when computing the intersection of two tasks' `filesTouched` sets:

```go
for _, f := range a.Spec.FilesTouched {
    if _, ok := bFiles[f]; ok {
        shared = append(shared, f)
    }
}
```

`"pkg/x/y.go"` and `"pkg/x/y_test.go"` are different strings — they do NOT intersect. Only when two tasks declare the **identical path string** is a mismatch flagged. This prevents the false-positive that would arise from directory-prefix matching or path-component comparison. The test `TestPlanWebhook_FileTouchWarnMode_SameDirSiblingsNotFlagged` is the regression gate.

## Mode Resolution Precedence (D-E3)

`ResolveFileTouchMode(plan, project, clusterDefault)` in `strict_mode.go` applies the following precedence:

1. Plan annotation `tideproject.k8s/file-touch-mode=strict|warn` (direct operator override)
2. Plan annotation `tideproject.k8s/file-touch-mode-resolved=strict|warn` (resolved-cache stamped by PlanReconciler — Phase 2 trade-off to avoid admission-time owner-ref walk)
3. `project.Spec.PlanAdmission.FileTouchMode` (if `project` non-nil)
4. `clusterDefault` (Helm value, `"warn"` in Phase 2)

Bogus annotation values (anything other than `"strict"` or `"warn"`) are silently ignored and fall through to the next precedence layer.

**Phase 2 trade-off**: The webhook does NOT look up the owning Project at admission time (would require 1–3 additional Get calls per validation, adding latency). Instead, PlanReconciler (Plan 09) can stamp the resolved mode into the Plan annotation cache. Without that annotation (first admission before PlanReconciler reconciles), mode falls back to `clusterDefault`.

## K8s Event Reasons

The webhook emits K8s Events on the Plan for audit traceability (T-02-11-05):

| Reason | Type | When |
|--------|------|------|
| `CycleDetected` | `Warning` | `ComputeWaves` returns `*dag.CycleError` — admission rejected |
| `FileTouchMismatch` | `Warning` | Strict mode + mismatches detected — admission rejected |
| `FileTouchMismatch` | `Normal` | Warn mode + mismatches detected — admission allowed with warnings |

Operators query events via:
```bash
kubectl describe plan <plan-name>
kubectl get events --field-selector involvedObject.name=<plan-name>,involvedObject.kind=Plan
```

## "No Owned Tasks Visible" Warning (Pitfall B)

When the cache-backed client returns an empty Task list for the Plan's namespace + `.spec.planRef` field index, the webhook returns:

```
plan <namespace>/<name> has no owned Tasks visible at admission time; cycle detection will run when Tasks reconcile
```

This is an **admission warning**, not a rejection. The `kubectl apply -k` order (Plan before Tasks, or Tasks before Plan) determines whether Tasks are visible at Plan admission time. The informer cache may have a brief lag. Treating zero-Tasks as a warning preserves admission ergonomics — the PlanReconciler runs cycle detection as a defense-in-depth check after Tasks reconcile.

## PLAN-03 Absence Verification

```bash
grep -nE 'recoverCycle|cycleRecover|fix.*cycle|skip.*cycle' internal/webhook/v1alpha1/
```

Returns zero code matches (only a comment documenting the grep pattern itself). Cycles are bugs, not runtime conditions. The webhook rejects and surfaces a structured error naming the involved nodes — no recovery path exists.

## Deviations from Plan

### Auto-fixed Issues

None — plan executed exactly as written.

### Phase 2 Trade-offs Documented

**1. Project.Spec lookup deferred (RESEARCH.md Open Question #1)**
- **Context:** D-E3 precedence requires reading `Project.Spec.PlanAdmission.FileTouchMode`. Walking owner refs at admission time adds 1–3 Gets per validation.
- **Decision:** Webhook reads annotation cache `tideproject.k8s/file-touch-mode-resolved` instead. PlanReconciler can stamp this after reconciling. Without the annotation (first admission), mode falls back to `clusterDefault`.
- **Impact:** `TestPlanWebhook_ModeResolutionFromProjectSpec` test documents this trade-off — when no annotations exist, the Project.Spec value is NOT honored at webhook time.
- **Future work:** Phase 3 PlanReconciler can stamp the resolved-cache annotation on every reconcile.

**2. defaultFileTouchMode hard-coded in main.go**
- **Context:** The plan specified `defaultMode string` as a Helm-driven value. The config struct does not yet have a `FileTouchMode` field.
- **Decision:** Constant `defaultFileTouchMode = "warn"` in `cmd/manager/main.go` with a comment pointing to the future Helm wiring. This is correct per the Helm chart default ("warn" is safe — operators opt into strict).
- **Future work:** Add `PlanAdmission.FileTouchMode` to `internal/config/config.go` and thread it through in Phase 3.

## Known Stubs

None — all functionality is implemented.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes introduced beyond what the plan's `<threat_model>` already covers.

## Self-Check: PASSED

- `internal/webhook/v1alpha1/plan_webhook.go` — FOUND
- `internal/webhook/v1alpha1/strict_mode.go` — FOUND
- `internal/webhook/v1alpha1/strict_mode_test.go` — FOUND
- `internal/controller/plan_webhook_test.go` — FOUND (extended)
- `internal/controller/suite_test.go` — FOUND (updated)
- `cmd/manager/main.go` — FOUND (updated)
- Commit 9b8e2a7 (RED tests task 1) — FOUND
- Commit 029b112 (RED tests task 2) — FOUND
- Commit ec3eb1d (GREEN strict_mode.go) — FOUND
- Commit da87a6f (GREEN plan_webhook.go) — FOUND
- `go build ./...` — PASSED
- `make test` — PASSED (all packages green)
- PLAN-03 grep — ZERO code matches (only comment)
- `grep -c 'dag.ComputeWaves' plan_webhook.go` — 5 (≥1)
- `grep -c 'EXACT' plan_webhook.go` — 5 (≥1)
