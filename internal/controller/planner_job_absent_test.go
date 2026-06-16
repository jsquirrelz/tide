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

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
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

	makeProject := func(name string) *tideprojectv1alpha2.Project {
		proj := &tideprojectv1alpha2.Project{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: tideprojectv1alpha2.ProjectSpec{SchemaRevision: "v1alpha2",
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha2.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha2.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				// Auto gates so the milestone/phase succession path is reached
				// (the default gate policy is Approve, which parks at AwaitingApproval
				// before the boundary/succession code — see milestone_controller.go:474).
				Gates: tideprojectv1alpha2.Gates{Milestone: "auto", Phase: "auto"},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(name, "default", &tideprojectv1alpha2.Project{})
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
			cleanup(&tideprojectv1alpha2.Project{}, projectName)
			deleteAllJobs()
		})

		// Drive the Project straight to Running with its planner Job ABSENT — the
		// exact TTL/GC race: the Step-2 dispatch body sees Phase=Running but the
		// planner Job is gone. (The full init/clone lifecycle is exercised in
		// project_phase3_test.go; here we isolate the Step-2 NotFound branch.)
		var got tideprojectv1alpha2.Project
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &got)).To(Succeed())
		statusPatch := client.MergeFrom(got.DeepCopy())
		got.Status.Phase = tideprojectv1alpha2.PhaseRunning
		Expect(mgrClient.Status().Patch(ctx, &got, statusPatch)).To(Succeed())
		Eventually(func(g Gomega) {
			var p tideprojectv1alpha2.Project
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, &p)).To(Succeed())
			g.Expect(p.Status.Phase).To(Equal(tideprojectv1alpha2.PhaseRunning))
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
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			SharedPVCName:  pvcName,
			TidePushImage:  "ghcr.io/jsquirrelz/tide-push:test",
			ReporterImage:  testReporterImage,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: projectName, Namespace: "default"}, 5)).To(Succeed())

		expectReporterSpawned(ctx, mgrClient, string(got.UID))
	})

	It("Milestone: succession fires when the milestone planner Job is gone while Running", func() {
		const projectName = "test-proj-absent-ms"
		const milestoneName = "test-ms-absent"
		makeProject(projectName)
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha2.Milestone{})
		DeferCleanup(func() {
			cleanup(&tideprojectv1alpha2.Milestone{}, milestoneName)
			cleanup(&tideprojectv1alpha2.Project{}, projectName)
			deleteAllJobs()
		})

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			ReporterImage:  testReporterImage,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Milestone
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
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha2.Milestone{})
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: milestoneName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseName, "default", &tideprojectv1alpha2.Phase{})
		DeferCleanup(func() {
			cleanup(&tideprojectv1alpha2.Phase{}, phaseName)
			cleanup(&tideprojectv1alpha2.Milestone{}, milestoneName)
			cleanup(&tideprojectv1alpha2.Project{}, projectName)
			deleteAllJobs()
		})

		envReader := newMapEnvReader()
		r := &PhaseReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			ReporterImage:  testReporterImage,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Phase
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &got)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, "5s", "100ms").Should(Succeed())

		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{TaskUID: string(got.UID), ExitCode: 0, ChildCount: 0})

		deletePlannerJob(ctx, mgrClient, fmt.Sprintf("tide-phase-%s-1", got.UID))

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		expectReporterSpawned(ctx, mgrClient, string(got.UID))
	})

	// Second root cause (debug real-claude-authoring-path): on the Job-absent
	// fall-through path the planner's out.json may already be gone (its per-run
	// PVC artifact wiped by the clone/init re-creation loop), so the completion
	// handler's envelope re-read fails. Previously Phase/Milestone HARD-FAILED with
	// EnvelopeReadFailed even though all children Succeeded — a false Failed on a
	// genuinely-complete level. The fix makes the read non-fatal (mirroring the
	// Project level): defer to the children-based succession gate. These two specs
	// drive Job-absent + envelope-absent + all-children-Succeeded and assert the
	// level reaches Succeeded, NOT Failed.
	It("Milestone: succeeds (not fails) when planner Job AND envelope are absent but child Phase Succeeded", func() {
		const projectName = "test-proj-envabsent-ms"
		const milestoneName = "test-ms-envabsent"
		makeProject(projectName)
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha2.Milestone{})
		DeferCleanup(func() {
			cleanup(&tideprojectv1alpha2.Phase{}, "child-phase-envabsent")
			cleanup(&tideprojectv1alpha2.Milestone{}, milestoneName)
			cleanup(&tideprojectv1alpha2.Project{}, projectName)
			deleteAllJobs()
		})

		envReader := newMapEnvReader()
		r := &MilestoneReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			ReporterImage:  testReporterImage,
			// TidePushImage intentionally empty: the boundary push is skipped so the
			// spec stays focused on succession, not the push Job (boundary_push.go:79-90).
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Milestone
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &got)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, "5s", "100ms").Should(Succeed())

		// Materialize a controller-owned child Phase already Succeeded — the proof
		// of completion that the children-based gate reads.
		tru := true
		childPhase := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "child-phase-envabsent",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "tideproject.k8s/v1alpha1",
					Kind:               "Milestone",
					Name:               got.GetName(),
					UID:                got.GetUID(),
					Controller:         &tru,
					BlockOwnerDeletion: &tru,
				}},
			},
			Spec: tideprojectv1alpha2.PhaseSpec{MilestoneRef: milestoneName},
		}
		Expect(k8sClient.Create(ctx, childPhase)).To(Succeed())
		waitForCacheSync("child-phase-envabsent", "default", &tideprojectv1alpha2.Phase{})
		var cp tideprojectv1alpha2.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "child-phase-envabsent", Namespace: "default"}, &cp)).To(Succeed())
		cpPatch := client.MergeFrom(cp.DeepCopy())
		cp.Status.Phase = "Succeeded"
		Expect(mgrClient.Status().Patch(ctx, &cp, cpPatch)).To(Succeed())

		// Job ABSENT, envelope ABSENT (no SetOut → ReadOut returns an error).
		deletePlannerJob(ctx, mgrClient, fmt.Sprintf("tide-milestone-%s-1", got.UID))

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: milestoneName, Namespace: "default"}, 5)).To(Succeed())

		Eventually(func(g Gomega) {
			var after tideprojectv1alpha2.Milestone
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, &after)).To(Succeed())
			g.Expect(after.Status.Phase).To(Equal("Succeeded"),
				"Milestone must Succeed via children-based gate, not Fail on the unreadable planner envelope")
		}, "5s", "100ms").Should(Succeed())
	})

	It("Phase: succeeds (not fails) when planner Job AND envelope are absent but child Plan Succeeded", func() {
		const projectName = "test-proj-envabsent-ph"
		const milestoneName = "test-ms-envabsent-ph"
		const phaseName = "test-phase-envabsent"
		makeProject(projectName)
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha2.Milestone{})
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: milestoneName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseName, "default", &tideprojectv1alpha2.Phase{})
		DeferCleanup(func() {
			cleanup(&tideprojectv1alpha2.Plan{}, "child-plan-envabsent")
			cleanup(&tideprojectv1alpha2.Phase{}, phaseName)
			cleanup(&tideprojectv1alpha2.Milestone{}, milestoneName)
			cleanup(&tideprojectv1alpha2.Project{}, projectName)
			deleteAllJobs()
		})

		envReader := newMapEnvReader()
		r := &PhaseReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      envReader,
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			ReporterImage:  testReporterImage,
			// TidePushImage intentionally empty (see milestone spec above).
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha2.Phase
		Eventually(func(g Gomega) {
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &got)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, "5s", "100ms").Should(Succeed())

		tru := true
		childPlan := &tideprojectv1alpha2.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "child-plan-envabsent",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion:         "tideproject.k8s/v1alpha1",
					Kind:               "Phase",
					Name:               got.GetName(),
					UID:                got.GetUID(),
					Controller:         &tru,
					BlockOwnerDeletion: &tru,
				}},
			},
			Spec: tideprojectv1alpha2.PlanSpec{PhaseRef: phaseName},
		}
		Expect(k8sClient.Create(ctx, childPlan)).To(Succeed())
		waitForCacheSync("child-plan-envabsent", "default", &tideprojectv1alpha2.Plan{})
		var cpl tideprojectv1alpha2.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "child-plan-envabsent", Namespace: "default"}, &cpl)).To(Succeed())
		cplPatch := client.MergeFrom(cpl.DeepCopy())
		cpl.Status.Phase = "Succeeded"
		Expect(mgrClient.Status().Patch(ctx, &cpl, cplPatch)).To(Succeed())

		// Job ABSENT, envelope ABSENT (no SetOut → ReadOut returns an error).
		deletePlannerJob(ctx, mgrClient, fmt.Sprintf("tide-phase-%s-1", got.UID))

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		Eventually(func(g Gomega) {
			var after tideprojectv1alpha2.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &after)).To(Succeed())
			g.Expect(after.Status.Phase).To(Equal("Succeeded"),
				"Phase must Succeed via children-based gate, not Fail on the unreadable planner envelope")
		}, "5s", "100ms").Should(Succeed())
	})
})
