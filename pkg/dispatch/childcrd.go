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

package dispatch

import "k8s.io/apimachinery/pkg/runtime"

// ChildCRDSpec is the typed child-CRD declaration a planner subagent emits in
// EnvelopeOut.ChildCRDs for the orchestrator to materialize server-side
// (D-A1). It is the authoritative source for the K8s state shaped by a
// planner dispatch: MilestoneReconciler emits Phase ChildCRDSpec entries,
// PhaseReconciler emits Plan entries, PlanReconciler emits Task + Wave
// entries. The companion Markdown artifact (MILESTONE.md / phase brief /
// PLAN.md) committed to the per-run branch is the human-review surface; this
// struct is the contract.
//
// runtime.RawExtension lets each child CRD carry its own Spec schema without
// pkg/dispatch importing api/v1alpha1 (which would invert the dependency
// arrow — controllers depend on pkg/dispatch, not the other way). The
// orchestrator consuming this slice (internal/controller/dispatch_helpers.go,
// plan 03-08) decodes Spec.Raw into the appropriate typed Spec at server-side
// create time.
//
// Consumers MUST validate Kind against an allowlist before server-side
// create — the planner pod has zero K8s verbs (Phase 2 D-A4), so the
// RawExtension is the only channel from a subagent process into the cluster's
// typed CRD graph. Threat T-308 (Tampering / Elevation) is mitigated at the
// consumer site, not in this type. See the threat register in
// 03-01-PLAN.md for the allowlist + ApplySSA validation commitment in plan
// 03-08.
type ChildCRDSpec struct {
	// Kind is the child CRD's Kind string (e.g. "Milestone", "Phase", "Plan",
	// "Task", "Wave"). Required (no omitempty). Consumer-side allowlist is the
	// security gate, not the JSON tag.
	Kind string `json:"kind"`

	// Name is the metadata.name the orchestrator assigns to the materialized
	// child CRD. Required (no omitempty). Planner is responsible for
	// uniqueness within the parent's namespace.
	Name string `json:"name"`

	// Spec is the raw JSON-encoded child CRD Spec. Decoded into the typed
	// schema by the orchestrator at server-side create time. Required.
	Spec runtime.RawExtension `json:"spec"`
}
