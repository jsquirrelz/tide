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

// LoopPolicy is the shared, CRD-embeddable configuration contract for a
// verification-driven quality loop (Phase 49 LOOP-01/LOOP-02). Per the
// five-element loop test, a construct is a loop — not a pipeline stage —
// only when it has: (1) a goal/spec (the domain CRD's own Spec), (2) a
// mutable candidate (the domain CRD's dispatched work product), (3)
// evaluator/environment feedback (LoopStatus.LastEvaluation), (4) a repeat
// policy (MaxIterations/MaxDuration/BudgetCents below), and (5) a bounded
// exit/escalation (Autonomy + EscalationPolicy below, surfaced back on
// LoopStatus.ExitReason). A type embedding only some of these fields is
// documenting a pipeline stage, not a loop.
//
// Fields are minimal for the two v1.0.9 consumers (the Task loop and
// plan-check re-plan) — grow per loop as new consumers arrive, never ship a
// speculative superset ahead of a real consumer (D-06).
type LoopPolicy struct {
	// MaxIterations bounds the repeat policy: the maximum number of fresh
	// attempts the loop may dispatch before exiting. 0 means the loop never
	// repeats (e.g. Phase/Milestone/Project escalate immediately instead of
	// iterating).
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxIterations int32 `json:"maxIterations,omitempty"`

	// MaxDuration bounds the repeat policy by wall-clock time across all
	// iterations, independent of MaxIterations.
	// +optional
	MaxDuration *metav1.Duration `json:"maxDuration,omitempty"`

	// BudgetCents bounds the repeat policy by cumulative spend (USD cents)
	// across all iterations of this loop.
	// +kubebuilder:validation:Minimum=0
	// +optional
	BudgetCents int64 `json:"budgetCents,omitempty"`

	// Autonomy declares how much of the bounded exit/escalation path runs
	// without a human in the loop.
	// +optional
	Autonomy AutonomyLevel `json:"autonomy,omitempty"`

	// EvaluatorRef names the evaluator config this LoopPolicy resolves
	// against (same-namespace name ref, mirroring PlanRef/PhaseRef/
	// MilestoneRef — plain string, not corev1.LocalObjectReference).
	// +optional
	EvaluatorRef string `json:"evaluatorRef,omitempty"`

	// EscalationPolicy declares the bounded exit path taken when the repeat
	// policy is exhausted (MaxIterations/MaxDuration/BudgetCents reached)
	// without an APPROVED evaluation.
	// +optional
	EscalationPolicy EscalationPolicy `json:"escalationPolicy,omitempty"`
}

// LoopStatus is the shared, CRD-embeddable observed-state contract for a
// verification-driven quality loop (Phase 49 LOOP-01/LOOP-02), the status
// counterpart to LoopPolicy. Per the five-element loop test, LoopStatus
// carries the loop's evaluator/environment feedback (LastEvaluation) and its
// bounded exit/escalation outcome (ExitReason) — the goal/spec and mutable
// candidate live on the embedding domain CRD's own Spec/Status, not here.
//
// LoopStatus carries ONLY the current-iteration summary + exit reason — it
// is structurally guaranteed to hold no accumulating iteration history
// (LOOP-03): etcd stays a state store, not an event DB. Iteration history
// lives in traces/artifacts, never in `.status`. See
// TestLoopStatus_NoForbiddenFields for the compile-time guard pinning this
// contract.
//
// Fields are minimal for the two v1.0.9 consumers (the Task loop and
// plan-check re-plan) — grow per loop as new consumers arrive, never ship a
// speculative superset ahead of a real consumer (D-06).
type LoopStatus struct {
	// Iteration is the current attempt number (1-indexed once dispatched;
	// 0 before the loop's first iteration).
	// +kubebuilder:validation:Minimum=0
	// +optional
	Iteration int32 `json:"iteration,omitempty"`

	// ParentRunID identifies the run that produced the mutable candidate
	// this LoopStatus is evaluating, correlating LoopStatus back to the
	// trace/artifact history where iteration-by-iteration detail lives
	// (LOOP-03 — that detail is never duplicated into `.status`).
	// +optional
	ParentRunID string `json:"parentRunID,omitempty"`

	// LastEvaluation is the bounded verdict summary from the most recent
	// evaluator/environment feedback — current-iteration only, never a
	// slice/history of past evaluations (LOOP-03).
	// +optional
	LastEvaluation *EvaluationSummary `json:"lastEvaluation,omitempty"`

	// ExitReason records the bounded exit/escalation outcome once the loop
	// has stopped iterating (empty while the loop is still active).
	// +optional
	ExitReason ExitReason `json:"exitReason,omitempty"`

	// CostCents is the cumulative spend (USD cents) across all iterations
	// of this loop so far, checked against LoopPolicy.BudgetCents.
	// +kubebuilder:validation:Minimum=0
	// +optional
	CostCents int64 `json:"costCents,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EvaluationSummary is the bounded, current-iteration-only projection of an
// evaluator verdict embedded on LoopStatus.LastEvaluation.
//
// Design note: api/v1alpha3.EvaluationSummary and pkg/dispatch.GateDecision
// are intentionally two separate types that serve different layers — this
// struct is the small decision+counts summary persisted to CRD `.status`,
// while pkg/dispatch.GateDecision is the full wire-format verdict (including
// the findings[] array) that round-trips through the envelope seam. The
// reconciler translates the latter into the former at evaluation time,
// keeping the CRD schema decoupled from the dispatch wire format and
// preventing the full findings evidence from ever leaking into `.status`
// (D-01; mirrors the Caps / pkg/dispatch.Caps decoupling in task_types.go).
type EvaluationSummary struct {
	// Decision is the locally-scoped verdict string ("APPROVED",
	// "REPAIRABLE", or "BLOCKED") — never the imported pkg/dispatch.GateDecision
	// type, per the two-homes design note above.
	// +kubebuilder:validation:Enum=APPROVED;REPAIRABLE;BLOCKED
	// +optional
	Decision string `json:"decision,omitempty"`

	// FindingsCount is the total number of findings the evaluator reported,
	// without the findings[] array itself.
	// +kubebuilder:validation:Minimum=0
	// +optional
	FindingsCount int32 `json:"findingsCount,omitempty"`

	// HighSeverityCount is the number of findings at the highest severity
	// tier the evaluator reported.
	// +kubebuilder:validation:Minimum=0
	// +optional
	HighSeverityCount int32 `json:"highSeverityCount,omitempty"`

	// CompletedAt is when the evaluator produced this verdict.
	// +optional
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}

// AutonomyLevel declares how much of a loop's bounded exit/escalation path
// runs without a human in the loop.
// +kubebuilder:validation:Enum=autonomous;supervised
type AutonomyLevel string

const (
	// AutonomyAutonomous: the loop iterates and exits (including escalation)
	// without pausing for human input.
	AutonomyAutonomous AutonomyLevel = "autonomous"

	// AutonomySupervised: the loop pauses for human confirmation at its
	// bounded exit/escalation point rather than resolving unattended.
	AutonomySupervised AutonomyLevel = "supervised"
)

// EscalationPolicy declares the bounded exit path a loop takes once its
// repeat policy (MaxIterations/MaxDuration/BudgetCents) is exhausted without
// an APPROVED evaluation.
// +kubebuilder:validation:Enum=escalate;requireApproval
type EscalationPolicy string

const (
	// EscalationEscalate: exhausting the repeat policy raises a halt
	// condition for the operator to triage (no automatic retry).
	EscalationEscalate EscalationPolicy = "escalate"

	// EscalationRequireApproval: exhausting the repeat policy blocks on an
	// explicit human approval gate before the loop's outcome is accepted.
	EscalationRequireApproval EscalationPolicy = "requireApproval"
)

// ExitReason records why a loop stopped iterating. This vocabulary is
// intentionally small and may grow per consumer (Claude's Discretion per
// RESEARCH Assumption A2) — it is NOT the Phase 50 Execution-loop terminal-
// reason set, which classifies in-Job run outcomes, not loop-level exit.
// +kubebuilder:validation:Enum=approved;iterationsExhausted;durationExhausted;budgetExhausted;escalated
type ExitReason string

const (
	// ExitApproved: the loop stopped because the evaluator returned APPROVED.
	ExitApproved ExitReason = "approved"

	// ExitIterationsExhausted: the loop stopped because LoopPolicy.MaxIterations
	// was reached without an APPROVED evaluation.
	ExitIterationsExhausted ExitReason = "iterationsExhausted"

	// ExitDurationExhausted: the loop stopped because LoopPolicy.MaxDuration
	// elapsed without an APPROVED evaluation.
	ExitDurationExhausted ExitReason = "durationExhausted"

	// ExitBudgetExhausted: the loop stopped because LoopPolicy.BudgetCents
	// was reached without an APPROVED evaluation.
	ExitBudgetExhausted ExitReason = "budgetExhausted"

	// ExitEscalated: the loop stopped by escalating per LoopPolicy.EscalationPolicy
	// (a halt condition or an approval gate), without resolving to APPROVED.
	ExitEscalated ExitReason = "escalated"
)
