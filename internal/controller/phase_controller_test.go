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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

var _ = Describe("PhaseReconciler — planner dispatch", Label("envtest", "phase3"), func() {
	const projectName = "test-proj-ph"
	const milestoneName = "test-ms-ph"
	const phaseName = "test-phase-1"
	ctx := context.Background()

	BeforeEach(func() {
		proj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: projectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent: tideprojectv1alpha1.SubagentConfig{
					Model: "claude-sonnet-4-6",
				},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
			},
		}
		Expect(k8sClient.Create(ctx, proj)).To(Succeed())
		waitForCacheSync(projectName, "default", &tideprojectv1alpha1.Project{})
		ms := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: projectName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneName, "default", &tideprojectv1alpha1.Milestone{})
	})

	AfterEach(func() {
		ph := &tideprojectv1alpha1.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha1.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: milestoneName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		proj := &tideprojectv1alpha1.Project{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: projectName, Namespace: "default"}, proj); err == nil {
			proj.Finalizers = nil
			_ = k8sClient.Update(ctx, proj)
			_ = k8sClient.Delete(ctx, proj)
		}
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs)
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	// Test 5 (plan 09-08 Defect B): Phase does NOT Succeed while its reporter has
	// not yet materialized the expected child Plans (observed < expected).
	// This is the exact race that caused Project=Complete while Plans=Running in the
	// 09-07 live acceptance: the phase_controller had NO guard and fell straight through
	// to patchPhaseSucceeded. With the ChildCount gate, the Phase must requeue (not
	// Succeed) until observed >= expected AND all Plans are Succeeded.
	It("Test 5 (09-08 Defect B): does NOT Succeed while expected Plans not yet materialized; Succeeds once they do", func() {
		const autoProjectName = "test-proj-ph-auto5"
		const autoMilestoneName = "test-ms-ph-auto5"
		const autoPhaseName = "test-phase-auto5"
		autoProj := &tideprojectv1alpha1.Project{
			ObjectMeta: metav1.ObjectMeta{Name: autoProjectName, Namespace: "default"},
			Spec: tideprojectv1alpha1.ProjectSpec{
				TargetRepo: "https://github.com/example/test.git",
				Subagent:   tideprojectv1alpha1.SubagentConfig{Model: "claude-opus-4-7"},
				Git: &tideprojectv1alpha1.GitConfig{
					RepoURL:        "https://github.com/example/test.git",
					CredsSecretRef: "test-creds",
				},
				// No gates → auto-pass so Phase can proceed without an approval gate stop.
			},
		}
		Expect(k8sClient.Create(ctx, autoProj)).To(Succeed())
		waitForCacheSync(autoProjectName, "default", &tideprojectv1alpha1.Project{})
		autoMs := &tideprojectv1alpha1.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: autoMilestoneName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.MilestoneSpec{ProjectRef: autoProjectName},
		}
		Expect(k8sClient.Create(ctx, autoMs)).To(Succeed())
		waitForCacheSync(autoMilestoneName, "default", &tideprojectv1alpha1.Milestone{})
		DeferCleanup(func() {
			cleanObj := func(obj client.Object) {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: "default"}, obj); err == nil {
					obj.SetFinalizers(nil)
					_ = k8sClient.Update(ctx, obj)
					_ = k8sClient.Delete(ctx, obj)
				}
			}
			cleanObj(&tideprojectv1alpha1.Plan{ObjectMeta: metav1.ObjectMeta{Name: "child-plan-ph5-pending", Namespace: "default"}})
			cleanObj(&tideprojectv1alpha1.Phase{ObjectMeta: metav1.ObjectMeta{Name: autoPhaseName, Namespace: "default"}})
			cleanObj(&tideprojectv1alpha1.Milestone{ObjectMeta: metav1.ObjectMeta{Name: autoMilestoneName, Namespace: "default"}})
			cleanObj(&tideprojectv1alpha1.Project{ObjectMeta: metav1.ObjectMeta{Name: autoProjectName, Namespace: "default"}})
		})

		autoPhase := &tideprojectv1alpha1.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: autoPhaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: autoMilestoneName},
		}
		Expect(k8sClient.Create(ctx, autoPhase)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &tideprojectv1alpha1.Phase{})
		}, "5s", "100ms").Should(Succeed())

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
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, 5)).To(Succeed())

		var got tideprojectv1alpha1.Phase
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &got)
		}, "5s", "100ms").Should(Succeed())

		// Set ChildCount=1: the planner authored 1 Plan child. This simulates the tiny
		// status the PodStatusEnvelopeReader would read from the termination message.
		envReader.SetOut(string(got.UID), pkgdispatch.EnvelopeOut{
			TaskUID:    string(got.UID),
			ExitCode:   0,
			ChildCount: 1,
		})

		jobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
		Expect(makeFakeJobTerminal(ctx, mgrClient, jobName, "default", true)).To(Succeed())

		// Gate assertion 1: with expected=1 and 0 Plans materialized yet, the Phase
		// must requeue — NOT Succeed. This is the premature-succession race fix.
		Eventually(func(g Gomega) {
			res, rerr := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: autoPhaseName, Namespace: "default"}})
			g.Expect(rerr).NotTo(HaveOccurred())
			g.Expect(res.RequeueAfter).To(BeNumerically(">", 0),
				"Phase must requeue while observed Plans < expected (premature-succession guard)")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		var afterNoPlan tideprojectv1alpha1.Phase
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &afterNoPlan)).To(Succeed())
		Expect(afterNoPlan.Status.Phase).NotTo(Equal("Succeeded"),
			"Phase must not Succeed when no Plans materialized yet (Defect B regression guard)")

		// Simulate reporter materializing the child Plan (still Pending).
		tru := true
		childPlan := &tideprojectv1alpha1.Plan{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "child-plan-ph5-pending",
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
			Spec: tideprojectv1alpha1.PlanSpec{PhaseRef: autoPhaseName},
		}
		Expect(k8sClient.Create(ctx, childPlan)).To(Succeed())
		waitForCacheSync("child-plan-ph5-pending", "default", &tideprojectv1alpha1.Plan{})

		// Gate assertion 2: observed=1 >= expected=1, but Plan is not yet Succeeded →
		// Phase must still requeue (BoundaryDetected returns false).
		Eventually(func(g Gomega) {
			res, rerr := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: autoPhaseName, Namespace: "default"}})
			g.Expect(rerr).NotTo(HaveOccurred())
			g.Expect(res.RequeueAfter).To(BeNumerically(">", 0),
				"Phase must requeue while child Plans exist but are not yet Succeeded")
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// Gate assertion 3: patch the child Plan to Succeeded → Phase now Succeeds.
		var latestPlan tideprojectv1alpha1.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: "child-plan-ph5-pending", Namespace: "default"}, &latestPlan)).To(Succeed())
		planPatch := client.MergeFrom(latestPlan.DeepCopy())
		latestPlan.Status.Phase = "Succeeded"
		Expect(mgrClient.Status().Patch(ctx, &latestPlan, planPatch)).To(Succeed())

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, 3)).To(Succeed())

		Eventually(func(g Gomega) {
			var final tideprojectv1alpha1.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: autoPhaseName, Namespace: "default"}, &final)).To(Succeed())
			g.Expect(final.Status.Phase).To(Equal("Succeeded"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("Test 4: dispatches planner Job tide-phase-<uid>-1 and patches Status.Phase=Running", func() {
		phase := &tideprojectv1alpha1.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseName, Namespace: "default"},
			Spec:       tideprojectv1alpha1.PhaseSpec{MilestoneRef: milestoneName},
		}
		Expect(k8sClient.Create(ctx, phase)).To(Succeed())
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &tideprojectv1alpha1.Phase{})
		}, "5s", "100ms").Should(Succeed())

		r := &PhaseReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      newMapEnvReader(),
			SubagentImage:  testSubagentImage,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: phaseName, Namespace: "default"}, 5)).To(Succeed())

		Eventually(func(g Gomega) {
			var got tideprojectv1alpha1.Phase
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: phaseName, Namespace: "default"}, &got)).To(Succeed())
			expectedJobName := fmt.Sprintf("tide-phase-%s-1", got.UID)
			var job batchv1.Job
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: expectedJobName, Namespace: "default"}, &job)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})
})
