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

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

var _ = Describe("PlanReconciler — planner dispatch (Phase 3)", Label("envtest", "phase3"), func() {
	const planName = "plan-planner-dispatch"
	const phaseRef = "phase-planner-dispatch"
	const milestoneRefName = "milestone-planner-dispatch"
	const projectRefName = "project-planner-dispatch"
	ctx := context.Background()

	AfterEach(func() {
		p := &tideprojectv1alpha2.Plan{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, p); err == nil {
			p.Finalizers = nil
			_ = k8sClient.Update(ctx, p)
			_ = k8sClient.Delete(ctx, p)
		}
		ph := &tideprojectv1alpha2.Phase{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: phaseRef, Namespace: "default"}, ph); err == nil {
			ph.Finalizers = nil
			_ = k8sClient.Update(ctx, ph)
			_ = k8sClient.Delete(ctx, ph)
		}
		ms := &tideprojectv1alpha2.Milestone{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: milestoneRefName, Namespace: "default"}, ms); err == nil {
			ms.Finalizers = nil
			_ = k8sClient.Update(ctx, ms)
			_ = k8sClient.Delete(ctx, ms)
		}
		cleanupProject(projectRefName)
		var jobs batchv1.JobList
		_ = k8sClient.List(ctx, &jobs, client.InNamespace("default"))
		for i := range jobs.Items {
			j := jobs.Items[i]
			_ = k8sClient.Delete(ctx, &j)
		}
	})

	It("Test 5: dispatches planner Job when Plan has no Tasks yet", func() {
		// Cascade-7 fix: reconcilePlannerDispatch now gates on
		// resolveProjectForPlan != nil. This test exercises the happy path
		// (dispatch proceeds when Project chain resolves), so we create the
		// full Project → Milestone → Phase → Plan hierarchy in-test.
		makeProjectForTask(projectRefName)
		ms := &tideprojectv1alpha2.Milestone{
			ObjectMeta: metav1.ObjectMeta{Name: milestoneRefName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.MilestoneSpec{ProjectRef: projectRefName},
		}
		Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		waitForCacheSync(milestoneRefName, "default", &tideprojectv1alpha2.Milestone{})
		ph := &tideprojectv1alpha2.Phase{
			ObjectMeta: metav1.ObjectMeta{Name: phaseRef, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PhaseSpec{MilestoneRef: milestoneRefName},
		}
		Expect(k8sClient.Create(ctx, ph)).To(Succeed())
		waitForCacheSync(phaseRef, "default", &tideprojectv1alpha2.Phase{})

		// Plan with no Tasks and no ValidationState — should trigger planner dispatch.
		p := &tideprojectv1alpha2.Plan{
			ObjectMeta: metav1.ObjectMeta{Name: planName, Namespace: "default"},
			Spec:       tideprojectv1alpha2.PlanSpec{PhaseRef: phaseRef},
		}
		Expect(k8sClient.Create(ctx, p)).To(Succeed())
		// Wait for cache.
		Eventually(func() error {
			return mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &tideprojectv1alpha2.Plan{})
		}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

		r := &PlanReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      newMapEnvReader(),
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())

		// Verify the planner Job exists.
		Eventually(func(g Gomega) {
			var got tideprojectv1alpha2.Plan
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &got)).To(Succeed())
			expectedJobName := fmt.Sprintf("tide-plan-%s-1", got.UID)
			var job batchv1.Job
			g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: expectedJobName, Namespace: "default"}, &job)).To(Succeed())
			g.Expect(got.Status.Phase).To(Equal("Running"))
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	})

	It("Test 6: preserves Wave materialization when Plan already has Validated Tasks", func() {
		// Plan with Validated state and pre-existing Tasks should skip planner dispatch
		// and run Wave materialization (Phase 2 D-E1 path).
		makePlan(planName, phaseRef, "Validated")
		taskNames := alphaThroughThetaFixture(planName)
		defer cleanupPlanFixture(planName, taskNames)

		var got tideprojectv1alpha2.Plan
		Expect(mgrClient.Get(ctx, types.NamespacedName{Name: planName, Namespace: "default"}, &got)).To(Succeed())
		planUID := got.UID

		r := &PlanReconciler{
			Client:         mgrClient,
			Scheme:         k8sClient.Scheme(),
			Dispatcher:     &stubDispatcher{},
			PlannerPool:    newPlannerPoolForTest(),
			EnvReader:      newMapEnvReader(),
			SubagentImage:  testSubagentImage, // dead since Phase 13; HelmProviderDefaults.Image is the default tier
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			HelmProviderDefaults: ProviderDefaults{
				Image: testSubagentImage,
			},
		}

		Expect(reconcileWithRetry(r.Reconcile, types.NamespacedName{Name: planName, Namespace: "default"}, 5)).To(Succeed())

		// Wave materialization should have created Waves (3 from α…θ fixture).
		Eventually(func(g Gomega) {
			for i := range 3 {
				waveName := fmt.Sprintf("tide-wave-%s-%d", planUID, i)
				var wave tideprojectv1alpha2.Wave
				g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: waveName, Namespace: "default"}, &wave)).To(Succeed())
			}
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

		// NO planner Job should be created (Tasks already exist).
		expectedPlannerJob := fmt.Sprintf("tide-plan-%s-1", planUID)
		var jobs batchv1.JobList
		Expect(mgrClient.List(ctx, &jobs, client.InNamespace("default"))).To(Succeed())
		for _, j := range jobs.Items {
			Expect(j.Name).NotTo(Equal(expectedPlannerJob), "planner job should not be dispatched when Tasks already exist")
		}
	})
})
