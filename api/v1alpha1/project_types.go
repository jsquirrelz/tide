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

// SecretRefs declares the K8s Secret names that carry credentials.
// Per AUTH-01, populated in Phase 3; Phase 1 ships the field shape only.
type SecretRefs struct {
	// AnthropicAPIKey is the Secret name carrying the LLM API key.
	// +optional
	AnthropicAPIKey string `json:"anthropicAPIKey,omitempty"`
	// GitCredentials is the Secret name carrying git push credentials (PAT or SSH key).
	// +optional
	GitCredentials string `json:"gitCredentials,omitempty"`
}

// ModelSelection picks per-level model identifiers.
// Phase 1 ships the field shape; Phase 2+ consumes.
type ModelSelection struct {
	// +optional
	Milestone string `json:"milestone,omitempty"`
	// +optional
	Phase string `json:"phase,omitempty"`
	// +optional
	Plan string `json:"plan,omitempty"`
	// +optional
	Task string `json:"task,omitempty"`
}

// GatePolicy is one of "auto" | "approve" | "pause" — per-level human gate.
// Phase 1 ships the field shape; Phase 4 consumes.
// +kubebuilder:validation:Enum=auto;approve;pause
type GatePolicy string

// Gates declares per-level gate policy. Phase 1 ships field; Phase 4 wires.
type Gates struct {
	// +optional
	Milestone GatePolicy `json:"milestone,omitempty"`
	// +optional
	Phase GatePolicy `json:"phase,omitempty"`
	// +optional
	Plan GatePolicy `json:"plan,omitempty"`
	// +optional
	Task GatePolicy `json:"task,omitempty"`
	// +optional
	PauseBetweenWaves bool `json:"pauseBetweenWaves,omitempty"`
}

// ProjectSpec defines the desired state of Project.
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http') || self.targetRepo.startsWith('git@')",message="targetRepo must be a valid http(s) or SSH git URL"
type ProjectSpec struct {
	// TargetRepo is the URL of the repo this Project operates on.
	// +kubebuilder:validation:MinLength=1
	TargetRepo string `json:"targetRepo"`

	// SecretRefs references K8s Secrets for credentials (AUTH-01 — Phase 3).
	// +optional
	SecretRefs SecretRefs `json:"secretRefs,omitempty"`

	// ModelSelection picks per-level model identifiers (Phase 2+).
	// +optional
	ModelSelection ModelSelection `json:"modelSelection,omitempty"`

	// Gates declares per-level human gate policy (Phase 4).
	// +optional
	Gates Gates `json:"gates,omitempty"`
}

// ProjectStatus defines the observed state of Project.
// PERSIST-02 / Pitfall 4: NO aggregate schedule fields here.
type ProjectStatus struct {
	// Phase is a high-level state label ("Pending", "Running", "Complete", "Failed").
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions follow the standard K8s convention.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced

// Project is the Schema for the projects API
type Project struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Project
	// +required
	Spec ProjectSpec `json:"spec"`

	// status defines the observed state of Project
	// +optional
	Status ProjectStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProjectList contains a list of Project
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
