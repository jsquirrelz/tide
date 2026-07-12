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
	goErrors "errors"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/budget"
	"github.com/jsquirrelz/tide/internal/credproxy"
	"github.com/jsquirrelz/tide/internal/dispatch/podjob"
	"github.com/jsquirrelz/tide/internal/finalizer"
	"github.com/jsquirrelz/tide/internal/gates"
	tidemetrics "github.com/jsquirrelz/tide/internal/metrics"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/internal/pool"
	"github.com/jsquirrelz/tide/pkg/dag"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// pushResultEnvelope mirrors the JSON envelope emitted by cmd/tide-push
// (see cmd/tide-push/main.go pushResult). It is read from the push Pod's
// Status.ContainerStatuses[0].State.Terminated.Message — K8s
// terminationMessagePath default surface — so the ProjectReconciler can
// classify the push outcome by Reason without mounting the PVC.
//
// Phase 4 W-1: the Reason field is the source of truth for the exit-10
// vs exit-11 split. Plan 04-06 task 1 added this parsing.
type pushResultEnvelope struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	ProjectUID string `json:"projectUID"`
	Branch     string `json:"branch"`
	HeadSHA    string `json:"headSHA"`
	ExitCode   int    `json:"exitCode"`
	Reason     string `json:"reason"`

	// Phase 34 D-09/D-12: extended detail fields for the integration-miss
	// gate. JSON tags MUST match cmd/tide-push/main.go's pushResult struct
	// exactly (cross-binary contract). MissingBranches is truncated to the
	// first 10 (sorted) with MissingTotal carrying the full count
	// (termination-log 4096-byte cap). ConflictBranch names the task branch
	// that hit a genuine merge conflict (reason="merge-conflict").
	MissingBranches []string `json:"missingBranches,omitempty"`
	MissingTotal    int      `json:"missingTotal,omitempty"`
	ConflictBranch  string   `json:"conflictBranch,omitempty"`

	// Phase 35 (BASE-02/BASE-03): clone-mode envelope fields (CloneResult).
	// BaseSHA is the resolved 40-hex commit the run branch was created from —
	// stamped into status.git.baseSHA on clone success (D-11 provenance).
	// BaseRef echoes the ref as given by the operator; the WR-03 classification
	// branch compares it to the current spec.git.baseRef so a stale envelope
	// (operator edited the spec while a failed Job still exists) does not halt
	// on the old ref. JSON keys MUST match cmd/tide-push/main.go's pushResult.
	BaseSHA string `json:"baseSHA,omitempty"`
	BaseRef string `json:"baseRef,omitempty"`
}

// readPushEnvelope locates the first Pod belonging to the named push Job
// (label `job-name=<pushJobName>`) and parses its container[0]
// State.Terminated.Message as a pushResultEnvelope JSON document. Returns
// (envelope, true) on a successful parse; (zero, false) when no pod or no
// terminationMessage exists, or the body is unparseable.
//
// This is the operator-visible source of the push outcome's `reason` — the
// W-1 exit-10 leak path depends on this surface returning
// reason="leak-detected" so the leak-blocked metric fires.
func (r *ProjectReconciler) readPushEnvelope(ctx context.Context, namespace, pushJobName string) (pushResultEnvelope, bool) {
	// Phase 34 plan 34-02: delegate to the package-level helper (git_writer.go)
	// so PlanReconciler can reuse the identical termination-log-only read path
	// (Pitfall 2) to classify wave-integration Job failures.
	return readJobPushEnvelope(ctx, r.Client, namespace, pushJobName)
}

const (
	projectFinalizer = "tideproject.k8s/project-cleanup"
	// conditionTypeCycleDetected is the shared name for both the Project's
	// CycleDetected status condition Type and the Plan's ValidationState value —
	// the same "a cycle was detected" signal surfaced at two levels.
	conditionTypeCycleDetected = "CycleDetected"
	// finalizerCleanupTimeout bounds every finalizer cleanup callback (Pitfall 21).
	finalizerCleanupTimeout = 5 * time.Minute
	// defaultSharedPVCName is the cluster-wide PVC provisioned by the Helm chart (Plan 12).
	defaultSharedPVCName = "tide-projects"
	// initJobBusyboxImage is the init Job container image (Plan 12 Helm value images.busybox).
	initJobBusyboxImage = "busybox:1.36"
	// initJobRequeueAfterNoPVC is the requeue interval when the shared PVC is absent (Pitfall 1).
	initJobRequeueAfterNoPVC = 30 * time.Second
	// bypassPushLeaseAnnotation is the Project annotation that clears
	// PhasePushLeaseFailed and triggers a retry push (Phase 3 D-B6, mirrors
	// Phase 2 D-D4 budget-bypass annotation pattern).
	bypassPushLeaseAnnotation = "tideproject.k8s/bypass-push-lease"

	// maxBoundaryPushAttempts caps the controller-driven boundary-push
	// auto-retry (debug defect #13b). Once Status.BoundaryPush.Attempts reaches
	// this constant the controller STOPS dispatching push Jobs and sets
	// BoundaryPushed=False/PushFailed — bounded recovery, never a push-loop.
	// Small constant: a transient remote/auth/network failure clears well
	// within 5 attempts; a persistent failure surfaces as PushFailed for the
	// operator rather than looping forever.
	maxBoundaryPushAttempts = 5

	// boundaryPushBaseBackoff is the first capped-exponential requeue delay
	// between boundary-push retries (2m → 4m → 8m → … capped at
	// boundaryPushMaxBackoff). The push Job's own BackoffLimit handles
	// in-Job pod retries; this is the controller-level inter-attempt spacing.
	boundaryPushBaseBackoff = 2 * time.Minute
	// boundaryPushMaxBackoff caps the capped-exponential requeue delay.
	boundaryPushMaxBackoff = 15 * time.Minute

	// cloneEnvelopeReadCutoff bounds the read-before-flip wait on the clone
	// success arm (Phase 35 D-11, RESEARCH Pattern 2). When the clone Job has
	// Succeeded but its CloneResult envelope is not yet readable (pod not
	// observed, termination message empty), the controller requeues instead of
	// flipping CloneComplete with a lost baseSHA. Once the Job's completionTime
	// is older than this cutoff the controller flips anyway with an empty
	// baseSHA and logs degraded provenance. This MUST stay well under the clone
	// Job's 300s TTLSecondsAfterFinished: if the requeue loop outlived TTL GC,
	// IsNotFound would fire, the dispatch guard would re-clone, and the retried
	// clone would hit EnsureRunBranch's run-branch-exists early return (Pitfall
	// 6) — producing a no-fresh-baseSHA envelope and stalling the flip forever.
	cloneEnvelopeReadCutoff = 60 * time.Second
)

// ProjectReconciler reconciles a Project object at Standard depth (D-C1):
// fetch, finalizer-on-delete, finalizer-ensure-on-create, owner-ref-on-children
// (Project has no parent), status condition propagation, Status().Update.
//
// The Deps.Dispatcher field is nil in Phase 1; Phase 2 (REQ-SUB-01) injects a
// real dispatch.Dispatcher and fills the `if r.Deps.Dispatcher != nil { ... }`
// body.
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

	// Deps carries the dispatch-tier dependencies shared with the
	// Milestone/Phase/Plan reconcilers (plan 41-06 consolidation; RESEARCH
	// Pitfall 2 — Project is included to avoid repeating the forgotten-
	// Dispatcher bug class).
	Deps PlannerReconcilerDeps

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
// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch

// Reconcile implements the six-step Standard-depth Reconcile pattern.
func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var project tidev1alpha3.Project
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
		// Requeue explicitly — the finalizer Update changes neither generation nor
		// annotations, so the For()-level predicate.Or(GenerationChangedPredicate,
		// AnnotationChangedPredicate) filters out the resulting Update event. Without
		// a self-requeue the Project would park at empty Status.Phase until the
		// default 10h resync, never reaching reconcileProjectPhase2 (init Job + dispatch).
		return ctrl.Result{Requeue: true}, nil
	}

	// 4. Owner refs on children — Project is top-level; no parent to reference.

	// 4a. Version-crank migration guards (SCHEMA-03 + DEPS-03, generalized by D-04).
	// Re-fetch the Project (v1alpha3 is the sole served+storage version). If the
	// Get fails (e.g., a race with deletion), the guards are skipped gracefully —
	// the CRD admission webhook is the primary gate; this is belt-and-suspenders.
	var v2project tidev1alpha3.Project
	if v2GetErr := r.Get(ctx, req.NamespacedName, &v2project); v2GetErr == nil {
		// Schema-revision guard: reject objects authored under a prior schema
		// revision that slipped through.
		if blocked, gErr := r.checkSchemaRevisionGuard(ctx, &v2project); blocked {
			return ctrl.Result{}, gErr
		}
		// Assemble the global dep graph ONCE per reconcile (Pitfall 7 — avoids
		// double List calls from checkGlobalCycleGate + deriveGlobalWaves each
		// calling the assembler independently). Both the cycle gate and the wave
		// derivation step (Plan 03) consume the same (nodes, edges). The assembler
		// also returns the task slice so deriveGlobalWaves can re-use it without
		// a second List (IN-02 / WR-03 assemble-once contract).
		depNodes, depEdges, asmTasks, asmErr := r.assembleProjectDepGraph(ctx, &v2project)
		if asmErr != nil {
			return ctrl.Result{}, fmt.Errorf("assemble dep graph for project %s: %w", v2project.Name, asmErr)
		}
		// Global cross-scope cycle gate: detect task-level dep cycles across plans.
		// Returns the computed wave schedule so deriveGlobalWaves need not recompute
		// (WR-03 — ComputeWaves runs exactly once per reconcile).
		blocked, globalWaves, gErr := r.checkGlobalCycleGate(ctx, &v2project, depNodes, depEdges)
		if blocked {
			return ctrl.Result{}, gErr
		}
		if gErr != nil {
			return ctrl.Result{}, gErr
		}
		// Derive global waves and reconcile Wave CR set (EXEC-02/03/04, Plan 03).
		if waveErr := r.deriveGlobalWaves(ctx, &v2project, globalWaves, asmTasks); waveErr != nil {
			return ctrl.Result{}, fmt.Errorf("derive global waves for project %s: %w", v2project.Name, waveErr)
		}
	}

	// 5. Phase 2: dispatcher seam — init Job + budget gate + bypass watch (REQ-SUB-01).
	if r.Deps.Dispatcher != nil {
		return r.reconcileProjectPhase2(ctx, &project)
	}

	// 6. Update status conditions and persist via Status().Update.
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             tidev1alpha3.ReasonInitialized,
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
func (r *ProjectReconciler) reconcileProjectPhase2(ctx context.Context, project *tidev1alpha3.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	now := time.Now()

	// Step 1: Budget cap check + bypass annotation handling.
	result, err := r.handleBudgetGate(ctx, project, now)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If the project is in BudgetExceeded and bypass did not clear it, halt dispatch.
	if project.Status.Phase == tidev1alpha3.PhaseBudgetExceeded {
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
	// Belt-and-suspenders guard (D-01 / BYPASS-01): skip init-Job dispatch when
	// the workspace is already initialized. BranchName is stamped by
	// reconcilePhase3Lifecycle Step 1 after a successful init, so a non-empty
	// BranchName is a durable "workspace exists" sentinel. This prevents the
	// TTL-GC'd init Job (IsNotFound after 300s) from triggering a destructive
	// workspace re-init on resume.
	if project.Status.Git.BranchName != "" {
		return r.reconcilePhase3Lifecycle(ctx, project)
	}
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
	result, hErr := r.handleInitJobCompletion(ctx, project, &existingJob)
	if hErr != nil {
		return result, hErr
	}
	// Step 5 (Phase 3): once Initialized, run the Phase 3 lifecycle
	// (branch-name init, clone Job, push Job, bypass-annotation handling).
	if project.Status.Phase == tidev1alpha3.PhaseInitialized || project.Status.Phase == tidev1alpha3.PhaseRunning ||
		project.Status.Phase == tidev1alpha3.PhasePushLeaseFailed || project.Status.Phase == tidev1alpha3.PhaseComplete {
		return r.reconcilePhase3Lifecycle(ctx, project)
	}
	return result, nil
}

// ensureInitJob creates the one-shot init Job (idempotent — AlreadyExists is success).
func (r *ProjectReconciler) ensureInitJob(ctx context.Context, project *tidev1alpha3.Project, pvcName string) error {
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
func (r *ProjectReconciler) handleInitJobCompletion(ctx context.Context, project *tidev1alpha3.Project, job *batchv1.Job) (ctrl.Result, error) {
	if isJobSucceeded(job) {
		// Cascade 13 idempotency guard: handleInitJobCompletion is called on
		// every reconcile pass — the init Job remains Succeeded permanently
		// after first completion. Without this guard, the function re-stomps
		// Phase=Initialized on every reconcile, clobbering forward Phase
		// transitions (Complete, PushLeaseFailed, PushLeakBlocked, Running).
		// That breaks the push-Job-failed branch at line ~480 which is gated
		// on Phase==Complete at line ~440 — push_lease Tests 3+4 timed out
		// observing Phase=Initialized instead of Phase=PushLeaseFailed.
		// Reference: .planning/debug/push-lease-phase-revert.md.
		switch project.Status.Phase {
		case tidev1alpha3.PhaseRunning,
			tidev1alpha3.PhaseComplete,
			tidev1alpha3.PhasePushLeaseFailed,
			tidev1alpha3.PhasePushLeakBlocked:
			// Phase has already advanced past Initialized — init-Job-completion
			// was processed in a prior reconcile. Skip the re-patch.
			return ctrl.Result{}, nil
		}
		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tidev1alpha3.PhaseInitialized
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tidev1alpha3.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             tidev1alpha3.ReasonInitialized,
			Message:            fmt.Sprintf("Init Job %s completed successfully", job.Name),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, patch); err != nil {
			return ctrl.Result{}, err
		}
		// Phase 3 D-B6: now that init succeeded, ensure the per-run branch
		// name is set on Status.Git.BranchName.
		return r.reconcilePhase3Lifecycle(ctx, project)
	}

	if isJobFailed(job) {
		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tidev1alpha3.PhaseInitFailed
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tidev1alpha3.ConditionFailed,
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

// reconcilePhase3Lifecycle implements the Phase 3 extension for the
// ProjectReconciler — clone Job dispatch + push Job dispatch at level
// boundary + branch lifecycle + lease writeback + bypass annotation.
//
// Step shape:
//  1. Branch-name init (D-B6): if Status.Git.BranchName == "", set it to
//     "tide/run-<project-name>-<unix-epoch>". Unix epoch only — refnames
//     cannot contain ":" so RFC3339 is forbidden.
//  2. Bypass annotation handling: if Status.Phase=PushLeaseFailed AND
//     the bypass annotation is set, clear the phase + consume the
//     annotation + requeue (Phase 2 D-D4 budget-bypass pattern).
//  3. Clone Job dispatch: if no clone Job for this Project exists, build
//     and create one (deterministic name `tide-clone-<project-uid>`).
//     AlreadyExists is idempotent success.
//  4. Push Job: when a level boundary completes (observed via the
//     Milestone/Phase/Plan child status), build and create a push Job
//     (deterministic name `tide-push-<project-uid>` — D-B5 serialization
//     key; AlreadyExists is idempotent success / requeue trigger).
//  5. Push Job completion: read the push-result envelope (termination log,
//     never the PVC copy — Pitfall 2); on success, patch
//     Status.Git.LastPushedSHA (Phase 34 D-14 — implemented in
//     reconcileBoundaryPush's success arm). On lease rejection, patch
//     Status.Phase=PushLeaseFailed + increment LeaseFailureCount.
//
// Plan 03-08 keeps the body skeletal — the production wiring for steps
// 4-5 (level-boundary detection, push-result envelope schema) lands in
// follow-up plans that wire cmd/manager end-to-end. The grep contract
// + the deterministic state transitions tested in envtest are the
// proof-of-shape Phase 3 needs.
//
//nolint:gocyclo // reconcile lifecycle is a flat sequence of state-transition arms; splitting would obscure the contract
func (r *ProjectReconciler) reconcilePhase3Lifecycle(ctx context.Context, project *tidev1alpha3.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Step -1 (Phase 34 D-13): consume the reset-boundary-push annotation
	// applied by `tide resume` (or directly via kubectl annotate). Runs
	// before the Complete check so the reset takes effect regardless of
	// which phase the Project is currently in.
	if v, ok := project.Annotations[gates.AnnotationResetBoundaryPush]; ok && v == "true" {
		if err := r.consumeResetBoundaryPushAnnotation(ctx, project); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Step -0.5: Bypass-annotation handling (D-B6 / D-D4 mirror). MUST run
	// before BOTH routes into reconcileBoundaryPush — the Step-0 Complete
	// fast-path and the Phase 34 mid-run push observation arm — because that
	// state machine's sticky ConditionPushLeaseFailed guard returns early,
	// making any consume placed after those routes unreachable while the
	// classified terminal-failed push Job still exists (it outlives the
	// failure by its 300s TTL). Keyed on the condition as well as the phase
	// so a Complete project whose Phase was re-asserted past PushLeaseFailed
	// still honors the operator's bypass.
	leaseCond := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionPushLeaseFailed)
	leaseFailed := project.Status.Phase == tidev1alpha3.PhasePushLeaseFailed ||
		(leaseCond != nil && leaseCond.Status == metav1.ConditionTrue)
	if leaseFailed {
		if v, ok := project.Annotations[bypassPushLeaseAnnotation]; ok && v == "true" {
			return r.consumeBypassPushLeaseAnnotation(ctx, project)
		}
		if project.Status.Phase == tidev1alpha3.PhasePushLeaseFailed {
			// Halted at PushLeaseFailed until bypass annotation lands.
			return ctrl.Result{}, nil
		}
	}

	// Step 0: Check if all owned Milestones have Succeeded → Complete.
	// IMPORTANT (debug #13b): reaching Complete does NOT short-circuit the
	// reconcile. Complete is the control-plane succession roll-up and is patched
	// by checkProjectComplete on boundary detection; the boundary push (landing
	// the run branch on the remote) is a SEPARATE concern handled by the bounded
	// retry state machine in reconcileBoundaryPush. The pre-#13b code returned
	// early on Complete, which left a failed boundary push with nothing to
	// re-attempt it (and a hollow Complete with nothing on the remote) — exactly
	// the #13b defect. So a Complete project fast-paths into the push state
	// machine instead of returning.
	complete, err := r.checkProjectComplete(ctx, project)
	if err != nil {
		return ctrl.Result{}, err
	}
	if complete || project.Status.Phase == tidev1alpha3.PhaseComplete {
		// The control-plane succession is done. Run ONLY the bounded
		// boundary-push retry state machine (debug #13b) — no further planner
		// dispatch, branch init, or clone on a Complete project.
		// dispatchIfMissing=true: this is the ONLY path allowed to CREATE the
		// first/retry push Job (Phase 34 D-14/OQ2 option b).
		return r.reconcileBoundaryPush(ctx, project, true)
	}

	// Phase 34 (RESEARCH Open Question 2, option b): observe a mid-run
	// (pre-Complete) boundary-push Job's terminal state even though this
	// Project has not reached Complete yet. Owns(&batchv1.Job{}) already
	// re-enqueues the Project on Job completion (SetupWithManager), so this
	// is cheap: Get the deterministic Job; if it exists and is terminal,
	// classify success/failure via the SAME state machine used at Complete
	// (dispatchIfMissing=false — a mid-run reconcile must never INITIATE a
	// push; only a level trigger does that). If it exists and is still
	// running, fall through — the Owns(Job) watch will re-enqueue on
	// completion. If it does not exist yet, there is nothing to observe.
	// Rationale: D-05 admits no unverified push class and a mid-run miss
	// must be diagnosable (D-12) rather than silently surfacing only at
	// Complete; a mid-run SUCCESS must also stamp LastPushedSHA (D-14)
	// immediately rather than going stale until Complete.
	if project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
		midRunPushJobName := fmt.Sprintf("tide-push-%s", project.UID)
		var midRunPush batchv1.Job
		midRunErr := r.Get(ctx, types.NamespacedName{Name: midRunPushJobName, Namespace: project.Namespace}, &midRunPush)
		switch {
		case midRunErr == nil && isJobTerminal(&midRunPush):
			return r.reconcileBoundaryPush(ctx, project, false)
		case midRunErr != nil && !apierrors.IsNotFound(midRunErr):
			return ctrl.Result{}, midRunErr
		}
		// Job absent, or present-and-running: fall through to normal dispatch below.
	}

	// Step 0b: Dispatch project-level planner Job (D-A2 5th dispatch site).
	//nolint:staticcheck // SA1019: result.Requeue is read here as part of the reconcile control-flow contract; behavior-preserving
	if result, err := r.reconcileProjectPlannerDispatch(ctx, project); err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}

	// Step 1: Branch-name init (D-B6). Format: tide/run-<project>-<unix>.
	// Unix epoch only — refnames cannot contain ":" so RFC3339 is forbidden.
	//
	// WRITE-ONCE via optimistic lock: the name embeds the current unix
	// second, so a reconcile acting on a STALE cached Project (BranchName
	// still "" in the informer while the live object was already stamped)
	// would coin a DIFFERENT name and a plain merge patch would silently
	// overwrite the first — after which the clone Job (EnsureRunBranch on
	// name-1), task envelopes, and every downstream git consumer disagree on
	// the run branch. The optimistic lock turns the stale re-stamp into a
	// Conflict; the requeued reconcile re-reads and sees the stamped value.
	if project.Status.Git.BranchName == "" {
		patch := client.MergeFromWithOptions(project.DeepCopy(), client.MergeFromWithOptimisticLock{})
		project.Status.Git.BranchName = fmt.Sprintf("tide/run-%s-%d", project.Name, time.Now().Unix())
		if err := r.Status().Patch(ctx, project, patch); err != nil {
			if apierrors.IsConflict(err) {
				// Stale read — another reconcile already stamped BranchName.
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("patch branch name: %w", err)
		}
		// Continue to clone dispatch on next reconcile.
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 2 (bypass-annotation handling) moved to Step -0.5 above: it must
	// precede the Complete fast-path and the mid-run push observation arm,
	// both of which return into reconcileBoundaryPush before reaching here
	// while the classified failed push Job still exists.

	// Step 3: Clone Job dispatch (D-B4 PVC layout init).
	pvcName := r.sharedPVCName()
	cloneJobName := fmt.Sprintf("tide-clone-%s", project.UID)
	var existingClone batchv1.Job
	cloneErr := r.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: project.Namespace}, &existingClone)
	if cloneErr != nil && !apierrors.IsNotFound(cloneErr) {
		return ctrl.Result{}, cloneErr
	}
	// Phase 35 D-07 (Pattern 4): generation-scoped clone-dispatch halt. A prior
	// clone Job that terminal-failed with an unresolvable baseRef stamps
	// CloneFailed=True/BaseRefUnresolvable with ObservedGeneration = this
	// generation. The halt is the CONDITION, not Job existence (Pitfall 2: the
	// 300s TTL GCs the failed Job, so an IsNotFound-only guard would re-dispatch
	// the same bad ref forever). A spec edit to baseRef bumps metadata.generation
	// → ObservedGeneration no longer matches → the halt releases and a fresh
	// clone dispatches (recovery = one kubectl edit, classify-don't-retry).
	baseRefHalted := false
	if cond := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionCloneFailed); cond != nil {
		baseRefHalted = cond.Status == metav1.ConditionTrue &&
			cond.Reason == tidev1alpha3.ReasonBaseRefUnresolvable &&
			cond.ObservedGeneration == project.Generation
	}

	// D-02 / BYPASS-02: gate clone dispatch on the durable CloneComplete flag.
	// IsNotFound alone is TTL-unreliable (clone Job TTL=300s; the Job may be GC'd
	// before a resume, causing a destructive re-clone into an existing workspace).
	if !project.Status.Git.CloneComplete && !baseRefHalted && apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
		cloneOpts := CloneOptions{TidePushImage: r.Deps.TidePushImage}
		// B6: wire the run branch name so tide-push calls EnsureRunBranch + provisions
		// the run worktree during clone (B5). project.Status.Git.BranchName is set by
		// the ProjectReconciler before dispatching the clone Job (reconcilePhase3Lifecycle
		// stamps BranchName in the same reconcile cycle that dispatches the clone Job).
		if project.Status.Git.BranchName != "" {
			cloneOpts.RunBranch = project.Status.Git.BranchName
		}
		// Phase 35 D-01/D-04: plumb the operator-selected baseRef to the clone
		// Job (Spec.Git is non-nil here per the guard above). tide-push resolves
		// it inside EnsureRunBranch; empty preserves default-HEAD behavior.
		cloneOpts.BaseRef = project.Spec.Git.BaseRef
		cloneJob := buildCloneJob(project, pvcName, cloneOpts, r.Scheme)
		if cErr := r.Create(ctx, cloneJob); cErr != nil {
			if !apierrors.IsAlreadyExists(cErr) {
				return ctrl.Result{}, fmt.Errorf("create clone job: %w", cErr)
			}
			// AlreadyExists: idempotent success.
		}
		logger.Info("created clone Job", "job", cloneJobName)
	}

	// D-02 / BYPASS-02: set-on-success — flip CloneComplete when the clone Job
	// reports terminal success. CloneComplete is NEVER set at dispatch time; a
	// failed clone leaves it false so dispatch is retried.
	//
	// Phase 35 D-11 (RESEARCH Pattern 2 — read-before-flip): the arm flips
	// CloneComplete exactly once, so it must read the CloneResult envelope FIRST
	// and stamp status.git.baseSHA in the SAME status patch — otherwise a
	// success whose envelope is momentarily unreadable would flip CloneComplete
	// and lose baseSHA permanently. On an unreadable envelope the arm requeues
	// (bounded by cloneEnvelopeReadCutoff) rather than flipping with empty
	// baseSHA on the first observation.
	if cloneErr == nil && existingClone.Status.Succeeded > 0 && !project.Status.Git.CloneComplete {
		env, ok := r.readPushEnvelope(ctx, project.Namespace, cloneJobName)
		if ok {
			patch := client.MergeFrom(project.DeepCopy())
			project.Status.Git.BaseSHA = env.BaseSHA
			project.Status.Git.CloneComplete = true
			// Clear any prior BaseRefUnresolvable halt: a spec edit released the
			// generation-scoped gate, a fresh clone ran, and it succeeded.
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               tidev1alpha3.ConditionCloneFailed,
				Status:             metav1.ConditionFalse,
				Reason:             "CloneSucceeded",
				Message:            "Clone Job succeeded; run branch provisioned.",
				ObservedGeneration: project.Generation,
				LastTransitionTime: metav1.Now(),
			})
			if pErr := r.Status().Patch(ctx, project, patch); pErr != nil {
				return ctrl.Result{}, fmt.Errorf("patch CloneComplete: %w", pErr)
			}
		} else {
			// Envelope not yet readable. If the Job completed longer ago than the
			// sub-TTL cutoff, flip anyway with empty baseSHA and log degraded
			// provenance (bounded); otherwise requeue and try again shortly.
			if ct := existingClone.Status.CompletionTime; ct != nil && time.Since(ct.Time) > cloneEnvelopeReadCutoff {
				logger.Info("clone Job succeeded but CloneResult envelope unreadable past cutoff; flipping CloneComplete with empty baseSHA (degraded provenance)",
					"job", cloneJobName, "completionTime", ct.Time)
				patch := client.MergeFrom(project.DeepCopy())
				project.Status.Git.CloneComplete = true
				if pErr := r.Status().Patch(ctx, project, patch); pErr != nil {
					return ctrl.Result{}, fmt.Errorf("patch CloneComplete: %w", pErr)
				}
			} else {
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		}
	}

	// WR-03 (Phase 27): terminal-failed clone arm. When the clone Job exhausts
	// its BackoffLimit it goes terminal-Failed (Failed>0, Succeeded==0). The
	// dispatch guard above is gated on IsNotFound(cloneErr), and set-on-success
	// is gated on Succeeded>0 — so a failed clone Job that still exists (not yet
	// TTL-GC'd) stalls the project for up to the clone Job TTL (300s) with no
	// progress signal: CloneComplete is never set and IsNotFound never fires.
	// Delete the failed Job so the next reconcile re-dispatches a fresh clone,
	// and surface a CloneFailed condition so an operator sees the stall+recovery.
	if cloneErr == nil && existingClone.Status.Failed > 0 && existingClone.Status.Succeeded == 0 && !project.Status.Git.CloneComplete {
		// Phase 35 D-06/D-07 (BASE-02, RESEARCH Pattern 3): classify the failure
		// BEFORE the generic delete-and-re-dispatch. An unresolvable baseRef is a
		// config error, not a transient one — halt re-dispatch (classify-don't-
		// retry) instead of hot-looping a bad ref through the 300s TTL.
		if env, ok := r.readPushEnvelope(ctx, project.Namespace, cloneJobName); ok && env.Reason == "baseref-unresolvable" {
			// Stale-envelope guard: only halt when the failed ref still matches
			// the current spec. If the operator already edited spec.git.baseRef
			// (a newer generation) while this stale failed Job lingers, fall
			// through to delete-and-re-dispatch so the corrected spec gets a
			// fresh clone rather than a halt keyed to the old ref.
			currentBaseRef := ""
			if project.Spec.Git != nil {
				currentBaseRef = project.Spec.Git.BaseRef
			}
			if env.BaseRef == currentBaseRef {
				condPatch := client.MergeFrom(project.DeepCopy())
				meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
					Type:   tidev1alpha3.ConditionCloneFailed,
					Status: metav1.ConditionTrue,
					Reason: tidev1alpha3.ReasonBaseRefUnresolvable,
					// D-06 Argo CD canonical wording; the ref reached the system
					// only through the admission Pattern (no control chars), so
					// the interpolated message is log/UI-safe (T-35-03).
					Message:            fmt.Sprintf("unable to resolve '%s' to a commit SHA; fix spec.git.baseRef to re-attempt the clone", env.BaseRef),
					ObservedGeneration: project.Generation,
					LastTransitionTime: metav1.Now(),
				})
				if pErr := r.Status().Patch(ctx, project, condPatch); pErr != nil {
					return ctrl.Result{}, fmt.Errorf("patch BaseRefUnresolvable condition: %w", pErr)
				}
				logger.Info("clone Job failed: baseRef unresolvable; halting re-dispatch (generation-scoped), Job left for TTL GC",
					"job", cloneJobName, "baseRef", env.BaseRef, "generation", project.Generation)
				// The halt is the CONDITION (Pitfall 2): do NOT delete the Job
				// and do NOT requeue — a spec edit bumps the generation and the
				// dispatch guard releases.
				return ctrl.Result{}, nil
			}
		}
		logger.Info("clone Job terminal-failed; deleting to re-dispatch", "job", cloneJobName, "failed", existingClone.Status.Failed)
		if dErr := r.Delete(ctx, &existingClone, client.PropagationPolicy(metav1.DeletePropagationBackground)); dErr != nil && !apierrors.IsNotFound(dErr) {
			return ctrl.Result{}, fmt.Errorf("delete failed clone job: %w", dErr)
		}
		condPatch := client.MergeFrom(project.DeepCopy())
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tidev1alpha3.ConditionCloneFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "CloneJobFailed",
			Message:            fmt.Sprintf("Clone Job %s reached terminal-Failed (failed=%d); deleted to re-dispatch", cloneJobName, existingClone.Status.Failed),
			LastTransitionTime: metav1.Now(),
		})
		if pErr := r.Status().Patch(ctx, project, condPatch); pErr != nil {
			return ctrl.Result{}, fmt.Errorf("patch CloneFailed condition: %w", pErr)
		}
		// Requeue so the next reconcile re-dispatches now that the failed Job is gone.
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 4 (boundary push): handled by reconcileBoundaryPush via the
	// Step-0 Complete fast-path above. A non-Complete project that reaches
	// here has no run branch to push yet, so there is nothing to do.

	return ctrl.Result{}, nil
}

// reconcileBoundaryPush is the bounded, controller-driven boundary-push retry
// state machine (debug defect #13b). It runs ONLY after the Project has reached
// Complete (the control-plane succession roll-up). Complete is NEVER gated on
// the push outcome — this method records a SEPARATE, non-terminal
// BoundaryPushed condition + a bounded retry tally on Status.BoundaryPush.
//
// State machine (boundary reached; project is Complete):
//
//   - Push Job Complete                → BoundaryPushed=True/Pushed, clear retry
//     state, STOP. The run branch is on the remote.
//   - Attempts >= cap                  → BoundaryPushed=False/PushFailed, emit a
//     Warning Event, STOP. Bounded — no push-loop.
//   - Push Job leak-detected (exit 10) → PhasePushLeakBlocked (operator recovery
//     path, unchanged from Phase 4 W-1); no auto-retry (a secret must be removed
//     by hand). Mirrored into the BoundaryPushed=False condition.
//   - Push Job lease-rejected (exit11) → PhasePushLeaseFailed (operator bypass
//     annotation recovery path, unchanged); no auto-retry.
//   - Push Job terminal-Failed (other / BackoffLimitExceeded) → delete the failed
//     Job, create a fresh one, increment attempts, set lastAttemptTime,
//     BoundaryPushed=False/Pushing, requeue with capped exponential backoff.
//   - Push Job pending/running         → BoundaryPushed=False/Pushing, requeue;
//     do NOT create a second Job (strict single-in-flight guard — the exact
//     pitfall class that caused the clone-recreation loop).
//   - No push Job yet                  → create the first one, increment
//     attempts, BoundaryPushed=False/Pushing, requeue.
//
// Idempotency: the boundary push pushes the already-integrated run-branch HEAD
// (per #13's tide-push fix), so re-creating the Job after a terminal failure
// converges — a re-push of an already-present HEAD is a no-op fast-forward.
//
//nolint:gocyclo // a flat state machine of mutually-exclusive arms; splitting obscures the contract
func (r *ProjectReconciler) reconcileBoundaryPush(ctx context.Context, project *tidev1alpha3.Project, dispatchIfMissing bool) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// No git target → nothing to push; nothing to observe.
	if project.Spec.Git == nil || project.Spec.Git.RepoURL == "" {
		return ctrl.Result{}, nil
	}

	// Already confirmed pushed — normally the terminal success arm. BUT a
	// BoundaryPushed=True that latched on an early push carrying only a strict SUBSET
	// of the cumulative artifact map (the [project]-only single-flight winner in a
	// fast auto-approve cascade, snapshotted before milestone/phase/plan rolled up to
	// Succeeded) would otherwise freeze those deeper levels off the run branch
	// forever: the stale-artifact supersede below sits behind this return, and the
	// staleness only becomes detectable AFTER the deeper levels materialize — by which
	// point this arm blocks it (DASH-02 artifact_staging:244). Re-check the terminal
	// push's staged map against the current one; stay terminal only when it carried
	// the full map. On a stale subset, reopen the condition and fall through so the
	// supersede re-dispatches a fresh full-map push whose landing re-captures
	// LastPushedSHA (idempotent clean-tree skip; bounded by maxBoundaryPushAttempts).
	if c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionBoundaryPushed); c != nil &&
		c.Status == metav1.ConditionTrue {
		pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
		var terminalPush batchv1.Job
		switch gErr := r.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &terminalPush); {
		case apierrors.IsNotFound(gErr):
			return ctrl.Result{}, nil // Job GC'd — nothing to supersede; stay terminal.
		case gErr != nil:
			return ctrl.Result{}, gErr
		}
		current, seErr := collectStageEnvelopes(ctx, r.Client, project)
		if seErr != nil || !isStaleArtifactPush(&terminalPush, current) {
			return ctrl.Result{}, nil
		}
		if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionFalse,
			tidev1alpha3.ReasonPushing,
			"terminal push staged a subset of the cumulative artifact map; superseding with the full map"); err != nil {
			return ctrl.Result{}, err
		}
		// fall through to the stale-artifact supersede below.
	}

	// Operator-recovery halt arms (leak / lease / merge-conflict). These are
	// distinct, sticky outcomes with their own recovery surfaces (remove the
	// secret; clear the bypass-push-lease annotation; `tide resume` after a
	// replan). Once set, the boundary-push state machine must NOT re-process
	// them every reconcile — the Step-0 Complete fast-path re-asserts
	// Phase=Complete on each pass, so without this guard the lease arm would
	// re-increment LeaseFailureCount in a loop.
	//
	// A MISS-reason IntegrationIncomplete is deliberately NOT an early-return
	// guard here: D-13 requires it to auto-clear the next time a verify+push
	// succeeds (e.g. after `tide resume` resets Attempts, or via a mid-run
	// observation of a Job dispatched by a different level's trigger). The
	// Attempts>=cap arm below is what keeps a parked miss from re-dispatching.
	// A CONFLICT-reason park IS a guard: retrying cannot fix a content
	// conflict, and without it the failed Job's 300s TTL erases the only
	// other obstacle to re-dispatch — the Complete fast-path would burn a
	// fresh doomed push every TTL cycle until the generic cap flipped the
	// terminal reason to PushFailed, losing the ConflictBranch detail.
	if c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionPushLeakBlocked); c != nil &&
		c.Status == metav1.ConditionTrue {
		return ctrl.Result{}, nil
	}
	if c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionPushLeaseFailed); c != nil &&
		c.Status == metav1.ConditionTrue {
		return ctrl.Result{}, nil
	}
	if c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionIntegrationIncomplete); c != nil &&
		c.Status == metav1.ConditionTrue && c.Reason == tidev1alpha3.ReasonMergeConflict {
		return ctrl.Result{}, nil
	}

	// Bounded-retry exhaustion arm. Re-derived from .status so the cap survives a
	// controller restart (no in-memory counter). Only declare terminal once —
	// guard on the existing condition reason so we don't re-emit the Event.
	if project.Status.BoundaryPush.Attempts >= maxBoundaryPushAttempts {
		if c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionBoundaryPushed); c == nil ||
			c.Reason != tidev1alpha3.ReasonPushFailed {
			// D-08: an integration-completeness miss parks with the sticky
			// IntegrationIncomplete condition (D-12 named detail) rather than
			// the generic PushFailed reason — the detail message was
			// captured at classification time into LastError (Job/pod may
			// be TTL'd by the time the cap is reached).
			if detail, ok := strings.CutPrefix(project.Status.BoundaryPush.LastError, integrationIncompleteLastErrorPrefix); ok {
				changed, err := r.setIntegrationIncompleteCondition(ctx, project, tidev1alpha3.ReasonIntegrationIncomplete, detail)
				if err != nil {
					return ctrl.Result{}, err
				}
				// Event only on the actual transition: this arm re-runs on
				// every reconcile of the parked project (BoundaryPushed keeps
				// Reason=Pushing here, so the outer once-only guard never
				// closes for the miss path).
				if changed && r.Recorder != nil {
					r.Recorder.Eventf(project, corev1.EventTypeWarning, tidev1alpha3.ReasonIntegrationIncomplete,
						"Boundary push exhausted %d/%d attempts on an integration-completeness miss: %s",
						project.Status.BoundaryPush.Attempts, maxBoundaryPushAttempts, detail)
				}
				return ctrl.Result{}, nil
			}
			if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionFalse,
				tidev1alpha3.ReasonPushFailed,
				fmt.Sprintf("Boundary push did not land after %d attempts; last error: %q",
					project.Status.BoundaryPush.Attempts, project.Status.BoundaryPush.LastError)); err != nil {
				return ctrl.Result{}, err
			}
			if r.Recorder != nil {
				r.Recorder.Eventf(project, corev1.EventTypeWarning, tidev1alpha3.ReasonPushFailed,
					"Boundary push exhausted %d/%d attempts; run branch %q not on remote (last error: %q)",
					project.Status.BoundaryPush.Attempts, maxBoundaryPushAttempts,
					project.Status.Git.BranchName, project.Status.BoundaryPush.LastError)
			}
			tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "exhausted").Inc()
		}
		return ctrl.Result{}, nil
	}

	pvcName := r.sharedPVCName()
	pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
	var existingPush batchv1.Job
	pErr := r.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &existingPush)
	if pErr != nil && !apierrors.IsNotFound(pErr) {
		return ctrl.Result{}, pErr
	}

	// Compute the cumulative planner-artifact staging map ONCE for this reconcile
	// pass. Threaded into every (re)dispatch so the project-Complete boundary push
	// always carries the freshest full map (closing the latent gap where
	// dispatchBoundaryPush staged none), and read against the stamped map of a
	// succeeded Job to detect a stale subset (Defect E / DASH-02). Best-effort:
	// mirror triggerBoundaryPush's degrade — a list error must not wedge the push.
	current, seErr := collectStageEnvelopes(ctx, r.Client, project)
	if seErr != nil {
		logger.Error(seErr, "collectStageEnvelopes failed during boundary push; proceeding without a staged map",
			"job", pushJobName)
		current = nil
	}

	// No push Job yet — create the first attempt, but ONLY if this call site
	// is allowed to initiate a push (Phase 34 D-14/OQ2 option b): the
	// Complete fast-path always may; a mid-run observation never does — a
	// level trigger (triggerBoundaryPush) owns initiation.
	if apierrors.IsNotFound(pErr) {
		if !dispatchIfMissing {
			return ctrl.Result{}, nil
		}
		return r.dispatchBoundaryPush(ctx, project, pvcName, pushJobName, project.Status.BoundaryPush.LastError, current)
	}

	// Push Job Complete — terminal success, but ONLY once the landed headSHA is
	// durably captured into the --force-with-lease anchor (D-B6 / Pitfall 13).
	if isJobSucceeded(&existingPush) {
		env, ok := r.readPushEnvelope(ctx, project.Namespace, pushJobName)
		if !ok || env.HeadSHA == "" {
			// The Job succeeded but its headSHA is not readable. Under the intentional
			// D-B5 / R-05 single-writer coupling, the shared tide-push-<uid> Job may
			// have been created by an EARLIER artifact- or level-boundary push whose
			// Pod has since been GC'd (TTLSecondsAfterFinished), or whose
			// terminationMessage is not yet populated at this tick. Going terminal
			// (BoundaryPushed=True) now would freeze Status.Git.LastPushedSHA empty
			// FOREVER — the terminal guard at the top of this method returns early on
			// every subsequent reconcile, so the anchor could never be captured and
			// every future push would degrade to a no-lease force push. Instead,
			// replace the stale Job with a fresh project-boundary push the state
			// machine OWNS: its Pod is fresh and its envelope headSHA is readable on
			// the next observation. The re-push is idempotent — a re-push of the
			// already-landed run-branch HEAD is a no-op fast-forward — and bounded by
			// maxBoundaryPushAttempts (dispatchBoundaryPush increments the tally).
			if delErr := r.deleteFailedPushJob(ctx, &existingPush); delErr != nil {
				return ctrl.Result{}, delErr
			}
			logger.Info("boundary push Job succeeded but headSHA is unreadable; re-dispatching a fresh owned push to capture the lease anchor",
				"job", pushJobName, "attempt", project.Status.BoundaryPush.Attempts, "cap", maxBoundaryPushAttempts)
			return r.dispatchBoundaryPush(ctx, project, pvcName, pushJobName, "headsha-unreadable", current)
		}

		// Defect E / DASH-02: the succeeded Job's headSHA is readable, but if it
		// staged only a STRICT SUBSET of the current cumulative map (an early D-B5/R-05
		// single-flight winner that snapshotted a [project]-only map before the
		// milestone/phase/plan children materialized), accepting it as terminal would
		// leave those levels off the run branch forever. Supersede it: delete the stale
		// Job and re-dispatch a fresh OWNED push carrying the FULL current map. The
		// restage is idempotent (37-02 clean-tree skip) and bounded by
		// maxBoundaryPushAttempts. This MUST run before the terminal patch so a
		// stale-map success never sets BoundaryPushed=True.
		if isStaleArtifactPush(&existingPush, current) {
			if delErr := r.deleteFailedPushJob(ctx, &existingPush); delErr != nil {
				return ctrl.Result{}, delErr
			}
			logger.Info("boundary push Job succeeded but staged a partial (subset) map; superseding with a fresh owned push carrying the full cumulative map",
				"job", pushJobName, "attempt", project.Status.BoundaryPush.Attempts, "cap", maxBoundaryPushAttempts)
			return r.dispatchBoundaryPush(ctx, project, pvcName, pushJobName, "stale-artifact-map", current)
		}

		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Git.LeaseFailureCount = 0
		project.Status.BoundaryPush.LastError = ""
		// The unreadable / empty-headSHA case was already handled above by the
		// re-dispatch guard (D-B6 / Pitfall 13), so env.HeadSHA is guaranteed
		// readable and non-empty here. Arm the --force-with-lease fence (D-14) in
		// the SAME patch that resets LeaseFailureCount — BoundaryPushed=True and
		// LastPushedSHA move together.
		project.Status.Git.LastPushedSHA = env.HeadSHA
		meta.RemoveStatusCondition(&project.Status.Conditions, tidev1alpha3.ConditionIntegrationIncomplete)
		if err := r.Status().Patch(ctx, project, patch); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionTrue,
			tidev1alpha3.ReasonPushed,
			fmt.Sprintf("Run branch %q pushed to remote (job %s)", project.Status.Git.BranchName, pushJobName)); err != nil {
			return ctrl.Result{}, err
		}
		tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "success").Inc()
		tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "success").Inc()
		logger.Info("boundary push landed on remote", "job", pushJobName, "branch", project.Status.Git.BranchName)
		return ctrl.Result{}, nil
	}

	// Push Job terminal-Failed — classify, then either halt (leak/lease/
	// conflict operator recovery) or auto-retry (generic/miss/BackoffLimitExceeded).
	if isJobFailed(&existingPush) {
		env, haveEnv := r.readPushEnvelope(ctx, project.Namespace, pushJobName)
		reason := ""
		if haveEnv {
			reason = env.Reason
		}

		switch reason {
		case "leak-detected":
			// Operator recovery path (Phase 4 W-1) — no auto-retry; a secret
			// must be removed from the staged diff by hand.
			patch := client.MergeFrom(project.DeepCopy())
			project.Status.Phase = tidev1alpha3.PhasePushLeakBlocked
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               tidev1alpha3.ConditionPushLeakBlocked,
				Status:             metav1.ConditionTrue,
				Reason:             "LeakDetected",
				Message:            fmt.Sprintf("Push Job %s blocked by gitleaks: secret pattern detected in diff", pushJobName),
				LastTransitionTime: metav1.Now(),
			})
			project.Status.BoundaryPush.LastError = "leak-detected"
			if err := r.Status().Patch(ctx, project, patch); err != nil {
				return ctrl.Result{}, err
			}
			tidemetrics.SecretLeakBlockedTotal.WithLabelValues(project.Name, "", "").Inc()
			tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "leak").Inc()
			return ctrl.Result{}, nil

		case "lease-rejected":
			// Operator bypass-annotation recovery path (Phase 3) — no auto-retry.
			patch := client.MergeFrom(project.DeepCopy())
			project.Status.Phase = tidev1alpha3.PhasePushLeaseFailed
			project.Status.Git.LeaseFailureCount++
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               tidev1alpha3.ConditionPushLeaseFailed,
				Status:             metav1.ConditionTrue,
				Reason:             "LeaseRejected",
				Message:            fmt.Sprintf("Push Job %s rejected by --force-with-lease", pushJobName),
				LastTransitionTime: metav1.Now(),
			})
			project.Status.BoundaryPush.LastError = "lease-rejected"
			if err := r.Status().Patch(ctx, project, patch); err != nil {
				return ctrl.Result{}, err
			}
			tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "lease").Inc()
			return ctrl.Result{}, nil

		case pushEnvelopeReasonMergeConflict:
			// D-09: a genuine content conflict parks IMMEDIATELY — no retry
			// budget burned on a problem retrying cannot fix. Mirrors the
			// lease-rejected sticky-arm shape but on ConditionIntegrationIncomplete
			// (D-11: push-gate outcome lives on the Project) with Reason=MergeConflict.
			patch := client.MergeFrom(project.DeepCopy())
			project.Status.BoundaryPush.LastError = pushEnvelopeReasonMergeConflict
			if err := r.Status().Patch(ctx, project, patch); err != nil {
				return ctrl.Result{}, err
			}
			msg := fmt.Sprintf("merge conflict integrating %s into %s: content problem, human needed — replan, then `tide resume --retry-failed`",
				env.ConflictBranch, project.Status.Git.BranchName)
			if _, err := r.setIntegrationIncompleteCondition(ctx, project, tidev1alpha3.ReasonMergeConflict, msg); err != nil {
				return ctrl.Result{}, err
			}
			tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "conflict").Inc()
			return ctrl.Result{}, nil

		default:
			// Generic terminal failure (BackoffLimitExceeded / auth / transient
			// remote / integration-incomplete miss). #13b bounded auto-retry:
			// delete the failed Job and create a fresh one, incrementing the
			// attempt tally. The cap guard at the top of this method stops
			// the loop; an integration-incomplete miss parks sticky (D-08)
			// at the cap instead of the generic PushFailed reason.
			lastErr := reason
			switch {
			case !haveEnv:
				lastErr = "envelope-unreadable"
				tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "transient").Inc()
			case lastErr == "integration-incomplete":
				// D-12: capture the named detail NOW — the Job/pod may be
				// TTL'd (300s) by the time the cap arm needs to render the
				// sticky condition message.
				lastErr = integrationIncompleteLastErrorPrefix + r.formatMissingBranchesMessage(ctx, project, env.MissingBranches, env.MissingTotal)
				tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "miss").Inc()
			case lastErr == "":
				// Terminal failure with no specific reason — the
				// BackoffLimitExceeded #13b class (e.g. empty commit / transient
				// remote). Record a generic marker so the operator-visible
				// LastError is never blank on a real failure.
				lastErr = "push-failed"
				tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "transient").Inc()
			default:
				tidemetrics.IntegrationOutcomesTotal.WithLabelValues(project.Name, "transient").Inc()
			}
			if delErr := r.deleteFailedPushJob(ctx, &existingPush); delErr != nil {
				return ctrl.Result{}, delErr
			}
			logger.Info("boundary push attempt failed; retrying",
				"job", pushJobName, "attempt", project.Status.BoundaryPush.Attempts,
				"cap", maxBoundaryPushAttempts, "lastError", lastErr)
			return r.dispatchBoundaryPush(ctx, project, pvcName, pushJobName, lastErr, current)
		}
	}

	// Push Job pending/running — single-in-flight guard. Do NOT create a second
	// Job; surface the in-flight state and requeue on capped backoff.
	if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionFalse,
		tidev1alpha3.ReasonPushing,
		fmt.Sprintf("Boundary push in flight (job %s, attempt %d/%d)",
			pushJobName, project.Status.BoundaryPush.Attempts, maxBoundaryPushAttempts)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: boundaryPushRequeue(project.Status.BoundaryPush.Attempts)}, nil
}

// integrationIncompleteLastErrorPrefix marks BoundaryPush.LastError as
// carrying a pre-formatted D-12 detail message (captured at classification
// time, since the Job/pod may be TTL'd by the time the cap-exhaustion arm
// needs to render the sticky condition) rather than a plain keyword.
const integrationIncompleteLastErrorPrefix = "integration-incomplete: "

// setIntegrationIncompleteCondition sets the sticky ConditionIntegrationIncomplete
// (Phase 34 D-08/D-09/D-11) with the given reason/message. Guards on
// (status, reason, message) equality so reconciles do not churn
// LastTransitionTime — mirrors setBoundaryPushedCondition's idiom. Returns
// whether the condition actually transitioned so callers can gate one-shot
// side effects (the cap-arm Warning event) on the real change, not the pass.
func (r *ProjectReconciler) setIntegrationIncompleteCondition(ctx context.Context, project *tidev1alpha3.Project, reason, message string) (bool, error) {
	existing := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionIntegrationIncomplete)
	if existing != nil && existing.Status == metav1.ConditionTrue && existing.Reason == reason && existing.Message == message {
		return false, nil
	}
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionIntegrationIncomplete,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	return true, r.Status().Patch(ctx, project, patch)
}

// formatMissingBranchesMessage renders the D-12 condition message naming
// each missing task + branch (the envelope's already-truncated
// MissingBranches list, plus the untruncated MissingTotal count). Best
// effort: maps a branch name (tide/wt-<uid>) back to its Task's .metadata.name
// by listing the project's Task CRs; if a Task can't be resolved (deleted,
// GC'd), the branch name alone is used.
func (r *ProjectReconciler) formatMissingBranchesMessage(ctx context.Context, project *tidev1alpha3.Project, missingBranches []string, missingTotal int) string {
	byUID := make(map[string]string)
	var taskList tidev1alpha3.TaskList
	if err := r.List(ctx, &taskList, client.InNamespace(project.Namespace),
		client.MatchingLabels{owner.LabelProject: project.Name}); err == nil {
		for i := range taskList.Items {
			byUID[string(taskList.Items[i].UID)] = taskList.Items[i].Name
		}
	}
	parts := make([]string, 0, len(missingBranches))
	for _, br := range missingBranches {
		uid := strings.TrimPrefix(br, "tide/wt-")
		if name, ok := byUID[uid]; ok {
			parts = append(parts, fmt.Sprintf("task %s (branch %s)", name, br))
		} else {
			parts = append(parts, fmt.Sprintf("branch %s", br))
		}
	}
	msg := fmt.Sprintf("%d branch(es) missing from the run branch: %s", missingTotal, strings.Join(parts, ", "))
	if missingTotal > len(missingBranches) {
		msg += fmt.Sprintf(" (showing first %d of %d)", len(missingBranches), missingTotal)
	}
	return msg
}

// dispatchBoundaryPush creates a fresh boundary-push Job, increments the bounded
// attempt tally + stamps lastAttemptTime, sets BoundaryPushed=False/Pushing, and
// requeues with capped exponential backoff. The Job pushes the already-
// integrated run-branch HEAD (idempotent per #13), so a re-create after a
// terminal failure converges.
func (r *ProjectReconciler) dispatchBoundaryPush(ctx context.Context, project *tidev1alpha3.Project, pvcName, pushJobName, lastErr string, stageEnvelopes []string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// D-02 single-flight gate: do not create a new git-writer Job while
	// another (wave-integration or boundary-push) is in flight for this
	// Project. Self-exclusion on pushJobName (Pitfall 7). Gate-wait is NOT
	// an attempt (Assumption/discretion note in 34-05): requeue WITHOUT
	// incrementing Attempts — attempts count dispatched Jobs, not waits.
	inFlight, gwErr := gitWriterInFlightCount(ctx, r.Client, project.Namespace, project.Name, pushJobName)
	if gwErr != nil {
		return ctrl.Result{}, fmt.Errorf("check git-writer in-flight count: %w", gwErr)
	}
	if inFlight > 0 {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	msg, mErr := buildCommitMessage("project", "")
	if mErr != nil {
		return ctrl.Result{}, fmt.Errorf("build commit message: %w", mErr)
	}
	// D-03/D-07: cumulative Succeeded-branch set, recomputed via a live List
	// — the project-level nil-branch site RESEARCH flagged.
	branches, bErr := succeededTaskBranches(ctx, r.Client, project.Namespace, project.Name)
	if bErr != nil {
		return ctrl.Result{}, fmt.Errorf("compute cumulative succeeded-task branches: %w", bErr)
	}
	pushOpts := PushOptions{
		TidePushImage:         r.Deps.TidePushImage,
		Branch:                project.Status.Git.BranchName,
		LastPushedSHA:         project.Status.Git.LastPushedSHA,
		CommitMessage:         msg,
		LeaksConfigMap:        project.Spec.Git.LeaksConfigRef,
		IntegrateTaskBranches: branches,
		// Defect E / DASH-02: thread the freshest cumulative staging map so the
		// project-Complete boundary push stages every planner-materialized level
		// (closing the latent gap where this dispatch carried no map at all).
		StageEnvelopes: stageEnvelopes,
	}
	pushJob := buildPushJob(project, pvcName, pushOpts, r.Scheme)
	if cErr := r.Create(ctx, pushJob); cErr != nil {
		if !apierrors.IsAlreadyExists(cErr) {
			return ctrl.Result{}, fmt.Errorf("create push job: %w", cErr)
		}
		// AlreadyExists: a deletion is still propagating (foreground) or a
		// concurrent reconcile won the race. Do not double-count; requeue.
		return ctrl.Result{RequeueAfter: boundaryPushRequeue(project.Status.BoundaryPush.Attempts)}, nil
	}

	now := metav1.Now()
	patch := client.MergeFrom(project.DeepCopy())
	project.Status.BoundaryPush.Attempts++
	project.Status.BoundaryPush.LastAttemptTime = &now
	project.Status.BoundaryPush.LastError = lastErr
	if err := r.Status().Patch(ctx, project, patch); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionFalse,
		tidev1alpha3.ReasonPushing,
		fmt.Sprintf("Boundary push dispatched (job %s, attempt %d/%d)",
			pushJobName, project.Status.BoundaryPush.Attempts, maxBoundaryPushAttempts)); err != nil {
		return ctrl.Result{}, err
	}
	tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "dispatched").Inc()
	logger.Info("dispatched boundary push", "job", pushJobName,
		"attempt", project.Status.BoundaryPush.Attempts, "cap", maxBoundaryPushAttempts, "integrateTaskBranches", len(branches))
	return ctrl.Result{RequeueAfter: boundaryPushRequeue(project.Status.BoundaryPush.Attempts)}, nil
}

// deleteFailedPushJob deletes a terminally-failed boundary-push Job so the
// deterministic tide-push-<uid> name is free for the bounded-retry replacement.
//
// Background propagation (not Foreground): the API server removes the Job object
// immediately and reaps its Pods asynchronously. Foreground propagation would
// leave the Job lingering behind a foreground finalizer until the GC controller
// runs — which never happens under envtest — so the same-named recreate would
// AlreadyExists forever. Background is correct here: the Pods are terminal and
// the run-branch push is idempotent, so there is nothing to serialize against.
func (r *ProjectReconciler) deleteFailedPushJob(ctx context.Context, job *batchv1.Job) error {
	policy := metav1.DeletePropagationBackground
	if err := r.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &policy}); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete failed boundary push job %s: %w", job.Name, err)
	}
	return nil
}

// consumeResetBoundaryPushAnnotation implements the D-13 recovery verb: reset
// Status.BoundaryPush.Attempts/LastError, remove a sticky
// ConditionIntegrationIncomplete, then consume (delete) the annotation in a
// SEPARATE metadata patch — mirrors the bypassPushLeaseAnnotation consumption
// pattern (:527 region) and the resumeRun re-fetch-between-patches discipline
// (annotations and status are different subresources with independent
// resourceVersion windows).
func (r *ProjectReconciler) consumeResetBoundaryPushAnnotation(ctx context.Context, project *tidev1alpha3.Project) error {
	logger := logf.FromContext(ctx)
	logger.Info("reset-boundary-push annotation present; resetting bounded-retry state", "project", project.Name)

	statusPatch := client.MergeFrom(project.DeepCopy())
	project.Status.BoundaryPush.Attempts = 0
	project.Status.BoundaryPush.LastError = ""
	meta.RemoveStatusCondition(&project.Status.Conditions, tidev1alpha3.ConditionIntegrationIncomplete)
	if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
		return fmt.Errorf("reset boundary-push status: %w", err)
	}

	annotPatch := client.MergeFrom(project.DeepCopy())
	newAnnotations := make(map[string]string, len(project.Annotations))
	for k, v := range project.Annotations {
		if k != gates.AnnotationResetBoundaryPush {
			newAnnotations[k] = v
		}
	}
	project.Annotations = newAnnotations
	if err := r.Patch(ctx, project, annotPatch); err != nil {
		return fmt.Errorf("consume reset-boundary-push annotation: %w", err)
	}
	return nil
}

// consumeBypassPushLeaseAnnotation implements the D-B6 operator recovery verb:
// consume (delete) the bypass annotation, dispose of the classified
// terminal-failed push Job, and clear the PushLeaseFailed phase + condition.
//
// Deleting the failed Job is load-bearing twice over:
//   - the Phase 34 mid-run observation arm re-enters reconcileBoundaryPush
//     whenever the deterministic tide-push-<uid> Job exists and is terminal —
//     with the condition freshly cleared, the failure-classification arm would
//     re-read the SAME lease-rejected envelope and re-enter PushLeaseFailed,
//     undoing the bypass within one reconcile;
//   - on a Complete project the fast-path's isJobFailed arm has always had the
//     same re-classification loop (pre-Phase-34 latent defect) — the bypass
//     only ever appeared to work because non-Complete projects never
//     re-observed the Job.
//
// The delete is guarded on isJobTerminal: a level trigger (triggerBoundaryPush
// has no lease-condition guard) may legitimately have a FRESH push Job in
// flight under the same deterministic name — that one must survive the bypass
// and be classified on its own terminal state.
func (r *ProjectReconciler) consumeBypassPushLeaseAnnotation(ctx context.Context, project *tidev1alpha3.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.Info("push-lease bypass annotation present; clearing PushLeaseFailed", "project", project.Name)

	// Consume the annotation.
	annotPatch := client.MergeFrom(project.DeepCopy())
	newAnnotations := make(map[string]string, len(project.Annotations))
	for k, v := range project.Annotations {
		if k != bypassPushLeaseAnnotation {
			newAnnotations[k] = v
		}
	}
	project.Annotations = newAnnotations
	if err := r.Patch(ctx, project, annotPatch); err != nil {
		return ctrl.Result{}, fmt.Errorf("consume bypass annotation: %w", err)
	}

	// Dispose of the classified terminal-failed push Job so neither the
	// mid-run arm nor the Complete fast-path re-classifies the bypassed
	// failure. Terminal-only: never delete a fresh in-flight push.
	pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
	var pushJob batchv1.Job
	getErr := r.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &pushJob)
	switch {
	case getErr == nil && isJobTerminal(&pushJob):
		if err := r.Delete(ctx, &pushJob, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete bypassed push job %s: %w", pushJobName, err)
		}
	case getErr != nil && !apierrors.IsNotFound(getErr):
		return ctrl.Result{}, getErr
	}

	// Clear PushLeaseFailed phase + condition. Preserve a non-lease phase
	// (e.g. Complete re-asserted by checkProjectComplete) — only the failure
	// phase itself transitions back to Running.
	statusPatch := client.MergeFrom(project.DeepCopy())
	if project.Status.Phase == tidev1alpha3.PhasePushLeaseFailed {
		project.Status.Phase = tidev1alpha3.PhaseRunning
	}
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionPushLeaseFailed,
		Status:             metav1.ConditionFalse,
		Reason:             tidev1alpha3.ReasonBypassApplied,
		Message:            "Push-lease failure bypassed by operator annotation",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// setBoundaryPushedCondition patches the non-terminal BoundaryPushed condition.
// It only writes when the (status, reason) actually changes so reconciles do not
// churn LastTransitionTime.
func (r *ProjectReconciler) setBoundaryPushedCondition(ctx context.Context, project *tidev1alpha3.Project, status metav1.ConditionStatus, reason, message string) error {
	existing := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionBoundaryPushed)
	if existing != nil && existing.Status == status && existing.Reason == reason && existing.Message == message {
		return nil
	}
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionBoundaryPushed,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	return r.Status().Patch(ctx, project, patch)
}

// boundaryPushRequeue returns the capped-exponential inter-attempt delay:
// 2m, 4m, 8m, … capped at boundaryPushMaxBackoff. attempts is the number of
// attempts already made (>= 1 after the first dispatch).
func boundaryPushRequeue(attempts int32) time.Duration {
	if attempts < 1 {
		return boundaryPushBaseBackoff
	}
	d := boundaryPushBaseBackoff
	for i := int32(1); i < attempts; i++ {
		d *= 2
		if d >= boundaryPushMaxBackoff {
			return boundaryPushMaxBackoff
		}
	}
	if d > boundaryPushMaxBackoff {
		return boundaryPushMaxBackoff
	}
	return d
}

// countChildMilestones returns the number of Milestones owned by this Project (plan 09-08).
// Used by the ChildCount-gated succession path in handleProjectJobCompletion.
func (r *ProjectReconciler) countChildMilestones(ctx context.Context, project *tidev1alpha3.Project) int {
	return countChildren(ctx, r.Client, project.Namespace, project.UID, &tidev1alpha3.MilestoneList{})
}

// checkProjectComplete returns true (and patches Status.Phase=Complete) when
// BoundaryDetected reports all owned Milestones have reached Succeeded.
// Returns false without patching when no Milestones exist yet (childless guard).
func (r *ProjectReconciler) checkProjectComplete(ctx context.Context, project *tidev1alpha3.Project) (bool, error) {
	detected, err := gates.BoundaryDetected(ctx, r.Client, project, "Milestone")
	if err != nil {
		return false, err
	}
	if !detected {
		return false, nil
	}
	patch := client.MergeFrom(project.DeepCopy())
	project.Status.Phase = tidev1alpha3.PhaseComplete
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             "MilestonesSucceeded",
		Message:            "All owned Milestones reached Succeeded",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, project, patch); err != nil {
		return true, err
	}
	return true, nil
}

// reconcileProjectPlannerDispatch is the D-A2 5th dispatch site — mirrors
// milestone_controller.go:reconcilePlannerDispatch one level up.
//
// Job name format (D-05): "tide-project-<uid>-1".
// Level string: "project". Project IS both the parent and the project parameter.
// No AwaitingApproval check (D-02: no gate at Project→Milestone level).
//
// Gated on len(r.Deps.SigningKey) > 0 — when SigningKey is not wired (test mode
// that doesn't configure dispatch), the function is a no-op so existing tests
// that only exercise clone/push lifecycle are unaffected.
//
//nolint:gocyclo // reconcile dispatch is a flat sequence of guard arms; splitting would obscure the contract
func (r *ProjectReconciler) reconcileProjectPlannerDispatch(ctx context.Context, project *tidev1alpha3.Project) (ctrl.Result, error) {
	// Guard: SigningKey is required to mint credproxy tokens — if not wired
	// (e.g. unit tests that only test clone/push lifecycle), skip dispatch.
	if len(r.Deps.SigningKey) == 0 {
		return ctrl.Result{}, nil
	}

	// Step 1: Terminal short-circuit.
	switch project.Status.Phase {
	case tidev1alpha3.PhaseComplete,
		tidev1alpha3.PhaseInitFailed:
		return ctrl.Result{}, nil
	}

	jobName := fmt.Sprintf("tide-project-%s-1", project.UID)

	// Step 2: On Running — check Job terminal state BEFORE the idempotency guard.
	// Mirrors milestone_controller.go:reconcilePlannerDispatch Step 2 (~286-301)
	// which runs BEFORE Step 2b (~304-326). The Step 1b idempotency guard in the
	// pre-fix code fired unconditionally when the Job existed, making the terminal
	// branch unreachable while the Job was still present — causing a ~10-min stall
	// (TTL/GC fallback at line 983) before handleProjectJobCompletion fired (QQH-01).
	if project.Status.Phase == tidev1alpha3.PhaseRunning {
		var job batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: jobName}, &job); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			// Planner Job is gone (TTL/GC) but the level is still Running: the planner
			// already completed and its envelope lives on the PVC keyed by UID, not on
			// the Job. Fall through to completion so succession fires instead of parking.
			return r.handleProjectJobCompletion(ctx, project, nil)
		}
		if isJobTerminal(&job) {
			return r.handleProjectJobCompletion(ctx, project, &job)
		}
		// Job is present and non-terminal (in-flight); do nothing.
		return ctrl.Result{}, nil
	}

	// Step 2b: Idempotency guard — skip dispatch when the planner Job already
	// exists (non-Running path only; the Running branch above handles both
	// terminal and in-flight cases). Gating on Job existence (rather than
	// owned-Milestone count) is safe for N>1 milestones: the N child Milestone
	// CRDs materialize incrementally after the planner runs, so a count-based
	// guard would fire mid-stream and abort the remaining N-1 Milestones. Job
	// presence is the single stable signal that the planner was already dispatched.
	{
		var existingJob batchv1.Job
		if err := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: jobName}, &existingJob); err == nil {
			// Planner Job already exists — planner already dispatched.
			return ctrl.Result{}, nil
		} else if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("idempotency: get planner job: %w", err)
		}
	}

	// Phase 13 HALT-01 / D-05: third dispatch-entry hold (after CheckRejected +
	// parent-approval); park, never fail; cleared by tide resume.
	// At the project level, the reconciled object IS the project — gate directly.
	// Position: BEFORE pool acquire and BEFORE Job creation (Pitfall 2).
	// No per-Project condition written (operator signal is the Project BillingHalt
	// condition itself; writing it here would be redundant and cause flapping).
	if checkBillingHalt(project) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
			"project", project.Name)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	// Phase 14 BUDGET-02 / D-04: BudgetBlocked hold (operator cap) — separate from
	// BillingHalt (provider billing); both may be true simultaneously.
	// No per-Project condition written (operator signal is the Project BudgetBlocked
	// condition itself; writing it here would be redundant).
	if checkBudgetBlocked(project) && !budget.IsBypassed(project, time.Now()) {
		logf.FromContext(ctx).V(1).Info("dispatch held: project budget blocked",
			"project", project.Name)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Phase 28 IMPORT-01: park planner dispatch until import completes.
	// Position: after terminal short-circuit and all earlier holds (Step 1),
	// BEFORE pool acquire (Pitfall 2 — parking after acquire leaks a slot).
	if project.Spec.ImportSource != nil {
		c := meta.FindStatusCondition(project.Status.Conditions, tidev1alpha3.ConditionImportComplete)
		if c == nil || c.Status != metav1.ConditionTrue {
			logf.FromContext(ctx).V(1).Info("import pending; holding planner dispatch", "project", project.Name)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	// Phase 31 D-01/D-02: durable project-planner suppression short-circuit.
	// Position: AFTER the import-pending hold (above) and BEFORE the live
	// r.List of owned Milestones (cache-independent) and BEFORE PlannerPool.Acquire
	// (D-06: no slot leak). When this condition is stamped True it is authoritative —
	// a cold informer cache cannot resurrect a paid project-planner dispatch (ADOPT-05).
	if project.Spec.ImportSource != nil {
		if suppCond := meta.FindStatusCondition(project.Status.Conditions,
			tidev1alpha3.ConditionProjectPlannerSuppressed); suppCond != nil &&
			suppCond.Status == metav1.ConditionTrue {
			logf.FromContext(ctx).V(1).Info(
				"project planner permanently suppressed (adoption complete); skipping dispatch",
				"project", project.Name,
			)
			return ctrl.Result{}, nil
		}
	}

	// Phase 30 RESUME-PARTIAL-02 / Phase 31 D-02: post-ImportComplete adoption guard.
	// When ImportComplete=True AND the Project has at least one owned Milestone,
	// the import tree is the authoritative materialization — the project planner
	// must not re-dispatch (run #2 defect: a fresh cluster had no prior planner
	// Job, so Step 2b above passed, and a paid project-planner Job fired
	// post-import). This guard permanently skips dispatch once the import tree
	// is confirmed present.
	//
	// Phase 31 D-02 upgrade: on first confirmation, the bare return is replaced
	// by a SINGLE Status().Patch that advances Phase=Running AND stamps
	// ConditionProjectPlannerSuppressed=True (Reason=AdoptionComplete) in one
	// MergeFrom patch (D-07 — never two sequential patches) before returning.
	// Subsequent reconciles hit the durable short-circuit above instead of
	// re-running this live List (D-01 cache-as-truth fix).
	//
	// Why this does NOT regress the N>1-milestone incremental-materialization
	// case (per the Step-2b comment above): that concern applies to a
	// count-based guard that could fire MID-STREAM while milestones are still
	// being created. This guard is gated on ImportComplete=True, which
	// reconcileCreatingCRs sets only AFTER materializing ALL seed nodes
	// (import_controller.go transitions CreatingCRs→CopyingEnvelopes only
	// after the full seed loop, then CopyingEnvelopes→Complete/ImportComplete=True
	// after the import Job). So when this arm fires the milestone list is always
	// complete — the mid-stream abort the Step-2b comment warns about cannot occur.
	if project.Spec.ImportSource != nil {
		if importCond := meta.FindStatusCondition(project.Status.Conditions,
			tidev1alpha3.ConditionImportComplete); importCond != nil &&
			importCond.Status == metav1.ConditionTrue {
			var msList tidev1alpha3.MilestoneList
			if listErr := r.List(ctx, &msList, client.InNamespace(project.Namespace)); listErr == nil {
				for i := range msList.Items {
					// WR-01: use UID-bound owner reference (mirrors countChildMilestones)
					// rather than a free-form name-string match on Spec.ProjectRef.
					// A stale Milestone from a prior Project incarnation with the same
					// name would collide on the name check and silently suppress a
					// legitimately-needed bootstrap dispatch.
					// reconcileCreatingCRs sets the owner ref via owner.EnsureOwnerRef
					// before client.Create, so the ref is present at guard time.
					if metav1.IsControlledBy(&msList.Items[i], project) {
						logf.FromContext(ctx).V(1).Info(
							"import adopted; stamping suppression condition and advancing to Running",
							"project", project.Name,
							"milestone", msList.Items[i].Name,
						)
						// D-07: batch Phase=Running advance + suppression condition into
						// ONE Status().Patch(MergeFrom(base)) — never two sequential patches.
						// D-04: set Phase=Running identically to the normal lifecycle.
						// D-06: return BEFORE PlannerPool.Acquire — no slot leak.
						// D-08: return nil (not err) for expected/permanent suppressed state.
						// WR-01: use MergeFromWithOptimisticLock so the "conflict is retryable"
						// comment is actually true — a plain MergeFrom performs a last-write-wins
						// server-side merge that embeds no resourceVersion and cannot conflict.
						patch := client.MergeFromWithOptions(project.DeepCopy(), client.MergeFromWithOptimisticLock{})
						project.Status.Phase = tidev1alpha3.PhaseRunning
						meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
							Type:               tidev1alpha3.ConditionProjectPlannerSuppressed,
							Status:             metav1.ConditionTrue,
							Reason:             tidev1alpha3.ReasonAdoptionComplete,
							Message:            "Project adopted from import; project-planner suppressed — import tree is authoritative",
							LastTransitionTime: metav1.Now(),
						})
						if pErr := r.Status().Patch(ctx, project, patch); pErr != nil {
							// Conflict is retryable (resourceVersion embedded by MergeFromWithOptimisticLock);
							// surface as err so controller requeues and re-fetches before retrying.
							return ctrl.Result{}, pErr
						}
						return ctrl.Result{}, nil
					}
				}
			}
			// List error or no owned Milestones: fall through to normal dispatch.
			// (A failed/empty import with no materialized Milestones may still
			// need a project planner run to bootstrap the milestone tree.)
		}
	}

	// Step 3a: D3 in-flight cap gate — BEFORE pool Acquire (D-03: no slot leak).
	// Counts non-terminal planner Jobs via a cached-client List; returns RequeueAfter
	// (never an error) when the count meets or exceeds the configured cap (CONCUR-04).
	if r.PlannerPool != nil {
		inFlight, err := plannerInFlightCount(ctx, r.Client, r.WatchNamespace)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("planner in-flight count: %w", err)
		}
		if inFlight >= r.PlannerPool.Capacity() {
			logf.FromContext(ctx).V(1).Info("planner dispatch deferred: concurrency cap reached",
				"inFlight", inFlight, "cap", r.PlannerPool.Capacity(), "project", project.Name)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	// Step 3b: Acquire PlannerPool (POOL-01) before creating the Job (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, err
		}
		defer r.PlannerPool.Release()
	}

	// Step 4: Build caps.
	plannerCaps := podjob.DefaultCaps(nil, podjob.JobKindPlanner)
	if plannerCaps.Iterations <= 0 {
		plannerCaps.Iterations = defaultPlannerIterations
	}

	// Step 5: Build planner envelope.
	// For ProjectReconciler: level="project", parent=project, project=project (same object).
	attempt := 1
	envIn, envInJSON, err := BuildPlannerEnvelope("project", project, project, attempt, "", project.Spec.OutcomePrompt, pkgdispatch.Caps{
		WallClockSeconds: int(plannerCaps.WallClockSeconds),
		Iterations:       int(plannerCaps.Iterations),
	}, credproxyEndpoint, r.Deps.HelmProviderDefaults, "" /* project is the root; no parent SharedContext */)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build project planner envelope: %w", err)
	}

	// Step 6: Mint signed token for the credproxy sidecar.
	token, err := credproxy.Sign(r.Deps.SigningKey, string(project.UID), time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("mint project planner signed token: %w", err)
	}

	// Step 7: Resolve secretUID from ProviderSecretRef.
	var secretUID string
	if project.Spec.ProviderSecretRef != "" {
		var secret corev1.Secret
		if sErr := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: project.Spec.ProviderSecretRef}, &secret); sErr == nil {
			secretUID = string(secret.UID)
		}
	}

	// Step 9: Build + Create planner Job via shared BuildJobSpec.
	// SIGN-01 / D-03: resolve committer/author identity (mirrors resolveImage's
	// HelmProviderDefaults tier) and stamp it into the planner Job env.
	agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
	resolvedImage := resolveImage(project, "project", r.Deps.HelmProviderDefaults)
	// D-02 / T-40-12: log the resolved model at dispatch — previously the
	// resolved model appeared nowhere outside the PVC envelope.
	logf.FromContext(ctx).Info("resolved subagent dispatch", "level", "project", "model", envIn.Provider.Model, "image", resolvedImage)
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindPlanner,
		ParentObj:            project,
		Level:                "project",
		Attempt:              attempt,
		Project:              project,
		SignedToken:          token,
		EnvelopeInJSON:       envInJSON,
		SubagentImage:        resolvedImage,
		AgentName:            agentName,
		AgentEmail:           agentEmail,
		CredproxyImage:       r.Deps.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              r.sharedPVCName(),
		ProjectUID:           string(project.UID),
		Caps:                 plannerCaps,
		PricingOverridesJSON: r.Deps.PricingOverridesJSON,
	}
	job := podjob.BuildJobSpec(opts)
	if err := owner.EnsureOwnerRef(job, project, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure owner ref on project planner job: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create project planner job: %w", err)
		}
		// AlreadyExists: idempotent success.
	}

	// Step 10: Patch Status.Phase=Running + Condition AuthoringPlanner=True.
	patch := client.MergeFrom(project.DeepCopy())
	project.Status.Phase = tidev1alpha3.PhaseRunning
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tidev1alpha3.ConditionAuthoringPlanner,
		Status:             metav1.ConditionTrue,
		Reason:             "PlannerDispatched",
		Message:            fmt.Sprintf("Planner Job %s dispatched", jobName),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, project, patch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleProjectJobCompletion reads tiny status from the completed planner Job's
// termination message (usage/git/exitCode/reason for budget rollup and failure
// classification), then spawns the tide-reporter reader Job in the project
// namespace to materialize child Milestone CRDs from the PVC-side out.json.
//
// Materialization is now the reporter Job's job (Phase 09 plan 09-06, REQ-09-01).
// The Manager no longer reads ChildCRDs from the cross-namespace PVC; children
// arrive via the existing Owns(&Milestone{}) watch once the reporter creates them.
//
// T-09-13 mitigation: spawn is idempotent — AlreadyExists on Create is treated
// as success, so a re-enqueue from the reporter Job's own completion does no harm.
//
//nolint:unparam // ctrl.Result kept so callers can `return r.handleProjectJobCompletion(...)` in the reconcile chain
func (r *ProjectReconciler) handleProjectJobCompletion(ctx context.Context, project *tidev1alpha3.Project, completedJob *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Read tiny status from the dispatch Job's termination message for budget
	// rollup and failure classification. The ChildCRDs field is NOT used here —
	// materialization has moved to the reporter Job (REQ-09-01).
	// Plan 09-08: capture out so we can gate on out.ChildCount below.
	var out pkgdispatch.EnvelopeOut
	envReadOK := false
	if r.Deps.EnvReader != nil {
		// project is both the top-level object and its own "parent" at this level;
		// use project.UID as both projectUID and parentUID (the envelope is keyed by
		// the parent's UID, and the Project IS the parent at the project level).
		var readErr error
		out, readErr = r.Deps.EnvReader.ReadOut(ctx, string(project.UID), string(project.UID))
		if readErr != nil {
			logger.Error(readErr, "project planner envelope tiny-status read failed (non-fatal)", "project", project.Name)
		} else {
			envReadOK = true
		}
	} else {
		logger.V(1).Info("no env reader; skipping tiny-status read", "project", project.Name)
	}

	// Spawn the tide-reporter reader Job in the project namespace (Option C).
	// The reporter reads out.json from the namespace PVC and materializes Milestone
	// children via the K8s API. Children arrive via the Owns(&Milestone{}) watch.
	// isFirstCompletion: true when the reporter Job is newly spawned (plan 09-08).
	isFirstCompletion := false
	if r.Deps.ReporterImage == "" {
		logger.Info("skipping reporter Job spawn: ReporterImage not configured", "project", project.Name)
		// No reporter → treat as first completion for budget rollup.
		isFirstCompletion = true
	} else {
		pvcName := r.sharedPVCName()
		reporterJobName := fmt.Sprintf("tide-reporter-%s", project.UID)

		// Idempotent check: if reporter Job already exists, materialization is in
		// flight or complete — skip Create (T-09-13 mitigation: re-fire safety).
		var existingReporterJob batchv1.Job
		if gErr := r.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: project.Namespace}, &existingReporterJob); gErr != nil {
			if !apierrors.IsNotFound(gErr) {
				return ctrl.Result{}, fmt.Errorf("get reporter job %s: %w", reporterJobName, gErr)
			}
			isFirstCompletion = true
			reporterJob := BuildReporterJob(project, project, pvcName, string(project.UID), "Project",
				ReporterOptions{ReporterImage: r.Deps.ReporterImage}, r.Scheme)
			if cErr := r.Create(ctx, reporterJob); cErr != nil {
				if !apierrors.IsAlreadyExists(cErr) {
					return ctrl.Result{}, fmt.Errorf("create reporter job %s: %w", reporterJobName, cErr)
				}
				// AlreadyExists: idempotent success (T-09-13).
			} else {
				logger.Info("spawned reporter Job", "job", reporterJobName, "project", project.Name)
			}
		} else {
			logger.V(1).Info("reporter Job already exists; skipping spawn", "job", reporterJobName)
		}
	}

	// 37-06 / DASH-02: request an artifact-stage push carrying the cumulative
	// planner-completed map, immediately after the reporter spawn. The Project has no
	// approve gate (D-02 auto-proceed), so the completion-site trigger suffices — no
	// parked-arm retry. Log-and-continue: the next boundary push self-heals on failure.
	if apErr := triggerArtifactPush(ctx, r.Client, r.Scheme, project, "project", r.Deps.TidePushImage, r.Deps.HelmProviderDefaults); apErr != nil {
		logger.Info("artifact push trigger failed at project completion (non-fatal)", "project", project.Name, "error", apErr.Error())
	}

	// Plan 09-08 Defect C / BYPASS-03: roll up planner-level Usage exactly once per
	// planner Job, gated by the durable PlannerRolledUpUID marker. The old
	// isFirstCompletion signal (reporter-Job-IsNotFound) flips true again after the
	// reporter Job's 300s TTL expires, causing double-count on halt→resume. The
	// PlannerRolledUpUID marker survives halt (it lives in CRD .status) and is never
	// cleared on bypass, so it remains the authoritative idempotency guard.
	//
	// T-27-03-01 mitigation: jobName is constructed from project.UID via
	// fmt.Sprintf("tide-project-%s-1", ...), not from external input — the
	// tide-project-<uid>-1 shape is a construction-site invariant.
	plannerJobName := fmt.Sprintf("tide-project-%s-1", project.UID)
	// D-11/R-13: budget rollup is suppressed unconditionally for imported envelopes —
	// the prior run already counted the planning cost; rolling up here would double-count.
	if project.Spec.ImportSource != nil {
		logger.V(1).Info("skipping budget rollup: project has importSource (D-11)", "project", project.Name)
	} else if isFirstCompletion && envReadOK {
		if project.Status.Budget.PlannerRolledUpUID != plannerJobName {
			if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
				logger.Error(rollErr, "project planner budget rollup failed (non-fatal)", "project", project.Name)
			} else {
				// Stamp the durable marker only after a successful rollup (Pitfall-2 ordering:
				// leaving the marker unset on error lets the next reconcile retry).
				// WR-02: re-fetch + RetryOnConflict + MergeFromWithOptimisticLock mirrors the
				// milestone/phase/plan pattern, making the stamp durable against concurrent status
				// writes on the Project. On retry-budget exhaustion return the error to requeue —
				// the marker must be durably set before the reporter Job's TTL-GC window reopens
				// rollup (PREFLIGHT-02 / T-34-03).
				if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					latest := &tidev1alpha3.Project{}
					if err := r.Get(ctx, client.ObjectKeyFromObject(project), latest); err != nil {
						return err
					}
					if latest.Status.Budget.PlannerRolledUpUID == plannerJobName {
						return nil // already set by a concurrent reconcile — idempotent
					}
					markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
					latest.Status.Budget.PlannerRolledUpUID = plannerJobName
					return r.Status().Patch(ctx, latest, markerPatch)
				}); mErr != nil {
					return ctrl.Result{}, fmt.Errorf("patch PlannerRolledUpUID: %w", mErr)
				}
			}
			// Phase 38 COST-02: surface an unknown-model pricing fallback carried
			// on the envelope — condition + metric, bounded by the same
			// exactly-once rollup guards. Non-fatal: informational only.
			if fbErr := setPricingFallbackIfNeeded(ctx, r.Client, project, out.Usage.PricingFallbackModel); fbErr != nil {
				logger.Error(fbErr, "setPricingFallbackIfNeeded failed (non-fatal)", "project", project.Name)
			}
		}
	}

	// Phase 13 D-04 layer 2: backstop — classify planner-envelope failure Reason.
	// NOT the push-Job path — push failures have their own classification.
	if envReadOK && out.ExitCode != 0 {
		var jobStart time.Time
		if completedJob != nil {
			jobStart = completedJob.CreationTimestamp.Time
		}
		if hErr := setBillingHaltIfNeeded(ctx, r.Client, project, out.Reason, jobStart); hErr != nil {
			logger.Error(hErr, "setBillingHaltIfNeeded failed (non-fatal)", "project", project.Name)
		}
	}

	// Plan 09-08 Defect B fix: uniform ChildCount-gated succession. Gate:
	//   expected == 0            → return (checkProjectComplete handles leaf case
	//                              on next reconcile via BoundaryDetected)
	//   observed < expected      → requeue 5s (reporter still materializing Milestones)
	//   observed >= expected     → return (checkProjectComplete will detect all-Succeeded)
	// When EnvReader is nil or read failed, fall back to returning nil and letting
	// checkProjectComplete handle succession on next reconcile via Owns watch.
	if envReadOK {
		expected := out.ChildCount
		if expected > 0 {
			observed := r.countChildMilestones(ctx, project)
			if observed < expected {
				logger.V(1).Info("requeue: reporter still materializing Milestone children",
					"project", project.Name, "expected", expected, "observed", observed)
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
		}
	}

	// D-02: no gate at Project→Milestone level — auto-proceed.
	// The Owns(&Milestone{}) watch re-enqueues the Project when Milestone status
	// changes (once the reporter Job creates the child), driving checkProjectComplete
	// to fire on the next reconcile.
	return ctrl.Result{}, nil
}

// handleBudgetGate checks the budget cap and bypass annotations, patching
// Project.Status.Phase and emitting K8s Events as needed (D-D4, FAIL-04).
// After this call, project.Status.Phase reflects the current budget state.
//
//nolint:unparam // ctrl.Result kept so callers can `return r.handleBudgetGate(...)` in the reconcile chain
func (r *ProjectReconciler) handleBudgetGate(ctx context.Context, project *tidev1alpha3.Project, now time.Time) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Phase 04.1 P4.1: reset rolling window if elapsed. Failures are logged
	// non-fatal (Pitfall C pattern) — never block dispatch on a tally op.
	if _, err := budget.MaybeResetWindow(ctx, r.Client, project, now); err != nil {
		logger.Error(err, "budget window reset failed (non-fatal)")
	}

	// Existing cap check follows — now sees the post-reset CostSpentCents value.
	bypassed := budget.IsBypassed(project, now)
	capExceeded := budget.IsCapExceeded(project)

	if project.Status.Phase == tidev1alpha3.PhaseBudgetExceeded && bypassed {
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

		// Clear the phase. D-01: if the workspace is already initialized (BranchName
		// is set), resume at Running — not Pending — to avoid re-entering the init-Job
		// dispatch path (BYPASS-01 fix).
		// D-04: record the acknowledged-spend baseline so re-halt fires only on NEW
		// post-bypass spend, not on the already-incurred amount.
		statusPatch := client.MergeFrom(project.DeepCopy())
		if project.Status.Git.BranchName != "" {
			project.Status.Phase = tidev1alpha3.PhaseRunning
		} else {
			project.Status.Phase = tidev1alpha3.PhasePending
		}
		project.Status.Budget.BypassBaselineCents = project.Status.Budget.CostSpentCents
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tidev1alpha3.ConditionBudgetExceeded,
			Status:             metav1.ConditionFalse,
			Reason:             tidev1alpha3.ReasonBypassApplied,
			Message:            "Budget exceeded bypass applied by operator",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		if r.Recorder != nil {
			r.Recorder.Event(project, corev1.EventTypeNormal, tidev1alpha3.ReasonBypassApplied,
				"Budget exceeded bypass applied by operator; dispatch resumed")
		}
		return ctrl.Result{}, nil
	}

	// D-04: re-halt fires only when new spend has occurred since the bypass
	// acknowledged the prior spend. A fresh bypass sets BypassBaselineCents ==
	// CostSpentCents, so re-halt is suppressed until dispatch spends more.
	// Without this guard, raising the absolute cap alone (or just resuming)
	// would immediately re-halt because the rolling-window cap is still
	// numerically exceeded by the already-incurred amount.
	newSpendSinceBypass := project.Status.Budget.CostSpentCents > project.Status.Budget.BypassBaselineCents
	if project.Status.Phase != tidev1alpha3.PhaseBudgetExceeded && capExceeded && !bypassed && newSpendSinceBypass {
		// Determine which cap triggered the halt (absolute takes priority when both are exceeded).
		reason := "AbsoluteCapReached"
		if project.Spec.Budget.AbsoluteCapCents <= 0 ||
			project.Status.Budget.CostSpentCents <= project.Spec.Budget.AbsoluteCapCents {
			reason = "RollingWindowCapReached"
		}
		message := fmt.Sprintf("Cost spent %d cents exceeds cap (absolute %d cents, rolling-window %d cents)",
			project.Status.Budget.CostSpentCents,
			project.Spec.Budget.AbsoluteCapCents,
			project.Spec.Budget.RollingWindowCapCents)

		// Cap hit — set BudgetExceeded and record Event.
		logger.Info("budget cap exceeded; halting dispatch", "project", project.Name, "reason", reason)
		statusPatch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tidev1alpha3.PhaseBudgetExceeded
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tidev1alpha3.ConditionBudgetExceeded,
			Status:             metav1.ConditionTrue,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		if r.Recorder != nil {
			r.Recorder.Event(project, corev1.EventTypeWarning, reason, message)
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
func (r *ProjectReconciler) buildInitJob(project *tidev1alpha3.Project, pvcName string) *batchv1.Job {
	backoffLimit := int32(2)
	ttl := int32(300)
	runAsUser := int64(1000)
	runAsGroup := int64(1000)
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
								// chmod 2775 (setgid) so the shared workspace dirs are
								// group-writable AND new entries created under them by
								// other-uid pods (planner/executor uid 1000, tide-push uid
								// 65532) inherit the shared gid 1000. Without setgid the
								// tide-push Job cannot mkdir /workspace/envelopes/push.
								//
								// GAP-7: envelopes is chmod'd separately and tolerantly. In
								// the resume-from-import flow the tide-import Job (uid 65532)
								// creates+owns /workspace/envelopes and sets it 2775 itself;
								// this uid-1000 init Job then cannot chmod it (EPERM, non-
								// owner) — so swallow that failure rather than flip the
								// Project to InitFailed. In the normal flow init owns the dir
								// and the chmod succeeds, so the `|| true` never fires.
								"mkdir -p /workspace/repo /workspace/artifacts /workspace/envelopes && chmod 2775 /workspace/repo /workspace/artifacts && { chmod 2775 /workspace/envelopes 2>/dev/null || true; }",
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: &runAsUser,
								// RunAsGroup pins the primary gid to 1000 so the shared
								// dirs this init Job creates are group-owned 1000, not gid
								// 0 — the root cause of the tide-push 'mkdir /workspace/
								// envelopes/push: permission denied' cross-uid failure.
								RunAsGroup:               &runAsGroup,
								ReadOnlyRootFilesystem:   new(false),
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

// isStaleArtifactPush reports whether a succeeded push Job staged a STRICT SUBSET
// of what collectStageEnvelopes now returns (Defect E / DASH-02 follow-up). Such a
// Job is an early D-B5/R-05 single-flight winner that snapshotted a partial
// cumulative map (e.g. an artifact push fired before the milestone/phase/plan
// children materialized); accepting it as the terminal boundary push would leave
// the fuller-map levels off the run branch forever.
//
// It reads the stagedEnvelopesAnnotation the Job carried at create time:
//   - No stamp → unknown provenance (a bare/pre-fix Job); return false so the
//     existing Tests 1-7 bare Jobs go terminal exactly as before.
//   - Stamp present but current is not strictly larger → false (nothing new
//     materialized; monotonic map has not grown).
//   - Stamp present, current strictly larger, and EVERY staged entry still appears
//     in current → true (a genuine stale subset to supersede).
//   - Any staged entry MISSING from current (an unexpected divergence — e.g. a
//     level CR was deleted) → false; don't second-guess an already-succeeded push.
func isStaleArtifactPush(job *batchv1.Job, current []string) bool {
	raw, ok := job.Annotations[stagedEnvelopesAnnotation]
	if !ok {
		return false
	}
	staged := map[string]struct{}{}
	if raw != "" {
		for e := range strings.SplitSeq(raw, ",") {
			staged[e] = struct{}{}
		}
	}
	if len(current) <= len(staged) {
		return false
	}
	currentSet := make(map[string]struct{}, len(current))
	for _, e := range current {
		currentSet[e] = struct{}{}
	}
	for e := range staged {
		if _, present := currentSet[e]; !present {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Version-crank fail-closed guard (D-04 / Phase 40 Plan 40-03 — generalized
// from the Phase 23 Plan 23-03 SCHEMA-03/D-09 guard)
// ---------------------------------------------------------------------------

// expectedSchemaRevision is the only SchemaRevision value checkSchemaRevisionGuard
// accepts. migrationGuideDocPath is the doc surfaced in the RequiresReinstall
// message. A future vNext crank changes exactly these two constants and
// nothing else in this function (D-04).
const (
	expectedSchemaRevision = "v1alpha3"
	migrationGuideDocPath  = "docs/migration/v1alpha2-to-v1alpha3.md"
)

// checkSchemaRevisionGuard is the SCHEMA-03 / D-09 fail-closed guard,
// generalized under D-04 so a future schema crank is a two-constant change.
// It rejects any Project whose Spec.SchemaRevision does not equal
// [expectedSchemaRevision] — the absence or mismatch of this field signals an
// object that was authored under a prior schema revision and slipped into
// etcd before the CRD upgrade.
//
// On detection it:
//   - Sets a Ready=False/RequiresReinstall condition on the Project status.
//   - Persists the condition via r.Status().Update.
//   - Returns (true, ctrl.Result{}, reconcile.TerminalError(...)) to prevent
//     the reconciler from running and to suppress requeue storms.
//
// Returns (false, ctrl.Result{}, nil) when the project shape is valid.
func (r *ProjectReconciler) checkSchemaRevisionGuard(
	ctx context.Context,
	project *tidev1alpha3.Project,
) (blocked bool, err error) {
	if project.Spec.SchemaRevision == expectedSchemaRevision {
		return false, nil
	}

	// The SchemaRevision is absent or wrong — this object was authored under
	// a prior schema revision. Surface a permanent failure condition and halt
	// reconciliation.
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:   tidev1alpha3.ConditionReady,
		Status: metav1.ConditionFalse,
		Reason: tidev1alpha3.ReasonRequiresReinstall,
		Message: "Project was authored under a prior schema revision; reinstall required: " +
			"kubectl delete project " + project.Name +
			" && kubectl apply -f <project.yaml> (with schemaRevision: " + expectedSchemaRevision + " set). " +
			"See " + migrationGuideDocPath + ".",
		LastTransitionTime: metav1.Now(),
	})
	if updateErr := r.Status().Update(ctx, project); updateErr != nil {
		// Non-fatal: log and continue — the TerminalError below still prevents dispatch.
		logf.FromContext(ctx).Error(updateErr,
			"failed to update RequiresReinstall condition; condition may not be visible yet",
			"project", project.Name)
	}
	return true, reconcile.TerminalError(
		fmt.Errorf("project %s/%s requires reinstall (schemaRevision must be %s)",
			project.Namespace, project.Name, expectedSchemaRevision),
	)
}

// assembleProjectDepGraph builds the task-level dependency graph for the given
// Project with FULL FAN-OUT (D-04 / Phase 24). It lists all Tasks, Plans,
// Phases, and Milestones in the namespace and resolves coarse-scope
// DependsOn entries (naming a Plan/Phase/Milestone) into task-level edges
// in-memory — nothing is written back to CRDs (D-05 / verify-no-aggregates).
//
// Fan-out carriers: Task.DependsOn, Plan.DependsOn, Phase.DependsOn, and
// Milestone.DependsOn are all iterated. A coarse ref left un-refined fans out
// conservatively to EVERY Task in that scope (D-06). An unresolved ref
// contributes no edge. Edges are de-duplicated before returning (Pitfall 2).
//
// The caller-owned task slice is returned so deriveGlobalWaves can re-use it
// without issuing a second List for the same project-labeled Tasks (IN-02).
//
// NOTE(phase-25+): tasks lacking the project label (directly applied or whose
// label backfill is pending) are not visible to this listing; WR-04 mitigation
// emits an observable warning. Full coverage via field-index listing or admission
// defaulting is a phase-25+ follow-up.
func (r *ProjectReconciler) assembleProjectDepGraph(
	ctx context.Context,
	project *tidev1alpha3.Project,
) (nodes []dag.NodeID, edges []dag.Edge, tasks []tidev1alpha3.Task, err error) {
	// 1. List all Tasks in the project namespace carrying the project label.
	var taskList tidev1alpha3.TaskList
	if listErr := r.List(ctx, &taskList,
		client.InNamespace(project.Namespace),
		client.MatchingLabels{owner.LabelProject: project.Name},
	); listErr != nil {
		return nil, nil, nil, fmt.Errorf("list tasks for project %s: %w", project.Name, listErr)
	}

	// 2. List Plans, Phases, and Milestones for coarse-scope resolution (D-04).
	var planList tidev1alpha3.PlanList
	if listErr := r.List(ctx, &planList, client.InNamespace(project.Namespace)); listErr != nil {
		return nil, nil, nil, fmt.Errorf("list plans for project %s: %w", project.Name, listErr)
	}
	var phaseList tidev1alpha3.PhaseList
	if listErr := r.List(ctx, &phaseList, client.InNamespace(project.Namespace)); listErr != nil {
		return nil, nil, nil, fmt.Errorf("list phases for project %s: %w", project.Name, listErr)
	}
	var msList tidev1alpha3.MilestoneList
	if listErr := r.List(ctx, &msList, client.InNamespace(project.Namespace)); listErr != nil {
		return nil, nil, nil, fmt.Errorf("list milestones for project %s: %w", project.Name, listErr)
	}

	// 3. Build the shared coarse-ref fan-out resolver (Phase 25 D-01).
	// Extracted to depgraph.go so TaskReconciler dispatch indegree and
	// ProjectReconciler wave derivation share one resolver — they can never
	// disagree about what an edge means.
	resolver := buildScopeResolver(taskList.Items, planList.Items, phaseList.Items, msList.Items)

	// 4. Build nodes (all Task names).
	nodes = make([]dag.NodeID, 0, len(taskList.Items))
	for i := range taskList.Items {
		nodes = append(nodes, taskList.Items[i].Name)
	}

	// WR-04 mitigation: warn when unlabeled Tasks in the namespace are excluded.
	// Tasks lacking owner.LabelProject are invisible to the global derivation engine,
	// producing a partial/incorrect global schedule with no error surfaced. Log a
	// Warning so the gap is observable rather than silent.
	// NOTE(phase-25+): full coverage (field-index listing or admission defaulting)
	// is a follow-up; this cheap scan makes the exclusion visible.
	{
		var allNsTasks tidev1alpha3.TaskList
		if scanErr := r.List(ctx, &allNsTasks, client.InNamespace(project.Namespace)); scanErr == nil {
			unlabeled := 0
			for i := range allNsTasks.Items {
				if allNsTasks.Items[i].Labels[owner.LabelProject] != project.Name {
					unlabeled++
				}
			}
			if unlabeled > 0 {
				logf.FromContext(ctx).Info(
					"WARNING: tasks in namespace lack project label and are excluded from global wave derivation",
					"project", project.Name,
					"unlabeledCount", unlabeled,
				)
			}
		}
	}

	// 5. Build de-duplicated edges via the shared resolver (sections 6a–6c moved
	// to depgraph.buildGlobalEdges — §6d Milestone fan-out removed in Phase 26;
	// Milestone.dependsOn is a planning-DAG edge contributing zero execution edges).
	edges = buildGlobalEdges(resolver, taskList.Items, planList.Items, phaseList.Items)

	// WR-04: surface any cross-Kind scope-name collision. resolveScope now unions
	// all matching levels (so wave derivation never drops a true edge — staying
	// consistent with computeGlobalIndegree, D-01), but an ambiguous DependsOn
	// name is a configuration smell worth logging for diagnosis.
	if names := resolver.collisionNames(); len(names) > 0 {
		logf.FromContext(ctx).V(1).Info(
			"assembleProjectDepGraph: DependsOn scope name matched multiple Kind levels (Task/Plan/Phase/Milestone); unioning members to avoid dropping edges",
			"project", project.Name, "collidingNames", names)
	}

	return nodes, edges, taskList.Items, nil
}

// deriveGlobalWaves reconciles the Wave CR set for the Project using the
// pre-computed global wave schedule from checkGlobalCycleGate (WR-03 — ComputeWaves
// runs exactly once per reconcile; the result is threaded here rather than
// recomputed). Accepts the assembler's task slice so no redundant List is issued
// for the same project-labeled Tasks (IN-02 — the comment that claimed re-use
// is now true).
//
// Reconciliation:
//   - Creates Wave CRs named tide-wave-<project>-<N> with WaveSpec{ProjectRef, WaveIndex},
//     owned by the Project (BlockOwnerDeletion). Increment WavesDispatchedTotal exactly
//     once on Create; AlreadyExists and reconcile-replay paths do NOT increment.
//   - Prunes Wave CRs whose WaveIndex >= len(globalWaves) (stale after re-derivation).
//     Phase 25 should gate prune on Wave.Status.Phase == "Succeeded" to avoid pruning
//     an in-flight Wave — see Open Question 3 in RESEARCH.md.
//   - Stamps each Task's tideproject.k8s/wave-index = <globalN> label (EXEC-03
//     bidirectional index). The stamp is idempotent — no patch if already correct.
//
// The computed schedule is NEVER written to Project.status (PERSIST-03 / D-05 / D-10).
// Re-derivation is O(V+E) and runs on every reconcile that passes checkGlobalCycleGate.
func (r *ProjectReconciler) deriveGlobalWaves(
	ctx context.Context,
	project *tidev1alpha3.Project,
	globalWaves [][]dag.NodeID,
	assembledTasks []tidev1alpha3.Task,
) error {
	logger := logf.FromContext(ctx)

	// Reconcile Wave CRs: create missing, skip existing (idempotent).
	for i := range globalWaves {
		waveName := fmt.Sprintf("tide-wave-%s-%d", project.Name, i)
		// CR-01: stamp the project label so the prune List selector actually matches
		// these Waves (D-09 label discipline; the wave webhook only validates, it
		// does not default labels).
		wave := &tidev1alpha3.Wave{
			ObjectMeta: metav1.ObjectMeta{
				Name:      waveName,
				Namespace: project.Namespace,
				Labels:    map[string]string{owner.LabelProject: project.Name},
			},
			Spec: tidev1alpha3.WaveSpec{
				ProjectRef: project.Name,
				WaveIndex:  i,
			},
		}

		var existing tidev1alpha3.Wave
		if getErr := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: waveName}, &existing); getErr != nil {
			if client.IgnoreNotFound(getErr) != nil {
				return fmt.Errorf("get wave %s: %w", waveName, getErr)
			}
			// Wave does not exist — set owner ref and create.
			if ownerErr := owner.EnsureOwnerRef(wave, project, r.Scheme); ownerErr != nil {
				return fmt.Errorf("ensure owner ref wave %s: %w", waveName, ownerErr)
			}
			if createErr := r.Create(ctx, wave); createErr != nil {
				if !apierrors.IsAlreadyExists(createErr) {
					return fmt.Errorf("create wave %s: %w", waveName, createErr)
				}
				// AlreadyExists: watch-lag race — idempotent success. The reconcile
				// that successfully created this Wave already counted it; do NOT increment.
			} else {
				// Create succeeded — exactly-once dispatch commit point (CR-02 / D-08).
				// Sentinel "global" for phase/plan — never emit empty label values (Pitfall 3).
				tidemetrics.WavesDispatchedTotal.WithLabelValues(project.Name, "global", "global").Inc()
				logger.Info("created global wave", "wave", waveName, "index", i)
			}
		} else {
			// Wave exists — ensure owner ref only when absent (WR-02: avoid unconditional
			// Update churn that bumps resourceVersion and triggers extra Project reconciles).
			if !metav1.IsControlledBy(&existing, project) {
				if ownerErr := owner.EnsureOwnerRef(&existing, project, r.Scheme); ownerErr != nil {
					return fmt.Errorf("ensure owner ref on existing wave %s: %w", waveName, ownerErr)
				}
				if err := r.Update(ctx, &existing); err != nil {
					return fmt.Errorf("update owner ref on wave %s: %w", waveName, err)
				}
			}
		}
	}

	// Prune stale Wave CRs whose WaveIndex >= len(globalWaves) (re-derivation produced
	// fewer waves, e.g., a dependency was removed). Phase 25 should gate this prune on
	// Wave.Status.Phase == "Succeeded" to avoid deleting an in-flight Wave (RESEARCH OQ-3).
	// CR-01: list by the project label that Waves now carry (stamped at create time above).
	var allWaves tidev1alpha3.WaveList
	if listErr := r.List(ctx, &allWaves,
		client.InNamespace(project.Namespace),
		client.MatchingLabels{owner.LabelProject: project.Name},
	); listErr != nil {
		return fmt.Errorf("list waves for prune (project %s): %w", project.Name, listErr)
	}
	for i := range allWaves.Items {
		w := &allWaves.Items[i]
		if w.Spec.ProjectRef == project.Name && w.Spec.WaveIndex >= len(globalWaves) {
			// OQ-3 fix: only prune if zero members OR already Succeeded.
			// Zero-member: TaskRefs is empty (aggregator set Phase="ZeroMembers").
			// Succeeded: all member tasks completed — safe to remove.
			//
			// CreationTimestamp fence: if the wave was just created (Phase still "")
			// and TaskRefs is empty, the WaveReconciler may not have run yet —
			// skip this prune pass to avoid deleting a wave before its members are
			// stamped. The fence applies ONLY when Phase is unset (pre-aggregation);
			// once the aggregator has run (Phase != ""), TaskRefs is authoritative.
			if w.Status.Phase == "" && len(w.Status.TaskRefs) == 0 &&
				time.Since(w.CreationTimestamp.Time) < 5*time.Second {
				logger.V(1).Info("skipping prune of in-flight wave", "wave", w.Name,
					"phase", w.Status.Phase, "memberCount", len(w.Status.TaskRefs))
				continue
			}
			if len(w.Status.TaskRefs) == 0 || w.Status.Phase == tidev1alpha3.LevelPhaseSucceeded {
				if delErr := r.Delete(ctx, w); delErr != nil && !apierrors.IsNotFound(delErr) {
					return fmt.Errorf("prune wave %s: %w", w.Name, delErr)
				}
				logger.Info("pruned stale global wave", "wave", w.Name, "waveIndex", w.Spec.WaveIndex, "currentWaveCount", len(globalWaves))
			} else {
				logger.V(1).Info("skipping prune of in-flight wave", "wave", w.Name,
					"phase", w.Status.Phase, "memberCount", len(w.Status.TaskRefs))
			}
		}
	}

	// Stamp global wave-index labels on Tasks (EXEC-03 bidirectional index).
	// assembledTasks is threaded from assembleProjectDepGraph — no redundant List
	// needed for the same project-labeled Tasks (IN-02 — comment now matches code).
	return r.stampGlobalTaskLabels(ctx, assembledTasks, globalWaves, project.Name)
}

// stampGlobalTaskLabels patches each Task with its global tideproject.k8s/wave-index
// label (= the global wave index from deriveGlobalWaves) and tideproject.k8s/project
// (= projectName). Ported verbatim in idiom from PlanReconciler.stampTaskLabels
// (plan_controller.go:1421-1455).
//
// Skip-if-already-correct prevents patch churn when the schedule is unchanged.
// Uses client.MergeFrom + r.Patch to avoid ResourceVersion conflicts (D-09).
func (r *ProjectReconciler) stampGlobalTaskLabels(
	ctx context.Context,
	tasks []tidev1alpha3.Task,
	globalWaves [][]dag.NodeID,
	projectName string,
) error {
	// Build a task-name → global wave index map.
	taskWave := make(map[string]int, len(tasks))
	for waveIdx, wave := range globalWaves {
		for _, name := range wave {
			taskWave[name] = waveIdx
		}
	}

	for i := range tasks {
		t := &tasks[i]
		waveIdx, ok := taskWave[t.Name]
		if !ok {
			continue
		}
		waveIndexStr := fmt.Sprintf("%d", waveIdx)
		// Skip patch if both labels are already correct — no churn on re-derivation.
		if t.Labels[owner.LabelWaveIndex] == waveIndexStr &&
			(projectName == "" || t.Labels[owner.LabelProject] == projectName) {
			continue
		}
		patch := client.MergeFrom(t.DeepCopy())
		if t.Labels == nil {
			t.Labels = map[string]string{}
		}
		t.Labels[owner.LabelWaveIndex] = waveIndexStr
		if projectName != "" {
			t.Labels[owner.LabelProject] = projectName
		}
		if err := r.Patch(ctx, t, patch); err != nil {
			return fmt.Errorf("stamp global wave label on task %s: %w", t.Name, err)
		}
	}
	return nil
}

// checkGlobalCycleGate is the DEPS-03 / D-10 validation-time cross-scope cycle
// detector. It accepts pre-assembled (nodes, edges) from assembleProjectDepGraph
// and calls pkg/dag.ComputeWaves. On a CycleError it surfaces the involved nodes
// via a CycleDetected Project status condition and returns blocked=true.
//
// The assembler is called ONCE in Reconcile and its result passed here (Pitfall 7
// — avoids double List calls when the fan-out assembler lists Plans/Phases/
// Milestones). The computed wave schedule is returned to the caller so
// deriveGlobalWaves can consume it without a second ComputeWaves call (WR-03 /
// IN-02 — ComputeWaves runs exactly once per reconcile).
//
// Unlike checkSchemaRevisionGuard, a cycle is NOT a TerminalError — the operator
// can fix the cycle by editing DependsOn on the relevant Tasks and the reconciler
// will requeue on changes. No schedule is stored (PERSIST-03 / verify-no-aggregates).
//
// A non-cycle error (e.g., transient List failure) is returned as a plain error
// for the controller to requeue with backoff.
func (r *ProjectReconciler) checkGlobalCycleGate(
	ctx context.Context,
	project *tidev1alpha3.Project,
	nodes []dag.NodeID,
	edges []dag.Edge,
) (blocked bool, waves [][]dag.NodeID, err error) {
	// ComputeWaves validates the graph and returns *CycleError on a cycle.
	// The computed waves are returned to the caller for use by deriveGlobalWaves
	// (WR-03 — compute once, not twice). No schedule is stored (PERSIST-03).
	computedWaves, computeErr := dag.ComputeWaves(nodes, edges)
	if computeErr != nil {
		var cyc *dag.CycleError
		if goErrors.As(computeErr, &cyc) {
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               conditionTypeCycleDetected,
				Status:             metav1.ConditionTrue,
				Reason:             tidev1alpha3.ReasonGlobalCycleDetected,
				Message:            fmt.Sprintf("cyclic global Execution DAG involving: %v", cyc.InvolvedNodes),
				LastTransitionTime: metav1.Now(),
			})
			if updateErr := r.Status().Update(ctx, project); updateErr != nil {
				logf.FromContext(ctx).Error(updateErr,
					"failed to update GlobalCycleDetected condition",
					"project", project.Name,
					"involved", cyc.InvolvedNodes)
			}
			// NOT a TerminalError — a plan edit can remove the cycle; allow requeue.
			return true, nil, nil
		}
		// Non-cycle error (e.g., unknown node from edge assembler defect) — transient requeue.
		return false, nil, fmt.Errorf("ComputeWaves error for project %s: %w", project.Name, computeErr)
	}
	// WR-01: no cycle — clear any prior sticky CycleDetected condition so the
	// operator sees a self-healing signal once the cycle is broken. The doc comment
	// on taskToProject claims this self-clears; make the code match.
	if meta.FindStatusCondition(project.Status.Conditions, conditionTypeCycleDetected) != nil {
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               conditionTypeCycleDetected,
			Status:             metav1.ConditionFalse,
			Reason:             "NoCycle",
			Message:            "global Execution DAG is acyclic",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Update(ctx, project); err != nil {
			return false, nil, err
		}
	}
	return false, computedWaves, nil
}

// taskToProject maps a Task to a reconcile.Request for its owning Project,
// read from the canonical tideproject.k8s/project label (owner.LabelProject).
// This re-enqueues the Project on any Task DependsOn edit so checkGlobalCycleGate
// re-runs and the sticky CycleDetected condition clears once the cycle is broken
// (WR-02). Returns an empty slice for Tasks not yet project-labeled.
func (r *ProjectReconciler) taskToProject(_ context.Context, obj client.Object) []reconcile.Request {
	projectName := obj.GetLabels()[owner.LabelProject]
	if projectName == "" {
		return nil
	}
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{Namespace: obj.GetNamespace(), Name: projectName},
	}}
}

// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02,
// annotation-change predicate for bypass annotations (D-D4), and a
// namespace-filter predicate per AUTH-02.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		//nolint:staticcheck // SA1019: GetEventRecorderFor returns record.EventRecorder (the Recorder field's type);
		// GetEventRecorder returns the incompatible events/v1 type — migrating is out of scope for lint hygiene.
		r.Recorder = mgr.GetEventRecorderFor("project-controller")
	}
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true // watch-all-namespaces mode
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tidev1alpha3.Project{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			)),
		).
		Owns(&batchv1.Job{}).
		Owns(&tidev1alpha3.Milestone{}).
		// Wave CRs are created by ProjectReconciler (global derivation, Plan 03).
		// Owning them re-enqueues the Project when Wave status changes — Phase 25
		// dispatch will read Wave status to drive wave-boundary progression.
		Owns(&tidev1alpha3.Wave{}).
		// Watch (not Owns) Tasks: a Task DependsOn edit must re-run the global
		// cycle gate so the CycleDetected condition clears when the operator
		// breaks the cycle (WR-02). Tasks are not owned by Project.
		Watches(&tidev1alpha3.Task{}, handler.EnqueueRequestsFromMapFunc(r.taskToProject)).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("project").
		Complete(r)
}
