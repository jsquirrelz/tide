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
	"strconv"
	"sync"

	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// reservedCostLabel is the K8s label key stamped on every dispatch Job at
// Job-create time (Phase 14). RederiveReservations uses this label to restore
// the in-process store after a manager restart.
const reservedCostLabel = "tideproject.k8s/estimated-cost"

// taskUIDLabel is the K8s label key identifying the Task whose cost is reserved.
// Stamped alongside reservedCostLabel at Job-create time.
const taskUIDLabel = "tideproject.k8s/task-uid"

// ReservationStore is a sync.Map-backed in-process pre-charge store.
// Keys are task UIDs (string); values are estimated cost in cents (int64).
//
// PERSIST-02 contract: reservations are NEVER persisted in CRD status. They
// exist only in the manager process memory and are rederivable from in-flight
// Job labels (tideproject.k8s/estimated-cost + tideproject.k8s/task-uid) on
// manager restart — the same doctrine as the indegree map and the rate-limiter
// bucket Store.
//
// Race-check note (T-14-06): there is an accepted bounded overshoot between a
// TotalReserved read and a subsequent Reserve call in concurrent reconcile
// loops. This is by design — the overshoot is bounded to one session's estimate
// per concurrent reconcile and is considered acceptable for v1 (RESEARCH §Security
// Domain). The store itself is goroutine-safe via sync.Map.
type ReservationStore struct {
	// m maps taskUID (string) → int64 estimated cents.
	// sync.Map is safe for concurrent use without external locking.
	m sync.Map
}

// NewReservationStore constructs an empty ReservationStore.
func NewReservationStore() *ReservationStore {
	return &ReservationStore{}
}

// Reserve records an estimated cost reservation for taskUID. Overwrites any
// existing reservation for the same UID. Nil-receiver safe (no-op).
func (s *ReservationStore) Reserve(taskUID string, estimatedCents int64) {
	if s == nil {
		return
	}
	s.m.Store(taskUID, estimatedCents)
}

// Settle removes the reservation for taskUID on task completion. The actual
// cost has already been rolled up by RollUpUsage — the reservation is no
// longer needed. Nil-receiver safe (no-op).
func (s *ReservationStore) Settle(taskUID string) {
	if s == nil {
		return
	}
	s.m.Delete(taskUID)
}

// Release removes the reservation for taskUID on terminal task failure. The
// actual cost is effectively 0 from a failed session — release the reserved
// headroom so other tasks can dispatch. Nil-receiver safe (no-op).
func (s *ReservationStore) Release(taskUID string) {
	if s == nil {
		return
	}
	s.m.Delete(taskUID)
}

// TotalReserved sums all in-flight estimated costs across the store.
// Returns 0 for a nil receiver.
func (s *ReservationStore) TotalReserved() int64 {
	if s == nil {
		return 0
	}
	var total int64
	s.m.Range(func(_, v any) bool {
		total += v.(int64) //nolint:forcetypeassert // only int64 written to the map
		return true
	})
	return total
}

// HasHeadroom returns true when dispatching a task with estimatedCents of cost
// would not push (spent + reserved + estimate) past the configured cap.
//
// Blocking condition (D-05): dispatch blocks when spent + reserved + estimate >= cap.
// Equivalently, headroom exists only when committed+estimatedCents < cap (strict less-than).
//
// Returns true (permissive) when:
//   - project is nil
//   - Spec.Budget.AbsoluteCapCents <= 0 (zero or negative cap = unlimited)
//   - s is nil (store not configured — pre-Phase-14 code paths)
func (s *ReservationStore) HasHeadroom(project *tidev1alpha1.Project, estimatedCents int64) bool {
	if s == nil {
		return true
	}
	if project == nil {
		return true
	}
	cap := project.Spec.Budget.AbsoluteCapCents
	if cap <= 0 {
		return true
	}
	committed := project.Status.Budget.CostSpentCents + s.TotalReserved()
	return committed+estimatedCents < cap
}

// RederiveReservations scans active Jobs carrying the reservedCostLabel label
// and pre-populates the store. Called once at manager startup before the
// controller starts reconciling (same pattern as budget.PreCharge for rate-limiter
// buckets).
//
// Jobs without the reservedCostLabel (pre-Phase-14, Pitfall 5) are silently
// skipped — conservative restart behavior that may allow a slight overshoot on
// first dispatch post-restart, which is no worse than pre-Phase-14 behavior.
// Jobs with missing, zero, or malformed estimated-cost label values are also
// skipped.
func RederiveReservations(ctx context.Context, c client.Client, store *ReservationStore) error {
	var jobs batchv1.JobList
	// client.HasLabels filters to Jobs that have the label key present (any value).
	if err := c.List(ctx, &jobs, client.HasLabels{reservedCostLabel}); err != nil {
		return err
	}
	for _, job := range jobs.Items {
		// Skip terminated Jobs — they no longer hold reserved cost headroom.
		if job.Status.Active <= 0 {
			continue
		}
		rawCents := job.Labels[reservedCostLabel]
		cents, err := strconv.ParseInt(rawCents, 10, 64)
		if err != nil || cents <= 0 {
			// Malformed or zero label value — skip (conservative: no headroom assumed).
			continue
		}
		taskUID := job.Labels[taskUIDLabel]
		if taskUID == "" {
			continue
		}
		store.Reserve(taskUID, cents)
	}
	return nil
}
