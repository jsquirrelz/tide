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

// Plan 53-10 Task 2 — proving tests for the Task verdict-final findings-push
// trigger (maybeTriggerTaskFindingsPush, task_controller.go): a VerifyHalted
// Task's findings reach the run branch through the EXISTING push machinery
// WHILE ConditionVerifyHalt freezes all dispatch project-wide (the plan-check
// BLOCKER this plan closes, THE BLOCKER PROOF below), the carried-entry edge
// gate stops churn once carried, a busy-but-not-yet-carrying push Job signals
// retry until the ProjectReconciler's isStaleArtifactPush supersede path (or
// Job completion) heals it, and a nil-evaluation halt (T-53-25 poison guard)
// never triggers a push at all.
//
// OWN Test* entries, never TestControllers — the internal/controller package's
// sole Ginkgo entry point vacuously "passes" an unfiltered -run filter
// against plain go-tests (Phase 51-03 lesson).
package controller

import (
	"context"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// findingsPushTestProject returns a git-configured, run-branch-provisioned
// Project fixture (reuses artifactTestProject's guard-chain shape) carrying
// ConditionVerifyHalt=True — the exact frozen-halt scenario the blocker this
// plan closes describes: dispatch is frozen project-wide, but a findings push
// must still fire.
func findingsPushTestProject() *tideprojectv1alpha3.Project {
	p := artifactTestProject()
	p.Name = "vh-proj"
	p.UID = types.UID("vh-proj-uid")
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha3.ConditionVerifyHalt,
		Status:             metav1.ConditionTrue,
		Reason:             tideprojectv1alpha3.ReasonVerifyExhausted,
		LastTransitionTime: metav1.Now(),
	})
	return p
}

// findingsTaskReconciler builds a minimal TaskReconciler sufficient to drive
// maybeTriggerTaskFindingsPush directly (no Reconcile()/dispatch machinery
// needed — this is a focused unit test of the trigger helper itself).
func findingsTaskReconciler(c client.Client, s *runtime.Scheme) *TaskReconciler {
	return &TaskReconciler{
		Client: c,
		Scheme: s,
		Deps:   TaskReconcilerDeps{TidePushImage: "tide-push:latest"},
	}
}

// pushJobFor returns the deterministic push Job's NamespacedName for project.
func pushJobFor(project *tideprojectv1alpha3.Project) types.NamespacedName {
	return types.NamespacedName{Name: "tide-push-" + string(project.UID), Namespace: project.Namespace}
}

// ---------- (a) THE BLOCKER PROOF ----------

// TestTaskFindingsPush_BlockerProof_PushesWhileVerifyHaltTrue is the primary
// proof this plan exists to deliver: a contract-bearing Task at VerifyHalted
// with a recorded evaluation, on a Project carrying ConditionVerifyHalt=True,
// gets its findings staged onto the deterministic push Job — asserted WHILE
// the halt condition is True (dispatch frozen project-wide, push still fires).
func TestTaskFindingsPush_BlockerProof_PushesWhileVerifyHaltTrue(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := findingsPushTestProject()
	task := taskCR("t-halted", "uid-halted-task", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
	r := findingsTaskReconciler(c, s)

	// Precondition: dispatch is frozen project-wide.
	if !checkVerifyHalt(project) {
		t.Fatal("test fixture must carry ConditionVerifyHalt=True")
	}

	if _, err := r.maybeTriggerTaskFindingsPush(context.Background(), task, project); err != nil {
		t.Fatalf("maybeTriggerTaskFindingsPush: %v", err)
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), pushJobFor(project), &job); err != nil {
		t.Fatalf("expected push Job created while ConditionVerifyHalt=True: %v", err)
	}
	staged := job.Annotations[stagedEnvelopesAnnotation]
	wantEntry := "uid-halted-task:task/t-halted"
	if !strings.Contains(staged, wantEntry) {
		t.Errorf("staged-envelopes annotation = %q, want to contain %q", staged, wantEntry)
	}

	// Re-assert the halt is still True at the moment the push Job exists —
	// this IS the blocker: dispatch stays frozen but findings still reached
	// the branch, never waiting for `tide resume`.
	if !checkVerifyHalt(project) {
		t.Fatal("ConditionVerifyHalt must remain True across the assertion")
	}
}

// ---------- (b) Edge-gate: no churn once carried ----------

// TestTaskFindingsPush_EdgeGate_NoChurnOnceCarried drives the trigger twice:
// the first call creates the carrying push Job, the second must report
// carried=true and create no second Job — steady state while halted (T-53-23).
func TestTaskFindingsPush_EdgeGate_NoChurnOnceCarried(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := findingsPushTestProject()
	task := taskCR("t-halted", "uid-halted-task", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
	r := findingsTaskReconciler(c, s)

	if _, err := r.maybeTriggerTaskFindingsPush(context.Background(), task, project); err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	var firstJob batchv1.Job
	if err := c.Get(context.Background(), pushJobFor(project), &firstJob); err != nil {
		t.Fatalf("get first job: %v", err)
	}

	carried, err := r.maybeTriggerTaskFindingsPush(context.Background(), task, project)
	if err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	if !carried {
		t.Error("expected carried=true once the push Job's annotation contains this Task's entry")
	}

	var jobs batchv1.JobList
	if err := c.List(context.Background(), &jobs); err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Errorf("expected exactly 1 push Job (no churn while halted); got %d", len(jobs.Items))
	}
}

// ---------- (c) Busy race: retry then carry ----------

// TestTaskFindingsPush_BusyRace_RetriesThenCarries pre-creates a push Job
// whose annotation lacks the task's entry (an in-flight push that snapshotted
// before this Task's status patch landed). The trigger must signal
// carried=false without creating a second Job (single-flight); once that Job
// completes (removed here, simulating Job completion/GC), the next invocation
// creates the carrying Job.
func TestTaskFindingsPush_BusyRace_RetriesThenCarries(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := findingsPushTestProject()
	task := taskCR("t-halted", "uid-halted-task", tideprojectv1alpha3.LevelPhaseVerifyHalted, true)
	busy := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "tide-push-" + string(project.UID),
			Namespace:   project.Namespace,
			Annotations: map[string]string{stagedEnvelopesAnnotation: "uid-other:milestone/m-other"},
		},
	}
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task, busy).Build()
	r := findingsTaskReconciler(c, s)

	carried, err := r.maybeTriggerTaskFindingsPush(context.Background(), task, project)
	if err != nil {
		t.Fatalf("trigger during busy race: %v", err)
	}
	if carried {
		t.Error("expected carried=false while the busy Job's annotation lacks this Task's entry")
	}

	var jobs batchv1.JobList
	if err := c.List(context.Background(), &jobs); err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("busy race must not create a second Job (single-flight); got %d", len(jobs.Items))
	}

	// The busy Job completes (removed here — simulates Job GC / the next push
	// cycle); the next invocation creates the carrying Job.
	if err := c.Delete(context.Background(), busy); err != nil {
		t.Fatalf("delete busy job: %v", err)
	}

	if _, err := r.maybeTriggerTaskFindingsPush(context.Background(), task, project); err != nil {
		t.Fatalf("trigger after busy job cleared: %v", err)
	}
	var job batchv1.Job
	if err := c.Get(context.Background(), pushJobFor(project), &job); err != nil {
		t.Fatalf("expected carrying Job created after busy Job cleared: %v", err)
	}
	staged := job.Annotations[stagedEnvelopesAnnotation]
	if !strings.Contains(staged, "uid-halted-task:task/t-halted") {
		t.Errorf("staged-envelopes annotation = %q, want to contain the task entry", staged)
	}
}

// ---------- (d) Poison guard: nil evaluation never triggers ----------

// TestTaskFindingsPush_PoisonGuard_NilEvaluationNeverTriggers pins T-53-25: a
// VerifyHalted Task whose verifier crashed before producing a verdict (nil
// LoopStatus.LastEvaluation — the VerifierEnvelopeUnreadable shape) must never
// trigger a push, because a task-kind entry without findings.json hard-fails
// the ENTIRE cumulative push in tide-push.
func TestTaskFindingsPush_PoisonGuard_NilEvaluationNeverTriggers(t *testing.T) {
	s := fakeSchemeWithAll(t)
	project := findingsPushTestProject()
	task := taskCR("t-halted-noeval", "uid-noeval-task", tideprojectv1alpha3.LevelPhaseVerifyHalted, false)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(project, task).Build()
	r := findingsTaskReconciler(c, s)

	carried, err := r.maybeTriggerTaskFindingsPush(context.Background(), task, project)
	if err != nil {
		t.Fatalf("maybeTriggerTaskFindingsPush: %v", err)
	}
	if carried {
		t.Error("a nil-evaluation halt must never report carried=true")
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), pushJobFor(project), &job); err == nil {
		t.Error("a VerifyHalted Task with nil LoopStatus.LastEvaluation must never trigger a push Job (T-53-25 poison guard)")
	}
}
