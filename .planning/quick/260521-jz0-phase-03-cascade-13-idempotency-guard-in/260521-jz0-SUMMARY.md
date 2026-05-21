---
phase: quick
plan: "260521-jz0"
subsystem: controller
tags: [cascade-13, project-controller, idempotency-guard, push-lease]
requires:
  - .planning/debug/push-lease-phase-revert.md
provides:
  - "Phase-state idempotency guard inside handleInitJobCompletion (isJobSucceeded branch)"
  - "envtest coverage for the no-revert invariant (compiled; runtime defer to make test-int)"
affects:
  - internal/controller/project_controller.go
  - internal/controller/project_controller_test.go
tech_stack:
  added: []
  patterns:
    - "Function-level idempotency guard via explicit Phase-state switch (resists premature Phase.IsForwardOf helper extraction)"
key_files:
  created: []
  modified:
    - internal/controller/project_controller.go
    - internal/controller/project_controller_test.go
decisions:
  - "Guard lives at function level (handleInitJobCompletion), not call site (reconcileProjectPhase2:267) — internal idempotency requirement of the helper, not a leak to callers."
  - "Four forward-progressed Phase constants (PhaseRunning, PhaseComplete, PhasePushLeaseFailed, PhasePushLeakBlocked) enumerated explicitly in the switch — grep-visible contract resists Phase.IsForwardOf(Initialized) helper abstraction."
  - "isJobFailed branch UNCHANGED — PhaseInitFailed is terminal-error pre-init state, K8s Jobs do not flip Failed→Succeeded."
  - "Envtest authored against plan spec but runtime execution deferred to make test-int (bin/k8s/ envtest assets absent in this worktree) — test-debt explicitly documented in this SUMMARY."
metrics:
  duration: "~7 min"
  completed_date: "2026-05-21"
  commits: 2
  files_modified: 2
  lines_added: 111
  lines_deleted: 0
---

# Phase 03 Cascade 13: handleInitJobCompletion idempotency guard Summary

One-liner: Cascade 13 closed at the source level by inserting a Phase-state idempotency switch inside `handleInitJobCompletion`'s `isJobSucceeded` branch so re-entry on every reconcile (the init Job remains Succeeded forever after first completion) no longer clobbers forward-progressed Phases (Complete, PushLeaseFailed, PushLeakBlocked, Running) back to Initialized — unblocking push_lease Tests 3+4 from observing `Phase=Initialized` and timing out on the 90s Eventually for `Phase=PushLeaseFailed`.

## Commits

| Task | Commit  | Files                                         | Stat       |
| ---- | ------- | --------------------------------------------- | ---------- |
| 1    | `0c6905b` | `internal/controller/project_controller.go`     | +18 lines  |
| 2    | `6a9f095` | `internal/controller/project_controller_test.go` | +93 lines  |

Total: 2 commits · 2 files · +111 lines · 0 deletions.

## What changed

### Task 1 — Production fix (`0c6905b`)

Inserted a comment block + `switch project.Status.Phase { ... }` immediately after `if isJobSucceeded(job) {` (now at lines 299-316 of `project_controller.go`). The switch has one `case` arm covering the four forward-progressed Phase constants in the canonical order from `api/v1alpha1/project_types.go:327-351`:

```go
switch project.Status.Phase {
case tideprojectv1alpha1.PhaseRunning,
    tideprojectv1alpha1.PhaseComplete,
    tideprojectv1alpha1.PhasePushLeaseFailed,
    tideprojectv1alpha1.PhasePushLeakBlocked:
    // Phase has already advanced past Initialized — init-Job-completion
    // was processed in a prior reconcile. Skip the re-patch.
    return ctrl.Result{}, nil
}
```

No `default:` arm — fall-through to the existing `patch := client.MergeFrom(...)` flow is the intended behavior when Phase ∈ {Pending, Initialized, InitFailed, BudgetExceeded, ""} (pre-init or terminal-error pre-init states; the first observation that should still advance to Initialized).

The comment block names `Cascade 13` and references `.planning/debug/push-lease-phase-revert.md` for the grep contract.

No new imports — `tideprojectv1alpha1` and `ctrl` were already in scope. The function signature, `isJobFailed` branch, terminal `return ctrl.Result{}, nil` (still-running case), and call site at `reconcileProjectPhase2:267` are all UNCHANGED.

### Task 2 — Envtest (`6a9f095`)

Added `It("TestProjectReconciler_OnInitJobSuccess_DoesNotRevertPhaseFromComplete", ...)` inside the existing `Describe("Init Job lifecycle", ...)` block in `project_controller_test.go`, sibling to `TestProjectReconciler_OnInitJobSuccess_SetsPhaseInitialized` (line 209) and `TestProjectReconciler_OnInitJobFailure_SetsPhaseInitFailed` (now line 375 after insertion).

Test flow:
1. Bound PVC + Project + Reconcile 1 (adds finalizer)
2. Pre-create Succeeded init Job + patch its Status (K8s 1.36 ordering: SuccessCriteriaMet=True before Complete=True, with StartTime + CompletionTime)
3. Reconcile 2 — asserts `Phase == "Initialized"` (first observation through the success branch)
4. **New (cascade-13 contract):** manual `client.MergeFrom` Status patch to advance `Phase = PhaseComplete`
5. Sanity check: re-fetch and confirm `Phase == "Complete"`
6. **New:** Reconcile 3 — without the guard would re-stomp Phase=Initialized; with the guard stays at Complete
7. **New:** assert `Phase == "Complete"` with the `Cascade 13:` prefix in the failure message (grep contract)

No new imports — `client`, `metav1`, `corev1`, `batchv1`, `types`, `reconcile`, `tideprojectv1alpha1` were already imported. No new top-level `Describe`, no modifications to any existing test.

## Mechanism reminder

`handleInitJobCompletion` is called on every reconcile pass because the init Job remains Succeeded **permanently** after first completion — controller-runtime emits Owns events on cache resync even when the Job's observed state is unchanged. Without the guard, the function unconditionally patched `Status.Phase = PhaseInitialized` every time, clobbering any forward Phase transitions (Complete from `forcePushReady`, PushLeaseFailed from the push-Job-failed branch at line ~480, etc.) that downstream code had set.

The guard's job is to NOT re-patch when Phase has already moved forward. The existing `isJobSucceeded` patch logic — set `Phase=Initialized` + append `ConditionReady` + call `reconcilePhase3Lifecycle` — still fires correctly on the FIRST observation, when Phase is `Pending`, `Initialized` (identity case — MergeFrom is a no-op), or empty `""`.

The push-Job-failed branch at `project_controller.go:480-545` is gated on `Phase == Complete` at line 440. With Phase reverting to Initialized on every reconcile pre-fix, that branch never fired — Tests 3+4 timed out observing Phase=Initialized. Post-fix, the test's `forcePushReady` Phase=Complete patch persists across reconciles, the push-Job-failed branch fires, and `Phase=PushLeaseFailed` lands within the 90s Eventually window.

## Why this branch only

The `isJobFailed` branch (lines 334-348 post-insertion) patches `Phase = PhaseInitFailed` — a terminal-error pre-initialization state. K8s Jobs do not flip Failed → Succeeded in practice, so this code path is never re-entered after the first Failed observation. Even if it were, `PhaseInitFailed → PhaseInitFailed` is a no-op `MergeFrom` and the Conditions array would only refresh `LastTransitionTime`. The cascade-13 bug is exclusively about the **success path stomping FORWARD-progressed states**, not the failure path.

## Verify-gate outcomes

Per-task automated verify (run after each commit):

| Gate                                                                                                                          | Result   |
| ----------------------------------------------------------------------------------------------------------------------------- | -------- |
| Task 1: `go vet ./internal/controller/...`                                                                                    | exit 0   |
| Task 1: `go build ./internal/controller/...`                                                                                  | exit 0   |
| Task 1: `grep -c 'cascade.*13\|Cascade 13' internal/controller/project_controller.go`                                          | 1 (≥1 ✓) |
| Task 1: `grep -cE 'PhaseRunning,\s*$\|PhaseComplete,\s*$\|PhasePushLeaseFailed,\s*$\|PhasePushLeakBlocked' project_controller.go` | 6 (≥4 ✓) |
| Task 1: `grep -c 'switch project.Status.Phase' internal/controller/project_controller.go`                                     | 1 (≥1 ✓) |
| Task 2: `go vet ./internal/controller/...`                                                                                    | exit 0   |
| Task 2: `go build ./internal/controller/...`                                                                                  | exit 0   |
| Task 2: `go test -c ./internal/controller/...` (compile-only — envtest binaries absent)                                       | exit 0   |
| Task 2: `grep -c 'TestProjectReconciler_OnInitJobSuccess_DoesNotRevertPhaseFromComplete' project_controller_test.go`          | 1 ✓      |
| Task 2: `grep -c 'Cascade 13:' internal/controller/project_controller_test.go`                                                | 1 (≥1 ✓) |

## Test-debt

**`bin/k8s/` envtest binaries are absent in this worktree**, so `go test ./internal/controller/...` cannot execute the new envtest end-to-end in-process. The plan's per-task verify command (`go test -run 'TestProjectReconciler|TestHandleInitJobCompletion|TestControllers' ./internal/controller/...`) requires those binaries to spin up the envtest API server, and they were not provisioned for this quick-task worker.

Mitigation (per plan's explicit fallback clause in Task 2 `<done>`):
- The envtest **compiles cleanly** (`go test -c` produces a 75 MB binary at exit 0), so the test code is syntactically and type-correct.
- **Runtime verification of the no-revert invariant defers to `make test-int`** (the user-controlled runtime gate). The canonical proof of cascade-13 closure is push_lease Tests 3+4 flipping from `[FAILED] Timed out after 90.001s. <string>: Initialized` (was) to `PASS` with `Phase=PushLeaseFailed` observed within the 90s Eventually window.
- The test's invariant is identical to what `make test-int` will exercise at the integration layer; the envtest is a faster local verification surface, not an additional production-side check.

## Expected post-fix `make test-int` shape

Per the plan's verification section:

1. **13/13 specs PASS** (was 11/13 with push_lease Tests 3 and 4 timing out at 90s).
2. **Push_lease Tests 3+4 reach `Phase=PushLeaseFailed`** within the 90s Eventually window:
   - Test 3 (`push_lease_test.go:130-139`): `Eventually` for `Status.Phase == "PushLeaseFailed"` passes.
   - Test 4 (`push_lease_test.go:156-161`): same.
   - `LeaseFailureCount == 1` increment also fires (default arm of the switch at `project_controller.go:530+`).
3. **No new cascade exposed by the fix.** All four forward-progressed Phase states (Running, Complete, PushLeaseFailed, PushLeakBlocked) are protected. If a new cascade DOES surface, it's "Cascade 14" — a follow-up, not a regression of Cascade 13.
4. **Manager log shows no recurring Phase=Initialized re-stomp** in push_lease test namespaces. A parallel `kubectl get project -o yaml -w` during the failing window pre-fix observed Phase=Complete → Phase=Initialized on every reconcile; post-fix should see Phase=Complete (stable) → Phase=PushLeaseFailed (single transition).
5. **Existing init-Job-success path unaffected.** `TestProjectReconciler_OnInitJobSuccess_SetsPhaseInitialized` (line 209) still passes: a Project in Phase=Pending or Phase="" with a Succeeded init Job still advances to Phase=Initialized on first observation. The guard only short-circuits when Phase is already past Initialized.

## Self-Check: PASSED

- Commits exist on `worktree-agent-a6792489db1280b15`: `0c6905b` (Task 1), `6a9f095` (Task 2) — confirmed via `git log --oneline -2`.
- Files modified: `internal/controller/project_controller.go` (+18), `internal/controller/project_controller_test.go` (+93) — confirmed via `git diff --stat HEAD~2..HEAD`.
- Grep contracts: all 5 production-side + 2 test-side grep assertions return their expected counts (see Verify-gate table above).
- No untracked files left behind: `git status --short` shows clean working tree at task completion.
- No file deletions: `git diff --diff-filter=D --name-only HEAD~2 HEAD` is empty.
- This SUMMARY exists at `.planning/quick/260521-jz0-phase-03-cascade-13-idempotency-guard-in/260521-jz0-SUMMARY.md`.

## Out of scope (NOT touched by this plan)

- **`isJobFailed` branch of `handleInitJobCompletion`** — terminal-error pre-init state, never re-entered in practice.
- **Call site at `reconcileProjectPhase2:267`** — delegate-only call; Phase-state awareness lives at the helper level.
- **Phase state machine refactor** — no `Phase.IsForwardOf(Initialized)` helper. The four forward-progressed constants are enumerated for grep visibility.
- **`phase_controller.go`, `milestone_controller.go`** — separate cascades (7-bis, 7-ter) tracked elsewhere.
- **`charts/tide/values.yaml`** — chart is FIXED contract per CLAUDE.md.
- **`chaos_resume_test.go`, `push_lease_test.go`, cascade-9/10/11/12 artifacts** — settled in previous cascades; this fix unblocks Tests 3+4 of push_lease without touching the test spec.
- **`make test-int` end-to-end execution** — user-controlled runtime gate; orchestrator runs it after merge per plan constraint.
