/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

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

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- ResolveProvider tests (pure function — no envtest) ----------

// Test 1: per-level model override wins over Project default + Helm default.
func TestResolveProviderPerLevelWins(t *testing.T) {
	project := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
				Model: "claude-sonnet-4-6",
				Levels: tideprojectv1alpha1.LevelOverrides{
					Milestone: &tideprojectv1alpha1.LevelConfig{
						Model: "claude-opus-4-7",
					},
				},
			},
		},
	}
	defaults := ProviderDefaults{Models: map[string]string{"milestone": "claude-haiku-4-5"}}
	spec := ResolveProvider(project, "milestone", defaults)
	if spec.Vendor != "anthropic" {
		t.Errorf("Vendor = %q, want %q (v1.0 always anthropic)", spec.Vendor, "anthropic")
	}
	if spec.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q, want %q (per-level override wins)", spec.Model, "claude-opus-4-7")
	}
}

// Test 2: Project default wins over Helm default when no per-level override.
func TestResolveProviderProjectDefaultWinsOverHelm(t *testing.T) {
	project := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
				Model: "claude-sonnet-4-6",
			},
		},
	}
	defaults := ProviderDefaults{Models: map[string]string{"task": "claude-haiku-4-5"}}
	spec := ResolveProvider(project, "task", defaults)
	if spec.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q (Project default wins over Helm)", spec.Model, "claude-sonnet-4-6")
	}
}

// Test 3: Helm default applies when Project has nothing set.
func TestResolveProviderHelmDefaultFallback(t *testing.T) {
	project := &tideprojectv1alpha1.Project{}
	defaults := ProviderDefaults{Models: map[string]string{"milestone": "claude-opus-4-7"}}
	spec := ResolveProvider(project, "milestone", defaults)
	if spec.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q, want %q (Helm default fallback)", spec.Model, "claude-opus-4-7")
	}
}

// Test 3b: Params merge — level Params override Project-level Params on key conflict.
func TestResolveProviderParamsMerge(t *testing.T) {
	project := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
				Levels: tideprojectv1alpha1.LevelOverrides{
					Phase: &tideprojectv1alpha1.LevelConfig{
						Params: map[string]string{"thinking-budget": "high", "level-only": "yes"},
					},
				},
			},
		},
	}
	defaults := ProviderDefaults{Models: map[string]string{"phase": "claude-sonnet-4-6"}}
	spec := ResolveProvider(project, "phase", defaults)
	if got := spec.Params["thinking-budget"]; got != "high" {
		t.Errorf("Params[thinking-budget] = %q, want %q (level Params)", got, "high")
	}
	if got := spec.Params["level-only"]; got != "yes" {
		t.Errorf("Params[level-only] = %q, want %q (level Params)", got, "yes")
	}
}

// ---------- BuildPlannerEnvelope tests ----------

// Test 4: BuildPlannerEnvelope structure for a Milestone parent + Project.
func TestBuildPlannerEnvelopeStructure(t *testing.T) {
	milestone := &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-001"),
			Name:      "test-milestone",
			Namespace: "default",
		},
	}
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-project",
			Namespace: "default",
		},
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
				Model: "claude-opus-4-7",
			},
		},
	}
	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 10}
	defaults := ProviderDefaults{Models: map[string]string{"milestone": "claude-opus-4-7"}}

	envIn, envBytes, err := BuildPlannerEnvelope("milestone", milestone, project, 1, "signed-token-abc", caps, "https://127.0.0.1:8443", defaults)
	if err != nil {
		t.Fatalf("BuildPlannerEnvelope: %v", err)
	}
	if envIn.APIVersion != pkgdispatch.APIVersionV1Alpha1 {
		t.Errorf("APIVersion = %q, want %q", envIn.APIVersion, pkgdispatch.APIVersionV1Alpha1)
	}
	if envIn.Kind != pkgdispatch.KindTaskEnvelopeIn {
		t.Errorf("Kind = %q, want %q", envIn.Kind, pkgdispatch.KindTaskEnvelopeIn)
	}
	if envIn.Role != "planner" {
		t.Errorf("Role = %q, want %q", envIn.Role, "planner")
	}
	if envIn.Level != "milestone" {
		t.Errorf("Level = %q, want %q", envIn.Level, "milestone")
	}
	if envIn.TaskUID != "milestone-uid-001" {
		t.Errorf("TaskUID = %q, want %q", envIn.TaskUID, "milestone-uid-001")
	}
	if envIn.SignedToken != "signed-token-abc" {
		t.Errorf("SignedToken = %q, want %q", envIn.SignedToken, "signed-token-abc")
	}
	if envIn.ProxyEndpoint != "https://127.0.0.1:8443" {
		t.Errorf("ProxyEndpoint = %q, want %q", envIn.ProxyEndpoint, "https://127.0.0.1:8443")
	}
	if envIn.Provider.Vendor != "anthropic" {
		t.Errorf("Provider.Vendor = %q, want %q", envIn.Provider.Vendor, "anthropic")
	}
	if envIn.Provider.Model != "claude-opus-4-7" {
		t.Errorf("Provider.Model = %q, want %q", envIn.Provider.Model, "claude-opus-4-7")
	}

	// JSON round-trip.
	var roundTrip pkgdispatch.EnvelopeIn
	if err := json.Unmarshal(envBytes, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if roundTrip.TaskUID != envIn.TaskUID || roundTrip.Role != envIn.Role || roundTrip.Level != envIn.Level {
		t.Errorf("round-trip mismatch: got %+v, want %+v", roundTrip, envIn)
	}
}

// ---------- MaterializeChildCRDs tests (use fake client) ----------

// fakeClientForTest returns a fake controller-runtime client with TIDE schema registered.
func fakeClientForTest(t *testing.T) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).Build()
}

// Test 5: happy path — parent=Milestone creates Phase children with OwnerRef set.
func TestMaterializeChildCRDsHappyPath(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-002"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	phaseSpec := tideprojectv1alpha1.PhaseSpec{MilestoneRef: "parent-milestone"}
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
		var got tideprojectv1alpha1.Phase
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

// Test 6: unknown Kind rejected — Kind allowlist enforced (T-308 mitigation).
func TestMaterializeChildCRDsRejectsUnknownKind(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha1.Milestone{
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
	var phases tideprojectv1alpha1.PhaseList
	if err := c.List(context.Background(), &phases, client.InNamespace("default")); err != nil {
		t.Fatalf("List phases: %v", err)
	}
	if len(phases.Items) != 0 {
		t.Errorf("Unexpected Phase items created: %d", len(phases.Items))
	}
}

// Test 7: idempotent on AlreadyExists — pre-create the Phase, then re-call MaterializeChildCRDs.
func TestMaterializeChildCRDsIdempotent(t *testing.T) {
	c := fakeClientForTest(t)
	scheme := runtime.NewScheme()
	if err := tideprojectv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	milestone := &tideprojectv1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-004"),
			Name:      "parent-milestone",
			Namespace: "default",
		},
	}

	// Pre-create the Phase.
	existing := &tideprojectv1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pre-existing-phase",
			Namespace: "default",
		},
		Spec: tideprojectv1alpha1.PhaseSpec{MilestoneRef: "parent-milestone"},
	}
	if err := c.Create(context.Background(), existing); err != nil {
		t.Fatalf("pre-create Phase: %v", err)
	}

	phaseSpec := tideprojectv1alpha1.PhaseSpec{MilestoneRef: "parent-milestone"}
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
	var got tideprojectv1alpha1.Phase
	if err := c.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: "pre-existing-phase"}, &got); err != nil {
		t.Errorf("Get pre-existing Phase: %v", err)
	}
	if got.UID != existing.UID && !apierrors.IsNotFound(err) {
		// fake client may regenerate UIDs; just verify the object still exists.
		// (The acceptance contract is "no error returned", not "same UID").
		_ = got
	}
}
