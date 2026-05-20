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

// PlanSpec defines the desired state of Plan.
type PlanSpec struct {
	// PhaseRef is the name of the owning Phase (same namespace).
	// +kubebuilder:validation:MinLength=1
	PhaseRef string `json:"phaseRef"`
}

// PlanStatus defines the observed state of Plan.
// PERSIST-02 / Pitfall 4: no aggregate wave list, no cached dag, no indegree
// map — see `make verify-no-aggregates` for the enforced field-name denylist.
// Phase 2 adds ValidationState + CycleEdges fields. Phase 1 stays minimal.
type PlanStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ValidationState records the result of the Plan admission webhook's DAG and
	// file-touch validation (Phase 2+). Set by Plan 11's webhook after admission.
	// +kubebuilder:validation:Enum=Pending;Validated;CycleDetected;FileTouchMismatch
	// +optional
	ValidationState string `json:"validationState,omitempty"`

	// CycleEdges holds the human-readable edge representations for any cycle detected
	// during DAG validation (populated when ValidationState=CycleDetected, Phase 2+).
	// +optional
	CycleEdges []string `json:"cycleEdges,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Plan is the Schema for the plans API
type Plan struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Plan
	// +required
	Spec PlanSpec `json:"spec"`

	// status defines the observed state of Plan
	// +optional
	Status PlanStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PlanList contains a list of Plan
type PlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Plan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Plan{}, &PlanList{})
}
