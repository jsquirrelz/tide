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

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
)

const waveFinalizer = "tideproject.k8s/wave-cleanup"

// WaveReconciler reconciles a Wave object at Standard depth (D-C1).
// Wave is owned by Plan; the parent ref is set via internal/owner.EnsureOwnerRef.
//
// Per D-B2, D-B4: WaveReconciler is OBSERVATIONAL ONLY — it aggregates member
// Task statuses and patches Wave.Status. It NEVER creates Jobs (D-B1 reserves
// Job creation exclusively to TaskReconciler).
type WaveReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	PlannerPool *pool.Pool
	// ExecutorPool — Wave reconcile dispatches executor-pool Tasks in Phase 2.
	ExecutorPool *pool.Pool

	Dispatcher dispatch.Dispatcher

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=waves,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=waves/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=waves/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks,verbs=get;list;watch

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *WaveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch v1alpha2.Wave (Spring Tide: Wave ownership moved Plan→Project, Plan 23-02).
	var wave tideprojectv1alpha2.Wave
	if err := r.Get(ctx, req.NamespacedName, &wave); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !wave.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &wave, waveFinalizer,
			func(_ context.Context) error {
				logger.Info("wave cleanup", "name", wave.Name)
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&wave, waveFinalizer) {
		controllerutil.AddFinalizer(&wave, waveFinalizer)
		if err := r.Update(ctx, &wave); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Owner ref to parent (CRD-02, Pitfall 23 prevention).
	// Owner ref is set at Wave create time by ProjectReconciler.deriveGlobalWaves via
	// owner.EnsureOwnerRef(wave, project, r.Scheme). No action needed here for new
	// Waves. The WaveReconciler trusts the owner ref written at create time.

	// 5. Phase 2 observational roll-up body (D-B2, D-B4 — NO Job creation).
	if r.Dispatcher != nil {
		return r.reconcileObservational(ctx, &wave)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&wave.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha2.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha2.ReasonInitialized,
		Message:            "Wave scaffolded; dispatcher not wired",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &wave); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileObservational implements the Wave observational roll-up body inside
// the Dispatcher seam (step 5 of the six-step pattern). Per D-B2 and D-B4,
// this method is PURELY observational — it NEVER creates Jobs.
func (r *WaveReconciler) reconcileObservational(ctx context.Context, wave *tideprojectv1alpha2.Wave) (ctrl.Result, error) {
	// Step 1: List Tasks by the tideproject.k8s/wave-index label stamped by
	// ProjectReconciler.stampGlobalTaskLabels (Phase 24 Plan 03). The global wave
	// index is Project-scoped: wave-index=<N> AND project=<ProjectRef> identifies
	// exactly the Tasks in global wave N. This is the correct bidirectional index
	// (EXEC-03 / README:54 namesake invariant).
	waveIndexLabel := fmt.Sprintf("%d", wave.Spec.WaveIndex)
	var taskList tideprojectv1alpha2.TaskList
	// Scope the roll-up to THIS Wave's project (interim WR-01 fix): wave-index is a
	// small integer reused per-plan, so a bare wave-index match cross-contaminates
	// Tasks from sibling plans/projects in the same namespace. Filtering by
	// owner.LabelProject == wave.Spec.ProjectRef removes that contamination until the
	// Phase-24 global wave index lands (see the TODO above).
	if err := r.List(ctx, &taskList,
		client.InNamespace(wave.Namespace),
		client.MatchingLabels{
			"tideproject.k8s/wave-index": waveIndexLabel,
			owner.LabelProject:           wave.Spec.ProjectRef,
		},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list tasks for wave %s: %w", wave.Name, err)
	}

	// Step 2: Filter by tideproject.k8s/wave-index = wave.Spec.WaveIndex label.
	// The label was already used to filter in the List call above; this pass
	// provides a secondary filter in case the label index is not exact-match only.
	var members []tideprojectv1alpha2.Task
	for _, t := range taskList.Items {
		if t.Labels["tideproject.k8s/wave-index"] == waveIndexLabel {
			members = append(members, t)
		}
	}

	// Step 3: Aggregate phase.
	// - Succeeded iff ALL members Succeeded.
	// - Failed iff ANY member Failed.
	// - Running otherwise.
	taskRefs := make([]string, 0, len(members))
	var failedTask string
	allSucceeded := len(members) > 0
	for _, m := range members {
		taskRefs = append(taskRefs, m.Name)
		if m.Status.Phase != "Succeeded" {
			allSucceeded = false
		}
		if m.Status.Phase == "Failed" && failedTask == "" {
			failedTask = m.Name
		}
	}

	var phase, message string
	switch {
	case allSucceeded && len(members) > 0:
		phase = "Succeeded"
		message = fmt.Sprintf("All %d member task(s) succeeded", len(members))
	case failedTask != "":
		phase = "Failed"
		message = fmt.Sprintf("Member task %q failed", failedTask)
	default:
		phase = "Running"
		message = fmt.Sprintf("%d member task(s); awaiting completion", len(members))
	}

	// Step 4: Patch Wave.Status.{Phase, TaskRefs, Conditions}.
	patch := client.MergeFrom(wave.DeepCopy())
	wave.Status.Phase = phase
	wave.Status.TaskRefs = taskRefs
	condType := tideprojectv1alpha2.ConditionReconciling
	condStatus := metav1.ConditionTrue
	reason := "Aggregating"
	switch phase {
	case "Succeeded":
		condType = tideprojectv1alpha2.ConditionSucceeded
		reason = "AllTasksSucceeded"
	case "Failed":
		condType = tideprojectv1alpha2.ConditionFailed
		reason = "MemberTaskFailed"
	}
	meta.SetStatusCondition(&wave.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, wave, patch); err != nil {
		return ctrl.Result{}, err
	}

	// Step 5: No Job creation (D-B2 — D-B1 reserves Job creation to TaskReconciler).
	return ctrl.Result{}, nil
}

// taskToWaveMapper returns the reconcile request for the one Wave that owns this
// Task, using the global wave-index label stamped by ProjectReconciler.stampGlobalTaskLabels
// (Phase 24 Plan 03). The Wave name is derived deterministically:
//
//	tide-wave-<projectName>-<waveIndex>
//
// This is an O(1) lookup — no List call required. Returns nil if the Task does not
// yet carry the project or wave-index labels (e.g., still awaiting first reconcile).
func (r *WaveReconciler) taskToWaveMapper(_ context.Context, obj client.Object) []reconcile.Request {
	task, ok := obj.(*tideprojectv1alpha2.Task)
	if !ok {
		return nil
	}
	labels := task.GetLabels()
	projectName := labels[owner.LabelProject]
	waveIndexStr := labels["tideproject.k8s/wave-index"]
	if projectName == "" || waveIndexStr == "" {
		return nil
	}
	waveName := fmt.Sprintf("tide-wave-%s-%s", projectName, waveIndexStr)
	return []reconcile.Request{{
		NamespacedName: client.ObjectKey{Namespace: task.Namespace, Name: waveName},
	}}
}

// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02, a
// namespace-filter predicate per AUTH-02, and a Task→Wave watch for D-B4.
// Plan 04-05: also wires AnnotationChangedPredicate via a self-Watches handler
// so wave-approve annotation writes on the Wave (operator-driven D-G3 surface)
// trigger reconciliation (T-04-G4 mitigation — no polling). The self-Watches
// pattern avoids filtering finalizer/owner-ref Update events at the For() level.
func (r *WaveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	annotationOnly := predicate.AnnotationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha2.Wave{}).
		Watches(
			&tideprojectv1alpha2.Task{},
			handler.EnqueueRequestsFromMapFunc(r.taskToWaveMapper),
		).
		Watches(
			&tideprojectv1alpha2.Wave{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(annotationOnly),
		).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("wave").
		Complete(r)
}
