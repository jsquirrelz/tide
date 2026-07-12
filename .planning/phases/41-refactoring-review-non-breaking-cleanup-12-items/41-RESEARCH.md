# Phase 41: Refactoring Review — Non-Breaking Cleanup - Research

**Researched:** 2026-07-11
**Domain:** Go/kubebuilder controller refactoring (internal cleanup, no schema/API changes)
**Confidence:** HIGH (every claim below is a live grep/read against current HEAD `a39baa6`, not the pre-Phase-40 seed)

## Summary

This phase has no external-library domain to research — it is a re-verification exercise. The seed (`.planning/todos/pending/2026-07-09-phase-41-refactoring-review.md`) was written against pre-Phase-40 source; Phase 40 (the v1alpha3 crank) landed 2026-07-11 and renamed the API package, moved line numbers, and added new controller code (import_controller.go, several new logging sites). CONTEXT.md's D-01 mandates a full re-verification before planning, which this document performs for all 11 live items (item 3 was already confirmed done and dropped by CONTEXT).

**Headline result: 10 of 11 items are confirmed still live with fresh file:line anchors; one item (6) has a materially better fix shape than the seed described** — a generic `reconcileWithRetry` helper already exists in the package (used at ~90 call sites across 12 test files) and item 6 reduces to deleting three duplicate drivers and repointing their callers at it, rather than authoring a new generic. A second finding not in the seed or CONTEXT: **the four dispatch-holds gate chains (item 7) do not actually share one order today** — milestone/phase/plan agree with each other, but task_controller.go's internal ordering of the Import-pending check differs. This is a real pre-existing inconsistency the planner must explicitly decide how to handle (see Pitfall 1).

**Primary recommendation:** Plan all 11 items using the file:line anchors in this document, not the seed's. Sequence per CONTEXT D-07 (2→5→6→4→1, then 7→8→10, with 9/11/12 as focused fixes). For item 6, target `reconcileWithRetry` (already shared) instead of writing a new generic. For item 7, make the ordering-divergence between task-tier and planner-tier an explicit plan decision, not an assumed detail.

## Architectural Responsibility Map

All 12 items live entirely inside a single K8s controller-manager binary — there is no multi-tier split to map (no browser, SSR, or CDN tier in this project). One tier table for completeness:

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Status.Phase constants (item 1) | API / CRD types (`api/v1alpha3`) | Controller / CLI / Dashboard consumers | Constants must live where the type is defined; consumers import them |
| Condition-check helpers (items 2, 9) | Controller (`internal/controller`) | — | Pure reconciler-internal logic, no CRD schema change |
| Dead code removal (item 4) | Controller + `cmd/manager` wiring | — | Struct fields wired in main.go must be removed in lockstep |
| Comment fixes (item 5) | Controller + `internal/subagent` | — | Comment-only, no behavior surface |
| Test helpers (item 6) | Test tier (`internal/controller/*_test.go`) | — | Envtest driver consolidation, zero production-code change |
| Dispatch-holds gate chain (item 7) | Controller (dispatch_helpers.go + 4 call sites) | — | Project-scoped holds are cross-cutting; belongs in the shared dispatch helper file |
| PlannerDeps carrier (item 8) | Controller structs + `cmd/manager/main.go` | — | Wiring-pattern change, mirrors existing `TaskReconcilerDeps` |
| Magic-literal centralization (item 11) | Controller + `internal/owner` | `cmd/tide` (CLI flag defaults) | Label-key constants belong next to `owner.LabelProject`; PVC name belongs on the reconciler that already has `SharedPVCName` |
| Log-style policy (item 12) | Documentation (`AGENTS.md`) only | — | D-05 explicitly avoids a controller-tier change |

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01: Every seed file:line is a hint, not an anchor.** Re-verify against current HEAD by symbol/grep before planning (this document is that re-verification).
- **D-02: All seed references to `api/v1alpha2` now mean `api/v1alpha3`.**
- **D-03 (item 1):** Constants live in `api/v1alpha3` per-kind types files, following the existing Project pattern (`project_types.go:463+`). Field type stays `string` — **NO** `+kubebuilder:validation:Enum` this phase (that's a CRD schema change; deferred to a future v1alpha4 crank).
- **D-04 (item 9):** `True == parent unresolved` (matches the type name and Task's existing usage). Fix `surfaceParentRefUnresolved` in milestone/phase controllers; clear to `False/ParentResolved` once the parent appears. Sweep every consumer and update tests in the same commit — document as an observable status-semantics change in the commit body.
- **D-05 (item 12):** Amend AGENTS.md, do not churn the code. Log text is load-bearing (test greps + CLAUDE.md runtime-gate verification protocols). Codify lowercase-initial in AGENTS.md's logging section as a quick win; zero log-message edits.
- **D-06:** No silent scope expansion from Phase 40's 40-REVIEW.md (0 Critical/6 Warning/10 Info) — those route via `/gsd:code-review 40 --fix` or explicit user fold, NOT this phase. Exception: item 5's mojibake fix in dispatch_helpers.go naturally overlaps a REVIEW Info finding — resolve both in one edit if the executor lands there anyway.
- **D-07 (sequencing):** 2 → 5 → 6 → 4 → 1 (quick wins, independently shippable), then structural 7 → 8 → 10, with 9 + 11 as focused correctness fixes and 12 landing as the AGENTS.md-amendment quick win. Item 7 migrates ONE controller per plan/commit — gate ordering is semantically load-bearing (slot leaks, park-before-acquire); preserve exact order + requeue values (5s/30s) **per controller** (see Pitfall 1 — this document found the "exact order" is not actually identical across all four sites today).
- **D-08:** Requirements minted at plan time (REFAC-xx pattern, mirroring Phase 40's CRANK-xx), one per item or per coherent item group.
- **D-09:** Honor the seed's "Do NOT refactor yet" list, except its `api/v1alpha1` entry (Phase 40 resolved — packages deleted, confirmed: `ls api/` shows only `v1alpha3`). Generated files, `//nolint:gocyclo` flat state machines, `ctrl.Result{Requeue: true}` sites (confirmed still 14, unchanged), webhook-test placement, and `charts/tide/values.yaml` all stay untouched.

### Claude's Discretion

- Exact plan/wave grouping of the 11 items (respecting D-07 sequencing).
- Item 6: interface param vs generics for `reconcileN` — but see this research's finding: the destination is an *existing* helper (`reconcileWithRetry`), not a new one, which narrows this discretion to "how do the 3 duplicate drivers get removed," not "what shape does the new generic take."
- Item 11's constant placement details (label keys → `internal/owner` next to `LabelProject`; PVC name plumbed from the reconciler field).

### Deferred Ideas (OUT OF SCOPE)

- `+kubebuilder:validation:Enum` on Status.Phase fields — CRD schema change, rides a future v1alpha4 crank.
- The 6 × 40-REVIEW.md WR findings — `/gsd:code-review 40 --fix` or explicit user fold.
- `/gsd:secure-phase 40` — security enforcement gate, still outstanding, not this phase.
- controller-runtime bump folding the 14 deprecated `ctrl.Result{Requeue: true}` sites.

## Phase Requirements

No requirement IDs were minted yet at research time (CONTEXT D-08: minted at plan time, REFAC-xx pattern). The table below maps the 11 seed items (candidate REFAC-xx requirements) to what this research confirms about each, for the planner to mint IDs against.

| Candidate ID (item) | Description | Research Support |
|---|---|---|
| item 1 | Typed Status.Phase constants for Milestone/Phase/Plan/Task/Wave | Confirmed: only Project has constants today (`project_types.go:463-485`); other 5 kinds are raw `string` fields. See "Item 1" below for literal-site counts. |
| item 2 | Replace hand-rolled condition loops with `meta.IsStatusConditionTrue` | Confirmed unchanged at `billing_halt.go:78-89`, `failure_halt.go:56-67` + `93-98`, `budget_blocked.go:55-66`. |
| item 4 | Delete dead code / dead struct fields | Confirmed at `task_controller.go:1433,1451` (nolint:unused), `SubagentImage` dead field × 5 controllers + wired 8× in main.go, `WaveReconciler.PlannerPool/ExecutorPool` wired but unread. |
| item 5 | Fix mojibake in comments | Confirmed: 13 lines `dispatch_helpers.go`, 9 lines `subagent.go` (exact `â` byte sequences). |
| item 6 | Test-helper unification (`isConflict` + 3 reconcile*N drivers) | Confirmed 3 duplicate drivers + a 4th (`reconcileWithRetry`) already shared across 12 files / ~90 call sites — the real fix target. |
| item 7 | Extract shared dispatch-holds gate chain | Confirmed at all 4 sites with exact line ranges; **found an ordering divergence between task-tier and planner-tier not in the seed** (Pitfall 1). |
| item 8 | Consolidate planner-reconciler deps into `PlannerDeps` carrier | Confirmed: 4 structs (`Milestone/Phase/Plan/Project`Reconciler) each declare ~9 identical dispatch-tier fields, wired in `main.go:416-527`. |
| item 9 | Normalize `ConditionParentUnresolved` polarity | Confirmed: `task_controller.go:344-355` sets True=unresolved; `milestone_controller.go:971-987` + `phase_controller.go:889-905` set False=unresolved. No dashboard consumer found (contrary to CONTEXT's caution to sweep `cmd/dashboard` — it's clean). |
| item 10 | Extract approve-consume + patch* status helpers | Confirmed: 15 `patch*` funcs (not exactly 16 — see breakdown), 4 `countChild*` copies, 2+2 approve-consume copies in milestone/phase controllers. |
| item 11 | Centralize repeated magic literals | Confirmed and **worse than seed described** — even `ProjectReconciler`'s own dispatch site (`project_controller.go:1763`) ignores its own `sharedPVCName()` accessor. |
| item 12 | Log-style policy (AGENTS.md amendment) | Confirmed: 88 lowercase-initial `logger.Info/Error` sites today (up from the seed's 47 — Phase 40 added new controllers), 0 uppercase — still fully internally consistent. AGENTS.md's conflicting guidance is at lines 213-230. |

## Standard Stack

Not applicable in the conventional sense — no new libraries are introduced. The "stack" for this phase is the existing toolchain, unchanged:

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `k8s.io/apimachinery/pkg/api/meta` | v0.36.1 (`go.mod`) | `meta.IsStatusConditionTrue`, `meta.SetStatusCondition`, `meta.FindStatusCondition` | Already imported and used elsewhere in the codebase (`gates.CheckRejected`, `budget.IsBypassed` etc. use the same package) — item 2's whole point is finishing this adoption |
| `k8s.io/apimachinery/pkg/api/errors` (aliased `apierrors`) | v0.36.1 | `apierrors.IsConflict(err)` | Already imported in several `*_controller.go` files (verify per-file before adding) — item 6's fix target |

**No installation step** — this phase touches zero `go.mod` entries. No Package Legitimacy Audit is required (no new external packages).

**Version verification:** `go.mod` confirms `go 1.26.0`, `sigs.k8s.io/controller-runtime v0.24.1`, `k8s.io/api v0.36.1`, `github.com/onsi/ginkgo/v2 v2.28.3`, `github.com/onsi/gomega v1.40.0` — matches CLAUDE.md's pinned Technology Stack table, no drift.

## Package Legitimacy Audit

Not applicable — Phase 41 installs no new packages (pure internal refactor of existing code using already-vendored `apimachinery` functions). Skipping the slopcheck/registry-verification gate per the protocol's own scope ("whenever this phase installs external packages").

## Architecture Patterns

### System Architecture Diagram

Phase 41 does not change data flow — it changes *how the same flow is expressed in code*. The diagram below shows where each item's change lands in the existing dispatch-entry request path (all four planner-tier controllers + the Task/executor-tier controller follow this shape):

```
Reconcile(ctx, req) called by controller-runtime on watch/requeue event
        │
        ▼
Fetch CR (Get) ──► NotFound? ──► IgnoreNotFound, done
        │
        ▼
Finalizer / owner-ref bookkeeping (unchanged this phase)
        │
        ▼
┌───────────────────────────────────────────────────────────┐
│ DISPATCH-ENTRY GATE CHAIN (item 7 extraction target)       │
│                                                             │
│  gates.CheckRejected(project) ──true──► patch*Rejected      │
│         │ false                                            │
│         ▼                                                   │
│  checkParentApproval (milestone/phase/plan only)            │
│         │ not held                                          │
│         ▼                                                   │
│  ImportComplete pending? ──true──► RequeueAfter 5s          │
│  (position differs: LAST in milestone/phase/plan,           │
│   EARLY in task — Pitfall 1)                                │
│         │ false                                             │
│         ▼                                                   │
│  checkBillingHalt (item 2 target) ──true──► RequeueAfter 30s│
│         ▼                                                    │
│  checkFailureHalt (item 2 target) ──true──► RequeueAfter 30s│
│         ▼                                                    │
│  checkBudgetBlocked && !IsBypassed (item 2 target) ──true──► │
│                                          RequeueAfter 30s     │
│         ▼ (task only)                                        │
│  reservation-headroom check ──true──► RequeueAfter 30s       │
└───────────────────────────────────────────────────────────┘
        │ all clear
        ▼
Pool.Acquire (slot) ──► createDispatchJob / ensureJob (item 4: ensureJob is
        │                DEAD, never called — createDispatchJob is the live path)
        ▼
Job created, Status.Phase → Running
        │
        ▼
On next reconcile: Job terminal? ──► handleJobCompletion
        │                                  │
        │                          ┌───────┴────────┐
        │                          ▼                 ▼
        │                   patch*Succeeded    patch*Failed
        │                   (item 10 target:   (item 10 target)
        │                    approve-consume
        │                    duplicated 3x)
        ▼
Children materialized from EnvelopeOut (MaterializeChildCRDs)
```

### Recommended Project Structure

No structural (directory) changes — every item edits files in place. The one new "location" is inside an existing file:

```
internal/controller/
├── dispatch_helpers.go     # item 7's new checkDispatchHolds() lands here (existing
│                            # home for shared dispatch logic — already houses
│                            # checkParentApproval, ResolveProvider, resolveImage)
├── milestone_controller.go # item 7/8/9/10 call-site migrations
├── phase_controller.go     # item 7/8/9/10 call-site migrations
├── plan_controller.go      # item 7/8/10 call-site migrations
├── task_controller.go      # item 4 (dead code removal), item 7 (distinct order — Pitfall 1), item 9 (already correct polarity)
├── billing_halt.go         # item 2
├── failure_halt.go         # item 2
├── budget_blocked.go       # item 2
├── milestone_controller_test.go  # item 6: reconcileWithRetry ALREADY lives here (line 45)
├── task_controller_test.go       # item 6: reconcileN (line 83) + isConflict (line 107) to be deleted
├── plan_controller_test.go       # item 6: reconcilePlanN (line 153) to be deleted
└── wave_controller_test.go       # item 6: reconcileWaveN (line 146) to be deleted

internal/owner/
└── label.go                 # item 11: new label-key constants land next to LabelProject (line 33)

api/v1alpha3/
├── project_types.go         # item 1: existing pattern to extend (lines 463-485)
├── milestone_types.go       # item 1: add constants (currently zero — just `Phase string`)
├── phase_types.go           # item 1: add constants
├── plan_types.go            # item 1: add constants
├── task_types.go            # item 1: add constants
├── wave_types.go            # item 1: add constants
└── shared_types.go          # item 9: ConditionParentUnresolved / Reason* already defined here (lines 193-205) — no schema change needed, just consumer fix

cmd/manager/main.go           # item 4 (drop dead SubagentImage/pool wiring), item 8
                               # (PlannerDeps carrier collapses lines 416-527)

AGENTS.md                     # item 12: amend Logging section (lines 213-230)
```

### Pattern 1: The dispatch-tier "Deps carrier" (item 8's model already exists)

**What:** `TaskReconciler` already consolidates its 9 dispatch-tier fields into a `TaskReconcilerDeps` struct (`task_controller.go:90-122`), leaving pool fields (`ExecutorPool`, `WatchNamespace`) as direct reconciler fields because they're "concurrency limiters, not dispatch-tier deps" (per the struct's own doc comment).

**When to use:** Item 8 extends the identical shape to the four planner reconcilers (Milestone/Phase/Plan/Project), which today each redeclare the same ~9 fields directly on the struct instead of behind a `Deps` field.

**Example:**
```go
// Source: internal/controller/task_controller.go:90-122 (current code, HEAD a39baa6)
type TaskReconcilerDeps struct {
	Dispatcher     dispatch.Dispatcher
	Budget         *budget.Store
	Defaults       budget.Limits
	SigningKey     []byte
	CredproxyImage string
	SubagentImage  string // dead since Phase 13, retained for legacy test wiring
	EnvReader      podjob.EnvelopeReader
	Recorder       record.EventRecorder
	HelmProviderDefaults ProviderDefaults
	Reservations         *budget.ReservationStore
	ReserveEstimateCents int64
	PricingOverridesJSON string
}
```
The wiring test that locks this shape already exists and item 8 must extend it:
```go
// Source: cmd/manager/wiring_test.go:88-92 (current code)
{
	name: "Task.Deps.Dispatcher",
	nilFn: func() bool {
		return (&controller.TaskReconciler{Deps: controller.TaskReconcilerDeps{Dispatcher: dispatcher}}).Deps.Dispatcher == nil
	},
	message: "TaskReconciler.Deps.Dispatcher must be non-nil (Phase 04.1 P3.2 — dispatch-tier deps now carried in Deps)",
},
```

### Pattern 2: Test-driver unification target already exists (item 6's real shape)

**What:** A generic-by-function-value driver, `reconcileWithRetry`, is already defined once (`milestone_controller_test.go:42-59`) and used by `Expect(reconcileWithRetry(r.Reconcile, name, n)).To(Succeed())` across **12 test files and ~60 call sites** — including `boundary_push_test.go`, `phase_gates_test.go`, `plan_gates_test.go`, `planner_job_absent_test.go`, `file_touch_gate_test.go`, and both `phase_controller_test.go`/`plan_controller_test.go`. It takes `r.Reconcile` (a bound method value matching `func(context.Context, reconcile.Request) (ctrl.Result, error)`), so it already works uniformly for Milestone/Phase/Plan/Project reconcilers.

**When to use:** Item 6's real fix is not "write a new generic reconcileN" — it's (a) fix `reconcileWithRetry`'s conflict-matching to use `apierrors.IsConflict`, then (b) delete the three still-duplicated, receiver-typed drivers (`reconcileN` for Task, `reconcilePlanN` for Plan, `reconcileWaveN` for Wave) and repoint their ~86 combined call sites (70 + 7 + 9) at `reconcileWithRetry(r.Reconcile, name, n)`.

**Example:**
```go
// Source: internal/controller/milestone_controller_test.go:42-59 (current code — the shared driver)
type reconcilerFunc func(context.Context, reconcile.Request) (ctrl.Result, error)

func reconcileWithRetry(r reconcilerFunc, name types.NamespacedName, n int) error {
	for range n {
		for range 5 {
			_, err := r(context.Background(), reconcile.Request{NamespacedName: name})
			if err == nil {
				break
			}
			if strings.Contains(err.Error(), "the object has been modified") || strings.Contains(err.Error(), "Conflict") {
				continue
			}
			return err
		}
	}
	return nil
}
```
Compare the still-duplicated `reconcileN` (task_controller_test.go:83-112) — same loop shape, receiver-typed to `*TaskReconciler`, string-matches only `"the object has been modified"` (narrower than `reconcileWithRetry`'s match, another small inconsistency this item should resolve in the one surviving function).

### Anti-Patterns to Avoid

- **Writing a brand-new generic reconcile driver for item 6.** One already exists and is package-standard; adding a second generic alongside it (rather than deleting the 3 outliers) would leave 2 patterns instead of 1.
- **Assuming item 7's four call sites share one order.** They don't (see Pitfall 1) — a helper written to match milestone/phase/plan's order and dropped unmodified into task_controller.go silently changes task-tier dispatch behavior when Import-pending coincides with a Billing/Failure/Budget hold.
- **Editing `charts/tide/values.yaml`.** FIXED contract per CLAUDE.md — no item in this phase should touch it (none currently need to).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| "Is condition X true on this object" | A manual `for range Conditions` loop (items 2's current shape) | `meta.IsStatusConditionTrue(conditions, type)` | Already vendored (`k8s.io/apimachinery/pkg/api/meta`), used elsewhere in the codebase, handles the nil-conditions-slice case correctly |
| "Is this a 409 Conflict error" | String-matching `err.Error()` for `"the object has been modified"` (items 6's current shape, in 2 of the 4 drivers) | `apierrors.IsConflict(err)` | The K8s API server's actual error type carries a structured `Reason: metav1.StatusReasonConflict` — string matching breaks if the server's message text ever changes wording |
| "Retry a Reconcile call N times in envtest" | A fourth per-reconciler-type driver | The existing `reconcileWithRetry` + `reconcilerFunc` (already package-standard, 12 files depend on it) | One driver, one conflict-detection bug to fix, not four |

**Key insight:** Every item in this phase is "finish an extraction the codebase already started," not "introduce a new pattern." The `dispatch_helpers.go` file, the `TaskReconcilerDeps` struct, and the `reconcileWithRetry` test driver are all pre-existing precedents the 11 items extend — there is no green-field design decision here, only consistent application of what's already idiomatic in this codebase.

## Common Pitfalls

### Pitfall 1: The four dispatch-holds gate chains do NOT share one order today (item 7)

**What goes wrong:** A shared `checkDispatchHolds` helper written to one fixed order will silently change dispatch-entry behavior at whichever of the four call sites doesn't match that order.

**Why it happens:** Verified by reading all four controllers' gate blocks at current HEAD:
- **Milestone** (`milestone_controller.go:343-393`): Reject → Billing(30s) → Failure(30s) → Budget(30s) → Import(5s, LAST)
- **Phase** (`phase_controller.go:330-386`): ParentApproval(5s) → { Reject → Billing(30s) → Failure(30s) → Budget(30s) → Import(5s, LAST) }
- **Plan** (`plan_controller.go:343-400`): ParentApproval(5s) → { Reject → Billing(30s) → Failure(30s) → Budget(30s) → Import(5s, LAST) } — identical shape to Phase
- **Task** (`task_controller.go:366-458`): Reject → ParentApproval(5s) → **Import(5s, SECOND — not last)** → Billing(30s) → Failure(30s) → Budget(30s) → reservation-headroom(30s, task-only)

Milestone/Phase/Plan (the three planner-tier reconcilers) agree with each other byte-for-byte on the *project-scoped* block's internal order (`Reject → Billing → Failure → Budget → Import`). Task (the executor-tier reconciler) checks Import right after ParentApproval — before Billing/Failure/Budget, not after. If a Project simultaneously has both `ConditionImportComplete` unset AND `BillingHalt=True`, a planner-tier reconciler reports "dispatch held: project billing halt" (30s requeue) while the Task reconciler reports "import pending; holding task dispatch" (5s requeue) for the exact same underlying Project state. This is a genuine (if narrow) existing inconsistency, not something Phase 41 introduces — but a naively-shared helper will either preserve it awkwardly (two helpers, or a parameterized order) or silently resolve it (a real behavior change requiring the same D-04-style "document + sweep tests" treatment CONTEXT already applies to item 9).

**How to avoid:** The planner must pick one of: (a) one `checkDispatchHolds` for the 3 planner-tier sites only (matching their shared order) and leave Task's gate chain untouched/inline (it already differs by having the extra headroom check anyway, so it's not a drop-in candidate for the same helper without a signature that also returns "held for reservation headroom"); or (b) parameterize the helper's Import-check position; or (c) explicitly normalize Task's order to match and document it as an intentional, tested behavior change (mirroring D-04's treatment of item 9). Do not assume "preserve exact order" from CONTEXT D-07 means the four sites already agree — they don't, on this one axis.

**Warning signs:** Any new envtest for `checkDispatchHolds` that asserts a fixed precedence between Import and Billing/Failure/Budget will pass for 3 controllers and needs a distinct assertion (or an explicit behavior-change note) for Task.

### Pitfall 2: item 8's "PlannerDeps" name collides conceptually with Project, which also has a Dispatcher field but different semantics

**What goes wrong:** `ProjectReconciler` has the same ~9 dispatch-tier fields as Milestone/Phase/Plan (confirmed at `project_controller.go:184-218`) — including `Dispatcher`, `EnvReader`, `SigningKey`, `CredproxyImage`, `SubagentImage`, `HelmProviderDefaults`, `ReporterImage`, `PricingOverridesJSON` — so it is a 4th candidate for the same carrier, not just Milestone/Phase/Plan (three). The seed's item 8 says "the four planner reconcilers" but only lists Milestone/Phase/Plan's file paths explicitly in its Files line; Project must be included or the extraction is incomplete and leaves exactly the class of "forgotten wiring" bug (cascade-8, the never-assigned Dispatcher) the item exists to prevent.

**How to avoid:** Confirm the plan's scope for item 8 explicitly includes `project_controller.go`'s struct (verified fields at lines 179-224) alongside milestone/phase/plan.

**Warning signs:** If the executor greps only "MilestoneReconciler\|PhaseReconciler\|PlanReconciler" for item 8's field removal, `ProjectReconciler`'s copy of the same fields will be missed.

### Pitfall 3: item 11's `sharedPVCName()` accessor exists but is already inconsistently used — even by its own owner

**What goes wrong:** `ProjectReconciler.sharedPVCName()` (`project_controller.go:2158-2164`) is the correct accessor (falls back to `defaultSharedPVCName = "tide-projects"` when `r.SharedPVCName` is unset) and IS used at 4 call sites in `project_controller.go` (lines 342, 622, 939, 1843) for Init/clone/push-mode Jobs — but the *planner-dispatch* Job spec in the same file, `project_controller.go:1763`, hardcodes the literal `"tide-projects"` directly instead of calling `r.sharedPVCName()`. The same literal is hardcoded in all 4 sibling planner/task dispatch sites too (`milestone_controller.go:512`, `phase_controller.go:471`, `plan_controller.go:506`, `task_controller.go:802,1477`). This means `--workspaces-pvc-name` is silently non-configurable for every dispatch Job (planner AND executor), even though `ProjectReconciler` demonstrably has the machinery to make it configurable.

**How to avoid:** Item 11's fix must plumb the PVC name from each reconciler's own field (`MilestoneReconciler`/`PhaseReconciler`/`PlanReconciler`/`TaskReconciler` currently have no `SharedPVCName` field at all — only `ProjectReconciler` does) — this is a slightly bigger lift than "replace a literal with an existing constant"; it requires adding the field to 4 more reconcilers and wiring it in `main.go` (same `sharedPVCName` variable already computed there at line 219, just not passed to these 4 reconcilers today). Confirm this during planning rather than treating it as a one-line literal swap.

**Warning signs:** `rg '"tide-projects"' internal/controller | grep -v _test` should return **zero** hits when item 11 is done (today it returns 9, including `project_controller.go:1763` — the pre-existing accessor's own package).

### Pitfall 4: item 1's literal counts are far higher than the seed's rough "~90 sites"

**What goes wrong:** Fresh grep at current HEAD across `internal/controller`, `cmd/tide`, `cmd/dashboard` for the six raw Phase-value literals (`"Succeeded"`, `"Failed"`, `"Running"`, `"AwaitingApproval"`, `"Pending"`, `"ZeroMembers"`) returns **117 non-test-file sites** (26+38+24+18+9+2) and **438 total sites including tests**. A mechanical sweep at this scale is genuinely a "Medium" not "Low" risk item if a single site is missed — `go vet`/`go build` will NOT catch a raw string literal that's merely comparing wrong, only a genuine typo that fails to match anything at all silently succeeds/fails wrong at runtime, which is exactly the failure mode item 1 exists to close.

**How to avoid:** Prefer a scripted `sed`/`gofmt`-verified mechanical replacement per-kind (Milestone/Phase/Plan/Task/Wave, one commit each per D-07's "each independently shippable" framing) over hand-editing, and run `go build ./...` after every kind, not just at the end.

**Warning signs:** `rg '"Succeeded"' internal/controller cmd` (the seed's own Verify line) returning any non-test, non-constant-definition hit after item 1 lands.

## Code Examples

### Item 2 target shape (current → target)

```go
// Source: internal/controller/billing_halt.go:78-89 (current code)
func checkBillingHalt(project *tideprojectv1alpha3.Project) bool {
	if project == nil {
		return false
	}
	for _, c := range project.Status.Conditions {
		if c.Type == tideprojectv1alpha3.ConditionBillingHalt &&
			c.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}
```
Target (per seed's shape, keep the nil-safe wrapper, replace only the loop body):
```go
func checkBillingHalt(project *tideprojectv1alpha3.Project) bool {
	if project == nil {
		return false
	}
	return meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionBillingHalt)
}
```
Identical transform applies to `failure_halt.go:56-67` and `budget_blocked.go:55-66`. The second loop in `failure_halt.go:93-98` (the "already halted, idempotent no-op" guard inside `setFailureHaltIfNeeded`) is the same pattern and should convert too.

### Item 9 target shape (current → target)

```go
// Source: internal/controller/milestone_controller.go:971-987 (current code — WRONG polarity)
func (r *MilestoneReconciler) surfaceParentRefUnresolved(ctx context.Context, ms *tideprojectv1alpha3.Milestone, parentKind, parentRef string) {
	// ...
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:    tideprojectv1alpha3.ConditionParentUnresolved,
		Status:  metav1.ConditionFalse, // WRONG per D-04 — should be True (parent IS unresolved)
		Reason:  tideprojectv1alpha3.ReasonParentRefNotFound,
		Message: msg,
	})
	// ...
}
```
Compare Task's already-correct usage:
```go
// Source: internal/controller/task_controller.go:344-351 (current code — CORRECT polarity, D-04's model)
meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
	Type:    tideprojectv1alpha3.ConditionParentUnresolved,
	Status:  metav1.ConditionTrue, // True == parent unresolved
	Reason:  tideprojectv1alpha3.ReasonNoProjectLabel,
	Message: "No Project found via label or owner-ref chain; awaiting label stamp by PlanReconciler",
})
```
Fixing milestone/phase requires also adding the "clear to False once resolved" half — Task's flow naturally clears the condition on next successful `resolveProject`, but milestone/phase's `surfaceParentRefUnresolved` has no counterpart "resolved" call today; the item needs one added (D-04: "clear to False/ParentResolved once the parent appears").

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `api/v1alpha1` + `api/v1alpha2` coexisting | `api/v1alpha3` sole served+storage version | Phase 40, landed 2026-07-11 | Every file:line anchor in the pre-Phase-40 seed is stale; this document supersedes it |
| Duplicate `AddToScheme` call + stale v1alpha1-decoding comment | Single `tidev1alpha3.AddToScheme(scheme)` call, corrected comment | Phase 40 | Item 3 CONFIRMED DONE — dropped from scope (verified: `main.go:303-309`, single call, comment now accurate) |

**Deprecated/outdated:** None introduced by this phase — it is entirely a cleanup of pre-existing internal debt, not an adoption of new library features.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Item 7's helper should NOT try to force Task's gate order to match Milestone/Phase/Plan without an explicit decision | Pitfall 1 | If the planner instead assumes silent normalization is fine, a real (if narrow) dispatch-ordering behavior change ships undocumented, violating this phase's "non-breaking" framing |
| A2 | Item 8 should include `ProjectReconciler`, not just the 3 controllers the seed's Files line names | Pitfall 2 | If Project is left out, the PlannerDeps extraction is incomplete and the exact "forgotten wiring" bug class (cascade-8) remains possible for Project's own Dispatcher/EnvReader/etc. fields |
| A3 | Item 11's PVC-name fix requires adding a new field to 4 reconcilers (not just swapping a literal for a constant) | Pitfall 3 | If planned as a 1-line literal-to-constant swap, the plan will underestimate scope and the executor will discover the field doesn't exist on Milestone/Phase/Plan/Task reconcilers mid-task |

**All three assumptions above are grounded in this session's own grep/read verification (not training-data recall)** — they are flagged `[ASSUMED]`-adjacent only in the sense that the *planning implication* (how to resolve the divergence) is a decision, not a fact; the underlying code facts themselves are `[VERIFIED: codebase grep]`.

## Open Questions (RESOLVED)

1. **Item 7: should the shared helper cover only Milestone/Phase/Plan (3 sites, identical order) or all 4 including Task (different order + extra headroom check)?**
   - What we know: 3 of 4 sites share byte-identical project-scoped gate order; Task's differs on Import position and has an extra reservation-headroom check with no counterpart elsewhere.
   - What's unclear: Whether CONTEXT's D-07 "migrates ONE controller per plan/commit" implies all 4 must eventually land on the same helper, or whether Task legitimately stays a structural outlier.
   - Recommendation: Plan item 7 as 3 commits (Milestone, Phase, Plan → shared `checkDispatchHolds`) plus a 4th commit that either (a) leaves Task's inline chain as-is with a comment cross-referencing the new helper, or (b) migrates Task with an explicit, tested, documented order (mirroring how D-04 documents item 9's behavior change). Surface this choice to the user at plan time if `discuss_mode` allows — it's a genuine fork, not free discretion.
   - **RESOLVED (plan-time, 2026-07-12):** Recommendation adopted as option (a) — plan 41-05 extracts `checkDispatchHolds` for the three planner-tier sites ONLY (Milestone/Phase/Plan, preserving their shared order and 5s/30s requeues); Task's inline chain stays untouched with a cross-reference comment plus the follow-up todo `.planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md`.

2. **Item 6: does `reconcileWithRetry`'s looser conflict-match (`"Conflict"` substring in addition to `"the object has been modified"`) need to be preserved, tightened to `apierrors.IsConflict`, or is the substring match hiding a case `apierrors.IsConflict` won't catch?**
   - What we know: `apierrors.IsConflict(err)` checks `StatusError.ErrStatus.Reason == metav1.StatusReasonConflict`, which is what the K8s API server returns for real 409s.
   - What's unclear: Whether any test path produces a conflict-shaped error that is NOT a `*apierrors.StatusError` (e.g., a wrapped error from a fake/cached client) that would only be caught by the substring match today.
   - Recommendation: Convert to `apierrors.IsConflict(err)` and run the full `internal/controller` test suite (not just a `-run` subset) before considering item 6 closed — the seed's own Verify line (`go test ./internal/controller/...`) already covers this, just make sure it runs the FULL package given the ~90-callsite blast radius, not a narrowed `-run` filter.
   - **RESOLVED (plan-time, 2026-07-12):** Recommendation adopted — plan 41-02 converts the surviving `reconcileWithRetry` driver to `apierrors.IsConflict` and gates on the FULL `go test ./internal/controller/...` package run (no `-run` narrowing), proving IsConflict catches every conflict shape the substring match did.

## Environment Availability

Skipped — this phase has no external tool/service dependencies beyond the existing Go toolchain, which is already confirmed present and pinned (`go.mod`: go 1.26.0, controller-runtime v0.24.1, Ginkgo v2.28.3, Gomega v1.40.0 — all match CLAUDE.md's Technology Stack table with no drift). `make manifests`/`make generate`/`make lint`/`make lint-fix`/`make test`/`make test-int`/`make vet`/`make build` all confirmed present in `Makefile` at the exact names the seed's Verify lines and CLAUDE.md reference.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28.3 + Gomega v1.40.0 (BDD specs) + plain `go test` (table-driven unit tests, e.g. `wiring_test.go`) |
| Config file | No `ginkgo.yaml`/`.ginkgo`; suite bootstrap is `suite_test.go`'s `RunSpecs` call per package (envtest-backed) |
| Quick run command | `go test ./internal/controller/... -run <Pattern>` (targeted; CLAUDE.md explicitly warns against repo-wide `make test` inside a sandboxed run for gate-chain work) |
| Full suite command | `make test` (Layer A / envtest unit tier) and `make test-int` (Layer A + Layer B kind, ~45-70 min budget per CLAUDE.md's raised timeouts) |

### Phase Requirements → Test Map

Requirement IDs are not yet minted (D-08: minted at plan time). The table below maps each seed item to its Verify line (carried forward per CONTEXT's "Verify lines are the acceptance criteria — carry them into plans verbatim") and the automated command that exercises it today.

| Item | Behavior | Test Type | Automated Command | File Exists? |
|------|----------|-----------|--------------------|--------------|
| 1 | No raw Status.Phase literal remains outside constant defs/tests | static grep + unit | `rg '"Succeeded"' internal/controller cmd` (expect 0 non-test hits) + `go build ./...` + `go test ./internal/controller/... ./cmd/tide/...` | ✅ existing suite covers behavior; grep is the completeness check |
| 2 | Halt/budget checks still gate correctly after `meta.IsStatusConditionTrue` swap | unit | `go test ./internal/controller/... -run 'Halt\|Budget'` | ✅ `billing_halt_regression_test.go`, `budget_blocked_regression_test.go` exist |
| 4 | Dead code removed, no live caller broke | build + unit | `go build ./...` && `go vet ./...` && `go test ./internal/controller/... ./cmd/manager/...` | ✅ |
| 5 | No mojibake remains | static grep + build | `grep -rc 'â' internal/controller/dispatch_helpers.go internal/subagent/anthropic/subagent.go` (expect 0) + `go build ./...` | ✅ |
| 6 | Conflict detection uses `apierrors.IsConflict`; unified driver used everywhere | unit (test-only change) | `go test ./internal/controller/...` (full package — blast radius is ~90 call sites across 12 files, do not narrow with `-run`) | ✅ |
| 7 | Gate ordering + requeue values preserved per controller (see Open Question 1) | integration (envtest) | `go test ./internal/controller/... -run 'Gates\|Halt\|Budget\|Import'` | ✅ `milestone_gates_test.go`, `phase_gates_test.go`, `plan_gates_test.go`, `task_gates_test.go` exist |
| 8 | No reconciler constructed with a zero-value Deps field | unit (wiring lock) | `go test ./cmd/manager/...` (extend `wiring_test.go` + `wave_dispatcher_wiring_test.go` for the new carrier) | ✅ both files exist today |
| 9 | `ConditionParentUnresolved` polarity consistent True=unresolved everywhere; consumers updated | integration (envtest) | `go test ./internal/controller/... -run 'Parent'` (covers `parentref_surface_test.go`, `task_controller_extracted_test.go`) | ✅ |
| 10 | Approve-consume + patch* helpers behave identically post-extraction | integration (envtest) | `go test ./internal/controller/... -run 'Gates\|Approve\|Boundary'` | ✅ `annotation_patch_test.go`-style coverage exists (verify exact filename at plan time) |
| 11 | `"tide-projects"` literal eliminated; PVC name genuinely configurable | static grep + integration | `rg '"tide-projects"' internal \| grep -v _test` (expect exactly 1 hit: the constant def itself) + `go test ./internal/controller/...` | ✅ |
| 12 | AGENTS.md amended; zero code/log-message diff; load-bearing greps unchanged | static grep (before/after diff) | `rg -l 'dispatch held\|creating job' internal test .planning` (compare hit list before/after — must be identical) + `go test ./internal/controller/...` | ✅ — this is a doc-only item, no new test needed |

### Sampling Rate

- **Per task commit:** targeted `go test ./internal/controller/... -run '<Pattern>'` matching the item's affected suite (see table above)
- **Per wave merge:** `go test ./internal/controller/... ./cmd/manager/... ./cmd/tide/...` (full package tier, Layer A only — Layer B kind suite not required for a non-breaking internal refactor unless item 7/8 wiring is touched, in which case run `make test-int` once at wave close)
- **Phase gate:** `make test-int` green before `/gsd:verify-work`, per CLAUDE.md's exact-string MAKE_EXIT + `grep -nE '^--- FAIL|^FAIL\s'` discipline (a RED go-test inside the `test/integration/kind` package fails the whole target even when Ginkgo prints SUCCESS — do not rely on the Ginkgo summary line alone)

### Wave 0 Gaps

None — existing test infrastructure covers all 11 items' behaviors. Every item's Verify line maps to a test file or suite that already exists in the repository (confirmed by direct `ls`/`grep` above); no new test framework, fixture, or shared conftest-equivalent is required before implementation starts. The one net-new test surface is item 8's wiring-lock extension (`cmd/manager/wiring_test.go` + `wave_dispatcher_wiring_test.go`), which is additive to existing files, not a new file.

## Sources

### Primary (HIGH confidence — direct codebase inspection, current HEAD `a39baa6`)

- `api/v1alpha3/*.go` (all 8 files) — read directly for item 1's constant-pattern precedent and item 9's condition/reason definitions
- `internal/controller/{milestone,phase,plan,task,project,wave}_controller.go` — read directly for items 2, 4, 7, 8, 9, 10, 11
- `internal/controller/dispatch_helpers.go` — read directly for item 5 (mojibake) and item 7's extraction target location
- `internal/subagent/anthropic/subagent.go` — read directly for item 5
- `internal/controller/{billing,failure}_halt.go`, `budget_blocked.go` — read directly for item 2
- `internal/controller/{milestone,phase,task,plan,wave}_controller_test.go` — read directly for item 6, including discovery of the pre-existing `reconcileWithRetry` shared driver
- `cmd/manager/main.go` — read directly for items 4, 8 (wiring lines 219, 390-611)
- `cmd/manager/wiring_test.go` — read directly for item 8's existing wiring-lock pattern
- `AGENTS.md` (lines 213-230) — read directly for item 12's amendment target
- `CLAUDE.md` (project file) — read for the exact-string log greps that drive D-05
- `go.mod` — read directly for version verification (go 1.26.0, controller-runtime v0.24.1, Ginkgo v2.28.3, Gomega v1.40.0)
- `Makefile` — grepped directly for target existence (`test`, `test-int`, `lint`, `lint-fix`, `manifests`, `generate`, `vet`, `build`)

### Secondary (MEDIUM confidence)

None — this research required no external web sources; the entire domain is internal codebase state, verifiable directly.

### Tertiary (LOW confidence)

None.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new dependencies; existing `go.mod` versions confirmed by direct read
- Architecture: HIGH — every claimed file:line was read or grepped in this session against current HEAD, not carried over from the seed
- Pitfalls: HIGH — Pitfalls 1-3 are net-new findings discovered by direct comparison of all four controllers' source in this session, not restated from the seed or CONTEXT

**Research date:** 2026-07-11
**Valid until:** Until the next structural change to `internal/controller` (e.g., another API version crank or a controller-runtime bump) — recommend re-verifying file:line anchors again if planning is delayed more than ~2 weeks, per this phase's own lesson about the Phase 40 staleness gap.
