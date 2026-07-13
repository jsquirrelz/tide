/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Plan 37-06 — artifact-stage write-path trigger. Unit coverage for the
// cumulative envelope map (collectStageEnvelopes), the deterministic-Job
// dispatch (triggerArtifactPush), single-flight no-op, and the guard chain.
package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// artifactTestProject returns a git-configured, run-branch-provisioned Project
// suitable for driving triggerArtifactPush past its guard chain.
func artifactTestProject() *tideprojectv1alpha3.Project {
	p := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "proj",
			Namespace: "default",
			UID:       types.UID("proj-uid"),
		},
		Spec: tideprojectv1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			Git:            &tideprojectv1alpha3.GitConfig{RepoURL: "https://example.com/repo.git"},
		},
	}
	p.Status.Phase = tideprojectv1alpha3.PhaseRunning
	p.Status.Git.BranchName = "tide/run-proj-123"
	return p
}

func milestone(name, uid, phase string) *tideprojectv1alpha3.Milestone {
	m := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(uid)},
	}
	m.Status.Phase = phase
	return m
}

func phaseCR(name, uid, phase string) *tideprojectv1alpha3.Phase {
	p := &tideprojectv1alpha3.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(uid)},
	}
	p.Status.Phase = phase
	return p
}

// ---------- Test 1: collectStageEnvelopes cumulative + deterministic ----------

func TestArtifactPush_CollectStageEnvelopes_CumulativeAndDeterministic(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()

	objs := []client.Object{
		project,
		// 2 planner-completed Milestones (one Succeeded, one AwaitingApproval).
		milestone("m-beta", "uid-mb", "Succeeded"),
		milestone("m-alpha", "uid-ma", "AwaitingApproval"),
		// 1 planner-completed Phase + 1 still-planning Phase (excluded).
		phaseCR("ph-done", "uid-pd", "Succeeded"),
		phaseCR("ph-planning", "uid-pp", "Running"),
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()

	got, err := collectStageEnvelopes(context.Background(), c, project)
	if err != nil {
		t.Fatalf("collectStageEnvelopes: %v", err)
	}

	// Expect exactly 4: 2 milestones + 1 phase + the project (has child milestones).
	// still-planning phase excluded. Order = kind then name.
	want := []string{
		"uid-ma:milestone/m-alpha",
		"uid-mb:milestone/m-beta",
		"uid-pd:phase/ph-done",
		"proj-uid:project/proj",
	}
	if len(got) != len(want) {
		t.Fatalf("entry count = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry[%d] = %q, want %q (full=%v)", i, got[i], want[i], got)
		}
	}
}

// ---------- Test 2: triggerArtifactPush creates a Job with --stage-envelopes ----------

func TestArtifactPush_TriggerCreatesJobWithStageEnvelopes(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, milestone("m-alpha", "uid-ma", "Succeeded")).
		Build()

	if err := triggerArtifactPush(context.Background(), c, s, project, "milestone", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}); err != nil {
		t.Fatalf("triggerArtifactPush: %v", err)
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err != nil {
		t.Fatalf("expected push Job created: %v", err)
	}
	args := strings.Join(job.Spec.Template.Spec.Containers[0].Args, " ")
	if !strings.Contains(args, "--stage-envelopes=uid-ma:milestone/m-alpha") {
		t.Errorf("args missing --stage-envelopes with collected CSV; got: %s", args)
	}
	if !strings.Contains(args, "--branch=tide/run-proj-123") {
		t.Errorf("args missing --branch; got: %s", args)
	}
	if !strings.Contains(args, "--commit-message=tide: stage planning artifacts (milestone)") {
		t.Errorf("commit message should identify artifact stage + level; got: %s", args)
	}
}

// ---------- Test 3: single-flight no-op when the Job already exists ----------

func TestArtifactPush_SingleFlightNoOp(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()
	existing := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "tide-push-proj-uid", Namespace: "default"},
	}
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, milestone("m-alpha", "uid-ma", "Succeeded"), existing).
		Build()

	if err := triggerArtifactPush(context.Background(), c, s, project, "milestone", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}); err != nil {
		t.Fatalf("triggerArtifactPush (single-flight): %v", err)
	}

	// The pre-existing Job must be untouched — no args mutated (it had none).
	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err != nil {
		t.Fatalf("get existing job: %v", err)
	}
	if len(job.Spec.Template.Spec.Containers) != 0 {
		t.Errorf("single-flight should not overwrite the in-flight Job; containers=%d", len(job.Spec.Template.Spec.Containers))
	}
}

// ---------- Test 4: guard chain skips without error ----------

func TestArtifactPush_GuardChainSkips(t *testing.T) {
	s := fakeSchemeWithAll(t)

	t.Run("nil project", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(s).Build()
		if err := triggerArtifactPush(context.Background(), c, s, nil, "milestone", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}); err != nil {
			t.Errorf("nil project should skip without error: %v", err)
		}
	})

	t.Run("git-less project", func(t *testing.T) {
		p := artifactTestProject()
		p.Spec.Git = nil
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(p).Build()
		if err := triggerArtifactPush(context.Background(), c, s, p, "milestone", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}); err != nil {
			t.Errorf("git-less project should skip without error: %v", err)
		}
		var job batchv1.Job
		if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err == nil {
			t.Error("git-less project must not create a push Job")
		}
	})

	t.Run("empty image", func(t *testing.T) {
		p := artifactTestProject()
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(p, milestone("m-alpha", "uid-ma", "Succeeded")).Build()
		if err := triggerArtifactPush(context.Background(), c, s, p, "milestone", "", defaultSharedPVCName, ProviderDefaults{}); err != nil {
			t.Errorf("empty image should skip without error: %v", err)
		}
		var job batchv1.Job
		if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err == nil {
			t.Error("empty image must not create a push Job")
		}
	})

	t.Run("no run branch", func(t *testing.T) {
		p := artifactTestProject()
		p.Status.Git.BranchName = ""
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(p, milestone("m-alpha", "uid-ma", "Succeeded")).Build()
		if err := triggerArtifactPush(context.Background(), c, s, p, "milestone", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}); err != nil {
			t.Errorf("no run branch should skip without error: %v", err)
		}
		var job batchv1.Job
		if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err == nil {
			t.Error("no run branch must not create a push Job (parked-arm requeue retries)")
		}
	})

	t.Run("empty map (nothing planner-completed)", func(t *testing.T) {
		p := artifactTestProject()
		// No children materialized and project not Complete → empty map → skip.
		// (A still-planning milestone would NOT make the map empty: its existence
		// proves the project planner authored it, so the Project itself qualifies.)
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(p).Build()
		if err := triggerArtifactPush(context.Background(), c, s, p, "milestone", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}); err != nil {
			t.Errorf("empty map should skip without error: %v", err)
		}
		var job batchv1.Job
		if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err == nil {
			t.Error("empty map must not create a push Job")
		}
	})
}

// ---------- Task 2: parked-arm trigger + Pitfall 8 requeue ----------

// parkedMilestone returns an AwaitingApproval milestone owned by the artifact
// test project, with no approve annotation (so reconcilePlannerDispatch takes the
// parked branch). Its own AwaitingApproval phase makes collectStageEnvelopes
// non-empty, so the trigger has something to stage.
func parkedMilestone() *tideprojectv1alpha3.Milestone {
	m := milestone("m-parked", "uid-parked", "AwaitingApproval")
	m.Spec.ProjectRef = "proj"
	return m
}

// Task 2 (a): a parked milestone triggers the artifact push (Job carries
// --stage-envelopes) AND requeues on the 30s cadence — the completion→park path's
// retry arm materializes artifacts before the operator approves (D-01).
func TestArtifactPush_ParkedMilestoneTriggersAndRequeues(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()
	ms := parkedMilestone()
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, ms).Build()

	r := &MilestoneReconciler{Client: c, Scheme: s, Deps: PlannerReconcilerDeps{TidePushImage: "tide-push:latest"}}
	res, err := r.reconcilePlannerDispatch(context.Background(), ms)
	if err != nil {
		t.Fatalf("reconcilePlannerDispatch (parked): %v", err)
	}
	if res.RequeueAfter != 30*time.Second {
		t.Errorf("parked milestone RequeueAfter = %v, want 30s (Pitfall 8)", res.RequeueAfter)
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err != nil {
		t.Fatalf("parked milestone must trigger the artifact push: %v", err)
	}
	args := strings.Join(job.Spec.Template.Spec.Containers[0].Args, " ")
	if !strings.Contains(args, "--stage-envelopes=uid-parked:milestone/m-parked") {
		t.Errorf("push Job args missing parked milestone envelope; got: %s", args)
	}
}

// Task 2 (b): Pitfall 8 regression guard — with the deterministic push Job already
// busy, the parked milestone STILL requeues on the 30s cadence (so the trigger is
// never permanently swallowed) and the single-flight no-op leaves the Job untouched.
func TestArtifactPush_ParkedMilestoneRequeuesWhileBusy(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()
	ms := parkedMilestone()
	busy := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "tide-push-proj-uid", Namespace: "default"}}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, ms, busy).Build()

	r := &MilestoneReconciler{Client: c, Scheme: s, Deps: PlannerReconcilerDeps{TidePushImage: "tide-push:latest"}}
	res, err := r.reconcilePlannerDispatch(context.Background(), ms)
	if err != nil {
		t.Fatalf("reconcilePlannerDispatch (parked, busy): %v", err)
	}
	if res.RequeueAfter != 30*time.Second {
		t.Errorf("parked milestone RequeueAfter = %v, want 30s even while Job busy (Pitfall 8)", res.RequeueAfter)
	}

	// Single-flight: the pre-existing busy Job is untouched (no containers written).
	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err != nil {
		t.Fatalf("get busy job: %v", err)
	}
	if len(job.Spec.Template.Spec.Containers) != 0 {
		t.Errorf("single-flight must not overwrite the in-flight Job; containers=%d", len(job.Spec.Template.Spec.Containers))
	}
}
