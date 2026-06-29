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

// Package controller — D3 concurrency cap gate unit tests (CONCUR-01/CONCUR-04).
// Uses a fake client pre-loaded with a non-terminal planner Job and a PlannerPool
// of capacity 1 to assert that the milestone dispatch path parks (RequeueAfter > 0,
// err == nil) when the live in-flight count meets the cap.
//
// These are pure Go tests (no envtest/Ginkgo) that run fast without a live cluster.
package controller

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	"github.com/jsquirrelz/tide/internal/pool"
)

// milestoneDispatchScheme builds a scheme with TIDE + batch/core types registered.
func milestoneDispatchScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	return fakeSchemeWithAll(t)
}

// TestConcurrencyCapGate_MilestoneDispatchParks constructs a MilestoneReconciler
// with a fake client pre-loaded with one non-terminal planner Job and a PlannerPool
// of capacity 1, then drives reconcilePlannerDispatch and asserts:
//  1. ctrl.Result.RequeueAfter > 0 (dispatch is deferred, not dropped)
//  2. err == nil (deferred dispatch is not an error per CONCUR-04)
//  3. No additional planner Job was created (gate fired BEFORE Acquire)
func TestConcurrencyCapGate_MilestoneDispatchParks(t *testing.T) {
	// Arrange: one non-terminal planner Job already running.
	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-ms-existing-1",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/role": "planner"},
		},
		// No terminal condition — counts as in-flight.
	}

	// A minimal Project so the early-project hold checks see a valid project.
	project := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-project",
			Namespace: "default",
			UID:       types.UID("proj-uid-cap"),
		},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			OutcomePrompt:  "build something",
		},
	}

	// A Milestone in an empty Phase (dispatch-eligible: not Succeeded/Failed/AwaitingApproval/Running).
	ms := &tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-cap-test",
			Namespace: "default",
			UID:       types.UID("ms-uid-cap"),
		},
		Spec: tideprojectv1alpha2.MilestoneSpec{
			ProjectRef: "test-project",
		},
	}

	s := milestoneDispatchScheme(t)
	c := newFakeClientForController(t, existingJob, project, ms)

	// PlannerPool of capacity 1 — one in-flight Job should park the next dispatch.
	plannerPool := pool.New(1, "planner")

	r := &MilestoneReconciler{
		Client:      c,
		Scheme:      s,
		PlannerPool: plannerPool,
		// WatchNamespace="" — cluster-scoped install (count all namespaces).
	}

	// Act: call reconcilePlannerDispatch directly (same package; bypasses the
	// r.Dispatcher!=nil guard in Reconcile, which is just a feature-flag gate).
	result, err := r.reconcilePlannerDispatch(context.Background(), ms)

	// Assert 1: deferred (RequeueAfter > 0), not an error (CONCUR-04).
	if err != nil {
		t.Fatalf("expected nil error for cap-reached park, got: %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("expected RequeueAfter > 0 when cap is reached, got %v", result.RequeueAfter)
	}

	// Assert 2: no additional planner Job was created (cap gate fired before Acquire).
	var jobs batchv1.JobList
	if err := c.List(context.Background(), &jobs, client.MatchingLabels{"tideproject.k8s/role": "planner"}); err != nil {
		t.Fatalf("listing planner jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Errorf("expected 1 planner Job (the pre-existing one), got %d — gate fired after Create (D-03 violation)", len(jobs.Items))
	}
}

// TestConcurrencyCapGate_RequeueAfterIs10s verifies the deferred RequeueAfter
// is exactly 10 seconds (CONCUR-04 / RESEARCH RQ-3/Pitfall 4 — longer than
// import-hold 5s, shorter than billing-halt 30s).
func TestConcurrencyCapGate_RequeueAfterIs10s(t *testing.T) {
	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-ms-existing-dur",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/role": "planner"},
		},
	}
	project := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-project-dur",
			Namespace: "default",
			UID:       types.UID("proj-uid-dur"),
		},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			OutcomePrompt:  "build something",
		},
	}
	ms := &tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-cap-dur",
			Namespace: "default",
			UID:       types.UID("ms-uid-dur"),
		},
		Spec: tideprojectv1alpha2.MilestoneSpec{ProjectRef: "test-project-dur"},
	}

	s := milestoneDispatchScheme(t)
	c := newFakeClientForController(t, existingJob, project, ms)
	plannerPool := pool.New(1, "planner")

	r := &MilestoneReconciler{
		Client:      c,
		Scheme:      s,
		PlannerPool: plannerPool,
	}

	result, err := r.reconcilePlannerDispatch(context.Background(), ms)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 10*time.Second {
		t.Errorf("RequeueAfter = %v, want 10s (CONCUR-04 / Pitfall 4)", result.RequeueAfter)
	}
}

// TestGatePrecedesAcquire_SlotNotConsumed verifies the gate returns BEFORE
// the semaphore is taken (D-03 no-slot-leak ordering invariant).
// With cap=1 and one in-flight job, the pool must still be fully available
// (no slots consumed) after the capped reconcile returns.
func TestGatePrecedesAcquire_SlotNotConsumed(t *testing.T) {
	existingJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-ms-existing-order",
			Namespace: "default",
			Labels:    map[string]string{"tideproject.k8s/role": "planner"},
		},
	}
	project := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-project-order",
			Namespace: "default",
			UID:       types.UID("proj-uid-order"),
		},
		Spec: tideprojectv1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			OutcomePrompt:  "build something",
		},
	}
	ms := &tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ms-cap-order",
			Namespace: "default",
			UID:       types.UID("ms-uid-order"),
		},
		Spec: tideprojectv1alpha2.MilestoneSpec{ProjectRef: "test-project-order"},
	}

	s := milestoneDispatchScheme(t)
	c := newFakeClientForController(t, existingJob, project, ms)
	plannerPool := pool.New(1, "planner")

	r := &MilestoneReconciler{
		Client:      c,
		Scheme:      s,
		PlannerPool: plannerPool,
	}

	result, err := r.reconcilePlannerDispatch(context.Background(), ms)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("expected RequeueAfter > 0 (cap gate fired), got %v", result.RequeueAfter)
	}

	// D-03 invariant: pool semaphore must be fully available (gate returned before Acquire).
	// Capacity-1 pool: if Acquire was called and not Released, next Acquire would block.
	// If gate returned before Acquire, semaphore is empty and next Acquire succeeds immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := plannerPool.Acquire(ctx); err != nil {
		t.Errorf("pool semaphore was consumed by the cap-gated reconcile (D-03 violation): Acquire blocked/failed: %v", err)
	} else {
		plannerPool.Release()
	}
}
