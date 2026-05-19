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

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
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

	// 1. Fetch.
	var wave tideprojectv1alpha1.Wave
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

	// 4. Ensure owner ref to parent Plan (CRD-02, Pitfall 23 prevention).
	// If the Plan is not found (e.g., Wave created before Plan or Plan deleted),
	// log and continue — owner ref is best-effort; observational roll-up must still proceed.
	if wave.Spec.PlanRef != "" {
		var parent tideprojectv1alpha1.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: wave.Namespace, Name: wave.Spec.PlanRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			// Plan not found: log and continue without owner ref.
			logger.V(1).Info("parent Plan not found; skipping owner ref", "planRef", wave.Spec.PlanRef)
		} else {
			if err := owner.EnsureOwnerRef(&wave, &parent, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Update(ctx, &wave); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 5. Phase 2 observational roll-up body (D-B2, D-B4 — NO Job creation).
	if r.Dispatcher != nil {
		return r.reconcileObservational(ctx, &wave)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&wave.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonInitialized,
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
func (r *WaveReconciler) reconcileObservational(ctx context.Context, wave *tideprojectv1alpha1.Wave) (ctrl.Result, error) {
	// Step 1: List Tasks via field-indexer .spec.planRef = wave.Spec.PlanRef.
	var taskList tideprojectv1alpha1.TaskList
	if err := r.List(ctx, &taskList,
		client.InNamespace(wave.Namespace),
		client.MatchingFields{taskPlanRefIndexKey: wave.Spec.PlanRef},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list tasks for wave %s: %w", wave.Name, err)
	}

	// Step 2: Filter by tideproject.k8s/wave-index = wave.Spec.WaveIndex label
	// (PlanReconciler stamps this label on each Task).
	waveIndexLabel := fmt.Sprintf("%d", wave.Spec.WaveIndex)
	var members []tideprojectv1alpha1.Task
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
	condType := tideprojectv1alpha1.ConditionReconciling
	condStatus := metav1.ConditionTrue
	reason := "Aggregating"
	if phase == "Succeeded" {
		condType = tideprojectv1alpha1.ConditionSucceeded
		reason = "AllTasksSucceeded"
	} else if phase == "Failed" {
		condType = tideprojectv1alpha1.ConditionFailed
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

// taskToWaveMapper returns reconcile requests for all Waves whose Spec.PlanRef
// matches the changed Task's Spec.PlanRef. This drives WaveReconciler re-evaluation
// when any member Task's status changes.
func (r *WaveReconciler) taskToWaveMapper(ctx context.Context, obj client.Object) []reconcile.Request {
	task, ok := obj.(*tideprojectv1alpha1.Task)
	if !ok {
		return nil
	}
	if task.Spec.PlanRef == "" {
		return nil
	}
	var waveList tideprojectv1alpha1.WaveList
	if err := r.List(ctx, &waveList,
		client.InNamespace(task.Namespace),
	); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0)
	for _, w := range waveList.Items {
		if w.Spec.PlanRef == task.Spec.PlanRef {
			reqs = append(reqs, reconcile.Request{
				NamespacedName: client.ObjectKey{Namespace: w.Namespace, Name: w.Name},
			})
		}
	}
	return reqs
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
		For(&tideprojectv1alpha1.Wave{}).
		Watches(
			&tideprojectv1alpha1.Task{},
			handler.EnqueueRequestsFromMapFunc(r.taskToWaveMapper),
		).
		Watches(
			&tideprojectv1alpha1.Wave{},
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
