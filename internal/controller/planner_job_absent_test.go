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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// testReporterImage is the placeholder reporter image used to observe the
// completion side-effect (reporter Job creation) across levels.
const testReporterImage = "tide-reporter:test"

// deletePlannerJob removes the planner Job named jobName and waits for the
// delete to be observable, reproducing the TTL/GC race: the level is still
// Running but its planner Job is gone.
func deletePlannerJob(ctx context.Context, c client.Client, jobName string) {
	var job batchv1.Job
	Expect(c.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &job)).To(Succeed())
	policy := metav1.DeletePropagationBackground
	Expect(c.Delete(ctx, &job, &client.DeleteOptions{PropagationPolicy: &policy})).To(Succeed())
	Eventually(func() bool {
		gErr := c.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &batchv1.Job{})
		return apierrors.IsNotFound(gErr)
	}, "5s", "100ms").Should(BeTrue(), "planner Job must be gone to exercise the NotFound-while-Running path")
}

// expectReporterSpawned asserts the reporter Job tide-reporter-<uid> was
// created — the observable proof that the level fell through to its completion
// handler instead of parking on the absent planner Job.
func expectReporterSpawned(ctx context.Context, c client.Client, uid string) {
	reporterJobName := fmt.Sprintf("tide-reporter-%s", uid)
	Eventually(func() error {
		return c.Get(ctx, types.NamespacedName{Name: reporterJobName, Namespace: "default"}, &batchv1.Job{})
	}, "5s", "100ms").Should(Succeed(),
		"completion must fire (reporter Job spawned) when the planner Job is absent while Running")
}

// Debug (real-claude-authoring-path): when a planner Job is garbage-collected
// (TTL/owner-cascade) before the controller observes its terminal state, the
// level was still Running and the NotFound branch did `return Result{}, nil` —
// parking the level forever and stalling succession. The fix falls through to
// the completion handler (envelope lives on the PVC keyed by UID, not the Job).
// These three specs drive the Running branch with the planner Job ABSENT and
// assert completion fires (reporter Job spawned) at every planner-dispatch level.
var _ = Describe("Planner Job absent while Running (debug real-claude-authoring-path)", Label("envtest", "phase3"), func() {
	ctx := context.Background()

	makeProject := func(name string) *tideprojectv1alpha1.Project {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha1.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha1.Project{})
		return proj
	}

	cleanup := func(obj client.Object, name string) {
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, obj); err == nil {
			obj.SetFinalizers(nil)
			_ = k8sClient.Update(ctx, obj)
			_ = k8sClient.Delete(ctx, obj)
		}
	}

	deleteAllJobs := func() {
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs)
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	}

	It("Project: succession fires when the project planner Job is gone while Running", func() {
		const projectName = "test-proj-absent"
		const pvcName = "tide-projects-absent"
		makeProject(projectName)
		ensurePVC(ctx, pvcName, "default")
		DeferCleanup(func() {
			cleanup(&tideprojectv1alpha1.Project{}, projectName)
			deleteAllJobs()
		})

		// Drive the Project straight to Running with its planner Job ABSENT — the
		// exact TTL/GC race: the Step-2 dispatch body sees Phase=Running but the
		// planner Job is gone. (The full init/clone lifecycle is exercised in
		// project_phase3_test.go; here we isolate the Step-2 NotFound branch.)
		var got tideprojectv1alpha1.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
		statusPatch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = tideprojectv1alpha1.PhaseRunning
		Expect(mgrClient.Status().Patch(ctx, &got, statusPatch)).To(Succeed())
		Eventually(func(g Gomega) {
			var p tideprojectv1alpha1.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
			g.Expect(p.Status.Phase).To(Equal(tideprojectv1alpha1.PhaseRunning))
		}, "5s", "100ms").Should(Succeed())

		envReader := newMapEnvReader()
		// Tiny status the (now-absent) planner would have left on the PVC: a leaf
		// authoring with no Milestone children, which drives a clean completion.
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0, ChildCount: 0})
		r := &ProjectReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			SharedPVCName:  pvcName,
			TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
			ReporterImage:  testReporterImage,
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: projectName, Namespace: "default"}, 5)).To(Succeed())

		expectReporterSpawned(ctx, mgrClient, string(got.UID))
	})

	It("Milestone: succession fires when the milestone planner Job is gone while Running", func() {
		const projectName = "test-proj-absent-ms"
		const milestoneName = "test-ms-absent"
		makeProject(projectName)
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha1.Milestone{})
		DeferCleanup(func() {
			cleanup(&tideprojectv1alpha1.Milestone{}, milestoneName)
			cleanup(&tideprojectv1alpha1.Project{}, projectName)
			deleteAllJobs()
		})

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			ReporterImage:  testReporterImage,
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Milestone
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &got)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, "5s", "100ms").Should(Succeed())

		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0, ChildCount: 0})

		deletePlannerJob(ctx, mgrClient, fmt.Sprintf("tide-milestone-%s-1", got.UID))

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		expectReporterSpawned(ctx, mgrClient, string(got.UID))
	})

	It("Phase: succession fires when the phase planner Job is gone while Running", func() {
		const projectName = "test-proj-absent-ph"
		const milestoneName = "test-ms-absent-ph"
		const phaseName = "test-phase-absent"
		makeProject(projectName)
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha1.Milestone{})
		ph := &tideprojectv1alpha1.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: milestoneName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseName, "default", &tideprojectv1alpha1.Phase{})
		DeferCleanup(func() {
			cleanup(&tideprojectv1alpha1.Phase{}, phaseName)
			cleanup(&tideprojectv1alpha1.Milestone{}, milestoneName)
			cleanup(&tideprojectv1alpha1.Project{}, projectName)
			deleteAllJobs()
		})

		envReader := newMapEnvReader()
		r := &PhaseReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			ReporterImage:  testReporterImage,
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Phase
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &got)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, "5s", "100ms").Should(Succeed())

		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0, ChildCount: 0})

		deletePlannerJob(ctx, mgrClient, fmt.Sprintf("tide-phase-%s-1", got.UID))

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		expectReporterSpawned(ctx, mgrClient, string(got.UID))
	})
})
