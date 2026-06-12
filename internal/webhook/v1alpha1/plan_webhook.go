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

package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	"github.com/jsquirrelz/tide/pkg/dag"
)

// planlog is the named logger for the Plan validating + conversion webhook.
//
// Phase 1: bodies are explicit no-ops (always Allow). Phase 2 fills validation
// logic inside the documented seams below per REQ-PLAN-01 / D-B3.
var planlog = logf.Log.WithName("plan-webhook") //nolint:logcheck // controller-runtime logf idiom; klogr LoggerWithName helper not adopted

// SetupPlanWebhookWithManager registers the validating webhook for Plan with
// the controller-runtime Manager and configures the stateful PlanCustomValidator
// with the cache-backed client, cluster-default file-touch mode, and event recorder.
//
// Phase 2: gains the `defaultMode string` argument (Helm-driven default for
// file-touch validation mode). SetupPlanWebhookWithManager is called from
// internal/controller/suite_test.go BeforeSuite and from cmd/main.go.
func SetupPlanWebhookWithManager(mgr ctrl.Manager, defaultMode string) error {
	return ctrl.NewWebhookManagedBy(mgr, &tideprojectv1alpha1.Plan{}).
		WithValidator(&PlanCustomValidator{
			Client:               mgr.GetClient(),
			DefaultFileTouchMode: defaultMode,
			//nolint:staticcheck // SA1019: GetEventRecorderFor returns record.EventRecorder (the Recorder field's type);
			// GetEventRecorder returns the incompatible events/v1 type — out of scope for lint hygiene.
			Recorder: mgr.GetEventRecorderFor("plan-webhook"),
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-tideproject-k8s-v1alpha1-plan,mutating=false,failurePolicy=fail,sideEffects=None,groups=tideproject.k8s,resources=plans,verbs=create;update,versions=v1alpha1,name=vplan-v1alpha1.kb.io,admissionReviewVersions=v1

// PlanCustomValidator validates Plan objects.
//
// Phase 2: cycle detection via pkg/dag.ComputeWaves (REQ-PLAN-01) and
// file-touch ↔ dependsOn reconciliation (REQ-PLAN-02) with layered strict/warn
// mode (D-E3). The validator is stateful — it holds a cache-backed client
// (mgr.GetClient), the cluster-level default file-touch mode (Helm value),
// and a K8s Event recorder for audit traceability (T-02-11-05).
//
// PLAN-03 invariant: there is no cycle "recovery" code path. The webhook
// rejects and surfaces only. Verified by absence:
//
//	grep -nE 'recoverCycle|cycleRecover|fix.*cycle|skip.*cycle' internal/webhook/v1alpha1/
//
// returns zero matches.
type PlanCustomValidator struct {
	// Client is the cache-backed client from mgr.GetClient().
	// Used to list owned Tasks via the .spec.planRef field indexer.
	Client client.Client

	// DefaultFileTouchMode is the cluster-level default mode from the Helm chart
	// (typically "warn"). Overridden by Plan annotations per D-E3 precedence.
	DefaultFileTouchMode string

	// Recorder emits K8s Events on the Plan for audit traceability (T-02-11-05).
	// Events: Reason=CycleDetected (Warning), Reason=FileTouchMismatch (Warning/Normal).
	Recorder record.EventRecorder
}

// ValidateCreate is invoked on every Plan POST. Delegates to the shared
// validate method which performs cycle detection and file-touch reconciliation.
//
// Signatures preserved per PATTERNS.md "self-extend" rules; only the body changes.
func (v *PlanCustomValidator) ValidateCreate(ctx context.Context, obj *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateCreate", "name", obj.GetName())
	return v.validate(ctx, obj)
}

// ValidateUpdate is invoked on every Plan PUT/PATCH. Re-runs the same cycle
// detection as ValidateCreate — a Plan edit can introduce a cycle that didn't
// exist at create time (D-B3).
//
// Signatures preserved per PATTERNS.md "self-extend" rules; only the body changes.
func (v *PlanCustomValidator) ValidateUpdate(ctx context.Context, _ *tideprojectv1alpha1.Plan, newObj *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateUpdate", "name", newObj.GetName())
	return v.validate(ctx, newObj)
}

// ValidateDelete is invoked on every Plan DELETE. Phase 2 is a no-op;
// the spec lets owner-ref cascade handle Task cleanup.
//
// Phase 3 may add a guard against deleting Plans whose Waves are dispatching.
func (v *PlanCustomValidator) ValidateDelete(_ context.Context, obj *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateDelete (no-op)", "name", obj.GetName())
	return nil, nil
}

// validate performs the full Plan admission validation:
//  1. Lists owned Tasks via the .spec.planRef field indexer (registered by TaskReconciler).
//  2. If no Tasks visible: returns an admission warning (Pitfall B — kubectl-apply order).
//  3. PLAN-01: runs pkg/dag.ComputeWaves; CycleError → rejection + K8s Event.
//  4. PLAN-02: computes file-touch mismatches; strict mode → rejection; warn mode → warnings + Event.
func (v *PlanCustomValidator) validate(ctx context.Context, plan *tideprojectv1alpha1.Plan) (admission.Warnings, error) {
	warnings := admission.Warnings{}

	// List owned Tasks via the .spec.planRef field indexer.
	// The indexer is registered by TaskReconciler.SetupWithManager in Plan 09.
	var taskList tideprojectv1alpha1.TaskList
	if err := v.Client.List(ctx, &taskList,
		client.InNamespace(plan.Namespace),
		client.MatchingFields{".spec.planRef": plan.Name},
	); err != nil {
		return nil, fmt.Errorf("plan webhook: list tasks: %w", err)
	}

	// Pitfall B: informer cache lag — Tasks may not be visible at Plan admission
	// time when `kubectl apply -k` processes Plan before Tasks. Treat as a warning,
	// not a hard rejection, so the apply order doesn't break admission ergonomics.
	if len(taskList.Items) == 0 {
		msg := fmt.Sprintf(
			"plan %s/%s has no owned Tasks visible at admission time; cycle detection will run when Tasks reconcile",
			plan.Namespace, plan.Name)
		planlog.V(1).Info("no owned Tasks visible at admission time (Pitfall B)", "plan", plan.Name)
		warnings = append(warnings, msg)
		return warnings, nil
	}

	// PLAN-01: cycle detection via pkg/dag.ComputeWaves.
	// node = Task.Name; edges from Task.Spec.DependsOn → (DependsOn[i], task.Name).
	nodes, edges := tasksToDAG(taskList.Items)
	if _, err := dag.ComputeWaves(nodes, edges); err != nil {
		var cyc *dag.CycleError
		if errors.As(err, &cyc) {
			// Emit K8s Event for audit traceability (T-02-11-05).
			if v.Recorder != nil {
				v.Recorder.Eventf(plan, corev1.EventTypeWarning, "CycleDetected",
					"cyclic task DAG involving %v", cyc.InvolvedNodes)
			}
			return warnings, fmt.Errorf("plan %s/%s rejected: cyclic task DAG involving %v",
				plan.Namespace, plan.Name, cyc.InvolvedNodes)
		}
		return warnings, fmt.Errorf("plan %s/%s rejected: dag computation failed: %w",
			plan.Namespace, plan.Name, err)
	}

	// PLAN-02: file-touch ↔ dependsOn reconciliation (D-E2).
	// Resolve mode per D-E3 precedence (annotation > resolved-cache > project.Spec > helm default).
	// D-08: walk the owner-ref chain to resolve the real Project so Project.Spec.FileTouchMode
	// drives mode at admission time. On any chain-Get failure, project is nil and we fall back
	// to the annotation-cached or cluster-default mode (nil-fallback preserved — admission never
	// hard-fails on a missing chain; the reconciler gate backstops at dispatch time).
	project := resolveProjectForWebhook(ctx, v.Client, plan)
	mode := ResolveFileTouchMode(plan, project, v.DefaultFileTouchMode)
	mismatches := ComputeFileTouchMismatches(taskList.Items)

	if len(mismatches) > 0 {
		summary := SummariseMismatches(mismatches)
		if mode == "strict" {
			// Strict mode: reject admission and emit Warning event.
			if v.Recorder != nil {
				v.Recorder.Eventf(plan, corev1.EventTypeWarning, "FileTouchMismatch",
					"file-touch mismatches (strict): %s", summary)
			}
			return warnings, fmt.Errorf("plan %s/%s rejected (strict mode): file-touch mismatches: %s",
				plan.Namespace, plan.Name, summary)
		}
		// Warn mode: admit but emit Normal event and return admission warnings.
		if v.Recorder != nil {
			v.Recorder.Eventf(plan, corev1.EventTypeNormal, "FileTouchMismatch",
				"file-touch mismatches (warn mode): %s", summary)
		}
		for _, m := range mismatches {
			warnings = append(warnings,
				fmt.Sprintf("file-touch mismatch on tasks %s/%s sharing path %q without declared dependsOn",
					m.TaskA, m.TaskB, m.SharedPath))
		}
	}

	return warnings, nil
}

// resolveProjectForWebhook walks the Plan → Phase → Milestone → Project owner-ref chain
// using the webhook's client. Returns nil on any Get failure so admission never hard-fails
// on a missing chain (nil-fallback preserved — the reconciler gate backstops at dispatch
// time, addressing D-08).
func resolveProjectForWebhook(ctx context.Context, c client.Client, plan *tideprojectv1alpha1.Plan) *tideprojectv1alpha1.Project {
	if plan.Spec.PhaseRef == "" {
		return nil
	}
	var ph tideprojectv1alpha1.Phase
	if err := c.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &ph); err != nil {
		return nil
	}
	if ph.Spec.MilestoneRef == "" {
		return nil
	}
	var ms tideprojectv1alpha1.Milestone
	if err := c.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		return nil
	}
	if ms.Spec.ProjectRef == "" {
		return nil
	}
	var p tideprojectv1alpha1.Project
	if err := c.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return nil
	}
	return &p
}

// FileTouchMismatchPair records a pair of Tasks that share an EXACT file path
// without a declared dependsOn edge between them. (Pitfall G: same-directory
// siblings — e.g. foo.go + foo_test.go — do NOT generate derived edges because
// they share only a directory prefix, not an exact path.)
//
// Exported so PlanReconciler can call ComputeFileTouchMismatches and use the
// result without importing private types from this package.
type FileTouchMismatchPair struct {
	TaskA      string
	TaskB      string
	SharedPath string // the EXACT path shared between TaskA.filesTouched and TaskB.filesTouched
}

// tasksToDAG translates a slice of Task CRDs into the (nodes, edges) form
// consumed by pkg/dag.ComputeWaves.
//
// node  = task.Name
// edge  = (DependsOn[i], task.Name) — "DependsOn[i] must complete before task"
func tasksToDAG(tasks []tideprojectv1alpha1.Task) ([]dag.NodeID, []dag.Edge) {
	nodes := make([]dag.NodeID, 0, len(tasks))
	var edges []dag.Edge

	for i := range tasks {
		t := &tasks[i]
		nodes = append(nodes, t.Name)
		for _, dep := range t.Spec.DependsOn {
			edges = append(edges, dag.Edge{From: dep, To: t.Name})
		}
	}
	return nodes, edges
}

// ComputeFileTouchMismatches returns pairs of Tasks (a, b) where their
// filesTouched sets overlap on EXACT path equality AND no declared dependsOn
// edge exists in either direction.
//
// Exported so the PlanReconciler dispatch gate (D-05) can call it after Tasks
// materialize — the webhook remains the early-admission layer; the reconciler
// is the authoritative seat that sees reporter-flow Tasks.
//
// Algorithm (EXACT-equality only — Pitfall G defense):
//  1. Build a name → declared-dependsOn set for O(1) edge lookup.
//  2. For each pair (a, b) with a.Name < b.Name (lexicographic — avoids duplicates):
//     - Compute exact intersection of a.FilesTouched ∩ b.FilesTouched.
//     - If empty → skip (no overlap).
//     - If b.Name in a.DependsOn OR a.Name in b.DependsOn → declared edge; skip.
//     - Else → append one FileTouchMismatchPair per shared path.
//  3. Return the list.
//
// Complexity: O(N² × P) where N = task count, P = average filesTouched length.
// Acceptable for v1 Plans bounded to ≤20 Tasks per RESEARCH.md.
func ComputeFileTouchMismatches(tasks []tideprojectv1alpha1.Task) []FileTouchMismatchPair {
	// Build name → dependsOn set for fast lookup.
	dependsOnSet := make(map[string]map[string]struct{}, len(tasks))
	for i := range tasks {
		t := &tasks[i]
		deps := make(map[string]struct{}, len(t.Spec.DependsOn))
		for _, d := range t.Spec.DependsOn {
			deps[d] = struct{}{}
		}
		dependsOnSet[t.Name] = deps
	}

	var mismatches []FileTouchMismatchPair

	for i := range tasks {
		for j := i + 1; j < len(tasks); j++ {
			a := &tasks[i]
			b := &tasks[j]

			// Canonical ordering: ensure a.Name < b.Name to avoid duplicate pairs.
			if a.Name > b.Name {
				a, b = b, a
			}

			// Compute EXACT intersection of filesTouched.
			// Pitfall G: "pkg/x/y.go" and "pkg/x/y_test.go" are different strings —
			// they do NOT intersect. Only identical path strings match.
			bFiles := make(map[string]struct{}, len(b.Spec.FilesTouched))
			for _, f := range b.Spec.FilesTouched {
				bFiles[f] = struct{}{}
			}

			var shared []string
			for _, f := range a.Spec.FilesTouched {
				if _, ok := bFiles[f]; ok {
					shared = append(shared, f)
				}
			}

			if len(shared) == 0 {
				continue
			}

			// Check for declared dependsOn edge in either direction.
			if _, depAtoB := dependsOnSet[b.Name][a.Name]; depAtoB {
				continue // b depends on a — declared; no mismatch
			}
			if _, depBtoA := dependsOnSet[a.Name][b.Name]; depBtoA {
				continue // a depends on b — declared; no mismatch
			}

			// Undeclared overlap: record one entry per shared path.
			for _, p := range shared {
				mismatches = append(mismatches, FileTouchMismatchPair{
					TaskA:      a.Name,
					TaskB:      b.Name,
					SharedPath: p,
				})
			}
		}
	}

	return mismatches
}

// SummariseMismatches returns a compact human-readable string of all mismatches
// for use in error messages and K8s Events.
//
// Exported so PlanReconciler can build the condition Message that names both
// tasks and the shared path (T-15-07 mitigaton).
func SummariseMismatches(mismatches []FileTouchMismatchPair) string {
	parts := make([]string, 0, len(mismatches))
	for _, m := range mismatches {
		parts = append(parts, fmt.Sprintf("(%s,%s)@%q", m.TaskA, m.TaskB, m.SharedPath))
	}
	return strings.Join(parts, "; ")
}
