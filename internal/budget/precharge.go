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
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// secretUIDLabel is the K8s label key the orchestrator stamps on every dispatch
// Job (Plan 08 wires this at Job-create time). PreCharge uses this label to
// enumerate active Jobs and pre-charge the corresponding buckets.
const secretUIDLabel = "tideproject.k8s/provider-secret-uid"

// PreCharge enumerates active Jobs on Manager startup and pre-charges their
// in-memory rate-limit buckets with one Reserve() per active Job.
//
// Rationale: after a Manager restart, the new process has empty buckets but
// active Jobs are still consuming provider rate-limit capacity. PreCharge
// re-approximates consumed capacity so the first set of new dispatches does not
// burst past the effective limit (D-D1).
//
// This is best-effort per Pitfall C (RESEARCH.md): timestamps are not persisted
// per-Job so the count may be slightly over or under. Accepted for v1.
//
// Parameters:
//   - c: controller-runtime client (cache-backed; called once at startup before
//     informers are fully synced — use uncached client in Plan 12 wiring).
//   - store: the bucket Store to pre-charge.
//   - defaults: the Limits to apply when creating buckets (same defaults used by
//     TaskReconciler when no Secret annotation overrides are set).
//   - window: only Jobs created within the last window period are considered
//     (default 60s in Plan 12 wiring, matching the RPM bucket interval).
//
// Returns nil on success; wraps client.List errors on failure.
func PreCharge(ctx context.Context, c client.Client, store *Store, defaults Limits, window time.Duration) error {
	var jobs batchv1.JobList

	// client.HasLabels filters Jobs to those that have the label key present
	// (any value). This is correct — we want all Jobs that declare a Secret UID,
	// regardless of which UID they reference. Using client.MatchingLabels{key: ""}
	// would match the empty-string value, which is incorrect (label absent ≠ label
	// present with empty value in K8s).
	if err := c.List(ctx, &jobs, client.HasLabels{secretUIDLabel}); err != nil {
		return err
	}

	now := time.Now()
	for _, job := range jobs.Items {
		// Skip terminated Jobs — they no longer consume rate-limit capacity.
		if job.Status.Active <= 0 {
			continue
		}

		// Skip Jobs outside the pre-charge window — they were dispatched far
		// enough in the past that their rate-limit pressure has dissipated.
		if now.Sub(job.CreationTimestamp.Time) > window {
			continue
		}

		secretUID := job.Labels[secretUIDLabel]
		lim := store.ForSecret(secretUID, defaults)
		if lim == nil {
			// RPM=0 means unlimited — no bucket to pre-charge.
			continue
		}

		// Reserve one token per active Job. This is best-effort (Pitfall C).
		// The Reservation is discarded because we do not intend to cancel it.
		_ = lim.Reserve()
	}

	return nil
}
