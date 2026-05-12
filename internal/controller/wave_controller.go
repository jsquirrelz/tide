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

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
)

const waveFinalizer = "tideproject.k8s/wave-cleanup"

// WaveReconciler reconciles a Wave object at Standard depth (D-C1).
// Wave is owned by Plan; the parent ref is set via internal/owner.EnsureOwnerRef.
// Per D-B1, Waves are produced exclusively by this reconciler (no human-applied Waves).
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
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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
				logger.Info("wave cleanup (no-op in Phase 1)", "name", wave.Name)
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
	if wave.Spec.PlanRef != "" {
		var parent tideprojectv1alpha1.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: wave.Namespace, Name: wave.Spec.PlanRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) == nil {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
		if err := owner.EnsureOwnerRef(&wave, &parent, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Update(ctx, &wave); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 5. Phase 1: dispatcher seam nil-guarded for Phase 2 body fill (REQ-SUB-01).
	if r.Dispatcher != nil {
		// Phase 2 fills.
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&wave.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonInitialized,
		Message:            "Wave scaffolded; awaiting dispatch logic (Phase 2)",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &wave); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02 and a
// namespace-filter predicate per AUTH-02.
func (r *WaveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Wave{}).
		Owns(&batchv1.Job{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("wave").
		Complete(r)
}
