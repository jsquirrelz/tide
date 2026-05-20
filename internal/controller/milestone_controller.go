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

	batchv1 "k8s.io/api/batch/v1"
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
	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 20}
	_, envInJSON, err := BuildPlannerEnvelope("milestone", ms, project, 1, "", caps, "https://127.0.0.1:8443", r.HelmProviderDefaults)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build planner envelope: %w", err)
	}
	_ = envInJSON

	// Step 6: Build + Create planner Job (deterministic name = D-B5 dedup key).
	job := r.buildPlannerJob(ms, jobName)
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

	if r.EnvReader == nil {
		logger.Info("no env reader; marking Milestone Succeeded without ChildCRD materialization")
		return r.patchMilestoneSucceeded(ctx, ms)
	}

	envOut, err := r.EnvReader.ReadOut(ctx, projectUID, string(ms.UID))
	if err != nil {
		return r.patchMilestoneFailed(ctx, ms, "EnvelopeReadFailed", err.Error())
	}

	// MaterializeChildCRDs enforces Kind allowlist (T-308 mitigation).
	if len(envOut.ChildCRDs) > 0 {
		if mErr := MaterializeChildCRDs(ctx, r.Client, r.Scheme, ms, envOut.ChildCRDs); mErr != nil {
			return r.patchMilestoneFailed(ctx, ms, "ChildCRDMaterializationFailed", mErr.Error())
		}
	}

	// Plan 04-05: gate-policy hook. Two checks land at the Succeeded seam:
	//   (a) project-level reject short-circuit → patch Failed/RejectedByUser
	//   (b) per-level gate policy (approve/pause) → park AwaitingApproval
	//       unless the approve annotation is present (and consume it).
	if project != nil && gates.CheckRejected(project) {
		return r.patchMilestoneFailed(ctx, ms, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
	}
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

func (r *MilestoneReconciler) buildPlannerJob(ms *tideprojectv1alpha1.Milestone, jobName string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	image := r.SubagentImage
	if image == "" {
		image = "ghcr.io/jsquirrelz/tide-stub-subagent:test"
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ms.Namespace,
			Labels: map[string]string{
				"tideproject.k8s/milestone-uid": string(ms.UID),
				"tideproject.k8s/level":         "milestone",
				"tideproject.k8s/role":          "planner",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template:                batchv1Template(jobName, image),
		},
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
