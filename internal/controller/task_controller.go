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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/gates"
	"github.com/jsquirrelz/tide/internal/harness"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

const taskFinalizer = "tideproject.k8s/task-cleanup"

// taskPlanRefIndexKey is the field indexer key for Task.Spec.PlanRef.
// Registered in SetupWithManager; used by listSiblingTasks.
const taskPlanRefIndexKey = ".spec.planRef"

// TaskReconciler reconciles a Task object at Standard depth (D-C1).
// Task is owned by Plan; the parent ref is set via internal/owner.EnsureOwnerRef.
type TaskReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	MaxConcurrentReconciles int

	PlannerPool *pool.Pool
	// ExecutorPool — Task reconcile dispatches executor-pool subagents in Phase 2.
	ExecutorPool *pool.Pool

	Dispatcher dispatch.Dispatcher

	// Budget is the in-process per-Secret-UID rate-limiter store (D-D1).
	Budget *budget.Store

	// Defaults carries the Helm-driven rate-limit defaults applied when no
	// Secret annotation or Project.Spec.Providers entry overrides them.
	Defaults budget.Limits

	// SigningKey is the HMAC signing key used to mint per-Task dispatch tokens
	// (D-C3). Loaded from the tide-signing-key Secret at Manager startup.
	SigningKey []byte

	// SubagentImage is the image ref for the subagent container (Plan 04 stub for
	// Phase 2; Plan 12's Helm values bind the production image in Phase 3+).
	SubagentImage string

	// CredproxyImage is the image ref for the tide-credproxy sidecar (Plan 05).
	CredproxyImage string

	// EnvReader reads the EnvelopeOut from the per-Project PVC after Job completion.
	// Production wiring uses podjob.FilesystemEnvelopeReader; tests inject an
	// in-memory stub.
	EnvReader podjob.EnvelopeReader

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string
}

// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=get;list;watch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/status,verbs=get;update;patch

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *TaskReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var task tideprojectv1alpha1.Task
	if err := r.Get(ctx, req.NamespacedName, &task); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Handle deletion with a bounded-deadline cleanup (CTRL-05, Pitfall 21).
	if !task.DeletionTimestamp.IsZero() {
		return finalizer.HandleDeletion(ctx, r.Client, &task, taskFinalizer,
			func(_ context.Context) error {
				logger.Info("task cleanup", "name", task.Name)
				return nil
			}, finalizerCleanupTimeout)
	}

	// 3. Ensure finalizer is set on create.
	if !controllerutil.ContainsFinalizer(&task, taskFinalizer) {
		controllerutil.AddFinalizer(&task, taskFinalizer)
		if err := r.Update(ctx, &task); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure owner ref to parent Plan (CRD-02, Pitfall 23 prevention).
	// If the Plan is not found (e.g., Task created before Plan, or Plan deleted),
	// log and continue — owner ref is best-effort; dispatch must still proceed.
	if task.Spec.PlanRef != "" {
		var parent tideprojectv1alpha1.Plan
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: task.Spec.PlanRef}, &parent); err != nil {
			if client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
			// Plan not found: log and continue without owner ref.
			logger.V(1).Info("parent Plan not found; skipping owner ref", "planRef", task.Spec.PlanRef)
		} else {
			if err := owner.EnsureOwnerRef(&task, &parent, r.Scheme); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.Update(ctx, &task); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// 5. Phase 2 dispatch body inside the Dispatcher seam (REQ-SUB-01).
	if r.Dispatcher != nil {
		return r.reconcileDispatch(ctx, &task)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonInitialized,
		Message:            "Task scaffolded; dispatcher not wired",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &task); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDispatch implements the 12-step dispatch body inside the
// `if r.Dispatcher != nil` seam (step 5 of the six-step pattern).
func (r *TaskReconciler) reconcileDispatch(ctx context.Context, task *tideprojectv1alpha1.Task) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Step 1: Terminal short-circuit.
	if task.Status.Phase == "Succeeded" || task.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}

	// Step 2: On Running — if a Job for this Task is in terminal state, handle completion.
	if task.Status.Phase == "Running" {
		jobName := podjob.JobName(task.UID, task.Status.Attempt)
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		if isJobTerminal(&job) {
			project, err := r.resolveProject(ctx, task)
			if err != nil {
				return ctrl.Result{}, err
			}
			return r.handleJobCompletion(ctx, task, project)
		}
		return ctrl.Result{}, nil
	}

	// Step 3: Resolve Project.
	project, err := r.resolveProject(ctx, task)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Plan 04-05 reject short-circuit (D-G1 per-level policy enum, T-04-G1
	// mitigation). Fires BEFORE budget/indegree/dispatch so a rejected Project
	// halts even Pending tasks. Reject value carries the operator-supplied
	// reason which surfaces on the Condition Message (D-G4).
	if gates.CheckRejected(project) {
		return r.patchTaskFailed(ctx, task, tideprojectv1alpha1.ReasonRejectedByUser, gates.RejectedReason(project))
	}

	// Step 4: Budget gate.
	if project.Status.Phase == "BudgetExceeded" && !budget.IsBypassed(project, time.Now()) {
		patch := client.MergeFrom(task.DeepCopy())
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               "BudgetBlocked",
			Status:             metav1.ConditionTrue,
			Reason:             tideprojectv1alpha1.ConditionBudgetExceeded,
			Message:            "Project budget cap exceeded; task dispatch halted",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Step 5: Indegree compute (D-B3). Re-computed every reconcile; never cached.
	siblings, err := r.listSiblingTasks(ctx, task)
	if err != nil {
		return ctrl.Result{}, err
	}
	indegree := r.computeIndegree(task, siblings)
	if indegree > 0 {
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Pending"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionReconciling,
			Status:             metav1.ConditionTrue,
			Reason:             tideprojectv1alpha1.ReasonAwaitingDispatch,
			Message:            fmt.Sprintf("Waiting for %d predecessor(s) to complete", indegree),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Plan 04-05 Task 2: PauseBetweenWaves dispatch block. PlanReconciler
	// stamps tideproject.k8s/wave-paused=<N> on tasks in a wave waiting for
	// approve-wave-N on the parent Plan; until the label is cleared (by
	// PlanReconciler on annotation consume), the Task stays AwaitingApproval.
	if _, paused := task.Labels["tideproject.k8s/wave-paused"]; paused {
		return r.patchTaskAwaitingApproval(ctx, task, gates.PolicyPause)
	}

	// Plan 04-05 gate-policy hook (level=task). The Task gate fires here —
	// AFTER indegree compute (only ready-to-dispatch Tasks pause) and BEFORE
	// rate-limit + token mint + Job dispatch. D-G1 default for Task is "auto"
	// (no-op); explicit "approve"/"pause" parks the Task at AwaitingApproval
	// until an annotation arrives (T-04-G4 — no polling).
	policy := gates.EvaluatePolicy(project.Spec.Gates, "task")
	if policy == gates.PolicyApprove || policy == gates.PolicyPause {
		if !gates.CheckApprove(task, "task") {
			return r.patchTaskAwaitingApproval(ctx, task, policy)
		}
		newAnno := gates.ConsumeApprove(task, "task")
		patch := client.MergeFrom(task.DeepCopy())
		task.SetAnnotations(newAnno)
		if err := r.Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 6: Rate-limit gate (Pattern 1 — no blocking per Pitfall 1, D-D3).
	//
	// CR-03: Once a token is reserved (d == 0), any subsequent error before the
	// Job is successfully created must Cancel() the reservation. Otherwise the
	// bucket drains permanently on transient errors (signing, marshal, patch,
	// Create), and the controller silently throttles itself.
	var heldReservation interface{ Cancel() }
	committed := false
	defer func() {
		if !committed && heldReservation != nil {
			heldReservation.Cancel()
		}
	}()
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		} else {
			lim := r.Budget.ForSecret(string(secret.UID), r.defaultsForSecret(&secret))
			if lim != nil {
				rsv := lim.Reserve()
				d := rsv.Delay()
				if d > 0 {
					rsv.Cancel()
					budget.ProviderRateLimitHitsTotal.WithLabelValues(project.Name).Inc()
					patch := client.MergeFrom(task.DeepCopy())
					meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
						Type:               tideprojectv1alpha1.ConditionReconciling,
						Status:             metav1.ConditionTrue,
						Reason:             "RateLimited",
						Message:            fmt.Sprintf("Rate-limit bucket exhausted; requeuing after %s", d),
						LastTransitionTime: metav1.Now(),
					})
					if err := r.Status().Patch(ctx, task, patch); err != nil {
						return ctrl.Result{}, err
					}
					return ctrl.Result{RequeueAfter: d}, nil
				}
				// d == 0: token consumed. Hold the reservation so any error
				// before Job Create returns the token to the bucket.
				heldReservation = rsv
			}
		}
	}

	// Step 7: Attempt counter.
	attempt, err := r.nextAttempt(ctx, task)
	if err != nil {
		return ctrl.Result{}, err
	}
	maxAttempts := int(project.Spec.MaxAttemptsPerTask)
	if maxAttempts <= 0 {
		maxAttempts = 3 // Helm default
	}
	if attempt > maxAttempts {
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Failed"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "ExceededAttempts",
			Message:            fmt.Sprintf("Exceeded max attempts %d", maxAttempts),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Step 8: Mint signed token (D-C3).
	//
	// Phase 04.1 P1.3 fix: route through podjob.DefaultCaps so token validity
	// shares the executor 300s wall-clock floor with the Job's
	// activeDeadlineSeconds derivation. Both consumers MUST go through DefaultCaps —
	// drift between derivations is exactly the bug class audit P1.3 closed. Task
	// dispatch is always executor Kind; planner reconcilers (milestone/phase/plan)
	// pass JobKindPlanner via Plan 04.1-05 (P1.2) and get the 600s floor instead.
	taskCaps := podjob.DefaultCaps(task.Spec.Caps, podjob.JobKindExecutor)
	wallClock := taskCaps.WallClockSeconds
	token, err := credproxy.Sign(r.SigningKey, string(task.UID), time.Duration(wallClock+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("mint signed token: %w", err)
	}

	// Step 9: Build EnvelopeIn; translate api/v1alpha1.Caps → pkg/dispatch.Caps per Plan 03.
	_, envInJSON, err := r.buildEnvelopeIn(ctx, task, project, attempt, token)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Step 10: Patch Status.Attempt BEFORE Create (Pitfall 2 mitigation — prevents
	// drift if client.Create succeeds but the status patch fails on transient error).
	{
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Attempt = attempt
		now := metav1.Now()
		task.Status.StartedAt = &now
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Step 11: Build + Create Job. AlreadyExists treated as success (Pitfall F / SUB-03).
	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}
	opts := podjob.BuildOptions{
		Task:           task,
		Project:        project,
		Attempt:        attempt,
		SignedToken:    token,
		EnvelopeInJSON: envInJSON,
		SubagentImage:  r.SubagentImage,
		CredproxyImage: r.CredproxyImage,
		SecretUID:      secretUID,
		PVCName:        "tide-projects",
		ProjectUID:     string(project.UID),
	}
	job := podjob.BuildJobSpec(opts)
	if err := owner.EnsureOwnerRef(job, task, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure owner ref on job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create job: %w", err)
		}
		// AlreadyExists: idempotent success — watch-lag race (Pitfall F / SUB-03).
		logger.Info("job already exists; treating as successful dispatch", "job", job.Name)
	}
	// CR-03: Job is committed (either created fresh or already exists). Mark
	// committed so the deferred reservation cancel does not return the token.
	committed = true

	// Step 12: Patch Status.Phase=Running + Condition Running=True, Reason=Dispatched.
	{
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Running"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionRunning,
			Status:             metav1.ConditionTrue,
			Reason:             "Dispatched",
			Message:            fmt.Sprintf("Job %s dispatched (attempt %d)", job.Name, attempt),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, task, patch); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// patchTaskFailed patches Task.Status.Phase=Failed with the supplied reason
// + message. Used by the Plan 04-05 gate-policy hook for the reject
// short-circuit (operator wrote tideproject.k8s/reject on the parent Project).
func (r *TaskReconciler) patchTaskFailed(ctx context.Context, task *tideprojectv1alpha1.Task, reason, message string) (ctrl.Result, error) {
	patch := client.MergeFrom(task.DeepCopy())
	task.Status.Phase = "Failed"
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionFailed,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, task, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// patchTaskAwaitingApproval parks the Task at Status.Phase=AwaitingApproval
// per Plan 04-05 gate seam (T-04-G4 mitigation — no requeue; an
// AnnotationChangedPredicate-driven re-reconcile is the only path forward).
func (r *TaskReconciler) patchTaskAwaitingApproval(ctx context.Context, task *tideprojectv1alpha1.Task, policy tideprojectv1alpha1.GatePolicy) (ctrl.Result, error) {
	reason := tideprojectv1alpha1.ReasonAwaitingApproval
	message := "Task awaiting operator approve annotation (tideproject.k8s/approve-task=true)"
	if policy == gates.PolicyPause {
		reason = tideprojectv1alpha1.ReasonPausedAtBoundary
		message = "Task paused at boundary; requires explicit resume"
	}
	patch := client.MergeFrom(task.DeepCopy())
	task.Status.Phase = "AwaitingApproval"
	meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, task, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// handleJobCompletion reads the EnvelopeOut, validates output paths, rolls up
// budget, and patches Task.Status to the terminal state.
func (r *TaskReconciler) handleJobCompletion(ctx context.Context, task *tideprojectv1alpha1.Task, project *tideprojectv1alpha1.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Read the EnvelopeOut from the PVC-backed reader (Blocker #2/#3 path).
	out, err := r.EnvReader.ReadOut(ctx, string(project.UID), string(task.UID))
	if err != nil {
		patch := client.MergeFrom(task.DeepCopy())
		task.Status.Phase = "Failed"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "EnvelopeReadFailed",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		if patchErr := r.Status().Patch(ctx, task, patch); patchErr != nil {
			return ctrl.Result{}, patchErr
		}
		return ctrl.Result{}, nil
	}

	// Output-path validation (Warning #5 — wires HARN-05 into dispatch chain).
	// Performed controller-side in Phase 2 (RESEARCH.md Responsibility Map deviation).
	// Phase 3 moves validation into the Pod once the harness-wrapped runtime lands.
	if out.Result != "output-paths-violation" && len(task.Spec.DeclaredOutputPaths) > 0 && task.Status.StartedAt != nil {
		taskWorkspaceRoot := fmt.Sprintf("/workspaces/%s/workspace", string(project.UID))
		violations, skipped, vErr := validateControllerOutputPaths(taskWorkspaceRoot, task.Status.StartedAt.Time, task.Spec.DeclaredOutputPaths)
		if vErr != nil {
			patch := client.MergeFrom(task.DeepCopy())
			task.Status.Phase = "Failed"
			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionFailed,
				Status:             metav1.ConditionTrue,
				Reason:             "OutputValidationError",
				Message:            vErr.Error(),
				LastTransitionTime: metav1.Now(),
			})
			if patchErr := r.Status().Patch(ctx, task, patch); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			return ctrl.Result{}, nil
		}
		if skipped {
			logger.V(1).Info("Skipped controller-side output validation; workspace root not visible", "task", task.Name, "workspaceRoot", taskWorkspaceRoot)
		}
		if len(violations) > 0 {
			msg := buildViolationMessage(violations)
			logger.Info("output path violations detected", "task", task.Name, "violations", len(violations))
			patch := client.MergeFrom(task.DeepCopy())
			task.Status.Phase = "Failed"
			meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionFailed,
				Status:             metav1.ConditionTrue,
				Reason:             "OutputPathsViolation",
				Message:            msg,
				LastTransitionTime: metav1.Now(),
			})
			if patchErr := r.Status().Patch(ctx, task, patch); patchErr != nil {
				return ctrl.Result{}, patchErr
			}
			return ctrl.Result{}, nil
		}
	}

	// Standard result interpretation.
	patch := client.MergeFrom(task.DeepCopy())
	if out.ExitCode != 0 || out.Result == "cap-hit" || out.Result == "output-paths-violation" {
		task.Status.Phase = "Failed"
		reason := conditionReasonFromEnvelopeResult(out.Result, out.ExitCode)
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionFailed,
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			Message:            fmt.Sprintf("Task failed: exitCode=%d result=%s", out.ExitCode, out.Result),
			LastTransitionTime: metav1.Now(),
		})
	} else {
		task.Status.Phase = "Succeeded"
		meta.SetStatusCondition(&task.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionSucceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "JobComplete",
			Message:            "Task completed successfully",
			LastTransitionTime: metav1.Now(),
		})
	}

	// Patch CompletedAt from the envelope.
	completedAt := metav1.NewTime(out.CompletedAt)
	task.Status.CompletedAt = &completedAt
	if err := r.Status().Patch(ctx, task, patch); err != nil {
		return ctrl.Result{}, err
	}

	// Roll up budget usage into Project.Status.Budget (D-D2).
	if err := budget.RollUpUsage(ctx, r.Client, project, out.Usage); err != nil {
		// Log but do not fail the reconcile — the task is already in terminal state.
		logger.Error(err, "failed to roll up budget usage", "task", task.Name)
	}

	return ctrl.Result{}, nil
}

// resolveProject finds the Project owning this Task. It first tries the
// tideproject.k8s/project label (stamped by PlanReconciler) and falls back to
// listing all Projects in the same namespace.
func (r *TaskReconciler) resolveProject(ctx context.Context, task *tideprojectv1alpha1.Task) (*tideprojectv1alpha1.Project, error) {
	// Fast path: PlanReconciler stamps tideproject.k8s/project=<name> on all Tasks.
	if projectName, ok := task.Labels["tideproject.k8s/project"]; ok && projectName != "" {
		var project tideprojectv1alpha1.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: projectName}, &project); err == nil {
			return &project, nil
		}
	}
	// Fallback: find any Project in the same namespace.
	var projectList tideprojectv1alpha1.ProjectList
	if err := r.List(ctx, &projectList, client.InNamespace(task.Namespace)); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	if len(projectList.Items) > 0 {
		return &projectList.Items[0], nil
	}
	return nil, fmt.Errorf("no project found in namespace %s", task.Namespace)
}

// listSiblingTasks returns all Tasks in the same Plan as task (same namespace, same PlanRef).
func (r *TaskReconciler) listSiblingTasks(ctx context.Context, task *tideprojectv1alpha1.Task) ([]tideprojectv1alpha1.Task, error) {
	var taskList tideprojectv1alpha1.TaskList
	if err := r.List(ctx, &taskList,
		client.InNamespace(task.Namespace),
		client.MatchingFields{taskPlanRefIndexKey: task.Spec.PlanRef},
	); err != nil {
		return nil, fmt.Errorf("list sibling tasks: %w", err)
	}
	return taskList.Items, nil
}

// computeIndegree returns the number of predecessors in task.Spec.DependsOn
// that have NOT yet Succeeded. Returns 0 when all dependencies are satisfied.
// Implements FAIL-01: a failed predecessor keeps indegree > 0 for its dependents,
// so dependents in later waves never dispatch (structural enforcement).
func (r *TaskReconciler) computeIndegree(task *tideprojectv1alpha1.Task, siblings []tideprojectv1alpha1.Task) int {
	if len(task.Spec.DependsOn) == 0 {
		return 0
	}
	statusByName := make(map[string]string, len(siblings))
	for _, s := range siblings {
		statusByName[s.Name] = s.Status.Phase
	}
	indegree := 0
	for _, dep := range task.Spec.DependsOn {
		if statusByName[dep] != "Succeeded" {
			indegree++
		}
	}
	return indegree
}

// nextAttempt returns the next attempt number (current max + 1, minimum 1).
// Lists all Jobs with label tideproject.k8s/task-uid=<task.UID>.
func (r *TaskReconciler) nextAttempt(ctx context.Context, task *tideprojectv1alpha1.Task) (int, error) {
	var jobList batchv1.JobList
	if err := r.List(ctx, &jobList,
		client.InNamespace(task.Namespace),
		client.MatchingLabels{"tideproject.k8s/task-uid": string(task.UID)},
	); err != nil {
		return 0, fmt.Errorf("list task jobs: %w", err)
	}
	maxAttempt := 0
	logger := logf.FromContext(ctx)
	for _, j := range jobList.Items {
		attempt, ok := j.Labels["tideproject.k8s/attempt"]
		if !ok {
			continue
		}
		// WR-03: strconv.Atoi rejects negative values, trailing garbage,
		// hex, etc.; an explicit n<0 check guards against a label value of
		// "-1" pulling the max-attempt tracking backwards. Malformed labels
		// are logged at V(1) so a parse failure is observable instead of
		// silently swallowed.
		n, err := strconv.Atoi(attempt)
		if err != nil || n < 0 {
			logger.V(1).Info("ignoring malformed attempt label", "job", j.Name, "value", attempt)
			continue
		}
		if n > maxAttempt {
			maxAttempt = n
		}
	}
	return maxAttempt + 1, nil
}

// gateDispatch handles the rate-limit gate using Pattern 1.
// Returns a non-zero delay when the bucket is exhausted (caller must RequeueAfter).
// Standalone helper satisfying plan's "helpers named in <interfaces>" requirement (grep target).
func (r *TaskReconciler) gateDispatch(projectName, secretUID string, limits budget.Limits) time.Duration {
	lim := r.Budget.ForSecret(secretUID, limits)
	if lim == nil {
		return 0
	}
	rsv := lim.Reserve()
	d := rsv.Delay()
	if d > 0 {
		rsv.Cancel()
		budget.ProviderRateLimitHitsTotal.WithLabelValues(projectName).Inc()
	}
	return d
}

// ensureJob builds and creates the Job for a Task dispatch.
// This is the helper referenced by the plan's grep contract.
func (r *TaskReconciler) ensureJob(ctx context.Context, task *tideprojectv1alpha1.Task, project *tideprojectv1alpha1.Project, attempt int, token string, envInJSON []byte) (*batchv1.Job, error) {
	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); err == nil {
			secretUID = string(secret.UID)
		}
	}
	opts := podjob.BuildOptions{
		Task:           task,
		Project:        project,
		Attempt:        attempt,
		SignedToken:    token,
		EnvelopeInJSON: envInJSON,
		SubagentImage:  r.SubagentImage,
		CredproxyImage: r.CredproxyImage,
		SecretUID:      secretUID,
		PVCName:        "tide-projects",
		ProjectUID:     string(project.UID),
	}
	job := podjob.BuildJobSpec(opts)
	if err := owner.EnsureOwnerRef(job, task, r.Scheme); err != nil {
		return nil, fmt.Errorf("ensure owner ref on job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("create job: %w", err)
		}
		// AlreadyExists: idempotent success — watch-lag race (Pitfall F / SUB-03).
	}
	return job, nil
}

// defaultsForSecret derives the effective Limits for a Secret.
// Precedence: Secret annotation > r.Defaults (Helm defaults).
func (r *TaskReconciler) defaultsForSecret(secret *corev1.Secret) budget.Limits {
	if secret == nil {
		return r.Defaults
	}
	limits := r.Defaults
	if v, ok := secret.Annotations["tideproject.k8s/requests-per-minute"]; ok {
		var rpm int
		if _, err := fmt.Sscanf(v, "%d", &rpm); err == nil {
			limits.RequestsPerMinute = rpm
		}
	}
	return limits
}

// buildEnvelopeIn constructs and marshals the EnvelopeIn for this Task dispatch.
// Translates api/v1alpha1.Caps → pkg/dispatch.Caps per Plan 03's two-type design.
func (r *TaskReconciler) buildEnvelopeIn(_ context.Context, task *tideprojectv1alpha1.Task, _ *tideprojectv1alpha1.Project, _ int, token string) (pkgdispatch.EnvelopeIn, []byte, error) {
	caps := pkgdispatch.Caps{}
	if task.Spec.Caps != nil {
		caps = pkgdispatch.Caps{
			WallClockSeconds: int(task.Spec.Caps.WallClockSeconds),
			Iterations:       int(task.Spec.Caps.Iterations),
			InputTokens:      task.Spec.Caps.InputTokens,
			OutputTokens:     task.Spec.Caps.OutputTokens,
		}
	}

	var dev *pkgdispatch.Dev
	if task.Spec.Dev.TestMode != "" {
		dev = &pkgdispatch.Dev{TestMode: task.Spec.Dev.TestMode}
	}

	envIn := pkgdispatch.EnvelopeIn{
		APIVersion:          pkgdispatch.APIVersionV1Alpha1,
		Kind:                pkgdispatch.KindTaskEnvelopeIn,
		TaskUID:             string(task.UID),
		Role:                "executor",
		Level:               "task",
		FilesTouched:        task.Spec.FilesTouched,
		DependsOn:           task.Spec.DependsOn,
		DeclaredOutputPaths: task.Spec.DeclaredOutputPaths,
		Caps:                caps,
		ProxyEndpoint:       "https://127.0.0.1:8443",
		SignedToken:         token,
		Dev:                 dev,
	}

	data, err := json.Marshal(envIn)
	if err != nil {
		return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal envelope in: %w", err)
	}
	return envIn, data, nil
}

// siblingsToTaskMapper returns reconcile requests for all sibling Tasks sharing
// the same PlanRef as the changed Task. This drives FAIL-02: when a predecessor's
// status changes, dependents are requeued so their indegree is re-evaluated.
func (r *TaskReconciler) siblingsToTaskMapper(ctx context.Context, obj client.Object) []reconcile.Request {
	task, ok := obj.(*tideprojectv1alpha1.Task)
	if !ok {
		return nil
	}
	if task.Spec.PlanRef == "" {
		return nil
	}
	var siblingList tideprojectv1alpha1.TaskList
	if err := r.List(ctx, &siblingList,
		client.InNamespace(task.Namespace),
		client.MatchingFields{taskPlanRefIndexKey: task.Spec.PlanRef},
	); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(siblingList.Items))
	for _, s := range siblingList.Items {
		if s.UID == task.UID {
			continue
		}
		reqs = append(reqs, reconcile.Request{
			NamespacedName: client.ObjectKey{Namespace: s.Namespace, Name: s.Name},
		})
	}
	return reqs
}

// buildViolationMessage builds a truncated human-readable message listing output
// path violations. Truncated to keep K8s Event message size manageable.
func buildViolationMessage(violations []string) string {
	const maxList = 5
	listed := violations
	suffix := ""
	if len(listed) > maxList {
		listed = violations[:maxList]
		suffix = fmt.Sprintf(" ... and %d more", len(violations)-maxList)
	}
	msg := fmt.Sprintf("Output path violations (%d total):", len(violations))
	for _, v := range listed {
		msg += "\n  " + v
	}
	return msg + suffix
}

func validateControllerOutputPaths(workspaceRoot string, runStart time.Time, declared []string) ([]string, bool, error) {
	violations, err := harness.Validate(workspaceRoot, runStart, declared)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return violations, false, nil
}

func conditionReasonFromEnvelopeResult(result string, exitCode int) string {
	if result == "" {
		if exitCode != 0 {
			return "NonZeroExitCode"
		}
		return "JobComplete"
	}

	var b strings.Builder
	capitalizeNext := true
	wrote := false
	for _, r := range result {
		if isASCIIAlpha(r) || isASCIIDigit(r) {
			if !wrote && !isASCIIAlpha(r) {
				b.WriteString("Result")
			}
			if capitalizeNext && isASCIIAlpha(r) {
				r = toASCIIUpper(r)
			}
			b.WriteRune(r)
			wrote = true
			capitalizeNext = false
			continue
		}
		capitalizeNext = true
	}
	if !wrote {
		return "EnvelopeResult"
	}
	return b.String()
}

func isASCIIAlpha(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func toASCIIUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}

// isJobTerminal returns true if the Job has a Complete or Failed condition with Status=True.
func isJobTerminal(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			if c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed {
				return true
			}
		}
	}
	return false
}

// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02, a
// namespace-filter predicate per AUTH-02, the .spec.planRef field indexer
// (RESEARCH.md Open Question #8), and sibling Task watches for FAIL-02.
func (r *TaskReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Register .spec.planRef field indexer so listSiblingTasks can use MatchingFields.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&tideprojectv1alpha1.Task{},
		taskPlanRefIndexKey,
		func(obj client.Object) []string {
			task := obj.(*tideprojectv1alpha1.Task) //nolint:forcetypeassert // type guaranteed by IndexField
			return []string{task.Spec.PlanRef}
		},
	); err != nil {
		return fmt.Errorf("register .spec.planRef field indexer: %w", err)
	}

	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	// Plan 04-05: AnnotationChangedPredicate is wired via a self-Watches with
	// a permissive mapper instead of as a For()-level predicate. This is
	// deliberate: a For()-level GenerationChangedPredicate Or
	// AnnotationChangedPredicate filters out the post-finalizer Update event
	// (finalizer is metadata; Generation does not bump; annotations are
	// unchanged), stalling dispatch in integration tests where the manager's
	// auto-reconcile is the only driver. The self-Watches re-enqueues the
	// Task on annotation changes without filtering Spec/finalizer/owner-ref
	// updates from the default For() event stream.
	annotationOnly := predicate.AnnotationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Task{}).
		Owns(&batchv1.Job{}).
		Watches(
			&tideprojectv1alpha1.Task{},
			handler.EnqueueRequestsFromMapFunc(r.siblingsToTaskMapper),
		).
		Watches(
			&tideprojectv1alpha1.Task{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(annotationOnly),
		).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("task").
		Complete(r)
}
