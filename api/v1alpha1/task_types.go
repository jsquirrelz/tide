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

// TaskSpec carries the executor envelope per D-F1, D-F2.
type TaskSpec struct {
	// PlanRef is the name of the owning Plan (same namespace).
	// +kubebuilder:validation:MinLength=1
	PlanRef string `json:"planRef"`

	// DependsOn lists sibling Task names in the same Plan (D-F1).
	// +optional
	DependsOn []string `json:"dependsOn,omitempty"`

	// FilesTouched declares the workspace paths this Task intends to write (D-F2).
	// +kubebuilder:validation:MinItems=1
	FilesTouched []string `json:"filesTouched"`

	// PromptRef is the name of a ConfigMap carrying the prompt (optional).
	// +optional
	PromptRef string `json:"promptRef,omitempty"`
}

// TaskStatus defines the observed state of Task.
// Stays small per PERSIST-02 / Pitfall 4.
type TaskStatus struct {
	// +optional
	Phase string `json:"phase,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	Attempt int `json:"attempt,omitempty"`

	// +optional
	ExitCode *int `json:"exitCode,omitempty"`

	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced

// Task is the Schema for the tasks API
type Task struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Task
	// +required
	Spec TaskSpec `json:"spec"`

	// status defines the observed state of Task
	// +optional
	Status TaskStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TaskList contains a list of Task
type TaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Task `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Task{}, &TaskList{})
}
