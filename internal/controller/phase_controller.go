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
	"k8s.io/client-go/tools/record"
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

	// HelmProviderDefaults carry Helm-chart provider/model defaults.
	HelmProviderDefaults ProviderDefaults

	// PricingOverridesJSON is the validated D-02 override JSON forwarded
	// opaquely to planner Jobs as TIDE_PRICING_OVERRIDES_JSON. Wired in Plan 14-05.
	PricingOverridesJSON string

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string

	// Recorder emits K8s Events for observable parent-ref-resolution failures
	// (defect #17). Nil-safe: every use is guarded by r.Recorder != nil.
	Recorder record.EventRecorder
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
				// defect #17: parent Milestone named by spec.milestoneRef does not
				// exist. Previously this was a SILENT Requeue (no condition, no
				// event) — a mismatched parent-ref wedged the whole subtree
				// invisibly. Surface it on Status + a Warning Event, then keep the
				// requeue so it self-heals if the parent later appears.
				r.surfaceParentRefUnresolved(ctx, &phase, "Milestone", phase.Spec.MilestoneRef)
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

	// 4b. D-03 (CUTS-01): backfill tideproject.k8s/project on the Phase
	// itself when the label is absent. Heals pre-Phase-15 CRs created by the
	// reporter before D-01 was in place. Guard: only patch when label is
	// missing so the second reconcile is a no-op (T-15-03 / idempotent).
	// Runs BEFORE reconcilePlannerDispatch so parked AwaitingApproval CRs
	// also self-heal on their first post-upgrade reconcile.
	if phase.Labels[owner.LabelProject] == "" {
		projectName := r.resolveProjectNameForPhase(ctx, &phase)
		if projectName != "" {
			patch := client.MergeFrom(phase.DeepCopy())
			if phase.Labels == nil {
				phase.Labels = map[string]string{}
			}
			phase.Labels[owner.LabelProject] = projectName
			if err := r.Patch(ctx, &phase, patch); err != nil {
				return ctrl.Result{}, fmt.Errorf("backfill project label on phase %s: %w", phase.Name, err)
			}
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

// resolveProjectNameForPhase returns the Project name for a Phase via the
// Phase→Milestone→Project chain (max 2 Gets). Returns "" if the chain cannot
// be resolved (orphan) — caller should skip the backfill silently.
func (r *PhaseReconciler) resolveProjectNameForPhase(ctx context.Context, ph *tideprojectv1alpha1.Phase) string {
	if ph.Spec.MilestoneRef == "" {
		return ""
	}
	var ms tideprojectv1alpha1.Milestone
	if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		return ""
	}
	if ms.Spec.ProjectRef == "" {
		return ""
	}
	var p tideprojectv1alpha1.Project
	if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return ""
	}
	return p.Name
}

// reconcilePlannerDispatch mirrors MilestoneReconciler one level down.
// Dispatches tide-phase-<phase-uid>-<attempt>; on completion materializes
// Plan child CRDs from EnvelopeOut.ChildCRDs.
func (r *PhaseReconciler) reconcilePlannerDispatch(ctx context.Context, ph *tideprojectv1alpha1.Phase) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	if ph.Status.Phase == "Succeeded" || ph.Status.Phase == "Failed" {
		return ctrl.Result{}, nil
	}

	// Step 1a: AwaitingApproval early-return (D-01 parity with milestone_controller.go).
	// Stops the finding-2 oscillation where a Phase parked at AwaitingApproval would
	// fall through to the idempotency guard and re-enter the planner dispatch body on
	// every reconcile (RESEARCH.md Pitfall 2). Two sub-cases:
	//   (a) no approve annotation → keep paused, return early (no requeue)
	//   (b) approve annotation present → D-04 two-step: consume annotation +
	//       patch Status.Phase=Running + ApprovedByUser condition, then Requeue.
	//       Succeeded fires ONLY via ChildCount-gated succession in handleJobCompletion.
	// Phase 12 D-01/D-04: approval never jumps a level to Succeeded past its children.
	if ph.Status.Phase == "AwaitingApproval" {
		if gates.CheckApprove(ph, "phase") {
			// Consume annotation (T-04-G2 one-shot).
			newAnno := gates.ConsumeApprove(ph, "phase")
			annoPatch := client.MergeFrom(ph.DeepCopy())
			ph.SetAnnotations(newAnno)
			if err := r.Patch(ctx, ph, annoPatch); err != nil {
				return ctrl.Result{}, err
			}
			// Return to Running + record ApprovedByUser condition (D-04).
			statusPatch := client.MergeFrom(ph.DeepCopy())
			ph.Status.Phase = "Running"
			meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
				Status:             metav1.ConditionFalse,
				Reason:             tideprojectv1alpha1.ReasonApprovedByUser,
				Message:            "Phase approved; children will dispatch",
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Patch(ctx, ph, statusPatch); err != nil {
				return ctrl.Result{}, err
			}
			// Requeue immediately — the Running branch calls handleJobCompletion
			// which owns the ChildCount-gated succession (D-03 invariant).
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	jobName := fmt.Sprintf("tide-phase-%s-1", ph.UID)

	if ph.Status.Phase == "Running" {
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: ph.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			// Planner Job is gone (TTL/GC) but the level is still Running: the planner
			// already completed and its envelope lives on the PVC keyed by UID, not on
			// the Job. Fall through to completion so succession fires instead of parking.
			return r.handleJobCompletion(ctx, ph, nil)
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

	// D-02 descent hold: if the parent Milestone is parked at AwaitingApproval,
	// hold Job dispatch here. The Phase stays at Status.Phase="" so tide approve's
	// findAwaitingPhase cannot target a held child instead of the parked parent
	// (12-RESEARCH.md Pitfall 5). NotFound parent is transient informer lag —
	// checkParentApproval returns (false, nil) and dispatch continues.
	if held, hErr := checkParentApproval(ctx, r.Client, ph.Namespace, ph.Spec.MilestoneRef, "Milestone"); hErr != nil {
		return ctrl.Result{}, hErr
	} else if held {
		logger.V(1).Info("dispatch held: parent Milestone awaiting approval",
			"phase", ph.Name, "milestone", ph.Spec.MilestoneRef)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// D-05 dispatch-entry reject hold — check reject annotation before acquiring the
	// pool or creating a Job. A rejected Project halts NEW dispatch; in-flight Jobs
	// drain (no Job deletion — resolved discretion call).
	{
		earlyProject := r.resolveProject(ctx, ph)
		if earlyProject != nil && gates.CheckRejected(earlyProject) {
			return r.patchPhaseRejected(ctx, ph, gates.RejectedReason(earlyProject))
		}
		// Phase 13 HALT-01 / D-05: third dispatch-entry hold (after CheckRejected +
		// parent-approval); park, never fail; cleared by tide resume.
		// Position: BEFORE pool acquire and BEFORE Job creation (Pitfall 2).
		// No per-Phase condition written (operator signal is the Project condition).
		if checkBillingHalt(earlyProject) {
			logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
				"phase", ph.Name)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		// Phase 14 BUDGET-02 / D-04: BudgetBlocked hold (operator cap) — separate from
		// BillingHalt (provider billing); both may be true simultaneously.
		// No per-Phase condition written (operator signal is the single Project BudgetBlocked condition).
		if checkBudgetBlocked(earlyProject) && !budget.IsBypassed(earlyProject, time.Now()) {
			logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked",
				"phase", ph.Name)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
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

	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindPlanner,
		ParentObj:            ph,
		Level:                "phase",
		Attempt:              attempt,
		Project:              project,
		SignedToken:          token,
		EnvelopeInJSON:       envInJSON,
		SubagentImage:        resolveImage(project, "phase", r.HelmProviderDefaults),
		CredproxyImage:       r.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              "tide-projects",
		ProjectUID:           projectUID,
		Caps:                 plannerCaps,
		PricingOverridesJSON: r.PricingOverridesJSON,
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

func (r *PhaseReconciler) handleJobCompletion(ctx context.Context, ph *tideprojectv1alpha1.Phase, completedJob *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	project := r.resolveProject(ctx, ph)
	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	// Phase 12 / Phase 04.1: reject short-circuit FIRST — operator stop should always
	// halt, regardless of envelope availability or read errors.
	// Mirrors plan_controller.go:470-471 ("reject short-circuit FIRST").
	// D-05: park (not fail) — in-flight Jobs drain; state is preserved for resume.
	if project != nil && gates.CheckRejected(project) {
		return r.patchPhaseRejected(ctx, ph, gates.RejectedReason(project))
	}

	// Read tiny status from the dispatch Job's termination message for budget
	// rollup and failure classification. ChildCRDs are NOT used here —
	// materialization has moved to the reporter Job (REQ-09-01). Continue through
	// gate + boundary-push logic regardless — those are envelope-independent.
	// Phase 04.1: previously a nil EnvReader short-circuited to patchSucceeded,
	// which skipped the boundary push trigger.
	// Phase 12 Pitfall 1 (parity with milestone_controller.go): track envReaderPresent
	// to distinguish nil-reader (unit-test fallback) from read error (transient).
	var out pkgdispatch.EnvelopeOut
	envReadOK := false
	envReaderPresent := r.EnvReader != nil
	if r.EnvReader != nil {
		var readErr error
		out, readErr = r.EnvReader.ReadOut(ctx, projectUID, string(ph.UID))
		if readErr != nil {
			// Non-fatal: log and defer to hasChildPlans fallback.
			logger.Error(readErr, "phase planner envelope tiny-status read failed (non-fatal); deferring to children-based succession", "phase", ph.Name)
		} else {
			envReadOK = true
		}
	} else {
		logger.V(1).Info("no env reader; skipping tiny-status read", "phase", ph.Name)
	}

	// Spawn the tide-reporter reader Job in the project namespace (Option C).
	// The reporter reads out.json from the PVC and materializes Plan children.
	// Children arrive via the Owns(&Plan{}) watch once the reporter creates them.
	// T-09-13: idempotent — AlreadyExists on Create is success.
	// isFirstCompletion: true when the reporter Job is newly spawned (plan 09-08).
	isFirstCompletion, spawnErr := spawnReporterIfNeeded(ctx, r.Client, r.Scheme, ph, project, "Phase", r.ReporterImage)
	if spawnErr != nil {
		return ctrl.Result{}, spawnErr
	}

	// Plan 09-08 Defect C: roll up planner-level Usage once per planner Job completion.
	if isFirstCompletion && envReadOK && project != nil {
		if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
			logger.Error(rollErr, "phase planner budget rollup failed (non-fatal)", "phase", ph.Name)
		}
	}

	// Phase 13 D-04 layer 2: backstop — classify planner-envelope failure Reason.
	if envReadOK && out.ExitCode != 0 && project != nil {
		var jobStart time.Time
		if completedJob != nil {
			jobStart = completedJob.CreationTimestamp.Time
		}
		if hErr := setBillingHaltIfNeeded(ctx, r.Client, project, out.Reason, jobStart); hErr != nil {
			logger.Error(hErr, "setBillingHaltIfNeeded failed (non-fatal)", "phase", ph.Name)
		}
	}

	// Plan 04-05: gate-policy hook (mirrors milestone_controller.go pattern).
	// Phase 12 D-04: if the phase already has an ApprovedByUser (or ResumedByUser)
	// condition, skip the park — don't re-park an already-approved level.
	if project != nil {
		policy := gates.EvaluatePolicy(project.Spec.Gates, "phase")
		if policy == gates.PolicyApprove || policy == gates.PolicyPause {
			alreadyApproved := false
			if c := meta.FindStatusCondition(ph.Status.Conditions, tideprojectv1alpha1.ConditionWaveOrLevelPaused); c != nil {
				if c.Status == metav1.ConditionFalse &&
					(c.Reason == tideprojectv1alpha1.ReasonApprovedByUser || c.Reason == tideprojectv1alpha1.ReasonResumedByUser) {
					alreadyApproved = true
				}
			}
			if !alreadyApproved {
				if !gates.CheckApprove(ph, "phase") {
					return r.patchPhaseAwaitingApproval(ctx, ph, policy)
				}
				// Annotation present at the hook (approved before park): consume +
				// write Running+ApprovedByUser so the condition is recorded.
				newAnno := gates.ConsumeApprove(ph, "phase")
				annoPatch := client.MergeFrom(ph.DeepCopy())
				ph.SetAnnotations(newAnno)
				if err := r.Patch(ctx, ph, annoPatch); err != nil {
					return ctrl.Result{}, err
				}
				statusPatch := client.MergeFrom(ph.DeepCopy())
				ph.Status.Phase = "Running"
				meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
					Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
					Status:             metav1.ConditionFalse,
					Reason:             tideprojectv1alpha1.ReasonApprovedByUser,
					Message:            "Phase approved; children will dispatch",
					LastTransitionTime: metav1.Now(),
				})
				if err := r.Status().Patch(ctx, ph, statusPatch); err != nil {
					return ctrl.Result{}, err
				}
				// Fall through to ChildCount-gated succession (D-03).
			}
			// alreadyApproved: fall through.
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

	// Fallback: EnvReader is nil (non-Option-C / unit-test path) OR had a read
	// error (envelope transiently absent). Use the prior hasChild-based behavior
	// with one extra guard: when the reader is PRESENT but returned an error, do
	// not fire the "no children → succeed" leaf path — the envelope may have had
	// ChildCount>0 but the read failed transiently. Only BoundaryDetected (all
	// children Succeeded) is safe to act on when the ChildCount is unknown.
	// Phase 12 Pitfall 1 fix (parity with milestone_controller.go).
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
	} else if envReaderPresent {
		// Reader exists but had a read error AND no children observed yet — the envelope
		// may have ChildCount>0 (children materializing). Requeue; don't auto-succeed.
		logger.V(1).Info("boundary push deferred: env reader present but unreadable, waiting (fallback)", "phase", ph.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	} else {
		logger.V(1).Info("boundary push skipped: phase has no child Plans (nil-EnvReader fallback)", "phase", ph.Name)
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

// patchPhaseRejected parks the Phase with a RejectedByUser condition WITHOUT
// writing Status.Phase=Failed (D-05). In-flight Jobs drain; state is preserved
// so clearing the reject annotation (tide resume) lets the level re-enter the
// normal dispatch path on the next reconcile.
// Returns RequeueAfter 5s so the park polls for the annotation clear.
func (r *PhaseReconciler) patchPhaseRejected(ctx context.Context, ph *tideprojectv1alpha1.Phase, reason string) (ctrl.Result, error) {
	patch := client.MergeFrom(ph.DeepCopy())
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionWaveOrLevelPaused,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha1.ReasonRejectedByUser,
		Message:            fmt.Sprintf("Rejected: %s", reason),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, ph, patch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
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

// surfaceParentRefUnresolved makes a parent-ref-NotFound stall observable
// (defect #17). It sets ConditionParentUnresolved (status False, reason
// ParentRefNotFound, message naming the missing parent) and emits a Warning
// Event, then returns — the caller keeps requeuing so the Phase self-heals if
// the parent appears later. Best-effort: a Status().Update failure is logged but
// not propagated, so the requeue still fires.
func (r *PhaseReconciler) surfaceParentRefUnresolved(ctx context.Context, ph *tideprojectv1alpha1.Phase, parentKind, parentRef string) {
	logger := logf.FromContext(ctx)
	msg := fmt.Sprintf("parent %s %q (spec.milestoneRef) not found in namespace %q; requeuing until it appears", parentKind, parentRef, ph.Namespace)
	meta.SetStatusCondition(&ph.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionParentUnresolved,
		Status:             metav1.ConditionFalse,
		Reason:             tideprojectv1alpha1.ReasonParentRefNotFound,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, ph); err != nil {
		logger.V(1).Info("surfaceParentRefUnresolved: status update failed (will retry on requeue)", "error", err)
	}
	if r.Recorder != nil {
		r.Recorder.Event(ph, corev1.EventTypeWarning, tideprojectv1alpha1.ReasonParentRefNotFound, msg)
	}
}

// SetupWithManager wires Owns(&Job{}) and Owns(&Plan{}) per D-A2. Plan 04-05
// adds AnnotationChangedPredicate via a self-Watches handler so approve/reject
// annotations trigger reconciliation (T-04-G4 mitigation — no polling). The
// self-Watches pattern avoids filtering finalizer/owner-ref Update events at
// the For() level (a GenerationChangedPredicate-based Or would do that).
func (r *PhaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		//nolint:staticcheck // SA1019: GetEventRecorderFor returns record.EventRecorder (the Recorder field type).
		r.Recorder = mgr.GetEventRecorderFor("phase-controller")
	}
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
