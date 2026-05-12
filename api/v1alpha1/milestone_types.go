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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MilestoneSpec defines the desired state of Milestone.
type MilestoneSpec struct {
	// ProjectRef is the name of the owning Project (same namespace).
	// +kubebuilder:validation:MinLength=1
	ProjectRef string `json:"projectRef"`

	// DependsOn lists sibling Milestone names in the same Project. Optional.
	// +optional
	DependsOn []string `json:"dependsOn,omitempty"`
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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced

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
