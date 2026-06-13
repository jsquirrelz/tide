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
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/gates"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
	webhookv1alpha1 "github.com/jsquirrelz/tide/internal/webhook/v1alpha1"
	"github.com/jsquirrelz/tide/pkg/dag"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

const planFinalizer = "tideproject.k8s/plan-cleanup"

// Note: ErrParentUnresolved is declared in task_controller.go (same package).
// Phase 04.1 P1.4 — shared across TaskReconciler and PlanReconciler.

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

	// SubagentImage is dead since Phase 13 — resolveImage owns resolution;
	// retained for legacy test wiring, ignored at dispatch.
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

	// HelmProviderDefaults carry Helm-chart provider/model defaults (Phase 3).
	HelmProviderDefaults ProviderDefaults

	// PricingOverridesJSON is the validated D-02 override JSON forwarded
	// opaquely to planner Jobs as TIDE_PRICING_OVERRIDES_JSON. Wired in Plan 14-05.
	PricingOverridesJSON string

	// DefaultFileTouchMode is the cluster-level file-touch validation default from
	// the Helm chart (typically "warn"). Matches the PlanCustomValidator field so
	// the reconciler gate (D-05) and the admission webhook use the same baseline
	// when no Project.Spec.PlanAdmission.FileTouchMode is set.
	DefaultFileTouchMode string

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

	// 4b. D-03 (CUTS-01): backfill tideproject.k8s/project on the Plan itself
	// when the label is absent. Heals pre-v1.0.1 Plan CRs on upgraded clusters.
	// Guard: only patch when label is missing so the second reconcile is a no-op
	// (idempotent). Runs BEFORE dispatch so a parked AwaitingApproval Plan
	// self-heals on its first post-upgrade reconcile. Uses resolveProjectName
	// (Plan→Phase→Milestone→Project chain); skips silently on ErrParentUnresolved
	// so orphan Plans stay unlabeled rather than mis-scoped (T-17-03 mitigation).
	if plan.Labels[owner.LabelProject] == "" {
		if name, err := r.resolveProjectName(ctx, &plan); err == nil && name != "" {
			patch := client.MergeFrom(plan.DeepCopy())
			if plan.Labels == nil {
				plan.Labels = map[string]string{}
			}
			plan.Labels[owner.LabelProject] = name
			if err := r.Patch(ctx, &plan, patch); err != nil {
				return ctrl.Result{}, fmt.Errorf("backfill project label on plan %s: %w", plan.Name, err)
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
	// Phase 12 CR-02 / CR-01 fix: AwaitingApproval early-return placed at the VERY
	// TOP — BEFORE the tasks-exist List — because a parked Plan with
	// reporter-materialized Tasks would otherwise take the tasks-exist exit to
	// dispatched=false, letting reconcileWaveMaterialization run while parked and
	// dispatch executor Jobs without approval.
	// Mirrors milestone_controller.go:216-243 Step 1a, adapted to the (ctrl.Result,
	// bool, error) signature: dispatched=true suppresses reconcileWaveMaterialization.
	if plan.Status.Phase == "AwaitingApproval" {
		if gates.CheckApprove(plan, "plan") {
			// Consume annotation (T-04-G2 one-shot).
			newAnno := gates.ConsumeApprove(plan, "plan")
			annoPatch := client.MergeFrom(plan.DeepCopy())
			plan.SetAnnotations(newAnno)
			if err := r.Patch(ctx, plan, annoPatch); err != nil {
				return ctrl.Result{}, true, err
			}
			// Return to Running + record ApprovedByUser condition (D-04).
			statusPatch := client.MergeFrom(plan.DeepCopy())
			plan.Status.Phase = "Running"
			meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
				Status:             metav1.ConditionFalse,
				Reason:             tideprojectv1alpha1.ReasonApprovedByUser,
				Message:            "Plan approved; Tasks will dispatch",
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Patch(ctx, plan, statusPatch); err != nil {
				return ctrl.Result{}, true, err
			}
			// Requeue immediately — the Running branch (below) calls
			// handlePlannerJobCompletion which owns ChildCount-gated succession (D-03).
			return ctrl.Result{Requeue: true}, true, nil
		}
		// No annotation — keep parked; dispatched=true so reconcileWaveMaterialization
		// never runs while parked (GATE-04: no executor Jobs, no Wave CRs).
		return ctrl.Result{}, true, nil
	}

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

	// D-02 descent hold: if the parent Phase is parked at AwaitingApproval,
	// hold Job dispatch here. The Plan stays at Status.Phase="" so tide approve's
	// findAwaitingPlan cannot target a held child instead of the parked parent
	// (12-RESEARCH.md Pitfall 5). NotFound parent is transient informer lag —
	// checkParentApproval returns (false, nil) and dispatch continues.
	if held, hErr := checkParentApproval(ctx, r.Client, plan.Namespace, plan.Spec.PhaseRef, "Phase"); hErr != nil {
		return ctrl.Result{}, true, hErr
	} else if held {
		logf.FromContext(ctx).V(1).Info("dispatch held: parent Phase awaiting approval",
			"plan", plan.Name, "phase", plan.Spec.PhaseRef)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
	}

	// D-05 dispatch-entry reject hold — resolve Project early to check for a reject
	// annotation before acquiring the pool or creating a Job. A rejected Project
	// halts NEW dispatch; in-flight Jobs drain (no Job deletion — resolved discretion call).
	{
		earlyProject := r.resolveProjectForPlan(ctx, plan)
		if earlyProject != nil && gates.CheckRejected(earlyProject) {
			res, err := r.patchPlanRejected(ctx, plan, gates.RejectedReason(earlyProject))
			return res, true, err
		}
		// Phase 13 HALT-01 / D-05: third dispatch-entry hold (after CheckRejected +
		// parent-approval); park, never fail; cleared by tide resume.
		// Position: BEFORE pool acquire and BEFORE Job creation (Pitfall 2).
		// No per-Plan condition written (operator signal is the Project condition).
		if checkBillingHalt(earlyProject) {
			logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
				"plan", plan.Name)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, true, nil
		}
		// Phase 14 BUDGET-02 / D-04: BudgetBlocked hold (operator cap) — separate from
		// BillingHalt (provider billing); both may be true simultaneously.
		// No per-Plan condition written (operator signal is the single Project BudgetBlocked condition).
		if checkBudgetBlocked(earlyProject) && !budget.IsBypassed(earlyProject, time.Now()) {
			logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked",
				"plan", plan.Name)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, true, nil
		}
	}

	// Acquire plannerPool (POOL-01) before Job creation (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, true, err
		}
		defer r.PlannerPool.Release()
	}

	project := r.resolveProjectForPlan(ctx, plan)

	// Cascade 7: BuildJobSpec drops the credproxy provider Secret when
	// opts.Project==nil (internal/dispatch/podjob/jobspec.go:259-273), causing
	// credproxy to start without ANTHROPIC_API_KEY → CrashLoopBackOff. Dispatch
	// is single-shot (idempotent on AlreadyExists), so the first nil-Project
	// create would permanently wedge the planner. Gate dispatch on Project
	// resolution.
	if project == nil {
		logger := logf.FromContext(ctx).WithValues("plan", plan.Name) //nolint:logcheck // controller-runtime logf idiom used codebase-wide; klogr helper not adopted
		if plan.Spec.PhaseRef == "" {
			// Permanent: empty PhaseRef is a configuration error; admission
			// validation should reject it. Refuse dispatch without requeueing so
			// we don't loop on bad input.
			logger.Info("refusing plan-planner dispatch: plan.spec.phaseRef is empty", "cascade", 7)
			return ctrl.Result{}, false, nil
		}
		// Transient: Phase/Milestone/Project chain not yet visible in informer
		// cache. Requeue to retry once the cache catches up.
		logger.V(1).Info("deferring plan-planner dispatch: project chain not yet resolvable, requeueing", "cascade", 7)
		return ctrl.Result{RequeueAfter: 1 * time.Second}, false, nil
	}

	// Phase 04.1 P1.2 fix: planner Jobs now share the full Phase 2 dispatch
	// contract via podjob.BuildJobSpec(Kind=JobKindPlanner).
	attempt := 1 // plan planner dispatch is single-shot per ROADMAP scope

	plannerCaps := podjob.DefaultCaps(nil, podjob.JobKindPlanner)
	if plannerCaps.Iterations <= 0 {
		plannerCaps.Iterations = 20
	}
	plannerPrompt := outcomePromptOf(project)
	_, envInJSON, err := BuildPlannerEnvelope("plan", plan, project, attempt, "", plannerPrompt, pkgdispatch.Caps{
		WallClockSeconds: int(plannerCaps.WallClockSeconds),
		Iterations:       int(plannerCaps.Iterations),
	}, "https://127.0.0.1:8443", r.HelmProviderDefaults)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("build planner envelope: %w", err)
	}

	// Mint a signed token for the credproxy sidecar.
	token, err := credproxy.Sign(r.SigningKey, string(plan.UID), time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if err != nil {
		return ctrl.Result{}, true, fmt.Errorf("mint planner signed token: %w", err)
	}

	var secretUID string
	if project != nil && project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}

	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindPlanner,
		ParentObj:            plan,
		Level:                "plan",
		Attempt:              attempt,
		Project:              project,
		SignedToken:          token,
		EnvelopeInJSON:       envInJSON,
		SubagentImage:        resolveImage(project, "plan", r.HelmProviderDefaults),
		CredproxyImage:       r.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              "tide-projects",
		ProjectUID:           projectUID,
		Caps:                 plannerCaps,
		PricingOverridesJSON: r.PricingOverridesJSON,
	}
	job := podjob.BuildJobSpec(opts)
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

// handlePlannerJobCompletion reads tiny status from the completed planner Job,
// spawns the tide-reporter reader Job to materialize Task child CRDs from the
// PVC-side out.json, and clears the Running phase so the Phase 2 Wave path can
// pick up on the next reconcile.
//
// Materialization is now the reporter Job's job (Phase 09 plan 09-06, REQ-09-01).
// Children (Tasks + Waves) arrive via the existing Owns watches once the reporter
// creates them. The reporter also stamps ValidationState=Validated in a follow-up
// reconcile when child Tasks are observed (reconcileWaveMaterialization gate).
//
// Note: This does NOT create Waves — the existing reconcileWaveMaterialization
// handles that once the admission webhook stamps ValidationState=Validated.
func (r *PlanReconciler) handlePlannerJobCompletion(ctx context.Context, plan *tideprojectv1alpha1.Plan, completedJob *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	project := r.resolveProjectForPlan(ctx, plan)
	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	// Phase 12 / Phase 04.1: reject short-circuit FIRST — operator stop should always
	// halt, regardless of envelope availability or read errors.
	// Mirrors milestone_controller.go:442-449 ("reject short-circuit FIRST").
	// D-05: park (not fail) — in-flight Jobs drain; state is preserved for resume.
	if project != nil && gates.CheckRejected(project) {
		return r.patchPlanRejected(ctx, plan, gates.RejectedReason(project))
	}

	// Read tiny status from the dispatch Job's termination message for budget
	// rollup and failure classification. ChildCRDs are NOT used here —
	// materialization has moved to the reporter Job (REQ-09-01).
	// Plan 09-08: capture out so we can gate on out.ChildCount below.
	//
	// Phase 17 DEBT-04 (CR-01): Pitfall-1 parity with milestone/phase controllers.
	// A transient PVC/read error must not wedge the Plan to terminal Status.Phase=Failed.
	// Track envReaderPresent to distinguish nil-reader (unit-test / non-Option-C path)
	// from read-error (transient); envReadOK gates the envelope-dependent downstream.
	var out pkgdispatch.EnvelopeOut
	envReadOK := false
	envReaderPresent := r.EnvReader != nil
	if r.EnvReader == nil {
		// Fallback: no EnvReader (non-Option-C / unit-test path). Clear Running phase
		// immediately and let the Wave path take over, mirroring prior behavior.
		logger.Info("no env reader; clearing Running phase to let Wave path proceed")
		patch := client.MergeFrom(plan.DeepCopy())
		plan.Status.Phase = ""
		_ = r.Status().Patch(ctx, plan, patch)
		return ctrl.Result{}, nil
	}

	var readErr error
	out, readErr = r.EnvReader.ReadOut(ctx, projectUID, string(plan.UID))
	if readErr != nil {
		// Non-fatal: log and defer to children-based succession (Pitfall-1 parity with
		// milestone_controller.go:535-539 and phase_controller.go:476-479). A transient
		// read error must not permanently wedge the Plan — the envelope is a status
		// optimization, not the success authority.
		logger.Error(readErr, "plan planner envelope tiny-status read failed (non-fatal); deferring to children-based succession", "plan", plan.Name)
	} else {
		envReadOK = true
	}

	// Spawn the tide-reporter reader Job in the project namespace (Option C).
	// The reporter reads out.json from the PVC and materializes Task children.
	// Children arrive via the Owns(&Task{}) / Owns(&Wave{}) watch once created.
	// T-09-13: idempotent — AlreadyExists on Create is success.
	// isFirstCompletion: true when the reporter Job is newly spawned (plan 09-08).
	isFirstCompletion := false
	if r.ReporterImage != "" && project != nil {
		reporterJobName := fmt.Sprintf("tide-reporter-%s", plan.UID)
		pvcName := defaultSharedPVCName
		var existingReporterJob batchv1.Job
		if gErr := r.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: plan.Namespace}, &existingReporterJob); gErr != nil {
			if !apierrors.IsNotFound(gErr) {
				return ctrl.Result{}, fmt.Errorf("get reporter job %s: %w", reporterJobName, gErr)
			}
			isFirstCompletion = true
			reporterJob := BuildReporterJob(plan, project, pvcName, string(plan.UID), "Plan",
				ReporterOptions{ReporterImage: r.ReporterImage}, r.Scheme)
			if cErr := r.Create(ctx, reporterJob); cErr != nil {
				if !apierrors.IsAlreadyExists(cErr) {
					return ctrl.Result{}, fmt.Errorf("create reporter job %s: %w", reporterJobName, cErr)
				}
			} else {
				logger.Info("spawned reporter Job", "job", reporterJobName, "plan", plan.Name)
			}
		} else {
			logger.V(1).Info("reporter Job already exists; skipping spawn (T-09-13)", "job", reporterJobName)
		}
	} else if r.ReporterImage == "" {
		isFirstCompletion = true
		logger.V(1).Info("skipping reporter Job spawn: ReporterImage not configured", "plan", plan.Name)
	}

	// Plan 09-08 Defect C: roll up planner-level Usage once per planner Job completion.
	// Guard on envReadOK: out.Usage is only valid when the envelope read succeeded.
	if isFirstCompletion && envReadOK && project != nil {
		if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
			logger.Error(rollErr, "plan planner budget rollup failed (non-fatal)", "plan", plan.Name)
		}
	}

	// Phase 13 D-04 layer 2: backstop — classify planner-envelope failure Reason.
	// Guard on envReadOK: out.ExitCode/Reason are only valid when the envelope read succeeded.
	if envReadOK && out.ExitCode != 0 && project != nil {
		var jobStart time.Time
		if completedJob != nil {
			jobStart = completedJob.CreationTimestamp.Time
		}
		if hErr := setBillingHaltIfNeeded(ctx, r.Client, project, out.Reason, jobStart); hErr != nil {
			logger.Error(hErr, "setBillingHaltIfNeeded failed (non-fatal)", "plan", plan.Name)
		}
	}

	// REQ-7a: stamp ValidationState=Validated so reconcileWaveMaterialization
	// proceeds past the gate. Only stamp when the envelope read succeeded (i.e. we
	// have a valid tiny status) — the reporter Job is in flight, Tasks will appear shortly.
	// On a read error, skip the stamp and fall through to the children-based fallback below
	// (Pitfall-1 parity: the envelope is a status optimization, not the success authority).
	if envReadOK {
		valPatch := client.MergeFrom(plan.DeepCopy())
		plan.Status.ValidationState = "Validated"
		if err := r.Status().Patch(ctx, plan, valPatch); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Phase 12 CR-01 fix: gate-policy hook moved BEFORE the ChildCount requeue so
	// the gate fires even when ChildCount>0. Previously the ChildCount requeue
	// returned first and patchPlanAwaitingApproval never ran for non-leaf Plans.
	// Position comment: the reporter Job was already spawned above, so children
	// keep materializing while parked — D-02 "materialize children, hold dispatch".
	// ValidationState=Validated is already stamped so the wave path is armed the
	// moment approval lands. Mirrors milestone_controller.go:510-553.
	if project != nil {
		policy := gates.EvaluatePolicy(project.Spec.Gates, "plan")
		if policy == gates.PolicyApprove || policy == gates.PolicyPause {
			// Check if this level was already approved (permanent ApprovedByUser or
			// ResumedByUser condition with Status=False means the park was lifted).
			// Prevents re-parking after the Edit-1 AwaitingApproval branch approved
			// the level — without this guard the consumed annotation re-parks on the
			// next pass through this function.
			alreadyApproved := false
			if c := meta.FindStatusCondition(plan.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused); c != nil {
				if c.Status == metav1.ConditionFalse &&
					(c.Reason == tideprojectv1alpha1.ReasonApprovedByUser || c.Reason == tideprojectv1alpha1.ReasonResumedByUser) {
					alreadyApproved = true
				}
			}
			if !alreadyApproved {
				if !gates.CheckApprove(plan, "plan") {
					// No annotation and not yet approved — park.
					return r.patchPlanAwaitingApproval(ctx, plan, policy)
				}
				// Annotation present at the hook (operator approved before the park fired):
				// consume it and write Running+ApprovedByUser so the condition is recorded
				// for future reconciles — otherwise the next reconcile would re-park because
				// the annotation is gone but no approval record exists.
				newAnno := gates.ConsumeApprove(plan, "plan")
				annoPatch := client.MergeFrom(plan.DeepCopy())
				plan.SetAnnotations(newAnno)
				if err := r.Patch(ctx, plan, annoPatch); err != nil {
					return ctrl.Result{}, err
				}
				statusPatch := client.MergeFrom(plan.DeepCopy())
				plan.Status.Phase = "Running"
				meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
					Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
					Status:             metav1.ConditionFalse,
					Reason:             tideprojectv1alpha1.ReasonApprovedByUser,
					Message:            "Plan approved; Tasks will dispatch",
					LastTransitionTime: metav1.Now(),
				})
				if err := r.Status().Patch(ctx, plan, statusPatch); err != nil {
					return ctrl.Result{}, err
				}
				// Fall through to ChildCount-gated succession (D-03).
			}
			// alreadyApproved: fall through to ChildCount-gated succession.
		}
	}

	// Plan 09-08 Defect B fix: uniform ChildCount-gated succession replaces the
	// prior reporterSpawned early-return. Gate:
	//   expected == 0            → clear Running immediately (genuine leaf: no Tasks)
	//   observed < expected      → requeue 5s (reporter still materializing Tasks)
	//   observed >= expected     → clear Running, let Wave path take over
	// The plan controller does NOT call patchPlanSucceeded here — succession
	// happens in reconcileWaveMaterialization once all Tasks complete.
	//
	// Phase 17 DEBT-04: when envReadOK=false (transient read error), out.ChildCount is
	// unreliable. Use the children-based fallback instead (Pitfall-1 parity):
	//   - reader present but errored AND no children yet → requeue (envelope may have ChildCount>0)
	//   - reader present but errored AND children already exist → fall through (reporter is in flight)
	// This mirrors phase_controller.go:617-621.
	if envReadOK {
		expected := out.ChildCount
		if expected > 0 {
			observed := r.countChildTasks(ctx, plan)
			if observed < expected {
				logger.V(1).Info("requeue: reporter still materializing Task children",
					"plan", plan.Name, "expected", expected, "observed", observed)
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
		}
	} else if envReaderPresent && r.countChildTasks(ctx, plan) == 0 {
		// Reader exists but had a read error AND no children observed yet — the envelope
		// may have ChildCount>0 (children still materializing). Requeue; don't auto-succeed.
		logger.V(1).Info("boundary push deferred: env reader present but unreadable, waiting (fallback)", "plan", plan.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Plan 04-06 W-2: boundary push trigger AFTER gate, BEFORE clearing
	// the Running phase. Plan boundary is the only D-B2 shape with the
	// `+ executed` suffix (Tasks have already run by this seam).
	//
	// CR-03 partial-fix note: the milestone/phase controllers now gate the
	// push on gates.BoundaryDetected, but the plan controller does NOT,
	// because the plan reconcile path is structurally different. Once child
	// Tasks exist, reconcilePlannerDispatch returns early
	// (dispatched=false → reconcileWaveMaterialization) without entering
	// handlePlannerJobCompletion, so any BoundaryDetected gate here becomes
	// unreachable when children are present. Properly tightening the plan
	// boundary requires firing the push from a separate seam in the wave-
	// materialization path on task-status updates (out of REVIEW-FIX scope).
	// Documented in 04-REVIEW-FIX.md.
	// At planner-Job completion time, Tasks do not yet exist (the planner just
	// materialized them). Pass nil task branches — D-04 integration only runs
	// after Tasks have Succeeded (handled in reconcileWaveMaterialization).
	if err := r.maybeTriggerBoundaryPush(ctx, plan, project, nil); err != nil {
		return ctrl.Result{}, err
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

// patchPlanSucceeded sets Plan.Status.Phase=Succeeded and stamps the
// ConditionSucceeded condition. Called from reconcileWaveMaterialization when
// BoundaryDetected(plan, "Task") returns true (REQ-7b). Mirrors
// milestone_controller.go's patchMilestoneSucceeded pattern.
func (r *PlanReconciler) patchPlanSucceeded(ctx context.Context, plan *tideprojectv1alpha1.Plan) (ctrl.Result, error) {
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.Phase = "Succeeded"
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             "TasksCompleted",
		Message:            "All owned Tasks reached Succeeded; Plan complete",
		LastTransitionTime: metav1.Now(),
	})
	// Clear any prior WaveOrLevelPaused state so the transition is
	// visible to operators tailing conditions (mirrors patchMilestoneSucceeded).
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             tideprojectv1alpha1.ReasonResumedByUser,
		Message:            "Plan complete; all Tasks Succeeded",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// patchPlanRejected parks the Plan with a RejectedByUser condition WITHOUT
// writing Status.Phase=Failed (D-05). In-flight Jobs drain; state is preserved
// so clearing the reject annotation (tide resume) lets the level re-enter the
// normal dispatch path on the next reconcile.
// Returns RequeueAfter 5s so the park polls for the annotation clear.
func (r *PlanReconciler) patchPlanRejected(ctx context.Context, plan *tideprojectv1alpha1.Plan, reason string) (ctrl.Result, error) {
	patch := client.MergeFrom(plan.DeepCopy())
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonRejectedByUser,
		Message:            fmt.Sprintf("Rejected: %s", reason),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// patchPlanFileTouchMismatch parks the Plan for a strict file-touch overlap (D-05,
// D-06). Sets ValidationState=FileTouchMismatch AND a WaveOrLevelPaused condition
// whose Message names both tasks and the shared paths via SummariseMismatches.
// Returns ctrl.Result{} without requeueing — the next Task create/update event
// re-enters reconcile (matching how the reporter flow materializes Tasks async;
// the false-negative window self-heals on the next Task event, per RESEARCH Pitfall 3).
// No Status.Phase mutation (park-not-fail doctrine, D-05).
func (r *PlanReconciler) patchPlanFileTouchMismatch(ctx context.Context, plan *tideprojectv1alpha1.Plan, mismatches []webhookv1alpha1.FileTouchMismatchPair) (ctrl.Result, error) {
	summary := webhookv1alpha1.SummariseMismatches(mismatches)
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.ValidationState = "FileTouchMismatch"
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             "FileTouchMismatch",
		Message:            fmt.Sprintf("strict file-touch overlap detected — fix by adding a dependsOn edge or splitting file ownership: %s", summary),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// liftPlanFileTouchMismatch clears a prior FileTouchMismatch park (D-06).
// Resets ValidationState to "Validated" and flips the WaveOrLevelPaused
// condition to Status=False so the reconcile proceeds to wave derivation.
func (r *PlanReconciler) liftPlanFileTouchMismatch(ctx context.Context, plan *tideprojectv1alpha1.Plan) (ctrl.Result, error) {
	patch := client.MergeFrom(plan.DeepCopy())
	plan.Status.ValidationState = "Validated"
	meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionFalse,
		Reason:             "FileTouchValidationPassed",
		Message:            "file-touch overlap resolved; proceeding to wave derivation",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, plan, patch); err != nil {
		return ctrl.Result{}, err
	}
	// Re-enter reconcile immediately so wave derivation runs this cycle.
	return ctrl.Result{Requeue: true}, nil
}

// patchPlanFailed sets Plan.Status.Phase=Failed with the given reason/message.
// Used by the Plan 04-05 gate-policy hook (genuine planner-Job failure classification).
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

// countChildTasks returns the number of Tasks owned by this Plan (plan 09-08).
// Used by the ChildCount-gated succession path to compare observed vs expected children.
func (r *PlanReconciler) countChildTasks(ctx context.Context, plan *tideprojectv1alpha1.Plan) int {
	var taskList tideprojectv1alpha1.TaskList
	if err := r.List(ctx, &taskList, client.InNamespace(plan.Namespace)); err != nil {
		return 0
	}
	count := 0
	for i := range taskList.Items {
		for _, ref := range taskList.Items[i].OwnerReferences {
			if ref.Kind == "Plan" && ref.UID == plan.UID {
				count++
			}
		}
	}
	return count
}

// resolveProjectForPlan walks Plan → Phase → Milestone → Project.
func (r *PlanReconciler) resolveProjectForPlan(ctx context.Context, plan *tideprojectv1alpha1.Plan) *tideprojectv1alpha1.Project {
	// Fast path: if the Plan carries the tideproject.k8s/project label (stamped
	// by stampTaskLabels), use it directly to avoid the Phase→Milestone→Project
	// chain walk. This is the same label fast-path resolveProjectName uses.
	if projectName, ok := plan.Labels["tideproject.k8s/project"]; ok && projectName != "" {
		var p tideprojectv1alpha1.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: projectName}, &p); err == nil {
			return &p
		}
	}

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

// reconcileWaveBoundary runs the per-wave integration gate (D-02 / SC-3 /
// Plan 11-03) for the single wave boundary k → k+1. Returns handled=true when
// the boundary decided the reconcile outcome (terminal failure, requeue, or
// error) and the caller must return (res, err) immediately; handled=false
// means this boundary needs nothing right now — fall through to the next.
func (r *PlanReconciler) reconcileWaveBoundary(
	ctx context.Context,
	plan *tideprojectv1alpha1.Plan,
	project *tideprojectv1alpha1.Project,
	taskByName map[string]*tideprojectv1alpha1.Task,
	layers [][]dag.NodeID,
	k int,
) (ctrl.Result, bool, error) {
	waveNum := k + 1 // 1-indexed wave number

	// If already integrated through this wave, skip to next boundary.
	if plan.Status.IntegratedThroughWave >= waveNum {
		return ctrl.Result{}, false, nil
	}

	// Integration only applies when a real git target + push image exist.
	// Stub/test/no-remote projects have no run branch to integrate into —
	// there is nothing to push, so this boundary must NOT block wave k+1
	// dispatch (otherwise the no-op triggerWaveIntegrationJob would requeue
	// forever and IntegratedThroughWave would never advance). Fall through to
	// the normal label-stamp + Task-dispatch path below.
	if project == nil || project.Spec.Git == nil || project.Spec.Git.RepoURL == "" || r.TidePushImage == "" {
		return ctrl.Result{}, false, nil
	}

	// PauseBetweenWaves (Plan 04-05) is the OUTER operator gate at this
	// boundary: do not integrate a wave that is still awaiting operator
	// approval. maybePauseForWaveApprove (downstream) sets the
	// WaveOrLevelPaused condition and blocks Task dispatch via the
	// wave-paused label. Once the operator approves, the wave-approved-<N>
	// label is stamped and integration proceeds on a later reconcile —
	// integrate-then-dispatch ordering is preserved past the gate.
	if project.Spec.Gates.PauseBetweenWaves &&
		plan.Labels[fmt.Sprintf("%s%d", planWaveApprovedLabelPrefix, waveNum)] != "true" {
		return ctrl.Result{}, false, nil
	}

	integJobName := fmt.Sprintf("tide-push-wave-%s-%d", plan.UID, waveNum)

	// RESPONSIBILITY A: Check if integration Job exists.
	var integJob batchv1.Job
	getErr := r.Get(ctx, types.NamespacedName{Name: integJobName, Namespace: plan.Namespace}, &integJob)
	if getErr == nil {
		// Job exists — check terminal status.
		// IMPORTANT: check Failed BEFORE Succeeded==0 to avoid livelock.
		if integJob.Status.Failed > 0 {
			// Permanently failed (BackoffLimit exhausted) → terminal Plan failure.
			res, err := r.patchPlanFailed(ctx, plan,
				tideprojectv1alpha1.ReasonWaveIntegrationFailed,
				fmt.Sprintf("wave %d integration job %s failed (BackoffLimit exhausted)", waveNum, integJobName))
			return res, true, err
		}
		if integJob.Status.Succeeded > 0 {
			// Integration complete — stamp IntegratedThroughWave and continue loop.
			patch := client.MergeFrom(plan.DeepCopy())
			plan.Status.IntegratedThroughWave = waveNum
			if err := r.Status().Patch(ctx, plan, patch); err != nil {
				return ctrl.Result{}, true, fmt.Errorf("patch IntegratedThroughWave=%d: %w", waveNum, err)
			}
			return ctrl.Result{}, false, nil
		}
		// Job is still running (Succeeded==0, Failed==0): block wave k+1 dispatch.
		return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
	}
	if !apierrors.IsNotFound(getErr) {
		return ctrl.Result{}, true, fmt.Errorf("get wave integration job %s: %w", integJobName, getErr)
	}

	// RESPONSIBILITY B: No Job found — dispatch if all wave-k tasks Succeeded.
	for _, name := range layers[k] {
		t := taskByName[name]
		if t == nil || t.Status.Phase != "Succeeded" {
			// Wave k not yet complete — nothing to integrate yet.
			return ctrl.Result{}, false, nil
		}
	}

	// Collect wave-k task branch names.
	var branches []string
	for _, name := range layers[k] {
		if t := taskByName[name]; t != nil {
			branches = append(branches, pkggit.TaskBranchName(string(t.UID)))
		}
	}

	// Dispatch the integration Job.
	if err := triggerWaveIntegrationJob(ctx, r.Client, r.Scheme, plan, project, waveNum, branches, r.TidePushImage); err != nil {
		return ctrl.Result{}, true, err
	}
	// Requeue to wait for the Job to complete (RESPONSIBILITY A on next cycle).
	// Do NOT stamp IntegratedThroughWave here — the Job has not yet completed.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
}

// reconcileWaveMaterialization implements the Wave materialization body inside the
// Dispatcher seam (step 5 of the six-step pattern).
//
// Per PERSIST-03: pkg/dag.ComputeWaves is called on EVERY reconcile — the schedule
// is re-derived from the current Task set, never cached in .status.
func (r *PlanReconciler) reconcileWaveMaterialization(ctx context.Context, plan *tideprojectv1alpha1.Plan) (ctrl.Result, error) {
	// Step 1: No-op until Plan is Validated by the admission webhook (Plan 11).
	// FileTouchMismatch is the dormant parked state set by this reconciler; treat
	// it as "Validated" so we re-enter the gate on every Task change and can lift
	// the park once the overlap is resolved (D-06).
	if plan.Status.ValidationState != "Validated" && plan.Status.ValidationState != "FileTouchMismatch" {
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

	// Step 2b: D-05 / D-06 file-touch dispatch gate.
	// After Tasks materialize (reporter flow or direct apply) and before wave
	// derivation: check for strict-mode file-touch overlaps. If found, park the
	// Plan with ValidationState=FileTouchMismatch and return without dispatching
	// any Jobs. If no overlaps (or mode is not strict), lift a prior park.
	// This gate is the authoritative seat — the webhook's Pitfall B means it never
	// sees reporter-flow Tasks; this gate always runs after Tasks exist.
	if len(taskList.Items) > 0 {
		project := r.resolveProjectForPlan(ctx, plan)
		mode := webhookv1alpha1.ResolveFileTouchMode(plan, project, r.DefaultFileTouchMode)
		mismatches := webhookv1alpha1.ComputeFileTouchMismatches(taskList.Items)

		if len(mismatches) > 0 && mode == "strict" {
			// Park: ValidationState=FileTouchMismatch, no wave derivation, no dispatch.
			return r.patchPlanFileTouchMismatch(ctx, plan, mismatches)
		}

		// D-06 un-park path: if we were parked for FileTouchMismatch but now either
		// the mode is non-strict or the overlaps are resolved, lift the park.
		if plan.Status.ValidationState == "FileTouchMismatch" {
			return r.liftPlanFileTouchMismatch(ctx, plan)
		}
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

	// Step 4: Materialize Waves (independent of Project resolution — DAG-only).
	if err := r.materializeWaves(ctx, plan, taskList.Items, layers); err != nil {
		return ctrl.Result{}, err
	}

	// Step 4b: Per-wave integration gate (D-02 / SC-3 / Plan 11-03).
	// For each wave boundary (wave k → wave k+1), we must ensure wave k's
	// task branches are integrated into the run branch before wave k+1 executors
	// are dispatched. Three responsibilities, checked in order each reconcile:
	//
	//   RESPONSIBILITY A — Completion gate / failure detection (check FIRST):
	//   If an integration Job already exists for wave k+1, check its status:
	//   - Failed > 0: permanently failed → mark Plan Failed (no livelock)
	//   - Succeeded > 0: stamp IntegratedThroughWave = k+1 and continue
	//   - Otherwise (running): return requeue to wait for completion
	//
	//   RESPONSIBILITY B — Dispatch:
	//   If no integration Job exists, all wave-k tasks are Succeeded, and
	//   IntegratedThroughWave < k+1: dispatch the integration Job and requeue.
	//
	//   RESPONSIBILITY C — Gate:
	//   materializeWaves is called above; the per-wave integration field
	//   gates task dispatch inside materializeWaves (enforced there).
	//   Here we block if any wave boundary requires integration.

	// Resolve project for wave integration jobs (need Project for push job spec).
	project := r.resolveProjectForPlan(ctx, plan)

	taskByName := make(map[string]*tideprojectv1alpha1.Task, len(taskList.Items))
	for i := range taskList.Items {
		taskByName[taskList.Items[i].Name] = &taskList.Items[i]
	}

	// Iterate each wave boundary k → k+1. Skip the last wave (no k+1 to gate on).
	for k := 0; k < len(layers)-1; k++ {
		res, handled, err := r.reconcileWaveBoundary(ctx, plan, project, taskByName, layers, k)
		if handled || err != nil {
			return res, err
		}
	}

	// Step 5: Resolve the project name for Task label stamping.
	// Phase 04.1 P1.4: on miss, set ConditionParentUnresolved and requeue after 30s
	// so Tasks can still be dispatched on the next cycle once the label is stamped.
	// Wave materialization above completes regardless (it is label-independent).
	projectName, projectErr := r.resolveProjectName(ctx, plan)
	if errors.Is(projectErr, ErrParentUnresolved) {
		patch := client.MergeFrom(plan.DeepCopy())
		meta.SetStatusCondition(&plan.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionParentUnresolved,
			Status:             metav1.ConditionTrue,
			Reason:             tideprojectv1alpha1.ReasonNoProjectLabel,
			Message:            "No Project found via label or owner-ref chain; awaiting owner-ref wiring by PhaseReconciler",
			LastTransitionTime: metav1.Now(),
		})
		if perr := r.Status().Patch(ctx, plan, patch); perr != nil {
			return ctrl.Result{}, fmt.Errorf("patch parent-unresolved condition on plan: %w", perr)
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	if projectErr != nil {
		return ctrl.Result{}, fmt.Errorf("resolve project name: %w", projectErr)
	}

	if err := r.stampTaskLabels(ctx, taskList.Items, layers, projectName); err != nil {
		return ctrl.Result{}, err
	}

	// REQ-7b: check whether all owned Tasks have Succeeded. When true, stamp
	// Plan.Status.Phase=Succeeded so PhaseReconciler.handleJobCompletion can
	// observe Plan=Succeeded via gates.BoundaryDetected(ph, "Plan") and advance
	// the Phase. The Succeeded short-circuit in reconcilePlannerDispatch (terminal
	// guard) prevents re-entry on subsequent reconciles. The childless guard in
	// BoundaryDetected (returns false when 0 Tasks owned) prevents premature
	// Succeeded before Task dispatch; Owns(&Task{}) re-enqueues this Plan on
	// every Task status update so the check converges correctly.
	detected, derr := gates.BoundaryDetected(ctx, r.Client, plan, "Task")
	if derr != nil {
		return ctrl.Result{}, derr
	}
	if detected {
		return r.patchPlanSucceeded(ctx, plan)
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
//
// CR-02: increments tide_waves_dispatched_total{project,phase,plan} ONLY on the
// r.Create success path — not on AlreadyExists (watch-lag race) and not on the
// Get-existing branch (reconcile replay). This mirrors D-12's exactly-once shape:
// the reconcile whose Create succeeded already counted it; replays Get the Wave.
func (r *PlanReconciler) materializeWaves(ctx context.Context, plan *tideprojectv1alpha1.Plan, _ []tideprojectv1alpha1.Task, layers [][]dag.NodeID) error {
	logger := logf.FromContext(ctx)

	// Resolve metric label values once before the layer loop.
	// resolveProjectName is non-fatal — on error use "unknown" (Metric Label Sentinel,
	// Pitfall 4 — never emit an empty label value). Wave materialization proceeds
	// regardless of label resolution success.
	projectName, err := r.resolveProjectName(ctx, plan)
	if err != nil {
		projectName = "unknown"
	}
	phaseName := plan.Spec.PhaseRef
	if phaseName == "" {
		phaseName = "unknown"
	}

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
				// The reconcile that successfully created this Wave already counted it;
				// do NOT increment WavesDispatchedTotal here.
			} else {
				// Create succeeded — this is the once-only dispatch commit point.
				// Increment adjacent to logger.Info per CR-02 plan (D-23).
				tidemetrics.WavesDispatchedTotal.WithLabelValues(projectName, phaseName, plan.Name).Inc()
			}
			logger.Info("created wave", "wave", waveName, "index", i)
		} else {
			// Wave exists — ensure owner ref is set (may be missing on first reconcile
			// after a restart where the Wave was created but the Plan was not owner-set).
			// Do NOT increment WavesDispatchedTotal — this is a reconcile replay.
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

// resolveProjectName returns the Project name for this Plan via:
//  1. label fast-path (tideproject.k8s/project)
//  2. owner-ref chain walk via resolveProjectForPlan (Plan→Phase→Milestone→Project)
//  3. ErrParentUnresolved on miss (caller sets ConditionParentUnresolved)
//
// Phase 04.1 P1.4 removed the prior `projectList.Items[0]` fallback which
// silently mis-routed Plans in multi-Project namespaces.
func (r *PlanReconciler) resolveProjectName(ctx context.Context, plan *tideprojectv1alpha1.Plan) (string, error) {
	// Fast path: label stamped on this Plan.
	if name, ok := plan.Labels["tideproject.k8s/project"]; ok && name != "" {
		return name, nil
	}
	// Owner-ref chain walk: Plan→Phase→Milestone→Project (via Spec.PhaseRef).
	if project := r.resolveProjectForPlan(ctx, plan); project != nil {
		return project.Name, nil
	}
	return "", ErrParentUnresolved
}

// SetupWithManager wires the watch with a namespace-filter predicate per AUTH-02.
// Note: WaveReconciler handles Wave→Plan re-enqueue; PlanReconciler uses Owns(&Wave{})
// so it is notified when owned Waves are created/updated. Plan 04-05 also wires
// AnnotationChangedPredicate via a self-Watches handler so approve-plan /
// approve-wave-N annotation writes trigger reconciliation (T-04-G4 mitigation).
// The self-Watches pattern avoids filtering finalizer/owner-ref Update events
// at the For() level.
// Owns(&batchv1.Job{}): the plan planner Job is created by reconcilePlannerDispatch;
// when it transitions to terminal state the plan reconciler must re-run to call
// handlePlannerJobCompletion and materialize child Tasks. Without this Owns, the
// plan stays in Running indefinitely — the Job completion event never re-enqueues
// the plan (cascade-8 follow-on: plan controller missing Job watch).
func (r *PlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	annotationOnly := predicate.AnnotationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Plan{}).
		Owns(&tideprojectv1alpha1.Wave{}).
		Owns(&tideprojectv1alpha1.Task{}).
		Owns(&batchv1.Job{}).
		Watches(
			&tideprojectv1alpha1.Plan{},
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKeyFromObject(obj)}}
			}),
			builder.WithPredicates(annotationOnly),
		).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("plan").
		Complete(r)
}
