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
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Annotation key constants — the operator's write-back surface (D-G3 / D-G4).
//
// Threat-model notes:
//   - T-04-G2 (replay): ConsumeApprove returns a NEW annotation map with the
//     approve-<level> key removed; caller patches once. Re-triggering the gate
//     requires a fresh annotation write.
//   - T-04-G3 (wave-skip): CheckWaveApprove keys the annotation off the
//     integer wave index — approve-wave-3 does NOT approve wave 4.
//   - T-04-G4 (rejection): tideproject.k8s/reject carries a human-readable
//     reason as its VALUE; an empty value is NOT treated as rejection so a
//     bare `kubectl annotate plan foo tideproject.k8s/reject-`-style clear
//     does not accidentally halt the run.
const (
	// AnnotationApprovePrefix — caller appends the level suffix
	// ("milestone" | "phase" | "plan" | "task"). Value MUST be the literal
	// string "true" for approval to count (strict).
	AnnotationApprovePrefix = "tideproject.k8s/approve-"

	// AnnotationApproveWavePrefix — caller appends the integer wave index
	// per D-G3 (e.g., "tideproject.k8s/approve-wave-3"). Value MUST be the
	// literal string "true".
	AnnotationApproveWavePrefix = "tideproject.k8s/approve-wave-"

	// AnnotationReject — `tide reject` writes this annotation with a
	// human-readable reason as the VALUE (D-G4). Reconciler halts dispatch
	// and leaves resources in place for inspection. `tide resume` clears it.
	AnnotationReject = "tideproject.k8s/reject"

	// AnnotationResetBoundaryPush — Phase 34 D-13. `tide resume` writes this
	// annotation (value "true") when the Project shows boundary-push retry
	// state (a sticky IntegrationIncomplete condition, or a non-zero
	// Attempts tally). The ProjectReconciler consumes it once: resets
	// Status.BoundaryPush.Attempts/LastError, clears any sticky
	// ConditionIntegrationIncomplete, then removes the annotation.
	// `kubectl annotate project <name> tideproject.k8s/reset-boundary-push=true`
	// is the sanctioned escape hatch when the CLI is unavailable.
	AnnotationResetBoundaryPush = "tideproject.k8s/reset-boundary-push"
)

// CheckApprove returns true iff obj.GetAnnotations()[approve-<level>] equals
// the literal string "true" (strict). Any other value (including "TRUE",
// "false", or empty) returns false.
//
// Threat T-04-G3 isolation: this function only matches the EXACT level key —
// approve-phase does not approve milestone, etc.
func CheckApprove(obj client.Object, level string) bool {
	if obj == nil {
		return false
	}
	v, ok := obj.GetAnnotations()[AnnotationApprovePrefix+level]
	return ok && v == "true"
}

// CheckWaveApprove returns true iff obj.GetAnnotations()[approve-wave-<N>]
// equals the literal string "true" (strict). N is the integer wave index per
// D-G3 — approve-wave-3 does NOT approve wave 4.
func CheckWaveApprove(obj client.Object, waveN int) bool {
	if obj == nil {
		return false
	}
	v, ok := obj.GetAnnotations()[AnnotationApproveWavePrefix+strconv.Itoa(waveN)]
	return ok && v == "true"
}

// CheckRejected returns true iff obj carries the tideproject.k8s/reject
// annotation with a NON-EMPTY value (D-G4). An empty value is treated as
// no-rejection so a clear-via-empty kubectl annotate does not accidentally
// halt the run.
func CheckRejected(obj client.Object) bool {
	if obj == nil {
		return false
	}
	v, ok := obj.GetAnnotations()[AnnotationReject]
	return ok && v != ""
}

// RejectedReason returns the reject annotation value (the operator-supplied
// reason). Returns the empty string when the annotation is missing or empty.
func RejectedReason(obj client.Object) string {
	if obj == nil {
		return ""
	}
	return obj.GetAnnotations()[AnnotationReject]
}

// ConsumeApprove returns a NEW map with the approve-<level> key removed and
// all other annotations preserved. Does NOT mutate the object's Annotations
// map — the caller is responsible for the Patch (mirrors
// budget.ConsumeBypass exactly per T-04-G2 mitigation).
//
// Returns a non-nil empty map when the object has no annotations (avoids the
// caller having to nil-check before patching).
func ConsumeApprove(obj client.Object, level string) map[string]string {
	return consumeKey(obj, AnnotationApprovePrefix+level)
}

// ConsumeWaveApprove returns a NEW map with the approve-wave-<N> key
// removed; same purity contract as ConsumeApprove.
func ConsumeWaveApprove(obj client.Object, waveN int) map[string]string {
	return consumeKey(obj, AnnotationApproveWavePrefix+strconv.Itoa(waveN))
}

// ConsumeReject returns a NEW map with the reject annotation removed; same
// purity contract as ConsumeApprove. Used by the `tide resume` flow (D-G4).
func ConsumeReject(obj client.Object) map[string]string {
	return consumeKey(obj, AnnotationReject)
}

// consumeKey is the shared implementation used by all three Consume*
// helpers. Returns a non-nil map with `key` removed.
func consumeKey(obj client.Object, key string) map[string]string {
	if obj == nil {
		return map[string]string{}
	}
	src := obj.GetAnnotations()
	out := make(map[string]string, len(src))
	for k, v := range src {
		if k != key {
			out[k] = v
		}
	}
	return out
}
