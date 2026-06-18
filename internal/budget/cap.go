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

// Package budget — see doc.go for package overview.
package budget

import (
	"maps"
	"time"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// IsCapExceeded returns true iff the Project's cumulative cost spend exceeds a
// configured cap. Two caps are checked:
//
//  1. AbsoluteCapCents — hard lifetime limit; checked unconditionally when > 0.
//  2. RollingWindowCapCents — window-relative limit; checked when > 0.
//     ProjectReconciler.handleBudgetGate calls MaybeResetWindow before
//     IsCapExceeded, so CostSpentCents reflects current-window spend only.
//
// Returns false when:
//   - project is nil
//   - Both caps are 0 or negative (zero cap = unlimited)
//   - Status.Budget.CostSpentCents ≤ both configured caps
//
// The halt is structural — TaskReconciler (Plan 09) calls this before every
// dispatch and refuses to create a Job if it returns true. Per D-D2.
//
// Phase 04.1 P4.1: RollingWindowCapCents check added (previously doc-only WR-02).
//
// Bypass/baseline behavior: this predicate has NO knowledge of
// Status.Budget.BypassBaselineCents. The acknowledged-spend baseline logic
// (BYPASS-04 / D-04) lives in ProjectReconciler.handleBudgetGate, scoped to the
// bypass/resume path, so the TaskReconciler dispatch gate is unaffected.
func IsCapExceeded(project *tidev1alpha2.Project) bool {
	if project == nil {
		return false
	}
	// Absolute cap check (unchanged — backward compatible).
	if project.Spec.Budget.AbsoluteCapCents > 0 && project.Status.Budget.CostSpentCents > project.Spec.Budget.AbsoluteCapCents {
		return true
	}
	// Phase 04.1 P4.1: rolling cap check.
	if project.Spec.Budget.RollingWindowCapCents > 0 && project.Status.Budget.CostSpentCents > project.Spec.Budget.RollingWindowCapCents {
		return true
	}
	return false
}

// IsBypassed checks the project's annotations for a budget bypass per D-D4.
// Two forms are supported:
//
//   - "tideproject.k8s/bypass-budget=true" — one-shot bypass; active immediately,
//     consumed by ConsumeBypass after one Task dispatch.
//   - "tideproject.k8s/bypass-budget-until=<RFC3339>" — TTL bypass; active while
//     the parsed time is in the future relative to now.
//
// When both annotations are present, either form independently activates the bypass.
// The TTL form is recommended (RESEARCH.md Pitfall 7 — TTL is raceless).
//
// Returns false if project is nil or neither annotation is set / valid.
func IsBypassed(project *tidev1alpha2.Project, now time.Time) bool {
	if project == nil {
		return false
	}

	// Check one-shot form.
	if v, ok := project.Annotations["tideproject.k8s/bypass-budget"]; ok && v == "true" {
		return true
	}

	// Check TTL form.
	if untilStr, ok := project.Annotations["tideproject.k8s/bypass-budget-until"]; ok {
		if t, err := time.Parse(time.RFC3339, untilStr); err == nil && t.After(now) {
			return true
		}
	}

	return false
}

// ConsumeBypass removes the one-shot bypass annotation from the project's
// annotations map. Returns a copy of the annotations with "tideproject.k8s/bypass-budget"
// deleted. Does NOT remove "tideproject.k8s/bypass-budget-until" — the TTL form
// expires naturally and does not need manual consumption.
//
// The caller is responsible for Patching the Project with the returned annotations
// map. Returns nil if project is nil.
func ConsumeBypass(project *tidev1alpha2.Project) map[string]string {
	if project == nil {
		return nil
	}
	out := make(map[string]string, len(project.Annotations))
	maps.Copy(out, project.Annotations)
	delete(out, "tideproject.k8s/bypass-budget")
	return out
}
