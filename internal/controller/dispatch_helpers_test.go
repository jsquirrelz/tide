/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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

	envIn, envBytes, err := BuildPlannerEnvelope("milestone", milestone, project, 1, "signed-token-abc", "author the first milestone", caps, "https://127.0.0.1:8443", defaults)
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
	project := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
				Image: "ghcr.io/project-default",
				Levels: tideprojectv1alpha1.LevelOverrides{
					Plan: &tideprojectv1alpha1.LevelConfig{Image: "ghcr.io/level-override"},
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
	project := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
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
	project := &tideprojectv1alpha1.Project{}
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
	project := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
				Image: "ghcr.io/project-image",
				Levels: tideprojectv1alpha1.LevelOverrides{
					Plan: &tideprojectv1alpha1.LevelConfig{Model: "claude-sonnet-4-6"}, // Image is ""
				},
			},
		},
	}
	defaults := ProviderDefaults{Image: "ghcr.io/helm-default"}
	if got := resolveImage(project, "plan", defaults); got != "ghcr.io/project-image" {
		t.Errorf("resolveImage = %q, want %q (empty level Image falls through)", got, "ghcr.io/project-image")
	}
}

// TestResolveImage_ProjectLevel_NoLevelTier: level "project" has no level-config case;
// Spec.Subagent.Image is returned directly.
func TestResolveImage_ProjectLevel_NoLevelTier(t *testing.T) {
	project := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Subagent: tideprojectv1alpha1.SubagentConfig{
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
	project := &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"},
		Spec: tideprojectv1alpha1.ProjectSpec{
			OutcomePrompt: "Add a /healthz endpoint returning 200 OK.",
		},
	}
	caps := pkgdispatch.Caps{WallClockSeconds: 600, Iterations: 10}

	envIn, envBytes, err := BuildPlannerEnvelope("project", project, project, 1, "tok-xyz", project.Spec.OutcomePrompt, caps, "https://127.0.0.1:8443", ProviderDefaults{})
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
