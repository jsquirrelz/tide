---
phase: 04
plan: 04
subsystem: gates
tags: [gates, annotation-handshake, boundary-detection, w-2, d-g1, d-g2, d-g3, d-g4, d-w2]
dependency_graph:
  requires:
    - "04-01 — api/v1alpha1 Phase 4 constants (ConditionWaveOrLevelPaused, 4 Reasons) — consumed by reconcilers in 04-05/04-06, not directly by internal/gates"
  provides:
    - "internal/gates.EvaluatePolicy(g, level) + DefaultGates() — pure-func policy evaluator"
    - "internal/gates.PolicyAuto / PolicyApprove / PolicyPause typed constants"
    - "internal/gates.CheckApprove / CheckWaveApprove / CheckRejected / RejectedReason — annotation readers"
    - "internal/gates.ConsumeApprove / ConsumeWaveApprove / ConsumeReject — one-shot purity-preserving annotation removers"
    - "internal/gates.AnnotationApprovePrefix / AnnotationApproveWavePrefix / AnnotationReject — three exported annotation key constants"
    - "internal/gates.BoundaryDetected(ctx, c, parent, childKind) — shared seam for gate hook AND W-2 push trigger"
  affects:
    - "04-05 (TaskReconciler gate hook + up-stack reconciler edits) — consumes EvaluatePolicy / CheckApprove / CheckRejected / ConsumeApprove"
    - "04-06 (boundary push trigger + W-1 exit-10 split) — consumes BoundaryDetected at the up-stack reconcilers' Succeeded-transition seam"
    - "cmd/tide approve / reject / resume — consume the annotation key constants for write-side patches via client-go"
tech_stack:
  added: []
  patterns:
    - "pure-func policy evaluator (mirrors internal/budget/cap.IsCapExceeded shape)"
    - "annotation-consumer with NEW-map purity contract (mirrors internal/budget/cap.ConsumeBypass exactly)"
    - "controller-runtime client.List + metav1.IsControlledBy owner-ref filter (matches MaterializeChildCRDs convention)"
    - "switch-on-childKind dispatch (matches the existing childKindAllowlist pattern in dispatch_helpers.go)"
key_files:
  created:
    - internal/gates/policy.go
    - internal/gates/policy_test.go
    - internal/gates/annotation.go
    - internal/gates/annotation_test.go
    - internal/gates/boundary.go
    - internal/gates/boundary_test.go
    - internal/gates/doc.go
  modified: []
decisions:
  - "BoundaryDetected filters via metav1.IsControlledBy (owner-ref), NOT a label selector. RESEARCH §1300-1303 recommended labels.SelectorFromSet OR owner-ref; the plan body offered both with fallback. Chose owner-ref-only because (a) MaterializeChildCRDs in dispatch_helpers.go uses controllerutil.SetControllerReference (no label stamping at child creation), (b) plan_controller.go stamps tideproject.k8s/project on Task children but no other reconciler does, so owner refs are the only universal seam."
  - "CheckRejected requires NON-EMPTY value (D-G4 nuance). The plan's Test 5 says 'any non-empty value' — implemented strictly. An empty-value reject annotation is treated as no-rejection so a clear-via-empty kubectl annotate does not accidentally halt the run."
  - "All Consume* helpers return a non-nil empty map when the source has no annotations. Spares every caller a nil-check before patching. Matches the budget.ConsumeBypass return-style closely (it returns nil for nil project, but the gates parallels client.Object which has GetAnnotations() yielding nil-safe maps)."
  - "BoundaryDetected's vacuous-truth rejection: returns (false, nil) on empty filtered child set. The plan's Test 7 asserts this directly — caller must NOT treat 'no children' as a boundary, since the reconcilers that consult this (in 04-06) are at the Succeeded-transition seam where at least one child must have existed for the transition to be meaningful."
  - "EvaluatePolicy unknown-level returns PolicyAuto (safe non-panic default), NOT an error. The plan's Test 5 specifies this. Reasoning: this function is on the hot path of every reconcile loop; a panic on a typo would crash the manager, while degrading to today's behavior (auto-advance) maintains correctness for the existing-Project case where Gates is empty."
metrics:
  duration_minutes: 12
  completed_date: 2026-05-19
  tasks_completed: 3
  files_created: 7
  files_modified: 0
  commits: 6
---

# Phase 4 Plan 04: internal/gates Package Summary

Ship the `internal/gates` package — three files (`policy.go`, `annotation.go`, `boundary.go`) plus `doc.go` — encapsulating every gate-policy decision and annotation handshake the up-stack reconcilers will consume in plan 04-05. Co-located per RESEARCH §1300-1303 over `internal/controller/push_helpers.go` because `BoundaryDetected` is the shared seam between the gate-policy code path AND the W-2 mid-stack push trigger.

## What landed

### `internal/gates/policy.go` — EvaluatePolicy + DefaultGates

```go
const (
    PolicyAuto    tideprojectv1alpha1.GatePolicy = "auto"
    PolicyApprove tideprojectv1alpha1.GatePolicy = "approve"
    PolicyPause   tideprojectv1alpha1.GatePolicy = "pause"
)

func DefaultGates() tideprojectv1alpha1.Gates // D-G1 locked defaults
func EvaluatePolicy(g tideprojectv1alpha1.Gates, level string) tideprojectv1alpha1.GatePolicy
```

EvaluatePolicy reads the matching `Gates` field by `level` and applies the D-G1 per-level default when the field is empty:

| level     | default     | source           |
| --------- | ----------- | ---------------- |
| milestone | `approve`   | D-G1             |
| phase     | `auto`      | D-G1             |
| plan      | `auto`      | D-G1             |
| task      | `auto`      | D-G1             |
| _unknown_ | `auto`      | safe non-panic   |

`DefaultGates()` returns the locked struct: `{Milestone: approve, Phase: auto, Plan: auto, Task: auto, PauseBetweenWaves: false}`.

### `internal/gates/annotation.go` — approve/reject/wave-approve handshake

Three exported annotation key constants:

| Constant                       | Value                              | Role                           |
| ------------------------------ | ---------------------------------- | ------------------------------ |
| `AnnotationApprovePrefix`      | `tideproject.k8s/approve-`         | level suffix (D-G2)            |
| `AnnotationApproveWavePrefix`  | `tideproject.k8s/approve-wave-`    | integer wave suffix (D-G3)     |
| `AnnotationReject`             | `tideproject.k8s/reject`           | value carries reason (D-G4)    |

Seven helpers — all pure, all `client.Object`-shaped:

| Function | Returns | Notes |
| -------- | ------- | ----- |
| `CheckApprove(obj, level)` | `bool` | strict-`"true"` value at `approve-<level>` |
| `CheckWaveApprove(obj, N)` | `bool` | strict-`"true"` at `approve-wave-<N>` (T-04-G3 isolation) |
| `CheckRejected(obj)` | `bool` | non-empty value at `reject` |
| `RejectedReason(obj)` | `string` | the reject annotation value |
| `ConsumeApprove(obj, level)` | `map[string]string` | NEW map with key removed |
| `ConsumeWaveApprove(obj, N)` | `map[string]string` | NEW map with key removed |
| `ConsumeReject(obj)` | `map[string]string` | NEW map with key removed |

All `Consume*` helpers mirror `budget.ConsumeBypass` exactly: caller does the `Patch`, original `Annotations` map is untouched, return value is non-nil (empty map when source is nil).

### `internal/gates/boundary.go` — BoundaryDetected (W-2 shared seam)

```go
func BoundaryDetected(ctx context.Context, c client.Client, parent client.Object, childKind string) (bool, error)
```

Handles four child kinds: `Milestone | Phase | Plan | Task`. Implementation:

1. Switch on `childKind` → instantiate the appropriate `*tideprojectv1alpha1.<Kind>List`
2. `c.List(ctx, &list, client.InNamespace(parent.GetNamespace()))`
3. Filter via `metav1.IsControlledBy(child, parent)` (matches `controllerutil.SetControllerReference` convention used by `MaterializeChildCRDs`)
4. Return `(true, nil)` iff `matched > 0` AND every filtered child has `Status.Phase == "Succeeded"`
5. Return `(false, nil)` on empty filtered child set (T-04-W2: not a boundary)
6. Return `(false, error)` on unsupported childKind / nil parent / nil client

## Test coverage

All tests pure-func or fake-client-based; no envtest required.

| File | Test functions | Sub-tests | Pass with `-race` |
| ---- | -------------- | --------- | ----------------- |
| `policy_test.go` | 3 | 14 (table-driven) | ✅ |
| `annotation_test.go` | 9 | 7 (within `TestCheckApprove`) | ✅ |
| `boundary_test.go` | 8 | 4 (within `TestBoundaryDetectedSupportedKinds`) | ✅ |

```
ok  	github.com/jsquirrelz/tide/internal/gates	2.339s
```

## Plan verification block satisfied

| Check | Result |
| ----- | ------ |
| `go test ./internal/gates/... -race -v` | all subtests PASS |
| `grep -c "tideproject.k8s/approve" internal/gates/annotation.go` | **3** (≥ 2 required) |
| `grep -c "tideproject.k8s/reject" internal/gates/annotation.go` | **4** (≥ 1 required) |
| `grep -c "BoundaryDetected" internal/gates/boundary.go` | **9** (≥ 1 required) |
| `make tide-lint` | clean (no metric-cardinality / provider-firewall violations) |
| `go build ./...` | clean (no module-wide regressions) |
| one-way arrow: `internal/gates` has zero `internal/controller/` imports | ✅ (grep returns no matches) |

## What downstream plans now consume

| Downstream plan | Consumes |
| --------------- | -------- |
| **04-05** (TaskReconciler + up-stack reconciler gate hooks) | `EvaluatePolicy`, `CheckApprove`, `CheckRejected`, `ConsumeApprove`, `ConsumeReject` |
| **04-06** (boundary push trigger + W-1 exit-10 split) | `BoundaryDetected` at the four up-stack reconcilers' Succeeded-transition seam |
| **cmd/tide approve / reject / resume** (Wave 4) | `AnnotationApprovePrefix`, `AnnotationApproveWavePrefix`, `AnnotationReject` for write-side annotation patches via client-go |
| All reconciler edits emitting wave-pause behavior | `PolicyAuto / PolicyApprove / PolicyPause` typed constants |

## TDD Gate Compliance

All three tasks followed strict RED → GREEN cycles. Commit ledger:

| Task | Phase | Commit | Type | Subject |
| ---- | ----- | ------ | ---- | ------- |
| 1 | RED   | `a2677ee` | test | failing tests for internal/gates policy.go |
| 1 | GREEN | `94d44c7` | feat | internal/gates policy.go + doc.go |
| 2 | RED   | `1526795` | test | failing tests for internal/gates annotation.go |
| 2 | GREEN | `ac58856` | feat | internal/gates annotation.go |
| 3 | RED   | `5172b75` | test | failing tests for internal/gates boundary.go |
| 3 | GREEN | `5c45dd4` | feat | internal/gates boundary.go (W-2 shared seam) |

Six commits. Every RED was verified to fail (build error: `undefined: PolicyApprove`, etc.) BEFORE the corresponding GREEN landed.

## Deviations from Plan

None. The plan executed exactly as written. Two minor implementation-detail choices documented as decisions above:

1. `BoundaryDetected` uses owner-ref filtering (not label selectors). The plan's Test 5 wording explicitly accepts either approach with documented rationale — owner refs are the universal seam in this codebase.
2. `CheckRejected` is strict on non-empty value. The plan's Test 5 specifies "any non-empty value"; this matches D-G4's intent (rejection carries a reason).

Both choices are pure plan-conformance refinements, not deviations.

## Known Stubs

None. Every function declared, every annotation key exported, every BoundaryDetected childKind branch implemented. The package compiles and tests independently of the downstream reconciler wiring (which lands in 04-05 and 04-06).

## Threat Flags

None. The plan's `<threat_model>` (T-04-G1 spoofing, T-04-G2 replay, T-04-G3 wave-skip, T-04-W2 boundary DoS) is fully mitigated by:

- **T-04-G1**: `EvaluatePolicy` reads only the CEL-validated CRD field; no env/annotation override path exists.
- **T-04-G2**: All `Consume*` helpers return a NEW map; caller patches once. Re-triggering requires a fresh annotation write.
- **T-04-G3**: `CheckWaveApprove` keys off `strconv.Itoa(waveN)` — approve-wave-3 does NOT approve wave 4 (Test 4 in `boundary_test.go` asserts this directly via the `CheckWaveApprove(plan, 4)` check).
- **T-04-W2**: `BoundaryDetected` returns `(false, nil)` on empty child set; documented as "vacuous boundary is NOT a real boundary" in the function godoc.

No new threat surface introduced.

## Self-Check: PASSED

Files exist:
- ✅ `internal/gates/policy.go`
- ✅ `internal/gates/policy_test.go`
- ✅ `internal/gates/annotation.go`
- ✅ `internal/gates/annotation_test.go`
- ✅ `internal/gates/boundary.go`
- ✅ `internal/gates/boundary_test.go`
- ✅ `internal/gates/doc.go`

Commits exist on worktree branch (`git log --all --oneline | grep 04-04`):
- ✅ `a2677ee` test(04-04): RED — policy
- ✅ `94d44c7` feat(04-04): GREEN — policy
- ✅ `1526795` test(04-04): RED — annotation
- ✅ `ac58856` feat(04-04): GREEN — annotation
- ✅ `5172b75` test(04-04): RED — boundary
- ✅ `5c45dd4` feat(04-04): GREEN — boundary

Tests pass with `-race` on the gates package. Plan verification block satisfied: grep counts (3 / 4 / 9), `make tide-lint` clean, `go build ./...` clean, no `internal/controller/` imports under `internal/gates/` (one-way arrow preserved).
