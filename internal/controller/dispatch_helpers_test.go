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
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ---------- ResolveProvider tests (pure function — no envtest) ----------

// Test 1: per-level model override wins over Project default + Helm default.
// D-02: dispatch level "milestone" (authors phase briefs) maps via
// levelOverrideKey to the Levels.Phase override slot — NOT Levels.Milestone
// (that slot now belongs to dispatch level "project", which authors
// MILESTONE.md). See levelOverrideKey's doc comment for the full table.
func TestResolveProviderPerLevelWins(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
				Model: "claude-sonnet-4-6",
				Levels: tideprojectv1alpha3.LevelOverrides{
					Phase: &tideprojectv1alpha3.LevelConfig{
						Model: "claude-opus-4-7",
					},
				},
			},
		},
	}
	defaults := ProviderDefaults{Models: map[string]string{"phase": "claude-haiku-4-5"}}
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
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
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
// D-02: dispatch level "milestone" maps to the Levels.Phase / helm "phase" slot.
func TestResolveProviderHelmDefaultFallback(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
	defaults := ProviderDefaults{Models: map[string]string{"phase": "claude-opus-4-7"}}
	spec := ResolveProvider(project, "milestone", defaults)
	if spec.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q, want %q (Helm default fallback)", spec.Model, "claude-opus-4-7")
	}
}

// Test 3b: Params merge — level Params override Project-level Params on key conflict.
// D-02: dispatch level "phase" (authors PLAN.md) maps to the Levels.Plan
// override slot (D-11 collapse — shared with dispatch level "plan").
func TestResolveProviderParamsMerge(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
				Levels: tideprojectv1alpha3.LevelOverrides{
					Plan: &tideprojectv1alpha3.LevelConfig{
						Params: map[string]string{"thinking-budget": "high", "level-only": "yes"},
					},
				},
			},
		},
	}
	defaults := ProviderDefaults{Models: map[string]string{"plan": "claude-sonnet-4-6"}}
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
	milestone := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("milestone-uid-001"),
			Name:      "test-milestone",
			Namespace: "default",
		},
	}
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-project",
			Namespace: "default",
		},
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
				Model: "claude-opus-4-7",
			},
		},
	}
	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 10}
	defaults := ProviderDefaults{Models: map[string]string{"milestone": "claude-opus-4-7"}}

	envIn, envBytes, err := BuildPlannerEnvelope("milestone", milestone, project, 1, "signed-token-abc", "author the first milestone", caps, "https://127.0.0.1:8443", defaults, "")
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

	if envIn.Prompt != "author the first milestone" {
		t.Errorf("Prompt = %q, want %q", envIn.Prompt, "author the first milestone")
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

// ---------- resolveImage tests (pure function — no envtest) ----------

// TestResolveImage_LevelWins: level Image override beats project default and Helm default.
func TestResolveImage_LevelWins(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
				Image: "ghcr.io/project-default",
				Levels: tideprojectv1alpha3.LevelOverrides{
					Plan: &tideprojectv1alpha3.LevelConfig{Image: "ghcr.io/level-override"},
				},
			},
		},
	}
	defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
	if got := resolveImage(project, "plan", defaults); got != "ghcr.io/level-override" {
		t.Errorf("resolveImage = %q, want %q (level override wins)", got, "ghcr.io/level-override")
	}
}

// TestResolveImage_ProjectDefaultWinsOverHelm: project Image wins when no level override.
func TestResolveImage_ProjectDefaultWinsOverHelm(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
				Image: "ghcr.io/project-image",
			},
		},
	}
	defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
	if got := resolveImage(project, "task", defaults); got != "ghcr.io/project-image" {
		t.Errorf("resolveImage = %q, want %q (project default wins over helm)", got, "ghcr.io/project-image")
	}
}

// TestResolveImage_HelmDefaultFallback: Helm default applies when project has no image set.
func TestResolveImage_HelmDefaultFallback(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
	defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
	if got := resolveImage(project, "milestone", defaults); got != "ghcr.io/helm-default" {
		t.Errorf("resolveImage = %q, want %q (helm default fallback)", got, "ghcr.io/helm-default")
	}
}

// TestResolveImage_NilProject_ReturnsHelmDefault: nil project returns helm default, no panic.
func TestResolveImage_NilProject_ReturnsHelmDefault(t *testing.T) {
	defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
	if got := resolveImage(nil, "plan", defaults); got != "ghcr.io/helm-default" {
		t.Errorf("resolveImage(nil) = %q, want %q", got, "ghcr.io/helm-default")
	}
}

// TestResolveImage_LevelConfigPresentImageEmpty_FallsThrough: level config set with Model only
// (Image "") falls through to project Spec.Subagent.Image.
func TestResolveImage_LevelConfigPresentImageEmpty_FallsThrough(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
				Image: "ghcr.io/project-image",
				Levels: tideprojectv1alpha3.LevelOverrides{
					Plan: &tideprojectv1alpha3.LevelConfig{Model: "claude-sonnet-4-6"}, // Image is ""
				},
			},
		},
	}
	defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
	if got := resolveImage(project, "plan", defaults); got != "ghcr.io/project-image" {
		t.Errorf("resolveImage = %q, want %q (empty level Image falls through)", got, "ghcr.io/project-image")
	}
}

// TestResolveImage_ProjectLevel_NoLevelTier: dispatch level "project" maps
// (D-02) to the Levels.Milestone override slot; with that slot unset here,
// resolution falls through to Spec.Subagent.Image directly.
func TestResolveImage_ProjectLevel_NoLevelTier(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			Subagent: tideprojectv1alpha3.SubagentConfig{
				Image: "ghcr.io/project-image",
			},
		},
	}
	defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
	if got := resolveImage(project, "project", defaults); got != "ghcr.io/project-image" {
		t.Errorf("resolveImage(project level) = %q, want %q", got, "ghcr.io/project-image")
	}
}

// ---------- BuildPlannerEnvelope tests ----------

// Test 4b: BuildPlannerEnvelope threads the supplied prompt into
// EnvelopeIn.Prompt and keeps it distinct from the signed token (defect #4 —
// the field was previously never assigned and the real Claude planner saw an
// empty prompt). Covers JSON round-trip so the on-PVC envelope carries it.
func TestBuildPlannerEnvelopePromptThreading(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			OutcomePrompt: "Add a /healthz endpoint returning 200 OK.",
		},
	}
	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 10}

	envIn, envBytes, err := BuildPlannerEnvelope("project", project, project, 1, "tok-xyz", project.Spec.OutcomePrompt, caps, "https://127.0.0.1:8443", ProviderDefaults{}, "")
	if err != nil {
		t.Fatalf("BuildPlannerEnvelope: %v", err)
	}
	if envIn.Prompt != "Add a /healthz endpoint returning 200 OK." {
		t.Errorf("Prompt = %q, want outcome prompt", envIn.Prompt)
	}
	// token and prompt must not be conflated.
	if envIn.SignedToken != "tok-xyz" {
		t.Errorf("SignedToken = %q, want %q", envIn.SignedToken, "tok-xyz")
	}
	if envIn.Prompt == envIn.SignedToken {
		t.Error("Prompt and SignedToken must be distinct fields")
	}

	var rt pkgdispatch.EnvelopeIn
	if err := json.Unmarshal(envBytes, &rt); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if rt.Prompt != envIn.Prompt {
		t.Errorf("round-trip Prompt = %q, want %q", rt.Prompt, envIn.Prompt)
	}

	// outcomePromptOf is nil-safe — a not-yet-resolved Project yields "".
	if got := outcomePromptOf(nil); got != "" {
		t.Errorf("outcomePromptOf(nil) = %q, want empty", got)
	}
	if got := outcomePromptOf(project); got != project.Spec.OutcomePrompt {
		t.Errorf("outcomePromptOf(project) = %q, want %q", got, project.Spec.OutcomePrompt)
	}
}

// ---------- SharedContext param tests (Phase 20 CACHE-02/D-07) ----------

// TestBuildPlannerEnvelopeSharedContext: BuildPlannerEnvelope stamps the
// supplied sharedContext into EnvelopeIn.SharedContext; two calls with the
// same blob produce byte-identical SharedContext (sibling identity — D-03/D-04).
func TestBuildPlannerEnvelopeSharedContext(t *testing.T) {
	proj := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			OutcomePrompt: "Build the auth service.",
		},
	}
	milestone := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{
			UID:  types.UID("ms-uid-sc-001"),
			Name: "ms-sc-test",
		},
	}
	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 10}
	blob := "Parent goal: build auth service.\nLoad-bearing constraints: use JWT.\nSiblings: [01 api, 02 db, 03 auth]"

	// Call #1 — sibling A.
	envA, bytesA, err := BuildPlannerEnvelope("milestone", milestone, proj, 1, "tok-a", "prompt", caps, "https://127.0.0.1:8443", ProviderDefaults{}, blob)
	if err != nil {
		t.Fatalf("BuildPlannerEnvelope (A): %v", err)
	}
	if envA.SharedContext != blob {
		t.Errorf("sibling A SharedContext = %q, want %q", envA.SharedContext, blob)
	}

	// Call #2 with same blob — sibling B. SharedContext must be byte-identical (D-03).
	envB, bytesB, err := BuildPlannerEnvelope("milestone", milestone, proj, 1, "tok-b", "prompt", caps, "https://127.0.0.1:8443", ProviderDefaults{}, blob)
	if err != nil {
		t.Fatalf("BuildPlannerEnvelope (B): %v", err)
	}
	if envB.SharedContext != envA.SharedContext {
		t.Errorf("sibling B SharedContext = %q, want byte-identical to A %q", envB.SharedContext, envA.SharedContext)
	}

	// Both blobs in the serialized bytes must be byte-identical.
	var rtA, rtB pkgdispatch.EnvelopeIn
	if err := json.Unmarshal(bytesA, &rtA); err != nil {
		t.Fatalf("unmarshal A: %v", err)
	}
	if err := json.Unmarshal(bytesB, &rtB); err != nil {
		t.Fatalf("unmarshal B: %v", err)
	}
	if rtA.SharedContext != rtB.SharedContext {
		t.Errorf("round-trip SharedContext mismatch: A=%q B=%q", rtA.SharedContext, rtB.SharedContext)
	}
}

// TestBuildPlannerEnvelopeSharedContextEmpty: sharedContext="" yields
// EnvelopeIn.SharedContext=="" and the marshaled bytes contain no
// "sharedContext" key (omitempty).
func TestBuildPlannerEnvelopeSharedContextEmpty(t *testing.T) {
	proj := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
	}
	ms := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("ms-uid-sc-empty"), Name: "ms-sc-empty"},
	}
	caps := pkgdispatch.Caps{WallClockSeconds: 300, Iterations: 5}

	envIn, envBytes, err := BuildPlannerEnvelope("milestone", ms, proj, 1, "tok", "prompt", caps, "https://127.0.0.1:8443", ProviderDefaults{}, "")
	if err != nil {
		t.Fatalf("BuildPlannerEnvelope: %v", err)
	}
	if envIn.SharedContext != "" {
		t.Errorf("SharedContext = %q, want empty string", envIn.SharedContext)
	}
	// omitempty: the key must be absent from the serialized JSON.
	if json.Valid(envBytes) && contains(string(envBytes), `"sharedContext"`) {
		t.Errorf("marshaled JSON contains \"sharedContext\" key when value is empty (omitempty expected to suppress it): %s", envBytes)
	}
}

// contains is a small helper used in SharedContext tests to avoid importing strings.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------- plannerInFlightCount tests (D3 concurrency cap — CONCUR-01) ----------

// newFakeClientForController builds a fake client with batchv1 + TIDE types registered.
func newFakeClientForController(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := tideprojectv1alpha3.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme tide: %v", err)
	}
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme batchv1: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme corev1: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

// makePlannerJob creates a batchv1.Job with the tideproject.k8s/role=planner label.
// terminal=true sets a Complete condition so isJobTerminal returns true.
func makePlannerJob(name, ns string, terminal bool) *batchv1.Job {
	j := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels:    map[string]string{"tideproject.k8s/role": "planner"},
		},
	}
	if terminal {
		j.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
		}
	}
	return j
}

// makeDeletingPlannerJob creates a non-terminal planner Job that is mid-deletion
// (DeletionTimestamp set). A finalizer is required so the fake client persists an
// object carrying a deletion timestamp. plannerInFlightCount must NOT count it.
func makeDeletingPlannerJob(name, ns string) *batchv1.Job {
	j := makePlannerJob(name, ns, false)
	ts := metav1.NewTime(time.Unix(1_700_000_000, 0))
	j.DeletionTimestamp = &ts
	j.Finalizers = []string{"tideproject.k8s/test-hold"}
	return j
}

// TestPlannerInFlightCount exercises the four key behaviors of plannerInFlightCount.
func TestPlannerInFlightCount(t *testing.T) {
	cases := []struct {
		name           string
		jobs           []*batchv1.Job
		watchNamespace string
		wantCount      int
	}{
		{
			name: "three non-terminal jobs returns 3",
			jobs: []*batchv1.Job{
				makePlannerJob("j1", "default", false),
				makePlannerJob("j2", "default", false),
				makePlannerJob("j3", "default", false),
			},
			watchNamespace: "",
			wantCount:      3,
		},
		{
			name: "two non-terminal and one terminal returns 2",
			jobs: []*batchv1.Job{
				makePlannerJob("j1", "default", false),
				makePlannerJob("j2", "default", false),
				makePlannerJob("j3", "default", true), // terminal
			},
			watchNamespace: "",
			wantCount:      2,
		},
		{
			name:           "zero jobs returns 0",
			jobs:           nil,
			watchNamespace: "",
			wantCount:      0,
		},
		{
			name: "deleting (DeletionTimestamp set) non-terminal job is not counted",
			jobs: []*batchv1.Job{
				makePlannerJob("j1", "default", false),
				makeDeletingPlannerJob("j2-deleting", "default"),
			},
			watchNamespace: "",
			wantCount:      1,
		},
		{
			name: "namespace-scoped: only counts jobs in watched namespace",
			jobs: []*batchv1.Job{
				makePlannerJob("j-a1", "ns-a", false),
				makePlannerJob("j-a2", "ns-a", false),
				makePlannerJob("j-b1", "ns-b", false),
			},
			watchNamespace: "ns-a",
			wantCount:      2,
		},
		{
			name: "watchNamespace empty: counts all namespaces",
			jobs: []*batchv1.Job{
				makePlannerJob("j-a1", "ns-a", false),
				makePlannerJob("j-b1", "ns-b", false),
			},
			watchNamespace: "",
			wantCount:      2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var objs []client.Object
			for _, j := range tc.jobs {
				objs = append(objs, j)
			}
			c := newFakeClientForController(t, objs...)
			count, err := plannerInFlightCount(context.Background(), c, tc.watchNamespace)
			if err != nil {
				t.Fatalf("plannerInFlightCount: %v", err)
			}
			if count != tc.wantCount {
				t.Errorf("plannerInFlightCount = %d, want %d", count, tc.wantCount)
			}
		})
	}
}

// ---------- resolveAgentIdentity tests (pure function — no envtest) ----------
//
// Precedence (D-03): project.Spec.Git.Agent* → helmDefaults.Agent* →
// pkggit compiled default. Tests use NON-DEFAULT strings at every tier so the
// compiled fallback cannot silently mask a missed wiring surface (Pitfall 3).

// Test 1 (compiled tier): nil project + zero-value ProviderDefaults falls all
// the way through to the pkg/git compiled default.
func TestResolveAgentIdentity_CompiledDefault(t *testing.T) {
	name, email := resolveAgentIdentity(nil, ProviderDefaults{})
	if name != "TIDE Agent" {
		t.Errorf("name = %q, want %q (compiled default)", name, "TIDE Agent")
	}
	if email != "tide-agent@tideproject.k8s" {
		t.Errorf("email = %q, want %q (compiled default)", email, "tide-agent@tideproject.k8s")
	}
}

// Test 2 (chart tier): chart-supplied identity wins over the compiled default
// when the Project sets nothing (Spec.Git nil).
func TestResolveAgentIdentity_ChartTier(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
	helm := ProviderDefaults{AgentName: "Chart Agent", AgentEmail: "chart@example.com"}
	name, email := resolveAgentIdentity(project, helm)
	if name != "Chart Agent" {
		t.Errorf("name = %q, want %q (chart tier)", name, "Chart Agent")
	}
	if email != "chart@example.com" {
		t.Errorf("email = %q, want %q (chart tier)", email, "chart@example.com")
	}
}

// Test 3 (spec tier beats chart): Project spec identity overrides a non-empty
// chart tier.
func TestResolveAgentIdentity_SpecBeatsChart(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{
			Git: &tideprojectv1alpha3.GitConfig{
				AgentName:  "Spec Agent",
				AgentEmail: "spec@example.com",
			},
		},
	}
	helm := ProviderDefaults{AgentName: "Chart Agent", AgentEmail: "chart@example.com"}
	name, email := resolveAgentIdentity(project, helm)
	if name != "Spec Agent" {
		t.Errorf("name = %q, want %q (spec beats chart)", name, "Spec Agent")
	}
	if email != "spec@example.com" {
		t.Errorf("email = %q, want %q (spec beats chart)", email, "spec@example.com")
	}
}

// Test 4 (per-field independence): the two fields resolve independently — a
// Project that sets only the name and a chart that sets only the email compose
// across tiers.
func TestResolveAgentIdentity_PerFieldIndependence(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{
			Git: &tideprojectv1alpha3.GitConfig{AgentName: "Spec Agent"},
		},
	}
	helm := ProviderDefaults{AgentEmail: "chart@example.com"}
	name, email := resolveAgentIdentity(project, helm)
	if name != "Spec Agent" {
		t.Errorf("name = %q, want %q (spec name)", name, "Spec Agent")
	}
	if email != "chart@example.com" {
		t.Errorf("email = %q, want %q (chart email)", email, "chart@example.com")
	}
}

// Test 5 (nil safety): a non-nil Project whose Spec.Git is nil must not panic —
// Spec.Git is a *GitConfig (Pitfall 7). Resolution falls to the chart tier.
func TestResolveAgentIdentity_NilGitConfig(t *testing.T) {
	project := &tideprojectv1alpha3.Project{}
	if project.Spec.Git != nil {
		t.Fatalf("test precondition: Spec.Git must be nil")
	}
	name, email := resolveAgentIdentity(project, ProviderDefaults{})
	if name != "TIDE Agent" || email != "tide-agent@tideproject.k8s" {
		t.Errorf("resolveAgentIdentity = (%q, %q), want compiled default", name, email)
	}
}
