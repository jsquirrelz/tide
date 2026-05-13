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

// ProviderConfig configures one LLM provider (Phase 2+).
type ProviderConfig struct {
	// Name is the provider identifier (only "anthropic" is supported in v1).
	// +kubebuilder:validation:Enum=anthropic
	Name string `json:"name"`

	// RequestsPerMinute optionally caps API requests per minute for this provider.
	// +optional
	RequestsPerMinute *int32 `json:"requestsPerMinute,omitempty"`

	// TokensPerMinute optionally caps token throughput per minute for this provider.
	// +optional
	TokensPerMinute *int32 `json:"tokensPerMinute,omitempty"`
}

// BudgetConfig declares cost/token caps for the Project (Phase 2+).
type BudgetConfig struct {
	// AbsoluteCapCents is the hard spending limit in USD cents for the project lifetime.
	// +kubebuilder:validation:Minimum=0
	AbsoluteCapCents int64 `json:"absoluteCapCents"`

	// RollingWindowCapCents optionally caps spending over the window defined by the
	// BudgetStatus.WindowStart field.
	// +optional
	RollingWindowCapCents int64 `json:"rollingWindowCapCents,omitempty"`
}

// PlanAdmissionConfig controls file-touch policy during plan validation (Phase 2+).
type PlanAdmissionConfig struct {
	// FileTouchMode determines how file-touch mismatches are handled:
	//   "strict" — reject plans whose file touches deviate from declarations.
	//   "warn"   — admit but annotate the mismatch on Plan.Status.
	// +kubebuilder:validation:Enum=strict;warn
	// +optional
	FileTouchMode string `json:"fileTouchMode,omitempty"`
}

// BudgetStatus records running spend tallies for the Project (Phase 2+).
// PERSIST-02 / Pitfall 4: this is a TALLY object, not an aggregate schedule.
// The PERSIST-02 denylist (enforced by `make verify-no-aggregates`) does not
// apply to this struct — it carries only spend counters (tokensSpent,
// costSpentCents, windowStart), not a derived execution plan.
type BudgetStatus struct {
	// TokensSpent is the cumulative token count spent since WindowStart.
	// +optional
	TokensSpent int64 `json:"tokensSpent,omitempty"`

	// CostSpentCents is the cumulative cost in USD cents since WindowStart.
	// +optional
	CostSpentCents int64 `json:"costSpentCents,omitempty"`

	// WindowStart marks the beginning of the current rolling budget window.
	// +optional
	WindowStart *metav1.Time `json:"windowStart,omitempty"`
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

	// ProviderSecretRef is the name of the K8s Secret carrying provider credentials (Phase 2+).
	// +optional
	ProviderSecretRef string `json:"providerSecretRef,omitempty"`

	// Providers lists per-provider configuration (rate limits, etc.) (Phase 2+).
	// +optional
	Providers []ProviderConfig `json:"providers,omitempty"`

	// Budget declares cost/token caps for this project (Phase 2+).
	// +optional
	Budget BudgetConfig `json:"budget,omitempty"`

	// PlanAdmission controls file-touch policy during plan validation (Phase 2+).
	// +optional
	PlanAdmission PlanAdmissionConfig `json:"planAdmission,omitempty"`

	// MaxAttemptsPerTask caps the number of dispatch attempts per Task before
	// the Task is marked failed (Phase 2+, consumed by TaskReconciler in Plan 09).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +optional
	MaxAttemptsPerTask int32 `json:"maxAttemptsPerTask,omitempty"`
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

	// Budget records running spend tallies (Phase 2+).
	// PERSIST-02 / Pitfall 4: BudgetStatus is a tally object, not an aggregate schedule.
	// +optional
	Budget BudgetStatus `json:"budget,omitempty"`
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
