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

// import_controller.go — one-shot pre-reconcile import state machine for
// envelope salvage (Phase 28, IMPORT-01/03/04/05, D-01..D-12).
//
// State machine: Pending → CreatingCRs → CopyingEnvelopes → Complete / Failed.
//
// The controller reads the seed ConfigMap (named by Project.Spec.ImportSource.SeedManifestConfigMap),
// runs dag.ComputeWaves on the SEED-DERIVED planning DAG (nodes = Milestone/Phase/Plan CR
// names that carry dependsOn; edges = their dependsOn refs) BEFORE any client.Create
// (D-10), materializes the CR tree with new UIDs and records the FQ-name → oldUID → newUID
// rekey table, dispatches the tide-import Job (two PVC subPath mounts, D-05 / IMPORT-05),
// and on Job success sets ConditionImportComplete=True.
//
// Design invariants:
//   - NEVER creates Wave CRs (D-09) — waves are re-derived by deriveGlobalWaves after import.
//   - Idempotent: ConditionImportComplete=True → immediate no-op return (D-12).
//   - Cycle detection on the seed's OWN planning DAG (not buildGlobalEdges, which is empty
//     under the Task-less D-04 seed). A plan-A ↔ plan-B dependsOn cycle would pass
//     buildGlobalEdges silently; the seed-derived check catches it before any client.Create.
//   - Budget rollup suppression (D-11) is applied at handleProjectJobCompletion (plan 05);
//     this controller only patches status + dispatches the Job.
package controller

import (
	"context"
	"encoding/json"
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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/owner"
	"github.com/jsquirrelz/tide/pkg/dag"
)

// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=projects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=milestones;phases;plans,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// importStatePhase represents the internal state machine phase for the import
// process, tracked via Project.Status.Conditions.
type importStatePhase string

const (
	importStateCreatingCRs      importStatePhase = "CreatingCRs"
	importStateCopyingEnvelopes importStatePhase = "CopyingEnvelopes"
	importStateComplete         importStatePhase = "Complete"
	importStateFailed           importStatePhase = "Failed"
)

// importSAName is the dedicated least-privilege SA for the import Job pod.
// Mirrors reporterSAName in reporter_jobspec.go.
const importSAName = "tide-import"

// importJobRoleLabel is the value of the tideproject.k8s/role label on import Jobs.
// Discriminates import Jobs from planner/reporter/dispatch Jobs in completion handlers.
const importJobRoleLabel = "importer"

// seedEntry is the JSON schema for each entry in the seed ConfigMap's
// milestones/phases/plans arrays. FQ-name is used as the rekey table key (D-07).
// The seed is produced by the Phase 29 export tooling; this struct must match that schema.
type seedEntry struct {
	// Name is the short K8s object name (used as the CR name).
	Name string `json:"name"`

	// FQName is the fully-qualified name (milestone-name/phase-name/plan-name)
	// used as the rekey table key to guarantee uniqueness across sibling subtrees
	// that reuse short names (D-07).
	FQName string `json:"fqName"`

	// OldUID is the UID from the salvaged run; used to locate the old PVC subPath.
	OldUID string `json:"oldUID"`

	// DependsOn is the list of FQ-name dependsOn references (mirrors Spec.DependsOn).
	// These are the seed-DAG edges consumed by the import cycle check (D-10).
	DependsOn []string `json:"dependsOn,omitempty"`

	// Status is the initial Status.Phase to patch onto the created CR so that
	// the controller sees the correct completion state immediately (Anti-Pattern 4).
	Status string `json:"status,omitempty"`

	// PhaseRef is the owning Phase name (for Plan entries only).
	PhaseRef string `json:"phaseRef,omitempty"`

	// MilestoneRef is the owning Milestone name (for Phase entries only).
	MilestoneRef string `json:"milestoneRef,omitempty"`

	// ProjectRef is the owning Project name (for Milestone entries only).
	ProjectRef string `json:"projectRef,omitempty"`
}

// seedManifest is the top-level structure of the seed ConfigMap's JSON value.
// Keys "milestones", "phases", "plans" each hold an array of seedEntry objects.
type seedManifest struct {
	Milestones []seedEntry `json:"milestones"`
	Phases     []seedEntry `json:"phases"`
	Plans      []seedEntry `json:"plans"`
}

// rekeyRow is one row of the rekey table written to the rekey ConfigMap. It is
// the exact wire shape the tide-import binary decodes from /rekey/rekey.json
// (a JSON ARRAY of these rows — see cmd/tide-import/main.go rekeyEntry). The
// binary keys the source/dest directories off OldUID/NewUID, so every row must
// carry both UIDs and the fully-qualified name (D-03/D-07).
type rekeyRow struct {
	FQName string `json:"fqName"`
	OldUID string `json:"oldUID"`
	NewUID string `json:"newUID"`
}

// ImportReconciler implements the one-shot pre-reconcile import state machine.
// It drives Project objects that carry Spec.ImportSource from Pending through
// CreatingCRs → CopyingEnvelopes → Complete (or Failed).
type ImportReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// MaxConcurrentReconciles is the per-Kind reconcile parallelism budget.
	MaxConcurrentReconciles int

	// WatchNamespace narrows the watch (AUTH-02). Empty = watch-all-namespaces.
	WatchNamespace string

	// ImportImage is the image ref for the tide-import Job container.
	// When empty, Job creation is skipped (mirrors ReporterImage skip pattern).
	ImportImage string

	// SharedPVCName is the name of the cluster-wide PVC (default "tide-projects").
	// Falls back to defaultSharedPVCName when empty.
	SharedPVCName string
}

// sharedPVCNameForImport returns the configured shared PVC name or the default.
func (r *ImportReconciler) sharedPVCNameForImport() string {
	if r.SharedPVCName != "" {
		return r.SharedPVCName
	}
	return defaultSharedPVCName
}

// Reconcile implements the import state machine for a Project with Spec.ImportSource set.
//
// State transitions:
//
//	Pending → CreatingCRs:   read seed ConfigMap, run cycle detection on seed planning DAG,
//	                          materialize Milestone/Phase/Plan CRs, record rekey ConfigMap.
//	CreatingCRs → CopyingEnvelopes: dispatch tide-import Job (two-subPath PVC mount).
//	CopyingEnvelopes → Complete: tide-import Job succeeded → ConditionImportComplete=True.
//	Any → Failed: cycle detected or unresolved ref → ConditionImportComplete=False with
//	              ReasonCyclicPlanDetected or ReasonImportFailed; zero CRs created.
func (r *ImportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// 1. Fetch.
	var project tideprojectv1alpha2.Project
	if err := r.Get(ctx, req.NamespacedName, &project); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fast path: only act on Projects with importSource declared.
	if project.Spec.ImportSource == nil {
		return ctrl.Result{}, nil
	}

	// Idempotency guard (D-12): ConditionImportComplete=True → no-op.
	// Honor AnnotationRetryImport to reset on operator retry.
	cond := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha2.ConditionImportComplete)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		// Check for retry annotation: if present, reset and continue.
		if _, hasRetry := project.Annotations[tideprojectv1alpha2.AnnotationRetryImport]; !hasRetry {
			return ctrl.Result{}, nil
		}
		// Retry annotation present: clear the condition and proceed.
		logger.Info("import retry annotation detected; resetting ImportComplete", "project", project.Name)
		if err := r.setImportCondition(ctx, &project, metav1.ConditionFalse,
			tideprojectv1alpha2.ReasonImportFailed, "retrying import per operator annotation"); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Determine current import state from conditions.
	state := r.currentImportState(&project)

	switch state {
	case importStateFailed:
		// Already failed and no retry annotation: park.
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil

	case importStateComplete:
		// Should not reach here (idempotency guard above), but be safe.
		return ctrl.Result{}, nil

	case importStateCreatingCRs, "":
		// Pending or CreatingCRs: read seed and materialize CRs.
		return r.reconcileCreatingCRs(ctx, &project)

	case importStateCopyingEnvelopes:
		// CRs materialized; check/dispatch Job.
		return r.reconcileCopyingEnvelopes(ctx, &project)
	}

	return ctrl.Result{}, nil
}

// currentImportState reads the current import phase from Project.Status.Conditions
// and Job existence. The state machine is tracked via the ConditionImportComplete
// condition status + reason, supplemented by a phase annotation.
//
// TODO(IN-01): in-progress states are distinguished by string-matching the
// human-facing cond.Message ("CreatingCRs"/"CopyingEnvelopes"), which is
// fragile — a message copyedit would silently break transitions. Track the
// import phase in a machine-owned field (a per-phase Reason constant or a
// Status.Import.Phase enum) instead. Deferred from the Phase 28 fix pass to
// avoid destabilizing the state machine; the reason-checks below are also
// partly unreachable.
func (r *ImportReconciler) currentImportState(project *tideprojectv1alpha2.Project) importStatePhase {
	cond := meta.FindStatusCondition(project.Status.Conditions, tideprojectv1alpha2.ConditionImportComplete)
	if cond == nil {
		return "" // Pending
	}
	if cond.Status == metav1.ConditionTrue {
		return importStateComplete
	}
	// False condition with specific reasons:
	if cond.Reason == tideprojectv1alpha2.ReasonCyclicPlanDetected ||
		cond.Reason == tideprojectv1alpha2.ReasonImportFailed {
		// Check if this is a terminal failure vs in-progress marker.
		// In-progress states use ReasonImportFailed with specific messages.
		// We use the message to distinguish in-progress vs terminal failures.
		if cond.Message == "CreatingCRs" {
			return importStateCreatingCRs
		}
		if cond.Message == "CopyingEnvelopes" {
			return importStateCopyingEnvelopes
		}
		return importStateFailed
	}
	if cond.Reason == "CreatingCRs" {
		return importStateCreatingCRs
	}
	if cond.Reason == "CopyingEnvelopes" {
		return importStateCopyingEnvelopes
	}
	return importStateFailed
}

// reconcileCreatingCRs handles the Pending → CreatingCRs → CopyingEnvelopes transition.
// It reads the seed ConfigMap, runs cycle detection on the seed-derived planning DAG,
// materializes the CR tree, records the rekey ConfigMap, then transitions to CopyingEnvelopes.
//
//nolint:gocyclo // a flat sequence of mutually-exclusive transition arms over a freshly E2E-validated import path; splitting obscures the contract
func (r *ImportReconciler) reconcileCreatingCRs(ctx context.Context, project *tideprojectv1alpha2.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.Info("import: creating CRs from seed", "project", project.Name)

	// Read seed ConfigMap.
	var seedCM corev1.ConfigMap
	seedCMKey := types.NamespacedName{
		Namespace: project.Namespace,
		Name:      project.Spec.ImportSource.SeedManifestConfigMap,
	}
	if err := r.Get(ctx, seedCMKey, &seedCM); err != nil {
		if apierrors.IsNotFound(err) {
			// Seed ConfigMap not yet created; requeue.
			logger.Info("import: seed ConfigMap not found; requeueing", "configmap", seedCMKey.Name)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("import: get seed ConfigMap %s: %w", seedCMKey.Name, err)
	}

	// Parse the seed manifest JSON from the "manifest" key.
	rawManifest, ok := seedCM.Data["manifest"]
	if !ok {
		logger.Info("import: seed ConfigMap missing 'manifest' key; failing")
		return ctrl.Result{}, r.failImport(ctx, project, tideprojectv1alpha2.ReasonImportFailed,
			"seed ConfigMap missing 'manifest' key")
	}

	var seed seedManifest
	if err := json.Unmarshal([]byte(rawManifest), &seed); err != nil {
		logger.Error(err, "import: failed to parse seed manifest JSON")
		return ctrl.Result{}, r.failImport(ctx, project, tideprojectv1alpha2.ReasonImportFailed,
			fmt.Sprintf("seed manifest JSON parse error: %v", err))
	}

	// === CYCLE DETECTION (D-10) — BEFORE any client.Create ===
	//
	// Build the seed-derived planning DAG: nodes = all Milestone/Phase/Plan CR names;
	// edges = {From: dep, To: crName} for each dep in each CR's DependsOn.
	//
	// This is NOT buildGlobalEdges (which projects coarse dependsOn down onto member
	// Tasks via tasksByPlan). Under D-04 the seed has ZERO Tasks, making that projection
	// edgeless and incapable of detecting a plan-A ↔ plan-B dependsOn cycle.
	//
	// The seed-derived DAG operates at the Milestone/Phase/Plan level — exactly the
	// level the seed encodes — and catches cycles that client.Create would otherwise
	// bypass (client.Create skips the validating admission webhook, D-10).
	var seedNodes []dag.NodeID
	var seedEdges []dag.Edge
	allCRNames := make(map[string]struct{})

	// Collect all CR names as nodes (include all so unresolved refs produce unknown-node errors).
	for _, ms := range seed.Milestones {
		allCRNames[ms.Name] = struct{}{}
		seedNodes = append(seedNodes, ms.Name)
	}
	for _, ph := range seed.Phases {
		allCRNames[ph.Name] = struct{}{}
		seedNodes = append(seedNodes, ph.Name)
	}
	for _, pl := range seed.Plans {
		allCRNames[pl.Name] = struct{}{}
		seedNodes = append(seedNodes, pl.Name)
	}

	// Build edges from DependsOn refs.
	for _, ms := range seed.Milestones {
		for _, dep := range ms.DependsOn {
			seedEdges = append(seedEdges, dag.Edge{From: dep, To: ms.Name})
		}
	}
	for _, ph := range seed.Phases {
		for _, dep := range ph.DependsOn {
			seedEdges = append(seedEdges, dag.Edge{From: dep, To: ph.Name})
		}
	}
	for _, pl := range seed.Plans {
		for _, dep := range pl.DependsOn {
			seedEdges = append(seedEdges, dag.Edge{From: dep, To: pl.Name})
		}
	}

	_, dagErr := dag.ComputeWaves(seedNodes, seedEdges)
	if dagErr != nil {
		var cycleErr *dag.CycleError
		if errors.As(dagErr, &cycleErr) {
			logger.Info("import: cyclic dependency detected in seed planning DAG",
				"involved", cycleErr.InvolvedNodes, "project", project.Name)
			return ctrl.Result{}, r.failImport(ctx, project,
				tideprojectv1alpha2.ReasonCyclicPlanDetected,
				fmt.Sprintf("cyclic dependency in seed planning DAG: %v", cycleErr.InvolvedNodes))
		}
		// Unknown-node error: an unresolved dependsOn ref.
		logger.Info("import: unresolved dependsOn reference in seed", "error", dagErr, "project", project.Name)
		return ctrl.Result{}, r.failImport(ctx, project,
			tideprojectv1alpha2.ReasonImportFailed,
			fmt.Sprintf("unresolved dependsOn reference in seed: %v", dagErr))
	}

	// Cycle detection passed. Now materialize the CR tree in parent-first order.
	// Collect oldUID → newUID mappings for the rekey ConfigMap (D-03/D-07).
	// The table is a JSON ARRAY of rekeyRow — the exact shape the tide-import
	// binary decodes (CR-02: a map would fail to unmarshal into []rekeyEntry).
	var rekeyTable []rekeyRow

	// Materialize Milestones first (they have no parent other than Project).
	for _, msSeed := range seed.Milestones {
		projectRef := msSeed.ProjectRef
		if projectRef == "" {
			projectRef = project.Name
		}
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{
				Name:      msSeed.Name,
				Namespace: project.Namespace,
			},
			Spec: tideprojectv1alpha2.MilestoneSpec{
				ProjectRef: projectRef,
				DependsOn:  msSeed.DependsOn,
			},
		}
		if err := owner.EnsureOwnerRef(ms, project, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("import: EnsureOwnerRef Milestone %s: %w", ms.Name, err)
		}
		if err := r.Create(ctx, ms); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, fmt.Errorf("import: create Milestone %s: %w", ms.Name, err)
			}
			// AlreadyExists: idempotent success. Fetch to get the new UID.
			if fetchErr := r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: msSeed.Name}, ms); fetchErr != nil {
				return ctrl.Result{}, fmt.Errorf("import: get existing Milestone %s: %w", ms.Name, fetchErr)
			}
		}
		fqName := msSeed.FQName
		if fqName == "" {
			fqName = msSeed.Name
		}
		rekeyTable = append(rekeyTable, rekeyRow{FQName: fqName, OldUID: msSeed.OldUID, NewUID: string(ms.UID)})

		// Patch initial Status.Phase from seed (Anti-Pattern 4: not blanket Succeeded).
		if msSeed.Status != "" {
			statusPatch := client.MergeFrom(ms.DeepCopy())
			ms.Status.Phase = msSeed.Status
			if patchErr := r.Status().Patch(ctx, ms, statusPatch); patchErr != nil {
				logger.V(1).Info("import: could not patch Milestone status phase (non-fatal)", "name", ms.Name, "err", patchErr)
			}
		}
	}

	// Materialize Phases (parent = Milestone).
	for _, phSeed := range seed.Phases {
		phaseRef := phSeed.MilestoneRef
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      phSeed.Name,
				Namespace: project.Namespace,
			},
			Spec: tideprojectv1alpha2.PhaseSpec{
				MilestoneRef: phaseRef,
				DependsOn:    phSeed.DependsOn,
			},
		}
		// Owner ref to the parent Milestone (if it exists); fall back to Project.
		var msOwner metav1.Object = project
		if phaseRef != "" {
			var parentMS tideprojectv1alpha2.Milestone
			if getErr := r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: phaseRef}, &parentMS); getErr == nil {
				msOwner = &parentMS
			}
		}
		if err := owner.EnsureOwnerRef(ph, msOwner, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("import: EnsureOwnerRef Phase %s: %w", ph.Name, err)
		}
		if err := r.Create(ctx, ph); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, fmt.Errorf("import: create Phase %s: %w", ph.Name, err)
			}
			if fetchErr := r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: phSeed.Name}, ph); fetchErr != nil {
				return ctrl.Result{}, fmt.Errorf("import: get existing Phase %s: %w", ph.Name, fetchErr)
			}
		}
		fqName := phSeed.FQName
		if fqName == "" {
			fqName = phSeed.Name
		}
		rekeyTable = append(rekeyTable, rekeyRow{FQName: fqName, OldUID: phSeed.OldUID, NewUID: string(ph.UID)})

		if phSeed.Status != "" {
			statusPatch := client.MergeFrom(ph.DeepCopy())
			ph.Status.Phase = phSeed.Status
			if patchErr := r.Status().Patch(ctx, ph, statusPatch); patchErr != nil {
				logger.V(1).Info("import: could not patch Phase status phase (non-fatal)", "name", ph.Name, "err", patchErr)
			}
		}
	}

	// Materialize Plans (parent = Phase).
	for _, plSeed := range seed.Plans {
		phaseRef := plSeed.PhaseRef
		pl := &tideprojectv1alpha2.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      plSeed.Name,
				Namespace: project.Namespace,
			},
			Spec: tideprojectv1alpha2.PlanSpec{
				PhaseRef:  phaseRef,
				DependsOn: plSeed.DependsOn,
			},
		}
		// Owner ref to the parent Phase (if it exists); fall back to Project.
		var phOwner metav1.Object = project
		if phaseRef != "" {
			var parentPh tideprojectv1alpha2.Phase
			if getErr := r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: phaseRef}, &parentPh); getErr == nil {
				phOwner = &parentPh
			}
		}
		if err := owner.EnsureOwnerRef(pl, phOwner, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("import: EnsureOwnerRef Plan %s: %w", pl.Name, err)
		}
		if err := r.Create(ctx, pl); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, fmt.Errorf("import: create Plan %s: %w", pl.Name, err)
			}
			if fetchErr := r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: plSeed.Name}, pl); fetchErr != nil {
				return ctrl.Result{}, fmt.Errorf("import: get existing Plan %s: %w", pl.Name, fetchErr)
			}
		}
		fqName := plSeed.FQName
		if fqName == "" {
			fqName = plSeed.Name
		}
		rekeyTable = append(rekeyTable, rekeyRow{FQName: fqName, OldUID: plSeed.OldUID, NewUID: string(pl.UID)})

		if plSeed.Status != "" {
			statusPatch := client.MergeFrom(pl.DeepCopy())
			pl.Status.Phase = plSeed.Status
			// GAP-12: arm the wave-materialization path for imported plans. Plan
			// succession (reconcileWaveMaterialization → BoundaryDetected → Succeeded)
			// is gated on ValidationState=Validated, which is normally stamped from
			// the planner Job's tiny-status (plan_controller.go:617, envReadOK). An
			// imported plan has no planner Job/pod for the manager to read, so that
			// stamp never fires and the plan parks at Running forever even after its
			// reporter materializes Tasks that all Succeed. The plan's planning IS
			// validated (its envelope passed the import completeness guard), so stamp
			// it here — mirroring the status.phase patch above. This only ARMS the
			// path; reconcileWaveMaterialization still re-runs ComputeWaves + file-
			// touch checks every reconcile and BoundaryDetected's childless guard
			// prevents any false Succeeded before Tasks exist.
			pl.Status.ValidationState = "Validated"
			if patchErr := r.Status().Patch(ctx, pl, statusPatch); patchErr != nil {
				logger.V(1).Info("import: could not patch Plan status (non-fatal)", "name", pl.Name, "err", patchErr)
			}
		}
	}

	// Write rekey ConfigMap (D-03/D-07, Pitfall 6: deterministic name).
	rekeyJSON, err := json.Marshal(rekeyTable)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("import: marshal rekey table: %w", err)
	}
	rekeyCMName := fmt.Sprintf("tide-import-rekey-%s", project.UID)
	rekeyCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rekeyCMName,
			Namespace: project.Namespace,
		},
		Data: map[string]string{
			// Key == on-disk filename: the whole-CM mount at /rekey produces
			// /rekey/rekey.json, which the Job's --rekey-file flag references (CR-03).
			"rekey.json": string(rekeyJSON),
		},
	}
	if cmOwnerErr := owner.EnsureOwnerRef(rekeyCM, project, r.Scheme); cmOwnerErr != nil {
		return ctrl.Result{}, fmt.Errorf("import: EnsureOwnerRef rekey ConfigMap: %w", cmOwnerErr)
	}
	if err := r.Create(ctx, rekeyCM); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("import: create rekey ConfigMap %s: %w", rekeyCMName, err)
		}
		// AlreadyExists: idempotent; the rekey table was already written.
	}

	logger.Info("import: CRs materialized; transitioning to CopyingEnvelopes",
		"project", project.Name,
		"milestones", len(seed.Milestones),
		"phases", len(seed.Phases),
		"plans", len(seed.Plans))

	// Transition: CreatingCRs → CopyingEnvelopes.
	if err := r.setImportPhaseCondition(ctx, project, "CopyingEnvelopes",
		"seed CRs materialized; dispatching import Job"); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// reconcileCopyingEnvelopes handles the CopyingEnvelopes phase: dispatch the
// tide-import Job (if not already running/complete) and watch for completion.
func (r *ImportReconciler) reconcileCopyingEnvelopes(ctx context.Context, project *tideprojectv1alpha2.Project) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	jobName := fmt.Sprintf("tide-import-%s", project.UID)

	// Fetch the import Job (if it exists).
	var importJob batchv1.Job
	jobKey := types.NamespacedName{Namespace: project.Namespace, Name: jobName}
	jobErr := r.Get(ctx, jobKey, &importJob)

	if apierrors.IsNotFound(jobErr) {
		// Job doesn't exist yet; dispatch it (or skip if ImportImage is empty).
		if r.ImportImage == "" {
			// Dev mode: skip Job; treat as success (mirrors ReporterImage empty-skip).
			logger.Info("import: ImportImage empty; skipping Job creation, marking complete", "project", project.Name)
			return ctrl.Result{}, r.succeedImport(ctx, project)
		}

		// Read the rekey ConfigMap to pass to the Job.
		rekeyCMName := fmt.Sprintf("tide-import-rekey-%s", project.UID)
		var rekeyCM corev1.ConfigMap
		if getErr := r.Get(ctx, types.NamespacedName{Namespace: project.Namespace, Name: rekeyCMName}, &rekeyCM); getErr != nil {
			if apierrors.IsNotFound(getErr) {
				// Rekey ConfigMap not yet visible; requeue.
				return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
			}
			return ctrl.Result{}, fmt.Errorf("import: get rekey ConfigMap %s: %w", rekeyCMName, getErr)
		}

		// Build and dispatch the import Job.
		job := BuildImportJob(project, ImportJobOptions{
			ImportImage:   r.ImportImage,
			SharedPVCName: r.sharedPVCNameForImport(),
			OldSubPath:    project.Spec.ImportSource.SalvagedPVCSubPath,
			NewSubPath:    fmt.Sprintf("%s/workspace", project.UID),
			RekeyCMName:   rekeyCMName,
		}, r.Scheme)

		if err := r.Create(ctx, job); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, fmt.Errorf("import: create import Job %s: %w", jobName, err)
			}
			// AlreadyExists: idempotent.
		}
		logger.Info("import: dispatched tide-import Job", "job", jobName, "project", project.Name)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if jobErr != nil {
		return ctrl.Result{}, fmt.Errorf("import: get import Job %s: %w", jobName, jobErr)
	}

	// Job exists — check for completion.
	for _, cond := range importJob.Status.Conditions {
		if cond.Type == batchv1.JobComplete && cond.Status == corev1.ConditionTrue {
			logger.Info("import: tide-import Job succeeded; marking ImportComplete=True",
				"job", jobName, "project", project.Name)
			return ctrl.Result{}, r.succeedImport(ctx, project)
		}
		if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
			logger.Info("import: tide-import Job failed; marking ImportComplete=False",
				"job", jobName, "project", project.Name)
			return ctrl.Result{}, r.failImport(ctx, project,
				tideprojectv1alpha2.ReasonImportFailed,
				fmt.Sprintf("tide-import Job %s failed", jobName))
		}
	}

	// Job still running; requeue to poll.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// setImportCondition patches Project.Status.Conditions[ConditionImportComplete].
// Uses the MergeFrom + Status().Patch pattern (mirrors ProjectReconciler).
func (r *ImportReconciler) setImportCondition(
	ctx context.Context,
	project *tideprojectv1alpha2.Project,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	statusPatch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha2.ConditionImportComplete,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	return r.Status().Patch(ctx, project, statusPatch)
}

// setImportPhaseCondition sets a False condition with a phase-name reason for
// in-progress state tracking (CreatingCRs / CopyingEnvelopes).
func (r *ImportReconciler) setImportPhaseCondition(
	ctx context.Context,
	project *tideprojectv1alpha2.Project,
	reason, message string,
) error {
	statusPatch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha2.ConditionImportComplete,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	return r.Status().Patch(ctx, project, statusPatch)
}

// failImport sets ConditionImportComplete=False with the given reason and message.
// Creates ZERO child CRs (call before any client.Create for atomicity — D-10).
func (r *ImportReconciler) failImport(
	ctx context.Context,
	project *tideprojectv1alpha2.Project,
	reason, message string,
) error {
	return r.setImportCondition(ctx, project, metav1.ConditionFalse, reason, message)
}

// succeedImport sets ConditionImportComplete=True with ReasonImportSucceeded.
func (r *ImportReconciler) succeedImport(ctx context.Context, project *tideprojectv1alpha2.Project) error {
	return r.setImportCondition(ctx, project, metav1.ConditionTrue,
		tideprojectv1alpha2.ReasonImportSucceeded,
		"tide-import Job completed; all envelopes copied and schema-converted to new-UID paths")
}

// SetupWithManager wires the ImportReconciler into the manager.
// Watches Projects (GenerationChanged OR AnnotationChanged), Owns Jobs.
// Named "import" to avoid conflicts with other controllers.
func (r *ImportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha2.Project{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			)),
		).
		Owns(&batchv1.Job{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("import").
		Complete(r)
}
