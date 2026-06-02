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

// Package budget unit tests for the PreCharge helper.
// Uses controller-runtime's fake client to simulate active batchv1.JobList
// without a real cluster.
package budget

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newBudgetFakeClient builds a fake controller-runtime client pre-loaded with
// the provided objects.
func newBudgetFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

// makeJobWithUID creates a batchv1.Job with Status.Active and a secret-UID label.
// createdAgo controls the fake creation timestamp relative to now.
func makeJobWithUID(name, secretUID string, active int32, createdAgo time.Duration) *batchv1.Job {
	created := metav1.NewTime(time.Now().Add(-createdAgo))
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			CreationTimestamp: created,
		},
		Status: batchv1.JobStatus{Active: active},
	}
	if secretUID != "" {
		j.Labels = map[string]string{
			"tideproject.k8s/provider-secret-uid": secretUID,
		}
	}
	return j
}

// TestPreCharge_DecrementsBucketsForActiveJobsInWindow verifies that 3 active
// Jobs labeled with the same Secret UID each result in one Reserve() call on the
// bucket, exhausting the burst capacity.
func TestPreCharge_DecrementsBucketsForActiveJobsInWindow(t *testing.T) {
	defaults := Limits{RequestsPerMinute: 60, BurstSize: 3}
	store := NewStore()

	c := newBudgetFakeClient(t,
		makeJobWithUID("j1", "secret-uid-A", 1, 10*time.Second),
		makeJobWithUID("j2", "secret-uid-A", 1, 20*time.Second),
		makeJobWithUID("j3", "secret-uid-A", 1, 30*time.Second),
	)

	if err := PreCharge(context.Background(), c, store, defaults, 60*time.Second); err != nil {
		t.Fatalf("PreCharge: %v", err)
	}

	// The bucket for secret-uid-A should have 3 Reserves applied.
	// With BurstSize=3 and 3 Reserves, the next Allow() must return false.
	lim := store.ForSecret("secret-uid-A", defaults)
	if lim == nil {
		t.Fatal("expected bucket for secret-uid-A to exist after PreCharge")
	}
	if lim.Allow() {
		t.Errorf("bucket should be exhausted after 3 pre-charge Reserves; Allow() returned true")
	}
}

// TestPreCharge_SkipsJobsOutsideWindow verifies that Jobs older than the window
// are not pre-charged.
func TestPreCharge_SkipsJobsOutsideWindow(t *testing.T) {
	defaults := Limits{RequestsPerMinute: 60, BurstSize: 3}
	store := NewStore()

	c := newBudgetFakeClient(t,
		// Created 90s ago — outside 60s window.
		makeJobWithUID("j-old", "secret-uid-B", 1, 90*time.Second),
	)

	if err := PreCharge(context.Background(), c, store, defaults, 60*time.Second); err != nil {
		t.Fatalf("PreCharge: %v", err)
	}

	// No bucket should have been pre-charged; the first Allow() should succeed.
	lim := store.ForSecret("secret-uid-B", defaults)
	if lim == nil {
		t.Fatal("expected bucket for secret-uid-B to be lazily created on ForSecret")
	}
	// BurstSize=3, no Reserves consumed → first Allow() succeeds.
	if !lim.Allow() {
		t.Errorf("bucket for secret-uid-B should be full (no pre-charge); Allow() returned false")
	}
}

// TestPreCharge_SkipsTerminatedJobs verifies that Jobs with Status.Active==0 are
// not pre-charged (they no longer consume rate-limit capacity).
func TestPreCharge_SkipsTerminatedJobs(t *testing.T) {
	defaults := Limits{RequestsPerMinute: 60, BurstSize: 3}
	store := NewStore()

	c := newBudgetFakeClient(t,
		makeJobWithUID("j-done", "secret-uid-C", 0, 10*time.Second), // Active=0
	)

	if err := PreCharge(context.Background(), c, store, defaults, 60*time.Second); err != nil {
		t.Fatalf("PreCharge: %v", err)
	}

	// Bucket should not have been pre-charged.
	lim := store.ForSecret("secret-uid-C", defaults)
	if lim == nil {
		t.Fatal("ForSecret returned nil")
	}
	if !lim.Allow() {
		t.Errorf("terminated Job should not have pre-charged the bucket; Allow() returned false")
	}
}

// TestPreCharge_HandlesNoLabel verifies that Jobs without the secret-UID label are
// excluded by the HasLabels filter and do not affect the bucket store.
func TestPreCharge_HandlesNoLabel(t *testing.T) {
	defaults := Limits{RequestsPerMinute: 60, BurstSize: 3}
	store := NewStore()

	// Job with no secret-UID label at all.
	unlabeled := makeJobWithUID("j-unlabeled", "", 1, 5*time.Second)
	c := newBudgetFakeClient(t, unlabeled)

	if err := PreCharge(context.Background(), c, store, defaults, 60*time.Second); err != nil {
		t.Fatalf("PreCharge: %v", err)
	}

	// Store should remain empty — no buckets created for unlabeled Jobs.
	found := false
	store.m.Range(func(_, _ any) bool {
		found = true
		return false
	})
	if found {
		t.Errorf("PreCharge should not create buckets for Jobs without the secret-UID label")
	}
}
