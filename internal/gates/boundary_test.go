/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package gates unit tests for boundary.go: BoundaryDetected. Uses the
// controller-runtime fake client; scheme setup mirrors
// internal/controller/dispatch_helpers_test.go fakeClientForTest.
package gates

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// fakeClientForGatesTest returns a fake controller-runtime client with TIDE
// schema registered (mirrors internal/controller/dispatch_helpers_test.go).
func fakeClientForGatesTest(t *testing.T) (client.Client, *runtime.Scheme) {
	t.Helper()
	s := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).Build(), s
}

func mkMilestoneForBoundary(name, namespace string) *tideprojectv1alpha1.Milestone {
	return &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(name + "-uid"),
			Name:      name,
			Namespace: namespace,
		},
		Spec: tideprojectv1alpha1.MilestoneSpec{ProjectRef: "proj1"},
	}
}

func mkPhaseChild(t *testing.T, parent *tideprojectv1alpha1.Milestone, name string, statusPhase string, scheme *runtime.Scheme) *tideprojectv1alpha1.Phase {
	t.Helper()
	ph := &tideprojectv1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: parent.Namespace,
		},
		Spec: tideprojectv1alpha1.PhaseSpec{MilestoneRef: parent.Name},
		Status: tideprojectv1alpha1.PhaseStatus{
			Phase: statusPhase,
		},
	}
	if err := controllerutil.SetControllerReference(parent, ph, scheme); err != nil {
		t.Fatalf("SetControllerReference Phase->Milestone: %v", err)
	}
	return ph
}

// Test 1: childless — no Phase CRDs under the Milestone -> false (NOT a
// boundary; "all children Succeeded" is vacuously true on an empty set but we
// explicitly reject it per the plan's Test 7 — at least 1 child must exist).
func TestBoundaryDetectedNoChildren(t *testing.T) {
	c, _ := fakeClientForGatesTest(t)
	ms := mkMilestoneForBoundary("m1", "default")
	if err := c.Create(context.Background(), ms); err != nil {
		t.Fatalf("create milestone: %v", err)
	}

	got, err := BoundaryDetected(context.Background(), c, ms, "Phase")
	if err != nil {
		t.Fatalf("BoundaryDetected: %v", err)
	}
	if got {
		t.Errorf("BoundaryDetected (no children) = true; want false")
	}
}

// Test 2: 3 Phase children, all Succeeded -> true.
func TestBoundaryDetectedAllSucceeded(t *testing.T) {
	c, scheme := fakeClientForGatesTest(t)
	ms := mkMilestoneForBoundary("m2", "default")
	if err := c.Create(context.Background(), ms); err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	for _, name := range []string{"phase-a", "phase-b", "phase-c"} {
		ph := mkPhaseChild(t, ms, name, "Succeeded", scheme)
		if err := c.Create(context.Background(), ph); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	got, err := BoundaryDetected(context.Background(), c, ms, "Phase")
	if err != nil {
		t.Fatalf("BoundaryDetected: %v", err)
	}
	if !got {
		t.Errorf("BoundaryDetected (3 Succeeded) = false; want true")
	}
}

// Test 3: one child Running -> false.
func TestBoundaryDetectedOneRunning(t *testing.T) {
	c, scheme := fakeClientForGatesTest(t)
	ms := mkMilestoneForBoundary("m3", "default")
	if err := c.Create(context.Background(), ms); err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	for _, st := range []struct{ name, phase string }{
		{"phase-a", "Succeeded"},
		{"phase-b", "Running"},
		{"phase-c", "Succeeded"},
	} {
		ph := mkPhaseChild(t, ms, st.name, st.phase, scheme)
		if err := c.Create(context.Background(), ph); err != nil {
			t.Fatalf("create %s: %v", st.name, err)
		}
	}
	got, err := BoundaryDetected(context.Background(), c, ms, "Phase")
	if err != nil {
		t.Fatalf("BoundaryDetected: %v", err)
	}
	if got {
		t.Errorf("BoundaryDetected (one Running) = true; want false")
	}
}

// Test 4: one Failed + two Succeeded -> false (Failed is not a push-trigger
// terminal state per the plan).
func TestBoundaryDetectedOneFailed(t *testing.T) {
	c, scheme := fakeClientForGatesTest(t)
	ms := mkMilestoneForBoundary("m4", "default")
	if err := c.Create(context.Background(), ms); err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	for _, st := range []struct{ name, phase string }{
		{"phase-a", "Succeeded"},
		{"phase-b", "Failed"},
		{"phase-c", "Succeeded"},
	} {
		ph := mkPhaseChild(t, ms, st.name, st.phase, scheme)
		if err := c.Create(context.Background(), ph); err != nil {
			t.Fatalf("create %s: %v", st.name, err)
		}
	}
	got, err := BoundaryDetected(context.Background(), c, ms, "Phase")
	if err != nil {
		t.Fatalf("BoundaryDetected: %v", err)
	}
	if got {
		t.Errorf("BoundaryDetected (one Failed) = true; want false")
	}
}

// Test 5: owner-ref filter — sibling Phase under a DIFFERENT Milestone is
// ignored. Canonical filter is the OwnerRef IsControlledBy check (the
// dispatch_helpers MaterializeChildCRDs uses controllerutil.SetControllerReference,
// not labels, so owner refs are the universal seam).
func TestBoundaryDetectedOwnerRefFilter(t *testing.T) {
	c, scheme := fakeClientForGatesTest(t)
	msA := mkMilestoneForBoundary("ms-a", "default")
	msB := mkMilestoneForBoundary("ms-b", "default")
	if err := c.Create(context.Background(), msA); err != nil {
		t.Fatalf("create msA: %v", err)
	}
	if err := c.Create(context.Background(), msB); err != nil {
		t.Fatalf("create msB: %v", err)
	}
	// msA: 2 children, both Succeeded -> boundary should be true.
	for _, name := range []string{"a-phase-1", "a-phase-2"} {
		ph := mkPhaseChild(t, msA, name, "Succeeded", scheme)
		if err := c.Create(context.Background(), ph); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	// msB: 1 child, Running -> should NOT pollute msA's boundary check.
	{
		ph := mkPhaseChild(t, msB, "b-phase-1", "Running", scheme)
		if err := c.Create(context.Background(), ph); err != nil {
			t.Fatalf("create b-phase-1: %v", err)
		}
	}
	gotA, err := BoundaryDetected(context.Background(), c, msA, "Phase")
	if err != nil {
		t.Fatalf("BoundaryDetected msA: %v", err)
	}
	if !gotA {
		t.Errorf("BoundaryDetected (msA, all Succeeded) = false; want true (sibling under msB should NOT poison this)")
	}
	gotB, err := BoundaryDetected(context.Background(), c, msB, "Phase")
	if err != nil {
		t.Fatalf("BoundaryDetected msB: %v", err)
	}
	if gotB {
		t.Errorf("BoundaryDetected (msB, one Running) = true; want false")
	}
}

// Test 6: signature is `(bool, error)` — verified by the integration shape
// `if ok, err := gates.BoundaryDetected(...); ok && err == nil { ... }`.
func TestBoundaryDetectedSignature(t *testing.T) {
	c, scheme := fakeClientForGatesTest(t)
	ms := mkMilestoneForBoundary("m6", "default")
	if err := c.Create(context.Background(), ms); err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	ph := mkPhaseChild(t, ms, "only-phase", "Succeeded", scheme)
	if err := c.Create(context.Background(), ph); err != nil {
		t.Fatalf("create phase: %v", err)
	}

	if ok, err := BoundaryDetected(context.Background(), c, ms, "Phase"); !(ok && err == nil) {
		t.Errorf("integration shape (ok=%v, err=%v) — want ok=true, err=nil", ok, err)
	}
}

// Test 7: unsupported childKind -> (false, error).
func TestBoundaryDetectedUnsupportedKind(t *testing.T) {
	c, _ := fakeClientForGatesTest(t)
	ms := mkMilestoneForBoundary("m7", "default")
	if err := c.Create(context.Background(), ms); err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	ok, err := BoundaryDetected(context.Background(), c, ms, "Pod")
	if err == nil {
		t.Errorf("BoundaryDetected(childKind=Pod) error = nil; want non-nil")
	}
	if ok {
		t.Errorf("BoundaryDetected(childKind=Pod) ok = true; want false")
	}
}

// Cover the four supported childKinds at the API level — happy-path single
// child Succeeded under each parent kind.
func TestBoundaryDetectedSupportedKinds(t *testing.T) {
	for _, ck := range []string{"Milestone", "Phase", "Plan", "Task"} {
		ck := ck
		t.Run(ck, func(t *testing.T) {
			c, scheme := fakeClientForGatesTest(t)
			var parent client.Object
			var child client.Object
			switch ck {
			case "Milestone":
				p := &tideprojectv1alpha1.Project{
					ObjectMeta: metav1.ObjectMeta{UID: "p-uid", Name: "p1", Namespace: "default"},
				}
				if err := c.Create(context.Background(), p); err != nil {
					t.Fatalf("create project: %v", err)
				}
				m := &tideprojectv1alpha1.Milestone{
					ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
					Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: "p1"},
					Status:     tideprojectv1alpha1.MilestoneStatus{Phase: "Succeeded"},
				}
				if err := controllerutil.SetControllerReference(p, m, scheme); err != nil {
					t.Fatalf("setcontrollerref: %v", err)
				}
				if err := c.Create(context.Background(), m); err != nil {
					t.Fatalf("create milestone: %v", err)
				}
				parent, child = p, m
			case "Phase":
				m := mkMilestoneForBoundary("m1", "default")
				if err := c.Create(context.Background(), m); err != nil {
					t.Fatalf("create milestone: %v", err)
				}
				ph := mkPhaseChild(t, m, "ph1", "Succeeded", scheme)
				if err := c.Create(context.Background(), ph); err != nil {
					t.Fatalf("create phase: %v", err)
				}
				parent, child = m, ph
			case "Plan":
				ph := &tideprojectv1alpha1.Phase{
					ObjectMeta: metav1.ObjectMeta{UID: "ph-uid", Name: "ph1", Namespace: "default"},
					Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: "m1"},
				}
				if err := c.Create(context.Background(), ph); err != nil {
					t.Fatalf("create phase: %v", err)
				}
				pl := &tideprojectv1alpha1.Plan{
					ObjectMeta: metav1.ObjectMeta{Name: "pl1", Namespace: "default"},
					Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: "ph1"},
					Status:     tideprojectv1alpha1.PlanStatus{Phase: "Succeeded"},
				}
				if err := controllerutil.SetControllerReference(ph, pl, scheme); err != nil {
					t.Fatalf("setcontrollerref: %v", err)
				}
				if err := c.Create(context.Background(), pl); err != nil {
					t.Fatalf("create plan: %v", err)
				}
				parent, child = ph, pl
			case "Task":
				pl := &tideprojectv1alpha1.Plan{
					ObjectMeta: metav1.ObjectMeta{UID: "pl-uid", Name: "pl1", Namespace: "default"},
					Spec:       tideprojectv1alpha1.PlanSpec{PhaseRef: "ph1"},
				}
				if err := c.Create(context.Background(), pl); err != nil {
					t.Fatalf("create plan: %v", err)
				}
				tk := &tideprojectv1alpha1.Task{
					ObjectMeta: metav1.ObjectMeta{Name: "tk1", Namespace: "default"},
					Spec: tideprojectv1alpha1.TaskSpec{
						PlanRef:             "pl1",
						FilesTouched:        []string{"f1"},
						DeclaredOutputPaths: []string{"out1"},
					},
					Status: tideprojectv1alpha1.TaskStatus{Phase: "Succeeded"},
				}
				if err := controllerutil.SetControllerReference(pl, tk, scheme); err != nil {
					t.Fatalf("setcontrollerref: %v", err)
				}
				if err := c.Create(context.Background(), tk); err != nil {
					t.Fatalf("create task: %v", err)
				}
				parent, child = pl, tk
			}
			_ = child // silence unused if branch ever drops it
			got, err := BoundaryDetected(context.Background(), c, parent, ck)
			if err != nil {
				t.Fatalf("BoundaryDetected(%s): %v", ck, err)
			}
			if !got {
				t.Errorf("BoundaryDetected(%s, single Succeeded child) = false; want true", ck)
			}
		})
	}
}
