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
	"fmt"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch"
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

	// PlannerPool is the planner-pool semaphore (Phase 1 POOL-01, default
	// size 16). MilestoneReconciler acquires plannerPool before creating
	// the planner Job (D-A4). POOL-03 cross-pool wait analyzer prohibits
	// acquiring both pools in the same code path; up-stack reconcilers
	// acquire plannerPool only.
	PlannerPool  *pool.Pool
	ExecutorPool *pool.Pool

	Dispatcher dispatch.Dispatcher

	// EnvReader reads the EnvelopeOut from the per-Project PVC after the
	// planner Job completes (Phase 2 D-A2 path).
	EnvReader podjob.EnvelopeReader

	// SubagentImage is the image ref for the planner subagent container.
	SubagentImage string

	// CredproxyImage is the image ref for the tide-credproxy sidecar.
	// Phase 04.1 P1.2 fix: planner Jobs share the credproxy sidecar contract.
	CredproxyImage string

	// SigningKey is the HMAC signing key used to mint per-dispatch tokens
	// for the credproxy sidecar (Phase 04.1 P1.2 fix).
	// Phase 04.1 P1.2 fix: each planner reconciler gains a SigningKey field,
	// wired in cmd/manager/main.go, so planner Jobs can authenticate with
	// the credproxy sidecar (mirrors TaskReconciler.SigningKey).
	SigningKey []byte

	// TidePushImage is the image ref for the tide-push container used by
	// the W-2 boundary push trigger (plan 04-06).
	TidePushImage string

	// HelmProviderDefaults carry Helm-chart provider/model defaults.
	HelmProviderDefaults ProviderDefaults

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string
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
	var milestone tideprojectv1alpha1.Milestone
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
		var parent tideprojectv1alpha1.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: milestone.Namespace, Name: milestone.Spec.ProjectRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) == nil {
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
	}

	// 5. Phase 3: planner dispatch body (REQ-SUB-01, D-A2).
	if r.Dispatcher != nil {
		return r.reconcilePlannerDispatch(ctx, &milestone)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&milestone.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonInitialized,
		Message:            "Milestone scaffolded; awaiting dispatch logic (Phase 2)",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &milestone); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcilePlannerDispatch is the Phase 3 planner dispatch body.
//
// Mirrors task_controller.go:reconcileDispatch (Phase 2) at the milestone
// level: dispatches a planner Job named tide-milestone-<uid>-<attempt>,
// patches Status.Phase=Running with Condition AuthoringPlanner=True, then
// on Job terminal state calls handleJobCompletion to materialize Phase
// child CRDs from EnvelopeOut.ChildCRDs.
func (r *MilestoneReconciler) reconcilePlannerDispatch(ctx context.Context, ms *tideprojectv1alpha1.Milestone) (ctrl.Result, error) {
	// Step 1: Terminal short-circuit.
	if ms.Status.Phase == "Succeeded" || ms.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}

	// Step 1a: AwaitingApproval is paused — the reconciler MUST NOT re-dispatch
	// the planner. Two sub-cases:
	//   (a) no approve annotation → keep paused, return early
	//   (b) approve annotation present → re-enter handleJobCompletion (which
	//       handles the annotation-consume + patchSucceeded branch).
	// Phase 04.1: closes a long-running flake where the reconciler fell through
	// to dispatchPlanner on AwaitingApproval and re-patched Phase=Running on
	// every reconcile (manifested as TestGateApproveFlow intermittent failures).
	if ms.Status.Phase == "AwaitingApproval" {
		if gates.CheckApprove(ms, "milestone") {
			jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
			var job batchv1.Job
			if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: jobName}, &job); err == nil {
				return r.handleJobCompletion(ctx, ms, &job)
			}
			// Job missing — annotation-only finalization (no envelope read needed).
			return r.patchMilestoneSucceeded(ctx, ms)
		}
		return ctrl.Result{}, nil
	}

	jobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)

	// Step 2: On Running — check Job terminal state.
	if ms.Status.Phase == "Running" {
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
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
		var existingPhases tideprojectv1alpha1.PhaseList
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

	// Step 3: Acquire plannerPool (POOL-01) before creating the Job (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, err
		}
		defer r.PlannerPool.Release()
	}

	// Step 4: Resolve project for provider config.
	var project *tideprojectv1alpha1.Project
	if ms.Spec.ProjectRef != "" {
		var p tideprojectv1alpha1.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: ms.Spec.ProjectRef}, &p); err == nil {
			project = &p
		}
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
	// If operator has not set Iterations, apply the 20-iteration planner default
	// inline (the Caps type doesn't carry per-Kind iteration defaults; only the
	// wall-clock floor differs by Kind via DefaultCaps).
	if plannerCaps.Iterations <= 0 {
		plannerCaps.Iterations = 20
	}
	_, envInJSON, err := BuildPlannerEnvelope("milestone", ms, project, attempt, "", pkgdispatch.Caps{
		WallClockSeconds: int(plannerCaps.WallClockSeconds),
		Iterations:       int(plannerCaps.Iterations),
	}, "https://127.0.0.1:8443", r.HelmProviderDefaults)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build planner envelope: %w", err)
	}

	// Mint a signed token for the credproxy sidecar (mirrors task_controller.go Step 8).
	token, err := credproxy.Sign(r.SigningKey, string(ms.UID), time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
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
	opts := podjob.BuildOptions{
		Kind:           podjob.JobKindPlanner,
		ParentObj:      ms,
		Level:          "milestone",
		Attempt:        attempt,
		Project:        project,
		SignedToken:    token,
		EnvelopeInJSON: envInJSON,
		SubagentImage:  r.SubagentImage,
		CredproxyImage: r.CredproxyImage,
		SecretUID:      secretUID,
		PVCName:        "tide-projects",
		ProjectUID:     string(project.UID),
		Caps:           plannerCaps,
	}
	if opts.SubagentImage == "" {
		opts.SubagentImage = r.HelmProviderDefaults.Image
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
	ms.Status.Phase = "Running"
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionAuthoringPlanner,
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

// handleJobCompletion reads EnvelopeOut, materializes Phase child CRDs,
// and patches Milestone.Status.Phase to a terminal state.
func (r *MilestoneReconciler) handleJobCompletion(ctx context.Context, ms *tideprojectv1alpha1.Milestone, _ *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var project *tideprojectv1alpha1.Project
	if ms.Spec.ProjectRef != "" {
		var p tideprojectv1alpha1.Project
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
	if project != nil && gates.CheckRejected(project) {
		return r.patchMilestoneFailed(ctx, ms, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
	}

	// Phase 04.1: tolerate nil EnvReader — continue through gate logic with an
	// empty envOut. Previously a nil EnvReader short-circuited to patchSucceeded
	// which skipped gate decisions in test setups.
	var envOut pkgdispatch.EnvelopeOut
	if r.EnvReader != nil {
		var err error
		envOut, err = r.EnvReader.ReadOut(ctx, projectUID, string(ms.UID))
		if err != nil {
			return r.patchMilestoneFailed(ctx, ms, "EnvelopeReadFailed", err.Error())
		}
	} else {
		logger.V(1).Info("no env reader; skipping envelope read", "milestone", ms.Name)
	}

	// MaterializeChildCRDs enforces Kind allowlist (T-308 mitigation).
	if len(envOut.ChildCRDs) > 0 {
		if mErr := MaterializeChildCRDs(ctx, r.Client, r.Scheme, ms, envOut.ChildCRDs); mErr != nil {
			return r.patchMilestoneFailed(ctx, ms, "ChildCRDMaterializationFailed", mErr.Error())
		}
	}

	// Plan 04-05: gate-policy hook (approve/pause). Reject check moved to
	// top of handleJobCompletion per Phase 04.1 — reject should not be
	// gated on envelope-read success.
	if project != nil {
		policy := gates.EvaluatePolicy(project.Spec.Gates, "milestone")
		if policy == gates.PolicyApprove || policy == gates.PolicyPause {
			if !gates.CheckApprove(ms, "milestone") {
				return r.patchMilestoneAwaitingApproval(ctx, ms, policy)
			}
			// Approve annotation present: consume it (one-shot) before patching Succeeded.
			newAnno := gates.ConsumeApprove(ms, "milestone")
			patch := client.MergeFrom(ms.DeepCopy())
			ms.SetAnnotations(newAnno)
			if err := r.Patch(ctx, ms, patch); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Plan 04-06 W-2: boundary push trigger lands AFTER gate-policy passes
	// (so paused/rejected levels do not push) and BEFORE patchSucceeded
	// (so the operator-visible Status.Phase=Succeeded happens after dispatch).
	//
	// CR-03 fix: gate the push on gates.BoundaryDetected so the push fires
	// only when all child Phases have actually Succeeded (the spec's "all-
	// children-Succeeded" boundary). At the moment handleJobCompletion runs,
	// child Phases have just been MATERIALIZED — not yet Succeeded — so the
	// short-circuit returns false on first entry, and the push only fires on
	// a subsequent reconcile (Owns(&Phase{}) re-enqueues when child status
	// updates). On the no-boundary path we still proceed to patchMilestoneSucceeded
	// to preserve the existing gate-test fixtures that assert immediate
	// Succeeded on auto-gate (the milestone level's own Job completion is a
	// sufficient signal for parent-Status=Succeeded; the push semantic is
	// what's tightened, not the level transition).
	detected, derr := gates.BoundaryDetected(ctx, r.Client, ms, "Phase")
	if derr != nil {
		return ctrl.Result{}, derr
	}
	if detected {
		if err := r.maybeTriggerBoundaryPush(ctx, ms, project); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		logger.V(1).Info("boundary push skipped: child Phases not all Succeeded yet", "milestone", ms.Name)
	}

	return r.patchMilestoneSucceeded(ctx, ms)
}

func (r *MilestoneReconciler) patchMilestoneSucceeded(ctx context.Context, ms *tideprojectv1alpha1.Milestone) (ctrl.Result, error) {
	patch := client.MergeFrom(ms.DeepCopy())
	ms.Status.Phase = "Succeeded"
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             "PlannerComplete",
		Message:            "Milestone planner completed; Phase children materialized",
		LastTransitionTime: metav1.Now(),
	})
	// Plan 04-05: when the level resumes from a prior approve-pause, clear the
	// WaveOrLevelPaused Condition to False with Reason=ResumedByUser so the
	// transition is visible to operators tailing conditions.
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             tideprojectv1alpha1.ReasonResumedByUser,
		Message:            "Milestone resumed from gate boundary",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, ms, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// patchMilestoneAwaitingApproval parks the Milestone at Status.Phase=AwaitingApproval
// with ConditionWaveOrLevelPaused True (Plan 04-05 D-G2/D-G3 surface). Returns
// ctrl.Result{} with no requeue — only an AnnotationChangedPredicate-triggered
// re-reconcile (via the approve annotation write) advances the level (T-04-G4
// mitigation: no polling DoS).
func (r *MilestoneReconciler) patchMilestoneAwaitingApproval(ctx context.Context, ms *tideprojectv1alpha1.Milestone, policy tideprojectv1alpha1.GatePolicy) (ctrl.Result, error) {
	reason := tideprojectv1alpha1.ReasonAwaitingApproval
	message := "Milestone awaiting operator approve annotation (tideproject.k8s/approve-milestone=true)"
	if policy == gates.PolicyPause {
		reason = tideprojectv1alpha1.ReasonPausedAtBoundary
		message = "Milestone paused at boundary; requires explicit resume"
	}
	patch := client.MergeFrom(ms.DeepCopy())
	ms.Status.Phase = "AwaitingApproval"
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, ms, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *MilestoneReconciler) patchMilestoneFailed(ctx context.Context, ms *tideprojectv1alpha1.Milestone, reason, message string) (ctrl.Result, error) {
	patch := client.MergeFrom(ms.DeepCopy())
	ms.Status.Phase = "Failed"
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionFailed,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, ms, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
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
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	annotationOnly := predicate.AnnotationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Milestone{}).
		Owns(&batchv1.Job{}).
		Owns(&tideprojectv1alpha1.Phase{}).
		Watches(
			&tideprojectv1alpha1.Milestone{},
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
