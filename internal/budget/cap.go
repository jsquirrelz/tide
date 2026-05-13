// Package budget — see doc.go for package overview.
package budget

import (
	"time"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// IsCapExceeded returns true iff the Project's cumulative cost spend exceeds its
// configured absolute cap. Returns false when:
//   - project is nil
//   - Spec.Budget.AbsoluteCapCents is 0 or negative (zero cap = unlimited)
//   - Status.Budget.CostSpentCents ≤ AbsoluteCapCents
//
// The halt is structural — TaskReconciler (Plan 09) calls this before every
// dispatch and refuses to create a Job if it returns true. Per D-D2.
func IsCapExceeded(project *tidev1alpha1.Project) bool {
	if project == nil || project.Spec.Budget.AbsoluteCapCents <= 0 {
		return false
	}
	return project.Status.Budget.CostSpentCents > project.Spec.Budget.AbsoluteCapCents
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
func IsBypassed(project *tidev1alpha1.Project, now time.Time) bool {
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
func ConsumeBypass(project *tidev1alpha1.Project) map[string]string {
	if project == nil {
		return nil
	}
	out := make(map[string]string, len(project.Annotations))
	for k, v := range project.Annotations {
		out[k] = v
	}
	delete(out, "tideproject.k8s/bypass-budget")
	return out
}
