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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
)

const (
	projectFinalizer = "tideproject.k8s/project-cleanup"
	// finalizerCleanupTimeout bounds every finalizer cleanup callback (Pitfall 21).
	finalizerCleanupTimeout = 5 * time.Minute
	// defaultSharedPVCName is the cluster-wide PVC provisioned by the Helm chart (Plan 12).
	defaultSharedPVCName = "tide-projects"
	// initJobBusyboxImage is the init Job container image (Plan 12 Helm value images.busybox).
	initJobBusyboxImage = "busybox:1.36"
	// initJobRequeueAfterNoPVC is the requeue interval when the shared PVC is absent (Pitfall 1).
	initJobRequeueAfterNoPVC = 30 * time.Second
)

// ProjectReconciler reconciles a Project object at Standard depth (D-C1):
// fetch, finalizer-on-delete, finalizer-ensure-on-create, owner-ref-on-children
// (Project has no parent), status condition propagation, Status().Update.
//
// The Dispatcher field is nil in Phase 1; Phase 2 (REQ-SUB-01) injects a real
// dispatch.Dispatcher and fills the `if r.Dispatcher != nil { ... }` body.
type ProjectReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// MaxConcurrentReconciles is the per-Kind reconcile parallelism budget (CTRL-04).
	MaxConcurrentReconciles int

	// PlannerPool and ExecutorPool are the two parallelism budgets (POOL-01).
	// Project keeps both nil-able fields so the struct shape is uniform across
	// all six reconcilers; Phase 2 wires neither for Project.
	PlannerPool  *pool.Pool
	ExecutorPool *pool.Pool

	// Dispatcher is the Phase 2 subagent-dispatch seam (REQ-SUB-01).
	// Nil in Phase 1; Phase 2's main.go injects a concrete impl.
	Dispatcher dispatch.Dispatcher

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string

	// SharedPVCName is the name of the cluster-wide PVC provisioned by the
	// Helm chart (Plan 12). Defaults to "tide-projects". Configurable via
	// --workspaces-pvc-name flag on the manager (Blocker #2/#3 architecture).
	SharedPVCName string

	// Recorder emits K8s Events for observable budget and bypass transitions
	// (T-02-10-05 — audit trail for AbsoluteCapReached; T-02-10-01 — bypass).
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var project tideprojectv1alpha1.Project
	if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !project.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &project, projectFinalizer,
			func(_ context.Context) error {
				logger.Info("project cleanup", "name", project.Name)
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&project, projectFinalizer) {
		controllerutil.AddFinalizer(&project, projectFinalizer)
		if err := r.Update(ctx, &project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Owner refs on children — Project is top-level; no parent to reference.

	// 5. Phase 2: dispatcher seam — init Job + budget gate + bypass watch (REQ-SUB-01).
	if r.Dispatcher != nil {
		return r.reconcileProjectPhase2(ctx, &project)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonInitialized,
		Message:            "Project scaffolded; awaiting dispatch logic (Phase 2)",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &project); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileProjectPhase2 implements the Phase 2 body inside the dispatcher seam:
// 1. Budget cap check + bypass annotation handling.
// 2. Shared PVC bind check (Blocker #2/#3 single-PVC architecture).
// 3. Init Job creation (idempotent, deterministic name tide-init-{UID}).
// 4. Init Job completion watch — patches Project.Status.Phase.
func (r *ProjectReconciler) reconcileProjectPhase2(ctx context.Context, project *tideprojectv1alpha1.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	now := time.Now()

	// Step 1: Budget cap check + bypass annotation handling.
	result, err := r.handleBudgetGate(ctx, project, now)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If the project is in BudgetExceeded and bypass did not clear it, halt dispatch.
	if project.Status.Phase == tideprojectv1alpha1.PhaseBudgetExceeded {
		return result, nil
	}

	// Step 2: Shared PVC bind check.
	pvcName := r.sharedPVCName()
	var pvc corev1.PersistentVolumeClaim
	if err := r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: pvcName}, &pvc); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("shared PVC not found; requeueing (Pitfall 1 — non-blocking)", "pvcName", pvcName)
			return ctrl.Result{RequeueAfter: initJobRequeueAfterNoPVC}, nil
		}
		return ctrl.Result{}, err
	}
	if pvc.Status.Phase != corev1.ClaimBound {
		logger.Info("shared PVC not yet Bound; requeueing", "pvcName", pvcName, "pvcPhase", pvc.Status.Phase)
		return ctrl.Result{RequeueAfter: initJobRequeueAfterNoPVC}, nil
	}

	// Step 3: Init Job creation (idempotent).
	initJobName := fmt.Sprintf("tide-init-%s", project.UID)
	var existingJob batchv1.Job
	err = r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: initJobName}, &existingJob)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		// Job does not exist yet — create it.
		if createErr := r.ensureInitJob(ctx, project, pvcName); createErr != nil {
			return ctrl.Result{}, createErr
		}
		return ctrl.Result{}, nil
	}

	// Step 4: Watch init Job completion — patch Project.Status.Phase based on outcome.
	return r.handleInitJobCompletion(ctx, project, &existingJob)
}

// ensureInitJob creates the one-shot init Job (idempotent — AlreadyExists is success).
func (r *ProjectReconciler) ensureInitJob(ctx context.Context, project *tideprojectv1alpha1.Project, pvcName string) error {
	job := r.buildInitJob(project, pvcName)
	if err := owner.EnsureOwnerRef(job, project, r.Scheme); err != nil {
		return fmt.Errorf("ensure owner ref on init job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil // idempotent success
		}
		return fmt.Errorf("create init job: %w", err)
	}
	return nil
}

// handleInitJobCompletion inspects the init Job's terminal state and patches
// Project.Status.Phase accordingly.
func (r *ProjectReconciler) handleInitJobCompletion(ctx context.Context, project *tideprojectv1alpha1.Project, job *batchv1.Job) (ctrl.Result, error) {
	if isJobSucceeded(job) {
		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhaseInitialized
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             tideprojectv1alpha1.ReasonInitialized,
			Message:            fmt.Sprintf("Init Job %s completed successfully", job.Name),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, patch); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if isJobFailed(job) {
		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhaseInitFailed
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "InitJobFailed",
			Message:            fmt.Sprintf("Init Job %s failed", job.Name),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, patch); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Job still running — watch via Owns event; nothing to do now.
	return ctrl.Result{}, nil
}

// handleBudgetGate checks the budget cap and bypass annotations, patching
// Project.Status.Phase and emitting K8s Events as needed (D-D4, FAIL-04).
// After this call, project.Status.Phase reflects the current budget state.
func (r *ProjectReconciler) handleBudgetGate(ctx context.Context, project *tideprojectv1alpha1.Project, now time.Time) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	bypassed := budget.IsBypassed(project, now)
	capExceeded := budget.IsCapExceeded(project)

	if project.Status.Phase == tideprojectv1alpha1.PhaseBudgetExceeded && bypassed {
		// Bypass is active — clear BudgetExceeded and record Event.
		logger.Info("budget bypass active; clearing BudgetExceeded", "project", project.Name)

		// Consume the one-shot annotation if present.
		newAnnotations := budget.ConsumeBypass(project)
		if len(newAnnotations) != len(project.Annotations) {
			// Annotations changed — patch metadata.
			annotPatch := client.MergeFrom(project.DeepCopy())
			project.Annotations = newAnnotations
			if err := r.Patch(ctx, project, annotPatch); err != nil {
				return ctrl.Result{}, fmt.Errorf("consume bypass annotation: %w", err)
			}
		}

		// Clear the phase.
		statusPatch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhasePending
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionBudgetExceeded,
			Status:             metav1.ConditionFalse,
			Reason:             tideprojectv1alpha1.ReasonBypassApplied,
			Message:            "Budget exceeded bypass applied by operator",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		if r.Recorder != nil {
			r.Recorder.Event(project, corev1.EventTypeNormal, tideprojectv1alpha1.ReasonBypassApplied,
				"Budget exceeded bypass applied by operator; dispatch resumed")
		}
		return ctrl.Result{}, nil
	}

	if project.Status.Phase != tideprojectv1alpha1.PhaseBudgetExceeded && capExceeded && !bypassed {
		// Cap hit — set BudgetExceeded and record Event.
		logger.Info("budget cap exceeded; halting dispatch", "project", project.Name)
		statusPatch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhaseBudgetExceeded
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionBudgetExceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "AbsoluteCapReached",
			Message:            fmt.Sprintf("Cost spent %d cents exceeds cap %d cents", project.Status.Budget.CostSpentCents, project.Spec.Budget.AbsoluteCapCents),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		if r.Recorder != nil {
			r.Recorder.Event(project, corev1.EventTypeWarning, "AbsoluteCapReached",
				fmt.Sprintf("Project budget cap reached: %d cents spent of %d cents allowed", project.Status.Budget.CostSpentCents, project.Spec.Budget.AbsoluteCapCents))
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// buildInitJob constructs the one-shot busybox init Job that bootstraps the
// per-Project workspace layout on the shared PVC (D-G1, Blocker #2/#3).
//
// The Job is deterministically named `tide-init-{project.UID}` — the
// AlreadyExists dedup key. The subPath isolates this Project's slice of the
// shared PVC from all other Projects.
func (r *ProjectReconciler) buildInitJob(project *tideprojectv1alpha1.Project, pvcName string) *batchv1.Job {
	backoffLimit := int32(2)
	ttl := int32(300)
	runAsUser := int64(1000)
	fsGroup := int64(1000)
	allowPrivEsc := false
	subPath := fmt.Sprintf("%s/workspace", project.UID)
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("tide-init-%s", project.UID),
			Namespace: project.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: "tide-subagent",
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: &fsGroup,
					},
					Volumes: []corev1.Volume{
						{
							Name: "project-workspace",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "init",
							Image: initJobBusyboxImage,
							Command: []string{
								"sh", "-c",
								"mkdir -p /workspace/repo /workspace/artifacts /workspace/envelopes && chmod 0775 /workspace/repo /workspace/artifacts /workspace/envelopes",
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:                &runAsUser,
								ReadOnlyRootFilesystem:   boolPtr(false),
								AllowPrivilegeEscalation: &allowPrivEsc,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "project-workspace",
									MountPath: "/workspace",
									SubPath:   subPath,
								},
							},
						},
					},
				},
			},
		},
	}
}

// sharedPVCName returns the configured shared PVC name or the default.
func (r *ProjectReconciler) sharedPVCName() string {
	if r.SharedPVCName != "" {
		return r.SharedPVCName
	}
	return defaultSharedPVCName
}

// isJobSucceeded returns true if the Job has a Complete condition with ConditionTrue.
func isJobSucceeded(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// isJobFailed returns true if the Job has a Failed condition with ConditionTrue.
func isJobFailed(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// boolPtr returns a pointer to the given bool value.
func boolPtr(b bool) *bool {
	return &b
}

// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02,
// annotation-change predicate for bypass annotations (D-D4), and a
// namespace-filter predicate per AUTH-02.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("project-controller")
	}
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true // watch-all-namespaces mode
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Project{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			)),
		).
		Owns(&batchv1.Job{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("project").
		Complete(r)
}
