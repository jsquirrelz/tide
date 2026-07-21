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
	"os"
	"path/filepath"
	"slices"
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

// taskCR builds a Task fixture for the taskFindingsStageable predicate
// coverage. withEvaluation controls whether Status.LoopStatus.LastEvaluation
// is set (the applyLoopStatus presence-proxy guard, task_controller.go:2505).
func taskCR(name, uid, phase string, withEvaluation bool) *tideprojectv1alpha3.Task {
	t := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(uid)},
	}
	t.Status.Phase = phase
	if withEvaluation {
		t.Status.LoopStatus.LastEvaluation = &tideprojectv1alpha3.EvaluationSummary{
			Decision: "APPROVED",
		}
	}
	return t
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

// ---------- Task 1: taskFindingsStageable predicate coverage (a)-(f) ----------

// TestCollectStageEnvelopes drives collectStageEnvelopes' task-kind predicate
// (taskFindingsStageable) through the six fixtures the plan requires: only a
// verdict-final Task carrying a recorded evaluation stages a "task" entry;
// mid-loop, pre-verify, and evaluation-less-halted Tasks never do (the poison
// guard — an entry without findings.json hard-fails the ENTIRE cumulative
// push in tide-push).
func TestCollectStageEnvelopes(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()

	t.Run("a_VerifyHalted_with_evaluation_stages", func(t *testing.T) {
		task := taskCR("t-halted-eval", "uid-t1", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		want := "uid-t1:task/t-halted-eval"
		if !containsEntry(got, want) {
			t.Fatalf("entries=%v want to contain %q", got, want)
		}
	})

	t.Run("b_VerifyHalted_no_evaluation_excluded", func(t *testing.T) {
		task := taskCR("t-halted-noeval", "uid-t2", tideprojectv1alpha3.LevelPhaseVerifyHalted, false)
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		if containsEntry(got, "uid-t2:task/t-halted-noeval") {
			t.Fatalf("entries=%v must NOT contain the evaluation-less halted task (poison guard)", got)
		}
	})

	t.Run("c_Succeeded_with_evaluation_stages", func(t *testing.T) {
		task := taskCR("t-succeeded-eval", "uid-t3", tideprojectv1alpha3.LevelPhaseSucceeded, true)
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		want := "uid-t3:task/t-succeeded-eval"
		if !containsEntry(got, want) {
			t.Fatalf("entries=%v want to contain %q", got, want)
		}
	})

	t.Run("d_Succeeded_no_evaluation_excluded", func(t *testing.T) {
		task := taskCR("t-succeeded-noeval", "uid-t4", tideprojectv1alpha3.LevelPhaseSucceeded, false)
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		if containsEntry(got, "uid-t4:task/t-succeeded-noeval") {
			t.Fatalf("entries=%v must NOT contain the pre-verify Succeeded task", got)
		}
	})

	t.Run("e_Verifying_with_evaluation_excluded", func(t *testing.T) {
		task := taskCR("t-verifying-eval", "uid-t5", tideprojectv1alpha3.LevelPhaseVerifying, true)
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		if containsEntry(got, "uid-t5:task/t-verifying-eval") {
			t.Fatalf("entries=%v must NOT contain a mid-loop Verifying task (never stage mid-iteration)", got)
		}
	})

	t.Run("f_AwaitingApproval_with_evaluation_stages", func(t *testing.T) {
		task := taskCR("t-parked-eval", "uid-t6", tideprojectv1alpha3.LevelPhaseAwaitingApproval, true)
		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		want := "uid-t6:task/t-parked-eval"
		if !containsEntry(got, want) {
			t.Fatalf("entries=%v want to contain %q", got, want)
		}
		// Assert the DestPrefix's first segment (what tide-push's strings.Cut
		// keys its kind dispatch on, cmd/tide-push/main.go) is exactly "task".
		for _, e := range got {
			if e == want {
				_, destPrefix, _ := strings.Cut(e, ":")
				kind, _, _ := strings.Cut(destPrefix, "/")
				if kind != "task" {
					t.Fatalf("entry %q: kind = %q, want %q", e, kind, "task")
				}
			}
		}
	})
}

// containsEntry reports whether entries contains the exact string want.
func containsEntry(entries []string, want string) bool {
	return slices.Contains(entries, want)
}

// ---------- WR-05: findings.json image-skew / write-failure guard ----------

// withWorkspacesRoot points the WR-05 disk check at a temp dir for the test's
// duration and restores the production mount point afterwards.
func withWorkspacesRoot(t *testing.T) string {
	t.Helper()
	prev := workspacesRoot
	root := t.TempDir()
	workspacesRoot = root
	t.Cleanup(func() { workspacesRoot = prev })
	return root
}

// mkEnvelopeDir creates <root>/<projectUID>/workspace/envelopes/<taskUID>
// (the manager-side view of a verifier Job's envelope dir), optionally with
// a findings.json inside.
func mkEnvelopeDir(t *testing.T, root, projectUID, taskUID string, withFindings bool) {
	t.Helper()
	dir := filepath.Join(root, projectUID, "workspace", "envelopes", taskUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir envelope dir: %v", err)
	}
	if withFindings {
		if err := os.WriteFile(filepath.Join(dir, "findings.json"), []byte(`{"verdict":"BLOCKED"}`), 0o644); err != nil {
			t.Fatalf("write findings.json: %v", err)
		}
	}
}

// TestArtifactPush_FindingsSkewGuard pins WR-05 (Phase 53 code review): a
// stageable Task whose envelope dir OBSERVABLY lacks findings.json (a
// verifier image predating the 53-11 writer, or the tolerated
// write_findings OSError swallow) must never be staged — tide-push stays
// fail-closed and a single such entry hard-fails the ENTIRE cumulative
// push, boundary pushes included, with no self-heal (the verifier Job is
// complete; the file will never appear). When the manager cannot observe
// the dir at all (no PVC mount — envtest), the Status-only predicate
// stands unchanged.
func TestArtifactPush_FindingsSkewGuard(t *testing.T) {
	s := fakeSchemeWithAll(t)

	t.Run("dir exists without findings.json: excluded from the collector", func(t *testing.T) {
		root := withWorkspacesRoot(t)
		project := artifactTestProject()
		task := taskCR("t-skew", "uid-skew", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
		mkEnvelopeDir(t, root, string(project.UID), "uid-skew", false)

		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		if containsEntry(got, "uid-skew:task/t-skew") {
			t.Fatalf("entries=%v must NOT contain the proven-missing task — it would hard-fail the whole cumulative push (WR-05)", got)
		}
	})

	t.Run("dir exists with findings.json: staged", func(t *testing.T) {
		root := withWorkspacesRoot(t)
		project := artifactTestProject()
		task := taskCR("t-ok", "uid-ok", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
		mkEnvelopeDir(t, root, string(project.UID), "uid-ok", true)

		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		if !containsEntry(got, "uid-ok:task/t-ok") {
			t.Fatalf("entries=%v want to contain the findings-backed task", got)
		}
	})

	t.Run("dir unobservable (no PVC visibility): status proxy stands", func(t *testing.T) {
		withWorkspacesRoot(t) // empty temp root — no envelope dirs at all
		project := artifactTestProject()
		task := taskCR("t-unobservable", "uid-unob", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)

		c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
		got, err := collectStageEnvelopes(context.Background(), c, project)
		if err != nil {
			t.Fatalf("collectStageEnvelopes: %v", err)
		}
		if !containsEntry(got, "uid-unob:task/t-unobservable") {
			t.Fatalf("entries=%v want the status-proxy fallback to stage the task when the PVC is unobservable", got)
		}
	})

	t.Run("ensure-entry union applies the same guard", func(t *testing.T) {
		root := withWorkspacesRoot(t)
		project := artifactTestProject()
		skewed := taskCR("t-skew-ensure", "uid-skew-e", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
		mkEnvelopeDir(t, root, string(project.UID), "uid-skew-e", false)

		got := ensureTaskEntries(nil, string(project.UID), []*tideprojectv1alpha3.Task{skewed})
		if len(got) != 0 {
			t.Fatalf("ensureTaskEntries=%v must not union a proven-missing task entry (WR-05)", got)
		}
	})
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

// ---------- Plan 53-10: ensure-entry union (just-patched Task races the List) ----------

// TestArtifactPush_EnsureEntryUnion_AddsMissingTaskEntry proves the exact race
// this plan closes: a Task fixture that is NOT visible to collectStageEnvelopes'
// own List (simulating an informer cache that has not yet observed the
// just-applied status patch) still rides the push when passed as an ensure entry.
func TestArtifactPush_EnsureEntryUnion_AddsMissingTaskEntry(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()
	// No Task fixture registered with the fake client at all.
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project).Build()

	task := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{Name: "t-just-patched", Namespace: "default", UID: types.UID("uid-jp")},
	}

	if err := triggerArtifactPush(context.Background(), c, s, project, "task", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}, task); err != nil {
		t.Fatalf("triggerArtifactPush with ensure: %v", err)
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err != nil {
		t.Fatalf("expected push Job created: %v", err)
	}
	args := strings.Join(job.Spec.Template.Spec.Containers[0].Args, " ")
	if !strings.Contains(args, "--stage-envelopes=uid-jp:task/t-just-patched") {
		t.Errorf("args missing ensure-union task entry; got: %s", args)
	}
}

// TestArtifactPush_EnsureEntryUnion_DedupsAgainstListedEntry proves ensure is
// deduped (by UID/entry, not blindly appended) when the List already found the
// same Task — no duplicate --stage-envelopes entry.
func TestArtifactPush_EnsureEntryUnion_DedupsAgainstListedEntry(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()
	listedTask := taskCR("t-listed", "uid-listed", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, listedTask).Build()

	if err := triggerArtifactPush(context.Background(), c, s, project, "task", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}, listedTask); err != nil {
		t.Fatalf("triggerArtifactPush with duplicate ensure: %v", err)
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err != nil {
		t.Fatalf("expected push Job created: %v", err)
	}
	args := strings.Join(job.Spec.Template.Spec.Containers[0].Args, " ")
	count := strings.Count(args, "uid-listed:task/t-listed")
	if count != 1 {
		t.Errorf("expected exactly one occurrence of the deduped entry; got %d in args: %s", count, args)
	}
}

// TestArtifactPush_EnsureEntryUnion_NoEnsureArgsUnaffected proves the four
// existing planner-tier callers (which pass no ensure Tasks) behave identically
// to pre-53-10 — no ensure param means no union, no behavior change.
func TestArtifactPush_EnsureEntryUnion_NoEnsureArgsUnaffected(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := artifactTestProject()
	c := fake.NewClientBuilder().WithScheme(s).
		WithObjects(project, milestone("m-alpha", "uid-ma", "Succeeded")).
		Build()

	if err := triggerArtifactPush(context.Background(), c, s, project, "milestone", "tide-push:latest", defaultSharedPVCName, ProviderDefaults{}); err != nil {
		t.Fatalf("triggerArtifactPush (no ensure): %v", err)
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tide-push-proj-uid", Namespace: "default"}, &job); err != nil {
		t.Fatalf("expected push Job created: %v", err)
	}
	args := strings.Join(job.Spec.Template.Spec.Containers[0].Args, " ")
	if !strings.Contains(args, "--stage-envelopes=uid-ma:milestone/m-alpha") {
		t.Errorf("args missing collected CSV; got: %s", args)
	}
	if strings.Count(args, "--stage-envelopes=") != 1 {
		t.Errorf("expected exactly one --stage-envelopes arg; got: %s", args)
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
