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

package v1alpha2

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/pkg/dag"
)

// planlog is the named logger for the Plan validating webhook.
var planlog = logf.Log.WithName("plan-webhook-v1alpha2") //nolint:logcheck // controller-runtime logf idiom

// SetupPlanWebhookWithManager registers the validating webhook for v1alpha2.Plan with
// the controller-runtime Manager. The stateful PlanCustomValidator is wired with the
// cache-backed client, cluster-default file-touch mode, and event recorder.
func SetupPlanWebhookWithManager(mgr ctrl.Manager, defaultMode string) error {
	return ctrl.NewWebhookManagedBy(mgr, &tideprojectv1alpha2.Plan{}).
		WithValidator(&PlanCustomValidator{
			Client:               mgr.GetClient(),
			DefaultFileTouchMode: defaultMode,
			//nolint:staticcheck // SA1019: GetEventRecorderFor returns record.EventRecorder (canonical kubebuilder pattern)
			Recorder: mgr.GetEventRecorderFor("plan-webhook"),
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-tideproject-k8s-v1alpha2-plan,mutating=false,failurePolicy=fail,sideEffects=None,groups=tideproject.k8s,resources=plans,verbs=create;update,versions=v1alpha2,name=vplan-v1alpha2.kb.io,admissionReviewVersions=v1

// PlanCustomValidator validates v1alpha2.Plan objects.
//
// Phase 2: cycle detection via pkg/dag.ComputeWaves (REQ-PLAN-01) and file-touch
// reconciliation (REQ-PLAN-02) with strict/warn mode (D-E3).
//
// Cross-scope dependsOn (Pitfall 6 / T-23-06): in v1alpha2, Task.Spec.DependsOn may
// name tasks outside this plan (cross-scope deps, DEPS-01). The per-plan cycle webhook
// filters these to within-plan edges only before calling ComputeWaves so "edge references
// unknown node" never triggers here. Cross-scope cycle detection is the global gate in
// the Project reconciler (Plan 23-03, DEPS-03); this webhook validates within-plan edges.
type PlanCustomValidator struct {
	// Client is the cache-backed client from mgr.GetClient().
	Client client.Client
	// DefaultFileTouchMode is the cluster-level default mode from the Helm chart.
	DefaultFileTouchMode string
	// Recorder emits K8s Events for audit traceability (T-02-11-05).
	Recorder record.EventRecorder
}

// ValidateCreate is invoked on every Plan POST.
func (v *PlanCustomValidator) ValidateCreate(ctx context.Context, obj *tideprojectv1alpha2.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateCreate", "name", obj.GetName())
	return v.validate(ctx, obj)
}

// ValidateUpdate is invoked on every Plan PUT/PATCH. Re-runs the same validation
// as ValidateCreate — an edit can introduce a cycle (D-B3).
func (v *PlanCustomValidator) ValidateUpdate(ctx context.Context, _ *tideprojectv1alpha2.Plan, newObj *tideprojectv1alpha2.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateUpdate", "name", newObj.GetName())
	return v.validate(ctx, newObj)
}

// ValidateDelete is a no-op; owner-ref cascade handles Task cleanup.
func (v *PlanCustomValidator) ValidateDelete(_ context.Context, obj *tideprojectv1alpha2.Plan) (admission.Warnings, error) {
	planlog.V(1).Info("ValidateDelete (no-op)", "name", obj.GetName())
	return nil, nil
}

// validate performs the full Plan admission validation:
//  1. Lists owned Tasks via the .spec.planRef field indexer.
//  2. If no Tasks visible: returns an admission warning (Pitfall B — kubectl-apply order).
//  3. PLAN-01: runs pkg/dag.ComputeWaves on within-plan edges only;
//     CycleError → rejection + K8s Event. Cross-scope deps are filtered out before
//     ComputeWaves to prevent "edge references unknown node" errors (Pitfall 6 / T-23-06).
//  4. PLAN-02: computes file-touch mismatches; strict mode → rejection; warn → warnings + Event.
func (v *PlanCustomValidator) validate(ctx context.Context, plan *tideprojectv1alpha2.Plan) (admission.Warnings, error) {
	warnings := admission.Warnings{}

	// List owned Tasks via the .spec.planRef field indexer.
	var taskList tideprojectv1alpha2.TaskList
	if err := v.Client.List(ctx, &taskList,
		client.InNamespace(plan.Namespace),
		client.MatchingFields{".spec.planRef": plan.Name},
	); err != nil {
		return nil, fmt.Errorf("plan webhook: list tasks: %w", err)
	}

	// Pitfall B: informer cache lag — Tasks may not be visible at Plan admission
	// time. Treat as a warning, not a hard rejection.
	if len(taskList.Items) == 0 {
		msg := fmt.Sprintf(
			"plan %s/%s has no owned Tasks visible at admission time; cycle detection will run when Tasks reconcile",
			plan.Namespace, plan.Name)
		planlog.V(1).Info("no owned Tasks visible at admission time (Pitfall B)", "plan", plan.Name)
		warnings = append(warnings, msg)
		return warnings, nil
	}

	// PLAN-01: cycle detection via pkg/dag.ComputeWaves.
	// Build within-plan nodes and within-plan edges only (Pitfall 6 / T-23-06):
	// cross-scope deps that name tasks outside this plan's node set are dropped.
	// Cross-scope cycle detection is the global gate in the Project reconciler
	// (Plan 23-03 DEPS-03); the per-plan webhook validates within-plan edges only.
	nodes, edges := tasksToDAGWithinPlan(taskList.Items)
	if _, err := dag.ComputeWaves(nodes, edges); err != nil {
		var cyc *dag.CycleError
		if errors.As(err, &cyc) {
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
	project := resolveProjectForWebhook(ctx, v.Client, plan)
	mode := ResolveFileTouchMode(plan, project, v.DefaultFileTouchMode)
	mismatches := ComputeFileTouchMismatches(taskList.Items)

	if len(mismatches) > 0 {
		summary := SummariseMismatches(mismatches)
		if mode == "strict" {
			if v.Recorder != nil {
				v.Recorder.Eventf(plan, corev1.EventTypeWarning, "FileTouchMismatch",
					"file-touch mismatches (strict): %s", summary)
			}
			return warnings, fmt.Errorf("plan %s/%s rejected (strict mode): file-touch mismatches: %s",
				plan.Namespace, plan.Name, summary)
		}
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

// tasksToDAGWithinPlan translates a slice of v1alpha2.Task CRDs into the (nodes, edges)
// form consumed by pkg/dag.ComputeWaves.
//
// Cross-scope filtering (Pitfall 6 / T-23-06): an edge is included ONLY when the dep
// name is in the within-plan node set. This prevents ComputeWaves from returning
// "edge references unknown node" for cross-scope deps (which name tasks in other plans,
// or scope nodes like Plan/Phase names). Cross-scope cycles are caught by the global
// gate in the Project reconciler (Plan 23-03, DEPS-03).
//
// within-plan node  = task.Name
// within-plan edge  = (DependsOn[i], task.Name) iff DependsOn[i] is a within-plan node
func tasksToDAGWithinPlan(tasks []tideprojectv1alpha2.Task) ([]dag.NodeID, []dag.Edge) {
	// Build the within-plan node set first.
	nodeSet := make(map[string]struct{}, len(tasks))
	nodes := make([]dag.NodeID, 0, len(tasks))
	for i := range tasks {
		nodeSet[tasks[i].Name] = struct{}{}
		nodes = append(nodes, tasks[i].Name)
	}

	var edges []dag.Edge
	for i := range tasks {
		t := &tasks[i]
		for _, dep := range t.Spec.DependsOn {
			// Filter: include the edge only if the dep names a within-plan task.
			// Cross-scope deps (task names in other plans, or Plan/Phase/Milestone scope
			// node names) are silently dropped here — they are resolved by the global
			// assembler in Phase 24 and cycle-checked by the global gate (Plan 23-03).
			if _, inPlan := nodeSet[dep]; inPlan {
				edges = append(edges, dag.Edge{From: dep, To: t.Name})
			}
		}
	}
	return nodes, edges
}

// resolveProjectForWebhook walks the Plan → Phase → Milestone → Project owner-ref chain.
// Returns nil on any Get failure so admission never hard-fails on a missing chain.
func resolveProjectForWebhook(ctx context.Context, c client.Client, plan *tideprojectv1alpha2.Plan) *tideprojectv1alpha2.Project {
	if plan.Spec.PhaseRef == "" {
		return nil
	}
	var ph tideprojectv1alpha2.Phase
	if err := c.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: plan.Spec.PhaseRef}, &ph); err != nil {
		return nil
	}
	if ph.Spec.MilestoneRef == "" {
		return nil
	}
	var ms tideprojectv1alpha2.Milestone
	if err := c.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		return nil
	}
	if ms.Spec.ProjectRef == "" {
		return nil
	}
	var p tideprojectv1alpha2.Project
	if err := c.Get(ctx, client.ObjectKey{Namespace: plan.Namespace, Name: ms.Spec.ProjectRef}, &p); err != nil {
		return nil
	}
	return &p
}
