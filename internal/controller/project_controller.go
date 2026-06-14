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
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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
	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels{"job-name": pushJobName},
	); err != nil {
		return pushResultEnvelope{}, false
	}
	if len(pods.Items) == 0 {
		return pushResultEnvelope{}, false
	}
	pod := &pods.Items[0]
	if len(pod.Status.ContainerStatuses) == 0 {
		return pushResultEnvelope{}, false
	}
	term := pod.Status.ContainerStatuses[0].State.Terminated
	if term == nil || term.Message == "" {
		return pushResultEnvelope{}, false
	}
	var env pushResultEnvelope
	if err := json.Unmarshal([]byte(term.Message), &env); err != nil {
		return pushResultEnvelope{}, false
	}
	return env, true
}

const (
	projectFinalizer = "tideproject.k8s/project-cleanup"
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

	// SharedPVCName is the name of the cluster-wide PVC provisioned by the
	// Helm chart (Plan 12). Defaults to "tide-projects". Configurable via
	// --workspaces-pvc-name flag on the manager (Blocker #2/#3 architecture).
	SharedPVCName string

	// TidePushImage is the image ref for the tide-push container used by
	// both clone- and push-mode Jobs (Phase 3 plan 03-06).
	TidePushImage string

	// Phase 7 (D-06): dispatch deps for project-level planner Job (mirrors MilestoneReconciler).
	EnvReader      podjob.EnvelopeReader
	SigningKey     []byte
	CredproxyImage string
	// SubagentImage is dead since Phase 13 — resolveImage owns resolution;
	// retained for legacy test wiring, ignored at dispatch.
	SubagentImage        string
	HelmProviderDefaults ProviderDefaults

	// ReporterImage is the image ref for the tide-reporter reader Job (Phase 09 plan 09-06).
	// When empty, spawning the reader Job is skipped (mirrors TidePushImage skip in
	// boundary_push.go:80-88). Set via TIDE_REPORTER_IMAGE env from Helm values.
	ReporterImage string

	// PricingOverridesJSON is the validated D-02 override JSON forwarded
	// opaquely to planner Jobs as TIDE_PRICING_OVERRIDES_JSON. Wired in Plan 14-05.
	PricingOverridesJSON string

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
	var project tideprojectv1alpha1.Project
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

	// 5. Phase 2: dispatcher seam — init Job + budget gate + bypass watch (REQ-SUB-01).
	if r.Dispatcher != nil {
		return r.reconcileProjectPhase2(ctx, &project)
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

// reconcileProjectPhase2 implements the Phase 2 body inside the dispatcher seam:
// 1. Budget cap check + bypass annotation handling.
// 2. Shared PVC bind check (Blocker #2/#3 single-PVC architecture).
// 3. Init Job creation (idempotent, deterministic name tide-init-{UID}).
// 4. Init Job completion watch — patches Project.Status.Phase.
func (r *ProjectReconciler) reconcileProjectPhase2(ctx context.Context, project *tideprojectv1alpha1.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	now := time.Now()

	// Step 1: Budget cap check + bypass annotation handling.
	result, err := r.handleBudgetGate(ctx, project, now)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If the project is in BudgetExceeded and bypass did not clear it, halt dispatch.
	if project.Status.Phase == tideprojectv1alpha1.PhaseBudgetExceeded {
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
	if project.Status.Phase == tideprojectv1alpha1.PhaseInitialized || project.Status.Phase == tideprojectv1alpha1.PhaseRunning ||
		project.Status.Phase == tideprojectv1alpha1.PhasePushLeaseFailed || project.Status.Phase == tideprojectv1alpha1.PhaseComplete {
		return r.reconcilePhase3Lifecycle(ctx, project)
	}
	return result, nil
}

// ensureInitJob creates the one-shot init Job (idempotent — AlreadyExists is success).
func (r *ProjectReconciler) ensureInitJob(ctx context.Context, project *tideprojectv1alpha1.Project, pvcName string) error {
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
func (r *ProjectReconciler) handleInitJobCompletion(ctx context.Context, project *tideprojectv1alpha1.Project, job *batchv1.Job) (ctrl.Result, error) {
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
		case tideprojectv1alpha1.PhaseRunning,
			tideprojectv1alpha1.PhaseComplete,
			tideprojectv1alpha1.PhasePushLeaseFailed,
			tideprojectv1alpha1.PhasePushLeakBlocked:
			// Phase has already advanced past Initialized — init-Job-completion
			// was processed in a prior reconcile. Skip the re-patch.
			return ctrl.Result{}, nil
		}
		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhaseInitialized
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             tideprojectv1alpha1.ReasonInitialized,
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
		project.Status.Phase = tideprojectv1alpha1.PhaseInitFailed
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionFailed,
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
//  5. Push Job completion: read the push-result envelope from PVC; on
//     success, patch Status.Git.LastPushedSHA. On lease rejection,
//     patch Status.Phase=PushLeaseFailed + increment LeaseFailureCount.
//
// Plan 03-08 keeps the body skeletal — the production wiring for steps
// 4-5 (level-boundary detection, push-result envelope schema) lands in
// follow-up plans that wire cmd/manager end-to-end. The grep contract
// + the deterministic state transitions tested in envtest are the
// proof-of-shape Phase 3 needs.
//
//nolint:gocyclo // reconcile lifecycle is a flat sequence of state-transition arms; splitting would obscure the contract
func (r *ProjectReconciler) reconcilePhase3Lifecycle(ctx context.Context, project *tideprojectv1alpha1.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

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
	if complete || project.Status.Phase == tideprojectv1alpha1.PhaseComplete {
		// The control-plane succession is done. Run ONLY the bounded
		// boundary-push retry state machine (debug #13b) — no further planner
		// dispatch, branch init, or clone on a Complete project.
		return r.reconcileBoundaryPush(ctx, project)
	}

	// Step 0b: Dispatch project-level planner Job (D-A2 5th dispatch site).
	//nolint:staticcheck // SA1019: result.Requeue is read here as part of the reconcile control-flow contract; behavior-preserving
	if result, err := r.reconcileProjectPlannerDispatch(ctx, project); err != nil || result.Requeue || result.RequeueAfter > 0 {
		return result, err
	}

	// Step 1: Branch-name init (D-B6). Format: tide/run-<project>-<unix>.
	// Unix epoch only — refnames cannot contain ":" so RFC3339 is forbidden.
	if project.Status.Git.BranchName == "" {
		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Git.BranchName = fmt.Sprintf("tide/run-%s-%d", project.Name, time.Now().Unix())
		if err := r.Status().Patch(ctx, project, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("patch branch name: %w", err)
		}
		// Continue to clone dispatch on next reconcile.
		return ctrl.Result{Requeue: true}, nil
	}

	// Step 2: Bypass-annotation handling (D-B6 / D-D4 mirror).
	if project.Status.Phase == tideprojectv1alpha1.PhasePushLeaseFailed {
		if v, ok := project.Annotations[bypassPushLeaseAnnotation]; ok && v == "true" {
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
			// Clear PushLeaseFailed phase.
			statusPatch := client.MergeFrom(project.DeepCopy())
			project.Status.Phase = tideprojectv1alpha1.PhaseRunning
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionPushLeaseFailed,
				Status:             metav1.ConditionFalse,
				Reason:             tideprojectv1alpha1.ReasonBypassApplied,
				Message:            "Push-lease failure bypassed by operator annotation",
				LastTransitionTime: metav1.Now(),
			})
			if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		// Halted at PushLeaseFailed until bypass annotation lands.
		return ctrl.Result{}, nil
	}

	// Step 3: Clone Job dispatch (D-B4 PVC layout init).
	pvcName := r.sharedPVCName()
	cloneJobName := fmt.Sprintf("tide-clone-%s", project.UID)
	var existingClone batchv1.Job
	cloneErr := r.Get(ctx, types.NamespacedName{Name: cloneJobName, Namespace: project.Namespace}, &existingClone)
	if cloneErr != nil && !apierrors.IsNotFound(cloneErr) {
		return ctrl.Result{}, cloneErr
	}
	if apierrors.IsNotFound(cloneErr) && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
		cloneOpts := CloneOptions{TidePushImage: r.TidePushImage}
		// B6: wire the run branch name so tide-push calls EnsureRunBranch + provisions
		// the run worktree during clone (B5). project.Status.Git.BranchName is set by
		// the ProjectReconciler before dispatching the clone Job (reconcilePhase3Lifecycle
		// stamps BranchName in the same reconcile cycle that dispatches the clone Job).
		if project.Status.Git.BranchName != "" {
			cloneOpts.RunBranch = project.Status.Git.BranchName
		}
		cloneJob := buildCloneJob(project, pvcName, cloneOpts, r.Scheme)
		if cErr := r.Create(ctx, cloneJob); cErr != nil {
			if !apierrors.IsAlreadyExists(cErr) {
				return ctrl.Result{}, fmt.Errorf("create clone job: %w", cErr)
			}
			// AlreadyExists: idempotent success.
		}
		logger.Info("created clone Job", "job", cloneJobName)
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
func (r *ProjectReconciler) reconcileBoundaryPush(ctx context.Context, project *tideprojectv1alpha1.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// No git target → nothing to push; nothing to observe.
	if project.Spec.Git == nil || project.Spec.Git.RepoURL == "" {
		return ctrl.Result{}, nil
	}

	// Already confirmed pushed — terminal success arm. Nothing further to do.
	if c := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionBoundaryPushed); c != nil &&
		c.Status == metav1.ConditionTrue {
		return ctrl.Result{}, nil
	}

	// Operator-recovery halt arms (leak / lease). These are distinct, sticky
	// outcomes with their own recovery surfaces (remove the secret; clear the
	// bypass-push-lease annotation). Once set, the boundary-push state machine
	// must NOT re-process them every reconcile — the Step-0 Complete fast-path
	// re-asserts Phase=Complete on each pass, so without this guard the lease
	// arm would re-increment LeaseFailureCount in a loop.
	if c := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionPushLeakBlocked); c != nil &&
		c.Status == metav1.ConditionTrue {
		return ctrl.Result{}, nil
	}
	if c := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionPushLeaseFailed); c != nil &&
		c.Status == metav1.ConditionTrue {
		return ctrl.Result{}, nil
	}

	// Bounded-retry exhaustion arm. Re-derived from .status so the cap survives a
	// controller restart (no in-memory counter). Only declare PushFailed once —
	// guard on the existing condition reason so we don't re-emit the Event.
	if project.Status.BoundaryPush.Attempts >= maxBoundaryPushAttempts {
		if c := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionBoundaryPushed); c == nil ||
			c.Reason != tideprojectv1alpha1.ReasonPushFailed {
			if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionFalse,
				tideprojectv1alpha1.ReasonPushFailed,
				fmt.Sprintf("Boundary push did not land after %d attempts; last error: %q",
					project.Status.BoundaryPush.Attempts, project.Status.BoundaryPush.LastError)); err != nil {
				return ctrl.Result{}, err
			}
			if r.Recorder != nil {
				r.Recorder.Eventf(project, corev1.EventTypeWarning, tideprojectv1alpha1.ReasonPushFailed,
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

	// No push Job yet — create the first attempt.
	if apierrors.IsNotFound(pErr) {
		return r.dispatchBoundaryPush(ctx, project, pvcName, pushJobName, project.Status.BoundaryPush.LastError)
	}

	// Push Job Complete — terminal success.
	if isJobSucceeded(&existingPush) {
		patch := client.MergeFrom(project.DeepCopy())
		project.Status.Git.LeaseFailureCount = 0
		project.Status.BoundaryPush.LastError = ""
		if err := r.Status().Patch(ctx, project, patch); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionTrue,
			tideprojectv1alpha1.ReasonPushed,
			fmt.Sprintf("Run branch %q pushed to remote (job %s)", project.Status.Git.BranchName, pushJobName)); err != nil {
			return ctrl.Result{}, err
		}
		tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "success").Inc()
		logger.Info("boundary push landed on remote", "job", pushJobName, "branch", project.Status.Git.BranchName)
		return ctrl.Result{}, nil
	}

	// Push Job terminal-Failed — classify, then either halt (leak/lease operator
	// recovery) or auto-retry (generic/BackoffLimitExceeded).
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
			project.Status.Phase = tideprojectv1alpha1.PhasePushLeakBlocked
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionPushLeakBlocked,
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
			project.Status.Phase = tideprojectv1alpha1.PhasePushLeaseFailed
			project.Status.Git.LeaseFailureCount++
			meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
				Type:               tideprojectv1alpha1.ConditionPushLeaseFailed,
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

		default:
			// Generic terminal failure (BackoffLimitExceeded / auth / transient
			// remote). #13b bounded auto-retry: delete the failed Job and create
			// a fresh one, incrementing the attempt tally. The cap guard at the
			// top of this method stops the loop.
			lastErr := reason
			switch {
			case !haveEnv:
				lastErr = "envelope-unreadable"
			case lastErr == "":
				// Terminal failure with no specific reason — the
				// BackoffLimitExceeded #13b class (e.g. empty commit / transient
				// remote). Record a generic marker so the operator-visible
				// LastError is never blank on a real failure.
				lastErr = "push-failed"
			}
			if delErr := r.deleteFailedPushJob(ctx, &existingPush); delErr != nil {
				return ctrl.Result{}, delErr
			}
			logger.Info("boundary push attempt failed; retrying",
				"job", pushJobName, "attempt", project.Status.BoundaryPush.Attempts,
				"cap", maxBoundaryPushAttempts, "lastError", lastErr)
			return r.dispatchBoundaryPush(ctx, project, pvcName, pushJobName, lastErr)
		}
	}

	// Push Job pending/running — single-in-flight guard. Do NOT create a second
	// Job; surface the in-flight state and requeue on capped backoff.
	if err := r.setBoundaryPushedCondition(ctx, project, metav1.ConditionFalse,
		tideprojectv1alpha1.ReasonPushing,
		fmt.Sprintf("Boundary push in flight (job %s, attempt %d/%d)",
			pushJobName, project.Status.BoundaryPush.Attempts, maxBoundaryPushAttempts)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: boundaryPushRequeue(project.Status.BoundaryPush.Attempts)}, nil
}

// dispatchBoundaryPush creates a fresh boundary-push Job, increments the bounded
// attempt tally + stamps lastAttemptTime, sets BoundaryPushed=False/Pushing, and
// requeues with capped exponential backoff. The Job pushes the already-
// integrated run-branch HEAD (idempotent per #13), so a re-create after a
// terminal failure converges.
func (r *ProjectReconciler) dispatchBoundaryPush(ctx context.Context, project *tideprojectv1alpha1.Project, pvcName, pushJobName, lastErr string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	msg, mErr := buildCommitMessage("project", "")
	if mErr != nil {
		return ctrl.Result{}, fmt.Errorf("build commit message: %w", mErr)
	}
	pushOpts := PushOptions{
		TidePushImage:  r.TidePushImage,
		Branch:         project.Status.Git.BranchName,
		LastPushedSHA:  project.Status.Git.LastPushedSHA,
		CommitMessage:  msg,
		LeaksConfigMap: project.Spec.Git.LeaksConfigRef,
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
		tideprojectv1alpha1.ReasonPushing,
		fmt.Sprintf("Boundary push dispatched (job %s, attempt %d/%d)",
			pushJobName, project.Status.BoundaryPush.Attempts, maxBoundaryPushAttempts)); err != nil {
		return ctrl.Result{}, err
	}
	tidemetrics.PushJobsTotal.WithLabelValues(project.Name, "dispatched").Inc()
	logger.Info("dispatched boundary push", "job", pushJobName,
		"attempt", project.Status.BoundaryPush.Attempts, "cap", maxBoundaryPushAttempts)
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

// setBoundaryPushedCondition patches the non-terminal BoundaryPushed condition.
// It only writes when the (status, reason) actually changes so reconciles do not
// churn LastTransitionTime.
func (r *ProjectReconciler) setBoundaryPushedCondition(ctx context.Context, project *tideprojectv1alpha1.Project, status metav1.ConditionStatus, reason, message string) error {
	existing := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha1.ConditionBoundaryPushed)
	if existing != nil && existing.Status == status && existing.Reason == reason && existing.Message == message {
		return nil
	}
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionBoundaryPushed,
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
func (r *ProjectReconciler) countChildMilestones(ctx context.Context, project *tideprojectv1alpha1.Project) int {
	var msList tideprojectv1alpha1.MilestoneList
	if err := r.List(ctx, &msList, client.InNamespace(project.Namespace)); err != nil {
		return 0
	}
	count := 0
	for i := range msList.Items {
		if metav1.IsControlledBy(&msList.Items[i], project) {
			count++
		}
	}
	return count
}

// checkProjectComplete returns true (and patches Status.Phase=Complete) when
// BoundaryDetected reports all owned Milestones have reached Succeeded.
// Returns false without patching when no Milestones exist yet (childless guard).
func (r *ProjectReconciler) checkProjectComplete(ctx context.Context, project *tideprojectv1alpha1.Project) (bool, error) {
	detected, err := gates.BoundaryDetected(ctx, r.Client, project, "Milestone")
	if err != nil {
		return false, err
	}
	if !detected {
		return false, nil
	}
	patch := client.MergeFrom(project.DeepCopy())
	project.Status.Phase = tideprojectv1alpha1.PhaseComplete
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionSucceeded,
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
// Gated on len(r.SigningKey) > 0 — when SigningKey is not wired (test mode
// that doesn't configure dispatch), the function is a no-op so existing tests
// that only exercise clone/push lifecycle are unaffected.
func (r *ProjectReconciler) reconcileProjectPlannerDispatch(ctx context.Context, project *tideprojectv1alpha1.Project) (ctrl.Result, error) {
	// Guard: SigningKey is required to mint credproxy tokens — if not wired
	// (e.g. unit tests that only test clone/push lifecycle), skip dispatch.
	if len(r.SigningKey) == 0 {
		return ctrl.Result{}, nil
	}

	// Step 1: Terminal short-circuit.
	switch project.Status.Phase {
	case tideprojectv1alpha1.PhaseComplete,
		tideprojectv1alpha1.PhaseInitFailed:
		return ctrl.Result{}, nil
	}

	// Step 1b: Idempotency guard — skip dispatch when the Project already owns
	// >=1 Milestone. Once the label fix makes the envelope round-trip succeed,
	// Projects that already have a pre-applied Milestone (push-lease, chaos-resume,
	// wave-test fixtures) would otherwise author a spurious extra Milestone.
	// This mirrors BoundaryDetected's ownership check without the all-Succeeded
	// requirement — we just need to know children exist.
	{
		var existingMilestones tideprojectv1alpha1.MilestoneList
		if lErr := r.List(ctx, &existingMilestones, client.InNamespace(project.Namespace)); lErr != nil {
			return ctrl.Result{}, fmt.Errorf("idempotency: list milestones: %w", lErr)
		}
		for i := range existingMilestones.Items {
			if metav1.IsControlledBy(&existingMilestones.Items[i], project) {
				// Project already has at least one owned Milestone — planner already ran.
				return ctrl.Result{}, nil
			}
		}
	}

	jobName := fmt.Sprintf("tide-project-%s-1", project.UID)

	// Step 2: On Running — check Job terminal state.
	if project.Status.Phase == tideprojectv1alpha1.PhaseRunning {
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
		return ctrl.Result{}, nil
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

	// Step 3: Acquire PlannerPool (POOL-01) before creating the Job (D-A4).
	if r.PlannerPool != nil {
		if err := r.PlannerPool.Acquire(ctx); err != nil {
			return ctrl.Result{}, err
		}
		defer r.PlannerPool.Release()
	}

	// Step 4: Build caps.
	plannerCaps := podjob.DefaultCaps(nil, podjob.JobKindPlanner)
	if plannerCaps.Iterations <= 0 {
		plannerCaps.Iterations = 20
	}

	// Step 5: Build planner envelope.
	// For ProjectReconciler: level="project", parent=project, project=project (same object).
	attempt := 1
	_, envInJSON, err := BuildPlannerEnvelope("project", project, project, attempt, "", project.Spec.OutcomePrompt, pkgdispatch.Caps{
		WallClockSeconds: int(plannerCaps.WallClockSeconds),
		Iterations:       int(plannerCaps.Iterations),
	}, "https://127.0.0.1:8443", r.HelmProviderDefaults)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build project planner envelope: %w", err)
	}

	// Step 6: Mint signed token for the credproxy sidecar.
	token, err := credproxy.Sign(r.SigningKey, string(project.UID), time.Duration(plannerCaps.WallClockSeconds+podjob.DefaultWallClockGraceSeconds)*time.Second)
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
	opts := podjob.BuildOptions{
		Kind:                 podjob.JobKindPlanner,
		ParentObj:            project,
		Level:                "project",
		Attempt:              attempt,
		Project:              project,
		SignedToken:          token,
		EnvelopeInJSON:       envInJSON,
		SubagentImage:        resolveImage(project, "project", r.HelmProviderDefaults),
		CredproxyImage:       r.CredproxyImage,
		SecretUID:            secretUID,
		PVCName:              "tide-projects",
		ProjectUID:           string(project.UID),
		Caps:                 plannerCaps,
		PricingOverridesJSON: r.PricingOverridesJSON,
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
	project.Status.Phase = tideprojectv1alpha1.PhaseRunning
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionAuthoringPlanner,
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
func (r *ProjectReconciler) handleProjectJobCompletion(ctx context.Context, project *tideprojectv1alpha1.Project, completedJob *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Read tiny status from the dispatch Job's termination message for budget
	// rollup and failure classification. The ChildCRDs field is NOT used here —
	// materialization has moved to the reporter Job (REQ-09-01).
	// Plan 09-08: capture out so we can gate on out.ChildCount below.
	var out pkgdispatch.EnvelopeOut
	envReadOK := false
	if r.EnvReader != nil {
		// project is both the top-level object and its own "parent" at this level;
		// use project.UID as both projectUID and parentUID (the envelope is keyed by
		// the parent's UID, and the Project IS the parent at the project level).
		var readErr error
		out, readErr = r.EnvReader.ReadOut(ctx, string(project.UID), string(project.UID))
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
	if r.ReporterImage == "" {
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
				ReporterOptions{ReporterImage: r.ReporterImage}, r.Scheme)
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

	// Plan 09-08 Defect C: roll up planner-level Usage once per planner Job completion.
	if isFirstCompletion && envReadOK {
		if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
			logger.Error(rollErr, "project planner budget rollup failed (non-fatal)", "project", project.Name)
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
func (r *ProjectReconciler) handleBudgetGate(ctx context.Context, project *tideprojectv1alpha1.Project, now time.Time) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Phase 04.1 P4.1: reset rolling window if elapsed. Failures are logged
	// non-fatal (Pitfall C pattern) — never block dispatch on a tally op.
	if _, err := budget.MaybeResetWindow(ctx, r.Client, project, now); err != nil {
		logger.Error(err, "budget window reset failed (non-fatal)")
	}

	// Existing cap check follows — now sees the post-reset CostSpentCents value.
	bypassed := budget.IsBypassed(project, now)
	capExceeded := budget.IsCapExceeded(project)

	if project.Status.Phase == tideprojectv1alpha1.PhaseBudgetExceeded && bypassed {
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

		// Clear the phase.
		statusPatch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhasePending
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionBudgetExceeded,
			Status:             metav1.ConditionFalse,
			Reason:             tideprojectv1alpha1.ReasonBypassApplied,
			Message:            "Budget exceeded bypass applied by operator",
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		if r.Recorder != nil {
			r.Recorder.Event(project, corev1.EventTypeNormal, tideprojectv1alpha1.ReasonBypassApplied,
				"Budget exceeded bypass applied by operator; dispatch resumed")
		}
		return ctrl.Result{}, nil
	}

	if project.Status.Phase != tideprojectv1alpha1.PhaseBudgetExceeded && capExceeded && !bypassed {
		// Cap hit — set BudgetExceeded and record Event.
		logger.Info("budget cap exceeded; halting dispatch", "project", project.Name)
		statusPatch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhaseBudgetExceeded
		meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
			Type:               tideprojectv1alpha1.ConditionBudgetExceeded,
			Status:             metav1.ConditionTrue,
			Reason:             "AbsoluteCapReached",
			Message:            fmt.Sprintf("Cost spent %d cents exceeds cap %d cents", project.Status.Budget.CostSpentCents, project.Spec.Budget.AbsoluteCapCents),
			LastTransitionTime: metav1.Now(),
		})
		if err := r.Status().Patch(ctx, project, statusPatch); err != nil {
			return ctrl.Result{}, err
		}
		if r.Recorder != nil {
			r.Recorder.Event(project, corev1.EventTypeWarning, "AbsoluteCapReached",
				fmt.Sprintf("Project budget cap reached: %d cents spent of %d cents allowed", project.Status.Budget.CostSpentCents, project.Spec.Budget.AbsoluteCapCents))
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
func (r *ProjectReconciler) buildInitJob(project *tideprojectv1alpha1.Project, pvcName string) *batchv1.Job {
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
								"mkdir -p /workspace/repo /workspace/artifacts /workspace/envelopes && chmod 2775 /workspace/repo /workspace/artifacts /workspace/envelopes",
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
		For(&tideprojectv1alpha1.Project{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			)),
		).
		Owns(&batchv1.Job{}).
		Owns(&tideprojectv1alpha1.Milestone{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("project").
		Complete(r)
}
