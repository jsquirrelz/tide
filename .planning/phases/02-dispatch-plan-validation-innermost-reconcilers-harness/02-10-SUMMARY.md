---
phase: 2
plan: 10
subsystem: controller/project
tags: [init-job, budget-gate, bypass-annotation, pvc, envtest, phase2]
dependency_graph:
  requires: ["02-03", "02-07", "02-09"]
  provides: ["project-init-job", "budget-cap-halt", "bypass-annotation-watch"]
  affects: ["internal/controller/project_controller.go", "api/v1alpha1/project_types.go"]
tech_stack:
  added: ["k8s.io/client-go/tools/record", "sigs.k8s.io/controller-runtime/pkg/builder"]
  patterns: ["idempotent-init-job", "budget-gate-halt", "annotation-bypass-TTL", "pvc-bind-guard"]
key_files:
  created: []
  modified:
    - internal/controller/project_controller.go
    - internal/controller/project_controller_test.go
    - api/v1alpha1/project_types.go
decisions:
  - "Shared PVC + subPath architecture: single tide-projects PVC across all Projects, each Project isolated by subPath={UID}/workspace (Blocker #2/#3 resolved at this layer)"
  - "RequeueAfter:30s (non-blocking) when shared PVC absent — operator must fix PVC; reconciler does not crash"
  - "AnnotationChangedPredicate added to For() predicate so bypass annotation changes trigger reconcile without spec change"
  - "K8s EventRecorder added for observable audit trail (AbsoluteCapReached, BypassApplied events)"
  - "Phase constants added to api/v1alpha1/project_types.go (PhaseBudgetExceeded, PhaseInitialized, etc.)"
metrics:
  duration: "55m"
  completed: "2026-05-13"
  tasks_completed: 1
  files_changed: 3
---

# Phase 2 Plan 10: ProjectReconciler Init Job + Budget Gate + Bypass Watch Summary

Fills the Phase 2 seam of `ProjectReconciler`: one-shot init Job creation for PVC workspace bootstrap (ART-01), budget cap halt (FAIL-04), and bypass annotation watch (D-D4). All 8 test cases pass against envtest with K8s 1.36.

## What Was Built

### Init Job Creation (ART-01 / D-G1)

- Job name: `tide-init-{project.UID}` — deterministic for deduplication (T-02-10-02 mitigated)
- Image: `busybox:1.36` (pinned; Plan 12 Helm value `images.busybox.repository:tag`)
- Command: `sh -c "mkdir -p /workspace/repo /workspace/artifacts /workspace/envelopes && chmod 0775 ..."`
- SecurityContext: `runAsUser: 1000`, `fsGroup: 1000`, `readOnlyRootFilesystem: false`, `capabilities.drop: [ALL]`, `allowPrivilegeEscalation: false`
- `ServiceAccountName: tide-subagent` (zero K8s verbs — D-A4)
- `backoffLimit: 2`, `ttlSecondsAfterFinished: 300`

### Shared PVC + SubPath Architecture (Blocker #2/#3 / RESEARCH.md Open Question #2 RESOLVED)

The init Job uses a **single cluster-wide PVC** (`tide-projects`, provisioned by the Helm chart in Plan 12) rather than per-Project PVCs (deferred to Phase 3 ART-02):

- Pod-level volume: `{name: project-workspace, persistentVolumeClaim: {claimName: "tide-projects"}}`
- Container volumeMount: `{name: project-workspace, mountPath: "/workspace", subPath: "{project.UID}/workspace"}`

The subPath isolates each Project's slice; kubelet enforces the boundary. The `SharedPVCName` field on `ProjectReconciler` defaults to `"tide-projects"` and is configurable via `--workspaces-pvc-name` flag (Plan 12 wires).

### Shared PVC Bind Check

Before creating the init Job, the reconciler checks whether `tide-projects` PVC exists and is `Bound`:
- PVC absent or unbound: `ctrl.Result{RequeueAfter: 30 * time.Second}` (Pitfall 1 — non-blocking)
- This only fires on misconfiguration; the Helm chart provisions the PVC at install time

### Init Job Completion Watch

- On `JobSuccessCriteriaMet=True + JobComplete=True`: patches `Project.Status.Phase=Initialized`
- On `JobFailureTarget=True + JobFailed=True`: patches `Project.Status.Phase=InitFailed`
- Implemented via `Owns(&batchv1.Job{})` already wired in Phase 1 — no new watch needed

### Budget Cap Halt (FAIL-04 / D-D2)

Budget gate is checked at the start of every `reconcileProjectPhase2` call:

1. `budget.IsCapExceeded(&project)` — checks `Status.Budget.CostSpentCents > Spec.Budget.AbsoluteCapCents`
2. If cap exceeded AND NOT bypassed: sets `Project.Status.Phase=BudgetExceeded` + `ConditionBudgetExceeded=True, Reason=AbsoluteCapReached` + emits K8s Event (T-02-10-05 — audit trail)
3. While Phase=BudgetExceeded, the reconciler returns without proceeding to PVC check or init Job

### Bypass Annotation Watch (D-D4 / T-02-10-01)

Two forms supported, both checked via `budget.IsBypassed()`:

| Form | Annotation | Behavior |
|------|-----------|----------|
| One-shot | `tideproject.k8s/bypass-budget=true` | Consumed via `budget.ConsumeBypass` after one reconcile |
| TTL | `tideproject.k8s/bypass-budget-until=<RFC3339>` | Active while parsed time is in the future; expires naturally |

When bypass is active and Phase=BudgetExceeded: clears the phase, removes one-shot annotation (if present), emits K8s Event with `Reason=BypassApplied`.

The `AnnotationChangedPredicate` added to `For()` ensures the reconciler wakes up when bypass annotations are set/removed without a spec change (previously only `GenerationChangedPredicate` was active).

### K8s Events Emitted

| Reason | Type | Trigger |
|--------|------|---------|
| `AbsoluteCapReached` | Warning | Budget cap exceeded for the first time |
| `BypassApplied` | Normal | Bypass annotation cleared BudgetExceeded |
| (no event for InitJobFailed) | — | Phase=InitFailed set via condition only |

### RBAC Markers Added

- `+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch` — PVC bind check
- (events already present from Phase 1)

### SetupWithManager Changes

- `For(&tideprojectv1alpha1.Project{}, builder.WithPredicates(predicate.Or(GenerationChangedPredicate{}, AnnotationChangedPredicate{})))` — annotation changes now trigger reconcile
- `r.Recorder = mgr.GetEventRecorderFor("project-controller")` wired in `SetupWithManager`

## How Plan 12 Hooks In

Plan 12's Helm chart provides:
- `charts/tide/templates/projects-pvc.yaml` — the `tide-projects` shared PVC (RWX, StorageClass configurable)
- `images.busybox.repository: busybox`, `images.busybox.tag: 1.36` — init Job image values
- `--workspaces-pvc-name` flag wired into `cmd/manager` → `ProjectReconciler.SharedPVCName`

## Architecture Decision: Single Shared PVC

RESEARCH.md Open Question #2 ("per-Project PVC vs. shared PVC") is RESOLVED at this layer:
- **v1 ships single shared PVC** (`tide-projects`) with per-Project subPath isolation
- Per-Project PVC (ART-02) deferred to Phase 3 — requires dynamic provisioning, StorageClass binding semantics, and cross-namespace PVC referencing which are out of scope for Phase 2
- The `SharedPVCName` field is the extension point; Phase 3 can switch to per-Project PVCs by changing how this field is populated without changing the reconciler body

## Tests (8 cases, all pass)

| Test | What It Verifies |
|------|-----------------|
| `TestProjectReconciler_CreatesInitJobOnFirstReconcile` | Job exists, has busybox mkdir cmd, shared PVC + subPath wiring |
| `TestProjectReconciler_InitJobIdempotent` | Exactly one Job after 3 reconciles |
| `TestProjectReconciler_OnInitJobSuccess_SetsPhaseInitialized` | Phase=Initialized after Succeeded Job |
| `TestProjectReconciler_OnInitJobFailure_SetsPhaseInitFailed` | Phase=InitFailed after Failed Job |
| `TestProjectReconciler_BudgetCapExceeded_SetsBudgetExceeded` | Phase=BudgetExceeded when cap hit |
| `TestProjectReconciler_BypassAnnotation_ClearsBudgetExceeded` | One-shot bypass consumed, phase cleared |
| `TestProjectReconciler_BypassUntilAnnotation_TTLHonored` | Future TTL clears; past TTL re-enables cap |
| `TestProjectReconciler_NoSharedPVC_RequeuesAfter30s` | RequeueAfter=30s, no init Job created |

K8s 1.36 batch/v1 validation note: `JobComplete=True` requires `JobSuccessCriteriaMet=True` first; `JobFailed=True` requires `JobFailureTarget=True` first; `startTime` required for terminal Jobs. Test fixtures updated accordingly.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] K8s 1.36 Job status validation (test fixtures)**
- **Found during:** Task 1 REFACTOR phase
- **Issue:** K8s 1.36 API server rejects Job status patches that set `Complete=True` without `SuccessCriteriaMet=True` first, and `Failed=True` without `FailureTarget=True` first; also requires `startTime` for terminal jobs
- **Fix:** Updated `buildSucceededInitJob` and `buildFailedInitJob` test helpers to include the required preconditions and timestamps
- **Files modified:** `internal/controller/project_controller_test.go`
- **Commit:** a424eff

### Pre-existing Out-of-Scope Failures

4 TaskReconciler tests were already failing on the wave-5 baseline before plan 02-10:
- `TestTaskReconciler_RateLimitGate_RequeuesWhenBucketExhausted`
- `TestTaskReconciler_RateLimitStormAbsorbed`
- `TestTaskReconciler_BudgetExceededHalts`
- `TestTaskReconciler_HaltsAtMaxAttempts`

These are logged in `deferred-items.md` and are not regressions from this plan.

## Verification Gates Passed

- `go build ./...` exits 0
- `make verify-no-blocking` exits 0 (RequeueAfter:30s, no time.Sleep)
- `make verify-rbac-marker-discipline` exits 0
- All 8 ProjectReconciler test cases pass
- All 4 Phase 1 ProjectReconciler tests preserved and passing
- `grep -c 'tide-init' project_controller.go` = 4
- `grep -c 'SubPath\|tide-projects' project_controller.go` = 3
- `grep -c 'budget.IsCapExceeded\|budget.IsBypassed' project_controller.go` = 2
- `grep -c 'AnnotationChangedPredicate' project_controller.go` = 1

## Known Stubs

None — the init Job creation, budget gate, and bypass watch are fully wired. The `tide-subagent` ServiceAccountName is a soft dependency on Plan 04's ServiceAccount creation; the init Job will fail to schedule if the SA doesn't exist, but this is a deploy-time concern, not a code stub.

## Threat Flags

No new network endpoints or trust boundary surfaces introduced beyond what is declared in the plan's threat model (T-02-10-01 through T-02-10-05, all mitigated per plan).

## TDD Gate Compliance

- RED commit: `e7670fb` — test(02-10): add failing tests for ProjectReconciler init Job + budget gate
- GREEN commit: `930e7c2` — feat(02-10): implement ProjectReconciler init Job + budget cap halt + bypass watch
- REFACTOR commit: `a424eff` — refactor(02-10): fix test Job status fixtures for K8s 1.36 batch/v1 API validation
