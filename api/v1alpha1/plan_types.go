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

	// SharedContext is the wave-scoped shared context string stamped by the
	// orchestrator at object creation time (Phase 20 D-05). Byte-identical
	// across all siblings in the same wave. Read by BuildPlannerEnvelope when
	// dispatching this object's planner Job (D-07 uniform path).
	// +optional
	SharedContext string `json:"sharedContext,omitempty"`
}

// WaveIntegrationStatus records the bounded auto-retry state of a single
// wave's integration Job (Phase 34 D-04). Mirrors BoundaryPushStatus's shape
// (project_types.go) so a wave-integration Job failure rides the same
// re-derived-from-.status bounded-retry pattern as the #13b boundary push —
// no in-memory counter, survives a controller restart.
type WaveIntegrationStatus struct {
	// Wave is the 0-indexed Kahn wave number this Attempts counter refers to.
	// The counter resets to 0 whenever the blocking wave changes (a new wave's
	// integration Job gets a fresh retry budget).
	// +optional
	Wave int `json:"wave,omitempty"`

	// Attempts is the number of wave-integration Jobs dispatched so far for
	// Wave. The controller stops dispatching once Attempts reaches
	// maxWaveIntegrationAttempts (Phase 34 D-04) and marks the Plan Failed
	// with ReasonWaveIntegrationFailed.
	// +optional
	Attempts int32 `json:"attempts,omitempty"`

	// LastAttemptTime is the timestamp of the most recently dispatched
	// wave-integration attempt. Drives the capped exponential requeue backoff
	// (boundaryPushRequeue, shared with the #13b machine).
	// +optional
	LastAttemptTime *metav1.Time `json:"lastAttemptTime,omitempty"`

	// LastError carries the most recent wave-integration failure reason (from
	// the push-result envelope) for operator visibility. Cleared on success.
	// +optional
	LastError string `json:"lastError,omitempty"`
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

	// IntegratedThroughWave records the highest 1-indexed wave number whose task
	// branches have been fully integrated into the run branch. The per-wave
	// integration trigger in reconcileWaveMaterialization gates on this field
	// to avoid re-firing integration every reconcile cycle (D-02/SC-3).
	// Zero means no waves have been integrated yet.
	// +optional
	IntegratedThroughWave int `json:"integratedThroughWave,omitempty"`

	// WaveIntegration records the bounded auto-retry state of the current
	// wave-integration Job (Phase 34 D-04). Reset (Wave/Attempts) whenever
	// reconcileWaveBoundary advances to a new blocking wave.
	// +optional
	WaveIntegration WaveIntegrationStatus `json:"waveIntegration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:unservedversion
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
