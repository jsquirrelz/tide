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
	"time"

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
	"github.com/jsquirrelz/tide/internal/pool"
)

const (
	projectFinalizer = "tideproject.k8s/project-cleanup"
	// finalizerCleanupTimeout bounds every finalizer cleanup callback (Pitfall 21).
	finalizerCleanupTimeout = 5 * time.Minute
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
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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
				logger.Info("project cleanup (no-op in Phase 1)", "name", project.Name)
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

	// 5. Phase 1: dispatcher seam nil-guarded for Phase 2 body fill (REQ-SUB-01).
	if r.Dispatcher != nil {
		// Phase 2 fills.
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

// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02 and a
// namespace-filter predicate per AUTH-02.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true // watch-all-namespaces mode
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Project{}).
		Owns(&batchv1.Job{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("project").
		Complete(r)
}
