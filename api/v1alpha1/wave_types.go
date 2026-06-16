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

// WaveSpec carries EXACTLY two fields per D-B1, D-B2. Anything else lives in Status.
type WaveSpec struct {
	// PlanRef is the name of the owning Plan (same namespace).
	// +kubebuilder:validation:MinLength=1
	PlanRef string `json:"planRef"`

	// WaveIndex is the 0-indexed layer position from pkg/dag.ComputeWaves.
	// +kubebuilder:validation:Minimum=0
	WaveIndex int `json:"waveIndex"`
}

// WaveStatus defines the observed state of Wave.
// Everything observed about this wave lives here, NOT in Spec.
type WaveStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// TaskRefs lists the Task names dispatched in this wave (observation only).
	// +optional
	TaskRefs []string `json:"taskRefs,omitempty"`

	// +optional
	DispatchedAt *metav1.Time `json:"dispatchedAt,omitempty"`

	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:unservedversion
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Wave is the Schema for the waves API
type Wave struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Wave
	// +required
	Spec WaveSpec `json:"spec"`

	// status defines the observed state of Wave
	// +optional
	Status WaveStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// WaveList contains a list of Wave
type WaveList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Wave `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Wave{}, &WaveList{})
}
