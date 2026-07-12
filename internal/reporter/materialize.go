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

// Package reporter contains the child-CRD materialization machinery used by
// the in-namespace reader Job (cmd/tide-reporter). It is intentionally
// import-safe from cmd binaries: the only dependencies are api/v1alpha3,
// pkg/dispatch, internal/owner, and the standard K8s client libraries —
// no internal/controller back-edge.
//
// MaterializeChildCRDs, ChildrenAlreadyMaterialized, and ChildKindAllowlist
// are lifted verbatim from internal/controller/dispatch_helpers.go (plan
// 09-04) to position the spec-parent-ref idempotency guard (cascade-9/10/11
// Pitfall 3) at the create-site — the reader Job IS the create-site under
// Option C. The runner-side mirror of ChildKindAllowlist in
// internal/subagent/anthropic/subagent.go stays as defense-in-depth.
package reporter

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/owner"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// maxSharedContextBytes is the etcd DoS guard for LLM-authored SharedContext
// blobs (T-20-03-01). etcd imposes a hard ~1.5 MiB per-object limit; curated
// wave-scoped summaries are expected to be ~300–700 tokens (~300–700 bytes),
// well within this cap. 64 KiB is a conservative ceiling that provides a large
// operational headroom above realistic use while remaining far below the etcd
// limit even when the blob is stamped onto N sibling child CRDs. Fail-closed:
// oversized input returns an error before any child CRD Create is attempted.
// See CONTEXT.md D-04 and RESEARCH.md Security Domain for rationale.
const maxSharedContextBytes = 64 * 1024

// ChildKindAllowlist is the T-308 mitigation gate: only these Kinds may
// pass through MaterializeChildCRDs. Anything else returns an error
// before any K8s API call is made. The set matches the five TIDE CRD
// Kinds; non-TIDE Kinds (Pod, ConfigMap, Job, etc.) MUST never reach
// server-side-create from a planner pod's emitted ChildCRDs envelope
// (subagent pod has zero K8s verbs per Phase 2 D-A4 — the envelope is
// the only channel from the subagent process into the cluster's typed
// CRD graph).
var ChildKindAllowlist = map[string]bool{
	"Milestone": true,
	"Phase":     true,
	"Plan":      true,
	"Task":      true,
	"Wave":      true,
}

// ChildrenAlreadyMaterialized reports whether the parent already has >=1
// child CRD of its expected child kind in the parent's namespace, matched
// by the parent-specRef field (Phase.spec.milestoneRef / Plan.spec.phaseRef /
// Task.spec.planRef / Milestone.spec.projectRef), with metav1.IsControlledBy
// as a belt-and-suspenders fallback.
//
// cascade-11: the cascade-10 spec-ref idempotency guard covered only the
// fresh-dispatch path (reconcilePlannerDispatch). The handleJobCompletion →
// MaterializeChildCRDs path was UNGUARDED, so a Milestone that dispatched its
// planner Job before its sibling Phase was visible (multi-doc kubectl apply
// race) would, on planner-Job completion, unconditionally materialize a
// spurious stub child subtree. Guarding dispatch is racy against not-yet-applied
// siblings; the guard must live at the race-free materialization point. The
// parent-specRef is set synchronously at child-apply time (race-free); ownerRef
// is set asynchronously by the child's reconciler, so IsControlledBy is only a
// fallback. This is the symmetric completion of the dispatch guards at
// milestone_controller.go:245 and phase_controller.go:206.
//
// bare-Project / genuine self-bootstrap is unaffected: at the first materialize
// the parent has 0 existing children of the expected kind — guard returns false
// — the genuine first child is materialized once; a later reconcile finds it by
// specRef — idempotent skip.
//
// parent must be one of *Project, *Milestone, *Phase, or *Plan. Any other type
// (or a List error) returns (false, err-or-nil) so the caller proceeds to
// materialize — fail-open preserves bare-Project bootstrap.
func ChildrenAlreadyMaterialized(ctx context.Context, c client.Client, parent metav1.Object) (bool, error) {
	ns := parent.GetNamespace()
	name := parent.GetName()

	switch p := parent.(type) {
	case *tideprojectv1alpha3.Project:
		var children tideprojectv1alpha3.MilestoneList
		if err := c.List(ctx, &children, client.InNamespace(ns)); err != nil {
			return false, fmt.Errorf("idempotency: list milestones: %w", err)
		}
		for i := range children.Items {
			if children.Items[i].Spec.ProjectRef == name || metav1.IsControlledBy(&children.Items[i], p) {
				return true, nil
			}
		}
	case *tideprojectv1alpha3.Milestone:
		var children tideprojectv1alpha3.PhaseList
		if err := c.List(ctx, &children, client.InNamespace(ns)); err != nil {
			return false, fmt.Errorf("idempotency: list phases: %w", err)
		}
		for i := range children.Items {
			if children.Items[i].Spec.MilestoneRef == name || metav1.IsControlledBy(&children.Items[i], p) {
				return true, nil
			}
		}
	case *tideprojectv1alpha3.Phase:
		var children tideprojectv1alpha3.PlanList
		if err := c.List(ctx, &children, client.InNamespace(ns)); err != nil {
			return false, fmt.Errorf("idempotency: list plans: %w", err)
		}
		for i := range children.Items {
			if children.Items[i].Spec.PhaseRef == name || metav1.IsControlledBy(&children.Items[i], p) {
				return true, nil
			}
		}
	case *tideprojectv1alpha3.Plan:
		var children tideprojectv1alpha3.TaskList
		if err := c.List(ctx, &children, client.InNamespace(ns)); err != nil {
			return false, fmt.Errorf("idempotency: list tasks: %w", err)
		}
		for i := range children.Items {
			if children.Items[i].Spec.PlanRef == name || metav1.IsControlledBy(&children.Items[i], p) {
				return true, nil
			}
		}
	default:
		// Unknown parent type — fail open (proceed to materialize). Should
		// not happen; the four reconcilers pass concrete typed pointers.
		return false, nil
	}
	return false, nil
}

// stampParentRef overrides the child's parent-reference spec field with the
// authoritative name of the parent the reporter is actually creating it under.
//
// defect #17: the planner LLM authors the parent-ref as free text and can
// mis-number it (observed live: a Phase authored under
// Milestone/milestone-01-time-formatting carried spec.milestoneRef=
// "milestone-02-time-formatting"). The phase controller resolves the parent
// via spec.milestoneRef → NotFound → silent infinite requeue, wedging the whole
// subtree. The reporter already knows the real parent (it sets the ownerRef to
// it via EnsureOwnerRef); the parent-ref is load-bearing (field-indexed:
// taskPlanRefIndexKey on .spec.planRef; idempotency guards match by
// milestoneRef/phaseRef/planRef/projectRef) so it must be CORRECT, not removed.
// Stamping it from the real parent name — overriding whatever the LLM authored —
// prevents the entire mismatched-parent-ref class at the create site, the same
// way ownerRef is already authoritative rather than LLM-trusted.
func stampParentRef(obj client.Object, parentName string) {
	switch o := obj.(type) {
	case *tideprojectv1alpha3.Milestone:
		o.Spec.ProjectRef = parentName
	case *tideprojectv1alpha3.Phase:
		o.Spec.MilestoneRef = parentName
	case *tideprojectv1alpha3.Plan:
		o.Spec.PhaseRef = parentName
	case *tideprojectv1alpha3.Task:
		o.Spec.PlanRef = parentName
		// Wave has no parent-ref spec field (it is derived, not declared) —
		// nothing to stamp.
	}
}

// MaterializeChildCRDs server-side-creates child CRDs from
// EnvelopeOut.ChildCRDs.
//
// Each child is allocated to its concrete *tideprojectv1alpha3 pointer
// based on Kind (only the allowlist-approved Kinds advance to creation —
// T-308 mitigation). The child's Spec is decoded from child.Spec.Raw via
// json.Unmarshal directly into the typed Spec field. ObjectMeta.Name is
// child.Name; Namespace is the parent's namespace. OwnerRef is set via
// internal/owner.EnsureOwnerRef (which enforces same-namespace per
// Pitfall 23 and sets Controller=true / BlockOwnerDeletion=true). The
// child's parent-reference spec field is stamped from the real parent name
// (defect #17) — the LLM-authored value is never trusted.
//
// AlreadyExists on Create is treated as idempotent success (mirrors
// Phase 2 task_controller.go:397-403 SUB-03 / Pitfall F watch-lag race
// handling). Any other error short-circuits the loop and returns —
// callers should patch their parent's Status.Phase=Failed.
func MaterializeChildCRDs(ctx context.Context, c client.Client, scheme *runtime.Scheme, parent metav1.Object, children []pkgdispatch.ChildCRDSpec) error {
	// Pre-flight: enforce Kind allowlist BEFORE any K8s API call.
	// Any rejected Kind aborts the whole batch (planner contract
	// violation — the envelope is poisoned; refuse to materialize any
	// of it).
	for _, child := range children {
		if !ChildKindAllowlist[child.Kind] {
			return fmt.Errorf("MaterializeChildCRDs: kind %q not in allowlist (allowed: Milestone, Phase, Plan, Task, Wave); refusing to create — T-308 mitigation", child.Kind)
		}
		// T-20-03-01: reject oversized SharedContext before any Create (etcd DoS guard).
		// maxSharedContextBytes (64 KiB) is a conservative ceiling well below etcd's
		// ~1.5 MiB per-object limit; curated wave-scoped blobs are expected ~300–700 bytes.
		if len(child.SharedContext) > maxSharedContextBytes {
			return fmt.Errorf("MaterializeChildCRDs: child %q SharedContext size %d bytes exceeds maxSharedContextBytes (%d); refusing to create — T-20-03-01 etcd DoS guard", child.Name, len(child.SharedContext), maxSharedContextBytes)
		}
	}

	for _, child := range children {
		var obj client.Object
		switch child.Kind {
		case "Milestone":
			ms := &tideprojectv1alpha3.Milestone{}
			if err := json.Unmarshal(child.Spec.Raw, &ms.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Milestone %q spec: %w", child.Name, err)
			}
			// D-05/D-07: stamp the parent-curated blob byte-identically (mirrors
			// tk.Spec.PromptPath = child.SourcePath at the Task branch below).
			ms.Spec.SharedContext = child.SharedContext
			obj = ms
		case "Phase":
			ph := &tideprojectv1alpha3.Phase{}
			if err := json.Unmarshal(child.Spec.Raw, &ph.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Phase %q spec: %w", child.Name, err)
			}
			// D-05/D-07: stamp SharedContext byte-identically onto the Phase spec.
			ph.Spec.SharedContext = child.SharedContext
			obj = ph
		case "Plan":
			pl := &tideprojectv1alpha3.Plan{}
			if err := json.Unmarshal(child.Spec.Raw, &pl.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Plan %q spec: %w", child.Name, err)
			}
			// D-05/D-07: stamp SharedContext byte-identically onto the Plan spec.
			pl.Spec.SharedContext = child.SharedContext
			obj = pl
		case "Task":
			tk := &tideprojectv1alpha3.Task{}
			if err := json.Unmarshal(child.Spec.Raw, &tk.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Task %q spec: %w", child.Name, err)
			}
			// Wire the executor instruction artifact (defect #10b): the prompt is
			// NOT inline on the Task spec — it lives at .spec.prompt inside the
			// originating children/<name>.json on the PVC. The subagent stamped
			// that file's workspace-relative path onto child.SourcePath; copy it to
			// PromptPath so the controller reads the prompt fresh at each dispatch.
			// PromptPath is required (MinLength=1) at the API boundary, so a missing
			// SourcePath fails Create with a clear validation error rather than
			// silently dispatching an empty-prompt executor (the #4 class).
			tk.Spec.PromptPath = child.SourcePath
			// D-05/D-07: stamp SharedContext byte-identically onto the Task spec
			// (mirrors the PromptPath = child.SourcePath stamp above; CACHE-02
			// lock is enforced at dispatch time by buildEnvelopeIn which omits it).
			tk.Spec.SharedContext = child.SharedContext
			obj = tk
		case "Wave":
			wv := &tideprojectv1alpha3.Wave{}
			if err := json.Unmarshal(child.Spec.Raw, &wv.Spec); err != nil {
				return fmt.Errorf("MaterializeChildCRDs: unmarshal Wave %q spec: %w", child.Name, err)
			}
			obj = wv
		default:
			// Unreachable — allowlist was checked above. Defensive.
			return fmt.Errorf("MaterializeChildCRDs: kind %q unreachable after allowlist", child.Kind)
		}

		obj.SetName(child.Name)
		obj.SetNamespace(parent.GetNamespace())

		// defect #17: stamp the parent-ref from the REAL parent, overriding the
		// LLM-authored value, before EnsureOwnerRef. ownerRef and parent-ref now
		// agree on the same authoritative parent.
		stampParentRef(obj, parent.GetName())

		// D-01 (CUTS-01): stamp the canonical project label from the parent at
		// create-site. Fail-open when the parent has no project label (StampProjectLabel
		// is a no-op on empty string — RESEARCH Pitfall 1).
		// 15-WR-03: *Project special-case — a Project does not carry
		// tideproject.k8s/project pointing at itself, so resolve the project
		// name from parent.GetName() when the parent IS a Project.
		projectName := parent.GetLabels()[owner.LabelProject]
		if _, isProject := parent.(*tideprojectv1alpha3.Project); isProject {
			projectName = parent.GetName()
		}
		owner.StampProjectLabel(obj, projectName)

		if err := owner.EnsureOwnerRef(obj, parent, scheme); err != nil {
			return fmt.Errorf("MaterializeChildCRDs: ensure owner ref on %s/%s: %w", child.Kind, child.Name, err)
		}

		if err := c.Create(ctx, obj); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("MaterializeChildCRDs: create %s/%s: %w", child.Kind, child.Name, err)
			}
			// AlreadyExists: idempotent success (SUB-03 / Pitfall F).
		}
	}
	return nil
}
