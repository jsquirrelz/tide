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

// Package budget unit tests for reservation.go.
// Pure-Go unit tests follow cap_test.go table-driven style.
// Rederivation tests use the fake client helper from precharge_test.go.
package budget

import (
	"context"
	"strconv"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// ---------- ReservationStore pure-Go unit tests ----------

func TestReservationStore_ReserveAndTotal(t *testing.T) {
	s := NewReservationStore()
	s.Reserve("task-a", 100)
	s.Reserve("task-b", 200)
	s.Reserve("task-c", 300)

	got := s.TotalReserved()
	if got != 600 {
		t.Errorf("TotalReserved: want 600, got %d", got)
	}
}

func TestReservationStore_Settle(t *testing.T) {
	s := NewReservationStore()
	s.Reserve("task-x", 500)
	s.Settle("task-x")

	got := s.TotalReserved()
	if got != 0 {
		t.Errorf("after Settle: want 0, got %d", got)
	}
}

func TestReservationStore_Release(t *testing.T) {
	s := NewReservationStore()
	s.Reserve("task-y", 750)
	s.Release("task-y")

	got := s.TotalReserved()
	if got != 0 {
		t.Errorf("after Release: want 0, got %d", got)
	}
}

// ---------- HasHeadroom table tests ----------

func makeProjectWithBudget(cap, spent int64) *tidev1alpha1.Project {
	return &tidev1alpha1.Project{
		Spec: tidev1alpha1.ProjectSpec{
			Budget: tidev1alpha1.BudgetConfig{AbsoluteCapCents: cap},
		},
		Status: tidev1alpha1.ProjectStatus{
			Budget: tidev1alpha1.BudgetStatus{CostSpentCents: spent},
		},
	}
}

func TestReservationStore_HasHeadroom(t *testing.T) {
	cases := []struct {
		name          string
		cap           int64
		spent         int64
		reserved      int64
		estimate      int64
		wantHeadroom  bool
		nilProject    bool
		nilStore      bool
	}{
		{
			name:         "under cap",
			cap:          1000, spent: 400, reserved: 100, estimate: 200,
			wantHeadroom: true, // 400+100+200=700 < 1000
		},
		{
			name:         "at cap (== not allowed by D-05 strict less-than)",
			cap:          1000, spent: 400, reserved: 300, estimate: 300,
			wantHeadroom: false, // 400+300+300=1000 == 1000, not < 1000
		},
		{
			name:         "over cap",
			cap:          1000, spent: 900, reserved: 50, estimate: 100,
			wantHeadroom: false, // 900+50+100=1050 > 1000
		},
		{
			name:         "zero cap = unlimited",
			cap:          0, spent: 999999, reserved: 0, estimate: 1,
			wantHeadroom: true,
		},
		{
			name:         "negative cap = unlimited",
			cap:          -1, spent: 999999, reserved: 0, estimate: 1,
			wantHeadroom: true,
		},
		{
			name:         "nil project",
			nilProject:   true,
			wantHeadroom: true,
		},
		{
			name:         "nil store",
			nilStore:     true,
			cap:          100, spent: 200, // would be over cap if store were active
			wantHeadroom: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var store *ReservationStore
			if !tc.nilStore {
				store = NewReservationStore()
				if tc.reserved > 0 {
					store.Reserve("some-task", tc.reserved)
				}
			}

			var project *tidev1alpha1.Project
			if !tc.nilProject {
				project = makeProjectWithBudget(tc.cap, tc.spent)
			}

			got := store.HasHeadroom(project, tc.estimate)
			if got != tc.wantHeadroom {
				t.Errorf("HasHeadroom(cap=%d, spent=%d, reserved=%d, estimate=%d) = %v; want %v",
					tc.cap, tc.spent, tc.reserved, tc.estimate, got, tc.wantHeadroom)
			}
		})
	}
}

// ---------- Nil-receiver safety ----------

func TestReservationStore_NilReceiver(t *testing.T) {
	var s *ReservationStore

	// None of these should panic.
	s.Reserve("uid", 100)
	s.Settle("uid")
	s.Release("uid")

	got := s.TotalReserved()
	if got != 0 {
		t.Errorf("nil TotalReserved: want 0, got %d", got)
	}

	// HasHeadroom on nil store must return true (permissive).
	p := makeProjectWithBudget(1, 999)
	if !s.HasHeadroom(p, 100) {
		t.Error("nil store HasHeadroom: want true (permissive)")
	}
}

// ---------- RederiveReservations fake-client tests ----------

// makeJobWithCostLabel creates a batchv1.Job with the reservedCostLabel and
// taskUIDLabel labels.
func makeJobWithCostLabel(name, taskUID string, estimatedCents int64, active int32) *batchv1.Job {
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				reservedCostLabel: strconv.FormatInt(estimatedCents, 10),
				taskUIDLabel:      taskUID,
			},
		},
		Status: batchv1.JobStatus{Active: active},
	}
	return j
}

func TestRederiveReservations_PopulatesFromLabel(t *testing.T) {
	job := makeJobWithCostLabel("job-a", "task-uid-1", 250, 1)
	c := newBudgetFakeClient(t, job)

	store := NewReservationStore()
	if err := RederiveReservations(context.Background(), c, store); err != nil {
		t.Fatalf("RederiveReservations: %v", err)
	}

	got := store.TotalReserved()
	if got != 250 {
		t.Errorf("TotalReserved after rederive: want 250, got %d", got)
	}
}

func TestRederiveReservations_SkipsTerminated(t *testing.T) {
	job := makeJobWithCostLabel("job-done", "task-uid-2", 500, 0) // Active=0
	c := newBudgetFakeClient(t, job)

	store := NewReservationStore()
	if err := RederiveReservations(context.Background(), c, store); err != nil {
		t.Fatalf("RederiveReservations: %v", err)
	}

	got := store.TotalReserved()
	if got != 0 {
		t.Errorf("terminated Job must not be rederived; TotalReserved: want 0, got %d", got)
	}
}

func TestRederiveReservations_SkipsMissingLabel(t *testing.T) {
	// Pre-Phase-14 Job: no estimated-cost label (Pitfall 5).
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "legacy-job",
			Namespace: "default",
			Labels: map[string]string{
				// Only the provider-secret-uid label — no estimated-cost label.
				"tideproject.k8s/provider-secret-uid": "some-uid",
			},
		},
		Status: batchv1.JobStatus{Active: 1},
	}
	c := newBudgetFakeClient(t, job)

	store := NewReservationStore()
	// RederiveReservations uses client.HasLabels{reservedCostLabel} filter,
	// so the legacy job should not appear in the list at all.
	if err := RederiveReservations(context.Background(), c, store); err != nil {
		t.Fatalf("RederiveReservations: %v", err)
	}

	got := store.TotalReserved()
	if got != 0 {
		t.Errorf("Job missing estimated-cost label must not be rederived; TotalReserved: want 0, got %d", got)
	}
}
