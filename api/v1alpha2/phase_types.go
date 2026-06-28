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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PhaseSpec defines the desired state of Phase.
type PhaseSpec struct {
	// MilestoneRef is the name of the owning Milestone (same namespace).
	// +kubebuilder:validation:MinLength=1
	MilestoneRef string `json:"milestoneRef"`

	// DependsOn lists any level node (Milestone/Phase/Plan/Task) in this Project
	// that this Phase's execution depends on. Entries may target any node at any
	// hierarchy level within the Project (any-level targets, D-02). Coarse scope
	// refs are fan-out expanded by the Phase 24 assembler (D-06).
	// +optional
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:XValidation:rule="!self.exists(d, d == '')",message="dependsOn entries must not be empty strings"
	DependsOn []string `json:"dependsOn,omitempty"`

	// SharedContext is the wave-scoped shared context string stamped by the
	// orchestrator at object creation time (Phase 20 D-05). Byte-identical
	// across all objects in the same wave. Read by BuildPlannerEnvelope when
	// dispatching this object's planner Job (D-07 uniform path).
	// +optional
	SharedContext string `json:"sharedContext,omitempty"`
}

// PhaseStatus defines the observed state of Phase.
// PERSIST-02 enforced: NO aggregate fields.
type PhaseStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// PhaseRolledUpUID is the name of this Phase's planner Job whose Usage
	// was successfully rolled up into the Project budget. Prevents double-counting
	// when the reporter Job has TTL-GC'd before a reconcile re-observes it.
	// Mirrors the project-level budget rollup marker at the Phase level
	// per the D-03 level-specific marker pattern. Phase 31 ADOPT-04 / D-03.
	// +optional
	PhaseRolledUpUID string `json:"phaseRolledUpUID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
// +kubebuilder:validation:XValidation:rule="!has(self.spec.dependsOn) || !(self.metadata.name in self.spec.dependsOn)",message="a phase cannot depend on itself"

// Phase is the Schema for the phases API
type Phase struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Phase
	// +required
	Spec PhaseSpec `json:"spec"`

	// status defines the observed state of Phase
	// +optional
	Status PhaseStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PhaseList contains a list of Phase
type PhaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Phase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Phase{}, &PhaseList{})
}
