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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
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
	"github.com/jsquirrelz/tide/internal/subagent/common"
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

// jobReasonDeadlineExceeded is the batchv1 JobFailed condition Reason set when
// a Job's ActiveDeadlineSeconds wall-clock cap SIGKILLs the pod before it can
// write out.json. It is the ONLY failure reason mapped to cap_exceeded in the
// no-envelope synthesis path (fail-closed — every other reason stays
// unclassified). Shared by the Task, Plan-check, and level-verify
// no-envelope synthesizers.
const jobReasonDeadlineExceeded = "DeadlineExceeded"

// reasonWallClockCapExceeded is the EnvelopeOut.Reason text the no-envelope
// synthesizers stamp for a wall-clock (ActiveDeadlineSeconds) kill.
const reasonWallClockCapExceeded = "wall-clock cap exceeded (ActiveDeadlineSeconds): pod was SIGKILLed before it could write out.json"

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
	EnvReader      podjob.EnvelopeReader
	Recorder       record.EventRecorder
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

	// ReporterImage is the image ref for the trace-only tide-reporter Job
	// spawned at Task completion (Phase 44 MSG-01); empty = spawn skipped —
	// mirrors PlannerReconcilerDeps.ReporterImage.
	ReporterImage string

	// OTLPEndpoint is the D-06 spawn gate + Job-env forwarding value; empty =
	// no trace-only spawns, zero Job churn on plain clusters.
	OTLPEndpoint string

	// OTLPHeadersSecret carries the per-project-namespace headers-Secret NAME
	// (never the decoded value) forwarded into the trace-only reporter Job
	// env as a secretKeyRef, so its TracerProvider authenticates to the same
	// auth-enabled collector the manager uses (Phase 47 PHX-02/D-08); empty =
	// no headers env, mirrors OTLPEndpoint.
	OTLPHeadersSecret string

	// VerifierImage is the image ref for the tide-langgraph-verifier subagent
	// container (Phase 51 TASK-04/RESEARCH A5), mirroring HelmProviderDefaults'
	// role for the executor/planner Image resolution. A dev-head default is
	// wired at Manager startup; the chart-configurable surface (CFG-01) is
	// Phase 53 — this field only needs to exist and reach BuildJobSpec today.
	VerifierImage string

	// VerifyDefaults carries the Helm-chart-supplied verify-tier defaults
	// (D-01/D-04) — evaluator model + per-level enablement/LoopPolicy
	// defaults — consumed by verificationEnabledForLevel and the Phase-52
	// resolvers. Assigned from the same construction site in main.go as
	// PlannerReconcilerDeps.VerifyDefaults; no dispatch-site behavior
	// changes in this plan — the AND gates land in 53-06.
	VerifyDefaults VerifyDefaults

	// TidePushImage is the image ref for the tide-push container; empty =
	// trigger skipped — mirrors ProjectReconciler's field (main.go:454) /
	// PlannerReconcilerDeps.TidePushImage. Consumed by plan 53-10's Task
	// verdict-final findings-push trigger (maybeTriggerTaskFindingsPush),
	// which reuses the SAME triggerArtifactPush machinery the planner tier
	// already dispatches through — no second push mechanism.
	TidePushImage string
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

	// SharedPVCName is the name of the cluster-wide PVC provisioned by the
	// Helm chart (Plan 12). Defaults to "tide-projects". Configurable via
	// --workspaces-pvc-name flag on the manager (Blocker #2/#3 architecture).
	SharedPVCName string

	// Deps carries the dispatch-tier dependencies. Phase 04.1 P3.2 — mirrors the
	// HelmProviderDefaults precedent on Milestone/Phase/Plan reconcilers.
	Deps TaskReconcilerDeps
}

// sharedPVCName returns the configured shared PVC name or the default.
func (r *TaskReconciler) sharedPVCName() string {
	if r.SharedPVCName != "" {
		return r.SharedPVCName
	}
	return defaultSharedPVCName
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
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseSucceeded {
		return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
	}
	// Step 1a (Phase 51 ESC-03/HI-01): VerifyHalted is a DISTINCT terminal
	// halt class — it must NOT flow through the Failed short-circuit below,
	// because that would stamp the conservative-profile ConditionFailureHalt
	// (setFailureHaltIfNeeded) on top of the ConditionVerifyHalt haltVerify
	// already stamped, and drag the Task into Failed-wave dependent semantics.
	// A VerifyHalt already froze new dispatch project-wide via
	// ConditionVerifyHalt (checkDispatchHolds/gateChecks); this Task simply
	// halts here with no additional failure propagation. Recovery is
	// `tide resume` (clears ConditionVerifyHalt), never `--retry-failed`.
	// See handleVerifyHaltedTerminal for the Plan 53-10 terminal-arm retry
	// detail (extracted from here to keep gateChecks under the D-10 gocyclo
	// threshold — no behavior change).
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifyHalted {
		return r.handleVerifyHaltedTerminal(ctx, task), nil
	}
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseFailed {
		// Phase 25 D-02b: conservative failure halt check at terminal short-circuit.
		// See handleFailedTerminal (extracted from here for the same D-10
		// gocyclo reason as handleVerifyHaltedTerminal above — no behavior
		// change).
		return r.handleFailedTerminal(ctx, task), nil
	}

	// Step 1b (Phase 52 D-08): a Task parked at AwaitingApproval via
	// exhaustVerifyLoop's requireApproval branch — LoopStatus.ExitReason
	// non-empty is the reachability signal (applyLoopStatus's Phase 51-07
	// contract: it is stamped only at a verify-loop terminal/park
	// transition, never cleared back to "" while the loop is active) — must
	// be RE-EVALUATED every reconcile, exactly like checkReadinessGates' own
	// Spec.Gates.Task-keyed gate-policy park below (Step 5). Unlike that
	// path, THIS park is triggered by loop exhaustion, not gate policy, so
	// it needs its own re-check here: without it, dispatch would silently
	// resume on the very next reconcile regardless of operator approval,
	// since checkReadinessGates' gate-policy branch only fires when
	// Spec.Gates.Task/the project default is explicitly "approve"/"pause"
	// (PolicyAuto — the common default — skips it entirely). Re-uses the
	// SAME tideproject.k8s/approve-task annotation + consumeApproveAndResume
	// two-step every other AwaitingApproval park already uses (D-08's
	// "existing gate machinery" instruction, ESC-02) — an ordinary
	// gate-policy park's own LoopStatus.ExitReason always stays empty, so
	// this branch never fires for it.
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval && task.Status.LoopStatus.ExitReason != "" {
		if !gates.CheckApprove(task, "task") {
			return taskGateResult{shouldHalt: true, result: ctrl.Result{}}, nil
		}
		result, err := consumeApproveAndResume(ctx, r.Client, task, &task.Status.Conditions, &task.Status.Phase, "task",
			"Operator approved; verify-exhaustion park lifted")
		return taskGateResult{shouldHalt: true, result: result}, err
	}

	// Step 2: On Running — delegate to checkRunningState.
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseRunning {
		// T-52-15 post-approval sentinel: task.Status.LoopStatus.ExitReason is
		// stamped by applyLoopStatus/exhaustVerifyLoop exactly once the verify
		// loop has taken a terminal or park decision — the live Task loop
		// itself NEVER leaves it non-empty while dispatch is genuinely active
		// (Phase 51-07's applyLoopStatus contract). So a Running Task
		// reconciling with a NON-EMPTY ExitReason is only reachable one way:
		// Step 1b's consumeApproveAndResume just resumed a parked
		// (requireApproval-exhausted) Task, which patches Phase back to
		// Running without ever touching LoopStatus. Route straight to
		// markVerifiedSucceeded (accept-the-findings semantics) BEFORE
		// checkRunningState can look up and re-process the executor Job that
		// already completed this attempt — otherwise handleJobCompletion
		// would re-run and re-dispatch a verifier against a Job the loop
		// already consumed (T-52-15: resurrecting paid work without human
		// intent).
		if task.Status.LoopStatus.ExitReason != "" {
			project, pErr := r.resolveProject(ctx, task)
			if errors.Is(pErr, ErrParentUnresolved) {
				return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
			}
			if pErr != nil {
				return taskGateResult{}, pErr
			}
			result, mErr := r.markVerifiedSucceeded(ctx, task, project, pkgdispatch.EnvelopeOut{})
			return taskGateResult{shouldHalt: true, result: result}, mErr
		}
		return r.checkRunningState(ctx, task)
	}

	// Step 2b (Phase 51 TASK-01/A4): On Verifying — delegate to
	// checkVerifyingState. A contract-bearing Task's executor already
	// exited 0 and dispatched an independent verifier (handleJobCompletion);
	// this Task must never fall through to a duplicate executor re-dispatch
	// while verification is outstanding.
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying {
		return r.checkVerifyingState(ctx, task)
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

	// Item 7 (Phase 41 D-07 → Phase 51 D-09): this gate chain now DELEGATES the
	// shared project-scoped hold chain to checkDispatchHolds (dispatch_helpers.go)
	// below, normalizing Task onto the planner tier's order — Billing → Failure →
	// Verify → Budget → Import — closing
	// .planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md
	// (Option 1). Task still adds two task-only holds with no planner-tier
	// counterpart — the legacy BudgetExceeded phase fallback and the BUDGET-03
	// reservation-headroom check — both run AFTER the delegated chain below.

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

	// Phase 14 BUDGET-02 / D-04: refresh BudgetBlocked BEFORE the shared chain
	// below reads it (checkDispatchHolds' Budget arm is read-only). Cap detection
	// happens here (not in ProjectReconciler) because Status patches from
	// RollUpUsage do NOT increment metadata.generation and thus do NOT
	// re-enqueue the ProjectReconciler (watch-predicate gap root cause,
	// 14-RESEARCH.md §Root Cause). setBudgetBlockedIfNeeded is bidirectional and
	// also handles cap-raise recovery: when IsCapExceeded becomes false it
	// clears the condition so dispatch can resume.
	if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.Deps.Reservations.TotalReserved()); err != nil {
		logf.FromContext(ctx).Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)")
	}

	// Phase 51 D-09: delegate Billing → Failure → Verify → Budget → Import to the
	// shared planner-tier chain (dispatch_helpers.go), normalizing Task onto the
	// same order and requeue intervals as Milestone/Phase/Plan. This is a
	// deliberate, tested behavior change from Task's prior Import-checked-SECOND
	// order — see co_occurring_holds_test.go.
	if held, result := checkDispatchHolds(ctx, project, "task", task.Name); held {
		return taskGateResult{shouldHalt: true, result: result}, nil
	}

	// Task-only: legacy BudgetExceeded phase fallback (pre-Phase-14 mechanism).
	// checkDispatchHolds has no counterpart for this — the BudgetBlocked
	// condition check inside it is the primary path; this fallback ensures
	// tasks parked by the pre-Phase-14 phase gate continue to be held.
	if project.Status.Phase == tideprojectv1alpha3.PhaseBudgetExceeded && !budget.IsBypassed(project, time.Now()) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked (legacy phase)",
			"task", task.Name, "project", project.Name,
			"spent", project.Status.Budget.CostSpentCents,
			"cap", project.Spec.Budget.AbsoluteCapCents)
		return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
	}

	// Task-only: Phase 14 BUDGET-03 / D-05 reservation headroom check. Prevents
	// wave-wide overshoot (run-1 root class) by gating dispatch when committed
	// spend + reserved + this estimate would exceed the cap. checkDispatchHolds
	// has NO planner-tier counterpart for this (dispatch_helpers.go documents it
	// as task-only) — it MUST survive this delegation, never be silently
	// dropped. Transient park — no per-Task condition stamp (not a cap breach,
	// just insufficient headroom at this moment).
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

// handleVerifyHaltedTerminal handles gateChecks' Step 1a VerifyHalted
// terminal short-circuit (Phase 51 ESC-03/HI-01). Extracted from gateChecks
// (Plan 53-09 D-10) to keep its cyclomatic complexity under the gocyclo
// threshold — a pure refactor, no behavior change.
//
// Plan 53-10 terminal-arm retry: re-runs the findings-push trigger on every
// reconcile of an already-halted Task so the carried-entry edge gate
// converges even if haltVerify's own verdict-final call raced a busy push
// Job. Converts the empty ctrl.Result{} into a 5s RequeueAfter ONLY while
// the entry is not yet carried by any push Job; once carried, steady state
// — no further requeue, no churn (T-53-23). Project resolution is
// best-effort, same posture as handleFailedTerminal: an unresolvable
// project just skips this reconcile's retry and tries again next time.
func (r *TaskReconciler) handleVerifyHaltedTerminal(ctx context.Context, task *tideprojectv1alpha3.Task) taskGateResult {
	result := ctrl.Result{}
	if project, pErr := r.resolveProject(ctx, task); pErr == nil && project != nil {
		carried, pushErr := r.maybeTriggerTaskFindingsPush(ctx, task, project)
		if pushErr != nil {
			logf.FromContext(ctx).Error(pushErr, "verdict-final findings push trigger failed at VerifyHalted terminal (non-fatal)", "task", task.Name)
		}
		if !carried {
			result = ctrl.Result{RequeueAfter: 5 * time.Second}
		}
	}
	return taskGateResult{shouldHalt: true, result: result}
}

// handleFailedTerminal handles gateChecks' Step 1 Failed terminal
// short-circuit (Phase 25 D-02b conservative failure halt). Extracted from
// gateChecks (Plan 53-09 D-10) for the same gocyclo reason as
// handleVerifyHaltedTerminal — no behavior change.
//
// A Failed task re-triggers the reconciler on every status change; this
// hook stamps ConditionFailureHalt on the Project when
// FailureProfile==conservative. Idempotent: setFailureHaltIfNeeded is a
// no-op if the condition is already True. Project resolution is
// best-effort here; if the project is unresolvable (transient), the halt
// fires when the task is next reconciled. Non-fatal: dispatch for this
// task has already halted.
func (r *TaskReconciler) handleFailedTerminal(ctx context.Context, task *tideprojectv1alpha3.Task) taskGateResult {
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
	return taskGateResult{shouldHalt: true, result: ctrl.Result{}}
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
		task.Status.Phase = tideprojectv1alpha3.LevelPhasePending
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
	if _, paused := task.Labels[owner.LabelWavePaused]; paused {
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
		// NOT migrated to consumeApproveAndResume (level_status.go, Phase 41 item 10):
		// this guard is inverted (annotation-present is the fallthrough arm here, not
		// the early-return arm) and, unlike the six planner-tier sites, Task never
		// wrote Status.Phase=Running/ConditionWaveOrLevelPaused=False here — a Task
		// gated at AwaitingApproval never reaches this point without also being
		// dispatch-ready, so only the annotation needs consuming. Not byte-equivalent
		// to the shared two-step; kept inline per plan 41-07 Task 2 discretion.
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

// checkVerifyingState handles a Task in Phase=Verifying (Phase 51 TASK-01):
// looks up the deterministic verifier Job for the current attempt and either
// retries a deferred dispatch, waits for it to complete, or (BACKWARD half,
// Plan 07) consumes its verdict via handleVerifierCompletion.
//
// Unlike checkRunningState's executor lookup — where NotFound is a genuine
// anomaly (Phase=Running is only ever reached AFTER a Job-create attempt
// succeeded or AlreadyExists'd, in the SAME transaction) — a verifier Job can
// legitimately be absent here: dispatchVerifier's ESC-04 concurrency cap may
// have deferred the very first attempt (Pitfall 6 — the Task already
// transitioned to Verifying before the cap check ran, so nothing retries the
// dispatch except this handler). NotFound therefore retries dispatchVerifier
// (idempotent: podjob.VerifierJobName is deterministic and AlreadyExists on
// Create is treated as success, SUB-03) rather than halting forever.
func (r *TaskReconciler) checkVerifyingState(ctx context.Context, task *tideprojectv1alpha3.Task) (taskGateResult, error) {
	jobName := podjob.VerifierJobName("task", string(task.UID), task.Status.Attempt)
	var job batchv1.Job
	if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: jobName}, &job); err != nil {
		if !apierrors.IsNotFound(err) {
			return taskGateResult{}, err
		}
		project, pErr := r.resolveProject(ctx, task)
		if errors.Is(pErr, ErrParentUnresolved) {
			return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
		}
		if pErr != nil {
			return taskGateResult{}, pErr
		}
		result, _, dErr := r.dispatchVerifier(ctx, task, project)
		return taskGateResult{shouldHalt: true, result: result}, dErr
	}
	if isJobTerminal(&job) {
		project, pErr := r.resolveProject(ctx, task)
		if errors.Is(pErr, ErrParentUnresolved) {
			return taskGateResult{shouldHalt: true, result: ctrl.Result{RequeueAfter: 30 * time.Second}}, nil
		}
		if pErr != nil {
			return taskGateResult{}, pErr
		}
		result, hErr := r.handleVerifierCompletion(ctx, task, project, &job)
		return taskGateResult{shouldHalt: true, result: result}, hErr
	}
	// Still running: nothing to do this reconcile — the Job watch fires again
	// on the verifier Job's terminal transition (Owns(&batchv1.Job{})).
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
		task.Status.Phase = tideprojectv1alpha3.LevelPhaseFailed
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

	// Step 9: Build EnvelopeIn; translate api/v1alpha3.Caps → pkg/dispatch.Caps per Plan 03.
	// "" evidencePacketPath: this is the Task's first/plain dispatch, never a
	// TASK-02 repair re-attempt (those go through dispatchRepairAttempt).
	_, envInJSON, err := r.buildEnvelopeIn(ctx, task, project, attempt, token, "")
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
	resolvedImage := resolveImage(project, "task", r.Deps.HelmProviderDefaults)
	resolvedModel := ResolveProvider(project, "task", r.Deps.HelmProviderDefaults).Model
	// D-02 / T-40-12: log the resolved model at dispatch — previously the
	// resolved model appeared nowhere outside the PVC envelope.
	logger.Info("resolved subagent dispatch", "level", "task", "model", resolvedModel, "image", resolvedImage)
	// PROP-01: Task's dispatch-hop TRACEPARENT is sourced from the IMMEDIATE
	// PARENT's (Plan's) persisted span ID — resolveProject's label fast-path
	// never touches Plan, so this is a genuinely new fetch (mirrors Plan's own
	// Phase fetch, RESEARCH.md A3). A missing PlanRef or a failed Get degrades
	// to an empty TraceParent (traceparentForLevel/FormatTraceparent already
	// return "" for a zero/invalid parent) rather than blocking dispatch.
	var parentPlanSpanHex string
	if task.Spec.PlanRef != "" {
		var parentPlan tideprojectv1alpha3.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Spec.PlanRef}, &parentPlan); err == nil {
			parentPlanSpanHex = parentPlan.Status.PlanTraceSpanID
		}
	}
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindExecutor,
		Task:                 task,
		ParentObj:            task,
		Level:                "task",
		Project:              project,
		Attempt:              spec.attempt,
		SignedToken:          spec.token,
		EnvelopeInJSON:       spec.envInJSON,
		SubagentImage:        resolvedImage,
		AgentName:            agentName,
		AgentEmail:           agentEmail,
		CredproxyImage:       r.Deps.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              r.sharedPVCName(),
		ProjectUID:           string(project.UID),
		EstimatedCostCents:   r.Deps.ReserveEstimateCents,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
		// D-02/Phase 46: literal true — cross-reconcile dispatch-time site;
		// the parent's sampled bit is not persisted (RESEARCH Pitfall 3's
		// rejected schema change; see docs/observability.md).
		TraceParent: traceparentForLevel(project, parentPlanSpanHex, true),
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
		task.Status.Phase = tideprojectv1alpha3.LevelPhaseRunning
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
	return patchLevelStatus(ctx, r.Client, task, &task.Status.Conditions, nil, "", false, ctrl.Result{RequeueAfter: 5 * time.Second},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionTrue,
			Reason:  tideprojectv1alpha3.ReasonRejectedByUser,
			Message: fmt.Sprintf("Rejected: %s", reason),
		},
	)
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
	// Unlike Milestone/Phase/Plan's AwaitingApproval wrappers, Task uses a plain
	// MergeFrom (no optimistic lock) here — preserved as-is (leaf param false).
	return patchLevelStatus(ctx, r.Client, task, &task.Status.Conditions, &task.Status.Phase, tideprojectv1alpha3.LevelPhaseAwaitingApproval, false, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	)
}

// emitTaskSpanOnce synthesizes at most one retroactive AGENT span per
// executor Job attempt (TRACE-01), mirroring the four planner levels'
// marker-gated call sites — Plan's is the closest receiver-type analog
// (43-PATTERNS.md). Gated by the TaskSpanEmittedUID marker keyed by
// completedJob.UID (D-02/D-04, generalized from the four-level pattern);
// mark-then-emit ordering (42-REVIEW WR-01): the marker is stamped durably
// BEFORE emission, so a crash between stamp and emission loses only that
// attempt's span, never double-counts.
//
// Generalized Option B (43-CONTEXT decision / RESEARCH Pitfall 1-A2): this
// method is called from BOTH of handleJobCompletion's envelope-dependent
// terminal sites — once from the EnvelopeReadFailed branch with
// envReadOK=false, once immediately after a successful envelope read
// (before the OutputValidationError/OutputPathsViolation/standard-result
// branch divergence) with envReadOK=true — so every one of Task's four
// terminal paths gets exactly one span.
//
// Returns the sampled bit (D-02, Phase 46): the real value from
// synthesizePlannerSpan when this call emitted a span, or true on every
// early-return path (already-stamped marker, unresolvable Job, nil
// project/completedJob, or a marker-patch failure) — matching the same
// "default true" convention the four planner completion handlers use, so
// spawnTaskTraceReporterIfNeeded's traceparentForLevel call always receives
// a defined value.
func (r *TaskReconciler) emitTaskSpanOnce(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, completedJob *batchv1.Job, out pkgdispatch.EnvelopeOut, envReadOK bool) bool {
	logger := logf.FromContext(ctx)

	if completedJob == nil || project == nil || task.Status.TaskSpanEmittedUID == string(completedJob.UID) || !plannerSpanResolvable(completedJob) {
		return true
	}

	stamped := false
	if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &tideprojectv1alpha3.Task{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(task), latest); err != nil {
			return err
		}
		if latest.Status.TaskSpanEmittedUID == string(completedJob.UID) {
			return nil // already stamped by a concurrent reconcile — its stamper emits
		}
		markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
		latest.Status.TaskSpanEmittedUID = string(completedJob.UID)
		if err := r.Status().Patch(ctx, latest, markerPatch); err != nil {
			return err
		}
		stamped = true
		return nil
	}); mErr != nil {
		logger.Error(mErr, "TaskSpanEmittedUID marker patch failed (non-fatal); span deferred to a later reconcile", "task", task.Name)
		return true
	}
	if !stamped {
		return true
	}

	// TRACE-02: Task's immediate parent is Plan — resolveProject's label
	// fast-path never touches Plan, so this is a genuinely new fetch (mirrors
	// Plan's own Phase fetch, RESEARCH.md A3). A missing PlanRef or a failed
	// Get degrades to an unnested span (zero parentSpanID) rather than
	// blocking emission — the span still groups by the deterministic TraceID.
	var parentSpanID trace.SpanID
	if task.Spec.PlanRef != "" {
		var parentPlan tideprojectv1alpha3.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Spec.PlanRef}, &parentPlan); err == nil {
			parentSpanID = spanIDFromHexOrZero(parentPlan.Status.PlanTraceSpanID)
		}
	}

	thisSpanID, sampled, emitted := synthesizePlannerSpan(ctx, "task", task.Name, task.Labels[owner.LabelWaveIndex], project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK, parentSpanID)
	if !emitted {
		return true
	}
	// Mirror in-memory unconditionally so same-reconcile downstream logic
	// reads it even if the persistence patch below fails.
	task.Status.TaskTraceSpanID = thisSpanID.String()
	if tErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &tideprojectv1alpha3.Task{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(task), latest); err != nil {
			return err
		}
		tracePatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
		latest.Status.TaskTraceSpanID = thisSpanID.String()
		return r.Status().Patch(ctx, latest, tracePatch)
	}); tErr != nil {
		// PROP-02/Pitfall 2: non-fatal — this is a SEPARATE, later patch from
		// the marker stamp above (the span ID isn't known until
		// synthesizePlannerSpan returns).
		logger.Error(tErr, "TaskTraceSpanID patch failed (non-fatal); child parent-linkage degraded for this level", "task", task.Name)
	}
	return sampled
}

// spawnTaskTraceReporterIfNeeded idempotently spawns a Phase 44 MSG-01
// trace-only tide-reporter Job for a completed Task dispatch Job attempt —
// the Task level's first in-namespace consumer of its own conversation
// (D-02/D-05: every terminal path, success and failure, gets a spawn attempt).
// Observability never gates Task's terminal-state machinery — contrast
// spawnReporterIfNeeded's error return: every failure here logs and
// continues, never a requeue error, so a broken trace-only spawn path can
// never wedge Task completion.
//
// Guards, in order: a nil completedJob or project skips (nothing resolvable
// to spawn against); r.Deps.OTLPEndpoint == "" skips BEFORE any API call
// (D-06 — the same value forwarded into the Job env; zero Job churn on plain
// clusters); r.Deps.ReporterImage == "" skips (no image configured).
//
// Idempotency: the Job name is deterministic per completed-Job-attempt
// ("tide-reporter-trace-"+completedJob.UID) — a retried Task Job has a new
// UID and so gets its own trace-only spawn (its own conversation span set),
// while a re-reconcile of the SAME completed Job finds the existing
// trace-only Job and skips (Get→NotFound→Create, AlreadyExists on Create is
// idempotent success — mirrors spawnReporterIfNeeded's shape).
//
// TraceParent: emitTaskSpanOnce mirrors TaskTraceSpanID onto the in-memory
// task object unconditionally after emission, in the SAME reconcile this
// method is called from immediately after — so reading task.Status here is
// correct even before emitTaskSpanOnce's own persistence patch lands. An
// empty span ID degrades to traceparentForLevel returning "" — the Job omits
// --traceparent and the reporter emits unparented spans (bounded
// degradation, matching Phase 43's precedent).
//
// sampled (D-02, Phase 46) is the bit emitTaskSpanOnce returned in the SAME
// reconcile, immediately before this call — the real value when this
// reconcile emitted Task's own AGENT span, or the default-true fallback on
// every early-return path (mirrors the four planner completion handlers'
// local-variable threading).
func (r *TaskReconciler) spawnTaskTraceReporterIfNeeded(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, completedJob *batchv1.Job, sampled bool) {
	logger := logf.FromContext(ctx)

	if completedJob == nil || project == nil {
		return
	}
	if r.Deps.OTLPEndpoint == "" {
		return
	}
	if r.Deps.ReporterImage == "" {
		return
	}

	// Phase 47 CR-01 gap-closure: the Get→IsNotFound→Create gate below is
	// name-only — it re-opens after the reporter Job's 300s TTL-GC
	// (reporter_jobspec.go), letting a sustained-reconcile parent re-Create a
	// duplicate reporter with freshly-recomputed ReporterOptions. Task always
	// observes a non-nil completedJob here (guarded above), so spawnKey is
	// always the completed Job's UID — no name-fallback branch needed (unlike
	// the four planner-level markers).
	spawnKey := string(completedJob.UID)
	if task.Status.TaskTraceReporterSpawnedUID == spawnKey {
		return // already spawned for this attempt — durable marker guard
	}

	jobName := "tide-reporter-trace-" + string(completedJob.UID)
	var existing batchv1.Job
	if gErr := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: jobName}, &existing); gErr == nil {
		r.stampTaskTraceReporterSpawnedUID(ctx, task, spawnKey)
		return // already spawned for this attempt (T-09-13-style idempotency)
	} else if !apierrors.IsNotFound(gErr) {
		logger.Error(gErr, "get trace-only reporter Job failed (non-fatal); spawn deferred to a later reconcile", "job", jobName)
		return
	}

	skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "task", r.Deps.HelmProviderDefaults).Vendor)
	// 46 D-05/OBS-02/OBS-03: enrichment values computed from the SAME inputs
	// Task's own AGENT span used (emitTaskSpanOnce, called immediately
	// above), so the trace-only reporter's LLM spans carry byte-identical
	// session.id/metadata/tags — includes wave_index (D-07 Task-only).
	enrichmentMD, enrichmentTags := buildLevelEnrichment(project, "task", task.Name, task.Labels[owner.LabelWaveIndex])
	traceOnlyJob := BuildReporterJob(task, project, r.sharedPVCName(), string(task.UID), "Task",
		ReporterOptions{
			ReporterImage:     r.Deps.ReporterImage,
			OTLPEndpoint:      r.Deps.OTLPEndpoint,
			OTLPHeadersSecret: r.Deps.OTLPHeadersSecret,
			TraceOnly:         true,
			TraceOnlyJobKey:   string(completedJob.UID),
			TraceParent:       traceparentForLevel(project, task.Status.TaskTraceSpanID, sampled),
			SkipMessageSpans:  skipMessageSpans,
			SessionID:         string(project.UID),
			MetadataJSON:      enrichmentMD,
			Tags:              enrichmentTags,
			// 50 D-01/D-05: the same {taskUID}-{attempt}/taskUID tuple
			// buildEnvelopeIn stamped onto EnvelopeIn at dispatch time and
			// synthesizePlannerSpan stamped onto the Task AGENT span (50-06)
			// — byte-identical derivation so the AGENT span and its
			// reporter's LLM spans correlate under the same attempt.
			AttemptID: fmt.Sprintf("%s-%d", task.UID, task.Status.Attempt),
			LoopRunID: string(task.UID),
		}, r.Scheme)
	if cErr := r.Create(ctx, traceOnlyJob); cErr != nil {
		if !apierrors.IsAlreadyExists(cErr) {
			logger.Error(cErr, "create trace-only reporter Job failed (non-fatal, observability never gates)", "job", jobName)
			return
		}
		// AlreadyExists: idempotent success (T-09-13) — a reporter Job verifiably
		// exists for this attempt, so the marker is stamped below same as Create.
	} else {
		logger.Info("spawned trace-only reporter Job", "job", jobName, "task", task.Name)
	}
	r.stampTaskTraceReporterSpawnedUID(ctx, task, spawnKey)
}

// stampTaskTraceReporterSpawnedUID durably records that the trace-only
// reporter Job for this Task attempt (spawnKey = completedJob.UID) has been
// observed to exist — the CR-01 gate that survives the reporter Job's 300s
// TTL-GC and manager restarts. Preserves spawnTaskTraceReporterIfNeeded's
// deliberate non-fatal posture (Phase 44 MSG-01/D-05: observability never
// gates Task's terminal-state machinery): on failure this logs and returns
// rather than propagating an error, so the gate stays open and the next
// reconcile retries the stamp — behavior degrades to pre-fix (possible
// duplicate spawn on TTL-GC), never worse.
func (r *TaskReconciler) stampTaskTraceReporterSpawnedUID(ctx context.Context, task *tideprojectv1alpha3.Task, spawnKey string) {
	logger := logf.FromContext(ctx)
	if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &tideprojectv1alpha3.Task{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(task), latest); err != nil {
			return err
		}
		if latest.Status.TaskTraceReporterSpawnedUID == spawnKey {
			return nil // already set by a concurrent reconcile — idempotent
		}
		markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
		latest.Status.TaskTraceReporterSpawnedUID = spawnKey
		return r.Status().Patch(ctx, latest, markerPatch)
	}); mErr != nil {
		logger.Error(mErr, "TaskTraceReporterSpawnedUID marker patch failed (non-fatal); gate stays open for retry on next reconcile", "task", task.Name)
	}
}

// synthesizeNoEnvelopeOut builds a synthetic terminal EnvelopeOut for the
// EnvelopeReadFailed path (50-06 Task 2, RESEARCH Pitfall 2 / Open Question
// 1 — controller half): the pod terminated (most commonly SIGKILLed by
// ActiveDeadlineSeconds) without ever writing out.json, so there is no real
// envelope to read. Extracted as a small pure function — no reconciler
// plumbing — for direct unit testing.
//
// Span identity must survive envelope loss: LoopRunID/AttemptID are always
// set from task.UID + task.Status.Attempt — the SAME tuple buildEnvelopeIn
// (D-01) stamped at dispatch time — so the AGENT span emitted from this
// synthetic envelope still carries loop.run_id/loop.parent_run_id even when
// the pod never produced a real envelope.
//
// TerminalReason classification is fail-closed: this is the ONLY producer of
// TerminalReasonCapExceeded for a wall-clock kill anywhere in the codebase —
// the SIGKILLed pod never gets a chance to classify itself. It maps ONLY the
// Job's JobFailed condition Reason "DeadlineExceeded" (the ActiveDeadlineSeconds
// kill) to cap_exceeded; every other failure reason leaves TerminalReason
// unset rather than guessing (mirrors ClassifyVerdict's fail-closed
// discipline — an unclassified envelope-less death stays visibly
// unclassified). The in-pod producer for iteration/token caps is
// harness.CheckCaps (Plan 50-04); this function covers only the
// no-envelope/wall-clock case.
func synthesizeNoEnvelopeOut(task *tideprojectv1alpha3.Task, completedJob *batchv1.Job) pkgdispatch.EnvelopeOut {
	out := pkgdispatch.EnvelopeOut{
		APIVersion:  pkgdispatch.APIVersionV1Alpha1,
		Kind:        pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:     string(task.UID),
		LoopRunID:   string(task.UID),
		AttemptID:   fmt.Sprintf("%s-%d", task.UID, task.Status.Attempt),
		CompletedAt: time.Now().UTC(),
	}

	if completedJob == nil {
		return out
	}
	for _, c := range completedJob.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if c.Reason == jobReasonDeadlineExceeded {
				out.TerminalReason = pkgdispatch.TerminalReasonCapExceeded
				out.Reason = reasonWallClockCapExceeded
			}
			break
		}
	}
	return out
}

// handleJobCompletion reads the EnvelopeOut, validates output paths, rolls up
// budget, and patches Task.Status to the terminal state.
//
//nolint:unparam,gocyclo // ctrl.Result kept so callers can `return r.handleJobCompletion(...)` in the reconcile chain; flat state machine of mutually-exclusive completion arms (now including the Phase 51 Verifying transition) — splitting obscures the contract (commit 9cae6bb precedent)
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
	//
	// Phase 51: settleExecutorReservation is flipped false ONLY when this
	// completion transitions a contract-bearing Task to Verifying and
	// dispatchVerifier successfully reserves a fresh BudgetCents entry for
	// task.UID below — settling here would otherwise immediately delete that
	// entry (both reservations share the same ReservationStore key; no
	// dedicated per-dispatch-kind key exists). Every other terminal path
	// (Failed, Succeeded-no-contract, EnvelopeReadFailed above, a cap-hit
	// deferred verifier dispatch that never reserved) settles exactly as
	// before.
	settleExecutorReservation := true
	defer func() {
		if settleExecutorReservation {
			r.Deps.Reservations.Settle(string(task.UID))
		}
	}()

	// Read the EnvelopeOut from the PVC-backed reader (Blocker #2/#3 path).
	out, err := r.Deps.EnvReader.ReadOut(ctx, string(project.UID), string(task.UID))
	if err != nil {
		// 50-06 Task 2: synthesize a terminal envelope so span identity
		// (LoopRunID/AttemptID) survives envelope loss and, for a wall-clock
		// (ActiveDeadlineSeconds) Job kill, TerminalReason is classified as
		// cap_exceeded — the only place that classification can ever happen,
		// since the SIGKILLed pod never wrote out.json.
		synthOut := synthesizeNoEnvelopeOut(task, completedJob)

		// TRACE-01/D-07: EnvelopeReadFailed is the only Task terminal path
		// reachable with envReadOK=false — the degraded-envelope span (Option B,
		// 43-05-PLAN.md call site 1). Emitted BEFORE the terminal status patch
		// below (span-loss-averse ordering).
		sampled := r.emitTaskSpanOnce(ctx, task, project, completedJob, synthOut, false)
		// MSG-01/D-05: failed Tasks are the highest-value debugging trace —
		// spawn the trace-only reporter here too, not just on the success path.
		r.spawnTaskTraceReporterIfNeeded(ctx, task, project, completedJob, sampled)

		// The condition Reason stays exactly "EnvelopeReadFailed" — wave
		// semantics and every consumer keying on that Reason are untouched
		// (scope fence). Only the Message gains the cap diagnostic, when
		// synthesizeNoEnvelopeOut classified this as a wall-clock kill.
		message := err.Error()
		if synthOut.TerminalReason == pkgdispatch.TerminalReasonCapExceeded {
			message = synthOut.Reason + ": " + message
		}

		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = tideprojectv1alpha3.LevelPhaseFailed
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "EnvelopeReadFailed",
			Message:            message,
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

	// TRACE-01/D-07: call site 2, positioned BEFORE the OutputValidationError/
	// OutputPathsViolation/standard-result branch divergence (Option B,
	// generalized — 43-05-PLAN.md) — one call covers all three post-read
	// terminal paths uniformly with envReadOK=true.
	sampled := r.emitTaskSpanOnce(ctx, task, project, completedJob, out, true)
	// MSG-01: covers the three post-read terminal paths uniformly (standard
	// result, OutputValidationError, OutputPathsViolation) — one call, same
	// D-06-gated idempotent spawn as the EnvelopeReadFailed call site above.
	r.spawnTaskTraceReporterIfNeeded(ctx, task, project, completedJob, sampled)

	// Output-path validation (Warning #5 — wires HARN-05 into dispatch chain).
	// Performed controller-side in Phase 2 (RESEARCH.md Responsibility Map deviation).
	// Phase 3 moves validation into the Pod once the harness-wrapped runtime lands.
	if out.Result != outputPathsViolation && len(task.Spec.DeclaredOutputPaths) > 0 && task.Status.StartedAt != nil {
		taskWorkspaceRoot := fmt.Sprintf("/workspaces/%s/workspace", string(project.UID))
		violations, skipped, vErr := validateControllerOutputPaths(taskWorkspaceRoot, task.Status.StartedAt.Time, task.Spec.DeclaredOutputPaths)
		if vErr != nil {
			patch := client.MergeFrom(task.DeepCopy())
			task.Status.Phase = tideprojectv1alpha3.LevelPhaseFailed
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
			if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage, out.CompletedAt, "internal", true); emitErr != nil {
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
			task.Status.Phase = tideprojectv1alpha3.LevelPhaseFailed
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
			if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage, out.CompletedAt, "internal", true); emitErr != nil {
				logger.Error(emitErr, "failed to emit task metrics (non-fatal)", "task", task.Name)
			}
			return ctrl.Result{}, nil
		}
	}

	// Standard result interpretation.
	patch := client.MergeFrom(task.DeepCopy())
	//nolint:goconst // "cap-hit" is a well-known EnvelopeOut.Result value shared across
	// internal/harness, pkg/otelai, and this package's test fixtures; a constant here
	// wouldn't reduce the raw-literal count package-wide and would need to span packages.
	if out.ExitCode != 0 || out.Result == "cap-hit" || out.Result == outputPathsViolation {
		task.Status.Phase = tideprojectv1alpha3.LevelPhaseFailed
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
	} else if hasVerificationContract(task) && verificationEnabledForLevel(project, "task", r.Deps.VerifyDefaults) {
		// Phase 51 TASK-06 anti-gaming (BL-01): the EXECUTOR's own changed-file
		// manifest is present in out.RunEvidence HERE — before a verifier
		// overwrites the shared out.json path with its verdict-only envelope
		// (which never carries RunEvidence). If the attempt touched a protected
		// evaluator/fixture/threshold path, escalate as a SYSTEM escalation NOW,
		// before dispatching a verifier that could bless the gaming attempt.
		// This fires on EVERY downstream verdict path (APPROVED and REPAIRABLE
		// alike), closing the hole where a *successful* gaming attempt (gate
		// goes green -> APPROVED) reached markVerifiedSucceeded with no
		// anti-gaming check at all. `out` here is the executor envelope, so
		// escalateSystem's terminal bookkeeping rolls up the executor's real
		// spend exactly once — this function's own roll-up/metrics below are
		// not reached (early return).
		if out.RunEvidence != nil && intersectsProtected(out.RunEvidence.ChangedFiles, protectedPathsFor(task)) {
			return r.escalateSystem(ctx, task, project, out)
		}
		// Phase 51 EXEC-04: the Execution loop believes it is complete but
		// NEVER stamps Task correctness directly for a contract-bearing
		// Task — only an independent verifier's consumed verdict can (Plan
		// 07). Transition to Verifying; the verifier is dispatched below,
		// AFTER this status patch and the usual roll-up/metrics bookkeeping
		// (the executor really did run and really did spend tokens,
		// regardless of what the verifier later decides).
		task.Status.Phase = tideprojectv1alpha3.LevelPhaseVerifying
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionReconciling,
			Status:             metav1.ConditionTrue,
			Reason:             "VerifierDispatched",
			Message:            "Executor completed; dispatching an independent verifier against the locked verification contract",
			LastTransitionTime: metav1.Now(),
		})
		// TASK-01 (LO-03): stamp a COARSE provenance anchor. LastPushedSHA is
		// the most recent commit landed on the per-Project run branch — the
		// closest available observation to "the commit spec.verification was
		// Locked at" (no finer-grained per-Lock commit SHA is tracked anywhere
		// in the codebase today). It is NOT the lock-commit, and because the
		// contract lives in the CRD (not git), `git show <lockedSHA>` does not
		// by itself reproduce the dispatched contract — a best-effort temporal
		// anchor, not a literal git-show reproduction guarantee.
		task.Status.LockedSHA = project.Status.Git.LastPushedSHA
		// WR-01 (Phase 53): stamp the RESOLVED MaxIterations at loop
		// engagement so read-only consumers (the dashboard API, which never
		// receives the chart tier) surface the bound that actually governs
		// the loop, not the raw authored Spec value. Re-stamped on every
		// Verifying entry (each repair attempt re-enters here), so a chart
		// re-config mid-loop refreshes it.
		task.Status.LoopStatus.EffectiveMaxIterations = ResolveLoopPolicy(project, nil, task, "task", r.Deps.VerifyDefaults).MaxIterations
		// BL-01/LO-02: persist the executor's bounded changed-file manifest so
		// the repairOrHalt anti-gaming belt-and-suspenders (TASK-06) and the
		// repair evidence packet (stageEvidencePacket, TASK-02) can read it
		// after the verifier overwrites out.json with its verdict-only envelope.
		task.Status.LastAttemptEvidence = runEvidenceSummaryFrom(out.RunEvidence)
	} else {
		task.Status.Phase = tideprojectv1alpha3.LevelPhaseSucceeded
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
	// Emit the six locked spend metrics at the same once-only commit point as
	// budget.RollUpUsage — guarantees Prometheus cost totals never diverge from
	// Budget accounting (Phase 16 D-12). Non-fatal.
	// Compute the bounded metric reason from the envelope result; "" = Succeeded.
	// ME-01: a contract-bearing Task moving to Verifying is NOT terminal — record
	// its executor spend but defer the completion/failure counter to
	// finishVerifierTerminal (countCompletion=false), so a Task is never counted
	// completed at each Verifying transition AND again at its real terminal.
	var metricReason string
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseFailed {
		metricReason = metricFailureReason(out.Result, out.ExitCode)
	}
	countCompletion := task.Status.Phase != tideprojectv1alpha3.LevelPhaseVerifying
	if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage, out.CompletedAt, metricReason, countCompletion); emitErr != nil {
		logger.Error(emitErr, "failed to emit task metrics (non-fatal)", "task", task.Name)
	}

	// Phase 14 BUDGET-02: stamp BudgetBlocked immediately after RollUpUsage — this
	// is the first moment where CostSpentCents may cross the cap (RESEARCH §Root Cause,
	// Architecture diagram). Bidirectional: also clears the condition when a cap raise
	// brings IsCapExceeded back to false. Non-fatal: the task is already terminal.
	if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.Deps.Reservations.TotalReserved()); err != nil {
		logger.Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)", "task", task.Name)
	}

	// Phase 51 TASK-01/ESC-04: contract-bearing Tasks dispatch the verifier
	// here, AFTER every bookkeeping step above has landed (the executor's
	// real spend is never conditional on the verify outcome). dispatchVerifier
	// itself owns the ESC-04 cap check and D-05 reservation (Pitfall 6); when
	// it reserves a fresh entry for task.UID, suppress this function's own
	// deferred Settle so that reservation survives past this return (see the
	// defer's own comment above for why the two share a store key).
	if task.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying {
		result, dispatchReserved, dErr := r.dispatchVerifier(ctx, task, project)
		if dispatchReserved {
			settleExecutorReservation = false
		}
		return result, dErr
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
	if projectName, ok := task.Labels[owner.LabelProject]; ok && projectName != "" {
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
// countCompletion gates ONLY the terminal TasksCompletedTotal/TasksFailedTotal
// counter (ME-01): the six token/cost/duration spend metrics are ALWAYS emitted
// (the executor's spend is real regardless of the loop's eventual outcome), but
// a NON-terminal transition (a contract-bearing Task moving to Verifying) passes
// false so it records the executor's spend without prematurely counting the Task
// as completed. The terminal counter is deferred to finishVerifierTerminal — a
// verify-halted Task must count as failed-only, never both completed and failed.
//
//nolint:unparam // error return kept so callers can `if err := r.emitTaskMetrics(...); err != nil` in the reconcile chain
func (r *TaskReconciler) emitTaskMetrics(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, usage pkgdispatch.Usage, completedAt time.Time, failureReason string, countCompletion bool) error {
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
	// ME-01: only at a TERMINAL transition — a non-terminal Verifying transition
	// passes countCompletion=false so the executor's spend (above) is recorded
	// without the Task being double-counted as completed here AND failed/completed
	// again at its real terminal (finishVerifierTerminal).
	if !countCompletion {
		return nil
	}
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
		if ref.Kind == "Project" && ref.APIVersion == tideprojectv1alpha3.GroupVersion.String() {
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
			if statusByName[member] != tideprojectv1alpha3.LevelPhaseSucceeded {
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
		attempt, ok := j.Labels[owner.LabelAttempt]
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
// Translates api/v1alpha3.Caps → pkg/dispatch.Caps per Plan 03's two-type design.
//
// D-01 (50-06 Task 1, EXEC-01): attempt is the same dispatch-attempt number
// podjob.JobName(taskUID, attempt) uses to derive the per-attempt Job name
// ("tide-task-{taskUID}-{attempt}") — LoopRunID/AttemptID below are re-derived
// from that identical tuple, never minted or persisted. LoopRunID is the outer
// Task-loop run anchor (loop.parent_run_id, stable across repair attempts);
// AttemptID is this execution attempt (loop.run_id). Planner-level dispatches
// (dispatch_helpers.go's BuildPlannerEnvelope) are deliberately NOT stamped
// this phase — the Execution loop is the in-Job Task attempt; planner-loop
// identity is future work.
//
// evidencePacketPath (Phase 51 TASK-02/D-04) is "" for the Task's plain
// dispatch and non-empty only for a repairOrHalt-minted fresh quality-
// iteration attempt (dispatchRepairAttempt) — when set, it is carried on
// EnvelopeIn.Verify.EvidencePacketPath, the same VerifyContext field a
// verifier dispatch's own repair re-check reads (see buildVerifierEnvelopeIn).
// This is the ONLY VerifyContext field a role="executor" envelope ever
// populates (GateCommand/Commands/RequiredArtifacts/EvaluatorRef stay empty).
func (r *TaskReconciler) buildEnvelopeIn(_ context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, attempt int, token, evidencePacketPath string) (pkgdispatch.EnvelopeIn, []byte, error) {
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
		// D-01: derived from the task.UID + attempt tuple, never minted.
		LoopRunID: string(task.UID),
		AttemptID: fmt.Sprintf("%s-%d", task.UID, attempt),
		// Resolve the executor's ProviderSpec the same way the planner
		// reconcilers do (BuildPlannerEnvelope → ResolveProvider): Vendor pinned
		// to "anthropic" + the task-level model. Without this the envelope's
		// Provider is the zero value and the anthropic runner refuses the task
		// ("refusing vendor=\"\""). Latent until a run first reached real task
		// execution (the planner paths set Provider; this builder never did).
		Provider:      ResolveProvider(project, "task", r.Deps.HelmProviderDefaults),
		ProxyEndpoint: credproxyEndpoint,
		SignedToken:   token,
		Dev:           dev,
	}
	if evidencePacketPath != "" {
		envIn.Verify = &pkgdispatch.VerifyContext{EvidencePacketPath: evidencePacketPath}
	}

	data, mErr := json.Marshal(envIn)
	if mErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal envelope in: %w", mErr)
	}
	return envIn, data, nil
}

// verificationPhaseLocked mirrors the CEL-enforced enum value
// api/v1alpha3.VerificationSpec.Phase carries once a planner locks a Task's
// verification contract (task_types.go's XValidation transition rule).
// VerificationSpec.Phase is a plain string with a +kubebuilder:validation:Enum
// marker — no Go const exists on the type itself — so this local const keeps
// the literal grep-distinct at its one call site (hasVerificationContract).
const verificationPhaseLocked = "Locked"

// defaultVerifierConcurrencyCap bounds concurrent verifier Jobs per project
// (ESC-04/D-10) via the count-based verifierInFlightCount gate — no
// pool.Pool semaphore is wired for the verifier tier this phase (Plan 06 has
// no cmd/manager/main.go wiring in scope; the executor/planner tiers' D3 cap
// checks run independently of, and before, their own PlannerPool.Acquire,
// which this mirrors). Claude's Discretion (no live-run data yet, mirrors
// podjob.verifierCapsFloorSeconds' own precedent) — Plan 08's kind
// concurrent-dispatch test pins/re-tunes the exact value.
const defaultVerifierConcurrencyCap = 2

// hasVerificationContract reports whether task carries a real, planner-
// authored verification contract this Task loop must dispatch an
// independent verifier against (Phase 51 TASK-01, RESEARCH Open Question 2).
// A contract exists only once BOTH a canonical GateCommand is set AND the
// contract has been Locked — an empty GateCommand or a still-Draft contract
// preserves the pre-Phase-51 exit-0 -> Succeeded path (OQ2 backward-compat);
// dispatching against a Draft (mutable) contract would break TASK-01's
// git-show reproducibility guarantee.
func hasVerificationContract(task *tideprojectv1alpha3.Task) bool {
	v := task.Spec.Verification
	return v.GateCommand != "" && v.Phase == verificationPhaseLocked
}

// dispatchVerifier creates the independent, read-only verifier Job for a
// contract-bearing Task whose executor believed it completed (EXEC-04).
// Mirrors the executor dispatch flow (createDispatchJob) but is a distinct,
// separately-pooled dispatch (D-10/TASK-04): the ESC-04 concurrency cap
// (verifierInFlightCount) is checked BEFORE any reservation or Job create
// (Pitfall 6 — no slot/reservation leak on cap-hit — the deferred requeue
// happens before Reserve is ever called), and the deterministic
// VerifierJobName makes a retry (e.g. after a prior cap-hit deferred
// dispatch, via checkVerifyingState) idempotent — AlreadyExists on Create is
// treated as success (SUB-03).
//
// Returns reserved=true only when a BudgetCents reservation was made for
// task.UID THIS call. The caller (handleJobCompletion) uses this to decide
// whether to suppress its own deferred Settle(task.UID) — which would
// otherwise immediately clear the fresh verifier reservation this call just
// made, since both the executor's and the verifier's reservations share the
// same ReservationStore key (no dedicated per-task BudgetCents field exists
// yet — VerificationSpec/LoopPolicy is not embedded on TaskSpec this phase;
// this rides the same flat ReserveEstimateCents estimate the executor
// dispatch uses, D-05 Option B, per "Cost bounding: BudgetCents rides the
// existing accounting").
func (r *TaskReconciler) dispatchVerifier(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project) (result ctrl.Result, reserved bool, err error) {
	logger := logf.FromContext(ctx)
	attempt := task.Status.Attempt
	verifierJobName := podjob.VerifierJobName("task", string(task.UID), attempt)

	// LO-01: no verifier image configured (TIDE_VERIFIER_IMAGE unset — test
	// fixtures or a dev cluster without the Helm chart). Building a Job with an
	// empty container image ref creates an unschedulable Job (ImagePullBackOff /
	// Invalid spec) that leaves the Task parked in Verifying indefinitely with no
	// signal. Log and leave the Task benignly parked instead — mirrors the
	// TIDE_REPORTER_IMAGE / TIDE_PUSH_IMAGE skip (boundary_push.go), and matches
	// the wiring comment in cmd/manager/main.go. In production the chart always
	// defaults this, so this is an edge-case safety net. Info (not V(1)) so the
	// silent disablement is operator-visible at default verbosity.
	if r.Deps.VerifierImage == "" {
		logger.Info("verifier image not configured (TIDE_VERIFIER_IMAGE empty); leaving Task parked in Verifying without dispatching a verifier Job",
			"task", task.Name)
		return ctrl.Result{}, false, nil
	}

	// ESC-04/D-10: cap-before-acquire (Pitfall 6). Self-excludes
	// verifierJobName so a re-reconcile of an already-dispatched verifier
	// (checkVerifyingState's NotFound-retry path never reaches here once the
	// Job exists) never counts itself.
	inFlight, cErr := verifierInFlightCount(ctx, r.Client, task.Namespace, project.Name, verifierJobName)
	if cErr != nil {
		return ctrl.Result{}, false, fmt.Errorf("verifier in-flight count: %w", cErr)
	}
	if inFlight >= defaultVerifierConcurrencyCap {
		logger.V(1).Info("verifier dispatch deferred: concurrency cap reached",
			"inFlight", inFlight, "cap", defaultVerifierConcurrencyCap, "task", task.Name)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, false, nil
	}

	if r.Deps.ReserveEstimateCents > 0 {
		r.Deps.Reservations.Reserve(string(task.UID), r.Deps.ReserveEstimateCents)
		reserved = true
	}
	releaseOnError := func() {
		if reserved {
			r.Deps.Reservations.Release(string(task.UID))
			reserved = false
		}
	}

	verifierCaps := podjob.DefaultCaps(nil, podjob.JobKindVerifier)
	wallClock := verifierCaps.WallClockSeconds
	token, sErr := credproxy.Sign(r.Deps.SigningKey, string(task.UID),
		time.Duration(wallClock+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if sErr != nil {
		releaseOnError()
		return ctrl.Result{}, false, fmt.Errorf("mint verifier signed token: %w", sErr)
	}

	_, envInJSON, bErr := r.buildVerifierEnvelopeIn(task, project, attempt, token)
	if bErr != nil {
		releaseOnError()
		return ctrl.Result{}, false, bErr
	}

	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if gErr := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); gErr == nil {
			secretUID = string(secret.UID)
		}
	}
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	job := podjob.BuildJobSpec(podjob.BuildOptions{
		Kind:           podjob.JobKindVerifier,
		Task:           task,
		ParentObj:      task,
		Level:          "task",
		Project:        project,
		Attempt:        attempt,
		SignedToken:    token,
		EnvelopeInJSON: envInJSON,
		SubagentImage:  r.Deps.VerifierImage,
		AgentName:      agentName,
		AgentEmail:     agentEmail,
		CredproxyImage: r.Deps.CredproxyImage,
		SecretUID:      secretUID,
		PVCName:        r.sharedPVCName(),
		ProjectUID:     string(project.UID),
		ReadOnly:       true,
		GateCommand:    task.Spec.Verification.GateCommand,
		// Stamp the estimated-cost label so budget.RederiveReservations can
		// restore this verifier's reservation after a manager restart while it
		// is in-flight (it shares the executor's per-task reservation key, but
		// the terminated executor Job is skipped on rederive — TASK-05/ESC-04).
		EstimatedCostCents: r.Deps.ReserveEstimateCents,
	})
	// BuildJobSpec's JobKindVerifier case stamps role=verifier + task-uid but
	// not the project label (only role/task-uid — mirrors the executor/
	// planner cases). verifierInFlightCount's project-scoped List needs it;
	// stamp it here at the create site, mirroring the git-writer Job label
	// convention (push_helpers.go stamps owner.LabelProject directly at its
	// own Job-create call site rather than inside BuildJobSpec).
	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	job.Labels[owner.LabelProject] = project.Name
	if job.Spec.Template.Labels == nil {
		job.Spec.Template.Labels = map[string]string{}
	}
	job.Spec.Template.Labels[owner.LabelProject] = project.Name

	if oErr := owner.EnsureOwnerRef(job, task, r.Scheme); oErr != nil {
		releaseOnError()
		return ctrl.Result{}, false, fmt.Errorf("ensure owner ref on verifier job: %w", oErr)
	}
	if createErr := r.Create(ctx, job); createErr != nil {
		if !apierrors.IsAlreadyExists(createErr) {
			releaseOnError()
			return ctrl.Result{}, false, fmt.Errorf("create verifier job: %w", createErr)
		}
		// AlreadyExists: idempotent success — watch-lag race, or a
		// checkVerifyingState retry after a prior cap-hit deferred dispatch
		// that raced a concurrent Create (Pitfall F / SUB-03).
		logger.Info("verifier job already exists; treating as successful dispatch", "job", job.Name)
	}

	logger.Info("dispatched verifier", "task", task.Name, "job", job.Name,
		"gateCommand", task.Spec.Verification.GateCommand)
	return ctrl.Result{}, reserved, nil
}

// buildVerifierEnvelopeIn constructs and marshals the EnvelopeIn for a
// verifier dispatch (Phase 51 TASK-01/TASK-04/D-01). Unlike buildEnvelopeIn
// (executor): Role="verifier", Provider.Vendor="langgraph" (the verifier is
// a logically independent process from the implementation agent, TASK-04).
// VerifyContext.GateCommand carries the canonical single primary command
// from the LOCKED spec.verification; Commands carries the resolved ordered
// union [GateCommand] ++ spec.verification.Commands — the full pass-criteria
// list the verifier executes out-of-band (no authored command left
// unexecuted).
//
// The prompt is rendered HERE, controller-side (Go), via
// common.LoadPromptTemplate("verifier","task") — the tide-langgraph-verifier
// image is pure Python and never imports internal/subagent/common (D-03
// import firewall; cmd/tide-langgraph-verifier/Dockerfile's own header states
// this explicitly) — mirroring how planner dispatches carry a pre-resolved
// Prompt rather than a PromptPath (BuildPlannerEnvelope's doc comment).
func (r *TaskReconciler) buildVerifierEnvelopeIn(task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, attempt int, token string) (pkgdispatch.EnvelopeIn, []byte, error) {
	verification := task.Spec.Verification

	// D-01: the resolved ordered union — GateCommand first (guaranteed
	// executed) then every additional authored pass-criterion, so no
	// authored command is left unexecuted by the verifier's out-of-band
	// capture (Plan 02's _run_commands_out_of_band iterates this list).
	var commands []string
	if verification.GateCommand != "" {
		commands = append(commands, verification.GateCommand)
	}
	commands = append(commands, verification.Commands...)

	envIn := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    string(task.UID),
		Role:       "verifier",
		Level:      "task",
		// D-01: derived from the task.UID + attempt tuple, never minted —
		// same shape as the executor's own LoopRunID/AttemptID stamp.
		LoopRunID: string(task.UID),
		AttemptID: fmt.Sprintf("%s-%d", task.UID, attempt),
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "langgraph",
			Model:  resolveVerifierModel(project, "task", r.Deps.VerifyDefaults, r.Deps.HelmProviderDefaults),
		},
		ProxyEndpoint: credproxyEndpoint,
		SignedToken:   token,
		Verify: &pkgdispatch.VerifyContext{
			GateCommand:       verification.GateCommand,
			Commands:          commands,
			RequiredArtifacts: verification.RequiredArtifacts,
			EvaluatorRef:      verification.Evaluator,
			// EvidencePacketPath: "" — first (non-repair) verify. Plan 07
			// stages a packet path for repair re-checks.
		},
	}

	tmpl, tErr := common.LoadPromptTemplate("verifier", "task")
	if tErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("load verifier prompt template: %w", tErr)
	}
	// EVAL-04 (ME-02): the template's "Original task instruction" section must
	// carry the executor's ORIGINAL intent so the verifier judges against it,
	// not its own preferences. Stamp the executor's task-instruction path onto
	// the DEDICATED PromptPath field (never the self-referential envIn.Prompt,
	// which is still empty here and is precisely what this render produces — the
	// original bug rendered {{.Prompt}} into itself as the empty string). The
	// template references {{.PromptPath}}; the Manager cannot read the prompt
	// CONTENT cross-namespace (Defect #10b — it lives on the project-namespace
	// PVC), so the verifier reads the original task artifact in-pod from its own
	// read-only /workspace mount, exactly as the executor reads its own
	// PromptPath (the verifier's Python entrypoint ignores this field for its
	// own execution — it runs the rendered envIn.Prompt below).
	envIn.PromptPath = task.Spec.PromptPath
	var promptBuf bytes.Buffer
	if xErr := tmpl.Execute(&promptBuf, envIn); xErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("render verifier prompt template: %w", xErr)
	}
	envIn.Prompt = promptBuf.String()

	data, mErr := json.Marshal(envIn)
	if mErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal verifier envelope: %w", mErr)
	}
	return envIn, data, nil
}

// gateCommandFindingSeverity/gateCommandFindingDimension mirror the
// severity="blocker"/dimension="gate-command" Finding the Plan 02 verifier
// entrypoint's own out-of-band dominance leg emits (__main__.py's
// _assemble_verdict) for ANY non-zero pass-criterion command exit. Kept as
// local consts (verificationPhaseLocked precedent, above) because
// pkg/dispatch's own highSeverityFindingToken is unexported outside its
// package boundary — D-06/D-08 retuning point.
const (
	gateCommandFindingSeverity  = "blocker"
	gateCommandFindingDimension = "gate-command"
)

// stageEvidencePacketMaxFindings bounds the Findings slice a staged evidence
// packet carries (TASK-02: reference-only, compact — never the prior
// agent's full context), mirroring RunEvidence.Bounded()'s own DoS-shaped
// discipline (T-50-01) for a value this phase doesn't already bound.
const stageEvidencePacketMaxFindings = 20

// protectedEvaluatorFixturePaths is the TASK-06 anti-gaming protected-path
// set (RunEvidence.ChangedFiles path-prefix match): the evaluator/fixture/
// threshold surface a repair attempt is never trusted to edit, scoped
// specifically to what the verification contract itself depends on for
// integrity (RESEARCH Pitfall 5) — NOT every *_test.go file, which would
// wrongly flag an ordinary test-driven repair as gaming. Claude's
// Discretion: no planner-declared per-Task override field exists on
// VerificationSpec this phase — a fixed, repo-wide set.
var protectedEvaluatorFixturePaths = []string{
	"internal/eval/",
	"evals/",
	"cmd/tide-langgraph-verifier/",
	"internal/subagent/common/templates/task_verifier.tmpl",
}

// hasDeterministicFailure reports whether gd carries a deterministic
// gate-command dominance Finding (D-06 controller-side re-check, defence-in-
// depth over the verifier's own out-of-band dominance leg): a red gate on
// ANY authored pass-criterion command dominates even a top-level APPROVED
// verdict. nil-safe.
func hasDeterministicFailure(gd *pkgdispatch.GateDecision) bool {
	if gd == nil {
		return false
	}
	for _, f := range gd.Findings {
		if f.Severity == gateCommandFindingSeverity && f.Dimension == gateCommandFindingDimension {
			return true
		}
	}
	return false
}

// protectedPathsFor returns the TASK-06 anti-gaming protected-path prefixes
// for task's verification contract. task is currently unused — the fixed
// protectedEvaluatorFixturePaths set applies uniformly this phase — but kept
// on the signature as the seam a future per-contract override would extend.
func protectedPathsFor(_ *tideprojectv1alpha3.Task) []string {
	return protectedEvaluatorFixturePaths
}

// intersectsProtected reports whether any changed file's path falls under a
// protected prefix (TASK-06, path-prefix match — an edit anywhere inside a
// protected directory counts, not just an exact file match). Consumes the
// EXECUTOR's wire-format RunEvidence.ChangedFiles at executor-completion time
// (handleJobCompletion), the ONE place the executor's manifest is present
// before the verifier overwrites out.json (BL-01).
func intersectsProtected(changed []pkgdispatch.ChangedFile, protected []string) bool {
	for _, f := range changed {
		for _, p := range protected {
			if strings.HasPrefix(f.Path, p) {
				return true
			}
		}
	}
	return false
}

// intersectsProtectedRefs is intersectsProtected over the api-local
// ChangedFileRef persisted on TaskStatus.LastAttemptEvidence. The
// repairOrHalt belt-and-suspenders reads the PERSISTED executor manifest
// through this — never the verifier's verdict-only envelope, which carries
// no RunEvidence (the BL-01 root cause).
func intersectsProtectedRefs(changed []tideprojectv1alpha3.ChangedFileRef, protected []string) bool {
	for _, f := range changed {
		for _, p := range protected {
			if strings.HasPrefix(f.Path, p) {
				return true
			}
		}
	}
	return false
}

// runEvidenceSummaryFrom projects the executor's bounded RunEvidence onto the
// api-local RunEvidenceSummary persisted on TaskStatus.LastAttemptEvidence
// (BL-01). Bounds before persisting (PERSIST-02); nil in -> nil out.
func runEvidenceSummaryFrom(ev *pkgdispatch.RunEvidence) *tideprojectv1alpha3.RunEvidenceSummary {
	if ev == nil {
		return nil
	}
	bounded := ev.Bounded()
	summary := &tideprojectv1alpha3.RunEvidenceSummary{Commands: bounded.Commands}
	for _, f := range bounded.ChangedFiles {
		summary.ChangedFiles = append(summary.ChangedFiles, tideprojectv1alpha3.ChangedFileRef{Path: f.Path, Status: f.Status})
	}
	return summary
}

// changedFileRefsToDispatch converts the persisted api-local ChangedFileRef
// slice back to the pkg/dispatch.ChangedFile wire type the evidence packet
// (stageEvidencePacket, LO-02) carries.
func changedFileRefsToDispatch(refs []tideprojectv1alpha3.ChangedFileRef) []pkgdispatch.ChangedFile {
	if len(refs) == 0 {
		return nil
	}
	out := make([]pkgdispatch.ChangedFile, 0, len(refs))
	for _, r := range refs {
		out = append(out, pkgdispatch.ChangedFile{Path: r.Path, Status: r.Status})
	}
	return out
}

// applyLoopStatus updates task.Status.LoopStatus with the current-iteration
// summary only (LOOP-03 — no accumulating history, TestLoopStatus_NoForbiddenFields):
// Iteration mirrors Status.Attempt (the attempt just evaluated), LastEvaluation
// is the bounded verdict summary from THIS terminal verifier envelope
// (nil-safe — a degraded/unreadable envelope leaves it nil), and ExitReason is
// set only once the loop has genuinely stopped — callers pass "" for a
// mid-loop repair dispatch (loop_types.go's own "empty while the loop is
// still active" contract) and the real ExitReason value for every terminal
// outcome (approved/iterationsExhausted/escalated).
func applyLoopStatus(task *tideprojectv1alpha3.Task, out pkgdispatch.EnvelopeOut, exitReason tideprojectv1alpha3.ExitReason) {
	task.Status.LoopStatus.Iteration = int32(task.Status.Attempt)
	if out.LoopRunID != "" {
		task.Status.LoopStatus.ParentRunID = out.LoopRunID
	}
	task.Status.LoopStatus.ExitReason = exitReason
	if out.Verdict == nil {
		return
	}
	var highSeverity int32
	for _, f := range out.Verdict.Findings {
		if f.Severity == gateCommandFindingSeverity {
			highSeverity++
		}
	}
	summary := tideprojectv1alpha3.EvaluationSummary{
		Decision:          string(out.Verdict.Verdict),
		FindingsCount:     int32(len(out.Verdict.Findings)),
		HighSeverityCount: highSeverity,
	}
	if !out.CompletedAt.IsZero() {
		ct := metav1.NewTime(out.CompletedAt)
		summary.CompletedAt = &ct
	}
	task.Status.LoopStatus.LastEvaluation = &summary
}

// emitEvaluatorSpanForVerifier resolves the SAME parentSpanID Task's own
// AGENT span was given (task.Spec.PlanRef's persisted PlanTraceSpanID,
// mirroring emitTaskSpanOnce's identical TRACE-02 fetch) and emits the
// OBS-03/D-11 EVALUATOR sibling span for a terminal verifier Job. Best-effort
// observability — never gates verdict consumption (mirrors
// spawnTaskTraceReporterIfNeeded's non-fatal posture); return values are
// discarded because this phase has no dedicated {Level}TraceSpanID-equivalent
// status field for the EVALUATOR span (Plan 07's declared file scope excludes
// api/v1alpha3 schema changes), so there is no persistence patch to make.
func (r *TaskReconciler) emitEvaluatorSpanForVerifier(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, verifierJob *batchv1.Job, out pkgdispatch.EnvelopeOut, envReadOK bool) {
	var parentSpanID trace.SpanID
	if task.Spec.PlanRef != "" {
		var parentPlan tideprojectv1alpha3.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Spec.PlanRef}, &parentPlan); err == nil {
			parentSpanID = spanIDFromHexOrZero(parentPlan.Status.PlanTraceSpanID)
		}
	}
	evaluatorVersion := ""
	if out.RunEvidence != nil && len(out.RunEvidence.EvaluatorVersions) > 0 {
		evaluatorVersion = out.RunEvidence.EvaluatorVersions[0]
	}
	synthesizeEvaluatorSpan(ctx, "task", task.Name, project, r.Deps.HelmProviderDefaults, verifierJob, out, envReadOK, evaluatorVersion, parentSpanID)
}

// settleVerifierSpend rolls the verifier's own real token spend into
// Project.Status.budget (mirrors handleJobCompletion's identical roll-up,
// D-D2) and settles the BudgetCents reservation dispatchVerifier made at
// verify-dispatch time (Plan 06) — nothing has settled it until this call
// (51-06-SUMMARY.md's own "Next Phase Readiness" note). Called exactly once
// per verifier completion regardless of verdict outcome: the verifier ran
// and spent real tokens either way. A subsequent Reserve (dispatchRepairAttempt's
// fresh attempt, or a later verify-dispatch) safely overwrites the same
// ReservationStore key after this Settle — no leak, no premature-clear race.
func (r *TaskReconciler) settleVerifierSpend(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) {
	logger := logf.FromContext(ctx)
	if err := budget.RollUpUsage(ctx, r.Client, project, out.Usage); err != nil {
		logger.Error(err, "failed to roll up verifier budget usage", "task", task.Name)
	}
	if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
		logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "task", task.Name)
	}
	r.Deps.Reservations.Settle(string(task.UID))
}

// finishVerifierTerminal performs the terminal-only bookkeeping (Task-level
// Prometheus metrics + the D-D2 budget-blocked recheck) for a verifier
// completion that ENDS the Task loop (Succeeded via markVerifiedSucceeded or
// Failed via haltVerify) — deliberately never called for a mid-loop repair
// dispatch (dispatchRepairAttempt), which is not yet a completion and must
// not increment TasksCompletedTotal/TasksFailedTotal.
func (r *TaskReconciler) finishVerifierTerminal(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut, metricFailureReason string) {
	logger := logf.FromContext(ctx)
	// finishVerifierTerminal is the Task's REAL terminal for a contract-bearing
	// Task (Succeeded via markVerifiedSucceeded or VerifyHalted via haltVerify) —
	// so this is where the completion/failure counter is emitted (ME-01,
	// countCompletion=true), never at the earlier non-terminal Verifying transition.
	if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage, out.CompletedAt, metricFailureReason, true); emitErr != nil {
		logger.Error(emitErr, "failed to emit verifier task metrics (non-fatal)", "task", task.Name)
	}
	if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.Deps.Reservations.TotalReserved()); err != nil {
		logger.Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)", "task", task.Name)
	}
}

// maybeTriggerTaskFindingsPush implements the Task verdict-final findings-push
// trigger (Plan 53-10 / OBS-04): stages and pushes a Task's verifier findings
// through the EXISTING artifact-push machinery (triggerArtifactPush) at the
// Task's verdict-final transition, even while a project-wide VerifyHalt
// freezes ALL dispatch (checkVerifyHalt/checkDispatchHolds). This is a git
// write of already-computed evaluator output, not a dispatch action — it must
// NEVER be gated by dispatch holds, and every call site below sits AFTER
// them.
//
// Eligibility reuses taskFindingsStageable (artifact_push.go) EXACTLY — the
// SAME named predicate the collector applies — so the trigger and the
// collector never diverge on which Tasks are push-eligible; a Task with a nil
// LoopStatus.LastEvaluation (verifier crashed before a verdict) is never
// pushed (T-53-25 poison guard).
//
// Edge-gated on stagedEnvelopesAnnotation (Defect E / DASH-02): once a push
// Job's carried-entry set includes this Task's own <uid>:task/<name> entry,
// carried=true — callers stop retrying (T-53-23 no-churn). While a push Job
// exists but has not YET carried the entry (a race with an in-flight push
// snapshotted before this Task's status patch landed), returns
// carried=false, err=nil — the ProjectReconciler's isStaleArtifactPush
// supersede path independently heals this once the in-flight Job completes.
// While no push Job exists, dispatches a fresh one via triggerArtifactPush,
// passing this Task as the ensure-entry so the just-patched Task rides the
// push even though the informer cache backing collectStageEnvelopes' List may
// not yet have observed the patch.
//
// Errors ARE returned to the caller (never swallowed here), but every call
// site treats them as non-fatal — logged and continued, never failing the
// verdict-final Status patch (mirrors setVerifyHaltIfNeeded's tolerated-error
// posture, level_status.go:228-230).
func (r *TaskReconciler) maybeTriggerTaskFindingsPush(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project) (carried bool, err error) {
	if project == nil || !taskFindingsStageable(task) {
		return false, nil
	}

	pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
	entry := stageEntry("task", task.Name, string(task.UID))

	var existing batchv1.Job
	getErr := r.Get(ctx, client.ObjectKey{Name: pushJobName, Namespace: project.Namespace}, &existing)
	switch {
	case getErr == nil:
		for e := range strings.SplitSeq(existing.Annotations[stagedEnvelopesAnnotation], ",") {
			if e == entry {
				return true, nil
			}
		}
		// Busy but not yet carrying this Task's entry: isStaleArtifactPush
		// (project_controller.go) independently heals this once the
		// in-flight Job completes — nothing more to do here.
		return false, nil
	case apierrors.IsNotFound(getErr):
		if tErr := triggerArtifactPush(ctx, r.Client, r.Scheme, project, "task", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults, task); tErr != nil {
			return false, tErr
		}
		return false, nil
	default:
		return false, getErr
	}
}

// haltVerify handles the fail-closed non-APPROVED exit of the verify loop —
// an unreadable envelope, a missing Verdict, a classified BLOCKED verdict,
// an anti-gaming escalation, or MaxIterations exhaustion without an APPROVED
// verdict. exitReason and conditionReason are caller-chosen (haltVerify
// never guesses the class of halt) so ExitIterationsExhausted (repairOrHalt's
// exhaustion leg) stays grep-distinguishable from ExitEscalated (BLOCKED /
// unreadable / anti-gaming).
//
// The terminal patch itself delegates to exhaustVerifyLoop (level_status.go)
// — Phase 52 D-08's ONE branch point: onExhaustion differentiates
// requireApproval (park at AwaitingApproval through the existing gate
// machinery, ESC-02) from escalate (Task's default — the unchanged Phase 51
// Failed-stamp/VerifyHalted + project-wide ConditionVerifyHalt behavior via
// setVerifyHaltIfNeeded, mirroring gateChecks' existing
// setFailureHaltIfNeeded-on-Failed-terminal pattern, this file's own CR-02
// precedent). "BLOCKED verdicts route through the same call" per the plan:
// every one of haltVerify's callers (BLOCKED, unreadable envelope, marshal
// failure, anti-gaming escalation, MaxIterations exhaustion) is a terminal
// non-APPROVED exit and consults policy.EscalationPolicy uniformly — at
// Task defaults (onExhaustion unset) this resolves to escalate, identical
// to Phase 51 (D-02's no-regression bar).
func (r *TaskReconciler) haltVerify(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut, message, conditionReason string, exitReason tideprojectv1alpha3.ExitReason) (ctrl.Result, error) {
	var completedAt time.Time
	if !out.CompletedAt.IsZero() {
		completedAt = out.CompletedAt
	}

	// exhaustVerifyLoop performs its own mutate-then-patch cycle and MUST run
	// before this function's own Status mutations below — see its doc
	// comment for why an earlier mutation would be silently dropped by its
	// DeepCopy-based patch base.
	policy := ResolveLoopPolicy(project, nil, task, "task", r.Deps.VerifyDefaults)
	result, err := exhaustVerifyLoop(ctx, r.Client, project, task, &task.Status.Conditions, &task.Status.Phase, "task", policy, completedAt, message)
	if err != nil {
		return ctrl.Result{}, err
	}

	// The caller-specific ConditionFailed reason (AntiGamingDetected,
	// VerifyIterationsExhausted, VerifyBlocked, ...) + LoopStatus/ExitReason
	// + CompletedAt — a second, focused patch (mirrors
	// consumeApproveAndResume's own two-step shape in level_status.go).
	// Skipped on the requireApproval leg: exhaustVerifyLoop already parked
	// the Task with its own WaveOrLevelPaused/ReasonVerifyExhausted
	// condition, and stamping ConditionFailed=True on a merely-parked (not
	// failed) Task would contradict that state.
	patch := client.MergeFrom(task.DeepCopy())
	if policy.EscalationPolicy != tideprojectv1alpha3.EscalationRequireApproval {
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             conditionReason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
		})
	}
	applyLoopStatus(task, out, exitReason)
	if !completedAt.IsZero() {
		ct := metav1.NewTime(completedAt)
		task.Status.CompletedAt = &ct
	}
	if pErr := r.Status().Patch(ctx, task, patch); pErr != nil {
		return ctrl.Result{}, pErr
	}

	// Plan 53-10: stage + push this Task's findings at the verdict-final seam —
	// covers BOTH exhaustVerifyLoop legs in one place, escalate→VerifyHalted
	// (the frozen-halt blocker case) and requireApproval→AwaitingApproval
	// (ESC-02 park; the operator needs findings to decide `tide approve`).
	// Never gated by dispatch holds (this is a git write, not dispatch); never
	// fails the verdict-final patch already applied above.
	if _, pushErr := r.maybeTriggerTaskFindingsPush(ctx, task, project); pushErr != nil {
		logf.FromContext(ctx).Error(pushErr, "verdict-final findings push trigger failed (non-fatal)", "task", task.Name)
	}

	r.settleVerifierSpend(ctx, task, project, out)
	// finishVerifierTerminal emits the Task-level completion/failure metric
	// counter — deliberately skipped on the requireApproval leg: the Task
	// hasn't reached its REAL terminal yet (it is parked, not done), and the
	// post-approval sentinel's markVerifiedSucceeded call fires it exactly
	// once when the loop genuinely closes (approved or later escalated).
	// Firing it here too would double-count.
	if policy.EscalationPolicy != tideprojectv1alpha3.EscalationRequireApproval {
		r.finishVerifierTerminal(ctx, task, project, out, "verify-halt")
	}
	return result, nil
}

// markVerifiedSucceeded is the sole path that stamps a contract-bearing
// Task Succeeded (EXEC-04/TASK-04): reached only after ClassifyVerdict
// returned APPROVED AND hasDeterministicFailure found no dominating red gate
// — the Execution (in-Job) loop itself never marks a contract-bearing Task
// correct.
func (r *TaskReconciler) markVerifiedSucceeded(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	patch := client.MergeFrom(task.DeepCopy())
	task.Status.Phase = tideprojectv1alpha3.LevelPhaseSucceeded
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             "VerifierApproved",
		Message:            "Independent verifier approved the locked verification contract",
		LastTransitionTime: metav1.Now(),
	})
	applyLoopStatus(task, out, tideprojectv1alpha3.ExitApproved)
	if !out.CompletedAt.IsZero() {
		ct := metav1.NewTime(out.CompletedAt)
		task.Status.CompletedAt = &ct
	}
	if err := r.Status().Patch(ctx, task, patch); err != nil {
		return ctrl.Result{}, err
	}

	// Plan 53-10: stage + push this Task's findings immediately at the
	// approved verdict-final — no waiting for the next boundary push. Never
	// gated by dispatch holds; never fails the Status patch already applied.
	if _, pushErr := r.maybeTriggerTaskFindingsPush(ctx, task, project); pushErr != nil {
		logf.FromContext(ctx).Error(pushErr, "verdict-final findings push trigger failed (non-fatal)", "task", task.Name)
	}

	r.settleVerifierSpend(ctx, task, project, out)
	r.finishVerifierTerminal(ctx, task, project, out, "")
	return ctrl.Result{}, nil
}

// escalateSystem handles the TASK-06 anti-gaming terminal: an attempt's
// RunEvidence.ChangedFiles intersected the protected evaluator/fixture path
// set. This is a SYSTEM escalation, structurally distinct from an ordinary
// BLOCKED/exhausted halt (never counted as a pass, never treated as an
// ordinary repairable finding) — routed through haltVerify with a dedicated
// condition Reason so it stays grep-distinguishable in Task.Status and
// project audit trails.
func (r *TaskReconciler) escalateSystem(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	logf.FromContext(ctx).Info("anti-gaming escalation: attempt touched a protected evaluator/fixture path", "task", task.Name)
	return r.haltVerify(ctx, task, project, out,
		"attempt's changed files intersected the protected evaluator/fixture/threshold path set — system escalation, never a pass",
		"AntiGamingDetected", tideprojectv1alpha3.ExitEscalated)
}

// repairOrHalt implements the REPAIRABLE leg of the verdict decision tree
// (TASK-02/TASK-06). Once task.Status.Attempt reaches
// spec.verification.MaxIterations without an APPROVED verdict, the loop halts
// (TASK-05, onExhaustion). Otherwise a fresh, evidence-seeded attempt is
// dispatched (TASK-02).
//
// The anti-gaming check here is belt-and-suspenders (BL-01): the PRIMARY
// enforcement runs at executor completion (handleJobCompletion) BEFORE a
// verifier is ever dispatched, so a protected-path edit escalates without
// reaching this function at all. This re-check reads the PERSISTED executor
// manifest (Task.Status.LastAttemptEvidence) — never the verifier's
// verdict-only `out`, which carries no RunEvidence (the original BL-01 root
// cause) — so the invariant still holds if a future path reaches repairOrHalt
// without the executor-completion gate.
func (r *TaskReconciler) repairOrHalt(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	if task.Status.LastAttemptEvidence != nil &&
		intersectsProtectedRefs(task.Status.LastAttemptEvidence.ChangedFiles, protectedPathsFor(task)) {
		return r.escalateSystem(ctx, task, project, out)
	}
	// Phase 52 SC3: gate policy resolves through ResolveLoopPolicy, never a
	// raw task.Spec.Verification read — behavior-preserving at Task defaults
	// (D-02): MaxIterations resolves exactly as authored, same
	// Attempt>=MaxIterations comparison (MaxIterations=1 allows zero repairs).
	policy := ResolveLoopPolicy(project, nil, task, "task", r.Deps.VerifyDefaults)
	if task.Status.Attempt >= int(policy.MaxIterations) {
		return r.haltVerify(ctx, task, project, out,
			fmt.Sprintf("verification loop exhausted MaxIterations=%d without an APPROVED verdict", policy.MaxIterations),
			"VerifyIterationsExhausted", tideprojectv1alpha3.ExitIterationsExhausted)
	}
	return r.dispatchRepairAttempt(ctx, task, project, out)
}

// evidencePacket is the compact, reference-only repair-attempt seed
// (TASK-02): the failing findings + a bounded run-evidence summary from the
// verifier's terminal envelope. Never the prior agent's full conversation —
// only what a fresh attempt needs to target its repair, alongside the
// ORIGINAL locked spec (task.Spec.PromptPath, untouched by this packet).
type evidencePacket struct {
	Attempt      int                       `json:"attempt"`
	Summary      string                    `json:"summary,omitempty"`
	Findings     []pkgdispatch.Finding     `json:"findings,omitempty"`
	ChangedFiles []pkgdispatch.ChangedFile `json:"changedFiles,omitempty"`
	Commands     []string                  `json:"commands,omitempty"`
}

// stageEvidencePacket writes a bounded evidence packet (TASK-02) to the
// per-Project PVC and returns its deterministic, workspace-relative path
// ("envelopes/<taskUID>/evidence/attempt-<N>.json", the same envelopes/
// convention every other PVC artifact this package writes/reads uses).
//
// The returned path does NOT depend on the write actually succeeding — it is
// pure string derivation, exactly like task.Spec.PromptPath (the controller
// sets that reference without ever verifying the file is readable ahead of
// time; only the in-pod executor's ReadPrompt validates at read time). A
// write failure here (e.g. the Manager's /workspaces PVC mount is not
// visible — always true under envtest, which has no real PVC) is logged and
// tolerated, never blocking the repair dispatch: the fresh attempt still
// carries the ORIGINAL locked spec regardless of whether the supplementary
// packet landed.
func (r *TaskReconciler) stageEvidencePacket(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) string {
	logger := logf.FromContext(ctx)
	relPath := path.Join("envelopes", string(task.UID), "evidence", fmt.Sprintf("attempt-%d.json", task.Status.Attempt))

	packet := evidencePacket{Attempt: task.Status.Attempt}
	if out.Verdict != nil {
		packet.Summary = out.Verdict.Summary
		findings := out.Verdict.Findings
		if len(findings) > stageEvidencePacketMaxFindings {
			findings = findings[:stageEvidencePacketMaxFindings]
		}
		packet.Findings = findings
	}
	// LO-02/BL-01: source the changed-file/command context from the EXECUTOR's
	// persisted run evidence (Task.Status.LastAttemptEvidence, already bounded),
	// NOT from `out` — `out` here is the verifier's verdict-only envelope, which
	// never carries RunEvidence, so the packet previously shipped an empty
	// ChangedFiles/Commands in production.
	if task.Status.LastAttemptEvidence != nil {
		packet.ChangedFiles = changedFileRefsToDispatch(task.Status.LastAttemptEvidence.ChangedFiles)
		packet.Commands = task.Status.LastAttemptEvidence.Commands
	}

	data, mErr := json.Marshal(packet)
	if mErr != nil {
		logger.Error(mErr, "marshal evidence packet failed (non-fatal); repair attempt dispatches without a packet reference", "task", task.Name)
		return ""
	}

	fullPath := filepath.Join("/workspaces", string(project.UID), "workspace", relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		logger.V(1).Info("evidence packet directory not writable (non-fatal; workspace PVC likely not mounted, e.g. envtest); repair attempt still carries the deterministic path",
			"task", task.Name, "path", fullPath, "error", err.Error())
		return relPath
	}
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		logger.V(1).Info("evidence packet write failed (non-fatal); repair attempt still carries the deterministic path",
			"task", task.Name, "path", fullPath, "error", err.Error())
	}
	return relPath
}

// dispatchRepairAttempt mints a FRESH quality-iteration attempt
// (Attempt++ -> new attemptID) — TASK-02, distinct from an infra-retry/
// eviction rerun, which reconciles the SAME attemptID via checkRunningState's
// existing Job re-read path and never reaches this function (that path only
// ever re-reads or re-dispatches the CURRENT task.Status.Attempt's Job — it
// has no route to repairOrHalt, which is reached exclusively from a
// TERMINAL verifier Job via checkVerifyingState/handleVerifierCompletion).
// The fresh attempt is seeded with the ORIGINAL locked spec
// (task.Spec.PromptPath, re-read fresh by the executor at dispatch time —
// untouched here, never the prior agent's full context) plus a bounded
// evidence packet staged via stageEvidencePacket. Mirrors createDispatchJob's
// Job-build shape but bypasses prepareDispatch's legacy maxAttemptsPerTask
// check entirely — MaxIterations (already checked by repairOrHalt) is the
// authoritative bound for quality-iteration attempts (TASK-05 supersedes the
// blind per-task cap for this path; the eviction/infra-retry path is
// unaffected and untouched by this function).
func (r *TaskReconciler) dispatchRepairAttempt(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	packetPath := r.stageEvidencePacket(ctx, task, project, out)

	attempt, aErr := r.nextAttempt(ctx, task)
	if aErr != nil {
		return ctrl.Result{}, fmt.Errorf("compute next repair attempt: %w", aErr)
	}

	taskCaps := podjob.DefaultCaps(task.Spec.Caps, podjob.JobKindExecutor)
	wallClock := taskCaps.WallClockSeconds
	token, tErr := credproxy.Sign(r.Deps.SigningKey, string(task.UID), time.Duration(wallClock+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if tErr != nil {
		return ctrl.Result{}, fmt.Errorf("mint repair-attempt signed token: %w", tErr)
	}

	_, envInJSON, bErr := r.buildEnvelopeIn(ctx, task, project, attempt, token, packetPath)
	if bErr != nil {
		return ctrl.Result{}, bErr
	}

	patch := client.MergeFromWithOptions(task.DeepCopy(), client.MergeFromWithOptimisticLock{})
	// applyLoopStatus BEFORE reassigning Status.Attempt below: LastEvaluation
	// must summarize the attempt that was JUST verified (Iteration mirrors
	// the OLD Status.Attempt), never the fresh attempt about to dispatch.
	applyLoopStatus(task, out, "") // loop continues: ExitReason stays empty (loop_types.go's own contract)
	task.Status.Attempt = attempt
	task.Status.Phase = tideprojectv1alpha3.LevelPhaseRunning
	now := metav1.Now()
	task.Status.StartedAt = &now
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionRunning,
		Status:             metav1.ConditionTrue,
		Reason:             "RepairAttemptDispatched",
		Message:            fmt.Sprintf("Verifier found repairable findings; dispatching fresh quality-iteration attempt %d", attempt),
		LastTransitionTime: metav1.Now(),
	})
	if pErr := r.Status().Patch(ctx, task, patch); pErr != nil {
		return ctrl.Result{}, pErr
	}

	// Settle the verifier's own outstanding reservation BEFORE this fresh
	// attempt's own Reserve below re-keys the same ReservationStore entry
	// (Reserve overwrites, never adds) — a clean one-reservation-at-a-time
	// handoff, mirroring handleJobCompletion's settleExecutorReservation
	// suppression pattern from Plan 06.
	r.settleVerifierSpend(ctx, task, project, out)

	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	resolvedImage := resolveImage(project, "task", r.Deps.HelmProviderDefaults)
	job := podjob.BuildJobSpec(podjob.BuildOptions{
		Kind:                 podjob.JobKindExecutor,
		Task:                 task,
		ParentObj:            task,
		Level:                "task",
		Project:              project,
		Attempt:              attempt,
		SignedToken:          token,
		EnvelopeInJSON:       envInJSON,
		SubagentImage:        resolvedImage,
		AgentName:            agentName,
		AgentEmail:           agentEmail,
		CredproxyImage:       r.Deps.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              r.sharedPVCName(),
		ProjectUID:           string(project.UID),
		EstimatedCostCents:   r.Deps.ReserveEstimateCents,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
	})
	if oErr := owner.EnsureOwnerRef(job, task, r.Scheme); oErr != nil {
		return ctrl.Result{}, fmt.Errorf("ensure owner ref on repair-attempt job: %w", oErr)
	}
	if cErr := r.Create(ctx, job); cErr != nil {
		if !apierrors.IsAlreadyExists(cErr) {
			return ctrl.Result{}, fmt.Errorf("create repair-attempt job: %w", cErr)
		}
		// AlreadyExists: idempotent success (Pitfall F / SUB-03).
		logger.Info("repair-attempt job already exists; treating as successful dispatch", "job", job.Name)
	}
	if r.Deps.ReserveEstimateCents > 0 {
		r.Deps.Reservations.Reserve(string(task.UID), r.Deps.ReserveEstimateCents)
	}

	logger.Info("dispatched quality-iteration repair attempt", "task", task.Name, "attempt", attempt, "job", job.Name, "evidencePacketPath", packetPath)
	return ctrl.Result{}, nil
}

// verifierEnvelopeReader is the optional role-aware read seam a reader may
// implement (podjob.PodStatusEnvelopeReader does). A contract-bearing Task's
// executor and verifier pods share the task-uid label, and the verifier's
// termination message is the tiny TerminationStub — not an EnvelopeOut — so
// the plain ReadOut can neither select the right pod nor see the verdict
// (found live 2026-07-19: every verify fail-closed VerifierVerdictMissing).
// Readers without the method (envtest fakes, FilesystemEnvelopeReader) fall
// back to ReadOut unchanged.
type verifierEnvelopeReader interface {
	ReadVerifierOut(ctx context.Context, projectUID, taskUID string) (pkgdispatch.EnvelopeOut, error)
}

// readVerifierEnvelope reads the verifier verdict envelope via the role-aware
// seam when the reader provides it, else the plain ReadOut.
func readVerifierEnvelope(ctx context.Context, reader podjob.EnvelopeReader, projectUID, taskUID string) (pkgdispatch.EnvelopeOut, error) {
	if vr, ok := reader.(verifierEnvelopeReader); ok {
		return vr.ReadVerifierOut(ctx, projectUID, taskUID)
	}
	return reader.ReadOut(ctx, projectUID, taskUID)
}

// handleVerifierCompletion consumes a terminal verifier Job's
// EnvelopeOut.Verdict (Plan 07, the BACKWARD half of the verifier
// sub-state-machine Plan 06 opened). Fail-closed by construction: an
// unreadable envelope or a nil Verdict halts via haltVerify (BLOCKED), never
// Succeeded (D-04). ClassifyVerdict drives the three-tier decision:
// APPROVED (and no deterministic gate-command dominance, D-06) ->
// markVerifiedSucceeded; REPAIRABLE, or an APPROVED verdict a red
// gate-command Finding dominates -> repairOrHalt; BLOCKED (and
// ClassifyVerdict's own fail-closed default) -> haltVerify.
func (r *TaskReconciler) handleVerifierCompletion(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, verifierJob *batchv1.Job) (ctrl.Result, error) {
	out, err := readVerifierEnvelope(ctx, r.Deps.EnvReader, string(project.UID), string(task.UID))
	if err != nil {
		// Fail-closed (mirrors handleJobCompletion's EnvelopeReadFailed path,
		// :1251): a Task whose verifier envelope cannot be read is never
		// Succeeded. synthesizeNoEnvelopeOut preserves LoopRunID/AttemptID
		// identity through the degraded envelope (same task.UID+Attempt tuple
		// buildVerifierEnvelopeIn stamped at dispatch time).
		synthOut := synthesizeNoEnvelopeOut(task, verifierJob)
		r.emitEvaluatorSpanForVerifier(ctx, task, project, verifierJob, synthOut, false)
		return r.haltVerify(ctx, task, project, synthOut, err.Error(), "VerifierEnvelopeUnreadable", tideprojectv1alpha3.ExitEscalated)
	}
	if out.Verdict == nil {
		r.emitEvaluatorSpanForVerifier(ctx, task, project, verifierJob, out, true)
		return r.haltVerify(ctx, task, project, out, "verifier envelope carried no verdict (fail-closed BLOCKED)", "VerifierVerdictMissing", tideprojectv1alpha3.ExitEscalated)
	}

	// OBS-03/D-11: the EVALUATOR sibling span, emitted before the terminal
	// status patches below (span-loss-averse ordering, mirrors
	// emitTaskSpanOnce's own call-site ordering in handleJobCompletion).
	r.emitEvaluatorSpanForVerifier(ctx, task, project, verifierJob, out, true)

	// D-04: re-derive the classification through the canonical fail-closed
	// ClassifyVerdict function rather than trusting out.Verdict.Verdict's
	// raw decoded string directly — json.Unmarshal into the Verdict string
	// type does not itself enforce the three-value enum, so an
	// unrecognized/malformed value must still collapse through
	// ClassifyVerdict's own default->BLOCKED branch.
	raw, mErr := json.Marshal(out.Verdict)
	if mErr != nil {
		return r.haltVerify(ctx, task, project, out, mErr.Error(), "VerifierVerdictMarshalFailed", tideprojectv1alpha3.ExitEscalated)
	}

	switch pkgdispatch.ClassifyVerdict(raw) {
	case pkgdispatch.VerdictApproved:
		if hasDeterministicFailure(out.Verdict) {
			// D-06 defence-in-depth: a red gate-command Finding dominates
			// even a top-level APPROVED verdict, controller-side.
			return r.repairOrHalt(ctx, task, project, out)
		}
		return r.markVerifiedSucceeded(ctx, task, project, out)
	case pkgdispatch.VerdictRepairable:
		return r.repairOrHalt(ctx, task, project, out)
	default: // pkgdispatch.VerdictBlocked, and ClassifyVerdict's own fail-closed default.
		return r.haltVerify(ctx, task, project, out, out.Verdict.Summary, "VerifyBlocked", tideprojectv1alpha3.ExitEscalated)
	}
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
