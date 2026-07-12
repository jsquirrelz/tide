/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Pure-Go unit tests (no envtest) for the per-wave integration gate in
// reconcileWaveMaterialization (Plan 11-03 Task 3). Uses the fake
// controller-runtime client to drive the reconciler across the five steps
// described in the plan's behavior block.
package controller

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	pkggit "github.com/jsquirrelz/tide/pkg/git"
)

// fakeSchemeForWaveInteg returns a scheme with TIDE types + batch/v1 registered.
func fakeSchemeForWaveInteg(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := fakeSchemeWithAll(t) // reuses fakeSchemeWithAll from task_controller_extracted_test.go
	return s
}

// waveIntegProject returns a minimal Project with git config set, wired as
// the owner of a Plan via the tideproject.k8s/project label. This lets
// resolveProjectForPlan find the Project so triggerWaveIntegrationJob can
// dispatch the integration Job.
func waveIntegProject(t *testing.T, name string) *tideprojectv1alpha3.Project {
	t.Helper()
	return &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID("proj-uid-wave-integ"),
		},
		Spec: tideprojectv1alpha3.ProjectSpec{SchemaRevision: "v1alpha3",
			TargetRepo: "https://github.com/example/test.git",
			Git: &tideprojectv1alpha3.GitConfig{
				RepoURL:        "https://github.com/example/test.git",
				CredsSecretRef: "test-creds",
			},
		},
		Status: tideprojectv1alpha3.ProjectStatus{
			Git: tideprojectv1alpha3.GitStatus{
				BranchName: "tide/run-test-1",
			},
		},
	}
}

// buildPlanReconcilerForWaveInteg builds a PlanReconciler that operates on a
// fake client with the field indexer for taskPlanRefIndexKey wired.
func buildPlanReconcilerForWaveInteg(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) (*PlanReconciler, client.Client) {
	t.Helper()
	fc := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(
			&tideprojectv1alpha3.Plan{},
			&tideprojectv1alpha3.Task{},
			&batchv1.Job{},
			&tideprojectv1alpha3.Project{},
		).
		WithIndex(&tideprojectv1alpha3.Task{}, taskPlanRefIndexKey, func(obj client.Object) []string {
			task := obj.(*tideprojectv1alpha3.Task) //nolint:forcetypeassert
			return []string{task.Spec.PlanRef}
		}).
		Build()
	r := &PlanReconciler{
		Client: fc,
		Scheme: scheme,
		Deps: PlannerReconcilerDeps{
			Dispatcher:     &stubDispatcher{},
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		},
	}
	return r, fc
}

// makeWaveIntegTask creates a Task with the given planRef, UID, and phase.
// All Tasks in a Plan are executor tasks — no Role field exists on TaskSpec.
// Phase 34: labeled tideproject.k8s/project=projectName so
// succeededTaskBranches (the D-03/D-07 cumulative-set helper) can find it —
// the cumulative set is now a project-wide List, not a wave-local collection.
func makeWaveIntegTask(name, uid, planRef, phase, projectName string, dependsOn []string) *tideprojectv1alpha3.Task {
	task := &tideprojectv1alpha3.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID(uid),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec: tideprojectv1alpha3.TaskSpec{
			PlanRef:             planRef,
			DependsOn:           dependsOn,
			PromptPath:          "envelopes/test/" + name + ".json",
			FilesTouched:        []string{"src/" + name + ".go"},
			DeclaredOutputPaths: []string{"artifacts/" + name + ".txt"},
		},
		Status: tideprojectv1alpha3.TaskStatus{
			Phase: phase,
		},
	}
	return task
}

// reconcileWaveIntegPlan drives reconcileWaveMaterialization on the plan.
func reconcileWaveIntegPlan(t *testing.T, r *PlanReconciler, planName string) (reconcile.Result, error) {
	t.Helper()
	// First re-fetch the plan so we have a fresh copy.
	var plan tideprojectv1alpha3.Plan
	if err := r.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &plan); err != nil {
		return reconcile.Result{}, fmt.Errorf("get plan: %w", err)
	}
	return r.reconcileWaveMaterialization(context.Background(), &plan)
}

// listWaveIntegJobs lists all batch Jobs in the default namespace.
func listWaveIntegJobs(t *testing.T, fc client.Client) []batchv1.Job {
	t.Helper()
	var jobs batchv1.JobList
	if err := fc.List(context.Background(), &jobs, client.InNamespace("default")); err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	return jobs.Items
}

// findIntegrationJobForWave finds the integration job for a given wave index and plan UID.
func findIntegrationJobForWave(jobs []batchv1.Job, planUID types.UID, waveIdx int) *batchv1.Job {
	jobName := fmt.Sprintf("tide-push-wave-%s-%d", planUID, waveIdx)
	for i := range jobs {
		if jobs[i].Name == jobName {
			return &jobs[i]
		}
	}
	return nil
}

// TestPlanReconcilerPerWaveIntegration exercises the per-wave integration gate
// across five reconcile steps (steps a through e per the plan behavior block).
func TestPlanReconcilerPerWaveIntegration(t *testing.T) {
	const planName = "wave-integ-plan"
	const projectName = "wave-integ-proj"
	scheme := fakeSchemeForWaveInteg(t)

	// Project with git config — needed by triggerWaveIntegrationJob.
	proj := waveIntegProject(t, projectName)
	proj.Status.Git.BranchName = "tide/run-wave-integ-1"

	// Plan with two waves: wave-0 (task1, task2, no deps) and wave-1 (task3 depends on task1).
	// Label tideproject.k8s/project allows resolveProjectForPlan fast-path.
	plan := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			UID:       types.UID("plan-uid-1"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec: tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
		Status: tideprojectv1alpha3.PlanStatus{
			ValidationState: "Validated",
		},
	}

	// Wave-0 tasks: both Succeeded.
	task1 := makeWaveIntegTask("task1", "uid-task1", planName, "Succeeded", projectName, nil)
	task2 := makeWaveIntegTask("task2", "uid-task2", planName, "Succeeded", projectName, nil)
	// Wave-1 task: depends on task1, not yet Succeeded.
	task3 := makeWaveIntegTask("task3", "uid-task3", planName, "Running", projectName, []string{"task1"})

	r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1, task2, task3)

	// Patch Project status (WithStatusSubresource requires Status().Update for status).
	var projGot tideprojectv1alpha3.Project
	if err := fc.Get(context.Background(), types.NamespacedName{Name: projectName, Namespace: "default"}, &projGot); err != nil {
		t.Fatalf("get project: %v", err)
	}
	projPatch := client.MergeFrom(projGot.DeepCopy())
	projGot.Status.Git.BranchName = "tide/run-wave-integ-1"
	if err := fc.Status().Patch(context.Background(), &projGot, projPatch); err != nil {
		t.Fatalf("patch project status: %v", err)
	}

	planUID := plan.UID
	integJobName := fmt.Sprintf("tide-push-wave-%s-1", planUID)

	// --- Step (a): Job dispatch ---
	// With wave-0 all Succeeded and no integration Job, reconcile should dispatch
	// the integration Job for wave-0 (wave index 1) and NOT stamp IntegratedThroughWave.
	result, err := reconcileWaveIntegPlan(t, r, planName)
	if err != nil {
		t.Fatalf("step (a) reconcile: %v", err)
	}

	jobs := listWaveIntegJobs(t, fc)
	integJob := findIntegrationJobForWave(jobs, planUID, 1)
	if integJob == nil {
		t.Fatalf("step (a): integration job %q not found; got jobs: %v", integJobName, jobNames(jobs))
	}

	// Verify --integrate-task-branches contains wave-0 branch names.
	integJobArgs := integJob.Spec.Template.Spec.Containers[0].Args
	joinedArgs := strings.Join(integJobArgs, " ")
	branch1 := pkggit.TaskBranchName("uid-task1")
	branch2 := pkggit.TaskBranchName("uid-task2")
	if !strings.Contains(joinedArgs, branch1) || !strings.Contains(joinedArgs, branch2) {
		t.Errorf("step (a): integration job args missing task branches; args=%s, want %s and %s",
			joinedArgs, branch1, branch2)
	}

	// IntegratedThroughWave must still be 0 (Job dispatched, not yet Succeeded).
	var freshPlan tideprojectv1alpha3.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &freshPlan); err != nil {
		t.Fatalf("step (a) get plan: %v", err)
	}
	if freshPlan.Status.IntegratedThroughWave != 0 {
		t.Errorf("step (a): IntegratedThroughWave = %d, want 0 (Job dispatched but not Succeeded yet)",
			freshPlan.Status.IntegratedThroughWave)
	}

	// Reconcile must have returned a requeue (waiting for Job to complete).
	if result.RequeueAfter == 0 {
		t.Errorf("step (a): expected RequeueAfter > 0 after dispatching integration Job; got %v", result)
	}

	// --- Step (b): Pending guard ---
	// Job is still pending (Succeeded=0, Failed=0). Reconcile must return
	// requeue and must NOT dispatch wave-1 tasks.
	result, err = reconcileWaveIntegPlan(t, r, planName)
	if err != nil {
		t.Fatalf("step (b) reconcile: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Errorf("step (b): expected RequeueAfter > 0 while integration Job is pending; got %v", result)
	}
	// IntegratedThroughWave still 0.
	if err := fc.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &freshPlan); err != nil {
		t.Fatalf("step (b) get plan: %v", err)
	}
	if freshPlan.Status.IntegratedThroughWave != 0 {
		t.Errorf("step (b): IntegratedThroughWave = %d, want 0", freshPlan.Status.IntegratedThroughWave)
	}

	// --- Step (c): Integration complete ---
	// Set Job to terminal-Succeeded (Status.Succeeded=1 AND the JobComplete
	// condition — Phase 34's D-02 gitWriterInFlightCount gate keys off
	// isJobTerminal, which reads Job CONDITIONS, not the raw counts alone;
	// a real cluster always sets both together, so tests must too).
	if err := makeFakeJobTerminal(context.Background(), fc, integJobName, "default", true); err != nil {
		t.Fatalf("step (c) mark integration job terminal-succeeded: %v", err)
	}

	_, err = reconcileWaveIntegPlan(t, r, planName)
	if err != nil {
		t.Fatalf("step (c) reconcile: %v", err)
	}

	// IntegratedThroughWave must now be 1.
	if err := fc.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &freshPlan); err != nil {
		t.Fatalf("step (c) get plan: %v", err)
	}
	if freshPlan.Status.IntegratedThroughWave != 1 {
		t.Errorf("step (c): IntegratedThroughWave = %d, want 1", freshPlan.Status.IntegratedThroughWave)
	}

	// --- Step (d): Idempotency ---
	// With IntegratedThroughWave=1, reconcile again. No new integration job for
	// wave-1 should be created (count stays at 1).
	_, err = reconcileWaveIntegPlan(t, r, planName)
	if err != nil {
		t.Fatalf("step (d) reconcile: %v", err)
	}
	jobsAfterD := listWaveIntegJobs(t, fc)
	waveOneJobs := 0
	for _, j := range jobsAfterD {
		if strings.HasPrefix(j.Name, "tide-push-wave-") {
			waveOneJobs++
		}
	}
	if waveOneJobs != 1 {
		t.Errorf("step (d): expected exactly 1 wave integration job, got %d (idempotency violation)", waveOneJobs)
	}

	// --- Step (e): Permanently-failed integration Job ---
	// Reset to a fresh plan with wave-0 all-Succeeded and no integration Job.
	const plan2Name = "wave-integ-plan-e"
	plan2 := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      plan2Name,
			Namespace: "default",
			UID:       types.UID("plan-uid-2"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec: tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-2"},
		Status: tideprojectv1alpha3.PlanStatus{
			ValidationState: "Validated",
		},
	}
	task4 := makeWaveIntegTask("task4-e", "uid-task4", plan2Name, "Succeeded", projectName, nil)
	task5 := makeWaveIntegTask("task5-e", "uid-task5", plan2Name, "Running", projectName, []string{"task4-e"})

	if err := fc.Create(context.Background(), plan2); err != nil {
		t.Fatalf("step (e) create plan2: %v", err)
	}
	if err := fc.Create(context.Background(), task4); err != nil {
		t.Fatalf("step (e) create task4: %v", err)
	}
	if err := fc.Create(context.Background(), task5); err != nil {
		t.Fatalf("step (e) create task5: %v", err)
	}
	// Patch plan2 status to Validated (WithStatusSubresource strips status from Create).
	var pp2 tideprojectv1alpha3.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: plan2Name, Namespace: "default"}, &pp2); err != nil {
		t.Fatalf("step (e) get plan2: %v", err)
	}
	pp2p := client.MergeFrom(pp2.DeepCopy())
	pp2.Status.ValidationState = "Validated"
	if err := fc.Status().Patch(context.Background(), &pp2, pp2p); err != nil {
		t.Fatalf("step (e) patch plan2 status: %v", err)
	}
	// Patch task4 status to Succeeded.
	var t4 tideprojectv1alpha3.Task
	if err := fc.Get(context.Background(), types.NamespacedName{Name: "task4-e", Namespace: "default"}, &t4); err != nil {
		t.Fatalf("step (e) get task4: %v", err)
	}
	t4p := client.MergeFrom(t4.DeepCopy())
	t4.Status.Phase = "Succeeded"
	if err := fc.Status().Patch(context.Background(), &t4, t4p); err != nil {
		t.Fatalf("step (e) patch task4 status: %v", err)
	}

	// Step (e-i): dispatch the integration Job.
	_, err = reconcileWaveIntegPlan(t, r, plan2Name)
	if err != nil {
		t.Fatalf("step (e-i) reconcile: %v", err)
	}
	plan2UID := plan2.UID
	integJobName2 := fmt.Sprintf("tide-push-wave-%s-1", plan2UID)
	jobs2 := listWaveIntegJobs(t, fc)
	if findIntegrationJobForWave(jobs2, plan2UID, 1) == nil {
		t.Fatalf("step (e-i): integration job %q not dispatched", integJobName2)
	}

	// Step (e-ii): Phase 34 D-04 bounded retry — a wave-integration Job
	// failure no longer fails the Plan on the first observation. It rides
	// the bounded-retry state machine (Plan.Status.WaveIntegration.Attempts,
	// capped at maxWaveIntegrationAttempts) exactly like the #13b
	// boundary-push machine. Drive maxWaveIntegrationAttempts-1 failed
	// attempts and assert the Plan stays Running (not Failed) with the
	// Attempts counter advancing; the final attempt crosses the cap and
	// fails the Plan.
	for attempt := 1; attempt <= maxWaveIntegrationAttempts; attempt++ {
		// Mark the current integration Job terminal-Failed. No envelope pod
		// exists in this fake-client test (no pods at all), so
		// readJobPushEnvelope returns (zero, false) → classified as a
		// transient failure (lastError="envelope-unreadable"), riding the
		// bounded-retry default rather than the D-09 conflict path.
		if err := makeFakeJobTerminal(context.Background(), fc, integJobName2, "default", false); err != nil {
			t.Fatalf("step (e) attempt %d: mark integration job terminal-failed: %v", attempt, err)
		}

		result, err = reconcileWaveIntegPlan(t, r, plan2Name)
		if err != nil {
			t.Fatalf("step (e) attempt %d reconcile: %v", attempt, err)
		}

		var plan2Fresh tideprojectv1alpha3.Plan
		if err := fc.Get(context.Background(), types.NamespacedName{Name: plan2Name, Namespace: "default"}, &plan2Fresh); err != nil {
			t.Fatalf("step (e) attempt %d get plan2: %v", attempt, err)
		}
		if plan2Fresh.Status.WaveIntegration.Attempts != int32(attempt) { //nolint:gosec // attempt is a small loop counter
			t.Errorf("step (e) attempt %d: WaveIntegration.Attempts = %d, want %d",
				attempt, plan2Fresh.Status.WaveIntegration.Attempts, attempt)
		}

		if attempt < maxWaveIntegrationAttempts {
			// Below cap: Plan must stay non-terminal and the failed Job must
			// have been deleted (Background propagation) so a fresh one can
			// be dispatched once the backoff window has passed.
			if plan2Fresh.Status.Phase == "Failed" {
				t.Fatalf("step (e) attempt %d: Plan prematurely Failed before the %d-attempt cap",
					attempt, maxWaveIntegrationAttempts)
			}
			if result.RequeueAfter == 0 {
				t.Errorf("step (e) attempt %d: expected RequeueAfter > 0 (bounded retry backoff)", attempt)
			}
			// The LastAttemptTime fence blocks an immediate re-dispatch (the
			// Job DELETE event would otherwise re-enqueue with zero delay).
			// Simulate the backoff window passing by rewinding the stamp past
			// the max backoff, then re-dispatch for the next iteration.
			rewound := metav1.NewTime(time.Now().Add(-16 * time.Minute))
			rp := client.MergeFrom(plan2Fresh.DeepCopy())
			plan2Fresh.Status.WaveIntegration.LastAttemptTime = &rewound
			if err := fc.Status().Patch(context.Background(), &plan2Fresh, rp); err != nil {
				t.Fatalf("step (e) attempt %d rewind LastAttemptTime: %v", attempt, err)
			}
			if _, err := reconcileWaveIntegPlan(t, r, plan2Name); err != nil {
				t.Fatalf("step (e) attempt %d re-dispatch reconcile: %v", attempt, err)
			}
			continue
		}

		// At the cap: Plan must now be terminal Failed with
		// ReasonWaveIntegrationFailed, and no further requeue (no livelock).
		if plan2Fresh.Status.Phase != "Failed" {
			t.Errorf("step (e): Plan.Status.Phase = %q, want \"Failed\" after %d attempts",
				plan2Fresh.Status.Phase, maxWaveIntegrationAttempts)
		}
		foundFailedCondition := false
		for _, cond := range plan2Fresh.Status.Conditions {
			if cond.Type == tideprojectv1alpha3.ConditionFailed &&
				cond.Reason == tideprojectv1alpha3.ReasonWaveIntegrationFailed {
				foundFailedCondition = true
				break
			}
		}
		if !foundFailedCondition {
			t.Errorf("step (e): ConditionFailed with Reason %q not found; conditions: %v",
				tideprojectv1alpha3.ReasonWaveIntegrationFailed, plan2Fresh.Status.Conditions)
		}
		if result.RequeueAfter != 0 {
			t.Errorf("step (e): RequeueAfter = %v, want 0 (permanently failed → no livelock)",
				result.RequeueAfter)
		}
	}

	// Assert: no wave-1 tasks/jobs for plan2 were ever dispatched.
	jobs2After := listWaveIntegJobs(t, fc)
	for _, j := range jobs2After {
		// The only jobs should be integration jobs; no executor job for task5-e.
		if strings.Contains(j.Name, "tide-task") || strings.Contains(j.Name, "task5") {
			t.Errorf("step (e): unexpected executor job dispatched: %s", j.Name)
		}
	}
}

// jobNames returns a list of job names for diagnostic output.
func jobNames(jobs []batchv1.Job) []string {
	names := make([]string, len(jobs))
	for i, j := range jobs {
		names[i] = j.Name
	}
	return names
}

// ---------- Phase 34 (INTEG-01/D-02/D-09/D-10): additional wave-integration tests ----------

// TestPlanReconcilerSingleWaveDispatchesIntegrationJob is the cheapest
// INTEG-01 regression: a Plan with exactly ONE wave (no dependencies among
// its tasks) must dispatch a wave-integration Job for that only wave. Before
// the Phase 34 fix, the wave-boundary loop ran `k < len(layers)-1`, so a
// single-wave plan (len(layers)==1) iterated ZERO times and integrated
// nothing — the cheapest RED repro of the last-wave skip.
func TestPlanReconcilerSingleWaveDispatchesIntegrationJob(t *testing.T) {
	const planName = "single-wave-plan"
	const projectName = "single-wave-proj"
	scheme := fakeSchemeForWaveInteg(t)

	proj := waveIntegProject(t, projectName)
	plan := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			UID:       types.UID("plan-uid-single-wave"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
		Status: tideprojectv1alpha3.PlanStatus{ValidationState: "Validated"},
	}
	// Two tasks, NO dependsOn — a single Kahn wave.
	task1 := makeWaveIntegTask("sw-task1", "uid-sw-task1", planName, "Succeeded", projectName, nil)
	task2 := makeWaveIntegTask("sw-task2", "uid-sw-task2", planName, "Succeeded", projectName, nil)

	r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1, task2)

	if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	jobs := listWaveIntegJobs(t, fc)
	if findIntegrationJobForWave(jobs, plan.UID, 1) == nil {
		t.Fatalf("single-wave plan did not dispatch a wave-integration Job (INTEG-01 last-wave skip regressed); jobs: %v",
			jobNames(jobs))
	}
}

// TestPlanReconcilerPauseBetweenWavesFinalWaveStillIntegrates pins the
// gated-project INTEG-01 contract: the wave-approved-<N> label range is
// [1, len(layers)-1] (maybePauseForWaveApprove only pauses BETWEEN waves), so
// the FINAL boundary can never be operator-approved — it must integrate
// without requiring an approval label, for single-wave plans (waveNum 1,
// nothing is ever stampable) and multi-wave plans (waveNum len(layers))
// alike. Inter-wave boundaries stay gated: an unapproved non-final boundary
// must NOT dispatch its integration Job.
func TestPlanReconcilerPauseBetweenWavesFinalWaveStillIntegrates(t *testing.T) {
	t.Run("single-wave plan integrates with no approval label", func(t *testing.T) {
		const planName = "paused-single-wave-plan"
		const projectName = "paused-single-wave-proj"
		scheme := fakeSchemeForWaveInteg(t)

		proj := waveIntegProject(t, projectName)
		proj.Spec.Gates.PauseBetweenWaves = true
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      planName,
				Namespace: "default",
				UID:       types.UID("plan-uid-paused-single"),
				Labels:    map[string]string{"tideproject.k8s/project": projectName},
			},
			Spec:   tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
			Status: tideprojectv1alpha3.PlanStatus{ValidationState: "Validated"},
		}
		task1 := makeWaveIntegTask("psw-task1", "uid-psw-task1", planName, "Succeeded", projectName, nil)
		task2 := makeWaveIntegTask("psw-task2", "uid-psw-task2", planName, "Succeeded", projectName, nil)

		r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1, task2)

		if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
			t.Fatalf("reconcile: %v", err)
		}

		jobs := listWaveIntegJobs(t, fc)
		if findIntegrationJobForWave(jobs, plan.UID, 1) == nil {
			t.Fatalf("PauseBetweenWaves single-wave plan never integrated: wave-approved-1 is unstampable for a final boundary, so the gate must exempt it; jobs: %v",
				jobNames(jobs))
		}
	})

	t.Run("inter-wave boundary stays gated without approval", func(t *testing.T) {
		const planName = "paused-two-wave-plan"
		const projectName = "paused-two-wave-proj"
		scheme := fakeSchemeForWaveInteg(t)

		proj := waveIntegProject(t, projectName)
		proj.Spec.Gates.PauseBetweenWaves = true
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      planName,
				Namespace: "default",
				UID:       types.UID("plan-uid-paused-two"),
				Labels:    map[string]string{"tideproject.k8s/project": projectName},
			},
			Spec:   tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
			Status: tideprojectv1alpha3.PlanStatus{ValidationState: "Validated"},
		}
		// Wave 1: ptw-task1 Succeeded. Wave 2: ptw-task2 depends on ptw-task1.
		task1 := makeWaveIntegTask("ptw-task1", "uid-ptw-task1", planName, "Succeeded", projectName, nil)
		task2 := makeWaveIntegTask("ptw-task2", "uid-ptw-task2", planName, "Pending", projectName, []string{"ptw-task1"})

		r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1, task2)

		if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
			t.Fatalf("reconcile: %v", err)
		}

		jobs := listWaveIntegJobs(t, fc)
		if findIntegrationJobForWave(jobs, plan.UID, 1) != nil {
			t.Fatalf("unapproved INTER-wave boundary dispatched its integration Job — the operator hold must still gate non-final boundaries")
		}
	})
}

// TestPlanReconcilerFinalWaveIntegratesAndGatesSucceeded is the 2-wave
// INTEG-01 regression: BOTH wave boundaries (k=0 AND k=1, i.e. waveNum 1 AND
// 2) must dispatch integration Jobs — including the FINAL one, which the
// pre-fix `k < len(layers)-1` loop bound always skipped. Also asserts the
// Pitfall-6 consequence: Plan does not stamp Succeeded until the final-wave
// integration Job completes.
func TestPlanReconcilerFinalWaveIntegratesAndGatesSucceeded(t *testing.T) {
	const planName = "final-wave-plan"
	const projectName = "final-wave-proj"
	scheme := fakeSchemeForWaveInteg(t)

	proj := waveIntegProject(t, projectName)
	plan := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			UID:       types.UID("plan-uid-final-wave"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
		Status: tideprojectv1alpha3.PlanStatus{ValidationState: "Validated"},
	}
	// wave-0: task1 (no deps). wave-1 (FINAL): task2 depends on task1.
	// Owner refs are required for gates.BoundaryDetected(plan, "Task") to
	// count these as the Plan's children when checking Plan=Succeeded.
	trueVal := true
	ownerRef := metav1.OwnerReference{
		APIVersion:         tideprojectv1alpha3.GroupVersion.String(),
		Kind:               "Plan",
		Name:               plan.Name,
		UID:                plan.UID,
		Controller:         &trueVal,
		BlockOwnerDeletion: &trueVal,
	}
	task1 := makeWaveIntegTask("fw-task1", "uid-fw-task1", planName, "Succeeded", projectName, nil)
	task1.OwnerReferences = []metav1.OwnerReference{ownerRef}
	task2 := makeWaveIntegTask("fw-task2", "uid-fw-task2", planName, "Succeeded", projectName, []string{"fw-task1"})
	task2.OwnerReferences = []metav1.OwnerReference{ownerRef}

	r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1, task2)

	// Reconcile 1: dispatches wave-0's integration Job (waveNum=1).
	if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
		t.Fatalf("reconcile 1: %v", err)
	}
	jobs := listWaveIntegJobs(t, fc)
	wave1Job := findIntegrationJobForWave(jobs, plan.UID, 1)
	if wave1Job == nil {
		t.Fatalf("wave-0 integration job not dispatched; jobs: %v", jobNames(jobs))
	}
	// Mark wave-0's Job terminal-succeeded.
	if err := makeFakeJobTerminal(context.Background(), fc, wave1Job.Name, "default", true); err != nil {
		t.Fatalf("mark wave-0 job succeeded: %v", err)
	}

	// Reconcile 2: stamps IntegratedThroughWave=1, then (RESPONSIBILITY B for
	// k=1, the FINAL boundary) dispatches wave-1's integration Job.
	if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
		t.Fatalf("reconcile 2: %v", err)
	}
	jobs = listWaveIntegJobs(t, fc)
	wave2Job := findIntegrationJobForWave(jobs, plan.UID, 2)
	if wave2Job == nil {
		t.Fatalf("FINAL wave (wave-1, waveNum=2) integration job not dispatched — INTEG-01 last-wave skip regressed; jobs: %v",
			jobNames(jobs))
	}

	// Plan must NOT be Succeeded yet — the final-wave integration Job is
	// still running (Pitfall 6: Plan=Succeeded now implies all waves,
	// including the last, are integrated).
	var planMid tideprojectv1alpha3.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &planMid); err != nil {
		t.Fatalf("get plan mid: %v", err)
	}
	if planMid.Status.Phase == "Succeeded" {
		t.Fatalf("Plan stamped Succeeded before the final-wave integration Job completed (Pitfall 6 ordering violated)")
	}

	// Complete the final wave's Job and reconcile again — NOW Plan may
	// stamp Succeeded (all owned Tasks are Succeeded and both waves are
	// integrated).
	if err := makeFakeJobTerminal(context.Background(), fc, wave2Job.Name, "default", true); err != nil {
		t.Fatalf("mark wave-1 job succeeded: %v", err)
	}
	if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
		t.Fatalf("reconcile 3: %v", err)
	}
	var planFinal tideprojectv1alpha3.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &planFinal); err != nil {
		t.Fatalf("get plan final: %v", err)
	}
	if planFinal.Status.IntegratedThroughWave != 2 {
		t.Errorf("IntegratedThroughWave = %d, want 2 (both waves integrated)", planFinal.Status.IntegratedThroughWave)
	}
	if planFinal.Status.Phase != "Succeeded" {
		t.Errorf("Plan.Status.Phase = %q, want Succeeded once both waves are integrated and all Tasks Succeeded", planFinal.Status.Phase)
	}
}

// TestPlanReconcilerWaveDispatchGatedByGitWriterBusy proves the D-02
// single-flight gate applies at RESPONSIBILITY B: with another git-writer
// Job already in flight for the same Project, the Plan reconciler must
// requeue instead of creating a second run-branch-writer Job.
func TestPlanReconcilerWaveDispatchGatedByGitWriterBusy(t *testing.T) {
	const planName = "busy-gate-plan"
	const projectName = "busy-gate-proj"
	scheme := fakeSchemeForWaveInteg(t)

	proj := waveIntegProject(t, projectName)
	plan := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			UID:       types.UID("plan-uid-busy-gate"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
		Status: tideprojectv1alpha3.PlanStatus{ValidationState: "Validated"},
	}
	task1 := makeWaveIntegTask("busy-task1", "uid-busy-task1", planName, "Succeeded", projectName, nil)

	// A DIFFERENT, non-terminal git-writer Job already in flight for the
	// same project (e.g. a boundary push mid-flight).
	otherWriter := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tide-push-some-other-writer",
			Namespace: "default",
			Labels: map[string]string{
				gitWriterRoleLabelKey:     gitWriterRoleLabelValue,
				"tideproject.k8s/project": projectName,
			},
		},
	}

	r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1, otherWriter)

	result, err := reconcileWaveIntegPlan(t, r, planName)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter > 0 while another git-writer Job is in flight (D-02 gate)")
	}
	jobs := listWaveIntegJobs(t, fc)
	if findIntegrationJobForWave(jobs, plan.UID, 1) != nil {
		t.Errorf("wave-integration Job created while another git-writer Job was in flight — D-02 gate did not block dispatch")
	}
}

// TestPlanReconcilerWaveConflictFailsPlanImmediately verifies D-09/D-10: a
// wave-integration Job whose envelope reason is "merge-conflict" fails the
// Plan IMMEDIATELY (ReasonMergeConflict) — no retry budget burned, distinct
// from the transient bounded-retry path.
func TestPlanReconcilerWaveConflictFailsPlanImmediately(t *testing.T) {
	const planName = "conflict-plan"
	const projectName = "conflict-proj"
	scheme := fakeSchemeForWaveInteg(t)

	proj := waveIntegProject(t, projectName)
	plan := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			UID:       types.UID("plan-uid-conflict"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
		Status: tideprojectv1alpha3.PlanStatus{ValidationState: "Validated"},
	}
	taskA := makeWaveIntegTask("conf-taskA", "uid-conf-taskA", planName, "Succeeded", projectName, nil)
	taskB := makeWaveIntegTask("conf-taskB", "uid-conf-taskB", planName, "Succeeded", projectName, nil)

	r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, taskA, taskB)

	if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
		t.Fatalf("dispatch reconcile: %v", err)
	}
	integJobName := fmt.Sprintf("tide-push-wave-%s-1", plan.UID)

	// Mark the Job terminal-Failed AND write a merge-conflict envelope pod
	// (mirrors the 34-02 test precedent: a Pod labeled job-name=<job> with a
	// Terminated container carrying the JSON envelope).
	if err := makeFakeJobTerminal(context.Background(), fc, integJobName, "default", false); err != nil {
		t.Fatalf("mark job failed: %v", err)
	}
	envelopeJSON := `{"apiVersion":"dispatch.tideproject.k8s/v1alpha1","kind":"PushResult","reason":"merge-conflict","conflictBranch":"tide/wt-uid-conf-taskB","exitCode":15}`
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      integJobName + "-pod",
			Namespace: "default",
			Labels:    map[string]string{"job-name": integJobName},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{{Name: "push", Image: "ghcr.io/jsquirrelz/tide-push:test"}},
		},
	}
	if err := fc.Create(context.Background(), pod); err != nil {
		t.Fatalf("create envelope pod: %v", err)
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{
		Name: "push",
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: 15, Reason: "Error", Message: envelopeJSON},
		},
	}}
	if err := fc.Status().Update(context.Background(), pod); err != nil {
		t.Fatalf("patch envelope pod status: %v", err)
	}

	result, err := reconcileWaveIntegPlan(t, r, planName)
	if err != nil {
		t.Fatalf("classify reconcile: %v", err)
	}

	var planFresh tideprojectv1alpha3.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &planFresh); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if planFresh.Status.Phase != "Failed" {
		t.Fatalf("Plan.Status.Phase = %q, want Failed immediately on merge-conflict (no retry)", planFresh.Status.Phase)
	}
	if planFresh.Status.WaveIntegration.Attempts != 0 {
		t.Errorf("WaveIntegration.Attempts = %d, want 0 — a conflict must not burn the bounded-retry budget",
			planFresh.Status.WaveIntegration.Attempts)
	}
	found := false
	for _, cond := range planFresh.Status.Conditions {
		if cond.Type == tideprojectv1alpha3.ConditionFailed && cond.Reason == tideprojectv1alpha3.ReasonMergeConflict {
			found = true
			if !strings.Contains(cond.Message, "tide/wt-uid-conf-taskB") {
				t.Errorf("condition message %q does not name the conflicting branch", cond.Message)
			}
		}
	}
	if !found {
		t.Errorf("no Failed condition with Reason=MergeConflict found; conditions: %v", planFresh.Status.Conditions)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0 (conflict fails immediately, no retry)", result.RequeueAfter)
	}
}

// TestPlanReconcilerWaveRetryHonorsBackoffFence pins the capped-backoff
// contract between wave-retry attempts: handleWaveIntegrationFailure deletes
// the failed Job and returns RequeueAfter=boundaryPushRequeue(Attempts), but
// the Job DELETE event re-enqueues the Plan immediately via Owns(Job) — so
// the delay must be enforced by a LastAttemptTime fence at dispatch, not by
// the requeue alone. A same-wave retry inside the window must wait; one past
// the window (and any new wave) dispatches.
func TestPlanReconcilerWaveRetryHonorsBackoffFence(t *testing.T) {
	buildFixture := func(t *testing.T, planName, projectName string, lastAttempt time.Time, attempts int32) (*PlanReconciler, client.Client, types.UID) {
		t.Helper()
		scheme := fakeSchemeForWaveInteg(t)
		proj := waveIntegProject(t, projectName)
		at := metav1.NewTime(lastAttempt)
		plan := &tideprojectv1alpha3.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      planName,
				Namespace: "default",
				UID:       types.UID("plan-uid-" + planName),
				Labels:    map[string]string{"tideproject.k8s/project": projectName},
			},
			Spec: tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
			Status: tideprojectv1alpha3.PlanStatus{
				ValidationState: "Validated",
				WaveIntegration: tideprojectv1alpha3.WaveIntegrationStatus{
					Wave: 1, Attempts: attempts, LastAttemptTime: &at, LastError: "integration-failed",
				},
			},
		}
		task1 := makeWaveIntegTask(planName+"-t1", "uid-"+planName+"-t1", planName, "Succeeded", projectName, nil)
		r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1)
		return r, fc, plan.UID
	}

	t.Run("inside the window: no dispatch, requeue for the remainder", func(t *testing.T) {
		r, fc, uid := buildFixture(t, "fence-hot-plan", "fence-hot-proj", time.Now(), 2)
		result, err := reconcileWaveIntegPlan(t, r, "fence-hot-plan")
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if findIntegrationJobForWave(listWaveIntegJobs(t, fc), uid, 1) != nil {
			t.Fatalf("retry dispatched immediately after a failure — the capped backoff (attempt 2 → %v) was bypassed",
				boundaryPushRequeue(2))
		}
		if result.RequeueAfter == 0 {
			t.Errorf("expected RequeueAfter > 0 while inside the backoff window")
		}
	})

	t.Run("past the window: dispatches", func(t *testing.T) {
		r, fc, uid := buildFixture(t, "fence-cold-plan", "fence-cold-proj", time.Now().Add(-30*time.Minute), 2)
		if _, err := reconcileWaveIntegPlan(t, r, "fence-cold-plan"); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if findIntegrationJobForWave(listWaveIntegJobs(t, fc), uid, 1) == nil {
			t.Fatalf("retry past the backoff window did not dispatch")
		}
	})
}

// TestPlanReconcilerWaveJobPodFailureIsNotTerminal pins the terminal-Failed
// contract: buildPushJob sets BackoffLimit=2, and batchv1 Job.Status.Failed
// counts failed PODS — it is >0 after the first pod failure while the Job
// controller still owes retries. Only the JobFailed CONDITION marks the Job
// terminal (mirrors the project-side isJobFailed gate). A mid-retry Job must
// be treated as still-running: no Attempts burned, no Job deletion.
func TestPlanReconcilerWaveJobPodFailureIsNotTerminal(t *testing.T) {
	const planName = "podfail-plan"
	const projectName = "podfail-proj"
	scheme := fakeSchemeForWaveInteg(t)

	proj := waveIntegProject(t, projectName)
	plan := &tideprojectv1alpha3.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			UID:       types.UID("plan-uid-podfail"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec:   tideprojectv1alpha3.PlanSpec{PhaseRef: "phase-1"},
		Status: tideprojectv1alpha3.PlanStatus{ValidationState: "Validated"},
	}
	task1 := makeWaveIntegTask("pf-task1", "uid-pf-task1", planName, "Succeeded", projectName, nil)

	r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1)

	if _, err := reconcileWaveIntegPlan(t, r, planName); err != nil {
		t.Fatalf("dispatch reconcile: %v", err)
	}
	integJobName := fmt.Sprintf("tide-push-wave-%s-1", plan.UID)

	// First pod failed; NO JobFailed condition — the Job controller is still
	// retrying (BackoffLimit budget remains).
	var job batchv1.Job
	if err := fc.Get(context.Background(), types.NamespacedName{Name: integJobName, Namespace: "default"}, &job); err != nil {
		t.Fatalf("get integ job: %v", err)
	}
	job.Status.Failed = 1
	if err := fc.Status().Update(context.Background(), &job); err != nil {
		t.Fatalf("mark pod failure: %v", err)
	}

	result, err := reconcileWaveIntegPlan(t, r, planName)
	if err != nil {
		t.Fatalf("mid-retry reconcile: %v", err)
	}

	if err := fc.Get(context.Background(), types.NamespacedName{Name: integJobName, Namespace: "default"}, &job); err != nil {
		t.Fatalf("mid-retry Job was deleted while the Job controller still owed pod retries: %v", err)
	}
	var planFresh tideprojectv1alpha3.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: planName, Namespace: "default"}, &planFresh); err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if planFresh.Status.WaveIntegration.Attempts != 0 {
		t.Errorf("WaveIntegration.Attempts = %d, want 0 — a pod-level failure is not a terminal Job failure",
			planFresh.Status.WaveIntegration.Attempts)
	}
	if planFresh.Status.Phase == "Failed" {
		t.Errorf("Plan failed on a non-terminal (mid-retry) wave Job")
	}
	if result.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter > 0 while the wave Job is still retrying pods")
	}
}
