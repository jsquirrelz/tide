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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MilestoneSpec defines the desired state of Milestone.
type MilestoneSpec struct {
	// ProjectRef is the name of the owning Project (same namespace).
	// +kubebuilder:validation:MinLength=1
	ProjectRef string `json:"projectRef"`

	// DependsOn lists any level node (Milestone/Phase/Plan/Task) in this Project
	// that this Milestone's execution depends on. Entries may target any node at
	// any hierarchy level within the Project (any-level targets, D-02). Coarse
	// scope refs are fan-out expanded by the Phase 24 assembler (D-06).
	// Progressive refinement (D-03) enables coarse-to-fine dependency narrowing
	// as deeper structure materializes.
	// +optional
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:XValidation:rule="!self.exists(d, d == '')",message="dependsOn entries must not be empty strings"
	DependsOn []string `json:"dependsOn,omitempty"`

	// SharedContext is the wave-scoped shared context string stamped by the
	// orchestrator at object creation time (Phase 20 D-05). Byte-identical
	// across all objects in the same wave. Read by BuildPlannerEnvelope when
	// dispatching this object's planner Job (D-07 uniform path).
	// Vestigial at the Milestone level (no parent planner above Project) but
	// kept uniform to avoid a level-conditional branch (D-07).
	// +optional
	SharedContext string `json:"sharedContext,omitempty"`
}

// MilestoneStatus defines the observed state of Milestone.
// PERSIST-02 enforced: NO aggregate fields.
type MilestoneStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// MilestoneRolledUpUID is the name of this Milestone's planner Job whose Usage
	// was successfully rolled up into the Project budget. Prevents double-counting
	// when the reporter Job has TTL-GC'd before a reconcile re-observes it.
	// Mirrors the project-level budget rollup marker at the Milestone level
	// per the D-03 level-specific marker pattern. Phase 31 ADOPT-04 / D-03.
	// +optional
	MilestoneRolledUpUID string `json:"milestoneRolledUpUID,omitempty"`

	// MilestoneSpanEmittedUID is the UID of the planner Job whose completion has
	// already had its dispatch span synthesized. Gates one-span-per-Job-attempt
	// emission INDEPENDENT of envReadOK — deliberately not reusing
	// MilestoneRolledUpUID, whose envReadOK gating would re-emit degraded spans on
	// every reconcile (Pitfall 2). Keyed by Job UID, not name: planner Job names
	// are deterministic, so a deleted-and-recreated attempt reuses the name but
	// never the UID (D-02: each retry attempt produces its own span).
	// Phase 42 D-02/D-04.
	// +optional
	MilestoneSpanEmittedUID string `json:"milestoneSpanEmittedUID,omitempty"`

	// MilestoneTraceSpanID is this level's own synthesized dispatch-span OTel
	// SpanID hex, persisted as the durable parent carrier for child-level
	// spans and Phase 46's dashboard deep-link (Phase 43 PROP-02). Never
	// stores the TraceID — always re-derivable from Project.UID via
	// otelai.TraceIDFromUID (D-03/D-04).
	// +optional
	MilestoneTraceSpanID string `json:"milestoneTraceSpanID,omitempty"`

	// MilestoneReporterSpawnedUID is the UID of the completed planner Job whose
	// reporter Job has been spawned for this level — the durable gate closing
	// the CR-01 window where the name-only spawn gate re-opens after the
	// reporter Job's 300s TTL-GC and a sustained-reconcile parent re-Creates a
	// duplicate reporter with recomputed options (Phase 47 gap-closure; mirrors
	// MilestoneRolledUpUID's role for budget rollup). The value is the
	// completed Job's UID, falling back to the deterministic planner-Job name
	// when the caller observes a nil Job object.
	// +optional
	MilestoneReporterSpawnedUID string `json:"milestoneReporterSpawnedUID,omitempty"`

	// LoopStatus is the level-verify loop's observed-state contract at the
	// Milestone boundary (Phase 52 D-07). Milestone runs with
	// MaxIterations:0 (LOOP-03-compliant, D-02's resolver), so in practice
	// only LastEvaluation + ExitReason are populated — there is no repair
	// branch at this level, any non-APPROVED verdict escalates immediately.
	// +optional
	LoopStatus LoopStatus `json:"loopStatus,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.dependsOn) || !(self.metadata.name in self.spec.dependsOn)",message="a milestone cannot depend on itself"

// Milestone is the Schema for the milestones API
type Milestone struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Milestone
	// +required
	Spec MilestoneSpec `json:"spec"`

	// status defines the observed state of Milestone
	// +optional
	Status MilestoneStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MilestoneList contains a list of Milestone
type MilestoneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Milestone `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Milestone{}, &MilestoneList{})
}
