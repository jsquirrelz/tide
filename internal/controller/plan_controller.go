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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	"github.com/jsquirrelz/tide/pkg/dag"
)

const planFinalizer = "tideproject.k8s/plan-cleanup"

// PlanReconciler reconciles a Plan object at Standard depth (D-C1).
// Plan is owned by Phase; the parent ref is set via internal/owner.EnsureOwnerRef.
type PlanReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	// PlannerPool — Plan reconcile dispatches planner-pool subagents in Phase 2.
	PlannerPool  *pool.Pool
	ExecutorPool *pool.Pool

	Dispatcher dispatch.Dispatcher

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=phases,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=waves,verbs=get;list;watch;create;update;patch;delete

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *PlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var plan tideprojectv1alpha1.Plan
	if err := r.Get(ctx, req.NamespacedName, &plan); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !plan.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &plan, planFinalizer,
			func(_ context.Context) error {
				logger.Info("plan cleanup", "name", plan.Name)
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&plan, planFinalizer) {
		controllerutil.AddFinalizer(&plan, planFinalizer)
		if err := r.Update(ctx, &plan); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure owner ref to parent Phase (CRD-02, Pitfall 23 prevention).
	// If the Phase is not found (e.g., Plan created before Phase, or Phase deleted),
	// log and continue — owner ref is best-effort; wave materialization must still proceed.
	if plan.Spec.PhaseRef != "" {
		var parent tideprojectv1alpha1.Phase
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			// Phase not found: log and continue without owner ref.
			logger.V(1).Info("parent Phase not found; skipping owner ref", "phaseRef", plan.Spec.PhaseRef)
		} else {
			if err := owner.EnsureOwnerRef(&plan, &parent, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Update(ctx, &plan); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 5. Phase 2 Wave materialization body inside the Dispatcher seam (REQ-SUB-01).
	if r.Dispatcher != nil {
		return r.reconcileWaveMaterialization(ctx, &plan)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonInitialized,
		Message:            "Plan scaffolded; dispatcher not wired",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &plan); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileWaveMaterialization implements the Wave materialization body inside the
// Dispatcher seam (step 5 of the six-step pattern).
//
// Per PERSIST-03: pkg/dag.ComputeWaves is called on EVERY reconcile — the schedule
// is re-derived from the current Task set, never cached in .status.
func (r *PlanReconciler) reconcileWaveMaterialization(ctx context.Context, plan *tideprojectv1alpha1.Plan) (ctrl.Result, error) {
	// Step 1: No-op until Plan is Validated by the admission webhook (Plan 11).
	if plan.Status.ValidationState != "Validated" {
		return ctrl.Result{}, nil
	}

	// Step 2: List Tasks via field-indexer .spec.planRef = plan.Name.
	var taskList tideprojectv1alpha1.TaskList
	if err := r.List(ctx, &taskList,
		client.InNamespace(plan.Namespace),
		client.MatchingFields{taskPlanRefIndexKey: plan.Name},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("list tasks for plan %s: %w", plan.Name, err)
	}

	// Build nodes + edges for ComputeWaves.
	nodes := make([]dag.NodeID, 0, len(taskList.Items))
	var edges []dag.Edge
	for _, t := range taskList.Items {
		nodes = append(nodes, t.Name)
		for _, dep := range t.Spec.DependsOn {
			edges = append(edges, dag.Edge{From: dep, To: t.Name})
		}
	}

	// Step 3: ComputeWaves on EVERY reconcile (PERSIST-03 — no cached schedule).
	layers, err := dag.ComputeWaves(nodes, edges)
	if err != nil {
		var cycleErr *dag.CycleError
		if errors.As(err, &cycleErr) {
			// Defense-in-depth: the Plan admission webhook should have caught this.
			patch := client.MergeFrom(plan.DeepCopy())
			plan.Status.Phase = "Failed"
			plan.Status.ValidationState = "CycleDetected"
			plan.Status.CycleEdges = cycleErr.InvolvedNodes
			meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionFailed,
				Status:             metav1.ConditionTrue,
				Reason:             tideprojectv1alpha1.ReasonCycleDetected,
				Message:            fmt.Sprintf("DAG cycle detected: %v", cycleErr.InvolvedNodes),
				LastTransitionTime: metav1.Now(),
			})
			if patchErr := r.Status().Patch(ctx, plan, patch); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("compute waves for plan %s: %w", plan.Name, err)
	}

	// Resolve the project name (for Task label stamping).
	projectName := r.resolveProjectName(ctx, plan)

	// Step 4+5: Materialize Waves and stamp Task labels.
	if err := r.materializeWaves(ctx, plan, taskList.Items, layers); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.stampTaskLabels(ctx, taskList.Items, layers, projectName); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// materializeWaves creates or gets one Wave per layer. Each Wave has a
// deterministic name tide-wave-{plan.UID}-{N} and is owned by the Plan.
// AlreadyExists is treated as success (idempotent on PERSIST-03 re-invocations).
func (r *PlanReconciler) materializeWaves(ctx context.Context, plan *tideprojectv1alpha1.Plan, _ []tideprojectv1alpha1.Task, layers [][]dag.NodeID) error {
	logger := logf.FromContext(ctx)
	for i := range layers {
		waveName := fmt.Sprintf("tide-wave-%s-%d", plan.UID, i)
		wave := &tideprojectv1alpha1.Wave{
			ObjectMeta: metav1.ObjectMeta{
				Name:      waveName,
				Namespace: plan.Namespace,
			},
			Spec: tideprojectv1alpha1.WaveSpec{
				PlanRef:   plan.Name,
				WaveIndex: i,
			},
		}

		// Check if Wave already exists.
		var existing tideprojectv1alpha1.Wave
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: waveName}, &existing); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("get wave %s: %w", waveName, err)
			}
			// Wave does not exist — set owner ref and create.
			if err := owner.EnsureOwnerRef(wave, plan, r.Scheme); err != nil {
				return fmt.Errorf("ensure owner ref wave %s: %w", waveName, err)
			}
			if err := r.Create(ctx, wave); err != nil {
				if !apierrors.IsAlreadyExists(err) {
					return fmt.Errorf("create wave %s: %w", waveName, err)
				}
				// AlreadyExists: idempotent success — watch-lag race (CR-01).
			}
			logger.Info("created wave", "wave", waveName, "index", i)
		} else {
			// Wave exists — ensure owner ref is set (may be missing on first reconcile
			// after a restart where the Wave was created but the Plan was not owner-set).
			if err := owner.EnsureOwnerRef(&existing, plan, r.Scheme); err == nil {
				// Patch if owner ref changed.
				_ = r.Update(ctx, &existing)
			}
		}
	}
	return nil
}

// stampTaskLabels patches each Task in layers[N] with:
//   - tideproject.k8s/wave-index=<N>
//   - tideproject.k8s/project=<projectName>
//
// These labels are the contract WaveReconciler and TaskReconciler depend on for
// fast lookups (RESEARCH.md Open Question #8).
func (r *PlanReconciler) stampTaskLabels(ctx context.Context, tasks []tideprojectv1alpha1.Task, layers [][]dag.NodeID, projectName string) error {
	// Build a name → layer-index map.
	taskLayer := make(map[string]int, len(tasks))
	for i, layer := range layers {
		for _, name := range layer {
			taskLayer[name] = i
		}
	}

	for i := range tasks {
		t := &tasks[i]
		layerIdx, ok := taskLayer[t.Name]
		if !ok {
			continue
		}
		waveIndexStr := fmt.Sprintf("%d", layerIdx)
		// Skip if labels are already correct.
		if t.Labels["tideproject.k8s/wave-index"] == waveIndexStr &&
			(projectName == "" || t.Labels["tideproject.k8s/project"] == projectName) {
			continue
		}
		patch := client.MergeFrom(t.DeepCopy())
		if t.Labels == nil {
			t.Labels = map[string]string{}
		}
		t.Labels["tideproject.k8s/wave-index"] = waveIndexStr
		if projectName != "" {
			t.Labels["tideproject.k8s/project"] = projectName
		}
		if err := r.Patch(ctx, t, patch); err != nil {
			return fmt.Errorf("stamp task labels on %s: %w", t.Name, err)
		}
	}
	return nil
}

// resolveProjectName returns the Project name for this Plan by walking the owner
// ref chain or listing Projects in the namespace. Returns empty string when no
// Project is found (graceful degradation — labels will be stamped on next reconcile).
func (r *PlanReconciler) resolveProjectName(ctx context.Context, plan *tideprojectv1alpha1.Plan) string {
	// Try to find a Project label on the Plan itself first.
	if name, ok := plan.Labels["tideproject.k8s/project"]; ok && name != "" {
		return name
	}
	// Fall back to listing projects in the namespace (Phase 2 fallback).
	var projectList tideprojectv1alpha1.ProjectList
	if err := r.List(ctx, &projectList, client.InNamespace(plan.Namespace)); err != nil {
		return ""
	}
	if len(projectList.Items) > 0 {
		return projectList.Items[0].Name
	}
	return ""
}

// SetupWithManager wires the watch with a namespace-filter predicate per AUTH-02.
// Note: WaveReconciler handles Wave→Plan re-enqueue; PlanReconciler uses Owns(&Wave{})
// so it is notified when owned Waves are created/updated.
func (r *PlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Plan{}).
		Owns(&tideprojectv1alpha1.Wave{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("plan").
		Complete(r)
}
