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
	"maps"
	"time"

	"go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/gates"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
	"github.com/jsquirrelz/tide/internal/subagent/common"
	webhookv1alpha3 "github.com/jsquirrelz/tide/internal/webhook/v1alpha3"
	"github.com/jsquirrelz/tide/pkg/dag"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

const planFinalizer = "tideproject.k8s/plan-cleanup"

// maxWaveIntegrationAttempts caps the controller-driven wave-integration
// Job auto-retry (Phase 34 D-04), mirroring the #13b
// maxBoundaryPushAttempts pattern exactly rather than inventing a second
// curve. Once Plan.Status.WaveIntegration.Attempts reaches this constant for
// the current blocking wave, the Plan is marked terminal Failed with
// ReasonWaveIntegrationFailed. A merge conflict (D-09/D-10) skips this
// budget entirely and fails the Plan immediately.
const maxWaveIntegrationAttempts = 5

// Note: ErrParentUnresolved is declared in task_controller.go (same package).
// Phase 04.1 P1.4 — shared across TaskReconciler and PlanReconciler.

// PlanReconciler reconciles a Plan object at Standard depth (D-C1).
// Plan is owned by Phase; the parent ref is set via internal/owner.EnsureOwnerRef.
type PlanReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	// PlannerPool — Plan reconcile dispatches planner-pool subagents.
	PlannerPool  *pool.Pool
	ExecutorPool *pool.Pool

	// Deps carries the dispatch-tier dependencies shared with the
	// Milestone/Phase/Project reconcilers (plan 41-06 consolidation).
	Deps PlannerReconcilerDeps

	// DefaultFileTouchMode is the cluster-level file-touch validation default from
	// the Helm chart (typically "warn"). Matches the PlanCustomValidator field so
	// the reconciler gate (D-05) and the admission webhook use the same baseline
	// when no Project.Spec.PlanAdmission.FileTouchMode is set.
	DefaultFileTouchMode string

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string

	// SharedPVCName is the name of the cluster-wide PVC provisioned by the
	// Helm chart (Plan 12). Defaults to "tide-projects". Configurable via
	// --workspaces-pvc-name flag on the manager (Blocker #2/#3 architecture).
	SharedPVCName string
}

// sharedPVCName returns the configured shared PVC name or the default.
func (r *PlanReconciler) sharedPVCName() string {
	if r.SharedPVCName != "" {
		return r.SharedPVCName
	}
	return defaultSharedPVCName
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=phases,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=waves,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *PlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var plan tideprojectv1alpha3.Plan
	if err := r.Get(ctx, req.NamespacedName, &plan); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !plan.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &plan, planFinalizer,
			func(_ context.Context) error {
				logger.Info("plan cleanup", "name", plan.Name)
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&plan, planFinalizer) {
		controllerutil.AddFinalizer(&plan, planFinalizer)
		if err := r.Update(ctx, &plan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure owner ref to parent Phase (CRD-02, Pitfall 23 prevention).
	// If the Phase is not found (e.g., Plan created before Phase, or Phase deleted),
	// log and continue — owner ref is best-effort; wave materialization must still proceed.
	if plan.Spec.PhaseRef != "" {
		var parent tideprojectv1alpha3.Phase
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			// Phase not found: log and continue without owner ref.
			logger.V(1).Info("parent Phase not found; skipping owner ref", "phaseRef", plan.Spec.PhaseRef)
		} else {
			if err := owner.EnsureOwnerRef(&plan, &parent, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Update(ctx, &plan); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 4b. D-03 (CUTS-01): backfill tideproject.k8s/project on the Plan itself
	// when the label is absent. Heals pre-v1.0.1 Plan CRs on upgraded clusters.
	// Guard: only patch when label is missing so the second reconcile is a no-op
	// (idempotent). Runs BEFORE dispatch so a parked AwaitingApproval Plan
	// self-heals on its first post-upgrade reconcile. Uses resolveProjectName
	// (Plan→Phase→Milestone→Project chain); skips silently on ErrParentUnresolved
	// so orphan Plans stay unlabeled rather than mis-scoped (T-17-03 mitigation).
	if plan.Labels[owner.LabelProject] == "" {
		if name, err := r.resolveProjectName(ctx, &plan); err == nil && name != "" {
			patch := client.MergeFrom(plan.DeepCopy())
			if plan.Labels == nil {
				plan.Labels = map[string]string{}
			}
			plan.Labels[owner.LabelProject] = name
			if err := r.Patch(ctx, &plan, patch); err != nil {
				return ctrl.Result{}, fmt.Errorf("backfill project label on plan %s: %w", plan.Name, err)
			}
		}
	}

	// 5. Dispatcher seam (REQ-SUB-01). Phase 3 splits this:
	// 5a. Planner dispatch — fires when Plan has no Tasks yet (D-A2).
	// 5b. Wave materialization — Phase 2 logic; runs once Tasks exist and
	//     admission webhook stamps Validated.
	if r.Deps.Dispatcher != nil {
		// Phase 52 D-03: a Plan parked in Verifying is mid-plan-check — route
		// straight to the verify state machine before any planner-dispatch or
		// wave-materialization processing runs this reconcile (mirrors Task's
		// gateChecks Step 2b delegation to checkVerifyingState).
		if plan.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying {
			project := r.resolveProjectForPlan(ctx, &plan)
			return r.checkPlanVerifyingState(ctx, &plan, project)
		}
		res, dispatched, err := r.reconcilePlannerDispatch(ctx, &plan)
		if err != nil {
			return res, err
		}
		if dispatched {
			return res, nil
		}
		return r.reconcileWaveMaterialization(ctx, &plan)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonInitialized,
		Message:            "Plan scaffolded; dispatcher not wired",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &plan); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcilePlannerDispatch is the Phase 3 planner-dispatch step (D-A2)
// that runs BEFORE reconcileWaveMaterialization.
//
// Returns (result, dispatched, error):
//   - dispatched=true → the planner-dispatch branch took the reconcile and
//     reconcileWaveMaterialization MUST NOT run on this pass.
//   - dispatched=false → no planner work needed (Tasks already exist or no
//     Project resolvable); the caller should run reconcileWaveMaterialization.
//
// Dispatch is triggered when the Plan has no Tasks AND has not yet reached
// a terminal state. The planner Job has deterministic name
// tide-plan-<plan-uid>-1 (D-B5 dedup). On Job completion, child Task CRDs
// are server-side-created from EnvelopeOut.ChildCRDs; Wave creation is left
// to reconcileWaveMaterialization (Phase 2 path) which fires once the
// admission webhook stamps ValidationState="Validated" on the Plan.
//
//nolint:gocyclo // a flat state machine of mutually-exclusive dispatch arms; splitting obscures the contract
func (r *PlanReconciler) reconcilePlannerDispatch(ctx context.Context, plan *tideprojectv1alpha3.Plan) (ctrl.Result, bool, error) {
	// Phase 12 CR-02 / CR-01 fix: AwaitingApproval early-return placed at the VERY
	// TOP — BEFORE the tasks-exist List — because a parked Plan with
	// reporter-materialized Tasks would otherwise take the tasks-exist exit to
	// dispatched=false, letting reconcileWaveMaterialization run while parked and
	// dispatch executor Jobs without approval.
	// Mirrors milestone_controller.go:216-243 Step 1a, adapted to the (ctrl.Result,
	// bool, error) signature: dispatched=true suppresses reconcileWaveMaterialization.
	if plan.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval {
		if gates.CheckApprove(plan, "plan") {
			// Consume annotation + return to Running + record ApprovedByUser (D-04).
			// Requeue immediately — the Running branch (below) calls
			// handlePlannerJobCompletion which owns ChildCount-gated succession (D-03).
			res, err := consumeApproveAndResume(ctx, r.Client, plan, &plan.Status.Conditions, &plan.Status.Phase, "plan", "Plan approved; Tasks will dispatch")
			return res, true, err
		}
		// No annotation — keep parked; dispatched=true so reconcileWaveMaterialization
		// never runs while parked (GATE-04: no executor Jobs, no Wave CRs).
		// 37-06 Pitfall 8: keep retrying the artifact trigger while parked so the
		// AwaitingApproval early-return cannot permanently swallow it. Re-triggers are
		// harmless (single-flight no-ops while busy; clean-tree skips empty commits).
		if project := r.resolveProjectForPlan(ctx, plan); project != nil {
			if apErr := triggerArtifactPush(ctx, r.Client, r.Scheme, project, "plan", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults); apErr != nil {
				logf.FromContext(ctx).Info("artifact push trigger failed at parked plan (non-fatal)", "plan", plan.Name, "error", apErr.Error())
			}
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, true, nil
	}

	// Phase 52 D-03: defensive guard mirroring the AwaitingApproval
	// early-return's placement discipline above. Reconcile() itself already
	// routes a Verifying Plan straight to checkPlanVerifyingState BEFORE this
	// function is ever called — this is a no-op safety net, not a second
	// entry point into the verify state machine. It guards the crash window
	// a future re-plan (52-09's dispatchPlanRepair) opens between deleting
	// the rejected attempt's child Tasks and patching Phase back off
	// Verifying: without this guard, a mid-transition reconcile that reached
	// this function directly (Tasks momentarily absent) could fall through
	// to the dispatch tail below and double-dispatch a planner Job.
	if plan.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying {
		return ctrl.Result{}, true, nil
	}

	// If Tasks already exist for this Plan, skip planner dispatch — the
	// Phase 2 Wave path runs.
	var taskList tideprojectv1alpha3.TaskList
	if err := r.List(ctx, &taskList,
		client.InNamespace(plan.Namespace),
		client.MatchingFields{taskPlanRefIndexKey: plan.Name},
	); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("list tasks for plan %s: %w", plan.Name, err)
	}
	if len(taskList.Items) > 0 {
		return ctrl.Result{}, false, nil
	}

	// Terminal short-circuit.
	if plan.Status.Phase == tideprojectv1alpha3.LevelPhaseSucceeded || plan.Status.Phase == tideprojectv1alpha3.LevelPhaseFailed {
		return ctrl.Result{}, true, nil
	}

	// Phase 52 D-06/OQ3: LoopStatus.Iteration doubles as the planner Job's
	// attempt identity — it only changes (in dispatchPlanRepair) at the same
	// moment Phase clears off Running, so this formula is stable across every
	// re-reconcile of a Running Plan (see reconcilePlannerDispatch's dispatch
	// tail below, which computes the SAME attempt for the Job it creates).
	jobName := fmt.Sprintf("tide-plan-%s-%d", plan.UID, int(plan.Status.LoopStatus.Iteration)+1)

	// On Running: check Job terminal state.
	if plan.Status.Phase == tideprojectv1alpha3.LevelPhaseRunning {
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, true, err
			}
			// Planner Job is gone (TTL/GC) OR never existed in this cluster — the Plan
			// was materialized by an import with status.phase=Running carried from the
			// seed (import_controller.go:421 "not blanket Succeeded"). Either way the
			// planner already ran and its envelope lives on the PVC keyed by plan.UID,
			// not on the Job. Fall through to completion so the tide-reporter spawns to
			// materialize Task children from the imported envelope and succession fires
			// — without this an imported Plan parks at Running forever with no Job, no
			// reporter, and no Tasks. Mirrors milestone_controller.go:293-296 (GAP-9).
			res, hErr := r.handlePlannerJobCompletion(ctx, plan, nil)
			return res, true, hErr
		}
		if isJobTerminal(&job) {
			res, err := r.handlePlannerJobCompletion(ctx, plan, &job)
			return res, true, err
		}
		return ctrl.Result{}, true, nil
	}

	// D-02 descent hold: if the parent Phase is parked at AwaitingApproval,
	// hold Job dispatch here. The Plan stays at Status.Phase="" so tide approve's
	// findAwaitingPlan cannot target a held child instead of the parked parent
	// (12-RESEARCH.md Pitfall 5). NotFound parent is transient informer lag —
	// checkParentApproval returns (false, nil) and dispatch continues.
	if held, hErr := checkParentApproval(ctx, r.Client, plan.Namespace, plan.Spec.PhaseRef, "Phase"); hErr != nil {
		return ctrl.Result{}, true, hErr
	} else if held {
		logf.FromContext(ctx).V(1).Info("dispatch held: parent Phase awaiting approval",
			"plan", plan.Name, "phase", plan.Spec.PhaseRef)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
	}

	// D-05 dispatch-entry reject hold — resolve Project early to check for a reject
	// annotation before acquiring the pool or creating a Job. A rejected Project
	// halts NEW dispatch; in-flight Jobs drain (no Job deletion — resolved discretion call).
	{
		earlyProject := r.resolveProjectForPlan(ctx, plan)
		if earlyProject != nil && gates.CheckRejected(earlyProject) {
			res, err := r.patchPlanRejected(ctx, plan, gates.RejectedReason(earlyProject))
			return res, true, err
		}
		// Item 7 (Phase 41 D-07): shared planner-tier project-scoped hold chain
		// (Billing/Failure/Budget/Import) -- see checkDispatchHolds in
		// dispatch_helpers.go for the order/requeue rationale.
		if held, res := checkDispatchHolds(ctx, earlyProject, "plan", plan.Name); held {
			return res, true, nil
		}
	}

	// D3 in-flight cap gate — BEFORE pool Acquire (D-03: no slot leak).
	// Counts non-terminal planner Jobs via a cached-client List; returns RequeueAfter
	// (never an error) when the count meets or exceeds the configured cap (CONCUR-04).
	if r.PlannerPool != nil {
		inFlight, err := plannerInFlightCount(ctx, r.Client, r.WatchNamespace)
		if err != nil {
			return ctrl.Result{}, true, fmt.Errorf("planner in-flight count: %w", err)
		}
		if inFlight >= r.PlannerPool.Capacity() {
			logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
				"inFlight", inFlight, "cap", r.PlannerPool.Capacity(), "plan", plan.Name)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, true, nil
		}
	}

	// Acquire plannerPool (POOL-01) before Job creation (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, true, err
		}
		defer r.PlannerPool.Release()
	}

	project := r.resolveProjectForPlan(ctx, plan)

	// Cascade 7: BuildJobSpec drops the credproxy provider Secret when
	// opts.Project==nil (internal/dispatch/podjob/jobspec.go:259-273), causing
	// credproxy to start without ANTHROPIC_API_KEY → CrashLoopBackOff. Dispatch
	// is single-shot (idempotent on AlreadyExists), so the first nil-Project
	// create would permanently wedge the planner. Gate dispatch on Project
	// resolution.
	if project == nil {
		logger := logf.FromContext(ctx).WithValues("plan", plan.Name) //nolint:logcheck // controller-runtime logf idiom used codebase-wide; klogr helper not adopted
		if plan.Spec.PhaseRef == "" {
			// Permanent: empty PhaseRef is a configuration error; admission
			// validation should reject it. Refuse dispatch without requeueing so
			// we don't loop on bad input.
			logger.Info("refusing plan-planner dispatch: plan.spec.phaseRef is empty", "cascade", 7)
			return ctrl.Result{}, false, nil
		}
		// Transient: Phase/Milestone/Project chain not yet visible in informer
		// cache. Requeue to retry once the cache catches up.
		logger.V(1).Info("deferring plan-planner dispatch: project chain not yet resolvable, requeueing", "cascade", 7)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, false, nil
	}

	// Phase 04.1 P1.2 fix: planner Jobs now share the full Phase 2 dispatch
	// contract via podjob.BuildJobSpec(Kind=JobKindPlanner).
	// Phase 52 D-04/OQ3: no longer single-shot — a REPAIRABLE plan-check
	// verdict re-dispatches a fresh planner attempt (dispatchPlanRepair).
	// LoopStatus.Iteration (D-06's quality-re-plan counter, distinct from
	// WaveIntegration.Attempts) doubles as this attempt identity: Plan's
	// planner dispatch never had a pre-existing infra-retry counter of its
	// own to preserve (RESEARCH Open Question 3's resolved answer).
	attempt := int(plan.Status.LoopStatus.Iteration) + 1

	plannerCaps := podjob.DefaultCaps(nil, podjob.JobKindPlanner)
	if plannerCaps.Iterations <= 0 {
		plannerCaps.Iterations = defaultPlannerIterations
	}
	plannerPrompt := outcomePromptOf(project)
	envIn, envInJSON, err := BuildPlannerEnvelope("plan", plan, project, attempt, "", plannerPrompt, pkgdispatch.Caps{
		WallClockSeconds: int(plannerCaps.WallClockSeconds),
		Iterations:       int(plannerCaps.Iterations),
	}, credproxyEndpoint, r.Deps.HelmProviderDefaults, plan.Spec.SharedContext)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("build planner envelope: %w", err)
	}

	// Phase 52 D-04: a re-plan attempt's bounded findings block — staged by
	// dispatchPlanRepair onto replanFindingsAnnotation — seeds this fresh
	// planner's prompt via the 52-03-pinned EnvelopeIn.RepairFindings field.
	// Re-marshal only when present: the common (non-re-plan) case leaves
	// envInJSON byte-identical to today's output.
	if repairFindings := decodeReplanFindings(plan); len(repairFindings) > 0 {
		envIn.RepairFindings = repairFindings
		envInJSON, err = json.Marshal(envIn)
		if err != nil {
			return ctrl.Result{}, true, fmt.Errorf("marshal planner envelope with repair findings: %w", err)
		}
	}

	// Mint a signed token for the credproxy sidecar.
	token, err := credproxy.Sign(r.Deps.SigningKey, string(plan.UID), time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("mint planner signed token: %w", err)
	}

	var secretUID string
	if project != nil && project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}

	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	// SIGN-01 / D-03: resolve committer/author identity (mirrors resolveImage's
	// HelmProviderDefaults tier) and stamp it into the planner Job env. The
	// resolver is nil-safe, so a nil project resolves to the chart tier /
	// compiled default without a caller-side guard.
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	resolvedImage := resolveImage(project, "plan", r.Deps.HelmProviderDefaults)
	// D-02 / T-40-12: log the resolved model at dispatch — previously the
	// resolved model appeared nowhere outside the PVC envelope.
	logf.FromContext(ctx).Info("resolved subagent dispatch", "level", "plan", "model", envIn.Provider.Model, "image", resolvedImage)

	// PROP-01: Plan's immediate parent is Phase — resolveProjectForPlan's label
	// fast-path never touches Phase, so this is a genuinely new fetch (mirrors
	// the identical fetch in handlePlannerJobCompletion). A missing PhaseRef or
	// a failed Get degrades to an empty hex, which traceparentForLevel/
	// FormatTraceparent already turn into an omitted env var.
	parentSpanIDHex := ""
	if plan.Spec.PhaseRef != "" {
		var parentPh tideprojectv1alpha3.Phase
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &parentPh); err == nil {
			parentSpanIDHex = parentPh.Status.PhaseTraceSpanID
		}
	}
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindPlanner,
		ParentObj:            plan,
		Level:                "plan",
		Attempt:              attempt,
		Project:              project,
		SignedToken:          token,
		EnvelopeInJSON:       envInJSON,
		SubagentImage:        resolvedImage,
		AgentName:            agentName,
		AgentEmail:           agentEmail,
		CredproxyImage:       r.Deps.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              r.sharedPVCName(),
		ProjectUID:           projectUID,
		Caps:                 plannerCaps,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
		// D-02/Phase 46: literal true — cross-reconcile dispatch-time site;
		// the parent's sampled bit is not persisted (RESEARCH Pitfall 3's
		// rejected schema change; see docs/observability.md).
		TraceParent: traceparentForLevel(project, parentSpanIDHex, true),
	}
	job := podjob.BuildJobSpec(opts)
	if err := owner.EnsureOwnerRef(job, plan, r.Scheme); err != nil {
		return ctrl.Result{}, true, fmt.Errorf("ensure owner ref on planner job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, true, fmt.Errorf("create planner job: %w", err)
		}
		// AlreadyExists: idempotent success.
	}

	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.Phase = tideprojectv1alpha3.LevelPhaseRunning
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionAuthoringPlanner,
		Status:             metav1.ConditionTrue,
		Reason:             "PlannerDispatched",
		Message:            fmt.Sprintf("Planner Job %s dispatched", jobName),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, true, err
	}

	return ctrl.Result{}, true, nil
}

// handlePlannerJobCompletion reads tiny status from the completed planner Job,
// spawns the tide-reporter reader Job to materialize Task child CRDs from the
// PVC-side out.json, and clears the Running phase so the Phase 2 Wave path can
// pick up on the next reconcile.
//
// Materialization is now the reporter Job's job (Phase 09 plan 09-06, REQ-09-01).
// Children (Tasks + Waves) arrive via the existing Owns watches once the reporter
// creates them. The reporter also stamps ValidationState=Validated in a follow-up
// reconcile when child Tasks are observed (reconcileWaveMaterialization gate).
//
// Note: This does NOT create Waves — the existing reconcileWaveMaterialization
// handles that once the admission webhook stamps ValidationState=Validated.
//
//nolint:gocyclo // a flat state machine of mutually-exclusive completion arms; splitting obscures the contract
func (r *PlanReconciler) handlePlannerJobCompletion(ctx context.Context, plan *tideprojectv1alpha3.Plan, completedJob *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Phase 52 D-04: consume (clear) the replan-findings annotation now that
	// the planner attempt it seeded has completed — mirrors the
	// approve-annotation "consume once acted on" idiom. A no-op for every
	// ordinary (non-re-plan) completion: the annotation is only ever present
	// starting from the SECOND planner attempt onward.
	if cErr := r.clearReplanFindingsAnnotation(ctx, plan); cErr != nil {
		logger.Error(cErr, "clear replan-findings annotation failed (non-fatal)", "plan", plan.Name)
	}

	project := r.resolveProjectForPlan(ctx, plan)
	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	// Phase 12 / Phase 04.1: reject short-circuit FIRST — operator stop should always
	// halt, regardless of envelope availability or read errors.
	// Mirrors milestone_controller.go:442-449 ("reject short-circuit FIRST").
	// D-05: park (not fail) — in-flight Jobs drain; state is preserved for resume.
	if project != nil && gates.CheckRejected(project) {
		return r.patchPlanRejected(ctx, plan, gates.RejectedReason(project))
	}

	// Read tiny status from the dispatch Job's termination message for budget
	// rollup and failure classification. ChildCRDs are NOT used here —
	// materialization has moved to the reporter Job (REQ-09-01).
	// Plan 09-08: capture out so we can gate on out.ChildCount below.
	//
	// Phase 17 DEBT-04 (CR-01): Pitfall-1 parity with milestone/phase controllers.
	// A transient PVC/read error must not wedge the Plan to terminal Status.Phase=Failed.
	// Track envReaderPresent to distinguish nil-reader (unit-test / non-Option-C path)
	// from read-error (transient); envReadOK gates the envelope-dependent downstream.
	var out pkgdispatch.EnvelopeOut
	envReadOK := false
	envReaderPresent := r.Deps.EnvReader != nil
	if r.Deps.EnvReader == nil {
		// Fallback: no EnvReader (non-Option-C / unit-test path). Clear Running phase
		// immediately and let the Wave path take over, mirroring prior behavior.
		logger.Info("no env reader; clearing Running phase to let Wave path proceed")
		patch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = ""
		_ = r.Status().Patch(ctx, plan, patch)
		return ctrl.Result{}, nil
	}

	var readErr error
	out, readErr = r.Deps.EnvReader.ReadOut(ctx, projectUID, string(plan.UID))
	if readErr != nil {
		// Non-fatal: log and defer to children-based succession (Pitfall-1 parity with
		// milestone_controller.go:535-539 and phase_controller.go:476-479). A transient
		// read error must not permanently wedge the Plan — the envelope is a status
		// optimization, not the success authority.
		logger.Error(readErr, "plan planner envelope tiny-status read failed (non-fatal); deferring to children-based succession", "plan", plan.Name)
	} else {
		envReadOK = true
	}

	// Phase 42 D-01/D-02/D-04: synthesize at most one retroactive AGENT span
	// per planner Job attempt, gated by the durable PlanSpanEmittedUID
	// marker keyed by Job UID (42-REVIEW WR-02: planner Job names are
	// deterministic, so a deleted-and-recreated attempt reuses the name but
	// never the UID — D-02 requires each attempt to produce its own span) —
	// INDEPENDENT of envReadOK and isFirstCompletion (Pitfall 2: the
	// existing PlanRolledUpUID marker below is envReadOK-gated by design
	// and would re-emit a degraded span on every reconcile forever if reused
	// here). Ordering is mark-then-emit (42-REVIEW WR-01): the marker is
	// stamped durably BEFORE the span is exported — the optimistic-lock patch
	// closes the stale-cache re-entry window, making duplicate emission
	// impossible; a crash between stamp and emission loses that attempt's
	// span, preferred over double-counting tokens/cost in Phoenix. Pattern 3:
	// plannerSpanResolvable refuses a nil completedJob (already TTL-GC'd) or
	// a Job with no resolvable timestamps — checked BEFORE stamping so a
	// stamp is never wasted on an unemittable span.
	// D-02/Phase 46: default true — matches today's behavior (and RESEARCH's
	// SDK read that every non-root span is AlwaysSample'd) when no emission
	// runs this reconcile (marker already stamped by an earlier attempt);
	// overwritten below with the real bit when this reconcile DOES emit.
	sampled := true
	if completedJob != nil && project != nil && plan.Status.PlanSpanEmittedUID != string(completedJob.UID) && plannerSpanResolvable(completedJob) {
		stamped := false
		if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &tideprojectv1alpha3.Plan{}
			if err := r.Get(ctx, client.ObjectKeyFromObject(plan), latest); err != nil {
				return err
			}
			if latest.Status.PlanSpanEmittedUID == string(completedJob.UID) {
				return nil // already stamped by a concurrent reconcile — its stamper emits
			}
			markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
			latest.Status.PlanSpanEmittedUID = string(completedJob.UID)
			if err := r.Status().Patch(ctx, latest, markerPatch); err != nil {
				return err
			}
			stamped = true
			return nil
		}); mErr != nil {
			// 42-REVIEW WR-03: telemetry bookkeeping is subordinate to pipeline
			// progression — a persistent status-patch failure must not block the
			// reporter spawn, budget rollup, or gate hooks below. No error return,
			// no requeue: the marker is still unset, so the next watch-driven
			// reconcile retries the stamp (mark-then-emit guarantees this degrade
			// path accrues no duplicate span).
			logger.Error(mErr, "PlanSpanEmittedUID marker patch failed (non-fatal); span deferred to a later reconcile", "plan", plan.Name)
		} else if stamped {
			// TRACE-02: Plan's immediate parent is Phase — resolveProjectForPlan's
			// label fast-path never touches Phase, so this is a genuinely new
			// fetch (RESEARCH.md A3). A missing PhaseRef or a failed Get degrades
			// to an unnested span (zero parentSpanID) rather than blocking
			// emission — the span still groups by the deterministic TraceID.
			var parentSpanID trace.SpanID
			if plan.Spec.PhaseRef != "" {
				var parentPh tideprojectv1alpha3.Phase
				if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &parentPh); err == nil {
					parentSpanID = spanIDFromHexOrZero(parentPh.Status.PhaseTraceSpanID)
				}
			}
			thisSpanID, gotSampled, emitted := synthesizePlannerSpan(ctx, "plan", plan.Name, "", project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK, parentSpanID)
			if emitted {
				sampled = gotSampled
				// Mirror in-memory unconditionally so same-reconcile downstream
				// logic reads it even if the persistence patch below fails.
				plan.Status.PlanTraceSpanID = thisSpanID.String()
				if tErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					latest := &tideprojectv1alpha3.Plan{}
					if err := r.Get(ctx, client.ObjectKeyFromObject(plan), latest); err != nil {
						return err
					}
					tracePatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
					latest.Status.PlanTraceSpanID = thisSpanID.String()
					return r.Status().Patch(ctx, latest, tracePatch)
				}); tErr != nil {
					// PROP-02/Pitfall 2: non-fatal — this is a SEPARATE, later patch
					// from the marker stamp above (the span ID isn't known until
					// synthesizePlannerSpan returns).
					logger.Error(tErr, "PlanTraceSpanID patch failed (non-fatal); child parent-linkage degraded for this level", "plan", plan.Name)
				}
			}
		}
	}

	// Spawn the tide-reporter reader Job in the project namespace (Option C).
	// The reporter reads out.json from the PVC and materializes Task children.
	// Children arrive via the Owns(&Task{}) / Owns(&Wave{}) watch once created.
	// T-09-13: idempotent — AlreadyExists on Create is success.
	//
	// Phase 47 CR-01 gap-closure: the inline Get→IsNotFound→Create gate below is
	// name-only — it re-opens after the reporter Job's 300s TTL-GC
	// (reporter_jobspec.go), letting a sustained-reconcile parent re-Create a
	// duplicate reporter with freshly-recomputed ReporterOptions. spawnKey is the
	// durable per-attempt guard: the completed planner Job's UID, falling back to
	// the deterministic planJobName when completedJob is nil (already TTL-GC'd or
	// never observed).
	// Phase 52 D-04/OQ3 (Rule 1 fix): must use the SAME Iteration-derived
	// attempt formula as reconcilePlannerDispatch's own jobName/attempt sites
	// — a hardcoded "-1" here silently broke PlanRolledUpUID's exactly-once
	// budget-rollup marker comparison below for any re-planned (attempt>1)
	// completion, since planJobName would never match the attempt-2+ Job
	// that was actually just processed.
	planJobName := fmt.Sprintf("tide-plan-%s-%d", plan.UID, int(plan.Status.LoopStatus.Iteration)+1)
	spawnKey := planJobName
	if completedJob != nil {
		spawnKey = string(completedJob.UID)
	}
	// Phase 47 CR-01 re-fix: once ANY marker is set, a later nil-Job reconcile
	// (planner Job GC'd at its 600s TTL) must NOT recompute a name-derived key and
	// re-open the gate. The stored marker is the live Job UID (stamped while the Job
	// was present); a UID can never equal the deterministic name, so honor a
	// non-empty marker directly on the nil-Job path. Only when a Job IS present
	// (a genuinely new attempt) does the per-attempt UID equality decide.
	alreadySpawned := plan.Status.PlanReporterSpawnedUID != "" &&
		(completedJob == nil || plan.Status.PlanReporterSpawnedUID == spawnKey)
	if alreadySpawned {
		// Already spawned for this attempt (durable marker honored) — skip the Create.
	} else if r.Deps.ReporterImage != "" && project != nil {
		reporterJobName := fmt.Sprintf("tide-reporter-%s", plan.UID)
		pvcName := r.sharedPVCName()
		var existingReporterJob batchv1.Job
		if gErr := r.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: plan.Namespace}, &existingReporterJob); gErr != nil {
			if !apierrors.IsNotFound(gErr) {
				return ctrl.Result{}, fmt.Errorf("get reporter job %s: %w", reporterJobName, gErr)
			}
			skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "plan", r.Deps.HelmProviderDefaults).Vendor)
			// 46 D-05/OBS-02/OBS-03: enrichment values computed from the SAME
			// inputs this level's AGENT span used above, so the reporter's LLM
			// spans carry byte-identical session.id/metadata/tags.
			enrichmentMD, enrichmentTags := buildLevelEnrichment(project, "plan", plan.Name, "")
			reporterJob := BuildReporterJob(plan, project, pvcName, string(plan.UID), "Plan",
				ReporterOptions{
					ReporterImage:     r.Deps.ReporterImage,
					TraceParent:       traceparentForLevel(project, plan.Status.PlanTraceSpanID, sampled),
					OTLPEndpoint:      r.Deps.OTLPEndpoint,
					OTLPHeadersSecret: r.Deps.OTLPHeadersSecret,
					SkipMessageSpans:  skipMessageSpans,
					SessionID:         projectUID,
					MetadataJSON:      enrichmentMD,
					Tags:              enrichmentTags,
				}, r.Scheme)
			if cErr := r.Create(ctx, reporterJob); cErr != nil {
				if !apierrors.IsAlreadyExists(cErr) {
					return ctrl.Result{}, fmt.Errorf("create reporter job %s: %w", reporterJobName, cErr)
				}
			} else {
				logger.Info("spawned reporter Job", "job", reporterJobName, "plan", plan.Name)
			}
		} else {
			logger.V(1).Info("reporter Job already exists; skipping spawn (T-09-13)", "job", reporterJobName)
		}
		// Stamp the durable marker: a reporter Job verifiably exists for this
		// attempt (newly-Created, AlreadyExists, or found-by-name — every branch
		// above reaches here without an early return). Stamped on every spawn path
		// so it also back-fills the marker for reporters spawned before this fix
		// landed. WR-03: on RetryOnConflict exhaustion, return the error to requeue
		// — the marker must be durable before the TTL-GC window re-opens the
		// name-only gate.
		if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &tideprojectv1alpha3.Plan{}
			if err := r.Get(ctx, client.ObjectKeyFromObject(plan), latest); err != nil {
				return err
			}
			if latest.Status.PlanReporterSpawnedUID == spawnKey {
				return nil // already set by a concurrent reconcile — idempotent
			}
			markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
			latest.Status.PlanReporterSpawnedUID = spawnKey
			return r.Status().Patch(ctx, latest, markerPatch)
		}); mErr != nil {
			return ctrl.Result{}, fmt.Errorf("patch PlanReporterSpawnedUID: %w", mErr)
		}
	} else if r.Deps.ReporterImage == "" {
		logger.V(1).Info("skipping reporter Job spawn: ReporterImage not configured", "plan", plan.Name)
	}

	// Plan 09-08 Defect C: roll up planner-level Usage once per planner Job completion.
	// Guard on envReadOK: out.Usage is only valid when the envelope read succeeded.
	//
	// Phase 31 D-03a / T-31-07: isFirstCompletion flips true again after the reporter
	// Job's 300s TTL-GC window, causing double-count on halt→resume. Gate on the
	// durable PlanRolledUpUID marker (lives in CRD .status, survives restart)
	// to guarantee exactly-once rollup regardless of TTL-GC (ADOPT-04).
	// D-03a: the plan level previously had no marker — this is a new addition.
	//
	// Phase 47 WR-01: the rollup is decoupled from isFirstCompletion — after the
	// CR-01 re-fix, isFirstCompletion is true only on the single reconcile that
	// spawns, so a transient RollUpUsage failure there would never be retried and
	// the spend would be silently lost. The durable PlanRolledUpUID marker is the
	// sole exactly-once guard (mirrors the *SpanEmittedUID idiom): every later
	// reconcile retries until RollUpUsage succeeds, then latches.
	if envReadOK && project != nil {
		if plan.Status.PlanRolledUpUID != planJobName {
			if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
				logger.Error(rollErr, "plan planner budget rollup failed (non-fatal)", "plan", plan.Name)
			} else {
				// Stamp the durable marker only after a successful rollup (Pitfall-2 ordering).
				// WR-02: re-fetch + RetryOnConflict + MergeFromWithOptimisticLock mirrors RollUpUsage,
				// making the stamp durable against concurrent status writes on this level object.
				// WR-03: on retry-budget exhaustion, return the error to requeue rather than swallow —
				// the marker must be durably set before the reporter Job's TTL-GC window reopens rollup.
				if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					latest := &tideprojectv1alpha3.Plan{}
					if err := r.Get(ctx, client.ObjectKeyFromObject(plan), latest); err != nil {
						return err
					}
					if latest.Status.PlanRolledUpUID == planJobName {
						return nil // already set by a concurrent reconcile — idempotent
					}
					markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
					latest.Status.PlanRolledUpUID = planJobName
					return r.Status().Patch(ctx, latest, markerPatch)
				}); mErr != nil {
					return ctrl.Result{}, fmt.Errorf("patch PlanRolledUpUID: %w", mErr)
				}
			}
			// Phase 38 COST-02: surface an unknown-model pricing fallback carried
			// on the envelope — condition + metric, bounded by the same
			// exactly-once rollup guards. Non-fatal: informational only.
			if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
				logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "plan", plan.Name)
			}
		}
	}

	// Phase 13 D-04 layer 2: backstop — classify planner-envelope failure Reason.
	// Guard on envReadOK: out.ExitCode/Reason are only valid when the envelope read succeeded.
	if envReadOK && out.ExitCode != 0 && project != nil {
		var jobStart time.Time
		if completedJob != nil {
			jobStart = completedJob.CreationTimestamp.Time
		}
		if hErr := setBillingHaltIfNeeded(ctx, r.Client, project, out.Reason, jobStart); hErr != nil {
			logger.Error(hErr, "setBillingHaltIfNeeded failed (non-fatal)", "plan", plan.Name)
		}
	}

	// REQ-7a: stamp ValidationState=Validated so reconcileWaveMaterialization
	// proceeds past the gate. Only stamp when the envelope read succeeded (i.e. we
	// have a valid tiny status) — the reporter Job is in flight, Tasks will appear shortly.
	// On a read error, skip the stamp and fall through to the children-based fallback below
	// (Pitfall-1 parity: the envelope is a status optimization, not the success authority).
	if envReadOK {
		valPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.ValidationState = "Validated"
		if err := r.Status().Patch(ctx, plan, valPatch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Phase 12 CR-01 fix: gate-policy hook moved BEFORE the ChildCount requeue so
	// the gate fires even when ChildCount>0. Previously the ChildCount requeue
	// returned first and patchPlanAwaitingApproval never ran for non-leaf Plans.
	// Position comment: the reporter Job was already spawned above, so children
	// keep materializing while parked — D-02 "materialize children, hold dispatch".
	// ValidationState=Validated is already stamped so the wave path is armed the
	// moment approval lands. Mirrors milestone_controller.go:510-553.
	if project != nil {
		policy := gates.EvaluatePolicy(project.Spec.Gates, "plan")
		if policy == gates.PolicyApprove || policy == gates.PolicyPause {
			// Check if this level was already approved (permanent ApprovedByUser or
			// ResumedByUser condition with Status=False means the park was lifted).
			// Prevents re-parking after the Edit-1 AwaitingApproval branch approved
			// the level — without this guard the consumed annotation re-parks on the
			// next pass through this function.
			alreadyApproved := false
			if c := meta.FindStatusCondition(plan.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused); c != nil {
				if c.Status == metav1.ConditionFalse &&
					(c.Reason == tideprojectv1alpha3.ReasonApprovedByUser || c.Reason == tideprojectv1alpha3.ReasonResumedByUser) {
					alreadyApproved = true
				}
			}
			if !alreadyApproved {
				if !gates.CheckApprove(plan, "plan") {
					// No annotation and not yet approved — park.
					// 37-06 / DASH-02 (D-01): stage the cumulative planner-artifact map
					// BEFORE the gate-park return. Park arm ONLY (not succeed) so it never
					// preempts the plan boundary push, which carries the task-branch
					// integration (D-04) and shares the deterministic Job name (R-05). The
					// parked-arm retry re-attempts until it lands.
					if apErr := triggerArtifactPush(ctx, r.Client, r.Scheme, project, "plan", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults); apErr != nil {
						logger.Info("artifact push trigger failed at plan park (non-fatal)", "plan", plan.Name, "error", apErr.Error())
					}
					return r.patchPlanAwaitingApproval(ctx, plan, policy)
				}
				// Annotation present at the hook (operator approved before the park fired):
				// consume it and write Running+ApprovedByUser so the condition is recorded
				// for future reconciles — otherwise the next reconcile would re-park because
				// the annotation is gone but no approval record exists.
				if _, err := consumeApproveAndResume(ctx, r.Client, plan, &plan.Status.Conditions, &plan.Status.Phase, "plan", "Plan approved; Tasks will dispatch"); err != nil {
					return ctrl.Result{}, err
				}
				// Fall through to ChildCount-gated succession (D-03).
			}
			// alreadyApproved: fall through to ChildCount-gated succession.
		}
	}

	// Plan 09-08 Defect B fix: uniform ChildCount-gated succession replaces the
	// prior reporterSpawned early-return. Gate:
	//   expected == 0            → clear Running immediately (genuine leaf: no Tasks)
	//   observed < expected      → requeue 5s (reporter still materializing Tasks)
	//   observed >= expected     → clear Running, let Wave path take over
	// The plan controller does NOT call patchPlanSucceeded here — succession
	// happens in reconcileWaveMaterialization once all Tasks complete.
	//
	// Phase 17 DEBT-04: when envReadOK=false (transient read error), out.ChildCount is
	// unreliable. Use the children-based fallback instead (Pitfall-1 parity):
	//   - reader present but errored AND no children yet → requeue (envelope may have ChildCount>0)
	//   - reader present but errored AND children already exist → fall through (reporter is in flight)
	// This mirrors phase_controller.go:617-621.
	if envReadOK {
		expected := out.ChildCount
		if expected > 0 {
			observed := r.countChildTasks(ctx, plan)
			if observed < expected {
				logger.V(1).Info("requeue: reporter still materializing Task children",
					"plan", plan.Name, "expected", expected, "observed", observed)
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
		}
	} else if envReaderPresent && r.countChildTasks(ctx, plan) == 0 {
		// Reader exists but had a read error AND no children observed yet — the envelope
		// may have ChildCount>0 (children still materializing). Requeue; don't auto-succeed.
		logger.V(1).Info("boundary push deferred: env reader present but unreadable, waiting (fallback)", "plan", plan.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Plan 04-06 W-2: boundary push trigger AFTER gate, BEFORE clearing
	// the Running phase. Plan boundary is the only D-B2 shape with the
	// `+ executed` suffix (Tasks have already run by this seam).
	//
	// CR-03 partial-fix note: the milestone/phase controllers now gate the
	// push on gates.BoundaryDetected, but the plan controller does NOT,
	// because the plan reconcile path is structurally different. Once child
	// Tasks exist, reconcilePlannerDispatch returns early
	// (dispatched=false → reconcileWaveMaterialization) without entering
	// handlePlannerJobCompletion, so any BoundaryDetected gate here becomes
	// unreachable when children are present. Properly tightening the plan
	// boundary requires firing the push from a separate seam in the wave-
	// materialization path on task-status updates (out of REVIEW-FIX scope).
	// Documented in 04-REVIEW-FIX.md.
	// At planner-Job completion time, Tasks do not yet exist (the planner just
	// materialized them). Phase 34 D-03: maybeTriggerBoundaryPush no longer
	// takes a branches parameter — triggerBoundaryPush computes the
	// cumulative Succeeded-branch set itself via a live List, which is
	// naturally empty here (no Tasks yet) and self-heals on the next trigger
	// once Tasks exist (handled in reconcileWaveMaterialization).
	if err := r.maybeTriggerBoundaryPush(ctx, plan, project); err != nil {
		if errors.Is(err, errGitWriterBusy) {
			// D-02: another git-writer Job is in flight — normal
			// serialization, not a failure. Requeue and retry.
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	// Phase 52 D-03: a Plan whose plan-check verification contract is
	// resolved (Task > Plan > Project precedence via ResolveVerificationSpec)
	// AND Locked enters Verifying instead of clearing to "" — the
	// checkParentApproval OR-clause (dispatch_helpers.go) structurally holds
	// child Task dispatch until the plan-check verdict is APPROVED. This is
	// the cheapest pre-spend rejection point: child CRDs already exist for
	// free (the reporter finished materializing them above), dispatch is
	// what spends. Absence of a resolved contract (empty GateCommand, or a
	// still-Draft spec — the same OQ2 activation key hasVerificationContract
	// uses at the Task level) preserves today's clear-to-"" behavior
	// byte-for-byte (D-01's off-switch).
	verification := ResolveVerificationSpec(project, plan, nil, "plan")
	patch := client.MergeFrom(plan.DeepCopy())
	if verification.GateCommand != "" && verification.Phase == verificationPhaseLocked {
		plan.Status.Phase = tideprojectv1alpha3.LevelPhaseVerifying
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionReconciling,
			Status:             metav1.ConditionTrue,
			Reason:             "PlanCheckDispatched",
			Message:            "Planner materialized child Tasks; dispatching an independent plan-check verifier before any Task dispatches",
			LastTransitionTime: metav1.Now(),
		})
	} else {
		// Clear Running phase so the Phase 2 Wave path takes over on next reconcile.
		plan.Status.Phase = ""
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:             metav1.ConditionFalse,
			Reason:             tideprojectv1alpha3.ReasonResumedByUser,
			Message:            "Plan resumed from gate boundary",
			LastTransitionTime: metav1.Now(),
		})
	}
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// ---------------------------------------------------------------------------
// Phase 52 D-03/D-10: plan-check loop (Plan-level verifier dispatch/consume).
// Mirrors the Task loop's checkVerifyingState/dispatchVerifier/
// handleVerifierCompletion state machine (task_controller.go) one level up.
// Every D-10 safety rail (ESC-04 concurrency cap, the shared ReservationStore,
// fail-closed ClassifyVerdict, the EVALUATOR sibling span) rides the SAME
// functions Task's verifier dispatch already uses — nothing here
// re-implements a rail, only adds the Plan-scoped call site.
// ---------------------------------------------------------------------------

// checkPlanVerifyingState handles a Plan in Phase=Verifying (Phase 52 D-03):
// looks up the deterministic plan-check verifier Job for the current attempt
// and either retries a deferred dispatch, waits for it to complete, or
// consumes its verdict via handlePlanVerifierCompletion. Mirrors Task's
// checkVerifyingState (task_controller.go) exactly, one level up.
//
// attempt = int(plan.Status.LoopStatus.Iteration) + 1 — the D-06 quality
// counter doubles as the verifier Job's attempt number. Unlike Task (whose
// executor Attempt already carries an infra-retry identity Phase 51 had to
// preserve), a Plan's planner dispatch has no pre-existing per-attempt
// counter of its own (RESEARCH Open Question 3's resolved answer), so
// LoopStatus.Iteration — which never repairs in this plan (52-09 owns the
// increment) — is safe to reuse directly as the sole source.
func (r *PlanReconciler) checkPlanVerifyingState(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project) (ctrl.Result, error) {
	if project == nil {
		// Transient: informer-cache lag on the label fast-path or the
		// Phase→Milestone→Project chain — requeue and retry, mirroring Task's
		// ErrParentUnresolved handling in its own checkVerifyingState.
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	attempt := int(plan.Status.LoopStatus.Iteration) + 1
	jobName := podjob.VerifierJobName("plan", string(plan.UID), attempt)
	var job batchv1.Job
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: jobName}, &job); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// NotFound is a legitimate "cap-hit deferred the dispatch" state
		// (ESC-04) — retry dispatchPlanVerifier (idempotent via the
		// deterministic Job name).
		result, _, dErr := r.dispatchPlanVerifier(ctx, plan, project)
		return result, dErr
	}
	if isJobTerminal(&job) {
		return r.handlePlanVerifierCompletion(ctx, plan, project, &job)
	}
	// Still running: nothing to do this reconcile — the Job watch fires
	// again on the verifier Job's terminal transition (Owns(&batchv1.Job{})).
	return ctrl.Result{}, nil
}

// planVerifierChildrenCap bounds the {{.Children}} render-data summary
// (52-03 D-09's pinned contract) — a plan authoring hundreds of child Tasks
// must not blow up the rendered prompt size.
const planVerifierChildrenCap = 50

// planVerifierChildSummary mirrors internal/subagent/common's test-only
// childFixture (52-03's pinned render-data contract for plan_verifier.tmpl's
// {{range .Children}}) — this is the production equivalent dispatch sites
// populate.
type planVerifierChildSummary struct {
	Name        string
	DependsOn   []string
	Files       []string
	GateCommand string
}

// planVerifierRenderData mirrors internal/subagent/common's test-only
// planVerifierFixture — the production render-data contract for
// plan_verifier.tmpl (D-09): embeds EnvelopeIn (.Verify/.TaskUID/...) plus
// PlanGoal/Children.
type planVerifierRenderData struct {
	pkgdispatch.EnvelopeIn
	PlanGoal string
	Children []planVerifierChildSummary
}

// dispatchPlanVerifier creates the independent, read-only plan-check
// verifier Job for a Plan whose planner attempt has materialized its child
// Task CRDs (D-03). Mirrors Task's dispatchVerifier (task_controller.go)
// exactly, one level up: cap-before-reserve ordering (ESC-04/D-10, Pitfall
// 6 — no reservation leak on cap-hit), and the deterministic
// VerifierJobName makes a retry (e.g. after a prior cap-hit deferred
// dispatch) idempotent via AlreadyExists-as-success (SUB-03).
func (r *PlanReconciler) dispatchPlanVerifier(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project) (result ctrl.Result, reserved bool, err error) {
	logger := logf.FromContext(ctx)
	attempt := int(plan.Status.LoopStatus.Iteration) + 1
	verifierJobName := podjob.VerifierJobName("plan", string(plan.UID), attempt)

	// LO-01 parity: no verifier image configured (TIDE_VERIFIER_IMAGE unset)
	// — leave the Plan benignly parked in Verifying rather than build an
	// unschedulable Job.
	if r.Deps.VerifierImage == "" {
		logger.Info("verifier image not configured (TIDE_VERIFIER_IMAGE empty); leaving Plan parked in Verifying without dispatching a plan-check verifier Job",
			"plan", plan.Name)
		return ctrl.Result{}, false, nil
	}

	// ESC-04/D-10: cap-before-acquire (Pitfall 6). Self-excludes
	// verifierJobName so a re-reconcile of an already-dispatched verifier
	// never counts itself.
	inFlight, cErr := verifierInFlightCount(ctx, r.Client, plan.Namespace, project.Name, verifierJobName)
	if cErr != nil {
		return ctrl.Result{}, false, fmt.Errorf("plan verifier in-flight count: %w", cErr)
	}
	if inFlight >= defaultVerifierConcurrencyCap {
		logger.V(1).Info("plan-check verifier dispatch deferred: concurrency cap reached",
			"inFlight", inFlight, "cap", defaultVerifierConcurrencyCap, "plan", plan.Name)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, false, nil
	}

	if r.Deps.ReserveEstimateCents > 0 {
		r.Deps.Reservations.Reserve(string(plan.UID), r.Deps.ReserveEstimateCents)
		reserved = true
	}
	releaseOnError := func() {
		if reserved {
			r.Deps.Reservations.Release(string(plan.UID))
			reserved = false
		}
	}

	verifierCaps := podjob.DefaultCaps(nil, podjob.JobKindVerifier)
	wallClock := verifierCaps.WallClockSeconds
	token, sErr := credproxy.Sign(r.Deps.SigningKey, string(plan.UID),
		time.Duration(wallClock+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if sErr != nil {
		releaseOnError()
		return ctrl.Result{}, false, fmt.Errorf("mint plan verifier signed token: %w", sErr)
	}

	verification := ResolveVerificationSpec(project, plan, nil, "plan")
	_, envInJSON, bErr := r.buildPlanVerifierEnvelopeIn(ctx, plan, project, verification, attempt, token)
	if bErr != nil {
		releaseOnError()
		return ctrl.Result{}, false, bErr
	}

	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if gErr := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); gErr == nil {
			secretUID = string(secret.UID)
		}
	}
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	job := podjob.BuildJobSpec(podjob.BuildOptions{
		Kind:                  podjob.JobKindVerifier,
		ParentObj:             plan,
		Level:                 "plan",
		Project:               project,
		Attempt:               attempt,
		SignedToken:           token,
		EnvelopeInJSON:        envInJSON,
		SubagentImage:         r.Deps.VerifierImage,
		AgentName:             agentName,
		AgentEmail:            agentEmail,
		CredproxyImage:        r.Deps.CredproxyImage,
		SecretUID:             secretUID,
		PVCName:               r.sharedPVCName(),
		ProjectUID:            string(project.UID),
		ReadOnly:              true,
		GateCommand:           verification.GateCommand,
		EstimatedCostCents:    r.Deps.ReserveEstimateCents,
		WorktreeCheckoutImage: r.Deps.TidePushImage,
		WorktreeBranch:        project.Status.Git.BranchName,
	})
	// BuildJobSpec's JobKindVerifier case stamps role=verifier + level +
	// <level>-uid but not the project label — mirrors dispatchVerifier's own
	// post-BuildJobSpec label-stamping idiom (verifierInFlightCount's
	// project-scoped List needs it).
	if job.Labels == nil {
		job.Labels = map[string]string{}
	}
	job.Labels[owner.LabelProject] = project.Name
	if job.Spec.Template.Labels == nil {
		job.Spec.Template.Labels = map[string]string{}
	}
	job.Spec.Template.Labels[owner.LabelProject] = project.Name

	if oErr := owner.EnsureOwnerRef(job, plan, r.Scheme); oErr != nil {
		releaseOnError()
		return ctrl.Result{}, false, fmt.Errorf("ensure owner ref on plan verifier job: %w", oErr)
	}
	if createErr := r.Create(ctx, job); createErr != nil {
		if !apierrors.IsAlreadyExists(createErr) {
			releaseOnError()
			return ctrl.Result{}, false, fmt.Errorf("create plan verifier job: %w", createErr)
		}
		// AlreadyExists: idempotent success — watch-lag race, or a
		// checkPlanVerifyingState retry after a prior cap-hit deferred
		// dispatch that raced a concurrent Create (Pitfall F / SUB-03).
		logger.Info("plan verifier job already exists; treating as successful dispatch", "job", job.Name)
	}

	logger.Info("dispatched plan-check verifier", "plan", plan.Name, "job", job.Name, "gateCommand", verification.GateCommand)
	return ctrl.Result{}, reserved, nil
}

// buildPlanVerifierEnvelopeIn constructs and marshals the EnvelopeIn for a
// plan-check verifier dispatch (Phase 52 D-01/D-03/D-09). Mirrors Task's
// buildVerifierEnvelopeIn (task_controller.go): Role="verifier",
// Provider.Vendor="langgraph" (the verifier is logically independent from
// the planner that authored the plan). VerifyContext.GateCommand carries the
// canonical single primary command from the resolved contract; Commands
// carries the resolved ordered union [GateCommand] ++ commands — the full
// pass-criteria list the verifier executes out-of-band (D-01). Branch is
// populated from project.Status.Git.BranchName (the plan-check verifier
// needs the run-branch tip for its worktree checkout — see the checkout
// init-container fields on dispatchPlanVerifier's BuildOptions; the Task
// verifier does not set Branch, since it inherits the executor's worktree by
// shared UID instead).
//
// The prompt is rendered HERE, controller-side (Go), via
// common.LoadPromptTemplate("verifier", "plan") — same EVAL-04/D-09 import
// firewall Task's verifier prompt honors. The render-data struct
// (planVerifierRenderData) is the 52-03-pinned contract: PlanGoal sources
// from the SAME Project.Spec.OutcomePrompt value reconcilePlannerDispatch
// already renders into the planner's own prompt (outcomePromptOf) — Plan has
// no authored goal/prompt text field of its own in the schema (Claude's
// Discretion; no locked alternative exists). Children is a bounded summary
// of this Plan's own child Tasks (capped at planVerifierChildrenCap).
func (r *PlanReconciler) buildPlanVerifierEnvelopeIn(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, verification tideprojectv1alpha3.VerificationSpec, attempt int, token string) (pkgdispatch.EnvelopeIn, []byte, error) {
	// D-01: the resolved ordered union — GateCommand first (guaranteed
	// executed) then every additional authored pass-criterion, mirroring
	// Task's buildVerifierEnvelopeIn exactly.
	var commands []string
	if verification.GateCommand != "" {
		commands = append(commands, verification.GateCommand)
	}
	commands = append(commands, verification.Commands...)

	envIn := pkgdispatch.EnvelopeIn{
		APIVersion: pkgdispatch.APIVersionV1Alpha1,
		Kind:       pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:    string(plan.UID),
		Role:       "verifier",
		Level:      "plan",
		Branch:     project.Status.Git.BranchName,
		LoopRunID:  string(plan.UID),
		AttemptID:  fmt.Sprintf("%s-%d", plan.UID, attempt),
		Provider: pkgdispatch.ProviderSpec{
			Vendor: "langgraph",
			Model:  ResolveProvider(project, "plan", r.Deps.HelmProviderDefaults).Model,
		},
		ProxyEndpoint: credproxyEndpoint,
		SignedToken:   token,
		Verify: &pkgdispatch.VerifyContext{
			GateCommand:       verification.GateCommand,
			Commands:          commands,
			RequiredArtifacts: verification.RequiredArtifacts,
			EvaluatorRef:      verification.Evaluator,
		},
	}

	tmpl, tErr := common.LoadPromptTemplate("verifier", "plan")
	if tErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("load plan verifier prompt template: %w", tErr)
	}

	var taskList tideprojectv1alpha3.TaskList
	if lErr := r.List(ctx, &taskList, client.InNamespace(plan.Namespace), client.MatchingFields{taskPlanRefIndexKey: plan.Name}); lErr != nil {
		logf.FromContext(ctx).Error(lErr, "list child tasks for plan-check prompt render failed (non-fatal); rendering with an empty Children summary", "plan", plan.Name)
	}
	children := make([]planVerifierChildSummary, 0, planVerifierChildrenCap)
	for i, t := range taskList.Items {
		if i >= planVerifierChildrenCap {
			break
		}
		children = append(children, planVerifierChildSummary{
			Name:        t.Name,
			DependsOn:   t.Spec.DependsOn,
			Files:       t.Spec.FilesTouched,
			GateCommand: t.Spec.Verification.GateCommand,
		})
	}

	renderData := planVerifierRenderData{
		EnvelopeIn: envIn,
		PlanGoal:   outcomePromptOf(project),
		Children:   children,
	}
	var promptBuf bytes.Buffer
	if xErr := tmpl.Execute(&promptBuf, renderData); xErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("render plan verifier prompt template: %w", xErr)
	}
	envIn.Prompt = promptBuf.String()

	data, mErr := json.Marshal(envIn)
	if mErr != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal plan verifier envelope: %w", mErr)
	}
	return envIn, data, nil
}

// synthesizeNoPlanEnvelopeOut mirrors Task's synthesizeNoEnvelopeOut
// (task_controller.go) for the plan-check level: a degraded envelope
// (unreadable/missing out.json) still needs a well-formed EnvelopeOut to
// carry through the fail-closed exhaust path, preserving LoopRunID/AttemptID
// identity through the degraded envelope.
func synthesizeNoPlanEnvelopeOut(plan *tideprojectv1alpha3.Plan, completedJob *batchv1.Job) pkgdispatch.EnvelopeOut {
	out := pkgdispatch.EnvelopeOut{
		APIVersion:  pkgdispatch.APIVersionV1Alpha1,
		Kind:        pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:     string(plan.UID),
		LoopRunID:   string(plan.UID),
		AttemptID:   fmt.Sprintf("%s-%d", plan.UID, int(plan.Status.LoopStatus.Iteration)+1),
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

// emitPlanEvaluatorSpan resolves the SAME parentSpanID the Plan's own AGENT
// span was given (Plan.Spec.PhaseRef's persisted PhaseTraceSpanID, mirroring
// handlePlannerJobCompletion's own TRACE-02 fetch) and emits the OBS-03/D-10
// EVALUATOR sibling span for a terminal plan-check verifier Job. Best-effort
// observability — never gates verdict consumption (mirrors
// emitEvaluatorSpanForVerifier's identical non-fatal posture one level down).
func (r *PlanReconciler) emitPlanEvaluatorSpan(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, verifierJob *batchv1.Job, out pkgdispatch.EnvelopeOut, envReadOK bool) {
	var parentSpanID trace.SpanID
	if plan.Spec.PhaseRef != "" {
		var parentPh tideprojectv1alpha3.Phase
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &parentPh); err == nil {
			parentSpanID = spanIDFromHexOrZero(parentPh.Status.PhaseTraceSpanID)
		}
	}
	evaluatorVersion := ""
	if out.RunEvidence != nil && len(out.RunEvidence.EvaluatorVersions) > 0 {
		evaluatorVersion = out.RunEvidence.EvaluatorVersions[0]
	}
	synthesizeEvaluatorSpan(ctx, "plan", plan.Name, project, r.Deps.HelmProviderDefaults, verifierJob, out, envReadOK, evaluatorVersion, parentSpanID)
}

// settlePlanVerifierSpend rolls the plan-check verifier's real token spend
// into Project.Status.budget and settles the BudgetCents reservation
// dispatchPlanVerifier made at dispatch time. Mirrors Task's
// settleVerifierSpend (task_controller.go) one level up. Called exactly once
// per plan-check verifier completion regardless of verdict outcome.
func (r *PlanReconciler) settlePlanVerifierSpend(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) {
	logger := logf.FromContext(ctx)
	if err := budget.RollUpUsage(ctx, r.Client, project, out.Usage); err != nil {
		logger.Error(err, "failed to roll up plan-check verifier budget usage", "plan", plan.Name)
	}
	if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
		logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "plan", plan.Name)
	}
	r.Deps.Reservations.Settle(string(plan.UID))
}

// applyPlanLoopStatus updates plan.Status.LoopStatus with the current
// plan-check evaluation summary only (LOOP-03 — no accumulating history):
// LastEvaluation is the bounded verdict summary from THIS terminal verifier
// envelope (nil-safe — a degraded/unreadable envelope leaves it nil), and
// ExitReason is set once the loop has taken a terminal or park decision.
// Deliberately does NOT touch LoopStatus.Iteration — 52-09 owns the re-plan
// counter increment (dispatchPlanRepair); this plan never repairs, so
// Iteration stays at its current value across every call here.
func applyPlanLoopStatus(plan *tideprojectv1alpha3.Plan, out pkgdispatch.EnvelopeOut, exitReason tideprojectv1alpha3.ExitReason) {
	if out.LoopRunID != "" {
		plan.Status.LoopStatus.ParentRunID = out.LoopRunID
	}
	plan.Status.LoopStatus.ExitReason = exitReason
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
	plan.Status.LoopStatus.LastEvaluation = &summary
}

// markPlanVerifiedApproved is the sole path that clears a contract-bearing
// Plan's Verifying hold on an APPROVED plan-check verdict (D-03/D-10): Phase
// clears to "" — the SAME value the no-contract path already uses in
// handlePlannerJobCompletion — so the existing wave-materialization path
// takes over and child Task dispatch unblocks via checkParentApproval's
// now-satisfied OR-clause.
func (r *PlanReconciler) markPlanVerifiedApproved(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.Phase = ""
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             "PlanCheckApproved",
		Message:            "Independent plan-check verifier approved the authored plan; Task dispatch unblocked",
		LastTransitionTime: metav1.Now(),
	})
	applyPlanLoopStatus(plan, out, tideprojectv1alpha3.ExitApproved)
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	r.settlePlanVerifierSpend(ctx, plan, project, out)
	return ctrl.Result{}, nil
}

// exhaustPlanVerifyLoop is the shared fail-closed exit for every
// non-APPROVED plan-check outcome this plan handles: an unreadable envelope,
// a nil verdict, a classified BLOCKED verdict, and — conservatively, until a
// later plan lands repairOrHaltPlan's findings-seeded re-plan — a REPAIRABLE
// verdict or an APPROVED verdict a deterministic gate-command failure
// dominates. Delegates to the shared D-08 branch point exhaustVerifyLoop
// (level_status.go) exactly as Task's haltVerify does, one level up:
// onExhaustion differentiates requireApproval (park at AwaitingApproval —
// the EXISTING top-of-reconcilePlannerDispatch AwaitingApproval branch
// already consumes the SAME "approve-plan" annotation via
// consumeApproveAndResume) from escalate (VerifyHalted + project-wide
// ConditionVerifyHalt).
func (r *PlanReconciler) exhaustPlanVerifyLoop(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut, message string) (ctrl.Result, error) {
	var completedAt time.Time
	if !out.CompletedAt.IsZero() {
		completedAt = out.CompletedAt
	}

	// exhaustVerifyLoop performs its own mutate-then-patch cycle and MUST run
	// before this function's own Status mutations below — an earlier
	// in-memory mutation would be silently dropped by its DeepCopy-based
	// patch base (see its doc comment).
	policy := ResolveLoopPolicy(project, plan, nil, "plan")
	result, err := exhaustVerifyLoop(ctx, r.Client, project, plan, &plan.Status.Conditions, &plan.Status.Phase, "plan", policy, completedAt, message)
	if err != nil {
		return ctrl.Result{}, err
	}

	// The caller-agnostic ConditionFailed reason + LoopStatus/ExitReason — a
	// second, focused patch (mirrors haltVerify's own two-step shape).
	// Skipped on the requireApproval leg: exhaustVerifyLoop already parked
	// the Plan with its own WaveOrLevelPaused/ReasonVerifyExhausted
	// condition, and stamping ConditionFailed=True on a merely-parked (not
	// failed) Plan would contradict that state.
	patch := client.MergeFrom(plan.DeepCopy())
	if policy.EscalationPolicy != tideprojectv1alpha3.EscalationRequireApproval {
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "PlanCheckExhausted",
			Message:            message,
			LastTransitionTime: metav1.Now(),
		})
	}
	applyPlanLoopStatus(plan, out, tideprojectv1alpha3.ExitEscalated)
	if pErr := r.Status().Patch(ctx, plan, patch); pErr != nil {
		return ctrl.Result{}, pErr
	}

	r.settlePlanVerifierSpend(ctx, plan, project, out)
	return result, nil
}

// replanFindingsAnnotation is the bounded, single-current-iteration D-04
// findings transport: the plan-check verdict is in hand at repair time, but
// the fresh planner re-dispatch happens on a LATER reconcile (after
// child-Task deletion completes) — this annotation is what carries the
// findings across that gap. Annotations, not .status, per LOOP-03's
// no-history rule (this is current-iteration-only, cleared on consumption —
// the gates-annotation precedent, mirroring the approve/reject annotations'
// own one-shot shape).
const replanFindingsAnnotation = "tideproject.k8s/replan-findings"

// T-52-29 DoS mitigation: an unbounded findings block must never accumulate
// on the Plan object. replanFindingsMaxCount caps the finding count;
// replanFindingsMaxSummaryBytes caps each finding's rendered Summary length
// (RunEvidence.Bounded's hard-byte-cut truncation idiom, pkg/dispatch/run_evidence.go).
const (
	replanFindingsMaxCount        = 10
	replanFindingsMaxSummaryBytes = 300
)

// severityScore computes the D-05 severity-weighted stall-detection score
// from a verdict's finding counts — high-severity findings dominate (weight
// 10) over the raw findings count (weight 1). The scheme's only structural
// requirement is that the score strictly decrease on genuine improvement;
// the absolute scale is otherwise arbitrary (Claude's Discretion per D-05).
func severityScore(findingsCount, highSeverityCount int32) int {
	return int(highSeverityCount)*10 + int(findingsCount)
}

// replanStalled reports whether a re-plan's newScore fails to strictly
// improve on the previous iteration's evaluation (D-05): prev is
// plan.Status.LoopStatus.LastEvaluation, and nil (no previous evaluation —
// the first-ever REPAIRABLE verdict, nothing to compare against yet) is
// never stalled.
func replanStalled(prev *tideprojectv1alpha3.EvaluationSummary, newScore int) bool {
	if prev == nil {
		return false
	}
	prevScore := severityScore(prev.FindingsCount, prev.HighSeverityCount)
	return newScore >= prevScore
}

// truncateReplanString hard-cuts s to at most n bytes — diagnostic-only text
// (a finding's Summary), never re-parsed, so a UTF-8-boundary-unaware cut is
// sufficient and keeps the annotation's byte bound exact (T-52-29). Mirrors
// pkg/dispatch's own truncateRunEvidenceString idiom (unexported there,
// re-implemented here rather than exported across the package boundary for
// one caller).
func truncateReplanString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// boundedRepairFindings condenses a plan-check verdict's Findings into the
// bounded []pkgdispatch.RepairFinding shape plan_planner.tmpl's D-04 block
// renders (the 52-03-pinned contract) — capped at replanFindingsMaxCount
// entries. RepairFinding.Summary sources from Finding.Evidence (falling back
// to SuggestedFix when Evidence is empty) — the verifier's own "concrete
// observation backing this finding" text — since RepairFinding is
// deliberately NOT Finding itself (RepairFinding is the compact
// planner-prompt summary; Finding is the verifier's full wire format,
// pkg/dispatch/envelope.go's own doc comment). Returns nil for an empty
// input (the "present iff informative" contract the annotation helpers rely
// on).
func boundedRepairFindings(findings []pkgdispatch.Finding) []pkgdispatch.RepairFinding {
	if len(findings) == 0 {
		return nil
	}
	n := min(len(findings), replanFindingsMaxCount)
	out := make([]pkgdispatch.RepairFinding, 0, n)
	for _, f := range findings[:n] {
		summary := f.Evidence
		if summary == "" {
			summary = f.SuggestedFix
		}
		out = append(out, pkgdispatch.RepairFinding{
			Severity:   f.Severity,
			Confidence: f.Confidence,
			Summary:    truncateReplanString(summary, replanFindingsMaxSummaryBytes),
		})
	}
	return out
}

// setReplanFindingsAnnotation stamps the bounded D-04 findings block from a
// just-verified REPAIRABLE (or deterministic-failure-dominated APPROVED)
// plan-check verdict onto replanFindingsAnnotation — a plain metadata
// MergeFrom patch (T-04-G2 idiom, mirrors consumeApproveAndResume's
// annotation-patch shape, level_status.go): annotations are not part of the
// .status subresource, so this cannot ride the LoopStatus patch
// dispatchPlanRepair also performs. A verdict carrying zero findings clears
// the annotation rather than stamping an empty array, keeping the
// "present iff informative" contract simple for decodeReplanFindings.
func (r *PlanReconciler) setReplanFindingsAnnotation(ctx context.Context, plan *tideprojectv1alpha3.Plan, out pkgdispatch.EnvelopeOut) error {
	var findings []pkgdispatch.Finding
	if out.Verdict != nil {
		findings = out.Verdict.Findings
	}
	bounded := boundedRepairFindings(findings)

	annoPatch := client.MergeFrom(plan.DeepCopy())
	newAnno := make(map[string]string, len(plan.Annotations)+1)
	maps.Copy(newAnno, plan.Annotations)
	if len(bounded) == 0 {
		delete(newAnno, replanFindingsAnnotation)
	} else {
		data, mErr := json.Marshal(bounded)
		if mErr != nil {
			return fmt.Errorf("marshal replan findings: %w", mErr)
		}
		newAnno[replanFindingsAnnotation] = string(data)
	}
	plan.SetAnnotations(newAnno)
	return r.Patch(ctx, plan, annoPatch)
}

// decodeReplanFindings reads replanFindingsAnnotation (set by
// setReplanFindingsAnnotation) back into the []pkgdispatch.RepairFinding
// shape plan_planner.tmpl's {{range .RepairFindings}} block consumes.
// Nil/absent/malformed all decode to nil — fail-soft: a corrupt annotation
// must not block the fresh planner attempt from dispatching entirely, it
// just dispatches without the D-04 evidence block rather than wedging the
// loop.
func decodeReplanFindings(plan *tideprojectv1alpha3.Plan) []pkgdispatch.RepairFinding {
	raw, ok := plan.Annotations[replanFindingsAnnotation]
	if !ok || raw == "" {
		return nil
	}
	var findings []pkgdispatch.RepairFinding
	if err := json.Unmarshal([]byte(raw), &findings); err != nil {
		return nil
	}
	return findings
}

// clearReplanFindingsAnnotation removes replanFindingsAnnotation once the
// fresh, findings-seeded planner attempt it seeded has completed (D-04's
// consumption point — mirrors the approve-annotation "consume once acted on"
// idiom). A no-op (zero API calls) when the annotation is already absent —
// true for every ordinary (non-re-plan) planner completion, the common case.
func (r *PlanReconciler) clearReplanFindingsAnnotation(ctx context.Context, plan *tideprojectv1alpha3.Plan) error {
	if _, ok := plan.Annotations[replanFindingsAnnotation]; !ok {
		return nil
	}
	annoPatch := client.MergeFrom(plan.DeepCopy())
	newAnno := make(map[string]string, len(plan.Annotations))
	maps.Copy(newAnno, plan.Annotations)
	delete(newAnno, replanFindingsAnnotation)
	plan.SetAnnotations(newAnno)
	return r.Patch(ctx, plan, annoPatch)
}

// repairOrHaltPlan implements D-04/D-05's re-plan decision tree, replacing
// 52-07's marked conservative seam (both the REPAIRABLE leg and the
// APPROVED-with-deterministic-gate-failure leg now route here — see
// handlePlanVerifierCompletion). Order (neither leg burns an iteration):
//
//  1. D-05 stall check — the new verdict's severity-weighted score against
//     plan.Status.LoopStatus.LastEvaluation (the attempt just before this
//     one). A non-improving re-plan halts immediately, before consuming a
//     remaining iteration — proven at MaxIterations:2 (D-05's own required
//     coverage: a maxIterations:1 default never reaches a second stall
//     check).
//  2. The MaxIterations boundary — plan.Status.LoopStatus.Iteration (D-06's
//     quality-re-plan counter) >= policy.MaxIterations. ResolveLoopPolicy
//     defaults Plan's MaxIterations to 1 when unset (dispatch_helpers.go),
//     so Iteration starts at 0 (no re-plan yet) and the first REPAIRABLE
//     verdict always proceeds; the SECOND REPAIRABLE verdict (Iteration
//     now 1) exhausts — exactly one re-plan at defaults (Phase 51
//     repairOrHalt's identical direct-comparison precedent, applied one
//     level up).
//  3. Otherwise dispatchPlanRepair mints the fresh, findings-seeded planner
//     attempt (D-04).
func (r *PlanReconciler) repairOrHaltPlan(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	policy := ResolveLoopPolicy(project, plan, nil, "plan")

	var findingsCount, highSeverity int32
	if out.Verdict != nil {
		findingsCount = int32(len(out.Verdict.Findings))
		for _, f := range out.Verdict.Findings {
			if f.Severity == gateCommandFindingSeverity {
				highSeverity++
			}
		}
	}
	newScore := severityScore(findingsCount, highSeverity)

	if replanStalled(plan.Status.LoopStatus.LastEvaluation, newScore) {
		return r.exhaustPlanVerifyLoop(ctx, plan, project, out,
			"re-plan loop stalled: the new plan-check verdict did not strictly improve on the prior iteration")
	}
	if int(plan.Status.LoopStatus.Iteration) >= int(policy.MaxIterations) {
		return r.exhaustPlanVerifyLoop(ctx, plan, project, out,
			fmt.Sprintf("plan-check iterations exhausted MaxIterations=%d without an APPROVED verdict", policy.MaxIterations))
	}
	return r.dispatchPlanRepair(ctx, plan, project, out)
}

// dispatchPlanRepair implements D-04's re-plan: the delete-then-recreate
// child-Task reconciliation RESEARCH.md's Pitfall 3 requires, plus the
// findings-seeded fresh planner attempt. Order matters:
//
//  1. Stamp the bounded findings block onto replanFindingsAnnotation
//     (setReplanFindingsAnnotation) — must land before the fresh planner
//     Job can possibly dispatch, since reconcilePlannerDispatch's dispatch
//     tail reads it.
//  2. DELETE every child Task owned by this Plan
//     (client.MatchingFields{taskPlanRefIndexKey: plan.Name} — the SAME
//     list reconcilePlannerDispatch's own tasks-exist early-return runs).
//     This is safe: D-03's Verifying hold guarantees none of these Tasks
//     ever dispatched an executor Job. This single delete satisfies BOTH
//     Pitfall 3 (unblocks the tasks-exist early-return so the fresh planner
//     Job can actually be created) and T-52-27 (there is nothing
//     stale left to dispatch).
//  3. A single status patch: applyPlanLoopStatus stamps LastEvaluation from
//     the attempt JUST verified (BEFORE the Iteration bump below — mirrors
//     dispatchRepairAttempt's applyLoopStatus-before-reassignment ordering
//     one level down, task_controller.go), then Iteration increments
//     (D-06's counter doubling as the next planner/verifier attempt
//     number), then Phase clears off Verifying to "" — the SAME value the
//     no-contract path already uses in handlePlannerJobCompletion — so
//     reconcilePlannerDispatch's dispatch tail re-engages on a LATER
//     reconcile once the child-Task List above observes zero items.
func (r *PlanReconciler) dispatchPlanRepair(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, out pkgdispatch.EnvelopeOut) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	if aErr := r.setReplanFindingsAnnotation(ctx, plan, out); aErr != nil {
		return ctrl.Result{}, fmt.Errorf("stamp replan-findings annotation: %w", aErr)
	}

	var taskList tideprojectv1alpha3.TaskList
	if lErr := r.List(ctx, &taskList, client.InNamespace(plan.Namespace), client.MatchingFields{taskPlanRefIndexKey: plan.Name}); lErr != nil {
		return ctrl.Result{}, fmt.Errorf("list rejected-attempt child tasks: %w", lErr)
	}
	for i := range taskList.Items {
		if dErr := r.Delete(ctx, &taskList.Items[i]); dErr != nil && !apierrors.IsNotFound(dErr) {
			return ctrl.Result{}, fmt.Errorf("delete rejected-attempt child task %s: %w", taskList.Items[i].Name, dErr)
		}
	}
	logger.Info("deleted rejected plan-check attempt's child tasks ahead of re-plan", "plan", plan.Name, "count", len(taskList.Items))

	patch := client.MergeFrom(plan.DeepCopy())
	applyPlanLoopStatus(plan, out, "") // loop continues: ExitReason stays empty
	plan.Status.LoopStatus.Iteration++
	plan.Status.Phase = ""
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionReconciling,
		Status:             metav1.ConditionTrue,
		Reason:             "PlanCheckRepairDispatched",
		Message:            fmt.Sprintf("Plan-check found repairable findings; deleted %d rejected-attempt child task(s) and cleared Verifying for a findings-seeded re-plan", len(taskList.Items)),
		LastTransitionTime: metav1.Now(),
	})
	if pErr := r.Status().Patch(ctx, plan, patch); pErr != nil {
		return ctrl.Result{}, pErr
	}

	r.settlePlanVerifierSpend(ctx, plan, project, out)
	return ctrl.Result{Requeue: true}, nil
}

// handlePlanVerifierCompletion consumes a terminal plan-check verifier Job's
// EnvelopeOut.Verdict (Phase 52 D-03/D-10) — mirrors Task's
// handleVerifierCompletion (task_controller.go) one level up. Fail-closed by
// construction: an unreadable envelope or a nil Verdict exhausts via
// exhaustPlanVerifyLoop, never markPlanVerifiedApproved. ClassifyVerdict
// drives the decision: APPROVED (and no deterministic gate-command
// dominance) -> markPlanVerifiedApproved; REPAIRABLE, or an APPROVED verdict
// a red gate-command Finding dominates, -> repairOrHaltPlan (D-04/D-05's
// findings-seeded re-plan with severity-weighted stall detection); BLOCKED
// (and ClassifyVerdict's own fail-closed default) -> the exhaustion path.
func (r *PlanReconciler) handlePlanVerifierCompletion(ctx context.Context, plan *tideprojectv1alpha3.Plan, project *tideprojectv1alpha3.Project, verifierJob *batchv1.Job) (ctrl.Result, error) {
	out, err := readVerifierEnvelope(ctx, r.Deps.EnvReader, string(project.UID), string(plan.UID))
	if err != nil {
		// Fail-closed (mirrors handleVerifierCompletion's identical guard): a
		// Plan whose plan-check envelope cannot be read is never approved.
		synthOut := synthesizeNoPlanEnvelopeOut(plan, verifierJob)
		r.emitPlanEvaluatorSpan(ctx, plan, project, verifierJob, synthOut, false)
		return r.exhaustPlanVerifyLoop(ctx, plan, project, synthOut, err.Error())
	}
	if out.Verdict == nil {
		r.emitPlanEvaluatorSpan(ctx, plan, project, verifierJob, out, true)
		return r.exhaustPlanVerifyLoop(ctx, plan, project, out, "plan-check verifier envelope carried no verdict (fail-closed BLOCKED)")
	}

	// OBS-03/D-10: the EVALUATOR sibling span, emitted before the terminal
	// status patches below (span-loss-averse ordering, mirrors
	// emitEvaluatorSpanForVerifier's own call-site ordering).
	r.emitPlanEvaluatorSpan(ctx, plan, project, verifierJob, out, true)

	// D-04/D-10 parity: re-derive the classification through the canonical
	// fail-closed ClassifyVerdict function rather than trusting
	// out.Verdict.Verdict's raw decoded string directly.
	raw, mErr := json.Marshal(out.Verdict)
	if mErr != nil {
		return r.exhaustPlanVerifyLoop(ctx, plan, project, out, mErr.Error())
	}

	switch pkgdispatch.ClassifyVerdict(raw) {
	case pkgdispatch.VerdictApproved:
		if hasDeterministicFailure(out.Verdict) {
			// D-06 defence-in-depth: a red gate-command Finding dominates
			// even a top-level APPROVED verdict, controller-side — routes
			// through the SAME re-plan decision tree as REPAIRABLE.
			return r.repairOrHaltPlan(ctx, plan, project, out)
		}
		return r.markPlanVerifiedApproved(ctx, plan, project, out)
	case pkgdispatch.VerdictRepairable:
		return r.repairOrHaltPlan(ctx, plan, project, out)
	default: // pkgdispatch.VerdictBlocked, and ClassifyVerdict's own fail-closed default.
		return r.exhaustPlanVerifyLoop(ctx, plan, project, out, out.Verdict.Summary)
	}
}

// patchPlanSucceeded sets Plan.Status.Phase=Succeeded and stamps the
// ConditionSucceeded condition. Called from reconcileWaveMaterialization when
// BoundaryDetected(plan, "Task") returns true (REQ-7b). Mirrors
// milestone_controller.go's patchMilestoneSucceeded pattern.
func (r *PlanReconciler) patchPlanSucceeded(ctx context.Context, plan *tideprojectv1alpha3.Plan) (ctrl.Result, error) {
	return patchLevelStatus(ctx, r.Client, plan, &plan.Status.Conditions, &plan.Status.Phase, tideprojectv1alpha3.LevelPhaseSucceeded, false, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionSucceeded,
			Status:  metav1.ConditionTrue,
			Reason:  "TasksCompleted",
			Message: "All owned Tasks reached Succeeded; Plan complete",
		},
		// Clear any prior WaveOrLevelPaused state so the transition is
		// visible to operators tailing conditions (mirrors patchMilestoneSucceeded).
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionFalse,
			Reason:  tideprojectv1alpha3.ReasonResumedByUser,
			Message: "Plan complete; all Tasks Succeeded",
		},
	)
}

// patchPlanRejected parks the Plan with a RejectedByUser condition WITHOUT
// writing Status.Phase=Failed (D-05). In-flight Jobs drain; state is preserved
// so clearing the reject annotation (tide resume) lets the level re-enter the
// normal dispatch path on the next reconcile.
// Returns RequeueAfter 5s so the park polls for the annotation clear.
func (r *PlanReconciler) patchPlanRejected(ctx context.Context, plan *tideprojectv1alpha3.Plan, reason string) (ctrl.Result, error) {
	return patchLevelStatus(ctx, r.Client, plan, &plan.Status.Conditions, nil, "", false, ctrl.Result{RequeueAfter: 5 * time.Second},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionTrue,
			Reason:  tideprojectv1alpha3.ReasonRejectedByUser,
			Message: fmt.Sprintf("Rejected: %s", reason),
		},
	)
}

// patchPlanFileTouchMismatch parks the Plan for a strict file-touch overlap (D-05,
// D-06). Sets ValidationState=FileTouchMismatch AND a WaveOrLevelPaused condition
// whose Message names both tasks and the shared paths via SummariseMismatches.
// Returns ctrl.Result{} without requeueing — the next Task create/update event
// re-enters reconcile (matching how the reporter flow materializes Tasks async;
// the false-negative window self-heals on the next Task event, per RESEARCH Pitfall 3).
// No Status.Phase mutation (park-not-fail doctrine, D-05) — fieldPtr targets
// ValidationState instead of Phase.
func (r *PlanReconciler) patchPlanFileTouchMismatch(ctx context.Context, plan *tideprojectv1alpha3.Plan, mismatches []webhookv1alpha3.FileTouchMismatchPair) (ctrl.Result, error) {
	summary := webhookv1alpha3.SummariseMismatches(mismatches)
	return patchLevelStatus(ctx, r.Client, plan, &plan.Status.Conditions, &plan.Status.ValidationState, "FileTouchMismatch", false, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionTrue,
			Reason:  "FileTouchMismatch",
			Message: fmt.Sprintf("strict file-touch overlap detected — fix by adding a dependsOn edge or splitting file ownership: %s", summary),
		},
	)
}

// liftPlanFileTouchMismatch clears a prior FileTouchMismatch park (D-06).
// Resets ValidationState to "Validated" and flips the WaveOrLevelPaused
// condition to Status=False so the reconcile proceeds to wave derivation.
func (r *PlanReconciler) liftPlanFileTouchMismatch(ctx context.Context, plan *tideprojectv1alpha3.Plan) (ctrl.Result, error) {
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.ValidationState = "Validated"
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             "FileTouchValidationPassed",
		Message:            "file-touch overlap resolved; proceeding to wave derivation",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	// Re-enter reconcile immediately so wave derivation runs this cycle.
	return ctrl.Result{Requeue: true}, nil
}

// patchPlanFailed sets Plan.Status.Phase=Failed with the given reason/message.
// Used by the Plan 04-05 gate-policy hook (genuine planner-Job failure classification).
//
//nolint:unparam // ctrl.Result kept so callers can `return r.patchPlanFailed(...)` in the reconcile chain
func (r *PlanReconciler) patchPlanFailed(ctx context.Context, plan *tideprojectv1alpha3.Plan, reason, message string) (ctrl.Result, error) {
	return patchLevelStatus(ctx, r.Client, plan, &plan.Status.Conditions, &plan.Status.Phase, tideprojectv1alpha3.LevelPhaseFailed, false, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionFailed,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	)
}

// patchPlanAwaitingApproval parks the Plan at Status.Phase=AwaitingApproval
// per Plan 04-05 gate seam (T-04-G4 mitigation — no requeue).
func (r *PlanReconciler) patchPlanAwaitingApproval(ctx context.Context, plan *tideprojectv1alpha3.Plan, policy tideprojectv1alpha3.GatePolicy) (ctrl.Result, error) {
	reason := tideprojectv1alpha3.ReasonAwaitingApproval
	message := "Plan awaiting operator approve annotation (tideproject.k8s/approve-plan=true)"
	if policy == gates.PolicyPause {
		reason = tideprojectv1alpha3.ReasonPausedAtBoundary
		message = "Plan paused at boundary; requires explicit resume"
	}
	// Optimistic lock: a stale-snapshot re-park must not blind-merge over a
	// concurrent approve's Running+ApprovedByUser write — that clobber consumes
	// the one-shot approve annotation and wedges the level at AwaitingApproval.
	// See patchMilestoneAwaitingApproval for the full race description.
	return patchLevelStatus(ctx, r.Client, plan, &plan.Status.Conditions, &plan.Status.Phase, tideprojectv1alpha3.LevelPhaseAwaitingApproval, true, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	)
}

// countChildTasks returns the number of Tasks owned by this Plan (plan 09-08).
// Used by the ChildCount-gated succession path to compare observed vs expected children.
func (r *PlanReconciler) countChildTasks(ctx context.Context, plan *tideprojectv1alpha3.Plan) int {
	return countChildren(ctx, r.Client, plan.Namespace, plan.UID, &tideprojectv1alpha3.TaskList{})
}

// resolveProjectForPlan walks Plan → Phase → Milestone → Project.
func (r *PlanReconciler) resolveProjectForPlan(ctx context.Context, plan *tideprojectv1alpha3.Plan) *tideprojectv1alpha3.Project {
	// Fast path: if the Plan carries the tideproject.k8s/project label (stamped
	// by stampTaskLabels), use it directly to avoid the Phase→Milestone→Project
	// chain walk. This is the same label fast-path resolveProjectName uses.
	if projectName, ok := plan.Labels[owner.LabelProject]; ok && projectName != "" {
		var p tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: projectName}, &p); err == nil {
			return &p
		}
	}

	if plan.Spec.PhaseRef == "" {
		return nil
	}
	var ph tideprojectv1alpha3.Phase
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &ph); err != nil {
		return nil
	}
	if ph.Spec.MilestoneRef == "" {
		return nil
	}
	var ms tideprojectv1alpha3.Milestone
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		return nil
	}
	if ms.Spec.ProjectRef == "" {
		return nil
	}
	var p tideprojectv1alpha3.Project
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return nil
	}
	return &p
}

// reconcileWaveBoundary runs the per-wave integration gate (D-02 / SC-3 /
// Plan 11-03) for the single wave boundary k → k+1. Returns handled=true when
// the boundary decided the reconcile outcome (terminal failure, requeue, or
// error) and the caller must return (res, err) immediately; handled=false
// means this boundary needs nothing right now — fall through to the next.
func (r *PlanReconciler) reconcileWaveBoundary(
	ctx context.Context,
	plan *tideprojectv1alpha3.Plan,
	project *tideprojectv1alpha3.Project,
	taskByName map[string]*tideprojectv1alpha3.Task,
	layers [][]dag.NodeID,
	k int,
) (ctrl.Result, bool, error) {
	waveNum := k + 1 // 1-indexed wave number

	// If already integrated through this wave, skip to next boundary.
	if plan.Status.IntegratedThroughWave >= waveNum {
		return ctrl.Result{}, false, nil
	}

	// Integration only applies when a real git target + push image exist.
	// Stub/test/no-remote projects have no run branch to integrate into —
	// there is nothing to push, so this boundary must NOT block wave k+1
	// dispatch (otherwise the no-op triggerWaveIntegrationJob would requeue
	// forever and IntegratedThroughWave would never advance). Fall through to
	// the normal label-stamp + Task-dispatch path below.
	if project == nil || project.Spec.Git == nil || project.Spec.Git.RepoURL == "" || r.Deps.TidePushImage == "" {
		return ctrl.Result{}, false, nil
	}

	// PauseBetweenWaves (Plan 04-05) is the OUTER operator gate at this
	// boundary: do not integrate a wave that is still awaiting operator
	// approval. maybePauseForWaveApprove (downstream) sets the
	// WaveOrLevelPaused condition and blocks Task dispatch via the
	// wave-paused label. Once the operator approves, the wave-approved-<N>
	// label is stamped and integration proceeds on a later reconcile —
	// integrate-then-dispatch ordering is preserved past the gate.
	//
	// The gate applies ONLY to inter-wave boundaries (waveNum < len(layers)):
	// maybePauseForWaveApprove pauses BETWEEN waves, so its stampable label
	// range is [1, len(layers)-1] — the final boundary (and a single-wave
	// plan's only boundary) has no approvable pause and gating it on a label
	// no code path can stamp deadlocks into a silent INTEG-01 skip. The
	// final wave's dispatch was itself approved at the prior boundary;
	// plan-level gates govern what happens after integration.
	if project.Spec.Gates.PauseBetweenWaves && waveNum < len(layers) &&
		plan.Labels[fmt.Sprintf("%s%d", planWaveApprovedLabelPrefix, waveNum)] != "true" {
		return ctrl.Result{}, false, nil
	}

	integJobName := fmt.Sprintf("tide-push-wave-%s-%d", plan.UID, waveNum)

	// RESPONSIBILITY A: Check if integration Job exists.
	var integJob batchv1.Job
	getErr := r.Get(ctx, types.NamespacedName{Name: integJobName, Namespace: plan.Namespace}, &integJob)
	if getErr == nil {
		// Job exists — check terminal status via Job CONDITIONS (JobFailed /
		// JobComplete), matching the project-side boundary-push gate.
		// Status.Failed counts failed PODS: with BackoffLimit=2 it is >0
		// after the first pod failure while the Job controller still owes
		// retries — classifying (and deleting) at that point burns the
		// bounded-retry budget on a Job that might still succeed.
		// IMPORTANT: check Failed BEFORE the still-running arm to avoid livelock.
		if isJobFailed(&integJob) {
			return r.handleWaveIntegrationFailure(ctx, plan, project, &integJob, integJobName, waveNum)
		}
		if isJobSucceeded(&integJob) {
			// Integration complete — stamp IntegratedThroughWave and continue loop.
			patch := client.MergeFrom(plan.DeepCopy())
			plan.Status.IntegratedThroughWave = waveNum
			if err := r.Status().Patch(ctx, plan, patch); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("patch IntegratedThroughWave=%d: %w", waveNum, err)
			}
			tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "success").Inc()
			return ctrl.Result{}, false, nil
		}
		// Job is still running (Succeeded==0, Failed==0): block wave k+1 dispatch.
		return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
	}
	if !apierrors.IsNotFound(getErr) {
		return ctrl.Result{}, true, fmt.Errorf("get wave integration job %s: %w", integJobName, getErr)
	}

	// RESPONSIBILITY B: No Job found — dispatch if all wave-k tasks Succeeded.
	for _, name := range layers[k] {
		t := taskByName[name]
		if t == nil || t.Status.Phase != tideprojectv1alpha3.LevelPhaseSucceeded {
			// Wave k not yet complete — nothing to integrate yet.
			return ctrl.Result{}, false, nil
		}
	}

	// Backoff fence between retry attempts for the SAME wave: the failure
	// handler's RequeueAfter alone cannot enforce the capped backoff —
	// deleting the failed Job re-enqueues the Plan immediately via
	// Owns(&batchv1.Job{}), and without this fence all
	// maxWaveIntegrationAttempts burn back-to-back against a condition that
	// needed minutes to clear. A new wave (Wave != waveNum) starts unfenced.
	if plan.Status.WaveIntegration.Wave == waveNum &&
		plan.Status.WaveIntegration.Attempts > 0 &&
		plan.Status.WaveIntegration.LastAttemptTime != nil {
		wait := boundaryPushRequeue(plan.Status.WaveIntegration.Attempts)
		if elapsed := time.Since(plan.Status.WaveIntegration.LastAttemptTime.Time); elapsed < wait {
			return ctrl.Result{RequeueAfter: wait - elapsed}, true, nil
		}
	}

	// D-02 single-flight gate: do not create a new git-writer Job while
	// another (wave-integration or boundary-push) is in flight for this
	// Project. Self-exclusion on integJobName (Pitfall 7) — this reconciler
	// is about to create/observe that exact Job, so it must never count
	// against itself.
	inFlight, gwErr := gitWriterInFlightCount(ctx, r.Client, plan.Namespace, project.Name, integJobName)
	if gwErr != nil {
		return ctrl.Result{}, true, fmt.Errorf("check git-writer in-flight count: %w", gwErr)
	}
	if inFlight > 0 {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
	}

	// D-01 (cumulative everywhere): the wave-integration Job carries the
	// CUMULATIVE Succeeded-branch set (every Succeeded task project-wide),
	// not just wave-k's branches — re-merging an already-integrated branch
	// is idempotent ("Already up to date"), and this is the self-healing
	// defense-in-depth half of D-01 (structural fix = the full-range loop;
	// this is the belt-and-braces half).
	branches, bErr := succeededTaskBranches(ctx, r.Client, plan.Namespace, project.Name)
	if bErr != nil {
		return ctrl.Result{}, true, fmt.Errorf("compute cumulative succeeded-task branches: %w", bErr)
	}

	// Dispatch the integration Job.
	if err := triggerWaveIntegrationJob(ctx, r.Client, r.Scheme, plan, project, waveNum, branches, r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults); err != nil {
		return ctrl.Result{}, true, err
	}
	// Requeue to wait for the Job to complete (RESPONSIBILITY A on next cycle).
	// Do NOT stamp IntegratedThroughWave here — the Job has not yet completed.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
}

// handleWaveIntegrationFailure classifies a terminally-failed wave-
// integration Job (Phase 34 D-04/D-09/D-10) and either:
//   - fails the Plan immediately with ReasonMergeConflict (a genuine content
//     conflict — conflicting parallel tasks were not actually independent,
//     so the plan is defective; recovery is replan + `tide resume
//     --retry-failed`), or
//   - rides a bounded retry (Attempts counter on Plan.Status.WaveIntegration,
//     capped at maxWaveIntegrationAttempts, Background-propagation Job
//     delete + capped-backoff requeue), failing the Plan with
//     ReasonWaveIntegrationFailed only after the cap.
func (r *PlanReconciler) handleWaveIntegrationFailure(
	ctx context.Context,
	plan *tideprojectv1alpha3.Plan,
	project *tideprojectv1alpha3.Project,
	integJob *batchv1.Job,
	integJobName string,
	waveNum int,
) (ctrl.Result, bool, error) {
	env, haveEnv := readJobPushEnvelope(ctx, r.Client, plan.Namespace, integJobName)

	if haveEnv && env.Reason == pushEnvelopeReasonMergeConflict {
		tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "conflict").Inc()
		res, err := r.patchPlanFailed(ctx, plan,
			tideprojectv1alpha3.ReasonMergeConflict,
			fmt.Sprintf("wave %d integration job %s hit a genuine merge conflict integrating %s into %s: content problem, human needed — replan, then `tide resume --retry-failed`",
				waveNum, integJobName, env.ConflictBranch, project.Status.Git.BranchName))
		return res, true, err
	}

	// Bounded retry (#13b pattern mirrored on Plan.Status.WaveIntegration):
	// reset the counter when the blocking wave changed since the last
	// attempt, then increment.
	patch := client.MergeFrom(plan.DeepCopy())
	if plan.Status.WaveIntegration.Wave != waveNum {
		plan.Status.WaveIntegration.Wave = waveNum
		plan.Status.WaveIntegration.Attempts = 0
	}
	plan.Status.WaveIntegration.Attempts++
	now := metav1.Now()
	plan.Status.WaveIntegration.LastAttemptTime = &now
	lastErr := env.Reason
	if !haveEnv {
		lastErr = "envelope-unreadable"
	} else if lastErr == "" {
		lastErr = "integration-failed"
	}
	plan.Status.WaveIntegration.LastError = lastErr
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, true, fmt.Errorf("patch WaveIntegration status: %w", err)
	}

	if plan.Status.WaveIntegration.Attempts >= maxWaveIntegrationAttempts {
		tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "transient").Inc()
		res, err := r.patchPlanFailed(ctx, plan,
			tideprojectv1alpha3.ReasonWaveIntegrationFailed,
			fmt.Sprintf("wave %d integration job %s failed after %d attempts (last error: %q)",
				waveNum, integJobName, plan.Status.WaveIntegration.Attempts, lastErr))
		return res, true, err
	}

	// Background propagation (not Foreground): Foreground leaves the Job
	// lingering behind a foreground finalizer until GC runs — which never
	// happens under envtest — wedging the deterministic name forever (the
	// same verified footgun the #13b boundary-push retry avoids).
	policy := metav1.DeletePropagationBackground
	if err := r.Delete(ctx, integJob, &client.DeleteOptions{PropagationPolicy: &policy}); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, true, fmt.Errorf("delete failed wave integration job %s: %w", integJobName, err)
	}
	tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "transient").Inc()
	return ctrl.Result{RequeueAfter: boundaryPushRequeue(plan.Status.WaveIntegration.Attempts)}, true, nil
}

// reconcileWaveMaterialization implements the Wave materialization body inside the
// Dispatcher seam (step 5 of the six-step pattern).
//
// Per PERSIST-03: pkg/dag.ComputeWaves is called on EVERY reconcile — the schedule
// is re-derived from the current Task set, never cached in .status.
func (r *PlanReconciler) reconcileWaveMaterialization(ctx context.Context, plan *tideprojectv1alpha3.Plan) (ctrl.Result, error) {
	// Step 1: No-op until Plan is Validated by the admission webhook (Plan 11).
	// FileTouchMismatch is the dormant parked state set by this reconciler; treat
	// it as "Validated" so we re-enter the gate on every Task change and can lift
	// the park once the overlap is resolved (D-06).
	if plan.Status.ValidationState != "Validated" && plan.Status.ValidationState != "FileTouchMismatch" {
		return ctrl.Result{}, nil
	}

	// Step 2: List Tasks via field-indexer .spec.planRef = plan.Name.
	var taskList tideprojectv1alpha3.TaskList
	if err := r.List(ctx, &taskList,
		client.InNamespace(plan.Namespace),
		client.MatchingFields{taskPlanRefIndexKey: plan.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list tasks for plan %s: %w", plan.Name, err)
	}

	// Phase 52 D-03 (Rule 2 gap-closure): handlePlannerJobCompletion's own
	// ChildCount-gated Verifying transition only fires for a genuine leaf
	// Plan (ChildCount==0) — reconcilePlannerDispatch's top-of-function
	// tasks-exist early-return (":290" above) means ANY reconcile after the
	// reporter has created even ONE child Task is structurally routed
	// straight here (dispatched=false), never back through
	// handlePlannerJobCompletion, so its own "observed >= expected" check can
	// never observe a satisfied count for a Plan that actually has children
	// (materialization is asynchronous — it cannot complete within the SAME
	// reconcile that first found zero Tasks). This IS the reachable
	// "materialization has happened" seam for that common case: a
	// Running-phase Plan whose child Tasks have started appearing. Checked
	// BEFORE wave derivation so no Task dispatches this reconcile.
	if plan.Status.Phase == tideprojectv1alpha3.LevelPhaseRunning && len(taskList.Items) > 0 {
		project := r.resolveProjectForPlan(ctx, plan)
		verification := ResolveVerificationSpec(project, plan, nil, "plan")
		if verification.GateCommand != "" && verification.Phase == verificationPhaseLocked {
			patch := client.MergeFrom(plan.DeepCopy())
			plan.Status.Phase = tideprojectv1alpha3.LevelPhaseVerifying
			meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha3.ConditionReconciling,
				Status:             metav1.ConditionTrue,
				Reason:             "PlanCheckDispatched",
				Message:            "Child Tasks materialized; dispatching an independent plan-check verifier before any Task dispatches",
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Patch(ctx, plan, patch); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
	}

	// Step 2b: D-05 / D-06 file-touch dispatch gate.
	// After Tasks materialize (reporter flow or direct apply) and before wave
	// derivation: check for strict-mode file-touch overlaps. If found, park the
	// Plan with ValidationState=FileTouchMismatch and return without dispatching
	// any Jobs. If no overlaps (or mode is not strict), lift a prior park.
	// This gate is the authoritative seat — the webhook's Pitfall B means it never
	// sees reporter-flow Tasks; this gate always runs after Tasks exist.
	if len(taskList.Items) > 0 {
		project := r.resolveProjectForPlan(ctx, plan)
		mode := webhookv1alpha3.ResolveFileTouchMode(plan, project, r.DefaultFileTouchMode)
		mismatches := webhookv1alpha3.ComputeFileTouchMismatches(taskList.Items)

		if len(mismatches) > 0 && mode == "strict" {
			// Park: ValidationState=FileTouchMismatch, no wave derivation, no dispatch.
			return r.patchPlanFileTouchMismatch(ctx, plan, mismatches)
		}

		// D-06 un-park path: if we were parked for FileTouchMismatch but now either
		// the mode is non-strict or the overlaps are resolved, lift the park.
		if plan.Status.ValidationState == "FileTouchMismatch" {
			return r.liftPlanFileTouchMismatch(ctx, plan)
		}
	}

	// Build nodes + edges for ComputeWaves.
	nodes := make([]dag.NodeID, 0, len(taskList.Items))
	var edges []dag.Edge
	for _, t := range taskList.Items {
		nodes = append(nodes, t.Name)
		for _, dep := range t.Spec.DependsOn {
			edges = append(edges, dag.Edge{From: dep, To: t.Name})
		}
	}

	// Step 3: ComputeWaves on EVERY reconcile (PERSIST-03 — no cached schedule).
	layers, err := dag.ComputeWaves(nodes, edges)
	if err != nil {
		var cycleErr *dag.CycleError
		if errors.As(err, &cycleErr) {
			// Defense-in-depth: the Plan admission webhook should have caught this.
			patch := client.MergeFrom(plan.DeepCopy())
			plan.Status.Phase = tideprojectv1alpha3.LevelPhaseFailed
			plan.Status.ValidationState = conditionTypeCycleDetected
			plan.Status.CycleEdges = cycleErr.InvolvedNodes
			meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha3.ConditionFailed,
				Status:             metav1.ConditionTrue,
				Reason:             tideprojectv1alpha3.ReasonCycleDetected,
				Message:            fmt.Sprintf("DAG cycle detected: %v", cycleErr.InvolvedNodes),
				LastTransitionTime: metav1.Now(),
			})
			if patchErr := r.Status().Patch(ctx, plan, patch); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("compute waves for plan %s: %w", plan.Name, err)
	}

	// Wave CR creation and wave-index label stamping are now owned exclusively by
	// ProjectReconciler.deriveGlobalWaves (Phase 24 Plan 03, D-03). PlanReconciler
	// no longer creates Wave CRs or stamps tideproject.k8s/wave-index on Tasks.

	// Step 4b: Per-wave integration gate (D-02 / SC-3 / Plan 11-03).
	// For each wave boundary (wave k → wave k+1), we must ensure wave k's
	// task branches are integrated into the run branch before wave k+1 executors
	// are dispatched. Three responsibilities, checked in order each reconcile:
	//
	//   RESPONSIBILITY A — Completion gate / failure detection (check FIRST):
	//   If an integration Job already exists for wave k+1, check its status:
	//   - Failed > 0: permanently failed → mark Plan Failed (no livelock)
	//   - Succeeded > 0: stamp IntegratedThroughWave = k+1 and continue
	//   - Otherwise (running): return requeue to wait for completion
	//
	//   RESPONSIBILITY B — Dispatch:
	//   If no integration Job exists, all wave-k tasks are Succeeded, and
	//   IntegratedThroughWave < k+1: dispatch the integration Job and requeue.
	//
	//   RESPONSIBILITY C — Gate:
	//   Per-wave integration boundary check follows immediately below.

	// Resolve project for wave integration jobs (need Project for push job spec).
	project := r.resolveProjectForPlan(ctx, plan)

	taskByName := make(map[string]*tideprojectv1alpha3.Task, len(taskList.Items))
	for i := range taskList.Items {
		taskByName[taskList.Items[i].Name] = &taskList.Items[i]
	}

	// Iterate EVERY wave boundary, including the final one (Phase 34
	// INTEG-01 — closes the `k < len(layers)-1` skip that left a plan's
	// final Kahn wave, and any single-wave plan, integrating nothing). The
	// final boundary now gates Plan completion rather than wave k+1
	// dispatch: patchPlanSucceeded below runs only after this loop, so
	// Plan=Succeeded now implies every wave — including the last — has been
	// integrated into the run branch.
	for k := range layers {
		res, handled, err := r.reconcileWaveBoundary(ctx, plan, project, taskByName, layers, k)
		if handled || err != nil {
			return res, err
		}
	}

	// REQ-7b: check whether all owned Tasks have Succeeded. When true, stamp
	// Plan.Status.Phase=Succeeded so PhaseReconciler.handleJobCompletion can
	// observe Plan=Succeeded via gates.BoundaryDetected(ph, "Plan") and advance
	// the Phase. The Succeeded short-circuit in reconcilePlannerDispatch (terminal
	// guard) prevents re-entry on subsequent reconciles. The childless guard in
	// BoundaryDetected (returns false when 0 Tasks owned) prevents premature
	// Succeeded before Task dispatch; Owns(&Task{}) re-enqueues this Plan on
	// every Task status update so the check converges correctly.
	detected, derr := gates.BoundaryDetected(ctx, r.Client, plan, "Task")
	if derr != nil {
		return ctrl.Result{}, derr
	}
	if detected {
		return r.patchPlanSucceeded(ctx, plan)
	}

	// Plan 04-05 Task 2: PauseBetweenWaves hook. After labels are stamped, check
	// whether the wave boundary requires operator approval before wave N+1 can
	// dispatch. The actual block on Task dispatch lands via the
	// tideproject.k8s/wave-paused label that TaskReconciler honors.
	return r.maybePauseForWaveApprove(ctx, plan, taskList.Items, layers)
}

// planWaveApprovedLabelPrefix is stamped on the Plan itself by
// maybePauseForWaveApprove when an approve-wave-N annotation is consumed.
// Its presence signals "this wave has been approved" so subsequent
// reconciles do not re-pause at the same boundary while wave N tasks are
// still mid-flight (Plan 04-05 — wave-approval is persistent until all
// tasks in the wave complete).
const planWaveApprovedLabelPrefix = "tideproject.k8s/wave-approved-"

// maybePauseForWaveApprove implements the PauseBetweenWaves boundary check
// per Plan 04-05 (D-G3). When `Project.Spec.Gates.PauseBetweenWaves` is true,
// the function:
//
//  1. Determines the smallest wave index N where wave N-1 is fully Succeeded
//     but at least one task in wave N has not yet Succeeded.
//  2. If the Plan already carries label tideproject.k8s/wave-approved-<N>
//     (set on a prior reconcile after annotation consume), skip pausing —
//     this wave is mid-flight and the operator already approved it.
//  3. If gates.CheckWaveApprove(plan, N) is true: consume the annotation (one-
//     shot, T-04-G2 mitigation), stamp the persistent wave-approved-<N> label
//     on the Plan, clear the wave-paused labels for wave N, and flip the
//     Condition to False (Reason=ResumedByUser).
//  4. Otherwise (no approval, no prior approval label): stamp the
//     tideproject.k8s/wave-paused: "<N>" label on every Task in wave N (the
//     block the TaskReconciler honors) and set Plan's Condition
//     WaveOrLevelPaused True (Reason=PausedAtBoundary, Message referencing N).
//
// When PauseBetweenWaves is false the function is a no-op (today's behavior).
func (r *PlanReconciler) maybePauseForWaveApprove(ctx context.Context, plan *tideprojectv1alpha3.Plan, tasks []tideprojectv1alpha3.Task, layers [][]dag.NodeID) (ctrl.Result, error) {
	project := r.resolveProjectForPlan(ctx, plan)
	if project == nil || !project.Spec.Gates.PauseBetweenWaves {
		return ctrl.Result{}, nil
	}

	// Index tasks by name for status lookup.
	taskByName := make(map[string]*tideprojectv1alpha3.Task, len(tasks))
	for i := range tasks {
		taskByName[tasks[i].Name] = &tasks[i]
	}

	// Find pending boundary: smallest N where wave N-1 is fully Succeeded AND
	// wave N has at least one non-Succeeded task.
	pendingWave := -1
	for n := 1; n < len(layers); n++ {
		prevAllSucceeded := true
		for _, name := range layers[n-1] {
			t := taskByName[name]
			if t == nil || t.Status.Phase != tideprojectv1alpha3.LevelPhaseSucceeded {
				prevAllSucceeded = false
				break
			}
		}
		if !prevAllSucceeded {
			continue
		}
		anyPending := false
		for _, name := range layers[n] {
			t := taskByName[name]
			if t == nil || t.Status.Phase != tideprojectv1alpha3.LevelPhaseSucceeded {
				anyPending = true
				break
			}
		}
		if anyPending {
			pendingWave = n
			break
		}
	}

	if pendingWave < 0 {
		return ctrl.Result{}, nil
	}

	approvedLabelKey := fmt.Sprintf("%s%d", planWaveApprovedLabelPrefix, pendingWave)

	// Prior-approval short-circuit: if we already marked this wave approved,
	// skip — tasks are mid-flight and we must not re-pause.
	if plan.Labels[approvedLabelKey] == "true" {
		return ctrl.Result{}, nil
	}

	if gates.CheckWaveApprove(plan, pendingWave) {
		// Consume the annotation (one-shot, T-04-G2) AND stamp the persistent
		// wave-approved label on the Plan in a single Patch.
		newAnno := gates.ConsumeWaveApprove(plan, pendingWave)
		patch := client.MergeFrom(plan.DeepCopy())
		plan.SetAnnotations(newAnno)
		if plan.Labels == nil {
			plan.Labels = map[string]string{}
		}
		plan.Labels[approvedLabelKey] = "true"
		if err := r.Patch(ctx, plan, patch); err != nil {
			return ctrl.Result{}, err
		}
		// Remove wave-paused labels from tasks in this wave (unblock TaskReconciler).
		for _, name := range layers[pendingWave] {
			t := taskByName[name]
			if t == nil || t.Labels == nil {
				continue
			}
			if _, has := t.Labels[owner.LabelWavePaused]; !has {
				continue
			}
			tPatch := client.MergeFrom(t.DeepCopy())
			delete(t.Labels, owner.LabelWavePaused)
			if err := r.Patch(ctx, t, tPatch); err != nil {
				return ctrl.Result{}, fmt.Errorf("clear wave-paused on task %s: %w", t.Name, err)
			}
		}
		// Flip Plan Condition to False (resumed).
		statusPatch := client.MergeFrom(plan.DeepCopy())
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:             metav1.ConditionFalse,
			Reason:             tideprojectv1alpha3.ReasonResumedByUser,
			Message:            fmt.Sprintf("Wave %d approved; dispatch proceeding", pendingWave),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, plan, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Stamp wave-paused label on every task in this wave (block dispatch).
	waveLabel := fmt.Sprintf("%d", pendingWave)
	for _, name := range layers[pendingWave] {
		t := taskByName[name]
		if t == nil {
			continue
		}
		if t.Labels[owner.LabelWavePaused] == waveLabel {
			continue
		}
		tPatch := client.MergeFrom(t.DeepCopy())
		if t.Labels == nil {
			t.Labels = map[string]string{}
		}
		t.Labels[owner.LabelWavePaused] = waveLabel
		if err := r.Patch(ctx, t, tPatch); err != nil {
			return ctrl.Result{}, fmt.Errorf("stamp wave-paused on task %s: %w", t.Name, err)
		}
	}

	statusPatch := client.MergeFrom(plan.DeepCopy())
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonPausedAtBoundary,
		Message:            fmt.Sprintf("Awaiting approval for wave %d (annotate %s%d=true on this Plan)", pendingWave, gates.AnnotationApproveWavePrefix, pendingWave),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, statusPatch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// resolveProjectName returns the Project name for this Plan via:
//  1. label fast-path (tideproject.k8s/project)
//  2. owner-ref chain walk via resolveProjectForPlan (Plan→Phase→Milestone→Project)
//  3. ErrParentUnresolved on miss (caller sets ConditionParentUnresolved)
//
// Phase 04.1 P1.4 removed the prior `projectList.Items[0]` fallback which
// silently mis-routed Plans in multi-Project namespaces.
func (r *PlanReconciler) resolveProjectName(ctx context.Context, plan *tideprojectv1alpha3.Plan) (string, error) {
	// Fast path: label stamped on this Plan.
	if name, ok := plan.Labels[owner.LabelProject]; ok && name != "" {
		return name, nil
	}
	// Owner-ref chain walk: Plan→Phase→Milestone→Project (via Spec.PhaseRef).
	if project := r.resolveProjectForPlan(ctx, plan); project != nil {
		return project.Name, nil
	}
	return "", ErrParentUnresolved
}

// SetupWithManager wires the watch with a namespace-filter predicate per AUTH-02.
// Note: WaveReconciler handles Wave→Plan re-enqueue; PlanReconciler uses Owns(&Wave{})
// so it is notified when owned Waves are created/updated. Plan 04-05 also wires
// AnnotationChangedPredicate via a self-Watches handler so approve-plan /
// approve-wave-N annotation writes trigger reconciliation (T-04-G4 mitigation).
// The self-Watches pattern avoids filtering finalizer/owner-ref Update events
// at the For() level.
// Owns(&batchv1.Job{}): the plan planner Job is created by reconcilePlannerDispatch;
// when it transitions to terminal state the plan reconciler must re-run to call
// handlePlannerJobCompletion and materialize child Tasks. Without this Owns, the
// plan stays in Running indefinitely — the Job completion event never re-enqueues
// the plan (cascade-8 follow-on: plan controller missing Job watch).
func (r *PlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	annotationOnly := predicate.AnnotationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha3.Plan{}).
		// Wave CRs are now owned by ProjectReconciler (global derivation, Phase 24 Plan 03).
		// PlanReconciler no longer owns Waves — removing Owns(&Wave{}) prevents spurious
		// Plan reconciles triggered by Project-owned Wave creates/updates (Pitfall 1).
		Owns(&tideprojectv1alpha3.Task{}).
		Owns(&batchv1.Job{}).
		Watches(
			&tideprojectv1alpha3.Plan{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(annotationOnly),
		).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("plan").
		Complete(r)
}
