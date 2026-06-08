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
	"k8s.io/apimachinery/pkg/types"
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

	// CredproxyImage is the image ref for the tide-credproxy sidecar.
	// Phase 04.1 P1.2 fix: planner Jobs share the credproxy sidecar contract.
	CredproxyImage string

	// SigningKey is the HMAC signing key used to mint per-dispatch tokens
	// for the credproxy sidecar (Phase 04.1 P1.2 fix).
	SigningKey []byte

	// TidePushImage is the image ref for the tide-push container used by
	// the W-2 boundary push trigger (plan 04-06).
	TidePushImage string

	// ReporterImage is the image ref for the tide-reporter reader Job (Phase 09 plan 09-06).
	// When empty, spawning the reader Job is skipped (mirrors TidePushImage skip in
	// boundary_push.go:80-88). Set via TIDE_REPORTER_IMAGE env from Helm values.
	ReporterImage string

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

	// Idempotency guard — skip NEW planner dispatch when the Phase already owns
	// >=1 Plan. Placed AFTER the terminal/Running short-circuits so it gates only
	// fresh authoring, never the Phase's own completion/boundary handling (the
	// early placement broke TestBoundaryPush_AllLevels). Symmetric to the
	// milestone/project guards. cascade-10: match by spec.phaseRef (set
	// synchronously at child-apply time), NOT ownerRef — a pre-applied child
	// (chaos-resume-plan) gets its ownerRef set asynchronously by the PlanReconciler,
	// so an IsControlledBy-only guard races and lets the Phase author a spurious
	// stub-plan-1. specRef is race-free; ownerRef kept as a fallback. bare-Project
	// flow is unaffected: each Phase starts with 0 child Plans and authors once.
	{
		var existingPlans tideprojectv1alpha1.PlanList
		if lErr := r.List(ctx, &existingPlans, client.InNamespace(ph.Namespace)); lErr != nil {
			return ctrl.Result{}, fmt.Errorf("idempotency: list plans: %w", lErr)
		}
		for i := range existingPlans.Items {
			if existingPlans.Items[i].Spec.PhaseRef == ph.Name || metav1.IsControlledBy(&existingPlans.Items[i], ph) {
				// Phase already has a child Plan — planner already authored; skip dispatch.
				return ctrl.Result{}, nil
			}
		}
	}

	// Acquire plannerPool before creating Job (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, err
		}
		defer r.PlannerPool.Release()
	}

	project := r.resolveProject(ctx, ph)

	// Phase 04.1 P1.2 fix: planner Jobs now share the full Phase 2 dispatch
	// contract via podjob.BuildJobSpec(Kind=JobKindPlanner).
	attempt := 1 // phase planner dispatch is single-shot per ROADMAP scope

	plannerCaps := podjob.DefaultCaps(nil, podjob.JobKindPlanner)
	if plannerCaps.Iterations <= 0 {
		plannerCaps.Iterations = 20
	}
	plannerPrompt := outcomePromptOf(project)
	_, envInJSON, err := BuildPlannerEnvelope("phase", ph, project, attempt, "", plannerPrompt, pkgdispatch.Caps{
		WallClockSeconds: int(plannerCaps.WallClockSeconds),
		Iterations:       int(plannerCaps.Iterations),
	}, "https://127.0.0.1:8443", r.HelmProviderDefaults)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build planner envelope: %w", err)
	}

	// Mint a signed token for the credproxy sidecar.
	token, err := credproxy.Sign(r.SigningKey, string(ph.UID), time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("mint planner signed token: %w", err)
	}

	var secretUID string
	if project != nil && project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}

	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	subagentImage := r.SubagentImage
	if subagentImage == "" {
		subagentImage = r.HelmProviderDefaults.Image
	}

	opts := podjob.BuildOptions{
		Kind:           podjob.JobKindPlanner,
		ParentObj:      ph,
		Level:          "phase",
		Attempt:        attempt,
		Project:        project,
		SignedToken:    token,
		EnvelopeInJSON: envInJSON,
		SubagentImage:  subagentImage,
		CredproxyImage: r.CredproxyImage,
		SecretUID:      secretUID,
		PVCName:        "tide-projects",
		ProjectUID:     projectUID,
		Caps:           plannerCaps,
	}
	job := podjob.BuildJobSpec(opts)
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

	// Read tiny status from the dispatch Job's termination message for budget
	// rollup and failure classification. ChildCRDs are NOT used here —
	// materialization has moved to the reporter Job (REQ-09-01). Continue through
	// gate + boundary-push logic regardless — those are envelope-independent.
	// Phase 04.1: previously a nil EnvReader short-circuited to patchSucceeded,
	// which skipped the boundary push trigger.
	var out pkgdispatch.EnvelopeOut
	envReadOK := false
	if r.EnvReader != nil {
		var readErr error
		out, readErr = r.EnvReader.ReadOut(ctx, projectUID, string(ph.UID))
		if readErr != nil {
			return r.patchPhaseFailed(ctx, ph, "EnvelopeReadFailed", readErr.Error())
		}
		envReadOK = true
	} else {
		logger.V(1).Info("no env reader; skipping tiny-status read", "phase", ph.Name)
	}

	// Spawn the tide-reporter reader Job in the project namespace (Option C).
	// The reporter reads out.json from the PVC and materializes Plan children.
	// Children arrive via the Owns(&Plan{}) watch once the reporter creates them.
	// T-09-13: idempotent — AlreadyExists on Create is success.
	if r.ReporterImage != "" && project != nil {
		reporterJobName := fmt.Sprintf("tide-reporter-%s", ph.UID)
		pvcName := defaultSharedPVCName
		var existingReporterJob batchv1.Job
		if gErr := r.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: ph.Namespace}, &existingReporterJob); gErr != nil {
			if !apierrors.IsNotFound(gErr) {
				return ctrl.Result{}, fmt.Errorf("get reporter job %s: %w", reporterJobName, gErr)
			}
			reporterJob := BuildReporterJob(ph, project, pvcName, string(ph.UID), "Phase",
				ReporterOptions{ReporterImage: r.ReporterImage}, r.Scheme)
			if cErr := r.Create(ctx, reporterJob); cErr != nil {
				if !apierrors.IsAlreadyExists(cErr) {
					return ctrl.Result{}, fmt.Errorf("create reporter job %s: %w", reporterJobName, cErr)
				}
			} else {
				logger.Info("spawned reporter Job", "job", reporterJobName, "phase", ph.Name)
			}
		} else {
			logger.V(1).Info("reporter Job already exists; skipping spawn (T-09-13)", "job", reporterJobName)
		}
	} else if r.ReporterImage == "" {
		logger.V(1).Info("skipping reporter Job spawn: ReporterImage not configured", "phase", ph.Name)
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
	//
	// Plan 09-08 Defect B fix: uniform ChildCount-gated succession replaces the
	// prior missing guard that caused the Phase to fall straight through to
	// patchPhaseSucceeded while its child Plans were still being materialized
	// by the reporter Job (the root cause of the premature-succession race).
	// Gate (mirrors milestone_controller.go):
	//   expected == 0            → succeed (genuine leaf: planner authored no Plans)
	//   observed < expected      → requeue 5s (reporter still materializing)
	//   observed >= expected     → BoundaryDetected? push+succeed : requeue 5s
	// When EnvReader is nil, fall back to the prior hasChildPlans behavior so
	// non-Option-C / unit-test paths keep working.
	if envReadOK {
		// Option-C path: gate on out.ChildCount from tiny status.
		expected := out.ChildCount
		if expected == 0 {
			// Genuine leaf — planner authored no Plan children.
			logger.V(1).Info("boundary push skipped: planner authored no Plan children (leaf)", "phase", ph.Name)
			return r.patchPhaseSucceeded(ctx, ph)
		}
		observed := r.countChildPlans(ctx, ph)
		if observed < expected {
			logger.V(1).Info("requeue: reporter still materializing Plan children",
				"phase", ph.Name, "expected", expected, "observed", observed)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		// observed >= expected: check if all Succeeded.
		detected, derr := gates.BoundaryDetected(ctx, r.Client, ph, "Plan")
		if derr != nil {
			return ctrl.Result{}, derr
		}
		if detected {
			if err := r.maybeTriggerBoundaryPush(ctx, ph, project); err != nil {
				return ctrl.Result{}, err
			}
			return r.patchPhaseSucceeded(ctx, ph)
		}
		logger.V(1).Info("boundary push deferred: Plan children exist but not all Succeeded",
			"phase", ph.Name, "expected", expected, "observed", observed)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Fallback: EnvReader is nil (non-Option-C / unit-test path). Use the prior
	// hasChild-based behavior (mirrors milestone_controller.go fallback).
	detected, derr := gates.BoundaryDetected(ctx, r.Client, ph, "Plan")
	if derr != nil {
		return ctrl.Result{}, derr
	}
	if detected {
		if err := r.maybeTriggerBoundaryPush(ctx, ph, project); err != nil {
			return ctrl.Result{}, err
		}
	} else if r.hasChildPlans(ctx, ph) {
		logger.V(1).Info("boundary push deferred: child Plans pending (fallback)", "phase", ph.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	} else {
		logger.V(1).Info("boundary push skipped: phase has no child Plans (fallback)", "phase", ph.Name)
	}

	return r.patchPhaseSucceeded(ctx, ph)
}

// hasChildPlans reports whether any Plan is owned by this Phase. Phase 04.1.
// Used by the nil-EnvReader fallback path in handleJobCompletion.
func (r *PhaseReconciler) hasChildPlans(ctx context.Context, ph *tideprojectv1alpha1.Phase) bool {
	return r.countChildPlans(ctx, ph) > 0
}

// countChildPlans returns the number of Plans owned by this Phase (plan 09-08).
// Used by the ChildCount-gated succession path to compare observed vs expected children.
func (r *PhaseReconciler) countChildPlans(ctx context.Context, ph *tideprojectv1alpha1.Phase) int {
	var planList tideprojectv1alpha1.PlanList
	if err := r.List(ctx, &planList, client.InNamespace(ph.Namespace)); err != nil {
		return 0
	}
	count := 0
	for i := range planList.Items {
		for _, ref := range planList.Items[i].OwnerReferences {
			if ref.Kind == "Phase" && ref.UID == ph.UID {
				count++
			}
		}
	}
	return count
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
