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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/gates"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
	"github.com/jsquirrelz/tide/pkg/dag"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

const planFinalizer = "tideproject.k8s/plan-cleanup"

// PlanReconciler reconciles a Plan object at Standard depth (D-C1).
// Plan is owned by Phase; the parent ref is set via internal/owner.EnsureOwnerRef.
type PlanReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	// PlannerPool — Plan reconcile dispatches planner-pool subagents.
	PlannerPool  *pool.Pool
	ExecutorPool *pool.Pool

	Dispatcher dispatch.Dispatcher

	// EnvReader reads EnvelopeOut from PVC after planner Job completes (Phase 3).
	EnvReader podjob.EnvelopeReader

	// SubagentImage is the planner subagent container image (Phase 3).
	SubagentImage string

	// HelmProviderDefaults carry Helm-chart provider/model defaults (Phase 3).
	HelmProviderDefaults ProviderDefaults

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

	// 5. Dispatcher seam (REQ-SUB-01). Phase 3 splits this:
	// 5a. Planner dispatch — fires when Plan has no Tasks yet (D-A2).
	// 5b. Wave materialization — Phase 2 logic; runs once Tasks exist and
	//     admission webhook stamps Validated.
	if r.Dispatcher != nil {
		res, dispatched, err := r.reconcilePlannerDispatch(ctx, &plan)
		if err != nil {
			return res, err
		}
		if dispatched {
			return res, nil
		}
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

// reconcilePlannerDispatch is the Phase 3 planner-dispatch step (D-A2)
// that runs BEFORE reconcileWaveMaterialization.
//
// Returns (result, dispatched, error):
//   - dispatched=true → the planner-dispatch branch took the reconcile and
//     reconcileWaveMaterialization MUST NOT run on this pass.
//   - dispatched=false → no planner work needed (Tasks already exist or no
//     Project resolvable); the caller should run reconcileWaveMaterialization.
//
// Dispatch is triggered when the Plan has no Tasks AND has not yet reached
// a terminal state. The planner Job has deterministic name
// tide-plan-<plan-uid>-1 (D-B5 dedup). On Job completion, child Task CRDs
// are server-side-created from EnvelopeOut.ChildCRDs; Wave creation is left
// to reconcileWaveMaterialization (Phase 2 path) which fires once the
// admission webhook stamps ValidationState="Validated" on the Plan.
func (r *PlanReconciler) reconcilePlannerDispatch(ctx context.Context, plan *tideprojectv1alpha1.Plan) (ctrl.Result, bool, error) {
	// If Tasks already exist for this Plan, skip planner dispatch — the
	// Phase 2 Wave path runs.
	var taskList tideprojectv1alpha1.TaskList
	if err := r.List(ctx, &taskList,
		client.InNamespace(plan.Namespace),
		client.MatchingFields{taskPlanRefIndexKey: plan.Name},
	); err != nil {
		return ctrl.Result{}, false, fmt.Errorf("list tasks for plan %s: %w", plan.Name, err)
	}
	if len(taskList.Items) > 0 {
		return ctrl.Result{}, false, nil
	}

	// Terminal short-circuit.
	if plan.Status.Phase == "Succeeded" || plan.Status.Phase == "Failed" {
		return ctrl.Result{}, true, nil
	}

	jobName := fmt.Sprintf("tide-plan-%s-1", plan.UID)

	// On Running: check Job terminal state.
	if plan.Status.Phase == "Running" {
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, true, err
			}
			return ctrl.Result{}, true, nil
		}
		if isJobTerminal(&job) {
			res, err := r.handlePlannerJobCompletion(ctx, plan, &job)
			return res, true, err
		}
		return ctrl.Result{}, true, nil
	}

	// Acquire plannerPool (POOL-01) before Job creation (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, true, err
		}
		defer r.PlannerPool.Release()
	}

	project := r.resolveProjectForPlan(ctx, plan)
	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 20}
	_, envInJSON, err := BuildPlannerEnvelope("plan", plan, project, 1, "", caps, "https://127.0.0.1:8443", r.HelmProviderDefaults)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("build planner envelope: %w", err)
	}
	_ = envInJSON

	job := r.buildPlanPlannerJob(plan, jobName)
	if err := owner.EnsureOwnerRef(job, plan, r.Scheme); err != nil {
		return ctrl.Result{}, true, fmt.Errorf("ensure owner ref on planner job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, true, fmt.Errorf("create planner job: %w", err)
		}
		// AlreadyExists: idempotent success.
	}

	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.Phase = "Running"
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionAuthoringPlanner,
		Status:             metav1.ConditionTrue,
		Reason:             "PlannerDispatched",
		Message:            fmt.Sprintf("Planner Job %s dispatched", jobName),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, true, err
	}

	return ctrl.Result{}, true, nil
}

// handlePlannerJobCompletion materializes Task child CRDs from
// EnvelopeOut.ChildCRDs and clears the Running phase so the Phase 2 Wave
// path can pick up on the next reconcile.
//
// Note: This does NOT create Waves — the existing reconcileWaveMaterialization
// handles that once the admission webhook stamps ValidationState=Validated.
func (r *PlanReconciler) handlePlannerJobCompletion(ctx context.Context, plan *tideprojectv1alpha1.Plan, _ *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	project := r.resolveProjectForPlan(ctx, plan)
	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	if r.EnvReader == nil {
		logger.Info("no env reader; clearing Running phase to let Wave path proceed")
		patch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = ""
		_ = r.Status().Patch(ctx, plan, patch)
		return ctrl.Result{}, nil
	}

	envOut, err := r.EnvReader.ReadOut(ctx, projectUID, string(plan.UID))
	if err != nil {
		patch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = "Failed"
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "EnvelopeReadFailed",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		if pErr := r.Status().Patch(ctx, plan, patch); pErr != nil {
			return ctrl.Result{}, pErr
		}
		return ctrl.Result{}, nil
	}

	if len(envOut.ChildCRDs) > 0 {
		if mErr := MaterializeChildCRDs(ctx, r.Client, r.Scheme, plan, envOut.ChildCRDs); mErr != nil {
			patch := client.MergeFrom(plan.DeepCopy())
			plan.Status.Phase = "Failed"
			meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionFailed,
				Status:             metav1.ConditionTrue,
				Reason:             "ChildCRDMaterializationFailed",
				Message:            mErr.Error(),
				LastTransitionTime: metav1.Now(),
			})
			if pErr := r.Status().Patch(ctx, plan, patch); pErr != nil {
				return ctrl.Result{}, pErr
			}
			return ctrl.Result{}, nil
		}
	}

	// Plan 04-05: gate-policy hook (level=plan). Lands BEFORE clearing the
	// Running phase so the gate stays the "exit gate" of the planner cycle.
	if project != nil && gates.CheckRejected(project) {
		return r.patchPlanFailed(ctx, plan, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
	}
	if project != nil {
		policy := gates.EvaluatePolicy(project.Spec.Gates, "plan")
		if policy == gates.PolicyApprove || policy == gates.PolicyPause {
			if !gates.CheckApprove(plan, "plan") {
				return r.patchPlanAwaitingApproval(ctx, plan, policy)
			}
			newAnno := gates.ConsumeApprove(plan, "plan")
			patch := client.MergeFrom(plan.DeepCopy())
			plan.SetAnnotations(newAnno)
			if err := r.Patch(ctx, plan, patch); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// Clear Running phase so the Phase 2 Wave path takes over on next reconcile.
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.Phase = ""
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             tideprojectv1alpha1.ReasonResumedByUser,
		Message:            "Plan resumed from gate boundary",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// patchPlanFailed sets Plan.Status.Phase=Failed with the given reason/message.
// Used by the Plan 04-05 gate-policy hook (reject short-circuit).
func (r *PlanReconciler) patchPlanFailed(ctx context.Context, plan *tideprojectv1alpha1.Plan, reason, message string) (ctrl.Result, error) {
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.Phase = "Failed"
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionFailed,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// patchPlanAwaitingApproval parks the Plan at Status.Phase=AwaitingApproval
// per Plan 04-05 gate seam (T-04-G4 mitigation — no requeue).
func (r *PlanReconciler) patchPlanAwaitingApproval(ctx context.Context, plan *tideprojectv1alpha1.Plan, policy tideprojectv1alpha1.GatePolicy) (ctrl.Result, error) {
	reason := tideprojectv1alpha1.ReasonAwaitingApproval
	message := "Plan awaiting operator approve annotation (tideproject.k8s/approve-plan=true)"
	if policy == gates.PolicyPause {
		reason = tideprojectv1alpha1.ReasonPausedAtBoundary
		message = "Plan paused at boundary; requires explicit resume"
	}
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.Phase = "AwaitingApproval"
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// resolveProjectForPlan walks Plan → Phase → Milestone → Project.
func (r *PlanReconciler) resolveProjectForPlan(ctx context.Context, plan *tideprojectv1alpha1.Plan) *tideprojectv1alpha1.Project {
	if plan.Spec.PhaseRef == "" {
		return nil
	}
	var ph tideprojectv1alpha1.Phase
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &ph); err != nil {
		return nil
	}
	if ph.Spec.MilestoneRef == "" {
		return nil
	}
	var ms tideprojectv1alpha1.Milestone
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		return nil
	}
	if ms.Spec.ProjectRef == "" {
		return nil
	}
	var p tideprojectv1alpha1.Project
	if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return nil
	}
	return &p
}

func (r *PlanReconciler) buildPlanPlannerJob(plan *tideprojectv1alpha1.Plan, jobName string) *batchv1.Job {
	backoffLimit := int32(0)
	ttl := int32(300)
	image := r.SubagentImage
	if image == "" {
		image = "ghcr.io/jsquirrelz/tide-stub-subagent:test"
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: plan.Namespace,
			Labels: map[string]string{
				"tideproject.k8s/plan-uid": string(plan.UID),
				"tideproject.k8s/level":    "plan",
				"tideproject.k8s/role":     "planner",
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttl,
			Template: batchv1Template(jobName, image),
		},
	}
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

	// Plan 04-05 Task 2: PauseBetweenWaves hook. After labels are stamped, check
	// whether the wave boundary requires operator approval before wave N+1 can
	// dispatch. The actual block on Task dispatch lands via the
	// tideproject.k8s/wave-paused label that TaskReconciler honors.
	return r.maybePauseForWaveApprove(ctx, plan, taskList.Items, layers)
}

// planWaveApprovedLabelPrefix is stamped on the Plan itself by
// maybePauseForWaveApprove when an approve-wave-N annotation is consumed.
// Its presence signals "this wave has been approved" so subsequent
// reconciles do not re-pause at the same boundary while wave N tasks are
// still mid-flight (Plan 04-05 — wave-approval is persistent until all
// tasks in the wave complete).
const planWaveApprovedLabelPrefix = "tideproject.k8s/wave-approved-"

// maybePauseForWaveApprove implements the PauseBetweenWaves boundary check
// per Plan 04-05 (D-G3). When `Project.Spec.Gates.PauseBetweenWaves` is true,
// the function:
//
//  1. Determines the smallest wave index N where wave N-1 is fully Succeeded
//     but at least one task in wave N has not yet Succeeded.
//  2. If the Plan already carries label tideproject.k8s/wave-approved-<N>
//     (set on a prior reconcile after annotation consume), skip pausing —
//     this wave is mid-flight and the operator already approved it.
//  3. If gates.CheckWaveApprove(plan, N) is true: consume the annotation (one-
//     shot, T-04-G2 mitigation), stamp the persistent wave-approved-<N> label
//     on the Plan, clear the wave-paused labels for wave N, and flip the
//     Condition to False (Reason=ResumedByUser).
//  4. Otherwise (no approval, no prior approval label): stamp the
//     tideproject.k8s/wave-paused: "<N>" label on every Task in wave N (the
//     block the TaskReconciler honors) and set Plan's Condition
//     WaveOrLevelPaused True (Reason=PausedAtBoundary, Message referencing N).
//
// When PauseBetweenWaves is false the function is a no-op (today's behavior).
func (r *PlanReconciler) maybePauseForWaveApprove(ctx context.Context, plan *tideprojectv1alpha1.Plan, tasks []tideprojectv1alpha1.Task, layers [][]dag.NodeID) (ctrl.Result, error) {
	project := r.resolveProjectForPlan(ctx, plan)
	if project == nil || !project.Spec.Gates.PauseBetweenWaves {
		return ctrl.Result{}, nil
	}

	// Index tasks by name for status lookup.
	taskByName := make(map[string]*tideprojectv1alpha1.Task, len(tasks))
	for i := range tasks {
		taskByName[tasks[i].Name] = &tasks[i]
	}

	// Find pending boundary: smallest N where wave N-1 is fully Succeeded AND
	// wave N has at least one non-Succeeded task.
	pendingWave := -1
	for n := 1; n < len(layers); n++ {
		prevAllSucceeded := true
		for _, name := range layers[n-1] {
			t := taskByName[name]
			if t == nil || t.Status.Phase != "Succeeded" {
				prevAllSucceeded = false
				break
			}
		}
		if !prevAllSucceeded {
			continue
		}
		anyPending := false
		for _, name := range layers[n] {
			t := taskByName[name]
			if t == nil || t.Status.Phase != "Succeeded" {
				anyPending = true
				break
			}
		}
		if anyPending {
			pendingWave = n
			break
		}
	}

	if pendingWave < 0 {
		return ctrl.Result{}, nil
	}

	approvedLabelKey := fmt.Sprintf("%s%d", planWaveApprovedLabelPrefix, pendingWave)

	// Prior-approval short-circuit: if we already marked this wave approved,
	// skip — tasks are mid-flight and we must not re-pause.
	if plan.Labels[approvedLabelKey] == "true" {
		return ctrl.Result{}, nil
	}

	if gates.CheckWaveApprove(plan, pendingWave) {
		// Consume the annotation (one-shot, T-04-G2) AND stamp the persistent
		// wave-approved label on the Plan in a single Patch.
		newAnno := gates.ConsumeWaveApprove(plan, pendingWave)
		patch := client.MergeFrom(plan.DeepCopy())
		plan.SetAnnotations(newAnno)
		if plan.Labels == nil {
			plan.Labels = map[string]string{}
		}
		plan.Labels[approvedLabelKey] = "true"
		if err := r.Patch(ctx, plan, patch); err != nil {
			return ctrl.Result{}, err
		}
		// Remove wave-paused labels from tasks in this wave (unblock TaskReconciler).
		for _, name := range layers[pendingWave] {
			t := taskByName[name]
			if t == nil || t.Labels == nil {
				continue
			}
			if _, has := t.Labels["tideproject.k8s/wave-paused"]; !has {
				continue
			}
			tPatch := client.MergeFrom(t.DeepCopy())
			delete(t.Labels, "tideproject.k8s/wave-paused")
			if err := r.Patch(ctx, t, tPatch); err != nil {
				return ctrl.Result{}, fmt.Errorf("clear wave-paused on task %s: %w", t.Name, err)
			}
		}
		// Flip Plan Condition to False (resumed).
		statusPatch := client.MergeFrom(plan.DeepCopy())
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
			Status:             metav1.ConditionFalse,
			Reason:             tideprojectv1alpha1.ReasonResumedByUser,
			Message:            fmt.Sprintf("Wave %d approved; dispatch proceeding", pendingWave),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, plan, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Stamp wave-paused label on every task in this wave (block dispatch).
	waveLabel := fmt.Sprintf("%d", pendingWave)
	for _, name := range layers[pendingWave] {
		t := taskByName[name]
		if t == nil {
			continue
		}
		if t.Labels["tideproject.k8s/wave-paused"] == waveLabel {
			continue
		}
		tPatch := client.MergeFrom(t.DeepCopy())
		if t.Labels == nil {
			t.Labels = map[string]string{}
		}
		t.Labels["tideproject.k8s/wave-paused"] = waveLabel
		if err := r.Patch(ctx, t, tPatch); err != nil {
			return ctrl.Result{}, fmt.Errorf("stamp wave-paused on task %s: %w", t.Name, err)
		}
	}

	statusPatch := client.MergeFrom(plan.DeepCopy())
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonPausedAtBoundary,
		Message:            fmt.Sprintf("Awaiting approval for wave %d (annotate %s%d=true on this Plan)", pendingWave, gates.AnnotationApproveWavePrefix, pendingWave),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, statusPatch); err != nil {
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
// so it is notified when owned Waves are created/updated. Plan 04-05 also wires
// AnnotationChangedPredicate so approve-plan / approve-wave-N annotation writes
// trigger reconciliation (T-04-G4 mitigation).
func (r *PlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Plan{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			)),
		).
		Owns(&tideprojectv1alpha1.Wave{}).
		Owns(&tideprojectv1alpha1.Task{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("plan").
		Complete(r)
}
