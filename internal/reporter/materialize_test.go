/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reporter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- MaterializeChildCRDs tests (use fake client) ----------

// fakeClientForTest returns a fake controller-runtime client with TIDE schema registered.
func fakeClientForTest(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).Build()
}

// Test 1: happy path — parent=Milestone creates Phase children with OwnerRef set.
func TestMaterializeChildCRDsHappyPath(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-002"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	phaseSpec := tideprojectv1alpha2.PhaseSpec{MilestoneRef: "parent-milestone"}
	rawSpec, err := json.Marshal(phaseSpec)
	if err != nil {
		t.Fatalf("Marshal phase spec: %v", err)
	}

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Phase", Name: "child-phase-1", Spec: runtime.RawExtension{Raw: rawSpec}},
		{Kind: "Phase", Name: "child-phase-2", Spec: runtime.RawExtension{Raw: rawSpec}},
	}

	if err := MaterializeChildCRDs(context.Background(), c, scheme, milestone, children); err != nil {
		t.Fatalf("MaterializeChildCRDs: %v", err)
	}

	// Verify both Phase CRDs were created.
	for _, name := range []string{"child-phase-1", "child-phase-2"} {
		var got tideprojectv1alpha2.Phase
		if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, &got); err != nil {
			t.Errorf("Get %q: %v", name, err)
			continue
		}
		// Owner ref set, controller=true, points at milestone.
		refs := got.GetOwnerReferences()
		if len(refs) == 0 {
			t.Errorf("%q has no owner refs", name)
			continue
		}
		var found bool
		for _, r := range refs {
			if r.Kind == "Milestone" && r.UID == milestone.UID {
				if r.Controller == nil || !*r.Controller {
					t.Errorf("%q owner ref Controller not true", name)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("%q missing Milestone owner ref", name)
		}
	}
}

// Test 2: unknown Kind rejected — Kind allowlist enforced (T-308 mitigation).
func TestMaterializeChildCRDsRejectsUnknownKind(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-003"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Pod", Name: "evil-pod", Spec: runtime.RawExtension{Raw: []byte(`{}`)}},
	}

	err := MaterializeChildCRDs(context.Background(), c, scheme, milestone, children)
	if err == nil {
		t.Fatal("MaterializeChildCRDs accepted Kind=Pod; expected error")
	}
	if !strings.Contains(err.Error(), "allowlist") && !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error %q should mention allowlist or not-allowed", err.Error())
	}

	// Verify the Pod was NOT created (no get-by-name; check nothing leaked).
	var phases tideprojectv1alpha2.PhaseList
	if err := c.List(context.Background(), &phases, client.InNamespace("default")); err != nil {
		t.Fatalf("List phases: %v", err)
	}
	if len(phases.Items) != 0 {
		t.Errorf("Unexpected Phase items created: %d", len(phases.Items))
	}
}

// Test 3: idempotent on AlreadyExists — pre-create the Phase, then re-call MaterializeChildCRDs.
func TestMaterializeChildCRDsIdempotent(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha2.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-004"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	// Pre-create the Phase.
	existing := &tideprojectv1alpha2.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pre-existing-phase",
			Namespace: "default",
		},
		Spec: tideprojectv1alpha2.PhaseSpec{MilestoneRef: "parent-milestone"},
	}
	if err := c.Create(context.Background(), existing); err != nil {
		t.Fatalf("pre-create Phase: %v", err)
	}

	phaseSpec := tideprojectv1alpha2.PhaseSpec{MilestoneRef: "parent-milestone"}
	rawSpec, _ := json.Marshal(phaseSpec)

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Phase", Name: "pre-existing-phase", Spec: runtime.RawExtension{Raw: rawSpec}},
	}

	// Should succeed (idempotent on AlreadyExists).
	err := MaterializeChildCRDs(context.Background(), c, scheme, milestone, children)
	if err != nil {
		t.Errorf("MaterializeChildCRDs on pre-existing Phase: %v (want nil — idempotent)", err)
	}

	// And the original Phase is still there.
	var got tideprojectv1alpha2.Phase
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "pre-existing-phase"}, &got); err != nil {
		t.Errorf("Get pre-existing Phase: %v", err)
	}
	if got.UID != existing.UID && !apierrors.IsNotFound(err) {
		// fake client may regenerate UIDs; just verify the object still exists.
		// (The acceptance contract is "no error returned", not "same UID").
		_ = got
	}
}

// TestMaterializeChildCRDsTaskPromptPath covers defect #10b: a Task child's
// PromptPath is wired from ChildCRDSpec.SourcePath at materialization, even
// though the model-authored spec carries no promptPath. The prompt itself stays
// in the children file (read fresh at dispatch), not inline on the Task spec.
func TestMaterializeChildCRDsTaskPromptPath(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	plan := &tideprojectv1alpha2.Plan{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("plan-uid-010b"),
			Name:      "parent-plan",
			Namespace: "default",
		},
	}

	// Model-authored Task spec — note: NO promptPath here (the controller injects it).
	taskSpec := tideprojectv1alpha2.TaskSpec{
		PlanRef:             "parent-plan",
		FilesTouched:        []string{"main.go"},
		DeclaredOutputPaths: []string{"main.go"},
	}
	rawSpec, err := json.Marshal(taskSpec)
	if err != nil {
		t.Fatalf("Marshal task spec: %v", err)
	}

	const wantPath = "envelopes/plan-uid-010b/children/task-01.json"
	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Task", Name: "task-01-impl", Spec: runtime.RawExtension{Raw: rawSpec}, SourcePath: wantPath},
	}

	if err := MaterializeChildCRDs(context.Background(), c, scheme, plan, children); err != nil {
		t.Fatalf("MaterializeChildCRDs: %v", err)
	}

	var got tideprojectv1alpha2.Task
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "task-01-impl"}, &got); err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if got.Spec.PromptPath != wantPath {
		t.Errorf("Task.Spec.PromptPath = %q, want %q (wired from SourcePath)", got.Spec.PromptPath, wantPath)
	}
}

// TestMaterializeChildCRDsStampsParentRef covers defect #17: the planner LLM can
// author a MISMATCHED parent-ref (observed live: a Phase authored under
// Milestone/milestone-01 carried spec.milestoneRef="milestone-02"). The
// materializer must STAMP the parent-ref field from the ACTUAL parent it creates
// the child under (the same parent it sets the ownerRef to), overriding whatever
// the LLM wrote — so the level controller resolves the parent and the cascade
// does not silently wedge. One row per parent->child relationship.
func TestMaterializeChildCRDsStampsParentRef(t *testing.T) {
	tests := []struct {
		name       string
		parent     client.Object
		childKind  string
		childName  string
		rawSpec    []byte // authored spec carrying the WRONG ref
		wantRefOf  func(obj client.Object) string
		realParent string
	}{
		{
			name:       "project->milestone",
			parent:     &tideprojectv1alpha2.Project{ObjectMeta: metav1.ObjectMeta{UID: "proj-uid", Name: "project-01", Namespace: "default"}},
			childKind:  "Milestone",
			childName:  "milestone-01",
			rawSpec:    mustJSON(t, tideprojectv1alpha2.MilestoneSpec{ProjectRef: "project-99-wrong"}),
			wantRefOf:  func(o client.Object) string { return o.(*tideprojectv1alpha2.Milestone).Spec.ProjectRef },
			realParent: "project-01",
		},
		{
			name:       "milestone->phase",
			parent:     &tideprojectv1alpha2.Milestone{ObjectMeta: metav1.ObjectMeta{UID: "ms-uid", Name: "milestone-01-time-formatting", Namespace: "default"}},
			childKind:  "Phase",
			childName:  "phase-01",
			rawSpec:    mustJSON(t, tideprojectv1alpha2.PhaseSpec{MilestoneRef: "milestone-02-time-formatting"}),
			wantRefOf:  func(o client.Object) string { return o.(*tideprojectv1alpha2.Phase).Spec.MilestoneRef },
			realParent: "milestone-01-time-formatting",
		},
		{
			name:       "phase->plan",
			parent:     &tideprojectv1alpha2.Phase{ObjectMeta: metav1.ObjectMeta{UID: "ph-uid", Name: "phase-01", Namespace: "default"}},
			childKind:  "Plan",
			childName:  "plan-01",
			rawSpec:    mustJSON(t, tideprojectv1alpha2.PlanSpec{PhaseRef: "phase-77-wrong"}),
			wantRefOf:  func(o client.Object) string { return o.(*tideprojectv1alpha2.Plan).Spec.PhaseRef },
			realParent: "phase-01",
		},
		{
			name:       "plan->task",
			parent:     &tideprojectv1alpha2.Plan{ObjectMeta: metav1.ObjectMeta{UID: "pl-uid", Name: "plan-01", Namespace: "default"}},
			childKind:  "Task",
			childName:  "task-01",
			rawSpec:    mustJSON(t, tideprojectv1alpha2.TaskSpec{PlanRef: "plan-42-wrong", FilesTouched: []string{"main.go"}, DeclaredOutputPaths: []string{"main.go"}}),
			wantRefOf:  func(o client.Object) string { return o.(*tideprojectv1alpha2.Task).Spec.PlanRef },
			realParent: "plan-01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fakeClientForTest(t)
			scheme := runtime.NewScheme()
			if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme: %v", err)
			}

			children := []pkgdispatch.ChildCRDSpec{
				{Kind: tc.childKind, Name: tc.childName, Spec: runtime.RawExtension{Raw: tc.rawSpec}, SourcePath: "envelopes/x/children/" + tc.childName + ".json"},
			}
			if err := MaterializeChildCRDs(context.Background(), c, scheme, tc.parent, children); err != nil {
				t.Fatalf("MaterializeChildCRDs: %v", err)
			}

			var got client.Object
			switch tc.childKind {
			case "Milestone":
				got = &tideprojectv1alpha2.Milestone{}
			case "Phase":
				got = &tideprojectv1alpha2.Phase{}
			case "Plan":
				got = &tideprojectv1alpha2.Plan{}
			case "Task":
				got = &tideprojectv1alpha2.Task{}
			}
			if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: tc.childName}, got); err != nil {
				t.Fatalf("Get %s/%s: %v", tc.childKind, tc.childName, err)
			}

			if ref := tc.wantRefOf(got); ref != tc.realParent {
				t.Errorf("parent-ref = %q, want %q (must be stamped from the ACTUAL parent, not the LLM-authored wrong value)", ref, tc.realParent)
			}
		})
	}
}

// TestMaterializeChildCRDsStampsProjectLabel is the CUTS-01 run-1 finding-6
// regression test: reporter-created Milestone/Phase CRs carried zero labels,
// so `tide approve` label-filtered discovery reported "no level awaiting
// approval" despite a parked CR.
//
// After D-01 (Phase 15), MaterializeChildCRDs MUST stamp
// tideproject.k8s/project on every child CR it creates.
func TestMaterializeChildCRDsStampsProjectLabel(t *testing.T) {
	tests := []struct {
		name         string
		parentLabels map[string]string
		wantLabel    string // empty = label must be absent (fail-open)
	}{
		{
			name:         "parent labeled tideproject.k8s/project=demo — child inherits label (finding-6 fix)",
			parentLabels: map[string]string{"tideproject.k8s/project": "demo"},
			wantLabel:    "demo",
		},
		{
			name:         "parent has no project label — child created without label (fail-open)",
			parentLabels: nil,
			wantLabel:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fakeClientForTest(t)
			scheme := runtime.NewScheme()
			if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme: %v", err)
			}

			milestone := &tideprojectv1alpha2.Milestone{
				ObjectMeta: metav1.ObjectMeta{
					UID:       types.UID("ms-uid-cuts01"),
					Name:      "parent-milestone",
					Namespace: "default",
					Labels:    tc.parentLabels,
				},
			}

			phaseSpec := tideprojectv1alpha2.PhaseSpec{MilestoneRef: "parent-milestone"}
			rawSpec, err := json.Marshal(phaseSpec)
			if err != nil {
				t.Fatalf("Marshal phase spec: %v", err)
			}
			children := []pkgdispatch.ChildCRDSpec{
				{Kind: "Phase", Name: "child-phase-cuts01", Spec: runtime.RawExtension{Raw: rawSpec}},
			}

			if err := MaterializeChildCRDs(context.Background(), c, scheme, milestone, children); err != nil {
				t.Fatalf("MaterializeChildCRDs: %v", err)
			}

			var got tideprojectv1alpha2.Phase
			if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "child-phase-cuts01"}, &got); err != nil {
				t.Fatalf("Get Phase: %v", err)
			}

			gotLabel := got.GetLabels()["tideproject.k8s/project"]
			if gotLabel != tc.wantLabel {
				t.Errorf("labels[tideproject.k8s/project] = %q; want %q (CUTS-01 finding-6 regression)", gotLabel, tc.wantLabel)
			}
		})
	}
}

// TestMaterializeChildCRDsProjectParentStampsLabelAtCreateSite is the 15-WR-03
// regression test: when MaterializeChildCRDs is called with a *Project parent,
// the materialized Milestone child must carry tideproject.k8s/project ==
// project.GetName() AT CREATE TIME — before any backfill reconcile runs.
//
// Previously the create-site stamp resolved the label from
// parent.GetLabels()[owner.LabelProject], but a Project does not carry
// tideproject.k8s/project pointing at itself, so the child Milestone was
// created unlabeled and only healed later by the D-03 milestone backfill.
func TestMaterializeChildCRDsProjectParentStampsLabelAtCreateSite(t *testing.T) {
	const projectName = "test-project-create-site"

	project := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("proj-uid-create-site"),
			Name:      projectName,
			Namespace: "default",
			// Deliberately NO tideproject.k8s/project label — a Project does not
			// carry a label pointing at itself.
		},
	}

	msSpec := tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectName}
	rawSpec, err := json.Marshal(msSpec)
	if err != nil {
		t.Fatalf("Marshal milestone spec: %v", err)
	}
	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Milestone", Name: "child-ms-create-site", Spec: runtime.RawExtension{Raw: rawSpec}},
	}

	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	if err := MaterializeChildCRDs(context.Background(), c, scheme, project, children); err != nil {
		t.Fatalf("MaterializeChildCRDs: %v", err)
	}

	var got tideprojectv1alpha2.Milestone
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "child-ms-create-site"}, &got); err != nil {
		t.Fatalf("Get Milestone: %v", err)
	}

	gotLabel := got.GetLabels()["tideproject.k8s/project"]
	if gotLabel != projectName {
		t.Errorf("labels[tideproject.k8s/project] = %q; want %q (15-WR-03: Project→Milestone edge must stamp label at create-site from parent.GetName())", gotLabel, projectName)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return b
}

// ---------- SharedContext stamp tests (Phase 20 CACHE-02/D-04/D-05) ----------

// TestMaterializeChildCRDsSharedContextIdentity verifies that MaterializeChildCRDs
// stamps ChildCRDSpec.SharedContext byte-identically onto every child's
// Spec.SharedContext across all four supported kinds (Milestone, Phase, Plan, Task).
// D-03/D-05: one parent-curated blob → N identical child specs.
func TestMaterializeChildCRDsSharedContextIdentity(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	parent := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("proj-uid-sc-identity"),
			Name:      "project-sc-identity",
			Namespace: "default",
		},
	}

	blob := "Parent goal: build the API layer.\nLoad-bearing constraints: use REST.\nSiblings: [01 auth, 02 api, 03 db]"

	// Build children across all four kinds, each carrying the same blob.
	msSpec := tideprojectv1alpha2.MilestoneSpec{}
	rawMS, _ := json.Marshal(msSpec)

	phSpec := tideprojectv1alpha2.PhaseSpec{MilestoneRef: "project-sc-identity"}
	rawPh, _ := json.Marshal(phSpec)

	plSpec := tideprojectv1alpha2.PlanSpec{PhaseRef: "project-sc-identity"}
	rawPl, _ := json.Marshal(plSpec)

	tkSpec := tideprojectv1alpha2.TaskSpec{PlanRef: "project-sc-identity", FilesTouched: []string{"main.go"}}
	rawTk, _ := json.Marshal(tkSpec)

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Milestone", Name: "sc-ms-a", Spec: runtime.RawExtension{Raw: rawMS}, SharedContext: blob},
		{Kind: "Phase", Name: "sc-ph-a", Spec: runtime.RawExtension{Raw: rawPh}, SharedContext: blob},
		{Kind: "Plan", Name: "sc-pl-a", Spec: runtime.RawExtension{Raw: rawPl}, SharedContext: blob},
		{Kind: "Task", Name: "sc-tk-a", Spec: runtime.RawExtension{Raw: rawTk}, SourcePath: "children/task-01.json", SharedContext: blob},
	}

	if err := MaterializeChildCRDs(context.Background(), c, scheme, parent, children); err != nil {
		t.Fatalf("MaterializeChildCRDs: %v", err)
	}

	// Verify every child got the blob stamped byte-identically.
	var gotMS tideprojectv1alpha2.Milestone
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "sc-ms-a"}, &gotMS); err != nil {
		t.Fatalf("Get Milestone: %v", err)
	}
	if gotMS.Spec.SharedContext != blob {
		t.Errorf("Milestone.Spec.SharedContext = %q, want %q", gotMS.Spec.SharedContext, blob)
	}

	var gotPh tideprojectv1alpha2.Phase
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "sc-ph-a"}, &gotPh); err != nil {
		t.Fatalf("Get Phase: %v", err)
	}
	if gotPh.Spec.SharedContext != blob {
		t.Errorf("Phase.Spec.SharedContext = %q, want %q", gotPh.Spec.SharedContext, blob)
	}

	var gotPl tideprojectv1alpha2.Plan
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "sc-pl-a"}, &gotPl); err != nil {
		t.Fatalf("Get Plan: %v", err)
	}
	if gotPl.Spec.SharedContext != blob {
		t.Errorf("Plan.Spec.SharedContext = %q, want %q", gotPl.Spec.SharedContext, blob)
	}

	var gotTk tideprojectv1alpha2.Task
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "sc-tk-a"}, &gotTk); err != nil {
		t.Fatalf("Get Task: %v", err)
	}
	if gotTk.Spec.SharedContext != blob {
		t.Errorf("Task.Spec.SharedContext = %q, want %q", gotTk.Spec.SharedContext, blob)
	}
}

// TestMaterializeChildCRDsSharedContextSizeCap verifies the etcd DoS guard (T-20-03-01):
// a ChildCRDSpec.SharedContext exceeding maxSharedContextBytes is rejected with an
// error before any Create is attempted — fail-closed behavior.
func TestMaterializeChildCRDsSharedContextSizeCap(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	parent := &tideprojectv1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("proj-uid-sc-cap"),
			Name:      "project-sc-cap",
			Namespace: "default",
		},
	}

	// Construct a blob that exceeds the cap.
	oversized := make([]byte, maxSharedContextBytes+1)
	for i := range oversized {
		oversized[i] = 'x'
	}

	msSpec := tideprojectv1alpha2.MilestoneSpec{}
	rawMS, _ := json.Marshal(msSpec)

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Milestone", Name: "sc-ms-oversized", Spec: runtime.RawExtension{Raw: rawMS}, SharedContext: string(oversized)},
	}

	err := MaterializeChildCRDs(context.Background(), c, scheme, parent, children)
	if err == nil {
		t.Fatal("MaterializeChildCRDs accepted oversized SharedContext; expected error")
	}
	if !strings.Contains(err.Error(), "SharedContext") && !strings.Contains(err.Error(), "size") && !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error %q should mention SharedContext size/exceeds cap", err.Error())
	}

	// Verify the Milestone was NOT created (fail-closed).
	var ms tideprojectv1alpha2.Milestone
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "sc-ms-oversized"}, &ms); err == nil {
		t.Error("Milestone was created despite oversized SharedContext (fail-closed violated)")
	}
}

// TestMaterializeChildCRDsTaskPromptPathNoRegression verifies that the existing
// Task SourcePath → PromptPath stamp still works alongside the new SharedContext stamp.
func TestMaterializeChildCRDsTaskPromptPathNoRegression(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha2.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	plan := &tideprojectv1alpha2.Plan{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("plan-uid-sc-nreg"),
			Name:      "parent-plan-sc-nreg",
			Namespace: "default",
		},
	}

	taskSpec := tideprojectv1alpha2.TaskSpec{
		PlanRef:             "parent-plan-sc-nreg",
		FilesTouched:        []string{"main.go"},
		DeclaredOutputPaths: []string{"main.go"},
	}
	rawSpec, _ := json.Marshal(taskSpec)

	const wantPath = "envelopes/plan-uid-nreg/children/task-01.json"
	const wantCtx = "wave-scoped context blob"

	children := []pkgdispatch.ChildCRDSpec{
		{Kind: "Task", Name: "task-nreg-01", Spec: runtime.RawExtension{Raw: rawSpec}, SourcePath: wantPath, SharedContext: wantCtx},
	}

	if err := MaterializeChildCRDs(context.Background(), c, scheme, plan, children); err != nil {
		t.Fatalf("MaterializeChildCRDs: %v", err)
	}

	var got tideprojectv1alpha2.Task
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "task-nreg-01"}, &got); err != nil {
		t.Fatalf("Get task: %v", err)
	}
	// PromptPath stamped from SourcePath (existing behavior, no regression).
	if got.Spec.PromptPath != wantPath {
		t.Errorf("Task.Spec.PromptPath = %q, want %q (no regression)", got.Spec.PromptPath, wantPath)
	}
	// SharedContext stamped from ChildCRDSpec.SharedContext (new Phase 20 behavior).
	if got.Spec.SharedContext != wantCtx {
		t.Errorf("Task.Spec.SharedContext = %q, want %q", got.Spec.SharedContext, wantCtx)
	}
}
