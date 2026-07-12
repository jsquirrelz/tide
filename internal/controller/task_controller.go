/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/client-go/tools/record"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/gates"
	"github.com/jsquirrelz/tide/internal/harness"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

const taskFinalizer = "tideproject.k8s/task-cleanup"

// taskPlanRefIndexKey is the field indexer key for Task.Spec.PlanRef.
// Registered in SetupWithManager; used by listSiblingTasks.
const taskPlanRefIndexKey = ".spec.planRef"

// outputPathsViolation is the EnvelopeOut.Result sentinel emitted when an
// executor wrote outside its DeclaredOutputPaths. Drives both the failure
// classification and the budget/metric reason mapping.
const outputPathsViolation = "output-paths-violation"

// ErrParentUnresolved signals that resolveProject (or walkOwnerChainToProject)
// could not locate the owning Project via either the label fast-path or the
// bounded owner-ref chain walk. Callers convert this to a
// ConditionParentUnresolved status patch + 30s requeue. Phase 04.1 P1.4.
var ErrParentUnresolved = errors.New("no parent Project found via label or owner-ref chain")

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
	// SubagentImage is dead since Phase 13 — resolveImage owns resolution;
	// retained for legacy test wiring, ignored at dispatch.
	SubagentImage string
	EnvReader     podjob.EnvelopeReader
	Recorder      record.EventRecorder
	// HelmProviderDefaults carry Helm-chart provider/model defaults, mirroring
	// the Milestone/Phase/Plan reconcilers. buildEnvelopeIn uses them to resolve
	// the executor task's ProviderSpec (Vendor "anthropic" + the task-level model).
	HelmProviderDefaults ProviderDefaults

	// Reservations is the in-process D-05 pre-charge store (nil-safe; wired in
	// main.go in Plan 14-05). All ReservationStore methods are nil-receiver-safe
	// so existing tests that do not set this field continue to pass without panic.
	Reservations *budget.ReservationStore

	// ReserveEstimateCents is the flat per-dispatch reservation estimate (D-05
	// Option B; sourced from Helm budget.reservePerDispatchCents via the
	// --budget-reserve-per-dispatch-cents flag, wired in Plan 14-05). Zero means
	// no pre-charge — safe default for pre-Phase-14 code paths.
	ReserveEstimateCents int64

	// PricingOverridesJSON is the validated D-02 override JSON forwarded
	// opaquely to executor Jobs as TIDE_PRICING_OVERRIDES_JSON. The manager
	// does not interpret prices — it passes the validated string through.
	// Wired in Plan 14-05.
	PricingOverridesJSON string
}

// TaskReconciler reconciles a Task object at Standard depth (D-C1).
// Task is owned by Plan; the parent ref is set via internal/owner.EnsureOwnerRef.
type TaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	PlannerPool *pool.Pool
	// ExecutorPool — Task reconcile dispatches executor-pool subagents in Phase 2.
	ExecutorPool *pool.Pool

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string

	// Deps carries the dispatch-tier dependencies. Phase 04.1 P3.2 — mirrors the
	// HelmProviderDefaults precedent on Milestone/Phase/Plan reconcilers.
	Deps TaskReconcilerDeps
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=get;list;watch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/status,verbs=get;update;patch

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var task tideprojectv1alpha3.Task
	if err := r.Get(ctx, req.NamespacedName, &task); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !task.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &task, taskFinalizer,
			func(_ context.Context) error {
				logger.Info("task cleanup", "name", task.Name)
				// D-05: release the reservation so a deleted Task does not leak
				// its reserved headroom until manager restart.
				r.Deps.Reservations.Release(string(task.UID))
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&task, taskFinalizer) {
		controllerutil.AddFinalizer(&task, taskFinalizer)
		if err := r.Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure owner ref to parent Plan (CRD-02, Pitfall 23 prevention).
	// If the Plan is not found (e.g., Task created before Plan, or Plan deleted),
	// log and continue — owner ref is best-effort; dispatch must still proceed.
	if task.Spec.PlanRef != "" {
		var parent tideprojectv1alpha3.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Spec.PlanRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			// Plan not found: log and continue without owner ref.
			logger.V(1).Info("parent Plan not found; skipping owner ref", "planRef", task.Spec.PlanRef)
		} else {
			if err := owner.EnsureOwnerRef(&task, &parent, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Update(ctx, &task); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 5. Phase 2 dispatch body inside the Dispatcher seam (REQ-SUB-01).
	if r.Deps.Dispatcher != nil {
		return r.reconcileDispatch(ctx, &task)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonInitialized,
		Message:            "Task scaffolded; dispatcher not wired",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &task); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// taskGateResult carries the output of gateChecks: the resolved Project (needed
// by prepareDispatch and createDispatchJob), a shouldHalt flag, and the
// reconcile result to return when shouldHalt is true.
//
// Phase 04.1 P3.1 — introduced to allow gateChecks to return structured halt
// context to the reconcileDispatch orchestrator without multi-return sprawl.
type taskGateResult struct {
	project    *tideprojectv1alpha3.Project
	shouldHalt bool
	result     ctrl.Result
}

// taskDispatchSpec bundles the values computed by prepareDispatch that
// createDispatchJob needs to build and submit the executor Job.
//
// Phase 04.1 P3.1 — extracted from reconcileDispatch steps 7-9.
type taskDispatchSpec struct {
	attempt   int
	token     string
	envInJSON []byte
	project   *tideprojectv1alpha3.Project
}

// reconcileDispatch is the 12-step dispatch flow decomposed into 4 named
// methods per Phase 04.1 P3.1. Behavior is unchanged from the pre-extraction
// version; tests in task_controller_test.go continue to pass without changes.
func (r *TaskReconciler) reconcileDispatch(ctx context.Context, task *tideprojectv1alpha3.Task) (ctrl.Result, error) {
	gate, err := r.gateChecks(ctx, task)
	if err != nil {
		return ctrl.Result{}, err
	}
	if gate.shouldHalt {
		return gate.result, nil
	}

	release, err := r.acquireDispatchSlots(ctx, task, gate.project)
	if err != nil {
		// rateLimitedError carries the requeue delay; translate to ctrl.Result.
		var rlErr *rateLimitedError
		if errors.As(err, &rlErr) {
			return ctrl.Result{RequeueAfter: rlErr.delay}, nil
		}
		return ctrl.Result{}, err
	}
	committed := false
	defer func() {
		if !committed {
			release()
		}
	}()

	spec, err := r.prepareDispatch(ctx, task, gate.project)
	if err != nil {
		// maxAttemptsError: status patch already applied; clean halt (no requeue).
		var maErr *maxAttemptsError
		if errors.As(err, &maErr) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	result, err := r.createDispatchJob(ctx, task, spec)
	if err == nil {
		committed = true
	}
	return result, err
}

// gateChecks runs the dispatch gates: terminal-state short-circuit, Running
// Job-completion branch, project resolution (Phase 04.1 P1.4 — owner-walk +
// ParentUnresolved condition), reject check, budget cap check, indegree compute
// (FAIL-01/FAIL-02 wave boundary contract), wave-pause check, and gate-policy
// hook. Returns shouldHalt=true with the reconcile result populated when any
// gate stops dispatch.
//
// Phase 04.1 P3.1 — extracted from reconcileDispatch steps 1-4 (plus the
// Running-branch and gate-policy checks that logically belong to the gate layer).
func (r *TaskReconciler) gateChecks(ctx context.Context, task *tideprojectv1alpha3.Task) (taskGateResult, error) {
	// Step 1: Terminal short-circuit.
	if task.Status.Phase == "Succeeded" {
		return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
	}
	if task.Status.Phase == "Failed" {
		// Phase 25 D-02b: conservative failure halt check at terminal short-circuit.
		// A Failed task re-triggers the reconciler on every status change; this
		// hook stamps ConditionFailureHalt on the Project when
		// FailureProfile==conservative. Idempotent: setFailureHaltIfNeeded is a no-op
		// if the condition is already True. Project resolution is best-effort here;
		// if the project is unresolvable (transient), the halt fires when the task
		// is next reconciled. Non-fatal: dispatch for this task has already halted.
		if project, pErr := r.resolveProject(ctx, task); pErr == nil && project != nil {
			// CR-02 resume time-fence: pass the task's CompletedAt so a pre-resume
			// straggler reconciling after `tide resume --retry-failed` does not
			// re-stamp the halt. Zero when CompletedAt is unset (fail-closed: stamp).
			var taskCompletedAt time.Time
			if task.Status.CompletedAt != nil {
				taskCompletedAt = task.Status.CompletedAt.Time
			}
			if hErr := setFailureHaltIfNeeded(ctx, r.Client, project, taskCompletedAt); hErr != nil {
				logf.FromContext(ctx).Error(hErr, "setFailureHaltIfNeeded at terminal short-circuit failed (non-fatal)", "task", task.Name)
			}
		}
		return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
	}

	// Step 2: On Running — delegate to checkRunningState.
	if task.Status.Phase == "Running" {
		return r.checkRunningState(ctx, task)
	}

	// Step 3: Resolve Project.
	project, err := r.resolveProject(ctx, task)
	if errors.Is(err, ErrParentUnresolved) {
		// Phase 04.1 P1.4: parent Project not yet resolvable. Set condition,
		// requeue without dispatching. PIT-4: MergeFrom+Patch (not Update) for
		// race safety with concurrent Plan reconciles that may stamp the label.
		patch := client.MergeFrom(task.DeepCopy())
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionParentUnresolved,
			Status:             metav1.ConditionTrue,
			Reason:             tideprojectv1alpha3.ReasonNoProjectLabel,
			Message:            "No Project found via label or owner-ref chain; awaiting label stamp by PlanReconciler",
			LastTransitionTime: metav1.Now(),
		})
		if perr := r.Status().Patch(ctx, task, patch); perr != nil {
			return taskGateResult{}, fmt.Errorf("patch parent-unresolved condition: %w", perr)
		}
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
	}
	if err != nil {
		return taskGateResult{}, fmt.Errorf("resolve project: %w", err)
	}

	// Plan 04-05 reject short-circuit (D-G1 per-level policy enum, T-04-G1
	// mitigation). Fires BEFORE budget/indegree/dispatch so a rejected Project
	// halts even Pending tasks. Reject value carries the operator-supplied
	// reason which surfaces on the Condition Message (D-G4).
	// D-05: park (not fail) — in-flight Jobs drain; state is preserved for resume.
	if gates.CheckRejected(project) {
		result, err := r.patchTaskRejected(ctx, task, gates.RejectedReason(project))
		return taskGateResult{shouldHalt: true, result: result}, err
	}

	// D-02 descent hold: if the parent Plan is parked at AwaitingApproval,
	// hold Job dispatch here. Position: AFTER resolveProject (so ParentUnresolved
	// handling still wins) and BEFORE budget/indegree/dispatch (so a held task
	// spends nothing). The Task stays at Status.Phase="" so tide approve's
	// findAwaitingTask cannot target a held child instead of the parked parent
	// (12-RESEARCH.md Pitfall 5). NotFound parent is transient informer lag —
	// checkParentApproval returns (false, nil) and dispatch continues.
	if held, hErr := checkParentApproval(ctx, r.Client, task.Namespace, task.Spec.PlanRef, "Plan"); hErr != nil {
		return taskGateResult{}, hErr
	} else if held {
		logf.FromContext(ctx).V(1).Info("dispatch held: parent Plan awaiting approval",
			"task", task.Name, "plan", task.Spec.PlanRef)
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 5 * time.Second}}, nil
	}

	// Phase 28 IMPORT-01: park task dispatch until import completes.
	// Position: AFTER resolveProject (project is non-nil here — Pitfall 1) and BEFORE
	// billing/budget/dispatch holds (Pitfall 2 — parking after pool acquire leaks a slot).
	if project.Spec.ImportSource != nil {
		c := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha3.ConditionImportComplete)
		if c == nil || c.Status != metav1.ConditionTrue {
			logf.FromContext(ctx).V(1).Info("import pending; holding task dispatch",
				"task", task.Name, "project", project.Name)
			return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 5 * time.Second}}, nil
		}
	}

	// Phase 13 HALT-01 / D-05: third dispatch-entry hold (after CheckRejected +
	// parent-approval); park, never fail; cleared by tide resume.
	// Position: BEFORE pool/slot acquisition and BEFORE Job creation (Pitfall 2).
	// No per-Task condition written (avoids status flapping — operator signal is the
	// single Project BillingHalt condition).
	if checkBillingHalt(project) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
			"task", task.Name, "project", project.Name)
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
	}

	// Phase 25 DISP-02 / D-02b: fifth dispatch-entry hold — conservative failure halt.
	// Fires only when Project.Spec.FailureProfile==conservative AND a task execution
	// failure has stamped ConditionFailureHalt=True. Park (never fail); cleared by
	// `tide resume --retry-failed`. No per-Task condition stamp (operator signal is the
	// single Project FailureHalt condition — same pattern as BillingHalt).
	if checkFailureHalt(project) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project failure halt (conservative profile)",
			"task", task.Name, "project", project.Name)
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
	}

	// Phase 14 BUDGET-02 / D-04: fourth dispatch-entry hold — BudgetBlocked condition.
	// Cap detection happens here (not in ProjectReconciler) because Status patches from
	// RollUpUsage do NOT increment metadata.generation and thus do NOT re-enqueue the
	// ProjectReconciler (watch-predicate gap root cause, 14-RESEARCH.md §Root Cause).
	// The bidirectional setBudgetBlockedIfNeeded also handles cap-raise recovery: when
	// IsCapExceeded becomes false it clears the condition so dispatch can resume.
	if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.Deps.Reservations.TotalReserved()); err != nil {
		logf.FromContext(ctx).Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)")
	}
	// OR the legacy BudgetExceeded phase check so the Phase machinery (D-04) is preserved
	// — the condition check is the new primary path; the phase check is the fallback that
	// ensures tasks parked by the pre-Phase-14 phase gate continue to be held.
	if (checkBudgetBlocked(project) || project.Status.Phase == "BudgetExceeded") &&
		!budget.IsBypassed(project, time.Now()) {
		// No per-Task condition stamp: the operator signal is the single Project
		// BudgetBlocked condition, same as the other four dispatch gates. A
		// per-Task stamp here was never cleared once dispatch resumed, so it
		// outlived the park as stale misinformation on terminal Tasks.
		logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked",
			"task", task.Name, "project", project.Name,
			"spent", project.Status.Budget.CostSpentCents,
			"cap", project.Spec.Budget.AbsoluteCapCents)
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
	}

	// Phase 14 BUDGET-03 / D-05: reservation headroom check. Prevents wave-wide
	// overshoot (run-1 root class) by gating dispatch when committed spend + reserved
	// + this estimate would exceed the cap. Transient park — no per-Task condition
	// stamp (not a cap breach, just insufficient headroom at this moment).
	if !budget.IsBypassed(project, time.Now()) &&
		!r.Deps.Reservations.HasHeadroom(project, r.Deps.ReserveEstimateCents) {
		logf.FromContext(ctx).V(1).Info("dispatch held: insufficient reservation headroom",
			"task", task.Name,
			"spent", project.Status.Budget.CostSpentCents,
			"reserved", r.Deps.Reservations.TotalReserved(),
			"estimate", r.Deps.ReserveEstimateCents,
			"cap", project.Spec.Budget.AbsoluteCapCents)
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
	}

	// Step 5: Indegree + wave-pause + gate-policy — delegate to checkReadinessGates.
	return r.checkReadinessGates(ctx, task, project)
}

// checkReadinessGates runs the indegree compute (FAIL-01/FAIL-02), wave-pause
// label check (Plan 04-05 PauseBetweenWaves), and gate-policy hook (D-G1
// approve/pause). Returns shouldHalt=true if any gate blocks dispatch; returns
// the resolved Project in taskGateResult.project when all gates pass.
//
// Phase 04.1 P3.1 — extracted from gateChecks (steps 5+) to keep gateChecks ≤ 80 lines.
func (r *TaskReconciler) checkReadinessGates(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project) (taskGateResult, error) {
	// DISP-01: list ALL project tasks (global scope) so computeGlobalIndegree
	// resolves DependsOn edges across plan/phase/milestone boundaries. project.Name
	// is guaranteed non-empty (resolveProject returned it without error above).
	allProjectTasks, err := r.listProjectTasks(ctx, task, project.Name)
	if err != nil {
		return taskGateResult{}, err
	}

	// List Plans, Phases, Milestones for coarse-ref fan-out (same as assembleProjectDepGraph).
	var planList tideprojectv1alpha3.PlanList
	if err := r.List(ctx, &planList, client.InNamespace(task.Namespace)); err != nil {
		return taskGateResult{}, fmt.Errorf("list plans for global indegree: %w", err)
	}
	var phaseList tideprojectv1alpha3.PhaseList
	if err := r.List(ctx, &phaseList, client.InNamespace(task.Namespace)); err != nil {
		return taskGateResult{}, fmt.Errorf("list phases for global indegree: %w", err)
	}
	var msList tideprojectv1alpha3.MilestoneList
	if err := r.List(ctx, &msList, client.InNamespace(task.Namespace)); err != nil {
		return taskGateResult{}, fmt.Errorf("list milestones for global indegree: %w", err)
	}

	indegree := r.computeGlobalIndegree(ctx, *task, allProjectTasks, planList.Items, phaseList.Items, msList.Items)
	if indegree > 0 {
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Pending"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionReconciling,
			Status:             metav1.ConditionTrue,
			Reason:             tideprojectv1alpha3.ReasonAwaitingDispatch,
			Message:            fmt.Sprintf("Waiting for %d predecessor(s) to complete", indegree),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return taskGateResult{}, err
		}
		// Level-triggered re-check. This indegree gate is otherwise the ONLY dispatch
		// gate that parks with a bare ctrl.Result{} (every other gate — rejected,
		// parent-approval, budget, failure, reservation — carries a 5-30s
		// RequeueAfter). Relying purely on the edge-triggered globalDependentsMapper
		// stalls under cache lag: when a predecessor goes Succeeded, the mapper wakes
		// this task once, but if the informer is still lagging the re-derived indegree
		// is unchanged, the condition message is identical, meta.SetStatusCondition
		// no-ops, the MergeFrom patch is empty → no resourceVersion bump → no watch
		// event → no re-enqueue. The predecessor is now terminal (its watch never fires
		// again) and there is no SyncPeriod (default 10h resync), so the task sat
		// Pending until the 10h resync — the RESUME-01/DISP-01 flake. A bounded requeue
		// re-derives the global indegree every 10s until the cache converges (idempotent
		// no-op patch while still blocked), making dispatch deterministic under
		// contention without depending on a single mapper edge landing against a fresh
		// cache.
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 10 * time.Second}}, nil
	}

	// Plan 04-05 Task 2: PauseBetweenWaves dispatch block. PlanReconciler
	// stamps tideproject.k8s/wave-paused=<N> on tasks in a wave waiting for
	// approve-wave-N on the parent Plan; until the label is cleared (by
	// PlanReconciler on annotation consume), the Task stays AwaitingApproval.
	if _, paused := task.Labels["tideproject.k8s/wave-paused"]; paused {
		result, err := r.patchTaskAwaitingApproval(ctx, task, gates.PolicyPause)
		return taskGateResult{shouldHalt: true, result: result}, err
	}

	// Phase 25 DISP-03: Task gate composes with global indegree. The effective
	// policy is the task-level Spec.Gates.Task when set (non-empty), falling
	// back to the project-level Project.Spec.Gates.Task. Task-level takes
	// precedence: a fully-supervised run sets Gates.Task="approve" on individual
	// tasks without requiring a project-wide approval gate.
	taskLevelGates := task.Spec.Gates
	var effectiveGates tideprojectv1alpha3.Gates
	if taskLevelGates.Task != "" {
		effectiveGates = taskLevelGates
	} else {
		effectiveGates = project.Spec.Gates
	}
	policy := gates.EvaluatePolicy(effectiveGates, "task")
	if policy == gates.PolicyApprove || policy == gates.PolicyPause {
		if !gates.CheckApprove(task, "task") {
			result, err := r.patchTaskAwaitingApproval(ctx, task, policy)
			return taskGateResult{shouldHalt: true, result: result}, err
		}
		newAnno := gates.ConsumeApprove(task, "task")
		patch := client.MergeFrom(task.DeepCopy())
		task.SetAnnotations(newAnno)
		if err := r.Patch(ctx, task, patch); err != nil {
			return taskGateResult{}, err
		}
	}

	return taskGateResult{project: project}, nil
}

// checkRunningState handles a Task in Phase=Running: looks up the current Job
// and delegates to handleJobCompletion if the Job has reached a terminal
// condition. Returns shouldHalt=true in all cases — a Running Task never
// proceeds to the dispatch path.
//
// Phase 04.1 P3.1 — extracted from gateChecks (step 2) to keep gateChecks ≤ 80 lines.
func (r *TaskReconciler) checkRunningState(ctx context.Context, task *tideprojectv1alpha3.Task) (taskGateResult, error) {
	jobName := podjob.JobName(task.UID, task.Status.Attempt)
	var job batchv1.Job
	if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: jobName}, &job); err != nil {
		if !apierrors.IsNotFound(err) {
			return taskGateResult{}, err
		}
		return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
	}
	if isJobTerminal(&job) {
		project, err := r.resolveProject(ctx, task)
		if errors.Is(err, ErrParentUnresolved) {
			// Task is Running (Job just completed) but the Project has disappeared.
			// Requeue to give label-stamping a chance; do not set the condition here
			// because the Task was already dispatched (not awaiting stamp).
			return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
		}
		if err != nil {
			return taskGateResult{}, err
		}
		result, err := r.handleJobCompletion(ctx, task, project, &job)
		return taskGateResult{shouldHalt: true, result: result}, err
	}
	return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
}

// acquireDispatchSlots performs the rate-limit gate (FAIL-03 token bucket) and
// returns a release function the caller MUST call (typically via defer). The
// release function cancels the held rate-limit reservation if it was acquired.
// On dispatch-job-create success the caller suppresses the release by setting
// committed=true before the deferred call executes (CR-03 deferred reservation
// cancel — preserved across Phase 04.1 P3.1 extraction).
//
// Phase 04.1 P3.1 — extracted from reconcileDispatch step 6.
func (r *TaskReconciler) acquireDispatchSlots(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project) (release func(), err error) {
	// Step 6: Rate-limit gate (Pattern 1 — no blocking per Pitfall 1, D-D3).
	//
	// CR-03: Once a token is reserved (d == 0), any subsequent error before the
	// Job is successfully created must Cancel() the reservation. Otherwise the
	// bucket drains permanently on transient errors (signing, marshal, patch,
	// Create), and the controller silently throttles itself.
	var heldReservation interface{ Cancel() }
	release = func() {
		if heldReservation != nil {
			heldReservation.Cancel()
		}
	}

	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); err != nil {
			if !apierrors.IsNotFound(err) {
				return release, err
			}
		} else {
			lim := r.Deps.Budget.ForSecret(string(secret.UID), r.defaultsForSecret(&secret))
			if lim != nil {
				rsv := lim.Reserve()
				d := rsv.Delay()
				if d > 0 {
					rsv.Cancel()
					budget.ProviderRateLimitHitsTotal.WithLabelValues(project.Name).Inc()
					patch := client.MergeFrom(task.DeepCopy())
					meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
						Type:               tideprojectv1alpha3.ConditionReconciling,
						Status:             metav1.ConditionTrue,
						Reason:             "RateLimited",
						Message:            fmt.Sprintf("Rate-limit bucket exhausted; requeuing after %s", d),
						LastTransitionTime: metav1.Now(),
					})
					if err := r.Status().Patch(ctx, task, patch); err != nil {
						return release, err
					}
					// Return a sentinel error that encodes the requeue delay so
					// reconcileDispatch can translate it back to a ctrl.Result.
					return release, &rateLimitedError{delay: d}
				}
				// d == 0: token consumed. Hold the reservation so any error
				// before Job Create returns the token to the bucket.
				heldReservation = rsv
			}
		}
	}

	return release, nil
}

// rateLimitedError is returned by acquireDispatchSlots when the rate-limit
// bucket is exhausted. reconcileDispatch unwraps it to produce the correct
// ctrl.Result{RequeueAfter: d}.
type rateLimitedError struct {
	delay time.Duration
}

func (e *rateLimitedError) Error() string {
	return fmt.Sprintf("rate-limit bucket exhausted; requeue after %s", e.delay)
}

// prepareDispatch computes the attempt counter (including max-attempts check),
// mints the credproxy-signed token (Phase 04.1 P1.3 — DefaultCaps applied via
// podjob.DefaultCaps), and builds the EnvelopeIn JSON. Returns a taskDispatchSpec
// the caller passes to createDispatchJob.
//
// Phase 04.1 P3.1 — extracted from reconcileDispatch steps 7-9.
func (r *TaskReconciler) prepareDispatch(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project) (taskDispatchSpec, error) {
	// Step 7: Attempt counter.
	attempt, err := r.nextAttempt(ctx, task)
	if err != nil {
		return taskDispatchSpec{}, err
	}
	maxAttempts := int(project.Spec.MaxAttemptsPerTask)
	if maxAttempts <= 0 {
		maxAttempts = 3 // Helm default
	}
	if attempt > maxAttempts {
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Failed"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "ExceededAttempts",
			Message:            fmt.Sprintf("Exceeded max attempts %d", maxAttempts),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return taskDispatchSpec{}, err
		}
		return taskDispatchSpec{}, &maxAttemptsError{}
	}

	// Step 8: Mint signed token (D-C3).
	//
	// Phase 04.1 P1.3 fix: route through podjob.DefaultCaps so token validity
	// shares the executor 300s wall-clock floor with the Job's
	// activeDeadlineSeconds derivation. Both consumers MUST go through DefaultCaps —
	// drift between derivations is exactly the bug class audit P1.3 closed. Task
	// dispatch is always executor Kind; planner reconcilers (milestone/phase/plan)
	// pass JobKindPlanner via Plan 04.1-05 (P1.2) and get the 600s floor instead.
	taskCaps := podjob.DefaultCaps(task.Spec.Caps, podjob.JobKindExecutor)
	wallClock := taskCaps.WallClockSeconds
	token, err := credproxy.Sign(r.Deps.SigningKey, string(task.UID), time.Duration(wallClock+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if err != nil {
		return taskDispatchSpec{}, fmt.Errorf("mint signed token: %w", err)
	}

	// Step 9: Build EnvelopeIn; translate api/v1alpha1.Caps → pkg/dispatch.Caps per Plan 03.
	_, envInJSON, err := r.buildEnvelopeIn(ctx, task, project, attempt, token)
	if err != nil {
		return taskDispatchSpec{}, err
	}

	return taskDispatchSpec{
		attempt:   attempt,
		token:     token,
		envInJSON: envInJSON,
		project:   project,
	}, nil
}

// maxAttemptsError is a sentinel returned by prepareDispatch when the Task has
// exhausted its retry budget. reconcileDispatch treats this as a clean halt
// (the status patch was already applied inside prepareDispatch) and returns
// ctrl.Result{}, nil to stop requeueing.
type maxAttemptsError struct{}

func (e *maxAttemptsError) Error() string { return "task exceeded max dispatch attempts" }

// createDispatchJob patches Task.Status to dispatching (step 10), calls
// podjob.BuildJobSpec, creates the Job via the K8s API (step 11, idempotent on
// AlreadyExists), and patches Task.Status to Running on success (step 12).
// Returns (ctrl.Result{}, nil) on successful dispatch.
//
// Phase 04.1 P3.1 — extracted from reconcileDispatch steps 10-12.
//
//nolint:unparam // ctrl.Result kept so callers can `return r.createDispatchJob(...)` in the reconcile chain
func (r *TaskReconciler) createDispatchJob(ctx context.Context, task *tideprojectv1alpha3.Task, spec taskDispatchSpec) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	project := spec.project

	// Step 10: Patch Status.Attempt BEFORE Create (Pitfall 2 mitigation — prevents
	// drift if client.Create succeeds but the status patch fails on transient error).
	//
	// Optimistic lock (DISP-01/RESUME-01 dispatch-clobber fix): the dispatch
	// decision above was made from a cache read of `task` that may be stale under
	// informer lag. If a predecessor's completion (or, in the resume path, an
	// out-of-band Succeeded write) has meanwhile driven THIS task to a terminal
	// phase in etcd, a non-locked MergeFrom patch would blindly regress it back to
	// a dispatch state — clobbering the terminal status and permanently stalling
	// any dependent whose indegree can then never reach 0. Sending the read
	// resourceVersion as a precondition makes that stale commit fail with Conflict
	// instead; the reconcile requeues, re-reads the now-terminal phase, and
	// gateChecks' terminal short-circuit halts the redundant dispatch. Deterministic:
	// the illegal Succeeded→Running transition is refused at its source.
	{
		patch := client.MergeFromWithOptions(task.DeepCopy(), client.MergeFromWithOptimisticLock{})
		task.Status.Attempt = spec.attempt
		now := metav1.Now()
		task.Status.StartedAt = &now
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 11: Build + Create Job. AlreadyExists treated as success (Pitfall F / SUB-03).
	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}
	// SIGN-01 / D-03: resolve committer/author identity (mirrors resolveImage's
	// r.Deps.HelmProviderDefaults tier) and stamp it into the subagent Job env.
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindExecutor,
		Task:                 task,
		ParentObj:            task,
		Level:                "task",
		Project:              project,
		Attempt:              spec.attempt,
		SignedToken:          spec.token,
		EnvelopeInJSON:       spec.envInJSON,
		SubagentImage:        resolveImage(project, "task", r.Deps.HelmProviderDefaults),
		AgentName:            agentName,
		AgentEmail:           agentEmail,
		CredproxyImage:       r.Deps.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              "tide-projects",
		ProjectUID:           string(project.UID),
		EstimatedCostCents:   r.Deps.ReserveEstimateCents,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
	}
	job := podjob.BuildJobSpec(opts)
	if err := owner.EnsureOwnerRef(job, task, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure owner ref on job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create job: %w", err)
		}
		// AlreadyExists: idempotent success — watch-lag race (Pitfall F / SUB-03).
		logger.Info("job already exists; treating as successful dispatch", "job", job.Name)
	}
	// D-05: pre-charge the reservation immediately after successful Job creation
	// (including AlreadyExists-as-success). Reserve only when a non-zero estimate
	// is configured; same-key re-Reserve on retry dispatch is an intentional overwrite.
	if r.Deps.ReserveEstimateCents > 0 {
		r.Deps.Reservations.Reserve(string(task.UID), r.Deps.ReserveEstimateCents)
	}

	// Step 12: Patch Status.Phase=Running + Condition Running=True, Reason=Dispatched.
	// Optimistic lock (same dispatch-clobber guard as Step 10): if the task reached
	// a terminal phase between Step 10 and here, refuse the Running regression with
	// Conflict rather than clobbering it; the reconcile requeues and short-circuits.
	{
		patch := client.MergeFromWithOptions(task.DeepCopy(), client.MergeFromWithOptimisticLock{})
		task.Status.Phase = "Running"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionRunning,
			Status:             metav1.ConditionTrue,
			Reason:             "Dispatched",
			Message:            fmt.Sprintf("Job %s dispatched (attempt %d)", job.Name, spec.attempt),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// patchTaskRejected parks the Task with a RejectedByUser condition WITHOUT
// writing Status.Phase=Failed (D-05). In-flight Jobs drain; state is preserved
// so clearing the reject annotation (tide resume) lets the task re-enter the
// normal dispatch path on the next reconcile.
// Returns RequeueAfter 5s so the park polls for the annotation clear.
//
//nolint:unparam // ctrl.Result kept so callers can return r.patchTaskRejected(...) in the reconcile chain
func (r *TaskReconciler) patchTaskRejected(ctx context.Context, task *tideprojectv1alpha3.Task, reason string) (ctrl.Result, error) {
	patch := client.MergeFrom(task.DeepCopy())
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonRejectedByUser,
		Message:            fmt.Sprintf("Rejected: %s", reason),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, task, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// patchTaskAwaitingApproval parks the Task at Status.Phase=AwaitingApproval
// per Plan 04-05 gate seam (T-04-G4 mitigation — no requeue; an
// AnnotationChangedPredicate-driven re-reconcile is the only path forward).
//
//nolint:unparam // ctrl.Result kept so callers can `return r.patchTaskAwaitingApproval(...)` in the reconcile chain
func (r *TaskReconciler) patchTaskAwaitingApproval(ctx context.Context, task *tideprojectv1alpha3.Task, policy tideprojectv1alpha3.GatePolicy) (ctrl.Result, error) {
	reason := tideprojectv1alpha3.ReasonAwaitingApproval
	message := "Task awaiting operator approve annotation (tideproject.k8s/approve-task=true)"
	if policy == gates.PolicyPause {
		reason = tideprojectv1alpha3.ReasonPausedAtBoundary
		message = "Task paused at boundary; requires explicit resume"
	}
	patch := client.MergeFrom(task.DeepCopy())
	task.Status.Phase = "AwaitingApproval"
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, task, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// handleJobCompletion reads the EnvelopeOut, validates output paths, rolls up
// budget, and patches Task.Status to the terminal state.
//
//nolint:unparam // ctrl.Result kept so callers can `return r.handleJobCompletion(...)` in the reconcile chain
func (r *TaskReconciler) handleJobCompletion(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, completedJob *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// D-05: settle the reservation when this handler returns — i.e. AFTER
	// budget.RollUpUsage has landed the actual cost in CostSpentCents. Settling
	// before roll-up opened a window (spanning several API round-trips) where
	// TotalReserved had already dropped while CostSpentCents had not yet risen,
	// letting concurrent HasHeadroom checks dispatch past the cap. The defer
	// still covers every early-return Failed branch below. Settle is a no-op
	// when Reservations is nil or the UID is not in the store (idempotent —
	// safe on reconcile replay).
	defer r.Deps.Reservations.Settle(string(task.UID))

	// Read the EnvelopeOut from the PVC-backed reader (Blocker #2/#3 path).
	out, err := r.Deps.EnvReader.ReadOut(ctx, string(project.UID), string(task.UID))
	if err != nil {
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Failed"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "EnvelopeReadFailed",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		if patchErr := r.Status().Patch(ctx, task, patch); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		// Deliberate exclusion: EnvelopeReadFailed performs no budget.RollUpUsage
		// and no emitTaskMetrics — task counters keep strict parity with the D-12
		// budget-rollup commit points (seams 1 and 2 only, per CR-02 scope). Widening
		// to envelope-less failures is a deliberate non-goal of this gap closure.
		return ctrl.Result{}, nil
	}

	// Output-path validation (Warning #5 — wires HARN-05 into dispatch chain).
	// Performed controller-side in Phase 2 (RESEARCH.md Responsibility Map deviation).
	// Phase 3 moves validation into the Pod once the harness-wrapped runtime lands.
	if out.Result != outputPathsViolation && len(task.Spec.DeclaredOutputPaths) > 0 && task.Status.StartedAt != nil {
		taskWorkspaceRoot := fmt.Sprintf("/workspaces/%s/workspace", string(project.UID))
		violations, skipped, vErr := validateControllerOutputPaths(taskWorkspaceRoot, task.Status.StartedAt.Time, task.Spec.DeclaredOutputPaths)
		if vErr != nil {
			patch := client.MergeFrom(task.DeepCopy())
			task.Status.Phase = "Failed"
			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha3.ConditionFailed,
				Status:             metav1.ConditionTrue,
				Reason:             "OutputValidationError",
				Message:            vErr.Error(),
				LastTransitionTime: metav1.Now(),
			})
			if patchErr := r.Status().Patch(ctx, task, patch); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			// The envelope was read successfully — the session burned real
			// tokens before the validation error, so its usage still counts
			// against the cap (non-fatal, same pattern as the terminal roll-up
			// below). Skipping this silently dropped spend on failure-heavy runs.
			if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
				logger.Error(rollErr, "failed to roll up budget usage", "task", task.Name)
			}
			// Phase 38 COST-02: surface an unknown-model pricing fallback carried
			// on the envelope — condition + metric. Non-fatal: informational only.
			if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
				logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "task", task.Name)
			}
			// Emit six locked metrics at the same once-only terminal commit point as
			// budget.RollUpUsage — guarantees Prometheus cost totals never diverge from
			// Budget accounting (Phase 16 D-12). Non-fatal: task is already terminal.
			// OutputValidationError is a controller-side policy failure → "internal" reason.
			if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage, out.CompletedAt, "internal"); emitErr != nil {
				logger.Error(emitErr, "failed to emit task metrics (non-fatal)", "task", task.Name)
			}
			return ctrl.Result{}, nil
		}
		if skipped {
			logger.V(1).Info("skipped controller-side output validation; workspace root not visible", "task", task.Name, "workspaceRoot", taskWorkspaceRoot)
		}
		if len(violations) > 0 {
			msg := buildViolationMessage(violations)
			logger.Info("output path violations detected", "task", task.Name, "violations", len(violations))
			patch := client.MergeFrom(task.DeepCopy())
			task.Status.Phase = "Failed"
			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha3.ConditionFailed,
				Status:             metav1.ConditionTrue,
				Reason:             "OutputPathsViolation",
				Message:            msg,
				LastTransitionTime: metav1.Now(),
			})
			if patchErr := r.Status().Patch(ctx, task, patch); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			// Real token spend precedes the violation — roll it up so the cap
			// does not under-count on failure-heavy runs (non-fatal, same
			// pattern as the terminal roll-up below).
			if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
				logger.Error(rollErr, "failed to roll up budget usage", "task", task.Name)
			}
			// Phase 38 COST-02: surface an unknown-model pricing fallback carried
			// on the envelope — condition + metric. Non-fatal: informational only.
			if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
				logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "task", task.Name)
			}
			// Emit six locked metrics at the same once-only terminal commit point as
			// budget.RollUpUsage — guarantees Prometheus cost totals never diverge from
			// Budget accounting (Phase 16 D-12). Non-fatal: task is already terminal.
			// OutputPathsViolation is a controller-side policy failure → "internal" reason.
			if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage, out.CompletedAt, "internal"); emitErr != nil {
				logger.Error(emitErr, "failed to emit task metrics (non-fatal)", "task", task.Name)
			}
			return ctrl.Result{}, nil
		}
	}

	// Standard result interpretation.
	patch := client.MergeFrom(task.DeepCopy())
	if out.ExitCode != 0 || out.Result == "cap-hit" || out.Result == outputPathsViolation {
		task.Status.Phase = "Failed"
		reason := conditionReasonFromEnvelopeResult(out.Result, out.ExitCode)
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			Message:            fmt.Sprintf("Task failed: exitCode=%d result=%s", out.ExitCode, out.Result),
			LastTransitionTime: metav1.Now(),
		})
		// Phase 13 D-04 layer 2: backstop — classify the envelope's failure Reason
		// against the billing signature; if matched, stamp BillingHalt=True on the
		// owning Project. Non-fatal: the task's own terminal patch proceeds regardless
		// (pattern: budget.RollUpUsage error handling below).
		var jobStart time.Time
		if completedJob != nil {
			jobStart = completedJob.CreationTimestamp.Time
		}
		if hErr := setBillingHaltIfNeeded(ctx, r.Client, project, out.Reason, jobStart); hErr != nil {
			logger.Error(hErr, "setBillingHaltIfNeeded failed (non-fatal)", "task", task.Name)
		}
		// Phase 25 D-02b: conservative failure halt on task execution failure.
		// When Project.Spec.FailureProfile==conservative, stamp ConditionFailureHalt=True
		// so all four EXECUTION dispatch gates park new dispatch until the operator
		// runs `tide resume --retry-failed`. Non-fatal: the task's own terminal patch
		// proceeds regardless (mirrors setBillingHaltIfNeeded pattern above).
		// CR-02 resume time-fence: pass the envelope's CompletedAt (the same value
		// stamped into task.Status.CompletedAt below) so a failure predating
		// `tide resume --retry-failed` does not re-stamp the halt.
		if hErr := setFailureHaltIfNeeded(ctx, r.Client, project, out.CompletedAt); hErr != nil {
			logger.Error(hErr, "setFailureHaltIfNeeded failed (non-fatal)", "task", task.Name)
		}
	} else {
		task.Status.Phase = "Succeeded"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionSucceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "JobComplete",
			Message:            "Task completed successfully",
			LastTransitionTime: metav1.Now(),
		})
	}

	// Patch CompletedAt from the envelope.
	completedAt := metav1.NewTime(out.CompletedAt)
	task.Status.CompletedAt = &completedAt
	if err := r.Status().Patch(ctx, task, patch); err != nil {
		return ctrl.Result{}, err
	}

	// Roll up budget usage into Project.Status.Budget (D-D2).
	if err := budget.RollUpUsage(ctx, r.Client, project, out.Usage); err != nil {
		// Log but do not fail the reconcile — the task is already in terminal state.
		logger.Error(err, "failed to roll up budget usage", "task", task.Name)
	}
	// Phase 38 COST-02: surface an unknown-model pricing fallback carried on
	// the envelope — condition + metric. Non-fatal: informational only.
	if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
		logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "task", task.Name)
	}
	// Emit six locked metrics at the same once-only terminal commit point as
	// budget.RollUpUsage — guarantees Prometheus cost totals never diverge from
	// Budget accounting (Phase 16 D-12). Non-fatal: task is already terminal.
	// Compute the bounded metric reason from the envelope result; "" = Succeeded.
	var metricReason string
	if task.Status.Phase == "Failed" {
		metricReason = metricFailureReason(out.Result, out.ExitCode)
	}
	if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage, out.CompletedAt, metricReason); emitErr != nil {
		logger.Error(emitErr, "failed to emit task metrics (non-fatal)", "task", task.Name)
	}

	// Phase 14 BUDGET-02: stamp BudgetBlocked immediately after RollUpUsage — this
	// is the first moment where CostSpentCents may cross the cap (RESEARCH §Root Cause,
	// Architecture diagram). Bidirectional: also clears the condition when a cap raise
	// brings IsCapExceeded back to false. Non-fatal: the task is already terminal.
	if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.Deps.Reservations.TotalReserved()); err != nil {
		logger.Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)", "task", task.Name)
	}

	return ctrl.Result{}, nil
}

// resolveProject locates the owning Project for a Task via:
//  1. label fast-path (tideproject.k8s/project)
//  2. owner-ref chain walk (Task→Plan→Phase→Milestone→Project, bounded depth 5)
//  3. ErrParentUnresolved on miss (caller sets ConditionParentUnresolved)
//
// Phase 04.1 P1.4 removed the prior `projectList.Items[0]` fallback which
// silently mis-routed Tasks in multi-Project namespaces.
func (r *TaskReconciler) resolveProject(ctx context.Context, task *tideprojectv1alpha3.Task) (*tideprojectv1alpha3.Project, error) {
	// Fast path: PlanReconciler stamps tideproject.k8s/project=<name> on all Tasks.
	if projectName, ok := task.Labels["tideproject.k8s/project"]; ok && projectName != "" {
		var project tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: projectName}, &project); err == nil {
			return &project, nil
		}
	}
	// Owner-ref chain walk: Task→Plan→Phase→Milestone→Project (bounded depth 5).
	if parent, err := r.walkOwnerChainToProject(ctx, task); err == nil && parent != nil {
		return parent, nil
	}
	return nil, ErrParentUnresolved
}

// resolveWave returns the name of the Wave CRD that directly owns this Task.
// Tasks in the normal execution path are created by the wave controller with
// Wave as their controller OwnerRef (wave_controller.go SetControllerReference).
// Returns "unknown" on miss — D-09 sentinel; never emits an empty label value
// (Metric Label Sentinel, RESEARCH Pitfall 4). No API call needed — OwnerReferences
// are part of the in-memory Task object.
//
// Phase 23 SCHEMA-02 / D-08 resemantics: after Plan 23-02's materializeWaves
// re-ownership (WaveSpec.PlanRef → WaveSpec.ProjectRef; global wave index), the
// Wave owner-ref name is now the global wave identifier across the entire Project's
// Execution DAG — not a per-plan layer index. The metric `wave` label emitted by
// emitTaskMetrics (via this function) now carries the global wave index, satisfying
// the SCHEMA-02 lock: {project, phase, plan, wave} with `wave` = global wave name.
func (r *TaskReconciler) resolveWave(task *tideprojectv1alpha3.Task) string {
	for _, ref := range task.GetOwnerReferences() {
		if ref.Kind == "Wave" {
			return ref.Name
		}
	}
	return "unknown"
}

// metricFailureReason maps an envelope result string and exit code onto the
// bounded six-value reason enum documented in internal/metrics/registry.go:
//
//	cap-hit               → "budget"  (Phase 2 D-D2 billing halt)
//	output-paths-violation→ "internal" (policy violation surfaced controller-side)
//	any other + exitCode≠0 → "exit-1"  (subagent CLI non-zero without specific class)
//	default               → "internal" (defensive; only reachable if the :905
//	                                    failure predicate changes — treat as TIDE bug)
//
// T-16-21 (cardinality bomb): envelope free-text NEVER becomes a label value —
// this function is the only code path that produces reason label strings.
// conditionReasonFromEnvelopeResult is deliberately NOT reused here because it
// capitalises arbitrary envelope result strings and is unbounded (Pitfall 17).
func metricFailureReason(envelopeResult string, exitCode int) string {
	switch envelopeResult {
	case "cap-hit":
		return "budget"
	case outputPathsViolation:
		// Policy violation surfaced by the controller (not a CLI exit class).
		return "internal"
	default:
		if exitCode != 0 {
			return "exit-1"
		}
		// Defensive default — only reachable if the :905 failure predicate changes.
		return "internal"
	}
}

// emitTaskMetrics emits the six locked Phase 16 TELEM-03 metrics at the exact
// terminal commit point shared with budget.RollUpUsage (D-12). Resolves the
// four label values: project from the owning Project CRD name; phase from the
// Plan's PhaseRef (via r.Get — the tideproject.k8s/phase label does not exist
// on Tasks); plan from task.Spec.PlanRef; wave from resolveWave. Any resolution
// miss falls back to "unknown" (sentinel, D-09). Failures are non-fatal — the
// task is already in terminal state.
//
// failureReason is the bounded metric reason string (see metricFailureReason).
// Empty string signals Succeeded — increments TasksCompletedTotal. Non-empty
// increments TasksFailedTotal with the given reason. Note: TasksCompletedTotal
// and TasksFailedTotal carry only {project, phase, plan} (no wave label) per
// registry.go — arity differs from the six TELEM-03 metrics above.
//
//nolint:unparam // error return kept so callers can `if err := r.emitTaskMetrics(...); err != nil` in the reconcile chain
func (r *TaskReconciler) emitTaskMetrics(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, usage pkgdispatch.Usage, completedAt time.Time, failureReason string) error {
	logger := logf.FromContext(ctx)
	wave := r.resolveWave(task)
	projectName := project.Name

	// Resolve plan — fall back to "unknown" on empty PlanRef.
	plan := task.Spec.PlanRef
	if plan == "" {
		plan = "unknown"
	}

	// Resolve phase via PlanRef → plan.Spec.PhaseRef (PLANNER CORRECTION: there
	// is no tideproject.k8s/phase label on Tasks; the label does not exist in
	// the codebase). Fall back to "unknown" on any miss.
	phase := "unknown"
	if task.Spec.PlanRef != "" {
		var planObj tideprojectv1alpha3.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Spec.PlanRef}, &planObj); err == nil {
			if planObj.Spec.PhaseRef != "" {
				phase = planObj.Spec.PhaseRef
			}
		}
	}

	tidemetrics.TokensInputTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.InputTokens))
	tidemetrics.TokensOutputTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.OutputTokens))
	tidemetrics.TokensCacheReadTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheReadTokens))
	tidemetrics.TokensCacheCreationTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheCreationTokens))
	tidemetrics.CostCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.EstimatedCostCents))
	tidemetrics.CacheSavingsCentsTotal.WithLabelValues(projectName, phase, plan, wave).Add(float64(usage.CacheSavingsCents))

	// WR-04: guard against negative durations from stale envelopes or manager↔pod
	// clock skew. Compute a signed duration; only observe when d >= 0. On negative,
	// log at V(1) as a stale-envelope / clock-skew signal and skip the observation —
	// observing a negative value would drag the histogram _sum permanently negative.
	if task.Status.StartedAt != nil && !completedAt.IsZero() {
		d := completedAt.Sub(task.Status.StartedAt.Time)
		if d >= 0 {
			tidemetrics.TaskDurationSeconds.WithLabelValues(projectName, phase, plan, wave).Observe(d.Seconds())
		} else {
			logger.V(1).Info("skipping negative task duration (stale envelope or clock skew)",
				"task", task.Name,
				"startedAt", task.Status.StartedAt.Time,
				"completedAt", completedAt,
				"durationSeconds", d.Seconds())
		}
	}

	// CR-02: emit task completion/failure counters with {project, phase, plan} (3-label,
	// no wave). These counters power the Dispatch Counts and Failure Rate panels.
	if failureReason == "" {
		tidemetrics.TasksCompletedTotal.WithLabelValues(projectName, phase, plan).Inc()
	} else {
		tidemetrics.TasksFailedTotal.WithLabelValues(projectName, phase, plan, failureReason).Inc()
	}
	return nil
}

// walkOwnerChainToProject walks the owner-ref chain looking for a Project,
// bounded to depth 5 (Task→Plan→Phase→Milestone→Project). Returns nil, nil
// on miss (not an error — the caller decides whether the miss is terminal).
// Phase 04.1 P1.4.
func (r *TaskReconciler) walkOwnerChainToProject(ctx context.Context, obj client.Object) (*tideprojectv1alpha3.Project, error) {
	const maxDepth = 5
	return r.walkOwnerChainToProjectDepth(ctx, obj, maxDepth)
}

func (r *TaskReconciler) walkOwnerChainToProjectDepth(ctx context.Context, obj client.Object, depth int) (*tideprojectv1alpha3.Project, error) {
	if depth <= 0 || obj == nil {
		return nil, nil
	}
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Kind == "Project" && (ref.APIVersion == "tideproject.k8s/v1alpha1" || ref.APIVersion == tideprojectv1alpha3.GroupVersion.String()) {
			var p tideprojectv1alpha3.Project
			if err := r.Get(ctx, client.ObjectKey{Namespace: obj.GetNamespace(), Name: ref.Name}, &p); err == nil {
				return &p, nil
			}
			continue
		}
		// Recurse: fetch the parent by Kind and walk from there. Supported
		// intermediate Kinds: Plan, Phase, Milestone, Wave.
		parent, err := r.fetchTaskOwnerParent(ctx, obj.GetNamespace(), ref)
		if err != nil || parent == nil {
			continue
		}
		if p, err := r.walkOwnerChainToProjectDepth(ctx, parent, depth-1); err == nil && p != nil {
			return p, nil
		}
	}
	return nil, nil
}

// fetchTaskOwnerParent returns the parent CRD identified by an OwnerReference,
// or nil if the Kind is unknown. Bounded to TIDE Kinds (Plan/Phase/Milestone/Wave/Task).
func (r *TaskReconciler) fetchTaskOwnerParent(ctx context.Context, ns string, ref metav1.OwnerReference) (client.Object, error) {
	key := client.ObjectKey{Namespace: ns, Name: ref.Name}
	switch ref.Kind {
	case "Plan":
		var p tideprojectv1alpha3.Plan
		if err := r.Get(ctx, key, &p); err != nil {
			return nil, err
		}
		return &p, nil
	case "Phase":
		var p tideprojectv1alpha3.Phase
		if err := r.Get(ctx, key, &p); err != nil {
			return nil, err
		}
		return &p, nil
	case "Milestone":
		var p tideprojectv1alpha3.Milestone
		if err := r.Get(ctx, key, &p); err != nil {
			return nil, err
		}
		return &p, nil
	case "Wave":
		var p tideprojectv1alpha3.Wave
		if err := r.Get(ctx, key, &p); err != nil {
			return nil, err
		}
		return &p, nil
	case "Task":
		var p tideprojectv1alpha3.Task
		if err := r.Get(ctx, key, &p); err != nil {
			return nil, err
		}
		return &p, nil
	}
	return nil, nil
}

// listProjectTasks returns all Tasks in the same Project as task, identified
// by the owner.LabelProject label. This is the global sibling set consumed by
// computeGlobalIndegree to resolve DependsOn across plan/phase/milestone
// boundaries (DISP-01 D-01). projectName must be non-empty (Pitfall 2 guard).
func (r *TaskReconciler) listProjectTasks(ctx context.Context, task *tideprojectv1alpha3.Task, projectName string) ([]tideprojectv1alpha3.Task, error) {
	if projectName == "" {
		return nil, fmt.Errorf("listProjectTasks: projectName must be non-empty")
	}
	var taskList tideprojectv1alpha3.TaskList
	if err := r.List(ctx, &taskList,
		client.InNamespace(task.Namespace),
		client.MatchingLabels{owner.LabelProject: projectName},
	); err != nil {
		return nil, fmt.Errorf("list project tasks: %w", err)
	}
	return taskList.Items, nil
}

// computeGlobalIndegree returns the number of unsatisfied global predecessors
// for task. It builds the shared coarse-ref resolver from the four lists and
// expands each DependsOn entry to its member task set. A coarse ref (e.g. a
// Plan name) is satisfied only when EVERY member task has Phase==Succeeded.
//
// This replaces the old Name-only computeIndegree (D-F1 retirement) and
// routes through the same buildScopeResolver as assembleProjectDepGraph so
// the dispatch indegree and wave map can never disagree (D-01 invariant).
func (r *TaskReconciler) computeGlobalIndegree(
	ctx context.Context,
	task tideprojectv1alpha3.Task,
	allProjectTasks []tideprojectv1alpha3.Task,
	plans []tideprojectv1alpha3.Plan,
	phases []tideprojectv1alpha3.Phase,
	ms []tideprojectv1alpha3.Milestone,
) int {
	if len(task.Spec.DependsOn) == 0 {
		return 0
	}
	resolver := buildScopeResolver(allProjectTasks, plans, phases, ms)

	// Build a status-by-name map for O(1) Phase lookup.
	statusByName := make(map[string]string, len(allProjectTasks))
	for _, t := range allProjectTasks {
		statusByName[t.Name] = t.Status.Phase
	}

	indegree := 0
	for _, dep := range task.Spec.DependsOn {
		memberTasks := resolver.resolveScope(dep)
		if len(memberTasks) == 0 {
			// Unresolved ref: conservative — count as unsatisfied (never invents
			// a satisfied dependency on an unresolvable scope name).
			indegree++
			continue
		}
		// A coarse ref is satisfied only when ALL member tasks have Succeeded.
		for _, member := range memberTasks {
			if statusByName[member] != "Succeeded" {
				indegree++
				break // one unsatisfied member in this dep is enough
			}
		}
	}

	// WR-04: surface any cross-Kind scope-name collision encountered during
	// resolution. The union behavior in resolveScope keeps dispatch fail-closed
	// (no dropped edge), but an ambiguous DependsOn name is a configuration smell
	// worth logging for diagnosis.
	if names := resolver.collisionNames(); len(names) > 0 {
		logf.FromContext(ctx).V(1).Info(
			"computeGlobalIndegree: DependsOn scope name matched multiple Kind levels (Task/Plan/Phase/Milestone); unioning members to avoid dropping edges",
			"task", task.Name, "collidingNames", names)
	}

	return indegree
}

// nextAttempt returns the next attempt number (current max + 1, minimum 1).
// Lists all Jobs with label tideproject.k8s/task-uid=<task.UID>.
func (r *TaskReconciler) nextAttempt(ctx context.Context, task *tideprojectv1alpha3.Task) (int, error) {
	var jobList batchv1.JobList
	if err := r.List(ctx, &jobList,
		client.InNamespace(task.Namespace),
		client.MatchingLabels{"tideproject.k8s/task-uid": string(task.UID)},
	); err != nil {
		return 0, fmt.Errorf("list task jobs: %w", err)
	}
	maxAttempt := 0
	logger := logf.FromContext(ctx)
	for _, j := range jobList.Items {
		attempt, ok := j.Labels["tideproject.k8s/attempt"]
		if !ok {
			continue
		}
		// WR-03: strconv.Atoi rejects negative values, trailing garbage,
		// hex, etc.; an explicit n<0 check guards against a label value of
		// "-1" pulling the max-attempt tracking backwards. Malformed labels
		// are logged at V(1) so a parse failure is observable instead of
		// silently swallowed.
		n, err := strconv.Atoi(attempt)
		if err != nil || n < 0 {
			logger.V(1).Info("ignoring malformed attempt label", "job", j.Name, "value", attempt)
			continue
		}
		if n > maxAttempt {
			maxAttempt = n
		}
	}
	return maxAttempt + 1, nil
}

// gateDispatch handles the rate-limit gate using Pattern 1.
// Returns a non-zero delay when the bucket is exhausted (caller must RequeueAfter).
// Standalone helper satisfying plan's "helpers named in <interfaces>" requirement (grep target).
//
//nolint:unused // intentionally retained per plan <interfaces> grep contract; wired by a later phase
func (r *TaskReconciler) gateDispatch(projectName, secretUID string, limits budget.Limits) time.Duration {
	lim := r.Deps.Budget.ForSecret(secretUID, limits)
	if lim == nil {
		return 0
	}
	rsv := lim.Reserve()
	d := rsv.Delay()
	if d > 0 {
		rsv.Cancel()
		budget.ProviderRateLimitHitsTotal.WithLabelValues(projectName).Inc()
	}
	return d
}

// ensureJob builds and creates the Job for a Task dispatch.
// This is the helper referenced by the plan's grep contract.
//
//nolint:unused // intentionally retained per plan grep contract; wired by a later phase
func (r *TaskReconciler) ensureJob(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, attempt int, token string, envInJSON []byte) (*batchv1.Job, error) {
	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); err == nil {
			secretUID = string(secret.UID)
		}
	}
	// SIGN-01 / D-03: resolve committer/author identity (mirrors resolveImage's
	// r.Deps.HelmProviderDefaults tier) and stamp it into the subagent Job env.
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindExecutor,
		Task:                 task,
		ParentObj:            task,
		Level:                "task",
		Project:              project,
		Attempt:              attempt,
		SignedToken:          token,
		EnvelopeInJSON:       envInJSON,
		SubagentImage:        resolveImage(project, "task", r.Deps.HelmProviderDefaults),
		AgentName:            agentName,
		AgentEmail:           agentEmail,
		CredproxyImage:       r.Deps.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              "tide-projects",
		ProjectUID:           string(project.UID),
		EstimatedCostCents:   r.Deps.ReserveEstimateCents,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
	}
	job := podjob.BuildJobSpec(opts)
	if err := owner.EnsureOwnerRef(job, task, r.Scheme); err != nil {
		return nil, fmt.Errorf("ensure owner ref on job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("create job: %w", err)
		}
		// AlreadyExists: idempotent success — watch-lag race (Pitfall F / SUB-03).
	}
	// D-05: pre-charge the reservation after successful Job creation (including
	// AlreadyExists-as-success). Reserve only when a non-zero estimate is configured.
	if r.Deps.ReserveEstimateCents > 0 {
		r.Deps.Reservations.Reserve(string(task.UID), r.Deps.ReserveEstimateCents)
	}
	return job, nil
}

// defaultsForSecret derives the effective Limits for a Secret.
// Precedence: Secret annotation > r.Deps.Defaults (Helm defaults).
func (r *TaskReconciler) defaultsForSecret(secret *corev1.Secret) budget.Limits {
	if secret == nil {
		return r.Deps.Defaults
	}
	limits := r.Deps.Defaults
	if v, ok := secret.Annotations["tideproject.k8s/requests-per-minute"]; ok {
		var rpm int
		if _, err := fmt.Sscanf(v, "%d", &rpm); err == nil {
			limits.RequestsPerMinute = rpm
		}
	}
	return limits
}

// buildEnvelopeIn constructs and marshals the EnvelopeIn for this Task dispatch.
// Translates api/v1alpha1.Caps → pkg/dispatch.Caps per Plan 03's two-type design.
func (r *TaskReconciler) buildEnvelopeIn(_ context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, _ int, token string) (pkgdispatch.EnvelopeIn, []byte, error) {
	caps := pkgdispatch.Caps{}
	if task.Spec.Caps != nil {
		caps = pkgdispatch.Caps{
			WallClockSeconds: int(task.Spec.Caps.WallClockSeconds),
			Iterations:       int(task.Spec.Caps.Iterations),
			InputTokens:      task.Spec.Caps.InputTokens,
			OutputTokens:     task.Spec.Caps.OutputTokens,
		}
	}

	var dev *pkgdispatch.Dev
	if task.Spec.Dev.TestMode != "" {
		dev = &pkgdispatch.Dev{TestMode: task.Spec.Dev.TestMode}
	}

	// Defect #10b fix: stamp PromptPath on EnvelopeIn so the executor reads its
	// own prompt in-pod from the project-namespace PVC. The Manager MUST NOT
	// read the prompt cross-namespace (the old ReadPrompt call was the bug:
	// the Manager's /workspaces PVC is tide-system-local; the executor's PVC is
	// namespace-local to the project). The in-pod anthropic runner now owns the
	// read, which is same-namespace and therefore correct.
	envIn := pkgdispatch.EnvelopeIn{
		APIVersion:          pkgdispatch.APIVersionV1Alpha1,
		Kind:                pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:             string(task.UID),
		Role:                "executor",
		Level:               "task",
		PromptPath:          task.Spec.PromptPath,
		Branch:              project.Status.Git.BranchName,
		FilesTouched:        task.Spec.FilesTouched,
		DependsOn:           task.Spec.DependsOn,
		DeclaredOutputPaths: task.Spec.DeclaredOutputPaths,
		Caps:                caps,
		// Resolve the executor's ProviderSpec the same way the planner
		// reconcilers do (BuildPlannerEnvelope → ResolveProvider): Vendor pinned
		// to "anthropic" + the task-level model. Without this the envelope's
		// Provider is the zero value and the anthropic runner refuses the task
		// ("refusing vendor=\"\""). Latent until a run first reached real task
		// execution (the planner paths set Provider; this builder never did).
		Provider:      ResolveProvider(project, "task", r.Deps.HelmProviderDefaults),
		ProxyEndpoint: "https://127.0.0.1:8443",
		SignedToken:   token,
		Dev:           dev,
	}

	data, mErr := json.Marshal(envIn)
	if mErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal envelope in: %w", mErr)
	}
	return envIn, data, nil
}

// globalDependentsMapper re-enqueues all Tasks in the same project whose
// DependsOn contains the name of the changed Task OR any ancestor scope name
// (plan, phase, milestone) of the changed Task. This drives DISP-01: when a
// global predecessor completes, fails, or becomes AwaitingApproval, both direct-
// name and coarse-ref dependents re-evaluate readiness immediately — not waiting
// for the next periodic resync (the D-01 "never disagree" clause).
//
// Uses owner.LabelProject label (same as assembleProjectDepGraph) so cross-plan/
// phase/milestone dependents are covered. Resolves ancestor scope names through
// the shared buildScopeResolver so the mapper and computeGlobalIndegree agree
// on what constitutes a dependency edge.
//
// Guards: UID self-skip (Pitfall 1), empty projectName nil-return (Pitfall 2).
func (r *TaskReconciler) globalDependentsMapper(ctx context.Context, obj client.Object) []reconcile.Request {
	task, ok := obj.(*tideprojectv1alpha3.Task)
	if !ok {
		return nil
	}
	projectName := task.Labels[owner.LabelProject]
	if projectName == "" {
		// WR-01: an unlabeled predecessor (label not yet stamped / informer lag)
		// still has direct-name dependents that must re-enqueue on its completion —
		// otherwise they stall until the next periodic resync (~10h). We cannot
		// resolve this task's ancestor scope names without the labeled project set,
		// but direct-name deps are always resolvable from a namespace-wide list.
		logf.FromContext(ctx).V(1).Info(
			"globalDependentsMapper: changed task has no project label; falling back to namespace-wide direct-name dependents",
			"task", task.Name, "namespace", task.Namespace)
		return r.directNameDependents(ctx, task)
	}

	// List all tasks in the project for label query.
	var all tideprojectv1alpha3.TaskList
	if err := r.List(ctx, &all,
		client.InNamespace(task.Namespace),
		client.MatchingLabels{owner.LabelProject: projectName},
	); err != nil {
		return nil
	}

	// Build the shared resolver to determine the changed task's ancestor scope
	// names (plan, phase, milestone) — these are additional identifiers that a
	// coarse-ref dependent's DependsOn might name.
	var planList tideprojectv1alpha3.PlanList
	if err := r.List(ctx, &planList, client.InNamespace(task.Namespace)); err != nil {
		return nil
	}
	var phaseList tideprojectv1alpha3.PhaseList
	if err := r.List(ctx, &phaseList, client.InNamespace(task.Namespace)); err != nil {
		return nil
	}
	var msList tideprojectv1alpha3.MilestoneList
	if err := r.List(ctx, &msList, client.InNamespace(task.Namespace)); err != nil {
		return nil
	}

	resolver := buildScopeResolver(all.Items, planList.Items, phaseList.Items, msList.Items)
	planName, phaseName, msName := resolver.ancestorScopeNames(task.Spec.PlanRef)

	// Build the set of scope identifiers this task belongs to:
	//   - the task's own Name (direct-name deps)
	//   - its plan, phase, milestone names (coarse-ref deps)
	matchable := make(map[string]struct{}, 4)
	matchable[task.Name] = struct{}{}
	if planName != "" {
		matchable[planName] = struct{}{}
	}
	if phaseName != "" {
		matchable[phaseName] = struct{}{}
	}
	if msName != "" {
		matchable[msName] = struct{}{}
	}

	reqs := make([]reconcile.Request, 0)
	for _, t := range all.Items {
		if t.UID == task.UID { // skip self-enqueue (Pitfall 1)
			continue
		}
		for _, dep := range t.Spec.DependsOn {
			if _, ok := matchable[dep]; ok {
				reqs = append(reqs, reconcile.Request{
					NamespacedName: client.ObjectKey{Namespace: t.Namespace, Name: t.Name},
				})
				break
			}
		}
	}
	return reqs
}

// directNameDependents re-enqueues every Task in the changed task's namespace
// whose DependsOn names the changed task directly. Used by globalDependentsMapper
// as the WR-01 fallback when the changed (predecessor) task carries no project
// label: coarse-ref (plan/phase/milestone) resolution needs the labeled set, but
// direct-name edges are always resolvable from a namespace-wide list, so an
// unlabeled predecessor's direct dependents do not stall until the periodic
// resync. Self-skip by UID (Pitfall 1).
func (r *TaskReconciler) directNameDependents(ctx context.Context, task *tideprojectv1alpha3.Task) []reconcile.Request {
	var all tideprojectv1alpha3.TaskList
	if err := r.List(ctx, &all, client.InNamespace(task.Namespace)); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0)
	for i := range all.Items {
		t := &all.Items[i]
		if t.UID == task.UID {
			continue
		}
		if slices.Contains(t.Spec.DependsOn, task.Name) {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: client.ObjectKey{Namespace: t.Namespace, Name: t.Name},
			})
		}
	}
	return reqs
}

// buildViolationMessage builds a truncated human-readable message listing output
// path violations. Truncated to keep K8s Event message size manageable.
func buildViolationMessage(violations []string) string {
	const maxList = 5
	listed := violations
	suffix := ""
	if len(listed) > maxList {
		listed = violations[:maxList]
		suffix = fmt.Sprintf(" ... and %d more", len(violations)-maxList)
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "Output path violations (%d total):", len(violations))
	for _, v := range listed {
		msg.WriteString("\n  " + v)
	}
	return msg.String() + suffix
}

func validateControllerOutputPaths(workspaceRoot string, runStart time.Time, declared []string) ([]string, bool, error) {
	violations, err := harness.Validate(workspaceRoot, runStart, declared)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return violations, false, nil
}

func conditionReasonFromEnvelopeResult(result string, exitCode int) string {
	if result == "" {
		if exitCode != 0 {
			return "NonZeroExitCode"
		}
		return "JobComplete"
	}

	var b strings.Builder
	capitalizeNext := true
	wrote := false
	for _, r := range result {
		if isASCIIAlpha(r) || isASCIIDigit(r) {
			if !wrote && !isASCIIAlpha(r) {
				b.WriteString("Result")
			}
			if capitalizeNext && isASCIIAlpha(r) {
				r = toASCIIUpper(r)
			}
			b.WriteRune(r)
			wrote = true
			capitalizeNext = false
			continue
		}
		capitalizeNext = true
	}
	if !wrote {
		return "EnvelopeResult"
	}
	return b.String()
}

func isASCIIAlpha(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func toASCIIUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// isJobTerminal returns true if the Job has a Complete or Failed condition with Status=True.
func isJobTerminal(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			if c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed {
				return true
			}
		}
	}
	return false
}

// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02, a
// newStatusPhaseOrDepsChangedPredicate returns a predicate.Predicate that fires
// only on Task UpdateEvents where the Status.Phase or Spec.DependsOn changed.
// This is the WR-02 guard on globalDependentsMapper's Task watch: no-op
// resourceVersion-only bumps are excluded so global re-derivation is not
// triggered spuriously on every controller-manager heartbeat or label patch.
//
// Conservative contract: if either object fails the *Task type assertion, the
// predicate returns true so the event is never silently dropped.
// CreateFunc and DeleteFunc always return true (new/removed tasks must be
// reflected in the global indegree map). GenericFunc returns false.
//
// Exported as newStatusPhaseOrDepsChangedPredicate (lowercase package-internal
// constructor) so the unit test in task_controller_predicate_test.go can
// exercise the firing matrix without going through SetupWithManager.
func newStatusPhaseOrDepsChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldT, ok1 := e.ObjectOld.(*tideprojectv1alpha3.Task)
			newT, ok2 := e.ObjectNew.(*tideprojectv1alpha3.Task)
			if !ok1 || !ok2 {
				return true // conservative: let untyped events through
			}
			return oldT.Status.Phase != newT.Status.Phase ||
				!slices.Equal(oldT.Spec.DependsOn, newT.Spec.DependsOn)
		},
		CreateFunc:  func(event.CreateEvent) bool { return true },
		DeleteFunc:  func(event.DeleteEvent) bool { return true },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// namespace-filter predicate per AUTH-02, the .spec.planRef field indexer
// (RESEARCH.md Open Question #8; still needed by checkParentApproval), and a
// global Task watch (globalDependentsMapper) for DISP-01 cross-plan readiness
// re-enqueue (Phase 25 replaces the plan-local siblingsToTaskMapper).
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Register .spec.planRef field indexer so checkParentApproval can use MatchingFields.
	// (listProjectTasks uses label matching; the field indexer is kept for checkParentApproval.)
	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tideprojectv1alpha3.Task{},
		taskPlanRefIndexKey,
		func(obj client.Object) []string {
			task := obj.(*tideprojectv1alpha3.Task) //nolint:forcetypeassert // type guaranteed by IndexField
			return []string{task.Spec.PlanRef}
		},
	); err != nil {
		return fmt.Errorf("register .spec.planRef field indexer: %w", err)
	}

	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	// Plan 04-05: AnnotationChangedPredicate is wired via a self-Watches with
	// a permissive mapper instead of as a For()-level predicate. This is
	// deliberate: a For()-level GenerationChangedPredicate Or
	// AnnotationChangedPredicate filters out the post-finalizer Update event
	// (finalizer is metadata; Generation does not bump; annotations are
	// unchanged), stalling dispatch in integration tests where the manager's
	// auto-reconcile is the only driver. The self-Watches re-enqueues the
	// Task on annotation changes without filtering Spec/finalizer/owner-ref
	// updates from the default For() event stream.
	annotationOnly := predicate.AnnotationChangedPredicate{}
	statusPhaseOrDepsChanged := newStatusPhaseOrDepsChangedPredicate() // WR-02
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha3.Task{}).
		Owns(&batchv1.Job{}).
		Watches(
			&tideprojectv1alpha3.Task{},
			handler.EnqueueRequestsFromMapFunc(r.globalDependentsMapper),
			builder.WithPredicates(statusPhaseOrDepsChanged), // WR-02
		).
		Watches(
			&tideprojectv1alpha3.Task{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(annotationOnly),
		).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("task").
		Complete(r)
}
