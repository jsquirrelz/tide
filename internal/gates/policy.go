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

// Package gates — see doc.go for package overview.
package gates

import (
	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// Exported policy constants — these are the only three values the CEL enum
// validation on api/v1alpha1.GatePolicy permits. Threat T-04-G1 mitigation:
// the CEL constraint (`+kubebuilder:validation:Enum=auto;approve;pause`) at
// admission time forbids anything else, so EvaluatePolicy never sees a
// fourth value off the wire.
const (
	// PolicyAuto — advance through the level boundary immediately. Today's
	// pre-Phase-4 behavior for every level. Default for phase/plan/task.
	PolicyAuto tideprojectv1alpha1.GatePolicy = "auto"

	// PolicyApprove — pause at the boundary; resume when the matching
	// tideproject.k8s/approve-<level> annotation arrives (D-G3). Default for
	// milestone (D-G1 — least-friction sane default).
	PolicyApprove tideprojectv1alpha1.GatePolicy = "approve"

	// PolicyPause — halt at the boundary; resume requires explicit `tide
	// resume` (D-G2). Stronger than approve: no annotation polling.
	PolicyPause tideprojectv1alpha1.GatePolicy = "pause"
)

// DefaultGates returns the locked default gate-policy configuration per
// CONTEXT.md D-G1:
//
//	milestone:          approve     (review every milestone by default)
//	phase/plan/task:    auto        (advance unattended)
//	pauseBetweenWaves:  false       (no slack-tide pause by default)
//
// Callers should treat the return value as immutable; copy via struct value
// (Gates has no pointer fields) before mutating.
func DefaultGates() tideprojectv1alpha1.Gates {
	return tideprojectv1alpha1.Gates{
		Milestone:         PolicyApprove,
		Phase:             PolicyAuto,
		Plan:              PolicyAuto,
		Task:              PolicyAuto,
		PauseBetweenWaves: false,
	}
}

// EvaluatePolicy returns the effective GatePolicy for the given level. The
// per-level default (per D-G1) applies whenever the corresponding Gates field
// is the empty string — i.e., the operator omitted it from the spec.
//
// Level vocabulary: "milestone" | "phase" | "plan" | "task". Any other value
// (the production reconcilers never pass one) safely returns PolicyAuto — the
// function is deliberately non-panicking so a typo in a downstream reconciler
// surface degrades to today's behavior instead of crashing the manager.
func EvaluatePolicy(g tideprojectv1alpha1.Gates, level string) tideprojectv1alpha1.GatePolicy {
	switch level {
	case "milestone":
		if g.Milestone == "" {
			return PolicyApprove
		}
		return g.Milestone
	case "phase":
		if g.Phase == "" {
			return PolicyAuto
		}
		return g.Phase
	case "plan":
		if g.Plan == "" {
			return PolicyAuto
		}
		return g.Plan
	case "task":
		if g.Task == "" {
			return PolicyAuto
		}
		return g.Task
	default:
		return PolicyAuto
	}
}
