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

// WaveSpec carries the global-scope wave identity per D-07 (SCHEMA-01).
// Wave ownership moves from Plan to Project in v1alpha2: one Wave CR per
// global wave position across the entire Project's execution DAG.
type WaveSpec struct {
	// ProjectRef is the name of the owning Project (same namespace).
	// +kubebuilder:validation:MinLength=1
	ProjectRef string `json:"projectRef"`

	// WaveIndex is the global monotonic 0-indexed wave position derived by
	// pkg/dag.ComputeWaves over the entire Project's task DAG (all Tasks in
	// all Milestones/Phases/Plans). Never cached in status (PERSIST-03 /
	// verify-no-aggregates). The Phase 24 assembler writes this field when
	// creating Wave CRs; Phase 23 defines the spec shape only.
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
// +kubebuilder:storageversion
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
