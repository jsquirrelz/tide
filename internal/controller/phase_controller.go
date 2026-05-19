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

const phaseFinalizer = "tideproject.k8s/phase-cleanup"

// PhaseReconciler reconciles a Phase object at Standard depth (D-C1).
// Phase is owned by Milestone. Phase 3 fills the body (plan 03-08) to
// dispatch a planner Job and materialize Plan child CRDs on completion.
type PhaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	// PlannerPool — up-stack reconciler acquires plannerPool only (POOL-03).
	PlannerPool  *pool.Pool
	ExecutorPool *pool.Pool

	Dispatcher dispatch.Dispatcher

	// EnvReader reads EnvelopeOut from PVC after planner Job completes.
	EnvReader podjob.EnvelopeReader

	// SubagentImage is the planner subagent container image.
	SubagentImage string

	// TidePushImage is the image ref for the tide-push container used by
	// the W-2 boundary push trigger (plan 04-06).
	TidePushImage string

	// HelmProviderDefaults carry Helm-chart provider/model defaults.
	HelmProviderDefaults ProviderDefaults

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=phases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=phases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=phases/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones,verbs=get;list;watch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *PhaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var phase tideprojectv1alpha1.Phase
	if err := r.Get(ctx, req.NamespacedName, &phase); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !phase.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &phase, phaseFinalizer,
			func(_ context.Context) error {
				logger.Info("phase cleanup", "name", phase.Name)
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&phase, phaseFinalizer) {
		controllerutil.AddFinalizer(&phase, phaseFinalizer)
		if err := r.Update(ctx, &phase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure owner ref to parent Milestone (CRD-02, Pitfall 23 prevention).
	if phase.Spec.MilestoneRef != "" {
		var parent tideprojectv1alpha1.Milestone
		if err := r.Get(ctx, client.ObjectKey{Namespace: phase.Namespace, Name: phase.Spec.MilestoneRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) == nil {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		if err := owner.EnsureOwnerRef(&phase, &parent, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Update(ctx, &phase); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 5. Phase 3: planner dispatch body (REQ-SUB-01, D-A2).
	if r.Dispatcher != nil {
		return r.reconcilePlannerDispatch(ctx, &phase)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&phase.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonInitialized,
		Message:            "Phase scaffolded; awaiting dispatch logic (Phase 2)",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &phase); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcilePlannerDispatch mirrors MilestoneReconciler one level down.
// Dispatches tide-phase-<phase-uid>-<attempt>; on completion materializes
// Plan child CRDs from EnvelopeOut.ChildCRDs.
func (r *PhaseReconciler) reconcilePlannerDispatch(ctx context.Context, ph *tideprojectv1alpha1.Phase) (ctrl.Result, error) {
	if ph.Status.Phase == "Succeeded" || ph.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}

	jobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)

	if ph.Status.Phase == "Running" {
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		if isJobTerminal(&job) {
			return r.handleJobCompletion(ctx, ph, &job)
		}
		return ctrl.Result{}, nil
	}

	// Acquire plannerPool before creating Job (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, err
		}
		defer r.PlannerPool.Release()
	}

	project := r.resolveProject(ctx, ph)

	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 20}
	_, envInJSON, err := BuildPlannerEnvelope("phase", ph, project, 1, "", caps, "https://127.0.0.1:8443", r.HelmProviderDefaults)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build planner envelope: %w", err)
	}
	_ = envInJSON

	job := r.buildPlannerJob(ph, jobName)
	if err := owner.EnsureOwnerRef(job, ph, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure owner ref on planner job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create planner job: %w", err)
		}
	}

	patch := client.MergeFrom(ph.DeepCopy())
	ph.Status.Phase = "Running"
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionAuthoringPlanner,
		Status:             metav1.ConditionTrue,
		Reason:             "PlannerDispatched",
		Message:            fmt.Sprintf("Planner Job %s dispatched", jobName),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, ph, patch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PhaseReconciler) handleJobCompletion(ctx context.Context, ph *tideprojectv1alpha1.Phase, _ *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	project := r.resolveProject(ctx, ph)
	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	if r.EnvReader == nil {
		logger.Info("no env reader; marking Phase Succeeded")
		return r.patchPhaseSucceeded(ctx, ph)
	}

	envOut, err := r.EnvReader.ReadOut(ctx, projectUID, string(ph.UID))
	if err != nil {
		return r.patchPhaseFailed(ctx, ph, "EnvelopeReadFailed", err.Error())
	}

	if len(envOut.ChildCRDs) > 0 {
		if mErr := MaterializeChildCRDs(ctx, r.Client, r.Scheme, ph, envOut.ChildCRDs); mErr != nil {
			return r.patchPhaseFailed(ctx, ph, "ChildCRDMaterializationFailed", mErr.Error())
		}
	}

	// Plan 04-05: gate-policy hook (mirrors milestone_controller.go pattern).
	if project != nil && gates.CheckRejected(project) {
		return r.patchPhaseFailed(ctx, ph, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
	}
	if project != nil {
		policy := gates.EvaluatePolicy(project.Spec.Gates, "phase")
		if policy == gates.PolicyApprove || policy == gates.PolicyPause {
			if !gates.CheckApprove(ph, "phase") {
				return r.patchPhaseAwaitingApproval(ctx, ph, policy)
			}
			newAnno := gates.ConsumeApprove(ph, "phase")
			patch := client.MergeFrom(ph.DeepCopy())
			ph.SetAnnotations(newAnno)
			if err := r.Patch(ctx, ph, patch); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Plan 04-06 W-2: boundary push trigger AFTER gate, BEFORE patchSucceeded.
	if err := r.maybeTriggerBoundaryPush(ctx, ph, project); err != nil {
		return ctrl.Result{}, err
	}

	return r.patchPhaseSucceeded(ctx, ph)
}

func (r *PhaseReconciler) patchPhaseSucceeded(ctx context.Context, ph *tideprojectv1alpha1.Phase) (ctrl.Result, error) {
	patch := client.MergeFrom(ph.DeepCopy())
	ph.Status.Phase = "Succeeded"
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             "PlannerComplete",
		Message:            "Phase planner completed; Plan children materialized",
		LastTransitionTime: metav1.Now(),
	})
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             tideprojectv1alpha1.ReasonResumedByUser,
		Message:            "Phase resumed from gate boundary",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, ph, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// patchPhaseAwaitingApproval parks the Phase at Status.Phase=AwaitingApproval
// per Plan 04-05 gate seam (T-04-G4 mitigation — no requeue).
func (r *PhaseReconciler) patchPhaseAwaitingApproval(ctx context.Context, ph *tideprojectv1alpha1.Phase, policy tideprojectv1alpha1.GatePolicy) (ctrl.Result, error) {
	reason := tideprojectv1alpha1.ReasonAwaitingApproval
	message := "Phase awaiting operator approve annotation (tideproject.k8s/approve-phase=true)"
	if policy == gates.PolicyPause {
		reason = tideprojectv1alpha1.ReasonPausedAtBoundary
		message = "Phase paused at boundary; requires explicit resume"
	}
	patch := client.MergeFrom(ph.DeepCopy())
	ph.Status.Phase = "AwaitingApproval"
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
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

func (r *PhaseReconciler) patchPhaseFailed(ctx context.Context, ph *tideprojectv1alpha1.Phase, reason, message string) (ctrl.Result, error) {
	patch := client.MergeFrom(ph.DeepCopy())
	ph.Status.Phase = "Failed"
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionFailed,
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

// resolveProject walks Phase → Milestone → Project. Returns nil on failure.
func (r *PhaseReconciler) resolveProject(ctx context.Context, ph *tideprojectv1alpha1.Phase) *tideprojectv1alpha1.Project {
	if ph.Spec.MilestoneRef == "" {
		return nil
	}
	var ms tideprojectv1alpha1.Milestone
	if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		return nil
	}
	if ms.Spec.ProjectRef == "" {
		return nil
	}
	var p tideprojectv1alpha1.Project
	if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return nil
	}
	return &p
}

func (r *PhaseReconciler) buildPlannerJob(ph *tideprojectv1alpha1.Phase, jobName string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	image := r.SubagentImage
	if image == "" {
		image = "ghcr.io/jsquirrelz/tide-stub-subagent:test"
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: ph.Namespace,
			Labels: map[string]string{
				"tideproject.k8s/phase-uid": string(ph.UID),
				"tideproject.k8s/level":     "phase",
				"tideproject.k8s/role":      "planner",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: batchv1Template(jobName, image),
		},
	}
}

// SetupWithManager wires Owns(&Job{}) and Owns(&Plan{}) per D-A2. Plan 04-05
// adds AnnotationChangedPredicate via a self-Watches handler so approve/reject
// annotations trigger reconciliation (T-04-G4 mitigation — no polling). The
// self-Watches pattern avoids filtering finalizer/owner-ref Update events at
// the For() level (a GenerationChangedPredicate-based Or would do that).
func (r *PhaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	annotationOnly := predicate.AnnotationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Phase{}).
		Owns(&batchv1.Job{}).
		Owns(&tideprojectv1alpha1.Plan{}).
		Watches(
			&tideprojectv1alpha1.Phase{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(annotationOnly),
		).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("phase").
		Complete(r)
}
