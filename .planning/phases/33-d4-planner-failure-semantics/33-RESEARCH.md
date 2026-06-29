# Phase 33: D4 — Planner Failure Semantics - Research

**Researched:** 2026-06-29
**Domain:** Go + controller-runtime — phase/milestone controller succession gates
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 (phase + milestone ONLY):** Apply `isPlannerFailure` at the phase and milestone controllers only. Plan and project are deliberately excluded. Add an inline comment at the excluded levels (or in the shared helper's doc) documenting *why* (the `matched > 0` protection in D-02), so a future reader doesn't "complete the set" by reflex.

- **D-02 (why only phase + milestone):** The false-succeed exists because phase and milestone take a **direct `expected == 0 → patchXSucceeded` shortcut** that bypasses `gates.BoundaryDetected`, ignoring `out.ExitCode`. Plan and project succeed only via `gates.BoundaryDetected` (returns `matched > 0` — false on zero children). So a zero-child failed planner cannot drive plan/project to `Succeeded`.

- **D-03 (plan/project hung-Running is deferred):** A zero-child failed planner at plan/project leaves the level stuck `Running` — do NOT fix this in Phase 33. It is visible/recoverable, not a planning-DAG corruption.

- **D-04 (sizing-policy doc only):** Soften the chart's "≥ widest wave" comment in `charts/tide/values.yaml` to a per-workload tuning note; add a one-line note that the single-node default intentionally trades throughput for safety. No value change. No behavior change.

### Claude's Discretion

- **D-05 (failure vocabulary):** Add `ReasonPlannerFailed` constant in `api/v1alpha2/shared_types.go` alongside `ReasonWaveIntegrationFailed`. New `patchPhaseFailed`/`patchMilestoneFailed` helpers mirroring `patchPlanFailed`. Operator-facing message names exitCode + zero-children explicitly. Keep `Failed` condition permanent (recovery via `--retry-failed`, no auto-retry).

- **D-06 (shared helper shape):** Package-level `isPlannerFailure(out pkgdispatch.EnvelopeOut, envReadOK bool) bool` in a shared controller file (mirrors `depgraph.go`, `failure_halt.go`, `billing_halt.go`). Check: `envReadOK && out.ExitCode != 0 && out.ChildCount == 0`. Ordering is load-bearing (PLANFAIL-03): fail-check before `expected == 0 → patchXSucceeded`.

### Deferred Ideas (OUT OF SCOPE)

- Plan/project hung-Running on a zero-child failed planner — candidate for a future hardening pass if dogfood surfaces a real stall.
- Raising `plannerConcurrency` default beyond single-node safety — only if/when TIDE targets multi-node clusters.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PLANFAIL-01 | Phase whose planner exits nonzero with zero children is marked `Failed`, not `Succeeded` | Fix site confirmed at `phase_controller.go:637`; `patchPhaseFailed` helper to be added; `isPlannerFailure` guard inserts before that line |
| PLANFAIL-02 | Milestone whose planner exits nonzero with zero children is marked `Failed`, not `Succeeded` (shared helper) | Fix site confirmed at `milestone_controller.go:718`; `patchMilestoneFailed` helper to be added; same `isPlannerFailure` helper |
| PLANFAIL-03 | Genuine leaf (exitCode==0, childCount==0) still `Succeeds` — no regression; fail-check ordered before succeed-check | Confirmed: guard inserts BEFORE the `if expected == 0 { return r.patchXSucceeded }` branch inside `if envReadOK {` block; leaf path remains unchanged |
| PLANFAIL-04 | Failed parent is recoverable via existing `tide resume --retry-failed` — guard patches permanent `Failed`, not Go error return | Confirmed: `retryFailedLevels` in `cmd/tide/resume.go:184` already walks Milestone + Phase + Plan + Task and resets `Status.Phase=="Failed"` via status patch; no new code needed in resume |
</phase_requirements>

---

## Summary

Phase 33 is a narrow surgical fix at two sites in the controller package: the `handleJobCompletion` succession path in `phase_controller.go` and `milestone_controller.go`. Both share an identical structural defect — when a planner Job completes with `envReadOK=true` and `out.ChildCount==0`, the controllers call `patchXSucceeded` unconditionally, ignoring `out.ExitCode`. A failed planner with no children is misclassified as a genuine leaf and marked `Succeeded`, corrupting the planning DAG (the parent milestone or project sees a falsely-succeeded child and advances).

The fix inserts a shared `isPlannerFailure(out, envReadOK) bool` check — `envReadOK && out.ExitCode != 0 && out.ChildCount == 0` — before the `expected == 0` succeed branch at both sites. When the guard fires it calls new `patchPhaseFailed`/`patchMilestoneFailed` helpers (mirroring the existing `patchPlanFailed` at `plan_controller.go:887`) and returns without requeueing. Recovery is the existing `tide resume --retry-failed` verb (`cmd/tide/resume.go:184`), which already resets `Status.Phase=="Failed"` on Milestones and Phases.

The carried-in D3 debt (D-04) is a docs/comment-only change to `charts/tide/values.yaml`: softening the "≥ widest wave" guidance to a per-workload tuning note without changing the default of `4`.

**Primary recommendation:** Add `isPlannerFailure` in a new `planner_failure.go` shared helper file, add `ReasonPlannerFailed` to `api/v1alpha2/shared_types.go`, add `patchPhaseFailed` in `phase_controller.go` and `patchMilestoneFailed` in `milestone_controller.go`, insert the guard at both succession sites, add envtests for all four PLANFAIL requirements, and update the chart comment.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Planner failure classification | controller (phase/milestone) | — | The reconciler observes Job terminal state + envelope ExitCode; classification must happen where the evidence is read |
| Failed condition stamping | CRD `.status` only | — | v1.0.6 constraint: no new persistence surface; status patch is the correct K8s idiom |
| Recovery from Failed | CLI (`tide resume`) | — | Operator-driven; auto-retry would cause storm and contradicts the established recovery-verb posture |
| Shared guard logic | internal/controller package | — | Mirrors existing shared helpers (`failure_halt.go`, `billing_halt.go`, `depgraph.go`) |
| API vocabulary (ReasonPlannerFailed) | api/v1alpha2/shared_types.go | — | All Reason* constants live in shared_types.go; v1alpha2 is the served version (controllers import only v1alpha2) |
| Sizing-policy documentation | charts/tide/values.yaml | — | Comment-only; no binary change; chart is the FIXED contract |

---

## Standard Stack

No new dependencies. This phase uses only existing project libraries. [VERIFIED: direct codebase inspection]

| Library | Role | Already Present |
|---------|------|-----------------|
| `sigs.k8s.io/controller-runtime` | Reconciler, client, status patch | Yes |
| `k8s.io/apimachinery/pkg/api/meta` | `meta.SetStatusCondition` | Yes |
| `k8s.io/apimachinery/pkg/apis/meta/v1` | `metav1.Condition`, `metav1.Now()` | Yes |
| `github.com/jsquirrelz/tide/api/v1alpha2` | `ConditionFailed`, `ReasonPlannerFailed` (new) | Yes (constant to add) |
| `github.com/jsquirrelz/tide/pkg/dispatch` | `pkgdispatch.EnvelopeOut` | Yes |
| `github.com/onsi/ginkgo/v2` + `gomega` | Envtest specs | Yes |

**Installation:** No new packages. No `go.mod` changes.

---

## Package Legitimacy Audit

No external packages are introduced by this phase. [VERIFIED: direct codebase inspection]

**Packages removed due to slopcheck:** none
**Packages flagged as suspicious:** none

---

## Architecture Patterns

### System Architecture Diagram

```
planner Job completes (terminal)
         │
         ▼
handleJobCompletion (phase_controller.go or milestone_controller.go)
         │
         ├─ setBillingHaltIfNeeded (already runs on exitCode != 0, before the new guard)
         │
         ▼
if envReadOK {
    ┌─────────────────────────────────────────────────────────┐
    │ [NEW] isPlannerFailure(out, envReadOK)                   │
    │       envReadOK && out.ExitCode != 0 && out.ChildCount == 0 │
    │       → patchPhaseFailed / patchMilestoneFailed          │
    │         (Status.Phase=Failed, ConditionFailed=True)      │
    │         return ctrl.Result{}, nil   ← no requeue         │
    └─────────────────────────────────────────────────────────┘
         │ (guard NOT fired — exitCode==0 or childCount>0)
         ▼
    expected := out.ChildCount
    if expected == 0 {
        // Genuine leaf (exitCode==0, childCount==0) — Succeeds (PLANFAIL-03 non-regression)
        return r.patchXSucceeded(...)
    }
    // observed < expected → requeue; observed >= expected → BoundaryDetected → Succeed
}

Recovery path:
tide resume --retry-failed
    → retryFailedLevels (cmd/tide/resume.go:184)
    → walks Milestone + Phase + Plan + Task
    → resets Status.Phase="" + stamps ResumedByUser condition
    → controller re-reconciles and re-dispatches planner
```

### Recommended Project Structure

No new top-level directories. Changes are co-located with existing patterns:

```
api/v1alpha2/
└── shared_types.go        # add ReasonPlannerFailed constant (new const block)

internal/controller/
├── planner_failure.go     # NEW: isPlannerFailure shared helper (mirrors failure_halt.go)
├── phase_controller.go    # insert guard + add patchPhaseFailed helper
├── milestone_controller.go # insert guard + add patchMilestoneFailed helper
└── planner_failure_test.go # NEW: unit tests for isPlannerFailure (pure function, no envtest)

charts/tide/
└── values.yaml            # soften plannerConcurrency comment (D-04)
```

### Pattern 1: Shared Guard Helper (mirrors `failure_halt.go`)

**What:** A package-level boolean predicate in its own file, callable at both succession sites without importing controller-specific types.

**When to use:** Any time a condition check must apply symmetrically across two or more controllers.

**Example:** [VERIFIED: direct codebase inspection — mirrors billing_halt.go/failure_halt.go pattern]
```go
// planner_failure.go — shared planner-failure guard for phase and milestone controllers.
//
// Phase 33 D4: a planner Job that exits nonzero with zero children is marked
// Failed (not Succeeded) at both the phase and milestone levels. Plan and project
// are structurally protected by gates.BoundaryDetected (returns matched > 0, false
// on zero children) — they are deliberately excluded. See 33-CONTEXT.md D-02.
package controller

import pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"

// isPlannerFailure returns true when a completed planner Job exited nonzero with
// zero children — the "false-leaf" condition that would otherwise cause patchXSucceeded
// to corrupt the planning DAG by falsely advancing the parent.
//
// The check is envReadOK && ExitCode != 0 && ChildCount == 0.
//
//   - !envReadOK → envelope unavailable; caller handles separately (transient error path).
//   - ExitCode == 0 → planner succeeded; ChildCount==0 is a genuine leaf → Succeed.
//   - ExitCode != 0, ChildCount > 0 → planner failed but authored children;
//     children are already materializing — leave Running, let the reporter drain.
//     (This case is unusual but not the false-leaf scenario D4 targets.)
//   - ExitCode != 0, ChildCount == 0 → false-leaf; mark Failed.
//
// NOT called at plan or project level (see D-02 in 33-CONTEXT.md).
func isPlannerFailure(out pkgdispatch.EnvelopeOut, envReadOK bool) bool {
    return envReadOK && out.ExitCode != 0 && out.ChildCount == 0
}
```

### Pattern 2: patchXFailed Helper (mirrors `patchPlanFailed`)

**What:** Method on the reconciler struct that stamps `Status.Phase=Failed` + `ConditionFailed=True` via status subresource patch. Returns `ctrl.Result{}, nil` (no requeue). The `//nolint:unparam` comment is required because `ctrl.Result{}` is always empty — the return value exists solely so callers can `return r.patchPhaseFailed(...)` in the reconcile chain.

**Example:** [VERIFIED: direct codebase inspection of `plan_controller.go:887`]
```go
// patchPhaseFailed sets Phase.Status.Phase=Failed with a concrete operator message.
// Called from handleJobCompletion when isPlannerFailure detects a nonzero-exit,
// zero-child planner — prevents false-leaf succession from corrupting the planning DAG.
// Recovery: `tide resume --retry-failed` resets Status.Phase and re-dispatches.
//
//nolint:unparam // ctrl.Result kept so callers can `return r.patchPhaseFailed(...)` in the reconcile chain
func (r *PhaseReconciler) patchPhaseFailed(ctx context.Context, ph *tideprojectv1alpha2.Phase, reason, message string) (ctrl.Result, error) {
    patch := client.MergeFrom(ph.DeepCopy())
    ph.Status.Phase = "Failed"
    meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha2.ConditionFailed,
        Status:             metav1.ConditionTrue,
        Reason:             reason,
        Message:            message,
        LastTransitionTime: metav1.Now(),
    })
    if err := r.Status().Patch(ctx, ph, patch); err != nil {
        return ctrl.Result{}, err
    }
    return ctrl.Result{}, nil
}
```

`patchMilestoneFailed` is structurally identical with `*tideprojectv1alpha2.Milestone`.

### Pattern 3: Guard Insertion Site (both controllers)

**What:** The guard inserts inside `if envReadOK {`, immediately before `expected := out.ChildCount` / `if expected == 0`. Ordering is non-negotiable: fail-check first, succeed-check second.

**Example — phase_controller.go (insertion before line 636):** [VERIFIED: direct codebase inspection]
```go
if envReadOK {
    // D4 guard (Phase 33 PLANFAIL-01/03): detect false-leaf BEFORE the genuine-leaf
    // succeed branch. A planner that exits nonzero with zero children is Failed, not
    // Succeeded. exitCode==0, childCount==0 falls through to the genuine-leaf path below.
    // Plan and project are excluded (see isPlannerFailure doc + 33-CONTEXT.md D-02).
    if isPlannerFailure(out, envReadOK) {
        return r.patchPhaseFailed(ctx, ph,
            tideprojectv1alpha2.ReasonPlannerFailed,
            fmt.Sprintf("planner exited nonzero (exitCode=%d) with zero children; "+
                "marked Failed to prevent false succession", out.ExitCode))
    }
    // Option-C path: gate on out.ChildCount from tiny status.
    expected := out.ChildCount
    if expected == 0 {
        // Genuine leaf — planner authored no Plan children.
        ...
        return r.patchPhaseSucceeded(ctx, ph)
    }
    ...
}
```

### Anti-Patterns to Avoid

- **Returning a Go error instead of status-patching Failed:** would trigger controller-runtime retry loop; the established TIDE pattern (see `patchPlanFailed`, `failure_halt.go`) is status-patch + `return ctrl.Result{}, nil`.
- **Checking `out.ExitCode != 0` without also checking `envReadOK`:** if the envelope read failed, `out` is a zero-value struct (ExitCode==0); the guard is only valid when `envReadOK==true`.
- **Inserting the guard AFTER the `expected == 0` succeed branch:** the genuine-leaf path would fire first on failed planners with zero children, masking the bug.
- **Adding the constant to v1alpha1 only or both:** controllers import only v1alpha2 (confirmed by grep — no `tideprojectv1alpha2` alias resolves to v1alpha1 in any controller file). Add `ReasonPlannerFailed` to `api/v1alpha2/shared_types.go` only. The v1alpha1 package is a legacy schema definition, not the served API; its constants are not used by the controller package.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Status condition stamping | Custom condition list manipulation | `meta.SetStatusCondition` from `k8s.io/apimachinery/pkg/api/meta` | Handles deduplication, LastTransitionTime semantics, idempotency |
| Failed phase recovery | New `tide resume` subcommand or new CRD field | Existing `retryFailedLevels` in `cmd/tide/resume.go:184` | Already walks Milestone + Phase; PLANFAIL-04 needs no new code |
| Cross-level boolean predicate | Inline `if envReadOK && out.ExitCode != 0 && out.ChildCount == 0` at each site | `isPlannerFailure(out, envReadOK)` shared helper | DRY; single definition is the only source of truth for the D4 contract |

---

## Code Examples

### Verified: `out` struct fields — `pkgdispatch.EnvelopeOut`

[VERIFIED: `pkg/dispatch/envelope.go:164-224` direct read]

```go
type EnvelopeOut struct {
    APIVersion  string    `json:"apiVersion"`
    Kind        string    `json:"kind"`
    TaskUID     string    `json:"taskUID"`
    ExitCode    int       `json:"exitCode"`    // 0 = success, nonzero = failure
    Result      string    `json:"result"`
    Reason      string    `json:"reason"`      // structured failure code (e.g. "forced-failure")
    Usage       Usage     `json:"usage"`
    Artifacts   []string  `json:"artifacts"`
    CompletedAt time.Time `json:"completedAt"`
    ChildCRDs   []ChildCRDSpec `json:"childCRDs,omitempty"`
    ChildCount  int       `json:"childCount,omitempty"` // authored child CRDs count
    // ...
}
```

The fields relevant to the guard: `ExitCode int`, `ChildCount int`. `Reason string` is carried into the operator-facing message.

### Verified: `envReadOK` is set at `phase_controller.go:508-517`

[VERIFIED: direct codebase inspection]

```go
var out pkgdispatch.EnvelopeOut
envReadOK := false
// ... (read attempt)
    envReadOK = true   // set only on successful ReadOut
```

`envReadOK` is false when the envelope read fails (transient); zero-value `out` has `ExitCode==0` and `ChildCount==0`. The `envReadOK &&` prefix in the guard is therefore essential.

### Verified: `patchPlanFailed` full signature at `plan_controller.go:887`

[VERIFIED: direct codebase inspection]

```go
//nolint:unparam // ctrl.Result kept so callers can `return r.patchPlanFailed(...)` in the reconcile chain
func (r *PlanReconciler) patchPlanFailed(ctx context.Context, plan *tideprojectv1alpha2.Plan, reason, message string) (ctrl.Result, error)
```

Signature to mirror: `(ctx context.Context, <resource> *tideprojectv1alpha2.<Type>, reason, message string) (ctrl.Result, error)`.

### Verified: `ReasonPlannerFailed` does NOT yet exist

[VERIFIED: grep across entire codebase returned zero hits in production code]

The constant must be added as a new `const` block in `api/v1alpha2/shared_types.go` (analogous to the Phase 11 block at line 197):

```go
// Phase 33 condition + reason vocabulary — planner failure semantics (D4).
const (
    // ReasonPlannerFailed — phase or milestone planner exited nonzero with zero
    // children, preventing false-leaf succession. Level is marked terminal Failed;
    // run `tide resume --retry-failed` to reset and re-dispatch.
    ReasonPlannerFailed = "PlannerFailed"
)
```

**v1alpha1 question:** Controllers import only `api/v1alpha2` — confirmed by grep (`tideprojectv1alpha2` alias is the only import; no `v1alpha1` alias appears in any non-test controller file). Add `ReasonPlannerFailed` to `api/v1alpha2/shared_types.go` only. Do NOT add it to `api/v1alpha1/shared_types.go` (that package is the legacy schema, not the served API).

### Verified: `retryFailedLevels` already handles Milestone and Phase

[VERIFIED: `cmd/tide/resume.go:184-245` direct read]

```go
func retryFailedLevels(ctx context.Context, c client.Client, ns, projectName string, out io.Writer) error {
    // Milestone — walks msList.Items, resets Status.Phase="" when == "Failed"
    // Phase — walks phList.Items, resets Status.Phase="" when == "Failed"
    // Plan, Task — also walked
    // Uses client.MatchingLabels{"tideproject.k8s/project": projectName}
    // Stamps ConditionWaveOrLevelPaused{Status:False, Reason:ReasonResumedByUser}
}
```

PLANFAIL-04 needs zero new code in the resume verb — it needs only the Phase 33 guard to set `Status.Phase=Failed` correctly so the walker can find and reset it.

### Verified: chart `plannerConcurrency` comment (D-04 carried-in debt)

[VERIFIED: `charts/tide/values.yaml:79-88` direct read]

Current text (the part to soften):
```yaml
# plannerConcurrency caps concurrent in-flight planner Jobs globally across all planner
# levels (project/milestone/phase/plan). The D3 gate counts non-terminal planner Jobs
# via a cached client.List and parks new dispatches with RequeueAfter(10s) when the
# count meets or exceeds this cap. Default 4 is single-node-safe: each planner pod
# (subagent + credproxy sidecar) consumes ~300-500 MiB; 4 pods leave headroom for the
# executor pool + system pods on a 7.65 GiB single-node kind cluster (RQ-2). Must be
# sized at least as wide as the widest expected planning wave (e.g. a 6-phase milestone
# needs plannerConcurrency >= 6 to avoid serialising phase dispatch). Increase for
# multi-node clusters where memory constraints are relaxed.
plannerConcurrency: 4
```

The phrase "Must be sized at least as wide as the widest expected planning wave (e.g. a 6-phase milestone needs plannerConcurrency >= 6 to avoid serialising phase dispatch)" overstates the requirement — a cap below wave width serializes dispatch but does not deadlock (single-shot planner Jobs drain). The plan should replace this with a softer tuning note.

### Verified: `config.go` binary default for `plannerConcurrency`

[VERIFIED: `internal/config/config.go:117` direct read]

```go
if err := resolveField("plannerConcurrency", raw.PlannerConcurrency, 4, &out.PlannerConcurrency); err != nil {
```

The binary default is `4`, matching the chart. No change to this value.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `ReasonPlannerFailed` string value `"PlannerFailed"` is the right format (PascalCase, no spaces, no dots) | Standard Stack / Code Examples | Style inconsistency only; all existing Reason* constants follow PascalCase (ReasonWaveIntegrationFailed, ReasonCreditBalanceTooLow, etc.) — LOW risk |

**All other claims in this research are `[VERIFIED: direct codebase inspection]`.**

---

## Common Pitfalls

### Pitfall 1: Guard fires on failed envelope read (envReadOK=false)

**What goes wrong:** If `isPlannerFailure` checked only `out.ExitCode != 0 && out.ChildCount == 0` without `envReadOK`, a transient envelope read error (returns zero-value `out`) would trigger the guard falsely — ExitCode is 0 in a zero-value struct, so this specific failure wouldn't fire, but a future refactor that changes the zero-value could.
**Why it happens:** Envelope read errors return `EnvelopeOut{}` with all fields zero.
**How to avoid:** Always prefix with `envReadOK &&`. The helper signature takes `envReadOK bool` explicitly so callers can't forget it.
**Warning signs:** Tests that inject read errors should confirm `isPlannerFailure` returns `false`.

### Pitfall 2: Guard inserts after the `expected == 0` succeed branch

**What goes wrong:** PLANFAIL-03 regression — a genuine leaf (exitCode==0, childCount==0) that hits the guard after the succeed branch would never reach the succeed branch. But more critically, a failed planner would also never be caught — the succeed branch fires first.
**Why it happens:** Easy to accidentally place the guard after the `expected := out.ChildCount` line.
**How to avoid:** The guard must be the first `if` inside `if envReadOK {`, before `expected := out.ChildCount`.
**Warning signs:** Test PLANFAIL-03 (leaf-succeeds non-regression) fails.

### Pitfall 3: Adding `ReasonPlannerFailed` to v1alpha1

**What goes wrong:** No immediate breakage, but unnecessary — no controller code uses v1alpha1 constants. Creates a maintenance burden and may mislead a reader into thinking the constant is served to external clients via v1alpha1.
**Why it happens:** Reflex to "keep both files in sync."
**How to avoid:** Controllers import only `api/v1alpha2`. Verify with grep before adding to v1alpha1.

### Pitfall 4: Forgetting `//nolint:unparam` on the new helpers

**What goes wrong:** `golangci-lint` (`unparam` linter) flags the `ctrl.Result` return as always being `ctrl.Result{}`. The lint gate blocks CI.
**Why it happens:** `patchPlanFailed` already has this comment; new helpers must mirror it.
**How to avoid:** Copy the `//nolint:unparam` comment from `patchPlanFailed` to both new helpers.

### Pitfall 5: `setBillingHaltIfNeeded` ordering relative to the new guard

**What goes wrong:** If the new guard fires before `setBillingHaltIfNeeded`, a billing-classified failure would be marked `Failed` without the billing halt being set — the billing backstop would be silently skipped.
**Why it happens:** `setBillingHaltIfNeeded` already runs at `phase_controller.go:569` (before the `if envReadOK {` block begins at line 634). The new guard is inside the `if envReadOK {` block. The existing ordering is: billing backstop first, then the `if envReadOK {` succession block. The new guard inserts inside the `if envReadOK {` block — billing backstop has already run. No ordering issue.
**Warning signs:** Would only be a problem if someone moved the billing backstop inside the `if envReadOK {` block.

---

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2 + Gomega (envtest specs) + standard `testing` package (unit tests) |
| Config file | No dedicated config; test suite boots via `TestControllers` in `suite_test.go` |
| Quick run command | `go test ./internal/controller/... -run TestCheckIsPlannerFailure -count=1` |
| Full suite command | `go test ./internal/controller/... -count=1` (Layer A envtest, ~30-60s) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PLANFAIL-01 | Phase with exitCode=1, childCount=0 → `Status.Phase=Failed` + `ConditionFailed=True` | envtest (Ginkgo) | `go test ./internal/controller/... -run "PLANFAIL-01" -count=1` | ❌ Wave 0 |
| PLANFAIL-02 | Milestone with exitCode=1, childCount=0 → `Status.Phase=Failed` + `ConditionFailed=True` | envtest (Ginkgo) | `go test ./internal/controller/... -run "PLANFAIL-02" -count=1` | ❌ Wave 0 |
| PLANFAIL-03 | Phase and Milestone with exitCode=0, childCount=0 → `Status.Phase=Succeeded` (non-regression) | envtest (Ginkgo) | `go test ./internal/controller/... -run "PLANFAIL-03" -count=1` | ❌ Wave 0 |
| PLANFAIL-04 | Failed Phase/Milestone reset by `resumeRun(retryFailed=true)` | unit (fake client) | `go test ./cmd/tide/... -run "TestResumePlannerFailed" -count=1` | ❌ Wave 0 |
| isPlannerFailure helper | Pure function unit test (all three inputs: envReadOK=false, exitCode=0, exitCode=1) | unit (stdlib) | `go test ./internal/controller/... -run "TestIsPlannerFailure" -count=1` | ❌ Wave 0 |

### Envtest Injection Pattern

**How to inject exitCode=1, childCount=0 in an envtest:**

Use the existing `mapEnvReader` + `makeFakeJobTerminal` pattern from `phase_controller_test.go` (Test 5 shape):

```go
// 1. Set envelope in fake reader BEFORE making Job terminal
envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
    TaskUID:    string(ph.UID),
    ExitCode:   1,            // nonzero — planner failed
    Reason:     "forced-failure",
    ChildCount: 0,            // no children authored
})
// 2. Mark the planner Job as succeeded at the Job level
//    (Job itself succeeded — K8s Job; the *planner* failed internally)
jobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)
Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())
// 3. Reconcile
_, err := r.Reconcile(ctx, reconcile.Request{...})
// 4. Assert Phase.Status.Phase == "Failed"
var got tideprojectv1alpha2.Phase
Expect(mgrClient.Get(ctx, ..., &got)).To(Succeed())
Expect(got.Status.Phase).To(Equal("Failed"))
// 5. Assert ConditionFailed=True with Reason=ReasonPlannerFailed
cond := meta.FindStatusCondition(got.Status.Conditions, tideprojectv1alpha2.ConditionFailed)
Expect(cond).NotTo(BeNil())
Expect(cond.Status).To(Equal(metav1.ConditionTrue))
Expect(cond.Reason).To(Equal(tideprojectv1alpha2.ReasonPlannerFailed))
```

**How to inject exitCode=0, childCount=0 (PLANFAIL-03 non-regression):**

```go
envReader.SetOut(string(ph.UID), pkgdispatch.EnvelopeOut{
    TaskUID:    string(ph.UID),
    ExitCode:   0,   // success — genuine leaf
    ChildCount: 0,
})
// Assert Phase.Status.Phase == "Succeeded"
```

**PLANFAIL-04 recovery — extend existing `TestResumeRetryFailedAllFourKinds`:**

The existing test at `cmd/tide/resume_test.go:218` already asserts that `retryFailedLevels` resets Failed Milestones and Phases. Extend it or add a new test that:
1. Creates a Phase with `Status.Phase="Failed"` + `ConditionFailed=True`/`Reason=ReasonPlannerFailed`
2. Calls `resumeRun(ctx, c, ns, project, true, &buf)`
3. Asserts `Status.Phase != "Failed"` and `ConditionWaveOrLevelPaused.Reason == ReasonResumedByUser`

**isPlannerFailure unit tests (new `planner_failure_test.go` file, stdlib):**

| Input | Expected |
|-------|----------|
| `(EnvelopeOut{ExitCode:1, ChildCount:0}, true)` | `true` |
| `(EnvelopeOut{ExitCode:0, ChildCount:0}, true)` | `false` (genuine leaf) |
| `(EnvelopeOut{ExitCode:1, ChildCount:0}, false)` | `false` (envelope unreadable) |
| `(EnvelopeOut{ExitCode:1, ChildCount:3}, true)` | `false` (children present) |

### Sampling Rate

- **Per task commit:** `go test ./internal/controller/... -run "PLANFAIL" -count=1`
- **Per wave merge:** `go test ./internal/controller/... ./cmd/tide/... -count=1` (full Layer A)
- **Phase gate:** `make test-int` green (Layer A + Layer B) before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `internal/controller/planner_failure.go` — `isPlannerFailure` helper + package doc
- [ ] `internal/controller/planner_failure_test.go` — pure unit tests for `isPlannerFailure` (4 cases above)
- [ ] Envtest specs in `phase_controller_test.go` or new `planner_failure_envtest_test.go` — PLANFAIL-01 + PLANFAIL-03 (phase level)
- [ ] Envtest specs in `milestone_controller_test.go` or same new file — PLANFAIL-02 + PLANFAIL-03 (milestone level)
- [ ] Additional test in `cmd/tide/resume_test.go` — PLANFAIL-04 with `ReasonPlannerFailed` condition

---

## Security Domain

This phase does not introduce new authentication, session management, cryptography, or network endpoints. The change is an in-process status patch; the threat surface is unchanged. Security domain section is not applicable.

---

## Environment Availability

This phase is a pure code/config change. No external services, databases, or CLIs beyond the existing Go toolchain are required.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | Build | ✓ | project-pinned | — |
| `make test-int` (envtest) | Phase gate | ✓ | existing | — |

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Direct `expected == 0 → patchXSucceeded` shortcut (ignores ExitCode) | `isPlannerFailure` guard before the succeed branch | Phase 33 (this phase) | Failed planners no longer corrupt the planning DAG by falsely advancing parents |
| Plan-level-only fail guard (`patchPlanFailed` added in Phase 30) | Guard now symmetric across phase + milestone | Phase 33 (this phase) | All three planner levels (phase/milestone/plan) are protected |

---

## Open Questions

1. **Should `isPlannerFailure` also handle `exitCode != 0, childCount > 0`?**
   - What we know: CONTEXT.md D-06 specifies the check as `envReadOK && out.ExitCode != 0 && out.ChildCount == 0` only.
   - What's unclear: A planner that fails but has non-zero ChildCount is a mixed state — children may be partially materialized. The current code leaves the level `Running` in that case (requeue loop).
   - Recommendation: Follow CONTEXT.md D-06 exactly. The non-zero-ChildCount case is distinct and not scoped to D4. Do not expand the guard.

2. **Should `patchPhaseFailed` / `patchMilestoneFailed` also clear `ConditionWaveOrLevelPaused`?**
   - What we know: `patchPlanFailed` does NOT clear `ConditionWaveOrLevelPaused` — it only stamps `ConditionFailed=True`. The `patchXSucceeded` helpers do clear it.
   - What's unclear: Whether an operator tailing conditions would benefit from seeing it explicitly cleared on failure.
   - Recommendation: Mirror `patchPlanFailed` exactly — stamp only `ConditionFailed=True`. Don't clear `ConditionWaveOrLevelPaused` (the condition may not even be set on a Failed planner). Keeping the helpers identical to the plan-level template reduces diff surface and test permutations.

---

## Sources

### Primary (HIGH confidence)

- `internal/controller/phase_controller.go` — lines 507-691 (envReadOK, insertion site at 637, patchPhaseSucceeded at 718)
- `internal/controller/milestone_controller.go` — lines 700-772 (same structure, insertion site at 718)
- `internal/controller/plan_controller.go` — lines 883-901 (`patchPlanFailed` template)
- `internal/controller/failure_halt.go` — shared helper pattern to mirror
- `internal/controller/billing_halt.go` — shared helper file pattern
- `internal/controller/depgraph.go` — package-level helper naming convention
- `api/v1alpha2/shared_types.go` — lines 197-204 (`ReasonWaveIntegrationFailed` block to insert after)
- `pkg/dispatch/envelope.go` — lines 164-224 (`EnvelopeOut` struct field confirmation)
- `cmd/tide/resume.go` — lines 184-245 (`retryFailedLevels` already handles Milestone + Phase)
- `cmd/tide/resume_test.go` — lines 218-283 (`TestResumeRetryFailedAllFourKinds` to extend)
- `charts/tide/values.yaml` — lines 79-88 (current `plannerConcurrency` comment text)
- `internal/config/config.go` — line 117 (binary default `plannerConcurrency=4`)
- `internal/controller/phase_controller_test.go` — Test 5 (envtest injection pattern with `mapEnvReader`, `makeFakeJobTerminal`)
- `internal/controller/suite_test.go` — lines 82-112 (`mapEnvReader` + `makeFakeJobTerminal` definitions)

### Secondary (MEDIUM confidence)

- None — all findings come from direct codebase inspection.

---

## Metadata

**Confidence breakdown:**
- Fix sites and insertion ordering: HIGH — direct line-by-line read, zero ambiguity
- Shared helper pattern: HIGH — mirrors existing `failure_halt.go`/`billing_halt.go` verbatim
- Reason constant and API version: HIGH — grep confirmed no existing `ReasonPlannerFailed`; v1alpha2-only confirmed by controller import grep
- Recovery path (PLANFAIL-04): HIGH — `retryFailedLevels` code read; existing test already covers Milestone + Phase reset
- Chart comment (D-04): HIGH — exact current text confirmed by direct read
- Test injection pattern: HIGH — copied from existing Test 5 in `phase_controller_test.go`

**Research date:** 2026-06-29
**Valid until:** 60 days (stable internal codebase, no external dependencies)
