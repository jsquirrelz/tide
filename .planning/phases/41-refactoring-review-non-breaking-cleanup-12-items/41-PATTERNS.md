# Phase 41: Refactoring Review — Non-Breaking Cleanup - Pattern Map

**Mapped:** 2026-07-11
**Files analyzed:** 11 items / ~35 distinct files (all pre-existing — this phase creates zero new files)
**Analogs found:** 11 / 11 items have an in-repo analog; 10 of 11 extend a pattern the codebase already established elsewhere

**Framing note (from RESEARCH.md):** every item in this phase is "finish an extraction the codebase already started," not "introduce a new pattern." There is no green-field design decision here — each item's analog is either (a) a sibling file that already did the same extraction for a different kind/reconciler, or (b) the same file's own established idiom applied inconsistently. Pattern assignments below cite the **current HEAD** file:line anchors from 41-RESEARCH.md, re-verified in this session (not the stale seed).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `api/v1alpha3/milestone_types.go` (+phase/plan/task/wave) | model (CRD types) | CRUD (status enum) | `api/v1alpha3/project_types.go:460-486` | exact |
| `internal/controller/billing_halt.go` | utility (halt-check) | transform | itself — `k8s.io/apimachinery/pkg/api/meta.IsStatusConditionTrue` (already used elsewhere in repo) | exact (self-referential fix) |
| `internal/controller/failure_halt.go` | utility (halt-check) | transform | `internal/controller/billing_halt.go:78-89` (sibling, same fix shape) | exact |
| `internal/controller/budget_blocked.go` | utility (halt-check) | transform | `internal/controller/billing_halt.go:78-89` | exact |
| `internal/controller/task_controller.go` (dead `gateDispatch`/`ensureJob`) | controller | request-response | N/A — deletion, no analog needed | n/a |
| `internal/controller/{milestone,phase,plan,project,task}_controller.go` (dead `SubagentImage` field) | controller (struct field) | CRUD (config) | N/A — deletion | n/a |
| `internal/controller/dispatch_helpers.go` (mojibake) | comment fix | n/a | N/A — text fix | n/a |
| `internal/subagent/anthropic/subagent.go` (mojibake) | comment fix | n/a | N/A — text fix | n/a |
| `internal/controller/task_controller_test.go` (delete `reconcileN`/`isConflict`) | test | request-response (envtest retry) | `internal/controller/milestone_controller_test.go:42-59` (`reconcileWithRetry`) | exact |
| `internal/controller/plan_controller_test.go` (delete `reconcilePlanN`) | test | request-response | `internal/controller/milestone_controller_test.go:42-59` | exact |
| `internal/controller/wave_controller_test.go` (delete `reconcileWaveN`) | test | request-response | `internal/controller/milestone_controller_test.go:42-59` | exact |
| `internal/controller/dispatch_helpers.go` (new `checkDispatchHolds`) | middleware (gate chain) | request-response | `internal/controller/dispatch_helpers.go:425-467` (`checkParentApproval`, same file/shape) | exact |
| `internal/controller/{milestone,phase,plan}_controller.go` (gate-chain call sites) | controller | request-response | `internal/controller/milestone_controller.go:343-393` (byte-identical order across the 3) | exact (3-way identical) |
| `internal/controller/task_controller.go` (gate-chain call site) | controller | request-response | same 3 sites — **but order differs**, see Pitfall 1 below | role-match only |
| `internal/controller/{milestone,phase,plan,project}_controller.go` (struct → `PlannerDeps`) | controller (deps carrier) | CRUD (config wiring) | `internal/controller/task_controller.go:90-122` (`TaskReconcilerDeps`) | exact |
| `cmd/manager/main.go` (wiring 416-527) | config (wiring) | CRUD | `cmd/manager/main.go:546-568` (`TaskReconciler.Deps` literal, same file) | exact |
| `cmd/manager/wiring_test.go` (extend) | test | request-response (unit) | `cmd/manager/wiring_test.go:88-93` (`Task.Deps.Dispatcher` case) | exact |
| `internal/controller/milestone_controller.go` (`surfaceParentRefUnresolved`, polarity) | controller | event-driven (status condition) | `internal/controller/task_controller.go:344-355` (correct polarity) | exact |
| `internal/controller/phase_controller.go` (`surfaceParentRefUnresolved`, polarity) | controller | event-driven | `internal/controller/task_controller.go:344-355` | exact |
| `internal/controller/{milestone,phase}_controller.go` (approve-consume/patch* dedup) | controller (leaf helper extraction) | transform | `internal/controller/dispatch_helpers.go:68-107` (`spawnReporterIfNeeded`) + `internal/controller/boundary_push.go:76-...` (`triggerBoundaryPush`) | role-match (shape precedent) |
| `internal/owner/label.go` (new label-key constants) | config (constants) | n/a | `internal/owner/label.go:29-33` (`LabelProject`, same file) | exact |
| `internal/controller/{milestone,phase,plan,task}_controller.go` (add `SharedPVCName` field + fix literal) | controller (config field) | CRUD | `internal/controller/project_controller.go:189-192` (`SharedPVCName` field) + `:2158-2164` (`sharedPVCName()` accessor) | exact |
| `cmd/manager/main.go` (wire `SharedPVCName` to 4 more reconcilers) | config (wiring) | CRUD | `cmd/manager/main.go:578-584` (`ImportReconciler.SharedPVCName: sharedPVCName`) | exact |
| `AGENTS.md` (Logging section, lines 213-230) | doc | n/a | N/A — doc-only amendment | n/a |

## Pattern Assignments

### Item 1 — Typed Status.Phase constants

**Files:** `api/v1alpha3/{milestone,phase,plan,task,wave}_types.go` (targets) — `api/v1alpha3/project_types.go` (analog, no change needed)

**Analog:** `api/v1alpha3/project_types.go:460-486`

**Core pattern to copy verbatim (const block + doc-comment shape):**
```go
// Source: api/v1alpha3/project_types.go:460-486 (current code)

// Project Phase constants for Project.Status.Phase (Plan 10 — init Job + budget gate).
const (
	// PhasePending is the initial phase before any reconcile has run.
	PhasePending = "Pending"
	// PhaseInitialized is set when the init Job completes successfully.
	PhaseInitialized = "Initialized"
	// PhaseInitFailed is set when the init Job exits non-zero.
	PhaseInitFailed = "InitFailed"
	...
	// PhaseComplete is the terminal success phase — set when all Milestones
	// reach Succeeded and the final push Job lands (Phase 3 D-B2 #4).
	PhaseComplete = "Complete"
)

// ProjectStatus defines the observed state of Project.
type ProjectStatus struct {
	// Phase is a high-level state label ("Pending", "Running", "Complete", "Failed").
	// +optional
	Phase string `json:"phase,omitempty"`
	...
```

**Target file's current shape** (`api/v1alpha3/milestone_types.go:54`, identical pattern in phase/plan/task/wave types) — field exists, zero constants exist yet:
```go
// Source: api/v1alpha3/milestone_types.go:54 (current code, inside MilestoneStatus)
Phase string `json:"phase,omitempty"`
```

**Constraint from D-03 (CONTEXT.md):** field type stays `string` — do NOT add `+kubebuilder:validation:Enum` this phase (that's a CRD schema change, deferred to v1alpha4). Only add the `const (...)` block per kind, matching Project's naming convention (`Phase<Value>`), then sweep raw literal consumers.

**Consumer sweep target:** `internal/controller/*.go` string literals for `"Succeeded"|"Failed"|"Running"|"AwaitingApproval"|"Pending"|"ZeroMembers"` — RESEARCH.md Pitfall 4 counts **117 non-test sites**. Prefer a scripted per-kind sweep (one commit per kind, `go build ./...` after each) over hand-editing.

---

### Item 2 — Replace hand-rolled condition loops with `meta.IsStatusConditionTrue`

**Files:** `internal/controller/billing_halt.go`, `failure_halt.go`, `budget_blocked.go`

**Current shape** (`internal/controller/billing_halt.go:78-89`, full file read this session):
```go
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

**Target shape (keep the nil-safe wrapper, replace only the loop body):**
```go
func checkBillingHalt(project *tideprojectv1alpha3.Project) bool {
	if project == nil {
		return false
	}
	return meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionBillingHalt)
}
```
Requires adding `"k8s.io/apimachinery/pkg/api/meta"` to the import block (already imported in `dispatch_helpers.go` and `task_controller.go` — copy the alias-free import exactly as shown there).

**Identical transform applies to:**
- `internal/controller/failure_halt.go:56-67` (`checkFailureHalt`)
- `internal/controller/failure_halt.go:93-98` (second loop — the "already halted, idempotent no-op" guard inside `setFailureHaltIfNeeded`, confirmed present in this session's read)
- `internal/controller/budget_blocked.go:55-66` (per RESEARCH.md; same shape, not re-read this session — trust the RESEARCH anchor)

**Import pattern** (already present in `dispatch_helpers.go:46` for a different apimachinery import — mirror the same unaliased style):
```go
"k8s.io/apimachinery/pkg/api/meta"
metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
```

---

### Item 4 — Dead code / dead field removal

**Files:** `internal/controller/task_controller.go` (dead `gateDispatch`/`ensureJob` at ~1434/1452), `SubagentImage` field × 5 controllers, `cmd/manager/main.go` wiring, `WaveReconciler.PlannerPool`/`ExecutorPool`.

No copy-pattern needed — pure deletion. Cross-reference constraint: `TaskReconcilerDeps.SubagentImage` (`task_controller.go:96-98`) is explicitly commented `"dead since Phase 13 ... retained for legacy test wiring, ignored at dispatch"` — confirm test wiring sites don't break before deleting the field; grep `SubagentImage` across `*_test.go` first.

---

### Item 5 — Fix mojibake in comments

**Files:** `internal/controller/dispatch_helpers.go` (5 lines confirmed this session at 17, 20, 24, 28, 117 — RESEARCH.md counted 13, this grep found the mojibake byte sequences at these anchors, more may exist elsewhere in the file body not grepped for this exact byte), `internal/subagent/anthropic/subagent.go` (5 lines confirmed at 19, 29, 32, 35, 59).

**Confirmed mojibake sites** (this session, `grep -n` for the malformed byte sequence — every occurrence is a mis-decoded em-dash, U+2014 double-encoded through Latin-1, rendering as the two-character garble seen below):
```
dispatch_helpers.go:17:  // Package controller <mojibake> dispatch_helpers.go consolidates...
dispatch_helpers.go:20:  // reconciler bodies from drifting in lockstep <mojibake> each reconciler...
dispatch_helpers.go:24:  //     per D-C2: levels.{level}.{model,params} <mojibake> Project default <mojibake>
dispatch_helpers.go:28:  //     planner-level dispatch <mojibake> sets Role="planner", Level=<level>,
dispatch_helpers.go:117: //     "no Helm default" <mojibake> caller is responsible for surfacing this

subagent.go:19: // code lives here, NOT in pkg/dispatch, pkg/controller, or pkg/dag <mojibake> the
subagent.go:29: //   - NEVER mount the host claude config dir <mojibake> the --bare flag (RESEARCH
subagent.go:32: //   - NEVER use OAuth headless <mojibake> claude-code#29983, #7100 break it. We pin
subagent.go:35: //   - NEVER embed an LLM API key directly <mojibake> the API key lives only in the
subagent.go:59: // orchestrator-resolved provider triple and the running subagent image <mojibake> if
```
Each `<mojibake>` marker above stands for the literal garbled byte sequence in place of an em-dash (—) in the real files — inspect the two files directly (`grep -n` for a non-ASCII byte, or open in an editor) rather than trusting a copy-pasted glyph here, since garbled multi-byte sequences do not always survive copy/paste faithfully. D-06 note: this overlaps a Phase-40-REVIEW.md Info finding in the same file — resolve in the same edit per CONTEXT D-06's exception clause. Verify with a byte-count grep for the malformed sequence returning 0 in both files after the fix, and confirm via `go build ./...` that the comment-only change didn't touch code.

---

### Item 6 — Test-helper unification (target: `reconcileWithRetry`, NOT a new generic)

**Files:** `internal/controller/task_controller_test.go` (delete `reconcileN`+`isConflict`), `plan_controller_test.go` (delete `reconcilePlanN`), `wave_controller_test.go` (delete `reconcileWaveN`) — repoint all call sites at the shared driver.

**Analog / surviving driver** (`internal/controller/milestone_controller_test.go:42-59`, full file header read this session):
```go
// Source: internal/controller/milestone_controller_test.go:1-59 (current code)
package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/gates"
	"github.com/jsquirrelz/tide/internal/pool"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// reconcileWithRetry drives a Reconcile call N times, retrying on 409 Conflict.
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

**Duplicated outlier to delete** (`internal/controller/task_controller_test.go:83-112`, confirmed this session):
```go
func reconcileN(r *TaskReconciler, name types.NamespacedName, n int) (ctrl.Result, error) {
	var result ctrl.Result
	var err error
	for range n {
		for range 5 {
			result, err = r.Reconcile(context.Background(), reconcile.Request{NamespacedName: name})
			if err == nil {
				break
			}
			// Retry on 409 Conflict (stale cache resource version).
			if isConflict(err) {
				err = nil
				continue
			}
			return result, err
		}
		if err != nil {
			return result, err
		}
	}
	return result, err
}

// isConflict returns true if the error is a Kubernetes 409 Conflict error.
func isConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "the object has been modified")
}
```
`reconcilePlanN` (`plan_controller_test.go:153`) and `reconcileWaveN` (`wave_controller_test.go:146`) are the same shape, receiver-typed to `*PlanReconciler`/`*WaveReconciler` — confirmed present at those line numbers this session (bodies not re-read; RESEARCH.md already extracted the shape as identical).

**Fix-in-place required on the survivor per RESEARCH Open Question 2:** convert `reconcileWithRetry`'s string-matching to `apierrors.IsConflict(err)` (already imported elsewhere as `apierrors "k8s.io/apimachinery/pkg/api/errors"` — see `dispatch_helpers.go:46`). Run the FULL `go test ./internal/controller/...` package (not `-run`-narrowed) given the ~90-call-site blast radius.

**Call-site repoint pattern** (from existing consumers, e.g. `boundary_push_test.go`, `phase_gates_test.go`):
```go
Expect(reconcileWithRetry(r.Reconcile, name, n)).To(Succeed())
```
`reconcileN(r, name, n)` (receiver-typed, returns `(ctrl.Result, error)`) becomes `reconcileWithRetry(r.Reconcile, name, n)` (bound method value, returns `error` only) — callers that inspect the returned `ctrl.Result` need a small adaptation; most call sites only check the error (`Expect(...).To(Succeed())`), so this is mechanical for the majority.

---

### Item 7 — Extract shared dispatch-holds gate chain

**Files:** `internal/controller/dispatch_helpers.go` (new `checkDispatchHolds` — target), `milestone_controller.go`/`phase_controller.go`/`plan_controller.go` (3-way identical call sites), `task_controller.go` (diverges — see Pitfall 1).

**Analog for the extraction shape** (`internal/controller/dispatch_helpers.go:425-467`, `checkParentApproval` — already a shared cross-reconciler gate function living in this exact file):
```go
// Source: internal/controller/dispatch_helpers.go:442-467 (current code — the shape to mirror)
func checkParentApproval(ctx context.Context, c client.Client, ns, parentName, parentKind string) (bool, error) {
	if parentName == "" {
		return false, nil
	}
	switch parentKind {
	case "Milestone":
		var ms tideprojectv1alpha3.Milestone
		if err := c.Get(ctx, client.ObjectKey{Namespace: ns, Name: parentName}, &ms); err != nil {
			return false, client.IgnoreNotFound(err)
		}
		return ms.Status.Phase == "AwaitingApproval", nil
	case "Phase":
		...
	}
	return false, nil
}
```

**Call-site content to extract** (`internal/controller/milestone_controller.go:343-393`, byte-identical order confirmed at Phase/Plan controllers per RESEARCH.md, this session re-read Milestone in full):
```go
// Source: internal/controller/milestone_controller.go:347-392 (current code)
{
	var earlyProject *tideprojectv1alpha3.Project
	if ms.Spec.ProjectRef != "" {
		var p tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: ms.Spec.ProjectRef}, &p); err == nil {
			earlyProject = &p
		}
	}
	if earlyProject != nil && gates.CheckRejected(earlyProject) {
		return r.patchMilestoneRejected(ctx, ms, gates.RejectedReason(earlyProject))
	}
	if checkBillingHalt(earlyProject) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
			"milestone", ms.Name, "project", ms.Spec.ProjectRef)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if checkFailureHalt(earlyProject) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project failure halt (conservative profile)",
			"milestone", ms.Name, "project", ms.Spec.ProjectRef)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if checkBudgetBlocked(earlyProject) && !budget.IsBypassed(earlyProject, time.Now()) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked",
			"milestone", ms.Name, "project", ms.Spec.ProjectRef)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if earlyProject != nil && earlyProject.Spec.ImportSource != nil {
		c := meta.FindStatusCondition(earlyProject.Status.Conditions, tideprojectv1alpha3.ConditionImportComplete)
		if c == nil || c.Status != metav1.ConditionTrue {
			logf.FromContext(ctx).V(1).Info("import pending; holding planner dispatch",
				"milestone", ms.Name, "project", ms.Spec.ProjectRef)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}
}
```
Order (Milestone/Phase/Plan, all 3 byte-identical per RESEARCH.md): **Reject → Billing(30s) → Failure(30s) → Budget(30s) → Import(5s, LAST)**.

**CRITICAL divergence (Pitfall 1, RESEARCH.md):** `task_controller.go:366-458` checks Import **second** (right after ParentApproval, BEFORE Billing/Failure/Budget) — not last. The seed's "preserve exact order" instruction from CONTEXT D-07 does not mean all 4 sites already agree; they don't on this one axis. The suggested helper signature from CONTEXT's `## Specific Ideas` (`checkDispatchHolds(ctx, c, project, level, objName) (held bool, result ctrl.Result)`) covers project-scoped holds only — this maps cleanly to the 3-way-identical Milestone/Phase/Plan order. Task's distinct order + extra reservation-headroom check (task-only) makes it a poor drop-in candidate for the same helper without either (a) leaving Task's inline chain as-is with a cross-reference comment, or (b) an explicit, tested, documented order-normalization (mirroring how D-04 treats item 9's polarity fix). **This fork must be surfaced to the user at plan time**, not silently resolved (RESEARCH.md Open Question 1).

**Requeue values that MUST be preserved exactly:** `30 * time.Second` (Billing/Failure/Budget), `5 * time.Second` (ParentApproval, Import).

---

### Item 8 — Consolidate planner-reconciler deps into a `Deps` carrier

**Files:** `internal/controller/{milestone,phase,plan,project}_controller.go` struct fields → carrier; `cmd/manager/main.go` wiring; `cmd/manager/wiring_test.go` (extend).

**Analog** (`internal/controller/task_controller.go:80-142`, full struct + doc comment read this session):
```go
// Source: internal/controller/task_controller.go:80-142 (current code — the carrier pattern to mirror)

// TaskReconcilerDeps carries the dispatch-related dependencies for TaskReconciler.
// Mirrors HelmProviderDefaults precedent at dispatch_helpers.go:60-69.
//
// Fields are populated at Manager wiring time (cmd/manager/main.go) and never
// mutated thereafter — copying a small struct at construction is cheaper than
// indirection at every dispatch (RESEARCH.md §P3.2 §Known pitfalls).
//
// Pool fields (PlannerPool, ExecutorPool) and WatchNamespace stay as direct
// TaskReconciler fields because they're conceptually separate from "what to
// dispatch with" — they're concurrency limiters, not dispatch-tier deps.
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

// TaskReconciler reconciles a Task object at Standard depth (D-C1).
type TaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	PlannerPool *pool.Pool
	ExecutorPool *pool.Pool
	WatchNamespace string

	// Deps carries the dispatch-tier dependencies. Phase 04.1 P3.2 — mirrors the
	// HelmProviderDefaults precedent on Milestone/Phase/Plan reconcilers.
	Deps TaskReconcilerDeps
}
```

**Wiring pattern to mirror** (`cmd/manager/main.go:546-568`, same file, already shows the carrier populated at construction):
```go
if err := (&controller.TaskReconciler{
	Client:                  mgr.GetClient(),
	Scheme:                  mgr.GetScheme(),
	MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Task,
	ExecutorPool:            executorPool,
	WatchNamespace:          watchNamespace,
	Deps: controller.TaskReconcilerDeps{
		Dispatcher:           dispatcher,
		Budget:               budgetStore,
		Defaults:             defaults,
		SigningKey:           signingKey,
		SubagentImage:        subagentImage,
		CredproxyImage:       credproxyImage,
		EnvReader:            envReader,
		HelmProviderDefaults: helmProviderDefaults,
		Reservations:         reservationStore,
		ReserveEstimateCents: budgetReservePerDispatchCents,
		PricingOverridesJSON: pricingOverridesJSON,
	},
}).SetupWithManager(mgr); err != nil { ... }
```

**Wiring-lock test to extend** (`cmd/manager/wiring_test.go:88-93`, full file header read this session):
```go
// Source: cmd/manager/wiring_test.go:87-93 (current code — the case shape to add 4 more of)
{
	name: "Task.Deps.Dispatcher",
	nilFn: func() bool {
		return (&controller.TaskReconciler{Deps: controller.TaskReconcilerDeps{Dispatcher: dispatcher}}).Deps.Dispatcher == nil
	},
	message: "TaskReconciler.Deps.Dispatcher must be non-nil (Phase 04.1 P3.2 — dispatch-tier deps now carried in Deps)",
},
```
The existing cases for `Project.Dispatcher`/`Milestone.Dispatcher`/`Milestone.EnvReader`/`Phase.Dispatcher`/`Phase.EnvReader`/`Plan.Dispatcher`/`Plan.EnvReader` (lines 53-86, direct-field style) all need converting to the same `.Deps.X` accessor style once the carrier lands.

**Pitfall 2 (RESEARCH.md, confirmed this session at `project_controller.go:175-219`):** `ProjectReconciler` has the SAME ~9 dispatch-tier fields (`Dispatcher`, `EnvReader`, `SigningKey`, `CredproxyImage`, `SubagentImage`, `HelmProviderDefaults`, `ReporterImage`, `PricingOverridesJSON`) as Milestone/Phase/Plan — confirmed fields:
```go
// Source: internal/controller/project_controller.go:179-219 (current code)
PlannerPool  *pool.Pool
ExecutorPool *pool.Pool
Dispatcher dispatch.Dispatcher
WatchNamespace string
SharedPVCName string
TidePushImage string
EnvReader      podjob.EnvelopeReader
SigningKey     []byte
CredproxyImage string
SubagentImage        string
HelmProviderDefaults ProviderDefaults
ReporterImage string
PricingOverridesJSON string
Recorder record.EventRecorder
```
**Project MUST be included in the carrier extraction**, not just the 3 controllers the seed's Files line names — leaving Project out repeats the exact "forgotten wiring" bug class (cascade-8, the never-assigned `Dispatcher` field) this item exists to prevent.

---

### Item 9 — Normalize `ConditionParentUnresolved` polarity (True == parent unresolved)

**Files:** `internal/controller/milestone_controller.go` (`surfaceParentRefUnresolved`, WRONG polarity), `phase_controller.go` (same function name, WRONG polarity, per RESEARCH.md at 889-905, not re-read this session — trust RESEARCH anchor).

**Analog — the CORRECT polarity (D-04's model)** (`internal/controller/task_controller.go:344-355`, confirmed this session):
```go
// Source: internal/controller/task_controller.go:344-351 (current code — CORRECT polarity)
patch := client.MergeFrom(task.DeepCopy())
meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
	Type:               tideprojectv1alpha3.ConditionParentUnresolved,
	Status:             metav1.ConditionTrue, // True == parent unresolved
	Reason:             tideprojectv1alpha3.ReasonNoProjectLabel,
	Message:            "No Project found via label or owner-ref chain; awaiting label stamp by PlanReconciler",
	LastTransitionTime: metav1.Now(),
})
if perr := r.Status().Patch(ctx, task, patch); perr != nil {
	return taskGateResult{}, fmt.Errorf("patch parent-unresolved condition: %w", perr)
}
```

**Current WRONG shape to fix** (`internal/controller/milestone_controller.go:965-987`, confirmed this session):
```go
// Source: internal/controller/milestone_controller.go:971-987 (current code — WRONG polarity)
func (r *MilestoneReconciler) surfaceParentRefUnresolved(ctx context.Context, ms *tideprojectv1alpha3.Milestone, parentKind, parentRef string) {
	logger := logf.FromContext(ctx)
	msg := fmt.Sprintf("parent %s %q (spec.projectRef) not found in namespace %q; requeuing until it appears", parentKind, parentRef, ms.Namespace)
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionParentUnresolved,
		Status:             metav1.ConditionFalse, // WRONG — should be True
		Reason:             tideprojectv1alpha3.ReasonParentRefNotFound,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, ms); err != nil {
		logger.V(1).Info("surfaceParentRefUnresolved: status update failed (will retry on requeue)", "error", err)
	}
	if r.Recorder != nil {
		r.Recorder.Event(ms, corev1.EventTypeWarning, tideprojectv1alpha3.ReasonParentRefNotFound, msg)
	}
}
```
**Two changes needed, not one:** (1) flip `metav1.ConditionFalse` → `metav1.ConditionTrue` at both call sites; (2) per D-04, add a "clear to False/ParentResolved once the parent appears" counterpart — Task's flow naturally clears the condition on the next successful `resolveProject` (no extra code needed there), but Milestone/Phase's `surfaceParentRefUnresolved` has NO counterpart "resolved" call today; a new clear-condition call must be added where the parent Project is successfully found.

**Note the status-update mechanism differs between the analog and target** — Task uses `client.MergeFrom` + `r.Status().Patch` (race-safe against concurrent writers), Milestone uses a direct `r.Status().Update` (confirmed above). Do not change the update mechanism as part of this polarity fix unless the plan explicitly scopes that — it's a separate concern from D-04's polarity ask.

**Sweep target:** `rg ConditionParentUnresolved` across `internal/controller`, `cmd/dashboard`, and test files (`parentref_surface_test.go`, `task_controller_extracted_test.go`) — RESEARCH.md confirms `cmd/dashboard` is clean (no consumer found there), contrary to CONTEXT's caution to check it.

---

### Item 10 — Extract approve-consume + `patch*` status helpers (leaf-helper shape, NOT a generic level-reconciler)

**Files:** `internal/controller/{milestone,phase}_controller.go` (15 `patch*` funcs, 4 `countChild*` copies, 2+2 approve-consume copies per RESEARCH.md) — new home file TBD at plan time (discretion).

**Analog 1 — the "shared implementation + thin per-reconciler entry point" shape** (`internal/controller/boundary_push.go:76-...` + `:266-291`, confirmed this session):
```go
// Source: internal/controller/boundary_push.go:76-85 (current code — shared impl signature)
func triggerBoundaryPush(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	parent client.Object,
	project *tideprojectv1alpha3.Project,
	level string,
	tidePushImage string,
	helmDefaults ProviderDefaults,
) error { ... }

// Source: internal/controller/boundary_push.go:271-291 (current code — thin per-reconciler entry points)
func (r *MilestoneReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha3.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "milestone", r.TidePushImage, r.HelmProviderDefaults)
}
func (r *PhaseReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha3.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "phase", r.TidePushImage, r.HelmProviderDefaults)
}
func (r *PlanReconciler) maybeTriggerBoundaryPush(ctx context.Context, parent client.Object, project *tideprojectv1alpha3.Project) error {
	return triggerBoundaryPush(ctx, r.Client, r.Scheme, parent, project, "plan", r.TidePushImage, r.HelmProviderDefaults)
}
```

**Analog 2 — the leaf idempotent-mutation-primitive shape** (`internal/controller/dispatch_helpers.go:68-107`, `spawnReporterIfNeeded`, confirmed this session): takes plain params (`client.Client`, `scheme`, `parent metav1.Object`, `project`, a discriminator string like `parentKind`, plus config), returns `(bool, error)` where the bool signals "first observation" — a leaf status-mutation primitive, not a generic level-reconciler. This is the exact shape CONTEXT/RESEARCH point to for item 10: extract `patch*` and approve-consume bodies as free functions taking the object + discriminator, called from thin per-reconciler wrapper methods, mirroring `triggerBoundaryPush`/`spawnReporterIfNeeded`'s "one shared body, N one-line callers" shape — NOT a generic `reconcileLevel[T]()`.

**Anti-pattern warning (RESEARCH.md, applies here by extension of item 6's lesson):** do not invent a brand-new generic dispatcher for the 4 `countChild*` copies or approve-consume duplicates — the established idiom in this codebase is "one shared function + thin typed entry points," as shown by both analogs above, not Go generics over the CRD kind.

---

### Item 11 — Centralize repeated magic literals

**Files:** `internal/owner/label.go` (label-key constants, analog+target in one file), `internal/controller/{milestone,phase,plan,task}_controller.go` (add `SharedPVCName` field + fix dispatch-site literal), `cmd/manager/main.go` (wire the field to 4 more reconcilers).

**Label-key analog** (`internal/owner/label.go:29-33`, full file read this session):
```go
// Source: internal/owner/label.go:29-33 (current code — the pattern to extend)
// LabelProject is the canonical TIDE project label key.
// All child CRs created by the reporter or a reconciler must carry this
// label so that `tide approve` label-filtered discovery finds them on the
// first call (CUTS-01 run-1 finding-6).
const LabelProject = "tideproject.k8s/project"
```
New label-key constants land next to this one, same file, same doc-comment density (one-sentence purpose + call-site convention note).

**PVC-name accessor analog** (`internal/controller/project_controller.go:2158-2164`, confirmed this session — the CORRECT pattern, but under-used even by its own package):
```go
// Source: internal/controller/project_controller.go:2158-2164 (current code)
// sharedPVCName returns the configured shared PVC name or the default.
func (r *ProjectReconciler) sharedPVCName() string {
	if r.SharedPVCName != "" {
		return r.SharedPVCName
	}
	return defaultSharedPVCName
}
```

**Pitfall 3 (RESEARCH.md, confirmed this session at `project_controller.go:1763`):** even `ProjectReconciler`'s own planner-dispatch Job spec hardcodes the literal instead of calling its own accessor:
```go
// Source: internal/controller/project_controller.go:1763 (current code — BUG, ignores r.sharedPVCName())
PVCName:              "tide-projects",
```
Fix: `PVCName: r.sharedPVCName(),`.

**Struct field is MISSING on 4 reconcilers** — confirmed this session by reading `cmd/manager/main.go:416-572`: the `MilestoneReconciler`/`PhaseReconciler`/`PlanReconciler`/`TaskReconciler` struct literals have NO `SharedPVCName:` key at all today (only `ProjectReconciler` at line 189-192 and `ImportReconciler` — see below — carry the field). This is a bigger lift than a literal-to-constant swap: add the field to each of the 4 reconciler structs, then wire it in `main.go`.

**Wiring analog to copy exactly** (`cmd/manager/main.go:578-584`, `ImportReconciler` — the ONE other reconciler correctly wired to the shared `sharedPVCName` variable computed at line 219):
```go
// Source: cmd/manager/main.go:576-585 (current code — the wiring pattern to replicate)
// SharedPVCName reads the same sharedPVCName source as ProjectReconciler.PVCName
// so the import Job mounts the PVC the rest of the controller writes to (WR-03).
if err := (&controller.ImportReconciler{
	Client:                  mgr.GetClient(),
	Scheme:                  mgr.GetScheme(),
	MaxConcurrentReconciles: cfg.MaxConcurrentReconciles.Project,
	WatchNamespace:          watchNamespace,
	ImportImage:             importImage,
	SharedPVCName:           sharedPVCName,
}).SetupWithManager(mgr); err != nil { ... }
```
The same `sharedPVCName` variable (`main.go:219`, `sharedPVCName := envOrDefault("TIDE_WORKSPACES_PVC_NAME", "tide-projects")`) is already in scope at every reconciler's construction site in the same function — this is an additive `SharedPVCName: sharedPVCName,` line in 4 more struct literals, no new variable needed.

**Hardcoded literal sites confirmed to fix (grep target, `rg '"tide-projects"' internal/controller | grep -v _test` must return exactly 1 hit after — the constant definition itself):**
- `project_controller.go:1763` (planner-dispatch — the accessor exists but is bypassed)
- `milestone_controller.go:512`, `phase_controller.go:471`, `plan_controller.go:506`, `task_controller.go:802,1477` (per RESEARCH.md; not independently re-read this session — trust the RESEARCH anchor, verify exact line at plan/execute time since Phase 40 may have shifted them slightly)

---

### Item 12 — Log-style policy (doc-only, D-05)

**File:** `AGENTS.md` lines 213-230 (amend) — **zero controller-tier changes**.

**Current conflicting guidance in AGENTS.md** (confirmed this session) — the section instructs a capital-letter start, past-tense phrasing, "specify object type" (`log.Info("Starting reconciliation")`, `log.Info("Created Deployment", "name", deploy.Name)`, `log.Error(err, "Failed to create Pod", "name", name)`), citing the upstream Kubernetes SIG logging message-style guidelines.

This directly contradicts the codebase's actual, 100%-consistent convention. Sample confirmed this session (`internal/controller/dispatch_helpers.go:100`, `:106`; `billing_halt.go` doc comments; `milestone_controller.go:363-390`):
```go
logger.Info("spawned reporter Job", "job", reporterJobName, ...)
logger.Info("skipping reporter Job spawn: ReporterImage not configured", ...)
logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt", "milestone", ms.Name, ...)
```
All lowercase-initial, no capital-letter-start. **Fix is textual: amend the `### Logging` section's bullet list and example block (AGENTS.md lines 213-230) to codify lowercase-initial** (matching the 88 confirmed real sites), not to add the upstream K8s SIG capital-letter convention. Do NOT touch any `logger.Info`/`logger.Error` call site — CLAUDE.md's exact-string log greps (`"creating job"`, `"dispatch held"`) and `phase_gates_test.go` assertions depend on the current casing.

## Shared Patterns

### Nil-safe halt-check wrapper (item 2)
**Source:** `internal/controller/billing_halt.go:78-89`
**Apply to:** `failure_halt.go`, `budget_blocked.go`
```go
func checkXHalt(project *tideprojectv1alpha3.Project) bool {
	if project == nil {
		return false
	}
	return meta.IsStatusConditionTrue(project.Status.Conditions, tideprojectv1alpha3.ConditionX)
}
```

### Dispatch-tier deps carrier (item 8)
**Source:** `internal/controller/task_controller.go:90-122` (`TaskReconcilerDeps`)
**Apply to:** Milestone/Phase/Plan/Project reconciler structs
```go
type <Kind>ReconcilerDeps struct {
	Dispatcher     dispatch.Dispatcher
	EnvReader      podjob.EnvelopeReader
	SigningKey     []byte
	CredproxyImage string
	HelmProviderDefaults ProviderDefaults
	PricingOverridesJSON string
	// + kind-specific fields (e.g. TidePushImage, ReporterImage for planner-tier)
}
```
Pool fields and `WatchNamespace` stay direct reconciler fields (concurrency limiters, not dispatch-tier deps) — do not fold them into the carrier.

### Shared cross-reconciler gate helper, co-located in `dispatch_helpers.go` (item 7)
**Source:** `internal/controller/dispatch_helpers.go:442-467` (`checkParentApproval`)
**Apply to:** new `checkDispatchHolds` for the 3-way-identical Milestone/Phase/Plan order
```go
func checkDispatchHolds(ctx context.Context, c client.Client, project *tideprojectv1alpha3.Project, level, objName string) (held bool, result ctrl.Result) {
	// Reject → Billing(30s) → Failure(30s) → Budget(30s) → Import(5s) — the
	// order confirmed byte-identical across Milestone/Phase/Plan.
}
```

### Test-driver unification via bound method value (item 6)
**Source:** `internal/controller/milestone_controller_test.go:42-59` (`reconcileWithRetry` + `reconcilerFunc`)
**Apply to:** delete `reconcileN`/`reconcilePlanN`/`reconcileWaveN`; repoint call sites at `reconcileWithRetry(r.Reconcile, name, n)`

### Leaf idempotent-mutation primitive, "one shared body + thin typed entry points" (item 10)
**Source:** `internal/controller/dispatch_helpers.go:68-107` (`spawnReporterIfNeeded`) and `internal/controller/boundary_push.go:76-291` (`triggerBoundaryPush` + 3 one-line `maybeTriggerBoundaryPush` wrappers)
**Apply to:** `patch*`/approve-consume dedup across Milestone/Phase controllers — extract shared body taking `(ctx, client.Client, parent, project, discriminator string, ...)`, keep thin per-reconciler wrapper methods.

### Label-key / config-constant colocation (item 11)
**Source:** `internal/owner/label.go:29-33` (`LabelProject`)
**Apply to:** any new label-key constants (same file); PVC-name accessor pattern from `internal/controller/project_controller.go:2158-2164` (`sharedPVCName()`) for the 4 reconcilers gaining `SharedPVCName` fields.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `AGENTS.md` (item 12) | doc | n/a | Doc-only amendment; no code pattern applies — the "analog" is the codebase's own log-call convention (88 confirmed sites), which the doc must be corrected to match, not the other way around. |
| Item 4/5 deletions and comment fixes | n/a | n/a | Pure removal/text-fix; no pattern to copy from, just confirm no live caller breaks (item 4) or the mojibake byte-sequence grep returns 0 (item 5). |

## Metadata

**Analog search scope:** `api/v1alpha3/*.go`, `internal/controller/*.go` (all 6 `*_controller.go` + `dispatch_helpers.go`, `billing_halt.go`, `failure_halt.go`, `budget_blocked.go`, `boundary_push.go`), `internal/owner/label.go`, `internal/subagent/anthropic/subagent.go`, `cmd/manager/main.go`, `cmd/manager/wiring_test.go`, `AGENTS.md`
**Files scanned (direct Read/Grep this session):** 17
**Files trusted from RESEARCH.md's own direct reads (not re-read this session per no-duplicate-range rule):** `budget_blocked.go`, `phase_controller.go` (889-905, 330-386, 471), `plan_controller.go` (343-400, 506), `task_controller.go` (366-458, 802/1477), `parentref_surface_test.go`, `task_controller_extracted_test.go`
**Pattern extraction date:** 2026-07-11

---

*Phase: 41-refactoring-review-non-breaking-cleanup-12-items*
