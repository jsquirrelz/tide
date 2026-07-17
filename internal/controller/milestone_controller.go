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
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/trace"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
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
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

const milestoneFinalizer = "tideproject.k8s/milestone-cleanup"

// MilestoneReconciler reconciles a Milestone object at Standard depth (D-C1).
// Milestone is owned by Project; the parent ref is set via
// internal/owner.EnsureOwnerRef.
//
// Phase 3 fills the body (plan 03-08) — dispatches a planner Job and on
// completion materializes Phase child CRDs from EnvelopeOut.ChildCRDs.
type MilestoneReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	// PlannerPool is the planner-pool semaphore (Phase 1 POOL-01, sized from
	// the plannerConcurrency config). MilestoneReconciler acquires plannerPool before creating
	// the planner Job (D-A4). POOL-03 cross-pool wait analyzer prohibits
	// acquiring both pools in the same code path; up-stack reconcilers
	// acquire plannerPool only.
	PlannerPool  *pool.Pool
	ExecutorPool *pool.Pool

	// Deps carries the dispatch-tier dependencies shared with the
	// Phase/Plan/Project reconcilers (plan 41-06 consolidation).
	Deps PlannerReconcilerDeps

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string

	// SharedPVCName is the name of the cluster-wide PVC provisioned by the
	// Helm chart (Plan 12). Defaults to "tide-projects". Configurable via
	// --workspaces-pvc-name flag on the manager (Blocker #2/#3 architecture).
	SharedPVCName string

	// Recorder emits K8s Events for observable parent-ref-resolution failures
	// (defect #17). Nil-safe: every use is guarded by r.Recorder != nil.
	Recorder record.EventRecorder
}

// sharedPVCName returns the configured shared PVC name or the default.
func (r *MilestoneReconciler) sharedPVCName() string {
	if r.SharedPVCName != "" {
		return r.SharedPVCName
	}
	return defaultSharedPVCName
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=get;list;watch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=phases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *MilestoneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var milestone tideprojectv1alpha3.Milestone
	if err := r.Get(ctx, req.NamespacedName, &milestone); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !milestone.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &milestone, milestoneFinalizer,
			func(_ context.Context) error {
				logger.Info("milestone cleanup", "name", milestone.Name)
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&milestone, milestoneFinalizer) {
		controllerutil.AddFinalizer(&milestone, milestoneFinalizer)
		if err := r.Update(ctx, &milestone); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure owner ref to parent Project (CRD-02, Pitfall 23 prevention).
	if milestone.Spec.ProjectRef != "" {
		var parent tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: milestone.Namespace, Name: milestone.Spec.ProjectRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) == nil {
				// defect #17: parent Project named by spec.projectRef does not
				// exist. Previously a SILENT Requeue (no condition, no event).
				// Surface it on Status + a Warning Event, then keep requeuing so
				// it self-heals if the parent later appears.
				r.surfaceParentRefUnresolved(ctx, &milestone, "Project", milestone.Spec.ProjectRef)
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		if err := owner.EnsureOwnerRef(&milestone, &parent, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Update(ctx, &milestone); err != nil {
			return ctrl.Result{}, err
		}
		// D-04 (Phase 41): the parent resolved — clear a stale
		// ParentUnresolved=True. Guarded on IsStatusConditionTrue so steady-state
		// reconciles (the common case, parent already resolved) are write-free
		// (T-41-08b).
		if meta.IsStatusConditionTrue(milestone.Status.Conditions, tideprojectv1alpha3.ConditionParentUnresolved) {
			meta.SetStatusCondition(&milestone.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha3.ConditionParentUnresolved,
				Status:             metav1.ConditionFalse,
				Reason:             tideprojectv1alpha3.ReasonParentResolved,
				Message:            fmt.Sprintf("parent Project %q resolved", parent.Name),
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Update(ctx, &milestone); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 4b. D-03 (CUTS-01): backfill tideproject.k8s/project on the Milestone
	// itself when the label is absent. Heals pre-Phase-15 CRs created by the
	// reporter before D-01 was in place. Guard: only patch when label is
	// missing so the second reconcile is a no-op (T-15-03 / idempotent).
	if milestone.Labels[owner.LabelProject] == "" {
		projectName := r.resolveProjectNameForMilestone(ctx, &milestone)
		if projectName != "" {
			patch := client.MergeFrom(milestone.DeepCopy())
			if milestone.Labels == nil {
				milestone.Labels = map[string]string{}
			}
			milestone.Labels[owner.LabelProject] = projectName
			if err := r.Patch(ctx, &milestone, patch); err != nil {
				return ctrl.Result{}, fmt.Errorf("backfill project label on milestone %s: %w", milestone.Name, err)
			}
		}
	}

	// 5. Phase 3: planner dispatch body (REQ-SUB-01, D-A2).
	if r.Deps.Dispatcher != nil {
		return r.reconcilePlannerDispatch(ctx, &milestone)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&milestone.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonInitialized,
		Message:            "Milestone scaffolded; awaiting dispatch logic (Phase 2)",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &milestone); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// resolveProjectNameForMilestone returns the Project name for a Milestone via
// Milestone.Spec.ProjectRef (1 Get). Returns "" if the chain cannot be
// resolved (orphan) — caller should skip the backfill silently.
func (r *MilestoneReconciler) resolveProjectNameForMilestone(ctx context.Context, ms *tideprojectv1alpha3.Milestone) string {
	if ms.Spec.ProjectRef == "" {
		return ""
	}
	var p tideprojectv1alpha3.Project
	if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return ""
	}
	return p.Name
}

// reconcilePlannerDispatch is the Phase 3 planner dispatch body.
//
// Mirrors task_controller.go:reconcileDispatch (Phase 2) at the milestone
// level: dispatches a planner Job named tide-milestone-<uid>-<attempt>,
// patches Status.Phase=Running with Condition AuthoringPlanner=True, then
// on Job terminal state calls handleJobCompletion to materialize Phase
// child CRDs from EnvelopeOut.ChildCRDs.
//
//nolint:gocyclo // a flat state machine of mutually-exclusive dispatch arms; splitting obscures the contract
func (r *MilestoneReconciler) reconcilePlannerDispatch(ctx context.Context, ms *tideprojectv1alpha3.Milestone) (ctrl.Result, error) {
	// Step 1: Terminal short-circuit.
	if ms.Status.Phase == tideprojectv1alpha3.LevelPhaseSucceeded || ms.Status.Phase == tideprojectv1alpha3.LevelPhaseFailed {
		return ctrl.Result{}, nil
	}

	// Step 1a: AwaitingApproval is paused — the reconciler MUST NOT re-dispatch
	// the planner. Two sub-cases:
	//   (a) no approve annotation → keep paused, return early
	//   (b) approve annotation present → D-04 two-step: consume annotation +
	//       patch Status.Phase=Running + ApprovedByUser condition, then Requeue.
	//       Succeeded fires ONLY via the ChildCount-gated succession inside
	//       handleJobCompletion on the next Running-branch reconcile (D-03 /
	//       GATE-01). The old path called patchMilestoneSucceeded directly here,
	//       bypassing the ChildCount guard — that was the run-1 finding-7 bug.
	// Phase 12 D-04: approve never jumps a level to Succeeded past its children.
	if ms.Status.Phase == tideprojectv1alpha3.LevelPhaseAwaitingApproval {
		if gates.CheckApprove(ms, "milestone") {
			// Consume annotation + return to Running + record ApprovedByUser (D-04).
			// Requeue immediately — the Running branch (below) calls handleJobCompletion
			// which owns the ChildCount-gated succession (D-03 invariant).
			return consumeApproveAndResume(ctx, r.Client, ms, &ms.Status.Conditions, &ms.Status.Phase, "milestone", "Milestone approved; children will dispatch")
		}
		// 37-06 Pitfall 8: keep retrying the artifact trigger while parked so the
		// AwaitingApproval early-return cannot permanently swallow it (e.g. the run
		// branch was not yet provisioned, or a push was busy, at completion time).
		// Re-triggers are harmless: single-flight no-ops while busy, clean-tree skips
		// empty commits once staged.
		if ms.Spec.ProjectRef != "" {
			var p tideprojectv1alpha3.Project
			if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: ms.Spec.ProjectRef}, &p); err == nil {
				if apErr := triggerArtifactPush(ctx, r.Client, r.Scheme, &p, "milestone", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults); apErr != nil {
					logf.FromContext(ctx).Info("artifact push trigger failed at parked milestone (non-fatal)", "milestone", ms.Name, "error", apErr.Error())
				}
			}
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)

	// Step 2: On Running — check Job terminal state.
	if ms.Status.Phase == tideprojectv1alpha3.LevelPhaseRunning {
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			// Planner Job is gone (TTL/GC) but the level is still Running: the planner
			// already completed and its envelope lives on the PVC keyed by UID, not on
			// the Job. Fall through to completion so succession fires instead of parking.
			return r.handleJobCompletion(ctx, ms, nil)
		}
		if isJobTerminal(&job) {
			return r.handleJobCompletion(ctx, ms, &job)
		}
		return ctrl.Result{}, nil
	}

	// Step 2b: Idempotency guard — skip NEW planner dispatch when the Milestone
	// already has >=1 child Phase. Placed AFTER the terminal/AwaitingApproval/Running
	// short-circuits so it gates ONLY fresh authoring — never the level's own
	// completion/boundary-push/gate handling (the early placement broke
	// TestBoundaryPush_AllLevels). Symmetric to the project-level guard (728b60a).
	// cascade-10: match by spec.milestoneRef (set synchronously at child-apply
	// time), NOT ownerRef — a pre-applied child (chaos-resume-phase) gets its
	// ownerRef set asynchronously by the PhaseReconciler, so an IsControlledBy-only
	// guard races and lets the milestone author a spurious stub-phase-1. specRef is
	// race-free; ownerRef is kept as a belt-and-suspenders fallback. bare-Project
	// flow is unaffected: each Milestone starts with 0 child Phases and authors once.
	{
		var existingPhases tideprojectv1alpha3.PhaseList
		if lErr := r.List(ctx, &existingPhases, client.InNamespace(ms.Namespace)); lErr != nil {
			return ctrl.Result{}, fmt.Errorf("idempotency: list phases: %w", lErr)
		}
		for i := range existingPhases.Items {
			if existingPhases.Items[i].Spec.MilestoneRef == ms.Name || metav1.IsControlledBy(&existingPhases.Items[i], ms) {
				// Milestone already has a child Phase — planner already authored; skip dispatch.
				return ctrl.Result{}, nil
			}
		}
	}

	// Step 2c: D-05 dispatch-entry reject hold — resolve Project early to check
	// for a reject annotation before acquiring the pool or creating a Job.
	// A rejected Project must halt NEW dispatch (not only post-completion advancement).
	// In-flight Jobs drain; no Job deletion (resolved discretion call).
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
		// Item 7 (Phase 41 D-07): shared planner-tier project-scoped hold chain
		// (Billing → Failure → Budget → Import) — see checkDispatchHolds in
		// dispatch_helpers.go for the order/requeue rationale.
		if held, res := checkDispatchHolds(ctx, earlyProject, "milestone", ms.Name); held {
			return res, nil
		}
	}

	// Step 3a: D3 in-flight cap gate — BEFORE pool Acquire (D-03: no slot leak).
	// Counts non-terminal planner Jobs via a cached-client List; returns RequeueAfter
	// (never an error) when the count meets or exceeds the configured cap (CONCUR-04).
	if r.PlannerPool != nil {
		inFlight, err := plannerInFlightCount(ctx, r.Client, r.WatchNamespace)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("planner in-flight count: %w", err)
		}
		if inFlight >= r.PlannerPool.Capacity() {
			logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
				"inFlight", inFlight, "cap", r.PlannerPool.Capacity(), "milestone", ms.Name)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	// Step 3b: Acquire plannerPool (POOL-01) before creating the Job (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, err
		}
		defer r.PlannerPool.Release()
	}

	// Step 4: Resolve project for provider config.
	var project *tideprojectv1alpha3.Project
	if ms.Spec.ProjectRef != "" {
		var p tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: ms.Spec.ProjectRef}, &p); err == nil {
			project = &p
		}
	}

	// CR-01 (Plan 13-05): guard nil project before the first deref at :370 and :394.
	// Mirrors the plan_controller.go cascade-7 guard shape: empty ProjectRef is a
	// near-unreachable config error (CRD MinLength=1) — refuse without requeueing;
	// transient Get failure / Project deleted between Step 2 and Step 4 — requeue so
	// the cache can catch up. BuildJobSpec drops the provider Secret on nil Project
	// (jobspec.go:259-273), causing credproxy CrashLoopBackOff on the first dispatch.
	if project == nil {
		if ms.Spec.ProjectRef == "" {
			logf.FromContext(ctx).Info("refusing milestone-planner dispatch: spec.projectRef is empty")
			return ctrl.Result{}, nil
		}
		logf.FromContext(ctx).V(1).Info("deferring milestone-planner dispatch: project not yet resolvable, requeueing",
			"milestone", ms.Name, "projectRef", ms.Spec.ProjectRef)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	// Step 5: Build planner envelope.
	// Phase 04.1 P1.2 fix: planner Jobs now share the full Phase 2 dispatch
	// contract via podjob.BuildJobSpec(Kind=JobKindPlanner). The marshaled
	// envelope (previously discarded with _ = envInJSON) is now passed into
	// BuildOptions and written by the envelope-writer init container; the
	// subagent container reads it at startup.
	attempt := 1 // milestone planner dispatch is single-shot per ROADMAP scope; CR-NN for retry semantics

	// Phase 04.1 P1.2 fix: pass JobKindPlanner so nil caps apply the 600s
	// planner floor (covers pod startup + Anthropic API call latency) instead of
	// the 300s executor floor. Plan 04.1-03 shipped the dual-floor DefaultCaps.
	// Project.Spec does not carry per-project Caps (only Task does) — pass nil
	// to let DefaultCaps apply the 600s planner floor unconditionally.
	plannerCaps := podjob.DefaultCaps(nil, podjob.JobKindPlanner)
	// If operator has not set Iterations, apply the planner default.
	if plannerCaps.Iterations <= 0 {
		plannerCaps.Iterations = defaultPlannerIterations
	}
	plannerPrompt := outcomePromptOf(project)
	envIn, envInJSON, err := BuildPlannerEnvelope("milestone", ms, project, attempt, "", plannerPrompt, pkgdispatch.Caps{
		WallClockSeconds: int(plannerCaps.WallClockSeconds),
		Iterations:       int(plannerCaps.Iterations),
	}, credproxyEndpoint, r.Deps.HelmProviderDefaults, ms.Spec.SharedContext)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build planner envelope: %w", err)
	}

	// Mint a signed token for the credproxy sidecar (mirrors task_controller.go Step 8).
	token, err := credproxy.Sign(r.Deps.SigningKey, string(ms.UID), time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("mint planner signed token: %w", err)
	}

	// Resolve secretUID from the Project's ProviderSecretRef (mirrors task_controller.go Step 11).
	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}

	// Step 6: Build + Create planner Job via shared BuildJobSpec (D-B5 dedup key).
	// Phase 04.1 P1.2 fix: replaced r.buildPlannerJob (skeletal 1-container PodSpec)
	// with podjob.BuildJobSpec(JobKindPlanner) which includes PVC subPath isolation,
	// envelope-writer init container, credproxy native sidecar, signed-token env,
	// bounded ActiveDeadline via DefaultCaps, and 3 SecurityContexts.
	// SIGN-01 / D-03: resolve committer/author identity (mirrors resolveImage's
	// HelmProviderDefaults tier) and stamp it into the planner Job env.
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	resolvedImage := resolveImage(project, "milestone", r.Deps.HelmProviderDefaults)
	// D-02 / T-40-12: log the resolved model at dispatch — previously the
	// resolved model appeared nowhere outside the PVC envelope.
	logf.FromContext(ctx).Info("resolved subagent dispatch", "level", "milestone", "model", envIn.Provider.Model, "image", resolvedImage)
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindPlanner,
		ParentObj:            ms,
		Level:                "milestone",
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
		ProjectUID:           string(project.UID),
		Caps:                 plannerCaps,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
		// PROP-01: Milestone's immediate parent is Project, already resolved
		// above — no new fetch needed.
		TraceParent: traceparentForLevel(project, project.Status.ProjectTraceSpanID),
	}
	job := podjob.BuildJobSpec(opts)
	if err := owner.EnsureOwnerRef(job, ms, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure owner ref on planner job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create planner job: %w", err)
		}
		// AlreadyExists: idempotent success.
	}

	// Step 7: Patch Status.Phase=Running + Condition AuthoringPlanner=True.
	patch := client.MergeFrom(ms.DeepCopy())
	ms.Status.Phase = tideprojectv1alpha3.LevelPhaseRunning
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionAuthoringPlanner,
		Status:             metav1.ConditionTrue,
		Reason:             "PlannerDispatched",
		Message:            fmt.Sprintf("Planner Job %s dispatched", jobName),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, ms, patch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleJobCompletion reads tiny status from the completed planner Job's
// termination message (usage/git/exitCode/reason), spawns the tide-reporter
// reader Job to materialize Phase child CRDs from the PVC-side out.json, and
// patches Milestone.Status.Phase to a terminal state.
//
// Materialization is now the reporter Job's job (Phase 09 plan 09-06, REQ-09-01).
// Children arrive via the Owns(&Phase{}) watch once the reporter creates them.
// T-09-13: idempotent spawn (AlreadyExists = ok) protects against re-entry when
// the reporter Job's own completion re-enqueues this reconciler.
//
//nolint:gocyclo // a flat state machine of mutually-exclusive completion arms; splitting obscures the contract
func (r *MilestoneReconciler) handleJobCompletion(ctx context.Context, ms *tideprojectv1alpha3.Milestone, completedJob *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var project *tideprojectv1alpha3.Project
	if ms.Spec.ProjectRef != "" {
		var p tideprojectv1alpha3.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: ms.Spec.ProjectRef}, &p); err == nil {
			project = &p
		}
	}
	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	// Phase 04.1: reject short-circuit FIRST (operator stop should always halt,
	// regardless of envelope availability or read errors). Was previously checked
	// after envelope read, which made TestRejectHalts race with the EnvelopeRead
	// failure path when the shared envReader has no SetOut for this UID.
	// D-05: park (not fail) — in-flight Jobs drain; state is preserved for resume.
	if project != nil && gates.CheckRejected(project) {
		return r.patchMilestoneRejected(ctx, ms, gates.RejectedReason(project))
	}

	// Read tiny status from the dispatch Job's termination message for budget
	// rollup and failure classification. ChildCRDs are NOT used here — materialization
	// has moved to the reporter Job (REQ-09-01).
	// Phase 12 Pitfall 1: when EnvReader exists but returns a read error, do NOT
	// fall into the nil-EnvReader fallback path unconditionally — that path's final
	// patchMilestoneSucceeded at :542 fires for "!hasChildPhases" (leaf) which could
	// incorrectly succeed a milestone that has a ChildCount>0 envelope still being
	// materialized. Instead, treat a read error the same as envReadOK=false but use
	// a sentinel to distinguish "reader error" from "no reader" so the fallback can
	// still fire BoundaryDetected succession when children ARE all done.
	var out pkgdispatch.EnvelopeOut
	envReadOK := false
	envReaderPresent := r.Deps.EnvReader != nil
	if r.Deps.EnvReader != nil {
		var readErr error
		out, readErr = r.Deps.EnvReader.ReadOut(ctx, projectUID, string(ms.UID))
		if readErr != nil {
			// Non-fatal: log and defer to the hasChildPhases fallback below.
			// The fallback uses BoundaryDetected (all-children-Succeeded) as the succession
			// signal, which is safe regardless of the envelope — it does not depend on ChildCount.
			logger.Error(readErr, "milestone planner envelope tiny-status read failed (non-fatal); deferring to children-based succession", "milestone", ms.Name)
		} else {
			envReadOK = true
		}
	} else {
		logger.V(1).Info("no env reader; skipping tiny-status read (nil-EnvReader unit-test path)", "milestone", ms.Name)
	}

	// Phase 42 D-01/D-02/D-04: synthesize at most one retroactive AGENT span
	// per planner Job attempt, gated by the durable MilestoneSpanEmittedUID
	// marker keyed by Job UID (42-REVIEW WR-02: planner Job names are
	// deterministic, so a deleted-and-recreated attempt reuses the name but
	// never the UID — D-02 requires each attempt to produce its own span) —
	// INDEPENDENT of envReadOK and isFirstCompletion (Pitfall 2: the
	// existing MilestoneRolledUpUID marker below is envReadOK-gated by design
	// and would re-emit a degraded span on every reconcile forever if reused
	// here). Ordering is mark-then-emit (42-REVIEW WR-01): the marker is
	// stamped durably BEFORE the span is exported — the optimistic-lock patch
	// closes the stale-cache re-entry window, making duplicate emission
	// impossible; a crash between stamp and emission loses that attempt's
	// span, preferred over double-counting tokens/cost in Phoenix. Pattern 3:
	// plannerSpanResolvable refuses a nil completedJob (already TTL-GC'd) or
	// a Job with no resolvable timestamps — checked BEFORE stamping so a
	// stamp is never wasted on an unemittable span.
	if completedJob != nil && project != nil && ms.Status.MilestoneSpanEmittedUID != string(completedJob.UID) && plannerSpanResolvable(completedJob) {
		stamped := false
		if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			latest := &tideprojectv1alpha3.Milestone{}
			if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
				return err
			}
			if latest.Status.MilestoneSpanEmittedUID == string(completedJob.UID) {
				return nil // already stamped by a concurrent reconcile — its stamper emits
			}
			markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
			latest.Status.MilestoneSpanEmittedUID = string(completedJob.UID)
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
			logger.Error(mErr, "MilestoneSpanEmittedUID marker patch failed (non-fatal); span deferred to a later reconcile", "milestone", ms.Name)
		} else if stamped {
			// TRACE-02: Milestone's immediate parent is Project, already fully
			// resolved above — guard project != nil to avoid a nil-pointer
			// dereference (a nil project also makes the synthesizer a no-op).
			var parentSpanID trace.SpanID
			if project != nil {
				parentSpanID = spanIDFromHexOrZero(project.Status.ProjectTraceSpanID)
			}
			thisSpanID, emitted := synthesizePlannerSpan(ctx, "milestone", project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK, parentSpanID)
			if emitted {
				// Mirror in-memory unconditionally so same-reconcile downstream
				// logic reads it even if the persistence patch below fails.
				ms.Status.MilestoneTraceSpanID = thisSpanID.String()
				if tErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					latest := &tideprojectv1alpha3.Milestone{}
					if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
						return err
					}
					tracePatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
					latest.Status.MilestoneTraceSpanID = thisSpanID.String()
					return r.Status().Patch(ctx, latest, tracePatch)
				}); tErr != nil {
					// PROP-02/Pitfall 2: non-fatal — this is a SEPARATE, later patch
					// from the marker stamp above (the span ID isn't known until
					// synthesizePlannerSpan returns).
					logger.Error(tErr, "MilestoneTraceSpanID patch failed (non-fatal); child parent-linkage degraded for this level", "milestone", ms.Name)
				}
			}
		}
	}

	// Spawn the tide-reporter reader Job in the project namespace (Option C).
	// The reporter reads out.json from the PVC and materializes Phase children.
	// Children arrive via the Owns(&Phase{}) watch once the reporter creates them.
	// T-09-13: idempotent spawn (AlreadyExists = ok) protects against re-entry when
	// the reporter Job's own completion re-enqueues this reconciler.
	//
	// isFirstCompletion tracks whether this is the initial observation of the planner
	// Job reaching terminal state (reporter Job not yet spawned). Used to guard the
	// once-per-completion budget rollup below (plan 09-08 Defect C).
	skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "milestone", r.Deps.HelmProviderDefaults).Vendor)
	isFirstCompletion, spawnErr := spawnReporterIfNeeded(ctx, r.Client, r.Scheme, ms, project, "Milestone", r.Deps.ReporterImage, r.sharedPVCName(),
		traceparentForLevel(project, ms.Status.MilestoneTraceSpanID), r.Deps.OTLPEndpoint, skipMessageSpans)
	if spawnErr != nil {
		return ctrl.Result{}, spawnErr
	}

	// Plan 09-08 Defect C: roll up planner-level Usage to Project.Status.Budget
	// exactly once per planner Job completion (guarded by isFirstCompletion).
	// Task executors already roll up in task_controller.go; this adds the planner's
	// own token/cost spend so Project.Status.Budget reflects the full planning cost.
	//
	// Phase 31 D-03 / T-31-07: isFirstCompletion flips true again after the reporter
	// Job's 300s TTL-GC window, causing double-count on halt→resume. Gate on the
	// durable MilestoneRolledUpUID marker (lives in CRD .status, survives restart)
	// to guarantee exactly-once rollup regardless of TTL-GC (ADOPT-04).
	milestoneJobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
	if isFirstCompletion && envReadOK && project != nil {
		if ms.Status.MilestoneRolledUpUID != milestoneJobName {
			if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
				logger.Error(rollErr, "milestone planner budget rollup failed (non-fatal)", "milestone", ms.Name)
			} else {
				// Stamp the durable marker only after a successful rollup (mirrors project-level
				// Pitfall-2 ordering: leaving the marker unset on error lets the next reconcile retry).
				// WR-02: re-fetch + RetryOnConflict + MergeFromWithOptimisticLock mirrors RollUpUsage,
				// making the stamp durable against concurrent status writes on this level object.
				// WR-03: on retry-budget exhaustion, return the error to requeue rather than swallow —
				// the marker must be durably set before the reporter Job's TTL-GC window reopens rollup.
				if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					latest := &tideprojectv1alpha3.Milestone{}
					if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
						return err
					}
					if latest.Status.MilestoneRolledUpUID == milestoneJobName {
						return nil // already set by a concurrent reconcile — idempotent
					}
					markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
					latest.Status.MilestoneRolledUpUID = milestoneJobName
					return r.Status().Patch(ctx, latest, markerPatch)
				}); mErr != nil {
					return ctrl.Result{}, fmt.Errorf("patch MilestoneRolledUpUID: %w", mErr)
				}
			}
			// Phase 38 COST-02: surface an unknown-model pricing fallback carried
			// on the envelope — condition + metric, bounded by the same
			// exactly-once rollup guards. Non-fatal: informational only.
			if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
				logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "milestone", ms.Name)
			}
		}
	}

	// Phase 13 D-04 layer 2: backstop — if the planner Job failed with a billing
	// reason, stamp BillingHalt=True on the owning Project. Non-fatal: the
	// reconcile continues through the normal completion path regardless.
	if envReadOK && out.ExitCode != 0 && project != nil {
		var jobStart time.Time
		if completedJob != nil {
			jobStart = completedJob.CreationTimestamp.Time
		}
		if hErr := setBillingHaltIfNeeded(ctx, r.Client, project, out.Reason, jobStart); hErr != nil {
			logger.Error(hErr, "setBillingHaltIfNeeded failed (non-fatal)", "milestone", ms.Name)
		}
	}

	// Phase 33 PLANFAIL-02/03 (CR-01 fix): a planner that exits nonzero with zero
	// children is terminally Failed and MUST be marked BEFORE the gate-policy hook.
	// The milestone default gate is approve (gates.policy D-G1), so without this the
	// failure parks at AwaitingApproval instead of Failed, hiding planning-DAG
	// corruption — you cannot gate-approve a planner that authored nothing. Ordered
	// before the succeed path too (PLANFAIL-03: a genuine leaf, exitCode==0/
	// childCount==0, still Succeeds — isPlannerFailure requires exitCode != 0). Plan
	// and project are NOT guarded — they succeed only via gates.BoundaryDetected
	// (matched > 0, false on zero children); see 33-CONTEXT.md D-01/D-02.
	// isPlannerFailure re-checks envReadOK internally, so calling it here is safe.
	if isPlannerFailure(out, envReadOK) {
		return r.patchMilestoneFailed(ctx, ms, tideprojectv1alpha3.ReasonPlannerFailed,
			fmt.Sprintf("planner exited nonzero (exitCode=%d) with zero children; marked Failed to prevent false succession", out.ExitCode))
	}

	// Plan 04-05: gate-policy hook (approve/pause). Reject check moved to
	// top of handleJobCompletion per Phase 04.1 — reject should not be
	// gated on envelope-read success.
	// Phase 12 D-04: if the milestone already has an ApprovedByUser (or
	// ResumedByUser) condition — i.e., the operator approved before this
	// reconcile entered handleJobCompletion — skip the park entirely so we
	// do not re-park an already-approved level.
	if project != nil {
		policy := gates.EvaluatePolicy(project.Spec.Gates, "milestone")
		if policy == gates.PolicyApprove || policy == gates.PolicyPause {
			// Check if this level was already approved (permanent ApprovedByUser or
			// ResumedByUser condition with Status=False means the park was lifted).
			alreadyApproved := false
			if c := meta.FindStatusCondition(ms.Status.Conditions, tideprojectv1alpha3.ConditionWaveOrLevelPaused); c != nil {
				if c.Status == metav1.ConditionFalse &&
					(c.Reason == tideprojectv1alpha3.ReasonApprovedByUser || c.Reason == tideprojectv1alpha3.ReasonResumedByUser) {
					alreadyApproved = true
				}
			}
			if !alreadyApproved {
				if !gates.CheckApprove(ms, "milestone") {
					// No annotation and not yet approved — park.
					// 37-06 / DASH-02 (D-01): stage the cumulative planner-artifact map
					// BEFORE the gate-park return, so artifacts land in git before the
					// operator approves. Placed in the park arm ONLY (not the succeed arm)
					// so it never preempts the boundary push's D-B2 commit + task-branch
					// integration, which share the deterministic Job name (R-05 single-
					// flight). The Step-1a parked-arm retry re-attempts until it lands.
					// Log-and-continue — artifact-push failure must not fail the park.
					if apErr := triggerArtifactPush(ctx, r.Client, r.Scheme, project, "milestone", r.Deps.TidePushImage, r.sharedPVCName(), r.Deps.HelmProviderDefaults); apErr != nil {
						logger.Info("artifact push trigger failed at milestone park (non-fatal)", "milestone", ms.Name, "error", apErr.Error())
					}
					return r.patchMilestoneAwaitingApproval(ctx, ms, policy)
				}
				// Annotation present at the hook (operator approved before the park fired):
				// consume it and write Running+ApprovedByUser so the condition is recorded
				// for future reconciles — otherwise the next reconcile would re-park because
				// the annotation is gone but no approval record exists.
				if _, err := consumeApproveAndResume(ctx, r.Client, ms, &ms.Status.Conditions, &ms.Status.Phase, "milestone", "Milestone approved; children will dispatch"); err != nil {
					return ctrl.Result{}, err
				}
				// Fall through to ChildCount-gated succession (D-03).
			}
			// alreadyApproved: fall through to ChildCount-gated succession.
		}
	}

	// Plan 04-06 W-2: boundary push trigger lands AFTER gate-policy passes
	// (so paused/rejected levels do not push) and BEFORE patchSucceeded
	// (so the operator-visible Status.Phase=Succeeded happens after dispatch).
	//
	// Plan 09-08 Defect B fix: uniform ChildCount-gated succession replaces the
	// inconsistent justMaterialized / hasChildPhases guards. The tiny status
	// carries out.ChildCount = the planner's authored Phase count. Gate:
	//   expected == 0            → succeed (genuine leaf: planner authored no Phases)
	//   observed < expected      → requeue 5s (reporter still materializing)
	//   observed >= expected     → BoundaryDetected? push+succeed : requeue 5s
	// When EnvReader is nil, fall back to the prior hasChild-based behavior so
	// non-Option-C / unit-test paths keep working.
	if envReadOK {
		// PLANFAIL false-leaf guard ran before the gate-policy hook above.
		// Option-C path: gate on out.ChildCount from tiny status.
		expected := out.ChildCount
		if expected == 0 {
			// Genuine leaf — planner authored no Phase children.
			logger.V(1).Info("boundary push skipped: planner authored no Phase children (leaf)", "milestone", ms.Name)
			return r.patchMilestoneSucceeded(ctx, ms)
		}
		observed := r.countChildPhases(ctx, ms)
		if observed < expected {
			logger.V(1).Info("requeue: reporter still materializing Phase children",
				"milestone", ms.Name, "expected", expected, "observed", observed)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		// observed >= expected: check if all Succeeded.
		detected, derr := gates.BoundaryDetected(ctx, r.Client, ms, "Phase")
		if derr != nil {
			return ctrl.Result{}, derr
		}
		if detected {
			if err := r.maybeTriggerBoundaryPush(ctx, ms, project); err != nil {
				if errors.Is(err, errGitWriterBusy) {
					// D-02: another git-writer Job is in flight — normal
					// serialization, not a failure. Requeue and retry.
					return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
				}
				return ctrl.Result{}, err
			}
			return r.patchMilestoneSucceeded(ctx, ms)
		}
		logger.V(1).Info("boundary push deferred: Phase children exist but not all Succeeded",
			"milestone", ms.Name, "expected", expected, "observed", observed)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Fallback: EnvReader is nil (non-Option-C / unit-test path) OR had a read
	// error (envelope transiently absent). Use the prior hasChild-based behavior
	// with one extra guard: when the reader is PRESENT but returned an error, do
	// not fire the "no children → succeed" leaf path — the envelope may have had
	// ChildCount>0 but the read failed transiently. Only BoundaryDetected (all
	// children Succeeded) is safe to act on when the ChildCount is unknown.
	// Phase 12 Pitfall 1 fix: envReaderPresent && !envReadOK → guard the leaf path.
	detected, derr := gates.BoundaryDetected(ctx, r.Client, ms, "Phase")
	if derr != nil {
		return ctrl.Result{}, derr
	}
	if detected {
		if err := r.maybeTriggerBoundaryPush(ctx, ms, project); err != nil {
			if errors.Is(err, errGitWriterBusy) {
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			return ctrl.Result{}, err
		}
	} else if r.hasChildPhases(ctx, ms) {
		logger.V(1).Info("boundary push deferred: child Phases pending (fallback)", "milestone", ms.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	} else if envReaderPresent {
		// Reader exists but had a read error AND no children observed yet — the envelope
		// may have ChildCount>0 (children materializing). Requeue; don't auto-succeed.
		logger.V(1).Info("boundary push deferred: env reader present but unreadable, waiting (fallback)", "milestone", ms.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	} else {
		logger.V(1).Info("boundary push skipped: milestone has no child Phases (nil-EnvReader fallback)", "milestone", ms.Name)
	}

	return r.patchMilestoneSucceeded(ctx, ms)
}

// hasChildPhases reports whether any Phase is owned by this Milestone. Debug #9
// (mirrors PhaseReconciler.hasChildPlans / Phase 04.1). Used by the nil-EnvReader
// fallback path in handleJobCompletion.
func (r *MilestoneReconciler) hasChildPhases(ctx context.Context, ms *tideprojectv1alpha3.Milestone) bool {
	return r.countChildPhases(ctx, ms) > 0
}

// countChildPhases returns the number of Phases owned by this Milestone (plan 09-08).
// Used by the ChildCount-gated succession path to compare observed vs expected children.
func (r *MilestoneReconciler) countChildPhases(ctx context.Context, ms *tideprojectv1alpha3.Milestone) int {
	return countChildren(ctx, r.Client, ms.Namespace, ms.UID, &tideprojectv1alpha3.PhaseList{})
}

// patchMilestoneFailed sets Milestone.Status.Phase=Failed with the given reason/message.
// Used by the Phase 33 D4 false-leaf guard (PLANFAIL-02).
//
//nolint:unparam // ctrl.Result kept so callers can `return r.patchMilestoneFailed(...)` in the reconcile chain
func (r *MilestoneReconciler) patchMilestoneFailed(ctx context.Context, ms *tideprojectv1alpha3.Milestone, reason, message string) (ctrl.Result, error) {
	return patchLevelStatus(ctx, r.Client, ms, &ms.Status.Conditions, &ms.Status.Phase, tideprojectv1alpha3.LevelPhaseFailed, false, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionFailed,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	)
}

func (r *MilestoneReconciler) patchMilestoneSucceeded(ctx context.Context, ms *tideprojectv1alpha3.Milestone) (ctrl.Result, error) {
	return patchLevelStatus(ctx, r.Client, ms, &ms.Status.Conditions, &ms.Status.Phase, tideprojectv1alpha3.LevelPhaseSucceeded, false, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionSucceeded,
			Status:  metav1.ConditionTrue,
			Reason:  "PlannerComplete",
			Message: "Milestone planner completed; Phase children materialized",
		},
		// Plan 04-05: when the level resumes from a prior approve-pause, clear the
		// WaveOrLevelPaused Condition to False with Reason=ResumedByUser so the
		// transition is visible to operators tailing conditions.
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionFalse,
			Reason:  tideprojectv1alpha3.ReasonResumedByUser,
			Message: "Milestone resumed from gate boundary",
		},
	)
}

// patchMilestoneRejected parks the Milestone with a RejectedByUser condition
// WITHOUT writing Status.Phase=Failed (D-05). In-flight Jobs drain; state is
// preserved so clearing the reject annotation (tide resume) lets the level
// re-enter the normal dispatch path on the next reconcile.
// Returns RequeueAfter 5s so the park polls for the annotation clear.
func (r *MilestoneReconciler) patchMilestoneRejected(ctx context.Context, ms *tideprojectv1alpha3.Milestone, reason string) (ctrl.Result, error) {
	return patchLevelStatus(ctx, r.Client, ms, &ms.Status.Conditions, nil, "", false, ctrl.Result{RequeueAfter: 5 * time.Second},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionTrue,
			Reason:  tideprojectv1alpha3.ReasonRejectedByUser,
			Message: fmt.Sprintf("Rejected: %s", reason),
		},
	)
}

// patchMilestoneAwaitingApproval parks the Milestone at Status.Phase=AwaitingApproval
// with ConditionWaveOrLevelPaused True (Plan 04-05 D-G2/D-G3 surface). Returns
// ctrl.Result{} with no requeue — only an AnnotationChangedPredicate-triggered
// re-reconcile (via the approve annotation write) advances the level (T-04-G4
// mitigation: no polling DoS).
func (r *MilestoneReconciler) patchMilestoneAwaitingApproval(ctx context.Context, ms *tideprojectv1alpha3.Milestone, policy tideprojectv1alpha3.GatePolicy) (ctrl.Result, error) {
	reason := tideprojectv1alpha3.ReasonAwaitingApproval
	message := "Milestone awaiting operator approve annotation (tideproject.k8s/approve-milestone=true)"
	if policy == gates.PolicyPause {
		reason = tideprojectv1alpha3.ReasonPausedAtBoundary
		message = "Milestone paused at boundary; requires explicit resume"
	}
	// Optimistic lock (WR-02 idiom): the park overwrites ConditionWaveOrLevelPaused —
	// the very condition whose False/ApprovedByUser value is the gate hook's only
	// re-park guard. A reconciler holding a stale pre-approval snapshot that walks
	// handleJobCompletion → gate hook (!alreadyApproved, !CheckApprove on the stale
	// copy) would blind-merge this park OVER a concurrent approve's
	// Running+ApprovedByUser write, permanently consuming the one-shot approve
	// annotation (already deleted) and wedging the level at AwaitingApproval
	// (Layer A gate-flow CI flake). With the lock, the stale writer 409s; the
	// requeued reconcile re-reads fresh state and sees alreadyApproved.
	return patchLevelStatus(ctx, r.Client, ms, &ms.Status.Conditions, &ms.Status.Phase, tideprojectv1alpha3.LevelPhaseAwaitingApproval, true, ctrl.Result{},
		metav1.Condition{
			Type:    tideprojectv1alpha3.ConditionWaveOrLevelPaused,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	)
}

// surfaceParentRefUnresolved makes a parent-ref-NotFound stall observable
// (defect #17). It sets ConditionParentUnresolved (status True — D-04, Phase
// 41: True == parent unresolved — reason ParentRefNotFound, message naming
// the missing parent) and emits a Warning Event, then returns — the caller
// keeps requeuing so the Milestone self-heals if the parent appears later.
// Best-effort: a Status().Update failure is logged but not propagated, so the
// requeue still fires.
func (r *MilestoneReconciler) surfaceParentRefUnresolved(ctx context.Context, ms *tideprojectv1alpha3.Milestone, parentKind, parentRef string) {
	logger := logf.FromContext(ctx)
	msg := fmt.Sprintf("parent %s %q (spec.projectRef) not found in namespace %q; requeuing until it appears", parentKind, parentRef, ms.Namespace)
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionParentUnresolved,
		Status:             metav1.ConditionTrue, // True == parent unresolved (D-04, Phase 41)
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

// SetupWithManager wires Owns(&Job{}) and Owns(&Phase{}) per D-A2, plus a
// namespace-filter predicate per AUTH-02. Plan 04-05: also wires
// AnnotationChangedPredicate via a self-Watches handler so operator
// approve/reject annotation writes trigger reconciliation without polling
// (T-04-G4 mitigation). Wired as a Watches with AnnotationChangedPredicate
// instead of a For()-level predicate so the post-finalizer Update event
// (no Generation bump, no annotation change) is not filtered, which would
// stall dispatch under the manager's auto-reconcile.
func (r *MilestoneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		//nolint:staticcheck // SA1019: GetEventRecorderFor returns record.EventRecorder (the Recorder field type).
		r.Recorder = mgr.GetEventRecorderFor("milestone-controller")
	}
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	annotationOnly := predicate.AnnotationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha3.Milestone{}).
		Owns(&batchv1.Job{}).
		Owns(&tideprojectv1alpha3.Phase{}).
		Watches(
			&tideprojectv1alpha3.Milestone{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(annotationOnly),
		).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("milestone").
		Complete(r)
}
