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

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
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
func waveIntegProject(t *testing.T, name string) *tideprojectv1alpha1.Project {
	t.Helper()
	return &tideprojectv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID("proj-uid-wave-integ"),
		},
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/test.git",
			Git: &tideprojectv1alpha1.GitConfig{
				RepoURL:        "https://github.com/example/test.git",
				CredsSecretRef: "test-creds",
			},
		},
		Status: tideprojectv1alpha1.ProjectStatus{
			Git: tideprojectv1alpha1.GitStatus{
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
			&tideprojectv1alpha1.Plan{},
			&tideprojectv1alpha1.Task{},
			&batchv1.Job{},
			&tideprojectv1alpha1.Project{},
		).
		WithIndex(&tideprojectv1alpha1.Task{}, taskPlanRefIndexKey, func(obj client.Object) []string {
			task := obj.(*tideprojectv1alpha1.Task) //nolint:forcetypeassert
			return []string{task.Spec.PlanRef}
		}).
		Build()
	r := &PlanReconciler{
		Client:         fc,
		Scheme:         scheme,
		Dispatcher:     &stubDispatcher{},
		SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
		CredproxyImage: testCredproxyImage,
		SigningKey:     testSigningKey,
		TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
		HelmProviderDefaults: ProviderDefaults{
			Image: testSubagentImage,
		},
	}
	return r, fc
}

// makeWaveIntegTask creates a Task with the given planRef, UID, and phase.
// All Tasks in a Plan are executor tasks — no Role field exists on TaskSpec.
func makeWaveIntegTask(name, uid, planRef, phase string, dependsOn []string) *tideprojectv1alpha1.Task {
	task := &tideprojectv1alpha1.Task{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       types.UID(uid),
		},
		Spec: tideprojectv1alpha1.TaskSpec{
			PlanRef:             planRef,
			DependsOn:           dependsOn,
			PromptPath:          "envelopes/test/" + name + ".json",
			FilesTouched:        []string{"src/" + name + ".go"},
			DeclaredOutputPaths: []string{"artifacts/" + name + ".txt"},
		},
		Status: tideprojectv1alpha1.TaskStatus{
			Phase: phase,
		},
	}
	return task
}

// reconcileWaveIntegPlan drives reconcileWaveMaterialization on the plan.
func reconcileWaveIntegPlan(t *testing.T, r *PlanReconciler, planName string) (reconcile.Result, error) {
	t.Helper()
	// First re-fetch the plan so we have a fresh copy.
	var plan tideprojectv1alpha1.Plan
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
	plan := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      planName,
			Namespace: "default",
			UID:       types.UID("plan-uid-1"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec: tideprojectv1alpha1.PlanSpec{PhaseRef: "phase-1"},
		Status: tideprojectv1alpha1.PlanStatus{
			ValidationState: "Validated",
		},
	}

	// Wave-0 tasks: both Succeeded.
	task1 := makeWaveIntegTask("task1", "uid-task1", planName, "Succeeded", nil)
	task2 := makeWaveIntegTask("task2", "uid-task2", planName, "Succeeded", nil)
	// Wave-1 task: depends on task1, not yet Succeeded.
	task3 := makeWaveIntegTask("task3", "uid-task3", planName, "Running", []string{"task1"})

	r, fc := buildPlanReconcilerForWaveInteg(t, scheme, proj, plan, task1, task2, task3)

	// Patch Project status (WithStatusSubresource requires Status().Update for status).
	var projGot tideprojectv1alpha1.Project
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
	var freshPlan tideprojectv1alpha1.Plan
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
	// Set Job Status.Succeeded=1 to simulate completion.
	var integJobObj batchv1.Job
	if err := fc.Get(context.Background(), types.NamespacedName{Name: integJobName, Namespace: "default"}, &integJobObj); err != nil {
		t.Fatalf("step (c) get integration job: %v", err)
	}
	integJobObj.Status.Succeeded = 1
	if err := fc.Status().Update(context.Background(), &integJobObj); err != nil {
		t.Fatalf("step (c) update integration job status: %v", err)
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
	plan2 := &tideprojectv1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      plan2Name,
			Namespace: "default",
			UID:       types.UID("plan-uid-2"),
			Labels:    map[string]string{"tideproject.k8s/project": projectName},
		},
		Spec: tideprojectv1alpha1.PlanSpec{PhaseRef: "phase-2"},
		Status: tideprojectv1alpha1.PlanStatus{
			ValidationState: "Validated",
		},
	}
	task4 := makeWaveIntegTask("task4-e", "uid-task4", plan2Name, "Succeeded", nil)
	task5 := makeWaveIntegTask("task5-e", "uid-task5", plan2Name, "Running", []string{"task4-e"})

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
	var pp2 tideprojectv1alpha1.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: plan2Name, Namespace: "default"}, &pp2); err != nil {
		t.Fatalf("step (e) get plan2: %v", err)
	}
	pp2p := client.MergeFrom(pp2.DeepCopy())
	pp2.Status.ValidationState = "Validated"
	if err := fc.Status().Patch(context.Background(), &pp2, pp2p); err != nil {
		t.Fatalf("step (e) patch plan2 status: %v", err)
	}
	// Patch task4 status to Succeeded.
	var t4 tideprojectv1alpha1.Task
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

	// Step (e-ii): set the Job to permanently failed (BackoffLimit exhausted).
	var integJobObj2 batchv1.Job
	if err := fc.Get(context.Background(), types.NamespacedName{Name: integJobName2, Namespace: "default"}, &integJobObj2); err != nil {
		t.Fatalf("step (e-ii) get integration job2: %v", err)
	}
	integJobObj2.Status.Failed = 1
	integJobObj2.Status.Active = 0
	integJobObj2.Status.Succeeded = 0
	if err := fc.Status().Update(context.Background(), &integJobObj2); err != nil {
		t.Fatalf("step (e-ii) update integration job2 status: %v", err)
	}

	// Step (e-iii): reconcile — should call patchPlanFailed with WaveIntegrationFailed.
	result, err = reconcileWaveIntegPlan(t, r, plan2Name)
	if err != nil {
		t.Fatalf("step (e-iii) reconcile: %v", err)
	}

	// Assert: Plan.Status.Phase == "Failed".
	var plan2Fresh tideprojectv1alpha1.Plan
	if err := fc.Get(context.Background(), types.NamespacedName{Name: plan2Name, Namespace: "default"}, &plan2Fresh); err != nil {
		t.Fatalf("step (e-iii) get plan2: %v", err)
	}
	if plan2Fresh.Status.Phase != "Failed" {
		t.Errorf("step (e): Plan.Status.Phase = %q, want \"Failed\"", plan2Fresh.Status.Phase)
	}
	// Assert: ConditionFailed condition with Reason WaveIntegrationFailed.
	foundFailedCondition := false
	for _, cond := range plan2Fresh.Status.Conditions {
		if cond.Type == tideprojectv1alpha1.ConditionFailed &&
			cond.Reason == tideprojectv1alpha1.ReasonWaveIntegrationFailed {
			foundFailedCondition = true
			break
		}
	}
	if !foundFailedCondition {
		t.Errorf("step (e): ConditionFailed with Reason %q not found; conditions: %v",
			tideprojectv1alpha1.ReasonWaveIntegrationFailed, plan2Fresh.Status.Conditions)
	}
	// Assert: no RequeueAfter (no livelock).
	if result.RequeueAfter != 0 {
		t.Errorf("step (e): RequeueAfter = %v, want 0 (permanently failed → no livelock)",
			result.RequeueAfter)
	}
	// Assert: no wave-1 tasks/jobs for plan2 were dispatched.
	jobs2After := listWaveIntegJobs(t, fc)
	for _, j := range jobs2After {
		// The only job should be the integration job; no executor job for task5-e.
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
