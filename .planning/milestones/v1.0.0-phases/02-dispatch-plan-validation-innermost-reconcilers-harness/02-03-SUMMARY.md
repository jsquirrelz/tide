---
phase: 2
plan: 3
subsystem: api/v1alpha1
tags: [schema, crd, codegen, persist-02, phase2]
dependency_graph:
  requires: []
  provides: [Project.Spec.ProviderSecretRef, Project.Spec.Providers, Project.Spec.Budget, Project.Spec.PlanAdmission, Project.Spec.MaxAttemptsPerTask, Project.Status.Budget, Task.Spec.DeclaredOutputPaths, Task.Spec.Caps, Task.Spec.Dev, Plan.Status.ValidationState, Plan.Status.CycleEdges, shared_types Phase2 constants]
  affects: [02-07, 02-09, 02-10, 02-11, 02-13]
tech_stack:
  added: []
  patterns: [kubebuilder CEL markers, controller-gen codegen, PERSIST-02 guard test, two-type Caps design]
key_files:
  created: []
  modified:
    - api/v1alpha1/project_types.go
    - api/v1alpha1/task_types.go
    - api/v1alpha1/plan_types.go
    - api/v1alpha1/shared_types.go
    - api/v1alpha1/zz_generated.deepcopy.go
    - api/v1alpha1/aggregates_guard_test.go
    - config/crd/bases/tideproject.k8s_projects.yaml
    - config/crd/bases/tideproject.k8s_tasks.yaml
    - config/crd/bases/tideproject.k8s_plans.yaml
    - config/samples/tide_v1alpha1_task_*.yaml (all 8)
    - internal/controller/task_controller_test.go
decisions:
  - api/v1alpha1.Caps is a CRD-local struct (not a type alias to pkg/dispatch.Caps) — controller-gen cannot traverse cross-package type aliases; Plan 09 TaskReconciler.buildEnvelopeIn translates between the two at dispatch time
  - DeclaredOutputPaths added to all task config/samples/ and task_controller_test.go fixture — required field (MinItems=1 without omitempty) so existing fixtures must include it
  - BudgetStatus comment reworded to avoid containing PERSIST-02 denylist tokens — original draft comment included the forbidden words verbatim as negation, which tripped the grep-based gate
metrics:
  duration: 4min
  completed: "2026-05-13T02:51:39Z"
  tasks: 2
  files: 19
---

# Phase 2 Plan 3: Phase 2 Schema Additions (Project/Task/Plan CRDs + shared_types) Summary

Phase 2 CRD schema additions landed additively on top of Phase 1: five new fields on Project.Spec, a BudgetStatus tally struct on Project.Status, DeclaredOutputPaths/Caps/Dev additions on Task.Spec, ValidationState/CycleEdges on Plan.Status, and 9 new constants in shared_types.go — all with codegen regenerated and PERSIST-02 guard clean.

## What Was Built

### Project.Spec additions (project_types.go)

| Field | Type | JSON tag | CEL constraint |
|-------|------|----------|----------------|
| `ProviderSecretRef` | `string` | `providerSecretRef,omitempty` | — |
| `Providers` | `[]ProviderConfig` | `providers,omitempty` | — |
| `Budget` | `BudgetConfig` | `budget,omitempty` | — |
| `PlanAdmission` | `PlanAdmissionConfig` | `planAdmission,omitempty` | — |
| `MaxAttemptsPerTask` | `int32` | `maxAttemptsPerTask,omitempty` | Minimum=1, Maximum=10 |

Nested structs added: `ProviderConfig` (Name Enum=anthropic, RequestsPerMinute *int32, TokensPerMinute *int32), `BudgetConfig` (AbsoluteCapCents int64 Minimum=0, RollingWindowCapCents int64), `PlanAdmissionConfig` (FileTouchMode Enum=strict;warn).

### Project.Status additions (project_types.go)

| Field | Type | JSON tag |
|-------|------|----------|
| `Budget` | `BudgetStatus` | `budget,omitempty` |

`BudgetStatus` struct: `TokensSpent int64`, `CostSpentCents int64`, `WindowStart *metav1.Time`. This is a tally object — not a schedule aggregate. PERSIST-02 guard clean.

### Task.Spec additions (task_types.go)

| Field | Type | JSON tag | CEL constraint |
|-------|------|----------|----------------|
| `DeclaredOutputPaths` | `[]string` | `declaredOutputPaths` | MinItems=1 |
| `Caps` | `*Caps` | `caps,omitempty` | pointer — optional |
| `Dev` | `TaskDev` | `dev,omitempty` | zero-value embed |

`Caps` struct (CRD-local): `WallClockSeconds int32 (Minimum=1)`, `Iterations int32 (Minimum=1)`, `InputTokens int64 (Minimum=0)`, `OutputTokens int64 (Minimum=0)`.

`TaskDev` struct: `TestMode string (Enum=success;fail-exit-1;hang;exceed-output-paths)`.

### Plan.Status additions (plan_types.go)

| Field | Type | JSON tag | CEL constraint |
|-------|------|----------|----------------|
| `ValidationState` | `string` | `validationState,omitempty` | Enum=Pending;Validated;CycleDetected;FileTouchMismatch |
| `CycleEdges` | `[]string` | `cycleEdges,omitempty` | — |

### shared_types.go additions — 9 new constants

**4 condition constants:**
- `ConditionValidated = "Validated"`
- `ConditionBudgetExceeded = "BudgetExceeded"`
- `ConditionRunning = "Running"`
- `ConditionSucceeded = "Succeeded"`

**5 reason constants:**
- `ReasonCycleDetected = "CycleDetected"`
- `ReasonFileTouchMismatch = "FileTouchMismatch"`
- `ReasonCapHit = "CapHit"`
- `ReasonRateLimitHit = "RateLimitHit"`
- `ReasonBypassApplied = "BypassApplied"`

All grouped in a second `const ( ... )` block after the Phase 1 block, preserving visual separation.

## PERSIST-02 Guard Status

`make verify-no-aggregates` exits 0 against the Phase 2 schema — no `Schedule`, `Waves []`, `IndegreeMap`, `CachedDag`, or `DerivedDag` tokens in `*_types.go` files.

`aggregates_guard_test.go` extended with two new functions (4 total):
- `TestAggregatesGuardCatchesViolation` — Phase 1, preserved
- `TestAggregatesGuardSilentOnCleanFile` — Phase 1, preserved
- `TestAggregatesGuard_PreservesPhase1Denylist` — Phase 2: walks real `*_types.go` tree, asserts denylist matches nothing
- `TestAggregatesGuard_BudgetStatusIsNotAggregate` — Phase 2: positive-shape assertion that `BudgetStatus` exists AND its body contains no forbidden tokens

All pass: `go test ./api/v1alpha1/... -run TestAggregatesGuard` PASS.

## api/v1alpha1.Caps vs pkg/dispatch.Caps Two-Type Design

These are intentionally two separate types that serve different layers:

- **`api/v1alpha1.Caps`** — CRD admission boundary type; CEL-validated at `kubectl apply` time; lives alongside the Task CRD; controller-gen generates the CRD YAML from it. Fields are JSON-tagged for Kubernetes.
- **`pkg/dispatch.Caps`** — Go-only public API used by the dispatcher interface; no kubebuilder markers; lives in the `pkg/dispatch` leaf package (stdlib-only, DAG-05 style firewall).

Plan 09's `TaskReconciler.buildEnvelopeIn` translates `api/v1alpha1.Caps` → `pkg/dispatch.Caps` at dispatch time. The two types must stay in sync field-for-field (same four capacity dimensions), but the CRD layer enforces admission constraints while the dispatch layer owns the in-process contract. This matches the kubebuilder pattern of keeping the CRD schema decoupled from internal Go abstractions.

## Codegen

- `make manifests` — regenerated `config/crd/bases/tideproject.k8s_{projects,tasks,plans}.yaml` with new fields, enum constraints, and validation rules.
- `make generate` — regenerated `api/v1alpha1/zz_generated.deepcopy.go` with DeepCopy methods for all new structs.
- Both are idempotent: second invocation produces zero diff.

## Verification Results

- `make manifests generate` — exit 0, idempotent
- `make verify-no-aggregates` — exit 0 (PERSIST-02 clean)
- `go test ./api/v1alpha1/... -run TestAggregatesGuard` — PASS (4/4)
- `go build ./...` — exit 0
- `go vet ./api/...` — exit 0
- CRD YAMLs contain: `enum: [strict, warn]`, `enum: [success, fail-exit-1, hang, exceed-output-paths]`, `enum: [Pending, Validated, CycleDetected, FileTouchMismatch]`
- No Apache header drift on modified `*_types.go` files

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing field] Added `DeclaredOutputPaths` to task samples and test fixture**
- **Found during:** Task 1
- **Issue:** Adding `DeclaredOutputPaths []string` with `MinItems=1` (no omitempty) makes it a required field. The existing 8 task sample YAMLs and the `task_controller_test.go` fixture did not include it — they would fail CRD admission validation.
- **Fix:** Added `declaredOutputPaths: [artifacts/{name}.json]` to all 8 task sample files; added `DeclaredOutputPaths: []string{"artifacts/test.json"}` to task controller test.
- **Files modified:** `config/samples/tide_v1alpha1_task_*.yaml` (8 files), `internal/controller/task_controller_test.go`
- **Commits:** 259c9d3

**2. [Rule 1 - Bug] Rewrote BudgetStatus comment to avoid false PERSIST-02 gate trip**
- **Found during:** Task 1 (verification)
- **Issue:** Original comment "No Schedule, Waves [], IndegreeMap, CachedDag, or DerivedDag tokens here" contained the forbidden denylist tokens verbatim — the grep-based `make verify-no-aggregates` gate matched the comment and reported a PERSIST-02 violation.
- **Fix:** Rewrote the comment to describe the intent (tally object, not schedule) without embedding the forbidden token names.
- **Files modified:** `api/v1alpha1/project_types.go`
- **Commits:** 259c9d3

## Known Stubs

None — all fields are properly typed schema additions. No hardcoded empty values, placeholder text, or unwired data sources. Downstream consumers (Plans 07, 09, 10, 11) reference these fields in their own plans.

## Threat Flags

| Flag | File | Description |
|------|------|-------------|
| threat_flag: input-validation | api/v1alpha1/task_types.go | New `DeclaredOutputPaths` field accepted at admission — Plan 06's harness (HARN-05) must enforce that subagent output matches this set; admission gate (MinItems=1) only prevents empty array, not path traversal. Path traversal validation is Plan 06's responsibility. |

## Task Commits

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Project.Spec/Status, Task.Spec, Plan.Status, shared_types additions + codegen | 259c9d3 | 17 files (api types + CRDs + samples + test) |
| 2 | Extend aggregates_guard_test.go to cover Phase 2 additions | f606a62 | 1 file |

## Self-Check: PASSED

| Item | Status |
|------|--------|
| api/v1alpha1/project_types.go | FOUND |
| api/v1alpha1/task_types.go | FOUND |
| api/v1alpha1/plan_types.go | FOUND |
| api/v1alpha1/shared_types.go | FOUND |
| api/v1alpha1/zz_generated.deepcopy.go | FOUND |
| api/v1alpha1/aggregates_guard_test.go | FOUND |
| config/crd/bases/tideproject.k8s_projects.yaml | FOUND |
| config/crd/bases/tideproject.k8s_tasks.yaml | FOUND |
| config/crd/bases/tideproject.k8s_plans.yaml | FOUND |
| Commit 259c9d3 (Task 1) | FOUND |
| Commit f606a62 (Task 2) | FOUND |
